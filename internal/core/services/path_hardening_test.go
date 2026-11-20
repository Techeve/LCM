package services

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"LCM/internal/core/domain"
)

// TestHaertungSichertDenVorzustand: Ohne den aufgezeichneten Vorzustand gäbe
// es keine Rücknahme - und eine Härtung, die man nicht zurücknehmen kann,
// traut sich niemand anzuwenden.
func TestHaertungSichertDenVorzustand(t *testing.T) {
	script := hardenScript("/srv/kundendaten", "", "")
	if !strings.Contains(script, "LCM-VORHER:") {
		t.Errorf("vorzustand wird nicht ausgegeben: %s", script)
	}
	if strings.Index(script, "LCM-VORHER:") > strings.Index(script, "chmod o-rwx") {
		t.Errorf("der vorzustand muss VOR der änderung gelesen werden: %s", script)
	}
	mode, group := parseHardenState("LCM-VORHER: 755 root\nLCM-NACHHER: 750 www-data\n", "LCM-VORHER:")
	if mode != "755" || group != "root" {
		t.Errorf("vorzustand falsch gelesen: %q %q", mode, group)
	}
	mode, group = parseHardenState("LCM-VORHER: 755 root\nLCM-NACHHER: 750 www-data\n", "LCM-NACHHER:")
	if mode != "750" || group != "www-data" {
		t.Errorf("nachzustand falsch gelesen: %q %q", mode, group)
	}
}

// TestHaertungSetztGruppeVorDemEntzug: /var/www steht typischerweise auf 755
// root:root, und der nginx-Worker liest als www-data über genau das
// Welt-Recht. Fiele es vor dem Gruppenwechsel, wäre die Seite kurzzeitig -
// oder bei einem Abbruch dauerhaft - tot.
func TestHaertungSetztGruppeVorDemEntzug(t *testing.T) {
	script := hardenScript("/var/www", "www-data", "")
	chgrp := strings.Index(script, "chgrp -h www-data")
	chmod := strings.Index(script, "chmod o-rwx")
	if chgrp < 0 || chmod < 0 {
		t.Fatalf("schritte fehlen: %s", script)
	}
	if chgrp > chmod {
		t.Errorf("die gruppe muss vor dem entzug gesetzt werden: %s", script)
	}
}

// TestHaertungNimmtSichBeiDienstausfallZurueck: Eine Härtung, die einen
// Dienst abschießt, darf nicht bestehen bleiben.
func TestHaertungNimmtSichBeiDienstausfallZurueck(t *testing.T) {
	script := hardenScript("/var/www", "www-data", "nginx")
	if !strings.Contains(script, "systemctl is-active --quiet nginx") {
		t.Errorf("wirkungsprobe fehlt: %s", script)
	}
	if !strings.Contains(script, `chgrp -h "$LCM_PREV_GID" '/var/www'; chmod "$LCM_PREV_MODE" '/var/www'`) {
		t.Errorf("rücknahme bei fehlschlag fehlt: %s", script)
	}
	// Ohne Unit gibt es keine Probe - dann darf auch nichts zurückgenommen
	// werden.
	if strings.Contains(hardenScript("/srv/daten", "", ""), "is-active") {
		t.Error("ohne unit darf keine dienstprobe entstehen")
	}
}

// TestHaertungFasstVerzeichnisseUndDateienAn - aber keine Symlinks: chmod
// folgt einem Link, eine Härtung von /srv/link träfe sein Ziel. Läuft gegen
// echte Pfade.
func TestHaertungFasstVerzeichnisseUndDateienAn(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "geheim.key")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(file, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{dir, file} {
		if out, code, _ := (localConn{}).Run(hardenScript(path, "", "")); code != 0 {
			t.Errorf("%s nicht abgeschottet (exit %d): %s", path, code, out)
		}
		if info, _ := os.Stat(path); info.Mode().Perm()&0o007 != 0 {
			t.Errorf("%s ist weiter für alle offen: %o", path, info.Mode().Perm())
		}
	}

	target := filepath.Join(dir, "ziel")
	link := filepath.Join(dir, "verweis")
	if err := os.WriteFile(target, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(target, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if out, code, _ := (localConn{}).Run(hardenScript(link, "", "")); code == 0 || !strings.Contains(out, "symlink") {
		t.Errorf("symlink muss abgelehnt werden (exit %d): %s", code, out)
	}
	if info, _ := os.Stat(target); info.Mode().Perm() != 0o644 {
		t.Errorf("das ziel des symlinks wurde verändert: %o", info.Mode().Perm())
	}
	// Nicht rekursiv: Fehlt am Kopf das Durchgangsrecht, ist der ganze Baum
	// unerreichbar - ein chmod -R fasste jede Datei an und wäre kaum
	// zurückzunehmen.
	if strings.Contains(hardenScript(dir, "", ""), "chmod -R") {
		t.Error("die härtung darf nicht rekursiv sein")
	}
}

// TestRuecknahmeStelltVorzustandHer.
func TestRuecknahmeStelltVorzustandHer(t *testing.T) {
	script := restoreScript(&domain.HardenedPath{
		Path: "/var/www", PrevMode: "755", PrevGroup: "root",
	})
	if !strings.Contains(script, "chgrp -h 'root' '/var/www'") || !strings.Contains(script, "chmod 755 '/var/www'") {
		t.Errorf("vorzustand wird nicht wiederhergestellt: %s", script)
	}
}

// TestDienstausfallStelltGruppeUndModusHer läuft echt: Fällt die Dienstprobe
// durch, nahm die Härtung nur `chmod o+rX` zurück. Die neue Gruppe blieb
// stehen, und ein Pfad mit 750 stand danach für alle lesbar da.
func TestDienstausfallStelltGruppeUndModusHer(t *testing.T) {
	needGNUStat(t)
	dir := filepath.Join(t.TempDir(), "daten")
	if err := os.Mkdir(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, os.ModeSetgid|0o750); err != nil {
		t.Fatal(err)
	}
	before := pathStat(t, dir)
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "systemctl"), []byte("#!/bin/sh\nexit 3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", "-c", hardenScript(dir, otherGroup(t, before), "nginx"))
	cmd.Env = []string{"PATH=" + bin + ":/usr/bin:/bin"}
	out, err := cmd.CombinedOutput()
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 2 {
		t.Fatalf("dienstprobe sollte mit exit 2 zurücknehmen: %v\n%s", err, out)
	}
	if after := pathStat(t, dir); after != before {
		t.Errorf("vorzustand %q nicht wiederhergestellt, jetzt %q", before, after)
	}
}

// pathStat liefert Modus und Gruppen-ID wie stat -c '%a %g'.
func pathStat(t *testing.T, path string) string {
	t.Helper()
	out, err := exec.Command("stat", "-c", "%a %g", path).Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

// otherGroup sucht eine Gruppe, in die der Test den Pfad umhängen darf. Als
// root jede, sonst eine weitere eigene; ohne sie bleibt die Gruppe gleich
// (dann prüft der Test nur den Modus).
func otherGroup(t *testing.T, state string) string {
	t.Helper()
	_, gid, _ := strings.Cut(state, " ")
	if os.Geteuid() == 0 {
		if gid == "1" {
			return "2"
		}
		return "1"
	}
	groups, _ := exec.Command("id", "-G").Output()
	for _, g := range strings.Fields(string(groups)) {
		if g != gid {
			return g
		}
	}
	return gid
}

// TestRuecknahmeFolgtKeinemSymlink: Der Vorzustand ist meist 755. Folgte die
// Rücknahme einem untergeschobenen Link auf /etc/shadow, wäre die Datei
// danach für alle lesbar.
func TestRuecknahmeFolgtKeinemSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "shadow")
	link := filepath.Join(dir, "daten")
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	out, code, _ := (localConn{}).Run(restoreScript(&domain.HardenedPath{Path: link, PrevMode: "755"}))
	if code == 0 || !strings.Contains(out, "symlink") {
		t.Errorf("rücknahme über einen symlink muss scheitern (exit %d): %s", code, out)
	}
	if info, _ := os.Stat(target); info.Mode().Perm() != 0o600 {
		t.Errorf("das ziel des symlinks wurde geöffnet: %o", info.Mode().Perm())
	}
}

// TestHaertungLehntSystempfadeAb: An /etc, /usr und Co. hängt der Betrieb -
// dort den Welt-Zugriff zu entfernen legte das System lahm.
func TestHaertungLehntSystempfadeAb(t *testing.T) {
	for _, path := range []string{"/", "/etc", "/usr", "/bin", "/var/lib/lcm", "srv/relativ", "/srv/*"} {
		if err := domain.ValidateHardenTarget(path, "", ""); err == nil {
			t.Errorf("%q muss abgelehnt werden", path)
		}
	}
	if err := domain.ValidateHardenTarget("/srv/kundendaten", "www-data", "nginx"); err != nil {
		t.Errorf("gültiges ziel abgelehnt: %v", err)
	}
	// Gruppen- und Dienstname landen in einem als root laufenden Skript.
	for _, group := range []string{"www data", "www;sh", "WWW"} {
		if err := domain.ValidateHardenTarget("/srv/daten", group, ""); err == nil {
			t.Errorf("gruppenname %q muss abgelehnt werden", group)
		}
	}
	for _, unit := range []string{"nginx; sh", "ngin*", "/bin/sh"} {
		if err := domain.ValidateHardenTarget("/srv/daten", "", unit); err == nil {
			t.Errorf("dienstname %q muss abgelehnt werden", unit)
		}
	}
}

// TestDienstprobeNurMitSystemd hält den zweiten Fund der
// Distributions-Prüfung fest: Auf Alpine und anderen OpenRC-Systemen gibt es
// kein systemctl. Ein „command not found" sähe aus wie ein toter Dienst - die
// Härtung wäre dort sofort und immer zurückgenommen worden.
func TestDienstprobeNurMitSystemd(t *testing.T) {
	script := hardenScript("/var/www", "www-data", "nginx")
	if !strings.Contains(script, "command -v systemctl") {
		t.Errorf("die probe läuft ohne prüfung auf systemctl: %s", script)
	}
	if !strings.Contains(script, "kein systemd") {
		t.Errorf("das überspringen wird nicht gemeldet: %s", script)
	}
	// Die Prüfung muss VOR dem is-active stehen, sonst greift sie nicht.
	if strings.Index(script, "command -v systemctl") > strings.Index(script, "is-active") {
		t.Errorf("systemctl-prüfung steht nach der abfrage: %s", script)
	}
}

// TestVorschlaegeSindEinePositivliste: Unter /etc liegt auch, was für alle
// lesbar bleiben MUSS (profile.d, alternatives, ssl/certs, Init-Skripte). Eine
// Ausschlussliste wäre nie vollständig, und der erste vergessene Eintrag legt
// ein System lahm. Vorgeschlagen wird deshalb nur, wovon LCM weiß, dass der
// Dienst seine Konfiguration als root oder unter eigener Kennung liest.
func TestVorschlaegeSindEinePositivliste(t *testing.T) {
	script := hardenSuggestScript()
	for _, safe := range []string{"/etc/profile.d", "/etc/alternatives", "/etc/ssl", "/etc/systemd", "/etc/init.d"} {
		if strings.Contains(script, safe+" ") || strings.Contains(script, safe+";") {
			t.Errorf("%s darf nicht vorgeschlagen werden: %s", safe, script)
		}
	}
	for _, want := range []string{"/etc/nginx", "/etc/postfix", "/srv", "/var/www"} {
		if !strings.Contains(script, want) {
			t.Errorf("%s fehlt in der suche: %s", want, script)
		}
	}
	// Nur Verzeichnisse mit Welt-Rechten sind Kandidaten (oktal, siehe
	// TestDriftRechnetOktal).
	if !strings.Contains(script, "% 8") {
		t.Errorf("die welt-bits werden nicht oktal geprüft: %s", script)
	}
	// Symlinks bleiben außen vor.
	if !strings.Contains(script, `[ -L "$d" ] && continue`) {
		t.Errorf("symlinks werden nicht übersprungen: %s", script)
	}
}

// TestVorschlagNenntDieGruppeDerInhalte: /var/www gehört meist root:root, die
// Dateien darin aber www-data - und genau diese Gruppe muss das Verzeichnis
// bekommen, bevor das Welt-Recht fällt. Ohne sie verliert der Dienst den
// Zugriff.
func TestVorschlagNenntDieGruppeDerInhalte(t *testing.T) {
	if !strings.Contains(hardenSuggestScript(), "printf '%g") {
		t.Error("die gruppe der inhalte wird nicht ermittelt")
	}
	got := parseHardenSuggestions("egal\nLCM-KAND|daten|/var/www|755|root|www-data\nLCM-KAND|konfig|/etc/nginx|755|root|root\n")
	if len(got) != 2 {
		t.Fatalf("erwartet zwei kandidaten, bekam %+v", got)
	}
	if got[0].Path != "/var/www" || got[0].ServiceGroup != "www-data" || got[0].Kind != "daten" {
		t.Errorf("kandidat falsch gelesen: %+v", got[0])
	}
	if got[1].Kind != "konfig" {
		t.Errorf("art falsch gelesen: %+v", got[1])
	}
	// Unvollständige Zeilen werden verworfen statt halb übernommen.
	if len(parseHardenSuggestions("LCM-KAND|daten|/srv\n")) != 0 {
		t.Error("unvollständige zeile wurde übernommen")
	}
}
