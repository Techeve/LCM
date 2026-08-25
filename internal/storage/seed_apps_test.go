package storage

import (
	"strings"
	"testing"

	"LCM/internal/core/domain"
)

// TestMitgelieferteAnwendungenSindGueltig ist die Abnahme des Katalogs: Ein
// Eintrag, dessen Merkmale sich nicht zerlegen lassen, wird beim Scan
// stillschweigend übersprungen - die Anwendung würde nie gefunden, und niemand
// merkte es.
func TestMitgelieferteAnwendungenSindGueltig(t *testing.T) {
	slugs, names := map[string]bool{}, map[string]bool{}
	for _, entry := range builtinAppEntries() {
		e := entry
		if err := domain.ValidateAppEntry(&e); err != nil {
			t.Errorf("%s: %v", entry.Slug, err)
			continue
		}
		if slugs[entry.Slug] {
			t.Errorf("%s: technischer Name doppelt", entry.Slug)
		}
		if names[entry.Name] {
			t.Errorf("%s: Anzeigename doppelt", entry.Name)
		}
		slugs[entry.Slug], names[entry.Name] = true, true

		// Ein Versionsmuster ohne Versionskommando trifft nie zu.
		if entry.VersionPattern != "" && entry.VersionCommand == "" {
			t.Errorf("%s: Muster ohne Versionskommando", entry.Slug)
		}
		// Eine Quelle für die neueste Version nützt nur, wenn es eine
		// installierte zum Vergleichen gibt und der Vergleich an ist.
		if entry.LatestSource != "" {
			if entry.VersionCommand == "" {
				t.Errorf("%s: Quelle hinterlegt, aber keine installierte Version ermittelbar", entry.Slug)
			}
			if entry.Compare == domain.CompareNone {
				t.Errorf("%s: Quelle hinterlegt, aber Vergleich abgeschaltet", entry.Slug)
			}
		}
	}
}

// TestMitgelieferteAnwendungenZeigenAufDenRichtigenOrt: Merkmale, die auf
// Verzeichnisse der Paketverwaltung zeigen, wären sinnlos - was dort liegt,
// gehört einem Paket und wird beim Scan ohnehin aussortiert.
func TestMitgelieferteAnwendungenZeigenAufDenRichtigenOrt(t *testing.T) {
	for _, entry := range builtinAppEntries() {
		markers, err := domain.ParseAppMarkers(entry.Markers)
		if err != nil {
			continue // schon vom Gültigkeits-Test gemeldet
		}
		for _, m := range markers {
			if m.Kind != domain.MarkerPath {
				continue
			}
			for _, verwaltet := range []string{"/usr/bin/", "/usr/sbin/", "/usr/lib/", "/etc/"} {
				if strings.HasPrefix(m.Value, verwaltet) {
					t.Errorf("%s: %q liegt im Bereich der Paketverwaltung", entry.Slug, m.Value)
				}
			}
		}
	}
}

// TestMitgelieferteAnwendungenWerdenAktualisiert: Ein einmal ausgeliefertes
// falsches Merkmal muss sich nachziehen lassen, ohne dass jemand die
// Datenbank anfasst.
func TestMitgelieferteAnwendungenWerdenAktualisiert(t *testing.T) {
	db := newMigrationTestDB(t)

	old := domain.AppCatalogEntry{
		Slug: "adguard-home", Name: "AdGuard", Builtin: true, Enabled: false,
		Description: "alter Stand", Markers: "path /falscher/pfad", Compare: domain.CompareNone,
	}
	if err := db.Create(&old).Error; err != nil {
		t.Fatal(err)
	}
	// Abgeschaltet gehört hier gesetzt statt beim Anlegen: Die Spalte trägt
	// den Vorgabewert true, und gorm haelt bei einem Struct-Create das falsche
	// Bool für „nicht gesetzt" und nimmt die Vorgabe.
	if err := db.Model(&domain.AppCatalogEntry{ID: old.ID}).Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	if err := ensureAppEntries(db); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	var updated domain.AppCatalogEntry
	if err := db.Where("slug = ?", "adguard-home").First(&updated).Error; err != nil {
		t.Fatal(err)
	}
	if updated.ID != old.ID {
		t.Error("der Eintrag wurde ersetzt statt aktualisiert")
	}
	if !strings.Contains(updated.Markers, "/opt/AdGuardHome") {
		t.Errorf("die Merkmale wurden nicht nachgezogen: %q", updated.Markers)
	}
	// Abgeschaltet lassen, was jemand abgeschaltet hat: Das ist eine
	// Betriebsentscheidung, kein Stammdatum.
	if updated.Enabled {
		t.Error("das Seeding hat den Eintrag wieder eingeschaltet")
	}
}

// TestSelbstAngelegteAnwendungBleibt: Wer einen Eintrag unter demselben
// technischen Namen selbst angelegt hat, behält ihn.
func TestSelbstAngelegteAnwendungBleibt(t *testing.T) {
	db := newMigrationTestDB(t)

	eigen := domain.AppCatalogEntry{
		Slug: "adguard-home", Name: "Mein AdGuard", Builtin: false, Enabled: true,
		Markers: "path /srv/adguard/AdGuardHome", Compare: domain.CompareNone,
	}
	if err := db.Create(&eigen).Error; err != nil {
		t.Fatal(err)
	}
	if err := ensureAppEntries(db); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	var nachher domain.AppCatalogEntry
	if err := db.Where("slug = ?", "adguard-home").First(&nachher).Error; err != nil {
		t.Fatal(err)
	}
	if nachher.Name != "Mein AdGuard" || !strings.Contains(nachher.Markers, "/srv/adguard") {
		t.Errorf("der eigene Eintrag wurde überschrieben: %+v", nachher)
	}
}

// TestMitgelieferteAnwendungenSindZweisprachig: dieselbe Zusage wie bei den
// Regelbausteinen - der Katalog steht in der Datenbank, nicht im
// Sprachkatalog der Oberfläche.
func TestMitgelieferteAnwendungenSindZweisprachig(t *testing.T) {
	for _, entry := range builtinAppEntries() {
		if strings.TrimSpace(entry.DescriptionEN) == "" {
			t.Errorf("%s: keine englische Beschreibung", entry.Slug)
		}
		if strings.TrimSpace(entry.NameEN) == "" {
			t.Errorf("%s: kein englischer Name", entry.Slug)
		}
		// Produktnamen wie "Nextcloud" sind in beiden Sprachen gleich - hier
		// wird deshalb nur die Beschreibung auf Verschiedenheit geprüft.
		if entry.DescriptionEN == entry.Description {
			t.Errorf("%s: englische Beschreibung gleicht der deutschen", entry.Slug)
		}
	}
}
