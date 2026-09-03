package domain

import "time"

// HealthProbe ist die eine Zeile, an der die Selbstaufsicht prüft, ob LCM
// noch ARBEITEN kann - nicht nur, ob es die Datenbank erreicht.
//
// Der Unterschied ist im Betrieb belegt: Hält ein fremder Vorgang die
// Schreibsperre der Datenbank, antwortet ein Verbindungs-Ping unverändert
// (SQLite lässt im WAL-Modus jeden Leser durch), während jeder Schreibvorgang
// scheitert. In einem Testlauf meldete der Dienst zweieinhalb Minuten lang
// „operational", konnte in dieser Zeit aber keine einzige Zeile schreiben -
// zwölf geplante Läufe fielen aus, und zwar spurlos: Nicht einmal die
// Jobzeile, die den Ausfall dokumentiert hätte, ließ sich anlegen.
//
// Deshalb schreibt die Prüfung. Eine Zeile, immer dieselbe (ID 1), immer nur
// ein neuer Zeitstempel: kein Wachstum, keine Fremdschlüssel, keine Bedeutung
// über die Prüfung hinaus. CheckedAt taugt nebenbei zur Diagnose - er sagt,
// wann die Datenbank zuletzt nachweislich beschreibbar war.
type HealthProbe struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	CheckedAt time.Time `json:"checked_at"`
}

// HealthProbeID ist die ID der einen Zeile - wie bei den globalen
// Einstellungen und der Cache-Statistik.
const HealthProbeID = 1
