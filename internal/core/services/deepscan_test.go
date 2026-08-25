package services

import (
	"strings"
	"testing"

	"LCM/internal/core/domain"
)

func TestParseDeepScanTools(t *testing.T) {
	nr, ly, measured := parseDeepScanTools("HAVE needrestart\nMISS lynis\n")
	if !nr || ly || !measured {
		t.Fatalf("erwartet needrestart=true lynis=false measured=true, bekam %v %v %v", nr, ly, measured)
	}
	nr, ly, measured = parseDeepScanTools("MISS needrestart\nHAVE lynis")
	if nr || !ly || !measured {
		t.Fatalf("erwartet needrestart=false lynis=true measured=true, bekam %v %v %v", nr, ly, measured)
	}
	// Leere Ausgabe heißt „die Messung ist fehlgeschlagen", NICHT „beide
	// Werkzeuge fehlen": genau diese Gleichsetzung hat den Bericht im
	// eingeschränkten Modus auf „null Befunde" gestellt, obwohl nichts
	// geprüft wurde (B16).
	for _, out := range []string{"", "\n", "sudo: a password is required\n"} {
		if _, _, measured := parseDeepScanTools(out); measured {
			t.Errorf("Ausgabe %q darf nicht als belastbare Messung gelten", out)
		}
	}
}

// TestDeepScanHelperKommando: im eingeschränkten Modus läuft der Deep Scan
// über den validierenden Helper - sonst laufen die Schritte als
// unprivilegierter Dienstbenutzer ins Leere. Der Helper muss das
// Unterkommando kennen, und die Prüfskripte dürfen dort nicht als zweite,
// abweichende Kopie liegen (sonst meldet der eingeschränkte Modus etwas
// anderes als der Voll-Modus).
func TestDeepScanHelperKommando(t *testing.T) {
	if !strings.Contains(lcmHelperScript, "deep-scan)") {
		t.Error("der Helper kennt das Unterkommando deep-scan nicht")
	}
	for _, part := range []string{"tools)", "needrestart)", "lynis)", "curated)"} {
		if !strings.Contains(lcmHelperScript, part) {
			t.Errorf("dem Helper fehlt der deep-scan-Teil %q", part)
		}
	}
	// Die Skripte selbst stammen aus deepscan.go - Stichproben aus jedem Teil.
	for _, marker := range []string{
		"HAVE $t",                     // Werkzeug-Erhebung
		"needrestart -b",              // Kernel-/Dienst-Neustartlücke
		"lynis audit system",          // Härtungs-Audit
		"LCMFIND|critical|Konto ohne", // kuratierte Prüfungen
	} {
		if !strings.Contains(lcmHelperScript, marker) {
			t.Errorf("das Prüfskript wurde nicht in den Helper eingesetzt: %q fehlt", marker)
		}
	}
	if strings.Contains(lcmHelperScript, "@@DEEPSCAN_") {
		t.Error("ein Platzhalter wurde nicht ersetzt - der Helper wäre auf dem Ziel kaputt")
	}
}

func TestParseNeedrestart(t *testing.T) {
	out := `NEEDRESTART-VER: 3.6
NEEDRESTART-KCUR: 6.1.0-13-amd64
NEEDRESTART-KEXP: 6.1.0-18-amd64
NEEDRESTART-KSTA: 3
NEEDRESTART-SVC: cron.service
NEEDRESTART-SVC: ssh.service
NEEDRESTART-SVC: cron.service`
	r := parseNeedrestart(out)
	if r.KSTA != 3 || !r.kernelRebootPending() {
		t.Fatalf("KSTA falsch: %+v", r)
	}
	if r.KCur != "6.1.0-13-amd64" || r.KExp != "6.1.0-18-amd64" {
		t.Fatalf("Kernel-Versionen falsch: %+v", r)
	}
	if len(r.Services) != 2 { // dedupliziert
		t.Fatalf("Services falsch (dedupe?): %#v", r.Services)
	}
	// KSTA 1 = kein Reboot nötig.
	if parseNeedrestart("NEEDRESTART-KSTA: 1").kernelRebootPending() {
		t.Fatal("KSTA 1 darf keinen Reboot melden")
	}
	// KSTA 2 = ABI-kompatibel ausstehend → Reboot nötig.
	if !parseNeedrestart("NEEDRESTART-KSTA: 2").kernelRebootPending() {
		t.Fatal("KSTA 2 sollte Reboot melden")
	}
}

func TestParseLynisReport(t *testing.T) {
	out := `# lynis report
hardening_index=67
warning[]=SSH-7408|Consider hardening SSH configuration|-|-|
suggestion[]=BOOT-5122|Set a password on GRUB bootloader|-|-|
suggestion[]=KRNL-6000|One or more sysctl values differ|-|-|`
	r := parseLynisReport(out)
	if r.HardeningIndex == nil || *r.HardeningIndex != 67 {
		t.Fatalf("hardening_index falsch: %v", r.HardeningIndex)
	}
	if len(r.Warnings) != 1 || r.Warnings[0] != "Consider hardening SSH configuration" {
		t.Fatalf("Warnungen falsch: %#v", r.Warnings)
	}
	if len(r.Suggestions) != 2 {
		t.Fatalf("Empfehlungen falsch: %#v", r.Suggestions)
	}
}

func TestParseCuratedChecks(t *testing.T) {
	out := "noise\nLCMFIND|critical|Konto ohne Passwort: test\nLCMFIND|warning|SSH: Root-Login erlaubt\nLCMFIND|bogus|Titel\n"
	f := parseCuratedChecks(out)
	if len(f) != 3 {
		t.Fatalf("erwartet 3 Befunde, bekam %d", len(f))
	}
	if f[0].Severity != domain.DeepScanCritical || f[0].Category != domain.DeepScanMisconfig {
		t.Fatalf("erster Befund falsch: %+v", f[0])
	}
	if f[2].Severity != domain.DeepScanInfo { // unbekannte Schwere → info
		t.Fatalf("unbekannte Schwere sollte info werden: %+v", f[2])
	}
}

func TestIsKernelPackage(t *testing.T) {
	for _, name := range []string{"linux-image-6.1.0-13-amd64", "linux", "kernel-default", "kernel"} {
		if !isKernelPackage(name) {
			t.Errorf("%q sollte als Kernel-Paket gelten", name)
		}
	}
	for _, name := range []string{"nginx", "openssl", "bash"} {
		if isKernelPackage(name) {
			t.Errorf("%q sollte NICHT als Kernel-Paket gelten", name)
		}
	}
}

func TestPkgInstallScript(t *testing.T) {
	cases := map[string]string{
		pkgApt:    "apt-get install -y",
		pkgDnf:    "dnf install -y",
		pkgYum:    "yum install -y",
		pkgZypper: "zypper --non-interactive install",
		pkgPacman: "pacman -S --noconfirm",
		pkgApk:    "apk add",
	}
	for mgr, want := range cases {
		s := pkgInstallScript(mgr, []string{"needrestart", "lynis"})
		if !strings.Contains(s, want) {
			t.Errorf("%s: Skript enthält %q nicht:\n%s", mgr, want, s)
		}
		if !strings.Contains(s, "needrestart") || !strings.Contains(s, "lynis") {
			t.Errorf("%s: Paketnamen fehlen", mgr)
		}
	}
}

// TestHelperVersionierung deckt B17 ab: Der Helper wird beim Einschränken
// geschrieben und danach nie erneuert - ein LCM-Update bringt Korrekturen mit,
// die auf dem Server nicht ankommen. Damit das nicht unbemerkt bleibt, trägt
// er einen Stand, der sich mit dem Skript automatisch ändert.
func TestHelperVersionierung(t *testing.T) {
	if lcmHelperVersion == "" || len(lcmHelperVersion) != 12 {
		t.Fatalf("unbrauchbarer Helper-Stand: %q", lcmHelperVersion)
	}
	if !strings.Contains(lcmHelperScript, "LCM-HELPER-VERSION: "+lcmHelperVersion) {
		t.Error("der Stand steht nicht im ausgelieferten Skript")
	}
	if !strings.Contains(lcmHelperScript, "version)") {
		t.Error("dem Helper fehlt das Unterkommando version")
	}
	// Der Stand muss sich mit dem Inhalt bewegen - eine von Hand gepflegte
	// Nummer würde beim Ändern vergessen, und dann hielte LCM einen veralteten
	// Helper für aktuell.
	if helperVersion() != lcmHelperVersion {
		t.Error("die Ableitung ist nicht deterministisch")
	}
	sameInput := helperVersionInput()
	if got := shortHash(sameInput + "x"); got == lcmHelperVersion {
		t.Error("eine Inhaltsänderung bewegt den Stand nicht")
	}

	// Auslesen der Server-Antwort.
	if got := parseHelperVersion("LCM-HELPER-VERSION: abc123def456\n"); got != "abc123def456" {
		t.Errorf("Stand nicht gelesen: %q", got)
	}
	for _, out := range []string{"", "lcm-helper: unbekanntes kommando: version\n", "sudo: a password is required\n"} {
		if got := parseHelperVersion(out); got != "" {
			t.Errorf("Ausgabe %q darf keinen Stand ergeben, bekam %q", out, got)
		}
	}
}

// TestAmpelWeistVeraltetenHelperAus: der Abgleich gehört sichtbar in die
// Ampel - sonst gilt ein Server als gepflegt, auf dem alle privilegierten
// Aktionen über veralteten Code laufen.
func TestAmpelWeistVeraltetenHelperAus(t *testing.T) {
	old := domain.CurrentHelperVersion
	domain.CurrentHelperVersion = "aktuell00cafe"
	defer func() { domain.CurrentHelperVersion = old }()

	cases := []struct {
		name       string
		restricted bool
		version    string
		want       bool
	}{
		{"eingeschränkt, veraltet", true, "alt000000000", true},
		{"eingeschränkt, gar kein Stand", true, "", true},
		{"eingeschränkt, aktuell", true, "aktuell00cafe", false},
		{"Voll-Modus", false, "", false},
	}
	for _, c := range cases {
		s := &domain.Server{RestrictedSudo: c.restricted, HelperVersion: c.version}
		if got := s.HelperOutdated(); got != c.want {
			t.Errorf("%s: HelperOutdated()=%v, erwartet %v", c.name, got, c.want)
		}
	}
}
