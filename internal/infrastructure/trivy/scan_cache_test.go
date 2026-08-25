package trivy

import (
	"fmt"
	"testing"
	"time"

	"LCM/internal/core/domain"
)

// debTarget baut ein Debian-Ziel mit den gegebenen Paketen.
func debTarget(pkgs ...domain.Package) Target {
	return Target{OSID: "debian", OSVersionID: "12", PackageManager: "apt", Packages: pkgs}
}

// TestScanCacheKeyStabilGegenReihenfolge: Zwei Server mit demselben Bestand in
// anderer Reihenfolge müssen denselben Schlüssel ergeben - sonst verfehlt der
// Cache genau den Flottenfall, für den er gebaut ist.
func TestScanCacheKeyStabilGegenReihenfolge(t *testing.T) {
	a := scanCacheKey(debTarget(
		domain.Package{Name: "openssl", Version: "3.0.11"},
		domain.Package{Name: "bash", Version: "5.2"},
	))
	b := scanCacheKey(debTarget(
		domain.Package{Name: "bash", Version: "5.2"},
		domain.Package{Name: "openssl", Version: "3.0.11"},
	))
	if a != b {
		t.Error("gleicher Bestand in anderer Reihenfolge ergab verschiedene Schlüssel")
	}
}

// TestScanCacheKeyUnterscheidetInhalte: Version, Distribution und
// Paketverwaltung gehören in den Schlüssel - jede Abweichung ist ein anderer
// Scan-Inhalt.
func TestScanCacheKeyUnterscheidetInhalte(t *testing.T) {
	base := scanCacheKey(debTarget(domain.Package{Name: "openssl", Version: "3.0.11"}))

	otherVersion := scanCacheKey(debTarget(domain.Package{Name: "openssl", Version: "3.0.12"}))
	if base == otherVersion {
		t.Error("andere Paketversion muss einen anderen Schlüssel ergeben")
	}

	otherDistro := scanCacheKey(Target{
		OSID: "debian", OSVersionID: "13", PackageManager: "apt",
		Packages: []domain.Package{{Name: "openssl", Version: "3.0.11"}},
	})
	if base == otherDistro {
		t.Error("andere Distribution muss einen anderen Schlüssel ergeben")
	}

	otherMgr := scanCacheKey(Target{
		OSID: "centos", OSVersionID: "9", PackageManager: "dnf",
		Packages: []domain.Package{{Name: "openssl", Version: "3.0.11"}},
	})
	otherMgr2 := scanCacheKey(Target{
		OSID: "centos", OSVersionID: "9", PackageManager: "yum",
		Packages: []domain.Package{{Name: "openssl", Version: "3.0.11"}},
	})
	if otherMgr == otherMgr2 {
		t.Error("andere Paketverwaltung muss einen anderen Schlüssel ergeben")
	}
}

// TestScanCacheHitUndKopie: Ein Treffer liefert das gemerkte Ergebnis - aber
// als Kopie: Die Aufrufer beschriften die Funde je Server, und das darf den
// Cache nicht verändern.
func TestScanCacheHitUndKopie(t *testing.T) {
	var c scanCache
	stamp := time.Date(2026, 8, 18, 6, 0, 0, 0, time.UTC)
	c.put(stamp, "k", []domain.Vulnerability{{CVEID: "CVE-1", PackageName: "openssl"}})

	got, ok := c.get(stamp, "k")
	if !ok || len(got) != 1 || got[0].CVEID != "CVE-1" {
		t.Fatalf("Treffer erwartet, bekam ok=%v %+v", ok, got)
	}
	got[0].ServerRef = "mutiert"

	again, _ := c.get(stamp, "k")
	if again[0].ServerRef != "" {
		t.Error("Mutation am gelieferten Ergebnis schlug in den Cache durch")
	}
}

// TestScanCacheLeeresErgebnisIstEinTreffer: „Keine Funde" ist die häufigste
// und wertvollste Antwort - auch sie muss aus dem Cache kommen.
func TestScanCacheLeeresErgebnisIstEinTreffer(t *testing.T) {
	var c scanCache
	stamp := time.Date(2026, 8, 18, 6, 0, 0, 0, time.UTC)
	c.put(stamp, "sauber", nil)

	got, ok := c.get(stamp, "sauber")
	if !ok {
		t.Fatal("leeres Ergebnis muss als Treffer zählen")
	}
	if len(got) != 0 {
		t.Errorf("erwartet keine Funde, bekam %d", len(got))
	}
}

// TestScanCacheNeueDatenbankEntwertetAlles: Ein neuer Datenbank-Stand macht
// jeden alten Eintrag wertlos - und ein Nachzügler mit altem Anker darf den
// Cache nicht zurückwerfen.
func TestScanCacheNeueDatenbankEntwertetAlles(t *testing.T) {
	var c scanCache
	old := time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC)
	fresh := old.Add(6 * time.Hour)

	c.put(old, "k", []domain.Vulnerability{{CVEID: "CVE-alt"}})
	if _, ok := c.get(fresh, "k"); ok {
		t.Fatal("neuer Datenbank-Stand darf keinen alten Eintrag liefern")
	}

	c.put(fresh, "k", []domain.Vulnerability{{CVEID: "CVE-neu"}})
	// Nachzügler mit altem Anker: Ergebnis verfällt, Cache bleibt auf neu.
	c.put(old, "k", []domain.Vulnerability{{CVEID: "CVE-alt"}})
	got, ok := c.get(fresh, "k")
	if !ok || got[0].CVEID != "CVE-neu" {
		t.Errorf("Nachzügler hat den Cache zurückgeworfen: ok=%v %+v", ok, got)
	}
	if _, ok := c.get(old, "k"); ok {
		t.Error("alter Anker darf nach der Umstellung keinen Treffer liefern")
	}
}

// TestScanCacheOhneZeitstempelStumm: Ohne lesbaren Datenbank-Stand gibt es
// keinen Gültigkeitsanker - der Cache bleibt stumm statt zu raten.
func TestScanCacheOhneZeitstempelStumm(t *testing.T) {
	var c scanCache
	c.put(time.Time{}, "k", []domain.Vulnerability{{CVEID: "CVE-1"}})
	if _, ok := c.get(time.Time{}, "k"); ok {
		t.Error("Zero-Stamp darf weder speichern noch liefern")
	}
}

// TestScanCacheVerdraengung: Die Obergrenze wird eingehalten; verdrängt wird
// der älteste Eintrag (FIFO), der jüngste bleibt.
func TestScanCacheVerdraengung(t *testing.T) {
	var c scanCache
	stamp := time.Date(2026, 8, 18, 6, 0, 0, 0, time.UTC)
	for i := 0; i < scanCacheLimit+1; i++ {
		c.put(stamp, fmt.Sprintf("k%d", i), nil)
	}
	if len(c.vulns) != scanCacheLimit {
		t.Errorf("erwartet %d Einträge, sind %d", scanCacheLimit, len(c.vulns))
	}
	if _, ok := c.get(stamp, "k0"); ok {
		t.Error("der älteste Eintrag muss verdrängt sein")
	}
	if _, ok := c.get(stamp, fmt.Sprintf("k%d", scanCacheLimit)); !ok {
		t.Error("der jüngste Eintrag muss erhalten bleiben")
	}
}

// TestScanCacheNebenlaeufig lässt Schreiber und Leser parallel auf den Cache
// los - der Test existiert für den Race-Detector (make test läuft mit -race).
func TestScanCacheNebenlaeufig(t *testing.T) {
	var c scanCache
	stamp := time.Date(2026, 8, 18, 6, 0, 0, 0, time.UTC)
	done := make(chan struct{})
	for w := 0; w < 4; w++ {
		go func(w int) {
			defer func() { done <- struct{}{} }()
			for i := 0; i < 200; i++ {
				key := fmt.Sprintf("k%d", i%8)
				c.put(stamp.Add(time.Duration(w)*time.Hour), key, []domain.Vulnerability{{CVEID: "CVE-1"}})
				c.get(stamp, key)
			}
		}(w)
	}
	for w := 0; w < 4; w++ {
		<-done
	}
}

// TestScanCacheZaehltTrefferUndFehlgriffe: Die Quote ist das Maß dafür, wie
// gleichförmig die Flotte ist - sie muss über die Laufzeit stimmen.
func TestScanCacheZaehltTrefferUndFehlgriffe(t *testing.T) {
	var c scanCache
	stamp := time.Date(2026, 8, 19, 6, 0, 0, 0, time.UTC)

	c.get(stamp, "k") // Fehlgriff (leer)
	c.put(stamp, "k", []domain.Vulnerability{{CVEID: "CVE-1"}})
	c.get(stamp, "k")       // Treffer
	c.get(stamp, "k")       // Treffer
	c.get(stamp, "anderer") // Fehlgriff

	st := c.stats()
	if st.Hits != 2 || st.Misses != 2 {
		t.Errorf("erwartet 2 Treffer / 2 Fehlgriffe, war %d/%d", st.Hits, st.Misses)
	}
	if st.Entries != 1 || st.Limit != scanCacheLimit {
		t.Errorf("Belegung falsch: %+v", st)
	}
	if st.Stamp == nil || !st.Stamp.Equal(stamp) {
		t.Errorf("Datenbank-Stand fehlt: %+v", st.Stamp)
	}
}

// TestScanCacheZaehlerUeberlebenDenDatenbankWechsel: Beim Wechsel verfallen
// die EINTRÄGE - die Zähler nicht. Sonst stünde die Trefferquote nach jedem
// 6-Stunden-Zug wieder bei null und wäre nie aussagekräftig.
func TestScanCacheZaehlerUeberlebenDenDatenbankWechsel(t *testing.T) {
	var c scanCache
	old := time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC)
	new := old.Add(6 * time.Hour)

	c.put(old, "k", nil)
	c.get(old, "k") // Treffer

	c.put(new, "k", nil) // neuer Stand: Einträge verfallen
	st := c.stats()
	if st.Hits != 1 {
		t.Errorf("Treffer-Zähler wurde zurückgesetzt: %d", st.Hits)
	}
	if st.Entries != 1 {
		t.Errorf("nach dem Wechsel sollte genau der neue Eintrag stehen, waren %d", st.Entries)
	}
}
