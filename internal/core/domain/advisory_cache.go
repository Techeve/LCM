package domain

import "time"

// AdvisoryPollIntervalMinutes ist der Takt, in dem die Frühwarnung läuft.
// Er steht hier und nicht nur im Scheduler, weil die Cache-Gültigkeit ohne
// ihn nicht sinnvoll zu wählen ist: Ein Eintrag, der vor dem nächsten
// Durchgang abläuft, wird nie gelesen - nur geschrieben.
const AdvisoryPollIntervalMinutes = 15

// Grenzen der Cache-Gültigkeit. Ein Treffer im Cache heißt: Wir haben NICHT
// nachgesehen - und zwar genau in dem Zeitfenster, für das die Frühwarnung
// überhaupt gebaut wurde. Deshalb ist die Obergrenze bewusst niedrig: eine
// halbe Stunde ist der äußerste Wert, den man noch „frühwarnend" nennen kann,
// wenn die Trivy-Spur daneben mit 6 bis 12 Stunden läuft.
//
// Die UNTERGRENZE ist der Poll-Takt: Darunter kann der Zwischenspeicher gar
// nicht greifen, denn beim nächsten Durchgang ist jeder Eintrag schon
// abgelaufen. Ein solcher Wert wäre das Schlechteste aus beiden Welten -
// jeder Durchgang fragt alles neu UND schreibt alles neu. Genau das war der
// bisherige Standardwert von 10 Minuten bei 15 Minuten Takt: Die Trefferquote
// lag über Monate bei null.
//
// Der Standard liegt bewusst nicht auf dem Maximum: Er soll greifen, aber die
// Frühwarnung nicht blinder machen als nötig. Die fünf Minuten Abstand zum
// Takt sind der Puffer für einen Durchgang, der sich etwas verspätet.
const (
	AdvisoryCacheTTLDefault = 20 // Minuten
	AdvisoryCacheTTLMin     = AdvisoryPollIntervalMinutes
	AdvisoryCacheTTLMax     = 30 // Minuten; 0 = Cache aus
)

// AdvisoryCacheEntry merkt sich, welche Advisories zu einem Paket in einer
// bestimmten Version gehören.
//
// Schlüssel ist der vollständige purl inklusive Version und Distribution
// (z.B. "pkg:deb/debian/openssl@3.0.11-1~deb12u2?distro=debian-12"). Das ist
// wichtiger, als es aussieht: Der Paketname allein wäre wertlos - betroffen
// ist immer eine Versionsspanne auf einer bestimmten Distribution. Und weil
// die Version im Schlüssel steckt, entwertet ein Paket-Update den Eintrag von
// selbst; es braucht keine eigene Invalidierungslogik.
//
// Der Eintrag ist nicht server-bezogen: Fünfzig Server mit demselben
// Paketstand teilen sich einen Cache-Eintrag - genau darin liegt der Gewinn.
type AdvisoryCacheEntry struct {
	Purl string `gorm:"primarykey" json:"purl"`
	// Source: welche Quelle den Befund geliefert hat (AdvisorySource*).
	Source string `gorm:"not null" json:"source"`
	// AdvisoryIDs sind die gefundenen Kennungen, kommagetrennt. Leer heißt
	// ausdrücklich „geprüft und nichts gefunden" - das ist die häufigste und
	// wertvollste Antwort, sie muss als Treffer zählen.
	AdvisoryIDs string `json:"advisory_ids"`
	// CheckedAt ist der Zeitpunkt der letzten Abfrage; daran hängt die TTL.
	CheckedAt time.Time `gorm:"index" json:"checked_at"`
}

// Fresh meldet, ob der Eintrag innerhalb der TTL liegt. ttlMinutes <= 0
// schaltet den Cache ab - dann ist kein Eintrag jemals frisch.
func (e *AdvisoryCacheEntry) Fresh(now time.Time, ttlMinutes int) bool {
	if e == nil || ttlMinutes <= 0 {
		return false
	}
	age := now.Sub(e.CheckedAt)
	if age < 0 {
		// Eintrag aus der Zukunft (schiefe Uhr) - als frisch werten statt
		// in eine Dauerschleife aus Nachfragen zu laufen.
		return true
	}
	return age < time.Duration(ttlMinutes)*time.Minute
}

// AdvisoryDetail ist die Beschreibung einer Advisory-Kennung - Schwere,
// Titel, behebende Version.
//
// Bewusst getrennt vom Cache-Eintrag, weil beide unterschiedlich altern: Der
// BEFUND zu einem purl ist zeitkritisch (deshalb die TTL); die BESCHREIBUNG
// eines Advisories ist nahezu unveränderlich. Sie über eine Uhr verfallen zu
// lassen, hieße, dieselben Detaildaten immer wieder abzuholen. Maßgeblich ist
// stattdessen der Änderungsstempel der Quelle: Modified.
type AdvisoryDetail struct {
	AdvisoryID string `gorm:"primarykey" json:"advisory_id"`
	Source     string `gorm:"not null" json:"source"`
	Kind       string `gorm:"not null" json:"kind"`

	Severity string `json:"severity"`
	Title    string `json:"title"`
	URL      string `json:"url"`
	// FixedVersions hält die behebenden Versionen je Paketname als
	// "paket=version"-Paare, kommagetrennt - ein Advisory kann mehrere
	// Pakete betreffen, und der Fix ist je Paket ein anderer.
	FixedVersions string `json:"fixed_versions"`
	// Aliases sind weitere Kennungen derselben Sache (kommagetrennt).
	// Ohne sie ließe sich eine Distributions-Meldung wie DSA-5973-1 nicht
	// mit einer CVE-bezogenen Liste abgleichen - und genau solche Meldungen
	// sind bei Betriebssystempaketen der Regelfall.
	Aliases string `json:"aliases"`

	// Modified ist der Änderungsstempel der Quelle. Meldet die Quelle einen
	// neueren Stand, wird der Eintrag neu geholt - sonst nie.
	Modified  time.Time `json:"modified"`
	FetchedAt time.Time `json:"fetched_at"`
}

// AdvisoryCacheStats zählt die Wirksamkeit des Advisory-Zwischenspeichers
// über die Zeit.
//
// Warum in der Datenbank statt im Arbeitsspeicher: Die Frühwarnung läuft
// alle 15 Minuten, ihre Trefferquote ist eine Aussage über Tage. Ein Zähler,
// der bei jedem Neustart des Dienstes von vorn beginnt, könnte sie nicht
// treffen - man sähe nach jedem Update wieder eine Quote nahe null und
// hielte den Zwischenspeicher für wirkungslos.
//
// Genau eine Zeile (ID 1), wie bei den globalen Einstellungen.
type AdvisoryCacheStats struct {
	ID uint `gorm:"primarykey" json:"-"`
	// Hits sind die purls, die aus dem Zwischenspeicher beantwortet wurden,
	// Misses die, für die nachgefragt werden musste.
	Hits   int64 `json:"hits"`
	Misses int64 `json:"misses"`
	// Runs ist die Zahl der Durchgänge, über die gezählt wurde.
	Runs int64 `json:"runs"`
	// SinceAt ist der Beginn der Zählung - ohne ihn wäre eine Quote nicht
	// einzuordnen.
	SinceAt   time.Time  `json:"since_at"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}
