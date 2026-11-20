package services

import (
	"strings"
	"testing"

	"LCM/internal/core/domain"
)

func kernelPins() []domain.PackagePin {
	return []domain.PackagePin{
		{Name: "linux-image-*", NoRemove: true},
		{Name: "linux-headers-*", NoRemove: true},
	}
}

// TestPinNamesUebergehtAlteEintraege: Ein gespeicherter Name, den die Prüfung
// heute ablehnt, darf die übrigen Pins nicht mitreißen.
func TestPinNamesUebergehtAlteEintraege(t *testing.T) {
	got := pinNames([]domain.PackagePin{{Name: "-o", NoRemove: true}, {Name: "nginx", NoRemove: true}}, false)
	if len(got) != 1 || got[0] != "nginx" {
		t.Errorf("pinNames = %v", got)
	}
}

// TestPinNamesTrenntWirkungen: NoRemove und Hold sind getrennte Mengen -
// ein reiner Schutz-Pin darf NIE als Hold auf dem Ziel landen (das wuerde
// Sicherheitsupdates blockieren).
func TestPinNamesTrenntWirkungen(t *testing.T) {
	pins := []domain.PackagePin{
		{Name: "linux-image-*", NoRemove: true},
		{Name: "nginx", NoRemove: true, Hold: true},
		{Name: "redis", Hold: true},
		{Name: "linux-image-*", NoRemove: true}, // Dublette global/server
	}
	noRemove := pinNames(pins, false)
	hold := pinNames(pins, true)
	if strings.Join(noRemove, ",") != "linux-image-*,nginx" {
		t.Errorf("NoRemove-Menge falsch: %v", noRemove)
	}
	if strings.Join(hold, ",") != "nginx,redis" {
		t.Errorf("Hold-Menge falsch: %v", hold)
	}
}

// TestPinScriptProPaketverwaltung prueft, dass je Paketverwaltung der
// richtige Mechanismus benutzt wird - und dass keine auf den apt-Zweig
// zurueckfaellt (der stille Fallback aus BUG-012).
func TestPinScriptProPaketverwaltung(t *testing.T) {
	cases := []struct{ mgr, want string }{
		{pkgApt, "APT::NeverAutoRemove"},
		{pkgDnf, "/etc/dnf/protected.d"},
		{pkgYum, "/etc/dnf/protected.d"},
		{pkgZypper, "removelock"},
		{pkgPacman, "HoldPkg"},
		{pkgApk, "apk kennt keinen Autoremove"},
	}
	for _, c := range cases {
		got := pkgPinScript(false, c.mgr, kernelPins())
		if !strings.Contains(got, c.want) {
			t.Errorf("pkgPinScript(%q) enthaelt %q nicht:\n%s", c.mgr, c.want, got)
		}
		if c.mgr != pkgApt && strings.Contains(got, "apt-get") {
			t.Errorf("pkgPinScript(%q) faellt faelschlich auf apt zurueck:\n%s", c.mgr, got)
		}
	}
}

// TestAptPinScriptHold: Ein reiner Hold-Pin landet nur in $HOLD, und der
// Abgleich laeuft ueber die Zustandsdatei.
func TestAptPinScriptHold(t *testing.T) {
	s := pkgPinScript(false, pkgApt, []domain.PackagePin{{Name: "nginx", Hold: true}})
	if !strings.Contains(s, "state="+pinHoldState) || !strings.Contains(s, "apt-mark hold") {
		t.Errorf("apt-Pin-Skript sollte Holds ueber die Zustandsdatei abgleichen:\n%s", s)
	}
	// Ein reiner Hold-Pin hat keine Schutz-Muster: $NOREMOVE bleibt leer,
	// und der Rumpf schreibt dann keine NeverAutoRemove-Datei.
	if !strings.Contains(s, "NOREMOVE=''") || !strings.Contains(s, "HOLD='nginx'") {
		t.Errorf("Hold-Pin gehoert nur in $HOLD:\n%s", s)
	}
	if !strings.Contains(s, "rm -f "+aptPinFile) {
		t.Errorf("ohne Schutz-Pins sollte die Schutzdatei entfernt werden:\n%s", s)
	}
}

// TestPinScriptGibtNichtAlleHoldsFrei: Kein Pin-Skript raeumt mehr pauschal
// alle Holds ab - das traf auch die, die ein Admin gesetzt hat.
func TestPinScriptGibtNichtAlleHoldsFrei(t *testing.T) {
	pauschal := []string{"unhold $(", "versionlock clear", "removelock '*'"}
	for _, restricted := range []bool{false, true} {
		for _, mgr := range []string{pkgApt, pkgDnf, pkgYum, pkgZypper, pkgApk} {
			s := pkgPinScript(restricted, mgr, []domain.PackagePin{{Name: "nginx", Hold: true}})
			for _, bad := range pauschal {
				if strings.Contains(s, bad) {
					t.Errorf("%s (eingeschraenkt=%v) gibt pauschal frei (%q):\n%s", mgr, restricted, bad, s)
				}
			}
		}
	}
	for _, body := range []string{pinAptBody, pinDnfBody, pinZypperBody, pinApkBody} {
		for _, bad := range pauschal {
			if strings.Contains(body, bad) {
				t.Errorf("Rumpf gibt pauschal frei (%q):\n%s", bad, body)
			}
		}
	}
}

// TestPinScriptEingeschraenktPrueftDenHelper: Ein veralteter Helper traegt
// noch den Rumpf, der alle Holds freigibt. Das Skript bricht dann ab, statt
// ihn aufzurufen.
func TestPinScriptEingeschraenktPrueftDenHelper(t *testing.T) {
	for _, mgr := range []string{pkgApt, pkgDnf, pkgZypper, pkgApk, pkgPacman} {
		s := pkgPinScript(true, mgr, []domain.PackagePin{{Name: "nginx", Hold: true}})
		check := strings.Index(s, lcmHelperVersion)
		call := strings.Index(s, "lcm-helper package-pins")
		if check < 0 || call < check {
			t.Errorf("%s: Versionspruefung fehlt vor dem Helper-Aufruf:\n%s", mgr, s)
		}
	}
}

// TestAutoremoveMitPins: apt/dnf verlassen sich auf ihre Schutzdatei, zypper
// und pacman muessen die Kandidatenliste selbst kuerzen - sonst raeumte der
// Lauf dort weiterhin gepinnte Pakete weg.
func TestAutoremoveMitPins(t *testing.T) {
	pins := kernelPins()
	if got := pkgAutoremoveScriptWithPins(pkgApt, pins); got != pkgAutoremoveScript(pkgApt) {
		t.Errorf("apt-Autoremove sollte unveraendert bleiben (Schutzdatei greift): %q", got)
	}
	if got := pkgAutoremoveScriptWithPins(pkgDnf, pins); got != pkgAutoremoveScript(pkgDnf) {
		t.Errorf("dnf-Autoremove sollte unveraendert bleiben (protected.d greift): %q", got)
	}
	for _, mgr := range []string{pkgZypper, pkgPacman} {
		got := pkgAutoremoveScriptWithPins(mgr, pins)
		if !strings.Contains(got, "ist gepinnt") {
			t.Errorf("%s-Autoremove sollte gepinnte Pakete aussparen:\n%s", mgr, got)
		}
		if !strings.Contains(got, "linux-image-*") {
			t.Errorf("%s-Autoremove sollte das Kernel-Muster kennen:\n%s", mgr, got)
		}
	}
	// Ohne Pins bleibt der Lauf ueberall der unveraenderte Standard.
	for _, mgr := range []string{pkgApt, pkgDnf, pkgZypper, pkgPacman, pkgApk} {
		if got := pkgAutoremoveScriptWithPins(mgr, nil); got != pkgAutoremoveScript(mgr) {
			t.Errorf("%s ohne Pins sollte der Standard-Lauf sein", mgr)
		}
	}
}

// TestValidPinName: Der Name landet in Konfigurationsdateien und in
// Shell-Kommandos - hier darf nichts durchrutschen.
func TestValidPinName(t *testing.T) {
	ok := []string{"nginx", "linux-image-*", "libstdc++6", "python3.11", "LINUX-IMAGE-*"}
	for _, n := range ok {
		if _, err := validPinName(n); err != nil {
			t.Errorf("validPinName(%q) sollte zulaessig sein: %v", n, err)
		}
	}
	bad := []string{"", "   ", "*", "ngin x", "nginx;rm -rf /", "$(id)", "a`b`", "nginx\nrm", "../etc/passwd", "-o", "--", ".hidden"}
	for _, n := range bad {
		if _, err := validPinName(n); err == nil {
			t.Errorf("validPinName(%q) sollte abgelehnt werden", n)
		}
	}
	// Normalisierung: Kleinschreibung, getrimmt.
	if got, _ := validPinName("  NGINX  "); got != "nginx" {
		t.Errorf("validPinName sollte normalisieren, bekam %q", got)
	}
}

// TestKernelPresetsDeckenPaketverwaltungenAb: Der Ein-Klick-Kernelschutz muss
// fuer jede unterstuetzte Familie eine bewusste Antwort haben (auch „nichts",
// wie bei apk) - ein fehlender Eintrag waere ein stiller Fehlschlag.
func TestKernelPresetsDeckenPaketverwaltungenAb(t *testing.T) {
	for _, mgr := range []string{pkgApt, pkgDnf, pkgYum, pkgZypper, pkgPacman, pkgApk} {
		if _, ok := domain.KernelPinPresets[pkgFamily(mgr)]; !ok {
			t.Errorf("kein Kernel-Preset fuer %q (Familie %q)", mgr, pkgFamily(mgr))
		}
	}
	// Die Presets sind Schutz, KEIN Hold - ein eingefrorener Kernel bekaeme
	// keine Sicherheitsupdates mehr.
	for _, name := range domain.KernelPinPresets[pkgApt] {
		pin := domain.PackagePin{Name: name, NoRemove: true}
		if !pin.Matches("linux-image-6.1.0-13-amd64") && !strings.HasPrefix(name, "linux-headers") {
			t.Errorf("apt-Preset %q trifft den laufenden Kernel nicht", name)
		}
	}
}
