package domain_test

import (
	"errors"
	"testing"

	"LCM/internal/core/domain"
)

// TestMerkmaleWerdenGeprueft: Die Werte landen in einem Skript, das als root
// läuft. Was hier durchkommt, ist shell-sicher - und die Art muss LCM kennen,
// sonst prüft das Skript nichts.
func TestMerkmaleWerdenGeprueft(t *testing.T) {
	markers, err := domain.ParseAppMarkers("path /opt/AdGuardHome\n# Kommentar\nunit AdGuardHome.service\n\nbin minio")
	if err != nil {
		t.Fatalf("gültige Merkmale abgelehnt: %v", err)
	}
	if len(markers) != 3 || markers[0].Kind != domain.MarkerPath || markers[2].Value != "minio" {
		t.Errorf("falsch zerlegt: %+v", markers)
	}

	for _, schlecht := range []string{
		"pfad /opt/x",               // unbekannte Art
		"path",                      // ohne Wert
		"path /opt/$(id)",           // Sonderzeichen
		"unit a;rm -rf /",           // Sonderzeichen
		"path /opt/mit leerzeichen", // Wert mit Leerzeichen
	} {
		if _, err := domain.ParseAppMarkers(schlecht); err == nil {
			t.Errorf("%q wurde nicht abgewiesen", schlecht)
		}
	}
	if _, err := domain.ParseAppMarkers("\n# nur ein Kommentar\n"); !errors.Is(err, domain.ErrAppNoMarker) {
		t.Error("ohne Merkmal müsste ErrAppNoMarker kommen")
	}
}

// TestVersionWirdHerausgeschnitten: Die Ausgabe eines Versionskommandos ist
// selten nur die Version.
func TestVersionWirdHerausgeschnitten(t *testing.T) {
	if got := domain.ExtractVersion("AdGuard Home, version v0.107.52\n", `v?([0-9]+\.[0-9]+\.[0-9]+)`); got != "0.107.52" {
		t.Errorf("AdGuard: %q", got)
	}
	// Ohne Muster: die erste nichtleere Zeile.
	if got := domain.ExtractVersion("\n\n  31.0.2  \nzweite Zeile\n", ""); got != "31.0.2" {
		t.Errorf("ohne Muster: %q", got)
	}
	// Kein Treffer heißt leer - nicht etwa die ganze Ausgabe.
	if got := domain.ExtractVersion("keine version hier", `([0-9]+\.[0-9]+)`); got != "" {
		t.Errorf("Fehltreffer lieferte %q", got)
	}
	// Ein kaputtes Muster darf nichts behaupten.
	if got := domain.ExtractVersion("1.2.3", "([0-9"); got != "" {
		t.Errorf("kaputtes Muster lieferte %q", got)
	}
}

// TestVersionsvergleich: Der häufigste Fehlalarm wäre ein Zeichenvergleich -
// „1.9" ist zeichenweise größer als „1.10", der Reihenfolge nach aber älter.
func TestVersionsvergleich(t *testing.T) {
	cases := []struct {
		installiert, neueste, art string
		expected                  bool
	}{
		{"1.9.0", "1.10.0", domain.CompareSemver, true},
		{"1.10.0", "1.9.0", domain.CompareSemver, false},
		{"v0.107.52", "v0.107.52", domain.CompareSemver, false},
		{"0.107.52", "v0.107.53", domain.CompareSemver, true},
		{"24.0.7", "24.0.7.1", domain.CompareSemver, true},
		// Exakt: alles Abweichende gilt als veraltet.
		{"2025-04a", "2025-05", domain.CompareExact, true},
		{"2025-05", "2025-05", domain.CompareExact, false},
		// Nie bewerten.
		{"1.0", "2.0", domain.CompareNone, false},
		// Fehlende Angaben behaupten nichts.
		{"", "2.0", domain.CompareSemver, false},
		{"1.0", "", domain.CompareSemver, false},
		// Was sich nicht in Zahlen zerlegen lässt, wird nicht geraten.
		{"stable", "aktuell", domain.CompareSemver, false},
	}
	for _, f := range cases {
		if got := domain.AppUpdateAvailable(f.installiert, f.neueste, f.art); got != f.expected {
			t.Errorf("%s → %s (%s): %v, erwartet %v", f.installiert, f.neueste, f.art, got, f.expected)
		}
	}
}

// TestKatalogeintragWirdGeprueft: Was hier durchrutscht, fällt erst auf dem
// Zielsystem auf - und zwar als Anwendung, die nie gefunden wird.
func TestKatalogeintragWirdGeprueft(t *testing.T) {
	gut := domain.AppCatalogEntry{Slug: "adguard-home", Name: "AdGuard Home", Markers: "path /opt/AdGuardHome"}
	if err := domain.ValidateAppEntry(&gut); err != nil {
		t.Fatalf("gültiger Eintrag abgelehnt: %v", err)
	}
	if gut.Compare != domain.CompareSemver {
		t.Errorf("Vorgabe für den Vergleich fehlt: %q", gut.Compare)
	}

	cases := map[string]domain.AppCatalogEntry{
		"leerer slug":        {Slug: "", Name: "X", Markers: "path /opt/x"},
		"slug mit Punkt":     {Slug: "ad.guard", Name: "X", Markers: "path /opt/x"},
		"ohne Name":          {Slug: "x", Name: "", Markers: "path /opt/x"},
		"ohne Merkmal":       {Slug: "x", Name: "X", Markers: ""},
		"falscher Vergleich": {Slug: "x", Name: "X", Markers: "path /opt/x", Compare: "irgendwas"},
		"kaputtes Muster":    {Slug: "x", Name: "X", Markers: "path /opt/x", VersionPattern: "([0-9"},
		"Quelle ohne Art":    {Slug: "x", Name: "X", Markers: "path /opt/x", LatestSource: "irgendwas"},
		"Quelle http":        {Slug: "x", Name: "X", Markers: "path /opt/x", LatestSource: "url:http://unsicher.example"},
		"github ohne repo":   {Slug: "x", Name: "X", Markers: "path /opt/x", LatestSource: "github:nurowner"},
	}
	for name, entry := range cases {
		e := entry
		if err := domain.ValidateAppEntry(&e); err == nil {
			t.Errorf("%s: wurde nicht abgewiesen", name)
		}
	}
}
