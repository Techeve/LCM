// Package wire definiert das MQTT-Nachrichtenformat zwischen LCM-Server
// und lcm-agent (LCM Remote). Bewusst ein eigenes, abhängigkeitsfreies
// Paket: der Server (Broker/Hub) und das Agent-Binary teilen sich nur
// diese Typen - der Agent zieht so weder mochi noch GORM in sein Binary.
//
// Topic-Schema (alle Payloads JSON, QoS 1):
//
//	lcm/a/<agentID>/cmd    Server→Agent   Command{ID, Cmd, Stdin} bzw. Cancel
//	lcm/a/<agentID>/res    Agent→Server   Result{ID, Output, ExitCode, …}
//	lcm/a/<agentID>/inv    Agent→Server   Inventory (bei jedem Connect)
//	lcm/a/<agentID>/status Agent→Server   "online" / LWT "offline" (retained;
//	                                      rein informativ - die Online-
//	                                      Erkennung macht der Broker über
//	                                      Session-Events)
package wire

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// WSPath ist der WebSocket-Pfad des eingebetteten Brokers auf dem
// bestehenden HTTPS-Port des LCM-Servers.
const WSPath = "/mqtt"

// Topic-Bausteine.
const (
	topicPrefix = "lcm/a/"
	TopicCmdSuf = "/cmd"
	TopicResSuf = "/res"
	TopicInvSuf = "/inv"
	TopicStaSuf = "/status"
)

func TopicCmd(agentID string) string    { return topicPrefix + agentID + TopicCmdSuf }
func TopicRes(agentID string) string    { return topicPrefix + agentID + TopicResSuf }
func TopicInv(agentID string) string    { return topicPrefix + agentID + TopicInvSuf }
func TopicStatus(agentID string) string { return topicPrefix + agentID + TopicStaSuf }

// AgentIDFromTopic extrahiert die Agent-UUID aus einem lcm/a/<id>/…-Topic
// (leer, wenn das Topic nicht dem Schema entspricht).
func AgentIDFromTopic(topic string) string {
	if len(topic) <= len(topicPrefix) || topic[:len(topicPrefix)] != topicPrefix {
		return ""
	}
	rest := topic[len(topicPrefix):]
	for i := 0; i < len(rest); i++ {
		if rest[i] == '/' {
			return rest[:i]
		}
	}
	return ""
}

// Command ist ein Kommando-Auftrag des Servers an den Agent. Cancel=true
// bricht den Auftrag mit derselben ID ab (Kill der Prozessgruppe).
type Command struct {
	ID     string `json:"id"`
	Cmd    string `json:"cmd,omitempty"`
	Stdin  string `json:"stdin,omitempty"`
	Cancel bool   `json:"cancel,omitempty"`
	// IdleTimeoutSec ist die Notbremse im Agent: Erzeugt das Kommando so
	// lange keine Ausgabe mehr, gilt es als hängend und wird abgebrochen.
	// Gemessen wird die STILLE, nicht die Gesamtdauer - ein Upgrade darf
	// beliebig lange laufen, solange es dabei arbeitet. 0 = eingebauter
	// Vorgabewert (ältere Server schicken das Feld nicht).
	IdleTimeoutSec int `json:"idle_timeout_sec,omitempty"`
}

// Result ist die Antwort des Agents auf ein Command: kombinierter Output
// (stdout+stderr) und Exit-Code, analog zu sshx.Conn.RunStdin. Error meldet
// Fehler VOR der Ausführung (z.B. Shell nicht startbar); Truncated zeigt an,
// dass der Output das Agent-Limit überschritten hat und gekürzt wurde.
type Result struct {
	ID        string `json:"id"`
	Output    string `json:"output"`
	ExitCode  int    `json:"exit_code"`
	Error     string `json:"error,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
	// Progress kennzeichnet ein Lebenszeichen statt eines Ergebnisses: Der
	// Agent meldet damit im Takt von ProgressInterval, dass das Kommando noch
	// arbeitet, und nennt in OutputBytes den bisher gesammelten Umfang. Der
	// Server nimmt es als Aktivität entgegen und wartet weiter - erst ein
	// Result OHNE dieses Flag schließt den Auftrag ab.
	//
	// Bewusst dasselbe Topic und derselbe Typ wie das Ergebnis: Ein Agent, der
	// noch keine Fortschrittsmeldungen kennt, bleibt damit anschlussfähig, und
	// ein alter Server ignoriert das unbekannte Feld.
	Progress    bool `json:"progress,omitempty"`
	OutputBytes int  `json:"output_bytes,omitempty"`
}

// ProgressInterval ist der Takt der Lebenszeichen eines laufenden Kommandos.
// Deutlich kürzer als jede sinnvolle Stille-Frist, damit ein arbeitendes
// Kommando nie versehentlich als hängend gilt.
const ProgressInterval = 30 * time.Second

// Inventory schickt der Agent bei jedem Connect - Versions-/Hostinfo für
// die Anzeige; der vollständige System-Scan läuft weiterhin serverseitig
// über die normalen Scan-Kommandos.
type Inventory struct {
	AgentVersion string `json:"agent_version"`
	Hostname     string `json:"hostname"`
	Kernel       string `json:"kernel,omitempty"`
}

// MaxPacketSize ist die Broker-Obergrenze je MQTT-Paket; MaxOutputBytes das
// Agent-Limit für den kombinierten Kommando-Output (darüber wird gekürzt,
// Truncated=true). Das Output-Limit liegt bewusst unter der Paketgrenze
// (JSON-Overhead + Reserve).
const (
	MaxPacketSize  = 8 * 1024 * 1024
	MaxOutputBytes = 4 * 1024 * 1024
	StatusOnline   = "online"
	StatusOffline  = "offline"
)

// HashSecret bildet den At-Rest-/Vergleichs-Hash des Token-Secrets
// (SHA-256, hex) - die EINE Definition für Enrollment (services) und
// Broker-Auth (remote), damit beide Seiten nie auseinanderlaufen.
func HashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// ---- Enrollment-Token ------------------------------------------------------

// TokenPrefix kennzeichnet LCM-Agent-Tokens (erleichtert Secret-Scanning);
// die "1" ist die Formatversion.
const TokenPrefix = "lcma1."

// tokenPayload ist der base64url-kodierte JSON-Inhalt des Tokens.
// Bewusst kurze Feldnamen - das Token wird von Hand kopiert.
type tokenPayload struct {
	AgentID     string `json:"u"` // MQTT-Username/Client-ID
	Secret      string `json:"k"` // Credential (at rest nur als SHA-256-Hash)
	Fingerprint string `json:"f"` // SHA-256 (hex) des Server-Zertifikats, leer = kein Pin
}

// EncodeToken baut das Klartext-Token für die einmalige Anzeige (Server)
// bzw. fürs Enrollment (Agent).
func EncodeToken(agentID, secret, certFP string) string {
	raw, _ := json.Marshal(tokenPayload{AgentID: agentID, Secret: secret, Fingerprint: certFP})
	return TokenPrefix + base64.RawURLEncoding.EncodeToString(raw)
}

// ParseToken zerlegt ein Klartext-Token (Gegenstück zu EncodeToken).
func ParseToken(token string) (agentID, secret, certFP string, err error) {
	if !strings.HasPrefix(token, TokenPrefix) {
		return "", "", "", errors.New("kein LCM-Agent-Token (Präfix lcma1. fehlt)")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(token, TokenPrefix))
	if err != nil {
		return "", "", "", fmt.Errorf("token dekodieren: %w", err)
	}
	var p tokenPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", "", "", fmt.Errorf("token lesen: %w", err)
	}
	if p.AgentID == "" || p.Secret == "" {
		return "", "", "", errors.New("unvollständiges Token")
	}
	return p.AgentID, p.Secret, p.Fingerprint, nil
}
