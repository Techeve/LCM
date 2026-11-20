package services

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"LCM/internal/core/domain"
)

// localConn führt die Skripte auf der Test-Maschine aus - so laufen die
// erzeugten Shell-Zeilen gegen ein echtes Dateisystem statt gegen einen
// String-Vergleich.
type localConn struct{}

func (localConn) Run(cmd string) (string, int, error) {
	out, err := exec.Command("sh", "-c", cmd).CombinedOutput()
	if exit, ok := err.(*exec.ExitError); ok {
		return string(out), exit.ExitCode(), nil
	}
	return string(out), 0, err
}

func (c localConn) RunStdin(cmd, _ string) (string, int, error) { return c.Run(cmd) }
func (localConn) OnActivity(func())                             {}
func (localConn) KeyExchange() string                           { return "" }
func (localConn) Close() error                                  { return nil }

var rootServer = &domain.Server{ServiceUser: "root"}

// needGNUStat überspringt Tests, die `stat -c` brauchen (fehlt auf macOS).
func needGNUStat(t *testing.T) {
	t.Helper()
	if exec.Command("sh", "-c", "stat -c %a /").Run() != nil {
		t.Skip("kein GNU stat - der Test läuft auf Linux (CI)")
	}
}

// TestRechteSollVergleichtVollstaendig hält den Review-Fund fest: Geprüft
// wurden nur die Welt-Bits, ein von 0700 auf 0770 geöffnetes Verzeichnis galt
// als in Ordnung. Jetzt zählen alle Bits samt Sonderbits und die Gruppe.
func TestRechteSollVergleichtVollstaendig(t *testing.T) {
	soll := &domain.HardenedPath{Path: "/srv/daten", Mode: "700", Group: "app"}
	for _, tc := range []struct {
		name string
		is   pathState
		want string
	}{
		{"stimmt", pathState{mode: 0o700, group: "app"}, ""},
		{"gruppe geöffnet", pathState{mode: 0o770, group: "app"}, "chgrp -h 'app' '/srv/daten' && chmod 00700 '/srv/daten'"},
		{"welt geöffnet", pathState{mode: 0o705, group: "app"}, "chmod 00700"},
		{"setgid dazu", pathState{mode: 0o2700, group: "app"}, "chmod 00700"},
		{"zu eng", pathState{mode: 0o500, group: "app"}, "chmod 00700"},
		{"gruppe gewechselt", pathState{mode: 0o700, group: "users"}, "chgrp -h 'app' '/srv/daten'"},
	} {
		got := strings.Join(hardenedFix(soll, &tc.is), " && ")
		if tc.want == "" && got != "" {
			t.Errorf("%s: kein eingriff erwartet, bekam %q", tc.name, got)
		}
		if !strings.Contains(got, tc.want) {
			t.Errorf("%s: %q erwartet, bekam %q", tc.name, tc.want, got)
		}
	}
	// Ohne gespeicherten Soll-Modus bleibt die alte Mindestgarantie.
	legacy := &domain.HardenedPath{Path: "/srv/alt"}
	if got := hardenedFix(legacy, &pathState{mode: 0o750, group: "x"}); got != nil {
		t.Errorf("ohne soll-modus ist 750 in ordnung: %v", got)
	}
	if got := hardenedFix(legacy, &pathState{mode: 0o755, group: "x"}); len(got) != 1 || got[0] != "chmod 00750 '/srv/alt'" {
		t.Errorf("ohne soll-modus fallen nur die welt-bits: %v", got)
	}
}

// TestRechteSollLaesstProfilMaskeStehen: Trägt der gehärtete Pfad eine
// Profil-ACL, zeigt der Modus in den Gruppen-Bits die Maske. Die Härtung
// darf sie weder als Abweichung werten noch kappen - sonst nähme sie bei
// jedem Lauf die Profilrechte weg, die ein Profil gerade erst gegeben hat.
func TestRechteSollLaesstProfilMaskeStehen(t *testing.T) {
	soll := &domain.HardenedPath{Path: "/srv/daten", Mode: "750", Group: "app"}
	ok := &pathState{mode: 0o770, group: "app", groupEntry: "r-x", mask: "rwx"}
	if got := hardenedFix(soll, ok); got != nil {
		t.Errorf("maske einer profil-freigabe ist keine abweichung: %v", got)
	}
	// Die besitzende Gruppe bekam von Hand Schreibrecht → zurück auf r-x,
	// die Maske bleibt beim chmod unangetastet (Gruppen-Ziffer 7).
	widened := &pathState{mode: 0o770, group: "app", groupEntry: "rwx", mask: "rwx"}
	got := strings.Join(hardenedFix(soll, widened), " && ")
	if !strings.Contains(got, "chmod 00770 '/srv/daten' && setfacl -m g::r-x '/srv/daten'") {
		t.Errorf("gruppen-eintrag wird nicht zurückgenommen: %s", got)
	}
	// Welt-Bits auf einem Pfad mit ACL.
	if got := strings.Join(hardenedFix(soll, &pathState{mode: 0o775, group: "app", groupEntry: "r-x", mask: "rwx"}), " "); !strings.Contains(got, "chmod 00770") || strings.Contains(got, "setfacl") {
		t.Errorf("welt-bits auf acl-pfad falsch korrigiert: %s", got)
	}
	// Enger als das Soll ist keine Abweichung - der gespeicherte Modus kann
	// selbst schon die Maske sein, Gleichheit würde die Gruppe aufweiten.
	if got := hardenedFix(soll, &pathState{mode: 0o750, group: "app", groupEntry: "r--", mask: "r-x"}); got != nil {
		t.Errorf("engerer gruppen-eintrag darf nicht aufgeweitet werden: %v", got)
	}
}

// TestRechteSollKorrigiertImDateisystem lässt Prüfung und Korrektur gegen
// echte Dateien laufen: ein Verzeichnis, dessen Gruppe von Hand Schreibrecht
// und setgid bekam, und eine gehärtete DATEI, die wieder für alle lesbar ist.
// Dateien übersah die alte Prüfung (`[ -d ]`) vollständig.
func TestRechteSollKorrigiertImDateisystem(t *testing.T) {
	needGNUStat(t)
	dir := t.TempDir()
	data := filepath.Join(dir, "daten")
	secret := filepath.Join(dir, "geheim.key")
	if err := os.Mkdir(data, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secret, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	group := strings.TrimSpace(runShell(t, "stat -c %G "+shellQuote(data)))
	paths := []domain.HardenedPath{
		{Path: data, Mode: "750", Group: group},
		{Path: secret, Mode: "640", Group: group},
	}
	runShell(t, "chmod 2770 "+shellQuote(data)+" && chmod 644 "+shellQuote(secret))

	probe, _, _ := localConn{}.Run(hardenedProbeScript(paths))
	reported, drifts := hardenedDrift(localConn{}, rootServer, paths, probe)
	if len(reported) != 0 || len(drifts) != 2 || !strings.Contains(drifts[0], "neu abgeschottet (war 2770") || !strings.Contains(drifts[1], "neu abgeschottet (war 644") {
		t.Fatalf("eingriffe falsch gemeldet: %v", drifts)
	}
	if got := strings.TrimSpace(runShell(t, "stat -c %a "+shellQuote(data)+" "+shellQuote(secret))); got != "750\n640" {
		t.Errorf("soll nicht hergestellt (setgid muss weg sein): %q", got)
	}
	// Zweiter Lauf: nichts mehr zu tun.
	probe, _, _ = localConn{}.Run(hardenedProbeScript(paths))
	if again, fixed := hardenedDrift(localConn{}, rootServer, paths, probe); len(again)+len(fixed) != 0 {
		t.Errorf("nach der korrektur darf keine abweichung bleiben: %v %v", again, fixed)
	}
}

// TestRechteSollMitProfilACLImDateisystem: Härtung und Profil-Freigabe auf
// demselben Verzeichnis - der Normalfall („erst abschotten, dann gezielt
// freigeben"). Die Freigabe hebt die Maske, das ist keine Abweichung; erst
// die von Hand aufgeweitete besitzende Gruppe ist eine. Die Korrektur nimmt
// sie zurück, ohne die Profilrechte zu kappen.
func TestRechteSollMitProfilACLImDateisystem(t *testing.T) {
	needGNUStat(t)
	data := filepath.Join(t.TempDir(), "daten")
	if err := os.Mkdir(data, 0o750); err != nil {
		t.Fatal(err)
	}
	if exec.Command("setfacl", "-m", "g:12345:rwx", data).Run() != nil {
		t.Skip("keine ACL-Unterstützung (setfacl fehlt oder Dateisystem ohne ACL)")
	}
	group := strings.TrimSpace(runShell(t, "stat -c %G "+shellQuote(data)))
	paths := []domain.HardenedPath{{Path: data, Mode: "750", Group: group}}

	probe, _, _ := localConn{}.Run(hardenedProbeScript(paths))
	if drifts, fixed := hardenedDrift(localConn{}, rootServer, paths, probe); len(drifts)+len(fixed) != 0 {
		t.Fatalf("die maske einer profil-freigabe ist keine abweichung: %v %v (%s)", drifts, fixed, probe)
	}
	runShell(t, "setfacl -m g::rwx "+shellQuote(data))
	probe, _, _ = localConn{}.Run(hardenedProbeScript(paths))
	if _, fixed := hardenedDrift(localConn{}, rootServer, paths, probe); len(fixed) != 1 {
		t.Fatalf("aufgeweitete gruppe nicht korrigiert: %v (%s)", fixed, probe)
	}
	acl := runShell(t, "getfacl -p "+shellQuote(data))
	if !strings.Contains(acl, "\ngroup::r-x\n") || !strings.Contains(acl, "\ngroup:12345:rwx\n") {
		t.Errorf("gruppe nicht zurückgenommen oder profilrechte gekappt:\n%s", acl)
	}
}

// TestRechteSollFolgtKeinemSymlink: Liegt an einem gehärteten Pfad ein
// Symlink, ist er selbst die Abweichung. chmod folgte ihm - ein Link auf
// /etc/shadow wäre sonst „abgeschottet" oder, schlimmer, geöffnet worden.
func TestRechteSollFolgtKeinemSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "ziel")
	link := filepath.Join(dir, "gehaertet")
	if err := os.WriteFile(target, nil, 0o666); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(target, 0o666); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	paths := []domain.HardenedPath{{Path: link, Mode: "750"}}
	probe, _, _ := localConn{}.Run(hardenedProbeScript(paths))
	drifts, _ := hardenedDrift(localConn{}, rootServer, paths, probe)
	if len(drifts) != 1 || !strings.Contains(drifts[0], "ist ein Symlink - nicht angefasst") {
		t.Errorf("symlink nicht als abweichung gemeldet: %v", drifts)
	}
	// Auch das Korrektur-Skript prüft noch einmal: Zwischen Prüfung und
	// Eingriff kann der Link untergeschoben worden sein.
	out, _, _ := localConn{}.Run(hardenedFixScript(paths, map[int][]string{0: {"chmod 00750 " + shellQuote(link)}}))
	if markedResults(out, "LCM-FIX|")[0] != "link" {
		t.Errorf("korrektur folgt dem symlink: %s", out)
	}
	if info, _ := os.Stat(target); info.Mode().Perm() != 0o666 {
		t.Errorf("das ziel des symlinks wurde verändert: %o", info.Mode().Perm())
	}
}

// TestRechteSollMeldetGescheiterteKorrektur: Im eingeschränkten Modus laufen
// chmod und chgrp ohne root. Scheitern sie, darf der Bericht nicht „neu
// abgeschottet" behaupten.
func TestRechteSollMeldetGescheiterteKorrektur(t *testing.T) {
	paths := []domain.HardenedPath{{Path: "/srv/daten", Mode: "750"}}
	conn := &scriptedConn{handler: func(string, string) (string, int) {
		return "chmod: Operation not permitted\nLCM-FIX|0|fehler\n", 0
	}}
	drifts, fixed := hardenedDrift(conn, rootServer, paths, "LCM-IST|0|770|root||\n")
	if len(drifts) != 1 || len(fixed) != 0 || !strings.Contains(drifts[0], "Korrektur fehlgeschlagen") {
		t.Errorf("gescheiterte korrektur falsch gemeldet: %v", drifts)
	}
	// Ein unlesbarer Zustand wird gemeldet, nicht als „ok" gezählt.
	if got, _ := hardenedDrift(conn, rootServer, paths, "LCM-IST|0||\n"); len(got) != 1 || !strings.Contains(got[0], "nicht lesbar") {
		t.Errorf("unlesbarer zustand verschluckt: %v", got)
	}
}

// runShell führt ein Kommando aus und bricht den Test bei Fehler ab.
func runShell(t *testing.T, cmd string) string {
	t.Helper()
	out, err := exec.Command("sh", "-c", cmd).CombinedOutput()
	if err != nil {
		t.Fatalf("%s: %v\n%s", cmd, err, out)
	}
	return string(out)
}

// TestACLDriftIstEineStichprobe: Nur der Wurzelpfad wird geprüft. Den ganzen
// Baum bei jedem Health-Ping durchzugehen wäre reine Last - der Voll-Abgleich
// läuft ohnehin beim Benutzer-Sync.
func TestACLDriftIstEineStichprobe(t *testing.T) {
	script := aclDriftScript(
		[]domain.AppliedProfilePath{{ProfileID: 5, Path: "/srv/www", Mode: domain.PathModeRead}},
		map[uint]string{5: "web"},
	)
	if !strings.Contains(script, "getfacl -p '/srv/www'") {
		t.Errorf("acl wird nicht geprüft: %s", script)
	}
	if strings.Contains(script, "-R") {
		t.Errorf("die stichprobe darf nicht rekursiv sein: %s", script)
	}
	// Ohne bekanntes Profil (gelöscht) entsteht keine Zeile - sonst prüfte
	// LCM gegen einen Gruppennamen, den es nicht mehr gibt.
	if got := aclDriftScript([]domain.AppliedProfilePath{{ProfileID: 9, Path: "/srv/x"}}, nil); got != "" {
		t.Errorf("unbekanntes profil darf keine prüfung erzeugen: %s", got)
	}
}

// TestACLDriftPrueftDieRechte hält den Review-Fund fest: Geprüft wurde nur,
// ob für die Profilgruppe IRGENDEIN Eintrag existiert - `group:…:---` ging
// durch. Die Fälle laufen mit echter getfacl-Ausgabe durch das Skript.
func TestACLDriftPrueftDieRechte(t *testing.T) {
	const head = "# file: /srv/www\n# owner: root\n# group: www-data\nuser::rwx\ngroup::r-x\n"
	const tail = "mask::rwx\nother::---\ndefault:user::rwx\ndefault:group::r-x\n"
	for _, tc := range []struct {
		name, mode, acl string
		drift           bool
	}{
		{"lesen ok", domain.PathModeRead, head + "group:lcm-prof-web:r-x\n" + tail + "default:group:lcm-prof-web:r-x\ndefault:mask::r-x\n", false},
		{"rechte entzogen", domain.PathModeRead, head + "group:lcm-prof-web:---\n" + tail + "default:group:lcm-prof-web:r-x\n", true},
		{"vererbung fehlt", domain.PathModeRead, head + "group:lcm-prof-web:r-x\n" + tail, true},
		{"maske gekappt", domain.PathModeReadWrite, head + "group:lcm-prof-web:rwx\t\t#effective:r-x\nmask::r-x\nother::---\ndefault:group:lcm-prof-web:rwx\n", true},
		{"schreiben ok", domain.PathModeReadWrite, head + "group:lcm-prof-web:rwx\n" + tail + "default:group:lcm-prof-web:rwx\n", false},
		{"verweigern ok", domain.PathModeDeny, head + "group:lcm-prof-web:---\n" + tail, false},
		{"verweigern aufgeweicht", domain.PathModeDeny, head + "group:lcm-prof-web:r-x\n" + tail, true},
		{"eintrag fehlt", domain.PathModeRead, head + tail, true},
	} {
		// getfacl als Shell-Funktion, die die Fixture ausgibt.
		fake := "getfacl() { printf '%s' " + shellQuote(tc.acl) + "; }\n"
		paths := []domain.AppliedProfilePath{{ProfileID: 5, Path: t.TempDir(), Mode: tc.mode}}
		out, code, _ := localConn{}.Run(fake + aclDriftScript(paths, map[uint]string{5: "web"}))
		if code != 0 {
			t.Errorf("%s: skript endete mit %d: %s", tc.name, code, out)
		}
		if got := len(aclDrifts(out, paths)) > 0; got != tc.drift {
			t.Errorf("%s: abweichung %v erwartet, bekam %v (%s)", tc.name, tc.drift, got, out)
		}
	}
}

// TestACLDriftFolgtKeinemSymlink: getfacl löst einen Symlink auf und läse
// die ACL seines Ziels.
func TestACLDriftFolgtKeinemSymlink(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "www")
	if err := os.Symlink(t.TempDir(), link); err != nil {
		t.Fatal(err)
	}
	paths := []domain.AppliedProfilePath{{ProfileID: 5, Path: link, Mode: domain.PathModeRead}}
	out, _, _ := localConn{}.Run(aclDriftScript(paths, map[uint]string{5: "web"}))
	drifts := aclDrifts(out, paths)
	if len(drifts) != 1 || !strings.Contains(drifts[0], "Symlink") {
		t.Errorf("symlink nicht gemeldet: %v (%s)", drifts, out)
	}
}

// TestACLEinrichtungLaeuftNichtInSchleife: Scheitert die Installation, darf
// sie nicht bei jedem Health-Ping erneut versucht und jedes Mal als Eingriff
// protokolliert werden.
func TestACLEinrichtungLaeuftNichtInSchleife(t *testing.T) {
	if wait := aclRetryWait(&domain.Server{}); wait != "" {
		t.Errorf("ohne fehlversuch darf nicht gesperrt sein: %s", wait)
	}
	future := time.Now().Add(time.Hour)
	if wait := aclRetryWait(&domain.Server{ACLRetryAfter: &future}); wait == "" {
		t.Error("innerhalb der sperrfrist muss gewartet werden")
	}
	past := time.Now().Add(-time.Hour)
	if wait := aclRetryWait(&domain.Server{ACLRetryAfter: &past}); wait != "" {
		t.Errorf("nach der sperrfrist darf es weitergehen: %s", wait)
	}
}

// TestNeueEnforceTypenWerdenEntdoppelt: Fordern mehrere Gruppen dasselbe
// „vorhanden/abgeglichen", ist das kein Konflikt, sondern eine Dopplung - die
// zweite Ausführung wäre wirkungslos.
func TestNeueEnforceTypenWerdenEntdoppelt(t *testing.T) {
	for _, typ := range []string{domain.RuleTypeACLSetup, domain.RuleTypePermSync} {
		if !enforceableRuleType(typ) {
			t.Errorf("%s muss als grundsatz-regel zulässig sein", typ)
		}
		if !dedupedEnforceType(typ) {
			t.Errorf("%s muss entdoppelt werden", typ)
		}
		if conflictingEnforceType(typ) {
			t.Errorf("%s trägt keinen widersprüchlichen soll-zustand", typ)
		}
	}
	active, superseded := resolveEnforceRules([]domain.Rule{
		ruleIn(1, 3, "A", 10, domain.RuleTypePermSync, "Soll A"),
		ruleIn(2, 7, "B", 100, domain.RuleTypePermSync, "Soll B"),
	})
	if len(active) != 1 || active[0].Name != "Soll A" {
		t.Fatalf("erwartet genau eine ausführung: %+v", active)
	}
	// Kein Konflikt - also auch keine „übersprungen"-Meldung, die nur Lärm wäre.
	if len(superseded) != 0 {
		t.Errorf("dopplung darf nicht als konflikt gemeldet werden: %+v", superseded)
	}
}
