package services

import (
	"encoding/base64"
	"strings"
	"testing"

	"LCM/internal/core/domain"
)

func testEntry() domain.AppCatalogEntry {
	return domain.AppCatalogEntry{
		Slug: "adguard-home", Name: "AdGuard Home",
		Markers:        "path /opt/AdGuardHome/AdGuardHome\nunit AdGuardHome.service",
		VersionCommand: "{path} --version",
		VersionPattern: `v?([0-9]+\.[0-9]+\.[0-9]+)`,
		Compare:        domain.CompareSemver,
	}
}

// TestErkennungsskriptPrueftDieMerkmaleDerReiheNach: Erster Treffer gewinnt -
// sonst überschriebe ein schwaches Merkmal den Fundort eines starken.
func TestErkennungsskriptPrueftDieMerkmaleDerReiheNach(t *testing.T) {
	script := appScanScript([]domain.AppCatalogEntry{testEntry()}, "apt")
	for _, part := range []string{
		`[ -e /opt/AdGuardHome/AdGuardHome ]`,
		`systemctl cat AdGuardHome.service`,
		`if [ -z "$p" ]; then`, // jedes weitere Merkmal nur ohne Treffer
		`printf 'APP\t%s\t%s\t%s\t%s\n'`,
	} {
		if !strings.Contains(script, part) {
			t.Errorf("im Skript fehlt %q:\n%s", part, script)
		}
	}
}

// TestVersionskommandoLaeuftBegrenztUndMitPfad: Ein hängendes Kommando darf
// nicht den ganzen Scan aufhalten, und {path} muss beim Kommando ankommen -
// als Argument, nicht in den Text geklebt.
func TestVersionskommandoLaeuftBegrenztUndMitPfad(t *testing.T) {
	script := appScanScript([]domain.AppCatalogEntry{testEntry()}, "apt")
	if !strings.Contains(script, `$TO sh -c`) {
		t.Errorf("die Zeitbegrenzung wirkt nicht auf das Kommando:\n%s", script)
	}
	if !strings.Contains(script, `_ "$p"`) {
		t.Errorf("der Fundort wird nicht als Argument übergeben:\n%s", script)
	}
	if strings.Contains(script, "{path}") {
		t.Errorf("der Platzhalter wurde nicht ersetzt:\n%s", script)
	}
}

// TestPaketverwalteteAnwendungenFallenRaus: Was der Paketverwaltung gehört,
// steht im Paket-Reiter und wird dort aktualisiert. Doppelt melden hieße,
// zwei Wahrheiten über dieselbe Software zu führen.
func TestPaketverwalteteAnwendungenFallenRaus(t *testing.T) {
	for mgr, expected := range map[string]string{
		"apt":    `dpkg -S "$f"`,
		"dnf":    `rpm -qf "$f"`,
		"zypper": `rpm -qf "$f"`,
		"pacman": `pacman -Qo "$f"`,
		"apk":    `apk info --who-owns "$f"`,
	} {
		script := appScanScript([]domain.AppCatalogEntry{testEntry()}, mgr)
		if !strings.Contains(script, expected) {
			t.Errorf("%s: Paketprüfung %q fehlt", mgr, expected)
		}
	}
	// Ohne bekannte Paketverwaltung wird nichts behauptet - weder beim
	// Aussortieren noch beim generischen Fund.
	script := appScanScript([]domain.AppCatalogEntry{testEntry()}, "unbekannt")
	if strings.Contains(script, "UNKNOWN") {
		t.Errorf("ohne Paketverwaltung dürfte es keinen generischen Fund geben:\n%s", script)
	}
}

// TestKaputterEintragStopptDenScanNicht: Ein Katalogeintrag mit unbrauchbaren
// Merkmalen darf die übrigen nicht mitreißen.
func TestKaputterEintragStopptDenScanNicht(t *testing.T) {
	kaputt := domain.AppCatalogEntry{Slug: "kaputt", Name: "Kaputt", Markers: "pfad /opt/x"}
	script := appScanScript([]domain.AppCatalogEntry{kaputt, testEntry()}, "apt")
	if !strings.Contains(script, "/opt/AdGuardHome/AdGuardHome") {
		t.Error("der gültige Eintrag fehlt im Skript")
	}
	if strings.Contains(script, "kaputt") {
		t.Error("der kaputte Eintrag steht trotzdem im Skript")
	}
}

// TestScanausgabeWirdZerlegt: Das Skript meldet zeilenweise; die Version
// kommt base64-kodiert, weil ihre Ausgabe mehrzeilig sein darf.
func TestScanausgabeWirdZerlegt(t *testing.T) {
	version := base64.StdEncoding.EncodeToString([]byte("AdGuard Home, version v0.107.52\nChannel: release\n"))
	out := strings.Join([]string{
		"APP\tadguard-home\tpath\t/opt/AdGuardHome/AdGuardHome\t" + version,
		"APP\tgibtsnicht\tpath\t/opt/x\t",
		"UNKNOWN\tfoobar.service\t/etc/systemd/system/foobar.service\t/opt/foobar/foobar",
		"",
		"irgendwas anderes",
	}, "\n")

	apps, unknown := parseAppScan(out, []domain.AppCatalogEntry{testEntry()})
	if len(apps) != 1 {
		t.Fatalf("erwartet genau einen Fund (der unbekannte Slug gehört nicht dazu), war %d", len(apps))
	}
	if apps[0].Version != "0.107.52" {
		t.Errorf("Version = %q", apps[0].Version)
	}
	if apps[0].Name != "AdGuard Home" || apps[0].Path != "/opt/AdGuardHome/AdGuardHome" || apps[0].Marker != "path" {
		t.Errorf("Fund falsch zerlegt: %+v", apps[0])
	}
	if len(unknown) != 1 || unknown[0].Unit != "foobar.service" || unknown[0].ExecPath != "/opt/foobar/foobar" {
		t.Errorf("generischer Fund falsch zerlegt: %+v", unknown)
	}
}

// TestUnleserlicheVersionBleibtLeer: Eine kaputte base64-Angabe darf keine
// erfundene Version ergeben.
func TestUnleserlicheVersionBleibtLeer(t *testing.T) {
	apps, _ := parseAppScan("APP\tadguard-home\tpath\t/opt/x\tkein-base64!", []domain.AppCatalogEntry{testEntry()})
	if len(apps) != 1 || apps[0].Version != "" {
		t.Errorf("erwartet Fund ohne Version, war %+v", apps)
	}
}
