package trivy

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"
	"sync"
	"time"

	"LCM/internal/core/domain"
)

// scanCache merkt sich Scan-Ergebnisse inhaltsadressiert: Schlüssel ist der
// Paketbestand (sortierte purl-Liste), Gültigkeitsanker der Bau-Zeitstempel
// der Schwachstellen-Datenbank. Trivy ist deterministisch - gleicher Bestand
// plus gleiche Datenbank ergibt zwangsläufig dasselbe Ergebnis. Deshalb gibt
// es hier bewusst KEIN Ablaufdatum und keine Einstellung: Der Cache kann
// nichts Veraltetes liefern; ändert sich ein Paket oder die Datenbank,
// ändert sich der Schlüssel bzw. der Anker.
//
// Der Gewinn liegt im Flottenbetrieb: 50 fast identische Debian-Server
// hießen bisher 50 SBOM-Bauten, 50 Sandbox-Prozessstarts und 50 vollständige
// Auswertungen - für im Wesentlichen dieselbe Paketliste.
type scanCache struct {
	mu sync.Mutex
	// stamp ist das DB-UpdatedAt, zu dem alle Einträge gehören. Eine neue
	// Datenbank entwertet sämtliche Einträge auf einen Schlag - alte
	// Schlüssel sind dann tot und werden nicht einzeln verdrängt.
	stamp time.Time
	// order hält die Einfüge-Reihenfolge für die Verdrängung (FIFO genügt:
	// ein Scan-Lauf trifft jeden Bestand am Stück, nicht verstreut).
	order []string
	vulns map[string][]domain.Vulnerability
	// hits/misses zaehlen ueber die gesamte Laufzeit des Prozesses - auch
	// ueber einen Datenbank-Wechsel hinweg, der die Eintraege verwirft. Sie
	// beantworten die Frage, wie gleichfoermig die Flotte ist: Bei lauter
	// verschiedenen Paketstaenden bringt der Zwischenspeicher nichts, bei
	// einer homogenen Flotte spart er fast jeden Lauf.
	hits, misses int
}

// CacheStats ist der Zustand des Ergebnis-Zwischenspeichers.
type CacheStats struct {
	// Hits/Misses seit dem Start des Prozesses (der Zwischenspeicher liegt
	// im Arbeitsspeicher und beginnt bei jedem Neustart von vorn).
	Hits   int `json:"hits"`
	Misses int `json:"misses"`
	// Entries ist die aktuelle Belegung, Limit die Obergrenze.
	Entries int `json:"entries"`
	Limit   int `json:"limit"`
	// Stamp ist der Datenbank-Stand, an den die Eintraege gebunden sind.
	Stamp *time.Time `json:"stamp,omitempty"`
}

// stats liefert den Zustand des Zwischenspeichers.
func (c *scanCache) stats() CacheStats {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := CacheStats{Hits: c.hits, Misses: c.misses, Entries: len(c.vulns), Limit: scanCacheLimit}
	if !c.stamp.IsZero() {
		stamp := c.stamp
		out.Stamp = &stamp
	}
	return out
}

// scanCacheLimit begrenzt den Speicher: mehr verschiedene Paketbestände als
// Server wird es nie geben, und 256 unterschiedliche Flotten-Profile sind
// weit jenseits des Erwartbaren.
const scanCacheLimit = 256

// get liefert das gemerkte Ergebnis zu einem Bestand - als Kopie, denn die
// Aufrufer beschriften die Funde anschließend je Server (ServerRef etc.);
// das darf nicht in den Cache zurückschlagen. Ein Zero-Stamp (Datenbank noch
// nie geladen oder Stand nicht lesbar) schaltet den Cache stumm.
func (c *scanCache) get(stamp time.Time, key string) ([]domain.Vulnerability, bool) {
	if stamp.IsZero() {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !stamp.Equal(c.stamp) {
		c.misses++
		return nil, false
	}
	vulns, ok := c.vulns[key]
	if !ok {
		c.misses++
		return nil, false
	}
	c.hits++
	return slices.Clone(vulns), true
}

// put merkt sich ein Ergebnis. Ein neuer Datenbank-Stand leert den Cache
// komplett; auch hier wird eine Kopie gespeichert, damit der Aufrufer sein
// Original weiterverwenden kann.
func (c *scanCache) put(stamp time.Time, key string, vulns []domain.Vulnerability) {
	if stamp.IsZero() {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if stamp.Before(c.stamp) {
		// Nachzügler: Ein Scan, der noch mit dem alten Datenbank-Stand
		// begonnen hat, darf den bereits umgestellten Cache nicht auf den
		// alten Anker zurückwerfen - sein Ergebnis verfällt einfach.
		return
	}
	if !stamp.Equal(c.stamp) || c.vulns == nil {
		// Nur die Eintraege verfallen - die Zaehler laufen weiter, sonst
		// stuende die Trefferquote nach jedem Datenbank-Zug wieder bei null.
		c.stamp = stamp
		c.order = nil
		c.vulns = map[string][]domain.Vulnerability{}
	}
	if _, exists := c.vulns[key]; !exists {
		if len(c.order) >= scanCacheLimit {
			delete(c.vulns, c.order[0])
			c.order = c.order[1:]
		}
		c.order = append(c.order, key)
	}
	c.vulns[key] = slices.Clone(vulns)
}

// scanCacheKey bildet den Paketbestand eines Ziels auf einen stabilen
// Schlüssel ab: die sortierte, deduplizierte purl-Liste (inklusive Version
// und Distro-Kennung) plus die Paketverwaltung - letztere, weil sie in die
// Beschriftung der Funde einfließt (PkgManager) und sich z.B. dnf und yum
// bei identischen purls unterscheiden könnten. Dieselbe purl-Logik wie im
// SBOM-Bau (PurlFor), damit Schlüssel und Scan-Inhalt nie auseinanderlaufen.
func scanCacheKey(target Target) string {
	purls := make([]string, 0, len(target.Packages))
	seen := map[string]bool{}
	for _, p := range target.Packages {
		if p.Name == "" {
			continue
		}
		purl := PurlFor(target, p.Name, p.Version)
		if seen[purl] {
			continue
		}
		seen[purl] = true
		purls = append(purls, purl)
	}
	slices.Sort(purls)
	sum := sha256.Sum256([]byte(target.PackageManager + "\n" + strings.Join(purls, "\n")))
	return hex.EncodeToString(sum[:])
}
