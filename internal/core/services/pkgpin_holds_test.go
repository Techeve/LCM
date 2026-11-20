package services

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"LCM/internal/core/domain"
)

// Tests zum Hold-Abgleich: Die Pin-Skripte laufen wirklich, gegen Attrappen
// der Paketverwaltungen, die ihre Holds in einer Datei fuehren.

// pinTestPaths sind die Pfade, die ein Pin-Lauf schreibt; der Test biegt sie
// in sein Verzeichnis um.
var pinTestPaths = []string{"/etc/apt/apt.conf.d", "/etc/dnf/protected.d", "/etc/pacman.d", "/etc/pacman.conf", "/var/lib/lcm-managed"}

// pinTestRedirect biegt die Pfade aus pinTestPaths nach dir um.
func pinTestRedirect(script, dir string) string {
	for _, p := range pinTestPaths {
		script = strings.ReplaceAll(script, p, dir+p)
	}
	return script
}

// holdAttrappe ist ein Pruefstand: Attrappen fuer apt-mark, dnf, zypper und
// apk, die Holds in der Datei holds fuehren, und ein lcm-helper, der den
// echten (umgebogenen) Helper ausfuehrt.
type holdAttrappe struct {
	t   *testing.T
	sh  string
	dir string
	bin string
}

func newHoldAttrappe(t *testing.T) *holdAttrappe {
	t.Helper()
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("kein sh")
	}
	dir := t.TempDir()
	a := &holdAttrappe{t: t, sh: sh, dir: dir, bin: filepath.Join(dir, "bin")}
	if err := os.MkdirAll(a.bin, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"/etc/apt/apt.conf.d", "/etc/dnf/protected.d", "/etc/pacman.d"} {
		if err := os.MkdirAll(dir+p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	a.attrappen()
	return a
}

func (a *holdAttrappe) schreib(name, body string, mode os.FileMode) {
	a.t.Helper()
	if err := os.WriteFile(filepath.Join(a.bin, name), []byte(body), mode); err != nil {
		a.t.Fatal(err)
	}
}

func (a *holdAttrappe) attrappen() {
	holds := filepath.Join(a.dir, "holds")
	a.setHolds()
	kopf := "#!/bin/sh\nH=" + holds + "\n" +
		`add() { for n in "$@"; do grep -qxF -e "$n" "$H" || echo "$n" >> "$H"; done; }` + "\n" +
		`del() { for n in "$@"; do grep -vxF -e "$n" "$H" > "$H.n"; mv "$H.n" "$H"; done; }` + "\n"
	bestand := `printf 'admin-tool-x\nbash\nnginx\nredis\n'`
	a.schreib("dpkg-query", "#!/bin/sh\n"+bestand+"\n", 0o755)
	a.schreib("rpm", "#!/bin/sh\n"+bestand+"\n", 0o755)
	a.schreib("apt-mark", kopf+`case "$1" in showhold) cat "$H" ;; hold) shift; add "$@" ;; unhold) shift; del "$@" ;; esac`+"\n", 0o755)
	// dnf4-Form der Sperrliste: name-epoch:version-release.*
	a.schreib("dnf", kopf+`while [ "$#" -gt 0 ]; do case "$1" in -q|-y) shift ;; *) break ;; esac; done
[ "$1" = versionlock ] || exit 0
case "$2" in
  list) echo '# Added by versionlock'; while read -r n; do echo "$n-0:1.0-1.el9.*"; done < "$H" ;;
  add) shift 2; add "$@" ;;
  delete) shift 2; del "$@" ;;
esac`+"\n", 0o755)
	a.schreib("zypper", kopf+`[ "$1" = --non-interactive ] && shift
case "$1" in
  locks) echo '# | Name | Type    | Repository'; echo '--+------+---------+-----------'
    i=0; while read -r n; do i=$((i+1)); echo "$i | $n | package | (any)"; done < "$H"
    echo '98 | kern* | package | (any)'; echo '99 | nginx | pattern | (any)' ;;
  addlock) shift; add "$@" ;;
  removelock) shift; del "$@" ;;
esac`+"\n", 0o755)
	a.schreib("apk", kopf+`case "$1" in
  hold) shift; [ "${1:-}" = --help ] && exit 0; add "$@" ;;
  unhold) shift; del "$@" ;;
  info) `+bestand+` ;;
esac`+"\n", 0o755)
	helper := filepath.Join(a.dir, "lcm-helper.sh")
	if err := os.WriteFile(helper, []byte(pinTestRedirect(lcmHelperScript, a.dir)), 0o700); err != nil {
		a.t.Fatal(err)
	}
	a.schreib("lcm-helper", "#!/bin/sh\nexec "+a.sh+" "+helper+` "$@"`+"\n", 0o755)
}

// setHolds setzt die Holds auf dem Pruefstand.
func (a *holdAttrappe) setHolds(names ...string) {
	a.t.Helper()
	body := ""
	for _, n := range names {
		body += n + "\n"
	}
	if err := os.WriteFile(filepath.Join(a.dir, "holds"), []byte(body), 0o644); err != nil {
		a.t.Fatal(err)
	}
}

// lauf wendet die Hold-Pins names an und liefert die Ausgabe.
func (a *holdAttrappe) lauf(restricted bool, mgr string, names ...string) string {
	a.t.Helper()
	var pins []domain.PackagePin
	for _, n := range names {
		pins = append(pins, domain.PackagePin{Name: n, Hold: true})
	}
	cmd := exec.Command(a.sh, "-c", pinTestRedirect(pkgPinScript(restricted, mgr, pins), a.dir))
	cmd.Env = []string{"PATH=" + a.bin + ":/usr/bin:/bin"}
	out, err := cmd.CombinedOutput()
	if err != nil {
		a.t.Fatalf("%s: %v\n%s", mgr, err, out)
	}
	return string(out)
}

// menge liest eine Datei als sortierte, kommagetrennte Namensliste.
func (a *holdAttrappe) menge(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return "<fehlt>"
	}
	names := strings.Fields(string(b))
	sort.Strings(names)
	return strings.Join(names, ",")
}

func (a *holdAttrappe) holds() string { return a.menge(filepath.Join(a.dir, "holds")) }
func (a *holdAttrappe) state() string { return a.menge(a.dir + pinHoldState) }

// TestPinAbgleichLaesstFremdeHoldsStehen: Ein Abgleich gibt nur frei, was LCM
// selbst gehalten hat. Ein Admin-Hold bleibt - auch wenn er auf einen Pin
// passt, und auch nachdem dieser Pin wieder entfernt wurde.
func TestPinAbgleichLaesstFremdeHoldsStehen(t *testing.T) {
	for _, restricted := range []bool{false, true} {
		for _, mgr := range []string{pkgApt, pkgDnf, pkgZypper, pkgApk} {
			name := mgr
			if restricted {
				name += "/eingeschraenkt"
			}
			t.Run(name, func(t *testing.T) { pinAbgleichSzenario(t, restricted, mgr) })
		}
	}
}

func pinAbgleichSzenario(t *testing.T, restricted bool, mgr string) {
	a := newHoldAttrappe(t)
	pruef := func(schritt, holds, state string) {
		t.Helper()
		if got := a.holds(); got != holds {
			t.Errorf("%s: Holds %q, erwartet %q", schritt, got, holds)
		}
		if got := a.state(); got != state {
			t.Errorf("%s: Zustandsdatei %q, erwartet %q", schritt, got, state)
		}
	}

	// Vor dem Update: admin-tool-x hat ein Admin gehalten, nginx die alte
	// LCM-Fassung (nginx ist gepinnt). Es gibt noch keine Zustandsdatei.
	a.setHolds("admin-tool-x", "nginx")
	a.lauf(restricted, mgr, "nginx", "bash")
	pruef("Uebergang", "admin-tool-x,bash,nginx", "bash,nginx")

	a.lauf(restricted, mgr, "bash")
	pruef("Pin nginx entfernt", "admin-tool-x,bash", "bash")

	// apk listet keine Holds: Einen Admin-Hold auf einem gepinnten Paket kann
	// LCM dort nicht erkennen - der Fall entfaellt.
	if mgr != pkgApk {
		a.setHolds("admin-tool-x", "bash", "redis")
		out := a.lauf(restricted, mgr, "bash", "redis")
		pruef("Admin haelt redis, Pin redis", "admin-tool-x,bash,redis", "bash")
		if !strings.Contains(out, "von Hand gehalten") {
			t.Errorf("der fremde Hold auf redis sollte im Protokoll stehen:\n%s", out)
		}
		a.lauf(restricted, mgr, "bash")
		pruef("Pin redis entfernt", "admin-tool-x,bash,redis", "bash")
		a.setHolds("admin-tool-x", "bash")
	}

	out := a.lauf(restricted, mgr)
	pruef("keine Pins", "admin-tool-x", "")
	if _, err := os.Stat(a.dir + pinHoldState); err != nil {
		t.Errorf("die Zustandsdatei muss auch leer bleiben, sonst gilt der naechste Lauf als Uebergang: %v", err)
	}
	if mgr != pkgApk && !strings.Contains(out, "fremde Holds bleiben unberuehrt: admin-tool-x") {
		t.Errorf("der fremde Hold sollte im Protokoll stehen:\n%s", out)
	}
}

// TestPinAbgleichUebergangGibtNichtsFrei: Beim ersten Lauf ohne Zustandsdatei
// gibt LCM nichts frei - auch keinen Hold, der zu einem inzwischen entfernten
// Pin gehoeren koennte. Er ist nicht nachweislich LCMs.
func TestPinAbgleichUebergangGibtNichtsFrei(t *testing.T) {
	a := newHoldAttrappe(t)
	a.setHolds("admin-tool-x", "redis")
	a.lauf(false, pkgApt)
	if got := a.holds(); got != "admin-tool-x,redis" {
		t.Errorf("Holds %q - der Uebergang darf nichts freigeben", got)
	}
	if got := a.state(); got != "" {
		t.Errorf("Zustandsdatei %q, erwartet leer", got)
	}
}

// TestPinAbgleichNimmtKeineOptionenAusDerZustandsdatei: Was in der
// Zustandsdatei steht, geht als Argument an apt-mark und Co. Ein Eintrag, der
// nicht wie ein Paketname beginnt, wird uebergangen statt als Option gelesen.
func TestPinAbgleichNimmtKeineOptionenAusDerZustandsdatei(t *testing.T) {
	a := newHoldAttrappe(t)
	if err := os.MkdirAll(filepath.Dir(a.dir+pinHoldState), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a.dir+pinHoldState, []byte("-o\nnginx\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.setHolds("-o", "nginx")
	a.lauf(false, pkgApt)
	if got := a.holds(); got != "-o" {
		t.Errorf("Holds %q, erwartet nur den uebergangenen Eintrag -o", got)
	}
}

// TestDnfSperrlisteLiefertNamen: pin_held macht aus den Formen von dnf4, yum
// und dnf5 Paketnamen - auch bei Namen mit Bindestrichen.
func TestDnfSperrlisteLiefertNamen(t *testing.T) {
	a := newHoldAttrappe(t)
	a.schreib("dnf", `#!/bin/sh
printf '%s\n' '# Added by versionlock' 'python3-dnf-plugin-x-0:4.3.0-5.el9.*' '0:kernel-core-5.14.0-1.el7.*' \
  '!excluded-pkg-0:1-1.*' 'Package name: dnf5-pkg' 'evr = 1.0-1.fc40' ''`+"\n", 0o755)
	start := strings.Index(pinDnfBody, "bin=dnf")
	end := strings.Index(pinDnfBody, "pin_hold()")
	cmd := exec.Command(a.sh, "-c", pinDnfBody[start:end]+"pin_held")
	cmd.Env = []string{"PATH=" + a.bin + ":/usr/bin:/bin"}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got := strings.Join(strings.Fields(string(out)), ","); got != "python3-dnf-plugin-x,kernel-core,dnf5-pkg" {
		t.Errorf("pin_held = %q", got)
	}
}
