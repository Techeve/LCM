package services

import (
	"strings"
	"testing"

	"LCM/internal/core/domain"
)

// blockProfile baut ein Profil, das einen Baustein mit gefülltem Parameter
// verwendet.
func blockProfile(variants []domain.ProfileBlockVariant, values string) *domain.PrivilegeProfile {
	return &domain.PrivilegeProfile{
		ID: 1, Slug: "web", Name: "Web",
		BlockUses: []domain.ProfileBlockUse{{
			Values: values,
			Block: &domain.ProfileBlock{
				ID: 7, Slug: "systemd-dienst", Name: "Systemd-Dienst betreiben",
				Params: "service", Variants: variants,
			},
		}},
	}
}

// TestBausteinJeDistributionAufgeloest: Dieselbe Aufgabe heißt je
// Distribution anders - der Baustein liefert die passende Variante.
func TestBausteinJeDistributionAufgeloest(t *testing.T) {
	profile := blockProfile([]domain.ProfileBlockVariant{
		{Family: "apt", SudoCommands: "/usr/bin/systemctl restart apache2"},
		{Family: "dnf", SudoCommands: "/usr/bin/systemctl restart httpd"},
	}, "")

	debian, notes := expandProfile(profile, "apt")
	if len(notes) != 0 {
		t.Fatalf("unerwartete hinweise: %v", notes)
	}
	if len(debian.SudoRules) != 1 || !strings.Contains(debian.SudoRules[0].Command, "apache2") {
		t.Fatalf("apt-variante nicht gewählt: %+v", debian.SudoRules)
	}
	rhel, _ := expandProfile(profile, "dnf")
	if len(rhel.SudoRules) != 1 || !strings.Contains(rhel.SudoRules[0].Command, "httpd") {
		t.Fatalf("dnf-variante nicht gewählt: %+v", rhel.SudoRules)
	}
}

// TestFehlendeVarianteWirdGemeldet: Eine fehlende Regel heißt fehlende
// Rechte. Still übersprungen sucht jemand stundenlang, warum der Neustart nur
// auf den Debian-Servern geht.
func TestFehlendeVarianteWirdGemeldet(t *testing.T) {
	profile := blockProfile([]domain.ProfileBlockVariant{
		{Family: "apt", SudoCommands: "/usr/bin/systemctl restart apache2"},
	}, "")

	expanded, notes := expandProfile(profile, "apk")
	if len(expanded.SudoRules) != 0 {
		t.Errorf("ohne passende variante darf keine regel entstehen: %+v", expanded.SudoRules)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "keine Variante") {
		t.Fatalf("fehlende variante wurde nicht gemeldet: %v", notes)
	}
}

// TestVarianteFuerAlleAlsRueckfall: Der Normalfall identischer Zeilen braucht
// keine fünf Kopien.
func TestVarianteFuerAlleAlsRueckfall(t *testing.T) {
	profile := blockProfile([]domain.ProfileBlockVariant{
		{Family: domain.BlockFamilyAll, SudoCommands: "/usr/bin/systemctl restart {service}"},
		{Family: "apk", SudoCommands: "/sbin/rc-service {service} restart"},
	}, "service=nginx")

	fuerAlle, _ := expandProfile(profile, "dnf")
	if len(fuerAlle.SudoRules) != 1 || !strings.Contains(fuerAlle.SudoRules[0].Command, "/usr/bin/systemctl") {
		t.Fatalf("rueckfall auf die variante fuer alle nicht genommen: %+v", fuerAlle.SudoRules)
	}
	eigen, _ := expandProfile(profile, "apk")
	if len(eigen.SudoRules) != 1 || !strings.Contains(eigen.SudoRules[0].Command, "rc-service") {
		t.Fatalf("eigene variante muss vorgehen: %+v", eigen.SudoRules)
	}
}

// TestPlatzhalterWerdenGefuellt - und der Pager-Schutz greift auch für
// Baustein-Zeilen.
func TestPlatzhalterWerdenGefuellt(t *testing.T) {
	profile := blockProfile([]domain.ProfileBlockVariant{
		{Family: domain.BlockFamilyAll, SudoCommands: "/usr/bin/systemctl status {service}"},
	}, "service=nginx")

	expanded, notes := expandProfile(profile, "apt")
	if len(notes) != 0 {
		t.Fatalf("unerwartete hinweise: %v", notes)
	}
	if got := expanded.SudoRules[0].Command; got != "/usr/bin/systemctl --no-pager status nginx" {
		t.Errorf("platzhalter oder --no-pager fehlen: %q", got)
	}
}

// TestUngefuellterPlatzhalterLandetNichtInSudoers: Ein nicht gefüllter
// Platzhalter würde eine halbfertige Regel ausrollen - die Prüfung fängt ihn
// ab und der Sync-Bericht nennt ihn.
func TestUngefuellterPlatzhalterLandetNichtInSudoers(t *testing.T) {
	profile := blockProfile([]domain.ProfileBlockVariant{
		{Family: domain.BlockFamilyAll, SudoCommands: "/usr/bin/systemctl restart {service}"},
	}, "")

	expanded, notes := expandProfile(profile, "apt")
	if len(expanded.SudoRules) != 0 {
		t.Errorf("regel mit offenem platzhalter wurde übernommen: %+v", expanded.SudoRules)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "verworfen") {
		t.Fatalf("verworfene regel wurde nicht gemeldet: %v", notes)
	}
}

// TestBausteinPruefungBeimSpeichern: Die Vorlage wird mit probeweise
// eingesetzten Werten geprüft - sonst fiele ein „/usr/bin/systemctl" ohne
// Unteraktion erst auf, wenn es auf den Servern steht.
func TestBausteinPruefungBeimSpeichern(t *testing.T) {
	_, err := checkedVariants([]domain.ProfileBlockVariant{
		{Family: domain.BlockFamilyAll, SudoCommands: "/usr/bin/systemctl"},
	}, "")
	if err == nil {
		t.Error("nacktes systemctl muss schon im baustein abgewiesen werden")
	}

	if _, err := checkedVariants([]domain.ProfileBlockVariant{
		{Family: "wolke7", SudoCommands: "/usr/bin/true"},
	}, ""); err == nil {
		t.Error("unbekannte familie muss abgewiesen werden")
	}

	if _, err := checkedVariants([]domain.ProfileBlockVariant{
		{Family: domain.BlockFamilyAll, SudoCommands: "/usr/bin/systemctl restart {service}"},
	}, "service"); err != nil {
		t.Errorf("gültige vorlage mit platzhalter abgewiesen: %v", err)
	}
}

// TestParameterwertePruefung: Werte landen in einer sudoers-Zeile - ein
// Leerzeichen erzeugte dort ein zusätzliches Argument.
func TestParameterwertePruefung(t *testing.T) {
	for _, bad := range []string{"", "nginx postgres", "nginx;sh", "ngin*", "nginx,sh"} {
		if domain.ValidBlockParamValue(bad) {
			t.Errorf("%q sollte als parameterwert unzulässig sein", bad)
		}
	}
	for _, ok := range []string{"nginx", "postgresql@14", "/srv/www"} {
		if !domain.ValidBlockParamValue(ok) {
			t.Errorf("%q sollte zulässig sein", ok)
		}
	}
}
