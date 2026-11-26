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
//	lcm/a/<agentID>/tty    Server→Agent   TTYFrame (Web-Konsole: öffnen,
//	                                      Tastatur, Fenstergröße, schließen)
//	lcm/a/<agentID>/ttyo   Agent→Server   TTYFrame (geöffnet, Bildschirm,
//	                                      beendet)
//
// Lebendprüfung (Capability "ping", nur additiv, nie ohne Capability):
// Meldet ein Agent im Inventory die Capability CapabilityPing, schickt LCM ihm
// alle PingInterval auf dem cmd-Topic ein Command{Ping: true} (QoS 0, ohne ID).
// Der Agent antwortet auf dem res-Topic mit Result{Pong: true} (QoS 0, ohne
// ID) und baut die Verbindung selbst neu auf, wenn mehrere Pings ausbleiben.
// So zählt die ganze Strecke einschließlich Abonnement, nicht nur TCP.
// Verträglichkeit: Ein Agent ohne die Capability bekommt nie ein Ping (LCM
// schickt keinen Nachrichtentyp, den er nicht kennt); und selbst ein Ping
// ohne ID überlesen alte Agenten (handleCommand verwirft Kommandos ohne ID),
// ebenso ein altes LCM das Pong (handleResult verwirft Ergebnisse ohne ID).
//
// Jobs, die Abbrüche überleben (Capability "jobs-v2", nur additiv):
// Meldet ein Agent im Inventory die Capability CapabilityJobs, führt er jedes
// Kommando als eigenen Job aus, der nicht am Agent-Prozess hängt (eigener
// systemd-Scope bzw. eigene Sitzung, Ausgabe und Exit-Code in einer Ablage auf
// der Platte). Daraus folgt für beide Seiten:
//   - Der Agent hält Kommandos nie von sich aus an: weder durch Stille noch
//     durch Neustart oder Absturz. Er ignoriert Command.IdleTimeoutSec. Nach
//     einem Neustart nimmt er laufende Jobs wieder auf, nach jedem Verbindungs-
//     aufbau stellt er noch nicht bestätigte Ergebnisse zu (QoS-1-PUBACK
//     bestätigt; Result.Recovered kennzeichnet ein aus der Ablage zugestelltes).
//   - Sein Lebenszeichen (Result.Progress) trägt Result.Vitals: was der Agent
//     am Kommando sieht (Prozessbaum, CPU, E/A, Stille, Warten).
//   - LCM lässt einen Aufruf bei Trennung nicht scheitern, sondern wartet auf
//     Wiederverbindung und Ergebnis, schickt kein IdleTimeoutSec und bricht nie
//     nach Zeit ab; nur ein ausdrücklicher Abbruch (Command.Cancel: SIGTERM,
//     nach Frist SIGKILL) hält ein Kommando an. Ein Cancel, das LCM bei
//     getrenntem Agent nicht zustellen kann, wird nach der Wiederverbindung
//     nachgereicht. Damit kein Aufrufer ewig blockiert, gibt LCM nach sehr
//     langer Zeit ohne jedes Lebenszeichen auf, ohne etwas zu beenden. Bei
//     VERBUNDENEM Agent genügt das Fünffache von ProgressInterval ohne Lebenszeichen
//     (ein Job meldet sich im Takt, solange der Agent ihn kennt), und ein Agent,
//     der im Inventar KnownJobs meldet, lässt Aufrufe scheitern, die er nicht
//     kennt: Sie sind nie bei ihm angekommen.
//
// Verträglichkeit: Ein Agent ohne die Capability (jede 2.x-Fassung vor jobs-v2)
// behält das bisherige Protokoll samt Verhalten: LCM lässt dessen Aufrufe bei
// Trennung scheitern, bricht ein Kommando nach der erlaubten Stille ab und
// schickt dieselbe Frist als Command.IdleTimeoutSec für die Notbremse des
// Agents: Ein hängendes Kommando ohne Jobs hielte den Server sonst für immer
// fest. Die neuen Felder überlesen ältere Agenten und LCM-Fassungen. Ein neuer Agent an einem alten LCM lässt Jobs ebenso über Abbrüche
// laufen; LCM verwirft dann nur das verspätete Ergebnis, wie bisher.
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
// Agent-Port des LCM-Servers.
const WSPath = "/mqtt"

// Topic-Bausteine.
const (
	topicPrefix = "lcm/a/"
	TopicCmdSuf = "/cmd"
	TopicResSuf = "/res"
	TopicInvSuf = "/inv"
	TopicStaSuf = "/status"
	TopicTTYSuf = "/tty"
	TopicTTOSuf = "/ttyo"
)

func TopicCmd(agentID string) string    { return topicPrefix + agentID + TopicCmdSuf }
func TopicRes(agentID string) string    { return topicPrefix + agentID + TopicResSuf }
func TopicInv(agentID string) string    { return topicPrefix + agentID + TopicInvSuf }
func TopicStatus(agentID string) string { return topicPrefix + agentID + TopicStaSuf }
func TopicTTY(agentID string) string    { return topicPrefix + agentID + TopicTTYSuf }
func TopicTTYOut(agentID string) string { return topicPrefix + agentID + TopicTTOSuf }

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
	// Ping ist die Lebendprüfung (siehe Kopf des Pakets): kein Auftrag, ID
	// leer. Nur an Agenten mit CapabilityPing.
	Ping bool `json:"ping,omitempty"`
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
	// Vitals sind die Beobachtungen des Agents am Kommando, nur in Lebenszeichen
	// von Agenten mit CapabilityJobs.
	Vitals *Vitals `json:"vitals,omitempty"`
	// Recovered kennzeichnet ein Ergebnis, das der Agent aus seiner Job-Ablage
	// zustellt (nach Neustart oder Wiederverbindung), nicht direkt nach dem
	// Ende des Kommandos. Rein informativ.
	Recovered bool `json:"recovered,omitempty"`
	// Pong beantwortet ein Ping des Servers (ID leer, kein Ergebnis).
	Pong bool `json:"pong,omitempty"`
}

// Vitals beschreibt, was der Agent an einem Kommando sieht. Er beurteilt
// selbst, ob es noch arbeitet; LCM zeigt es an („läuft, seit X ohne Ausgabe“)
// und hält deshalb nichts nach Zeit an. Alle Zahlen sind Näherungen (Linux:
// /proc, Summe über alle Prozesse der Job-Sitzung); was der Agent nicht
// ermitteln kann, bleibt 0.
type Vitals struct {
	// Alive: Der Prozessbaum des Kommandos lebt.
	Alive bool `json:"alive"`
	// Procs ist die Zahl der Prozesse im Baum (0 = nicht ermittelbar).
	Procs int `json:"procs,omitempty"`
	// CPUMillis ist die CPU-Zeit des Baums seit dem letzten Lebenszeichen.
	CPUMillis int64 `json:"cpu_ms,omitempty"`
	// IOBytes sind die gelesenen und geschriebenen Bytes des Baums seit dem
	// letzten Lebenszeichen (rchar+wchar aus /proc/<pid>/io).
	IOBytes int64 `json:"io_bytes,omitempty"`
	// SilentSec: Sekunden seit der letzten Ausgabe des Kommandos.
	SilentSec int `json:"silent_sec,omitempty"`
	// Waiting nennt, worauf das Kommando wartet (WaitingPrevious, WaitingLock),
	// leer, wenn es läuft.
	Waiting string `json:"waiting,omitempty"`
}

// Werte von Vitals.Waiting.
const (
	// WaitingPrevious: Das Kommando wartet, bis das vorige im Agent fertig ist
	// (es läuft höchstens eins zugleich).
	WaitingPrevious = "previous-command"
	// WaitingLock: Ein Prozess des Kommandos wartet auf eine Dateisperre (aus
	// /proc/locks, z.B. dpkg).
	WaitingLock = "lock"
)

// ProgressInterval ist der Takt der Lebenszeichen eines laufenden Kommandos.
// Deutlich kürzer als jede sinnvolle Stille-Frist, damit ein arbeitendes
// Kommando nie versehentlich als hängend gilt.
const ProgressInterval = 30 * time.Second

// PingInterval ist der Takt der Pings, die LCM an Agenten mit CapabilityPing
// schickt. Der Agent baut die Verbindung neu auf, wenn drei Pings in Folge
// ausbleiben.
const PingInterval = 30 * time.Second

// ProtocolVersion ist die Protokollversion dieses Pakets, die ein Agent im
// Inventory meldet. 1 hat jeder Agent, der das Feld nicht schickt (2.0 bis 2.3);
// 2 steht für Ping und Jobs-v2. Die Fähigkeiten (Capability*) sagen, was ein
// Agent im Einzelnen kann; die Protokollversion hebt nur, wer das Format
// einer bestehenden Nachricht NICHT mehr additiv erweitern kann - LCM stuft
// einen Agent mit höherem Protokoll als dem eigenen als „neuer als LCM“ ein
// und bietet nur an, was es selbst kennt (internal/core/agentcaps).
const ProtocolVersion = 2

// Inventory schickt der Agent bei jedem Connect - Versions-/Hostinfo für
// die Anzeige; der vollständige System-Scan läuft weiterhin serverseitig
// über die normalen Scan-Kommandos.
type Inventory struct {
	AgentVersion string `json:"agent_version"`
	// ProtocolVersion ist die Protokollversion, die der Agent spricht
	// (ProtocolVersion). Fehlt das Feld, spricht er Protokoll 1 (2.0 bis 2.3).
	ProtocolVersion int    `json:"protocol_version,omitempty"`
	Hostname        string `json:"hostname"`
	Kernel          string `json:"kernel,omitempty"`
	// KeyPin ist der Schlüssel-Pin, den der Agent führt (SHA-256 über die
	// SubjectPublicKeyInfo des LCM-Zertifikats, hex). Agenten bis 2.0
	// schicken ihn nicht - sie pinnen nur das Zertifikat selbst, und LCM
	// stellt sein Zertifikat erst um, wenn kein solcher Agent mehr übrig ist.
	KeyPin string `json:"key_pin,omitempty"`
	// CAPin ist der Fingerprint der LCM-CA, der der Agent vertraut (SHA-256
	// über das CA-Zertifikat, hex). Erst wenn jeder Agent ihn meldet, bekommt
	// das Server-Zertifikat bei der Erneuerung einen neuen Schlüssel.
	CAPin string `json:"ca_pin,omitempty"`
	// Capabilities nennt, was der Agent über Kommandos hinaus kann (z.B.
	// CapabilityTTY). Ältere Agents schicken das Feld nicht - LCM bietet
	// ihnen dann nichts davon an, statt ins Leere zu senden.
	Capabilities []string `json:"capabilities,omitempty"`
	// KnownJobs sind die IDs der Kommandos, die der Agent kennt: angenommene,
	// laufende und fertige mit noch unbestätigtem Ergebnis (nur von Agenten mit
	// CapabilityJobs, die es melden). Ein Agent ohne Kommandos schickt eine leere
	// Liste, ein älterer gar nichts (nil): Dann weiß LCM nichts. Kommandos, die
	// LCM vor dieser Verbindung abgeschickt hat und die hier fehlen, haben den Agent
	// nie erreicht - LCM lässt den wartenden Aufruf scheitern, statt zu warten.
	KnownJobs []string `json:"known_jobs"`
	// RolledBack nennt die Agent-Version, von der der Agent nach einem Update
	// auf das vorige Binary zurückgerollt hat, weil sich die neue nicht
	// verbinden konnte (leer: kein Rollback). Ältere Agenten schicken das Feld
	// nicht. LCM zeigt es als gescheitertes Update und als Befund am Server.
	RolledBack string `json:"rolled_back,omitempty"`
	// RolledBackTo ist die Version, die nach dem Rückfall läuft, und
	// RolledBackReason der Grund: crash-loop (das neue Programm stürzte in
	// Folge ab) oder not-ready (es wurde nicht rechtzeitig bereit). Beides nur
	// zusammen mit RolledBack.
	RolledBackTo     string `json:"rolled_back_to,omitempty"`
	RolledBackReason string `json:"rolled_back_reason,omitempty"`
}

// Fähigkeiten eines Agents (Inventory.Capabilities).
const (
	// CapabilityTTY: Der Agent öffnet Terminals (Web-Konsole).
	CapabilityTTY = "tty"
	// CapabilityTTYOff: Der Agent könnte es, der Betreiber hat es auf dem
	// Server abgeschaltet (lcm-agent terminal off).
	CapabilityTTYOff = "tty-off"
	// CapabilityPing: Der Agent beantwortet die Pings des Servers und prüft
	// damit seine Verbindung (siehe Kopf des Pakets).
	CapabilityPing = "ping"
	// CapabilityJobs: Der Agent führt Kommandos als Jobs aus, die Abbrüche und
	// Neustarts des Agents überleben (siehe Kopf des Pakets).
	CapabilityJobs = "jobs-v2"
	// CapabilitySelfUpdate: Das Paket des Agents sperrt sich gegen normale
	// Upgrades (apt-Pin) und bringt den Rückfall auf das vorige Programm mit.
	// Ohne sie lässt sich der Agent trotzdem aktualisieren, nur ohne Sperre
	// und ohne Rückfall.
	CapabilitySelfUpdate = "self-update-v2"
)

// ServerCertMarker steht als URI im Server-Zertifikat, das die LCM-CA
// ausstellt. Ein Agent, der die CA pinnt, nimmt nur ein Zertifikat mit dieser
// Kennung an - eine CA des Betreibers stellt oft auch Zertifikate für andere
// Dienste aus, und die sollen sich nicht als LCM ausgeben können.
const ServerCertMarker = "urn:lcm:server"

// MaxPacketSize ist die Broker-Obergrenze je MQTT-Paket; MaxOutputBytes das
// Agent-Limit für den kombinierten Kommando-Output, den er im Speicher hält
// (Anfang und Ende, Truncated=true). MaxResultBytes begrenzt das
// serialisierte Result: die Paketgrenze abzüglich Reserve für Topic und
// MQTT-Kopf. Darauf kommt es an - JSON kann die Rohausgabe bis aufs Sechsfache
// aufblähen, der Agent kürzt deshalb nach der kodierten Größe nach.
const (
	MaxPacketSize  = 8 * 1024 * 1024
	MaxOutputBytes = 4 * 1024 * 1024
	MaxResultBytes = MaxPacketSize - 64*1024
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

// Token ist der Inhalt eines Enrollment-Tokens.
type Token struct {
	AgentID string // MQTT-Username/Client-ID
	Secret  string // Credential (at rest nur als SHA-256-Hash)
	// CertFingerprint ist der SHA-256 (hex) des Server-Zertifikats, leer =
	// kein Pin.
	CertFingerprint string
	// CAFingerprint ist der SHA-256 (hex) der LCM-CA, die das
	// Server-Zertifikat ausstellt. Tokens älterer LCM tragen ihn nicht, ältere
	// Agenten überlesen ihn.
	CAFingerprint string
	// CustomCAFingerprint ist der SHA-256 (hex) der CA, die ein eigenes
	// Zertifikat des Betreibers ausgestellt hat (Heim-CA). Das Token trägt
	// ihn nur, wenn LCM für die Adresse des Agents ein solches ausliefert.
	// Ältere Agenten überlesen das Feld: Sie halten sich dann an den
	// Fingerprint des Zertifikats und die LCM-CA.
	CustomCAFingerprint string
}

// tokenPayload ist der base64url-kodierte JSON-Inhalt des Tokens.
// Bewusst kurze Feldnamen - das Token wird von Hand kopiert.
type tokenPayload struct {
	AgentID     string `json:"u"`
	Secret      string `json:"k"`
	Fingerprint string `json:"f"`
	CA          string `json:"c,omitempty"`
	CustomCA    string `json:"h,omitempty"`
}

// EncodeToken baut das Klartext-Token für die einmalige Anzeige (Server)
// bzw. fürs Enrollment (Agent).
func EncodeToken(t Token) string {
	raw, _ := json.Marshal(tokenPayload{AgentID: t.AgentID, Secret: t.Secret, Fingerprint: t.CertFingerprint,
		CA: t.CAFingerprint, CustomCA: t.CustomCAFingerprint})
	return TokenPrefix + base64.RawURLEncoding.EncodeToString(raw)
}

// ParseToken zerlegt ein Klartext-Token (Gegenstück zu EncodeToken).
func ParseToken(token string) (Token, error) {
	if !strings.HasPrefix(token, TokenPrefix) {
		return Token{}, errors.New("kein LCM-Agent-Token (Präfix lcma1. fehlt)")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(token, TokenPrefix))
	if err != nil {
		return Token{}, fmt.Errorf("token dekodieren: %w", err)
	}
	var p tokenPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return Token{}, fmt.Errorf("token lesen: %w", err)
	}
	if p.AgentID == "" || p.Secret == "" {
		return Token{}, errors.New("unvollständiges Token")
	}
	return Token{AgentID: p.AgentID, Secret: p.Secret, CertFingerprint: p.Fingerprint,
		CAFingerprint: p.CA, CustomCAFingerprint: p.CustomCA}, nil
}
