package domain

import "strings"

// Post-Quanten-Bewertung des SSH-Schlüsselaustauschs.
//
// Warum ausgerechnet der Schlüsselaustausch und nicht die Verschlüsselung:
// Ein Quantenrechner bedroht beide Seiten unterschiedlich stark.
//
//   - Symmetrische Verfahren (AES-256-GCM für die Felder und die Backups,
//     HMAC-SHA256 für die Blindindizes, argon2id für Passwörter) trifft der
//     Grover-Algorithmus. Er halbiert die effektive Schlüssellänge - AES-256
//     bleibt damit bei 128 Bit und außer Reichweite. Diese Verfahren sind
//     bereits quantenresistent; ein Austausch machte sie schwächer.
//   - Asymmetrische Verfahren bricht der Shor-Algorithmus vollständig. Genau
//     die stecken im SSH-Schlüsselaustausch: Aus einem klassischen
//     Diffie-Hellman lässt sich der Sitzungsschlüssel später berechnen.
//
// Der Unterschied ist praktisch, nicht theoretisch: Wer den Verkehr HEUTE
// mitschneidet, entschlüsselt ihn, sobald es die Rechner gibt („harvest now,
// decrypt later"). Über diese Verbindungen laufen Root-Kommandos und
// Passwörter - der Mitschnitt lohnt sich für einen Angreifer also.
//
// Deshalb wird das ausgehandelte Verfahren je Verbindung festgehalten und
// bewertet.

// pqKexAlgorithms sind die Schlüsselaustausch-Verfahren mit
// Post-Quanten-Anteil. Alle sind HYBRIDE: Sie kombinieren ein klassisches
// X25519 mit einem quantenresistenten Verfahren, sodass die Verbindung nicht
// schlechter wird, falls im neuen Verfahren ein Fehler gefunden wird.
//
// LCM selbst bietet nur mlkem768x25519 an (x/crypto/ssh folgt dem
// NIST-Standard ML-KEM). Die sntrup-Einträge stehen hier, weil Gegenstellen
// sie aushandeln können, sobald LCM sie einmal unterstützt - und damit die
// Bewertung nicht falsch ausfällt, wenn es so weit ist.
var pqKexAlgorithms = map[string]bool{
	"mlkem768x25519-sha256":                  true,
	"mlkem1024x448-sha512":                   true,
	"sntrup761x25519-sha512":                 true,
	"sntrup761x25519-sha512@openssh.com":     true,
	"sntrup4591761x25519-sha512@tinyssh.org": true,
}

// KexPostQuantum meldet, ob ein ausgehandelter Schlüsselaustausch einen
// Post-Quanten-Anteil hat. Leer = unbekannt (noch keine Verbindung erfasst).
func KexPostQuantum(kex string) bool {
	return pqKexAlgorithms[strings.TrimSpace(kex)]
}

// KexKnown meldet, ob überhaupt ein Verfahren erfasst wurde. Ohne Erfassung
// wird NICHT bewertet: Nichtwissen ist kein Befund - dieselbe Regel wie bei
// den Speicher-Verbünden.
func KexKnown(kex string) bool {
	return strings.TrimSpace(kex) != ""
}

// MinOpenSSHForMLKEM ist die OpenSSH-Fassung, ab der mlkem768x25519-sha256
// angeboten wird. Davor kennt OpenSSH nur sntrup761, das LCM nicht anbietet -
// die Verbindung fällt dann auf klassisches curve25519 zurück, obwohl BEIDE
// Seiten je ein Post-Quanten-Verfahren beherrschen. Genau diese Konstellation
// ist der Regelfall auf Ubuntu 24.04 LTS (OpenSSH 9.6).
const MinOpenSSHForMLKEM = "9.9"
