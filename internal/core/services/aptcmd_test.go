package services

import (
	"errors"
	"strings"
	"testing"
)

func TestParsePackageNames(t *testing.T) {
	got, err := parsePackageNames("htop, unzip  openssh-server\tnginx;htop")
	if err != nil {
		t.Fatalf("gültige liste abgelehnt: %v", err)
	}
	want := []string{"htop", "unzip", "openssh-server", "nginx"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("parse falsch: %v", got)
	}

	// Leere Liste.
	if _, err := parsePackageNames("  , ;"); !errors.Is(err, ErrNoPackages) {
		t.Errorf("leere liste sollte ErrNoPackages liefern, bekam %v", err)
	}

	// Injection-Versuche werden abgewiesen.
	for _, bad := range []string{"htop; rm -rf /", "foo && bar", "$(whoami)", "pkg`id`", "a|b", "../etc"} {
		if _, err := parsePackageNames(bad); !errors.Is(err, ErrInvalidPackage) {
			t.Errorf("gefährlicher name %q nicht abgelehnt: %v", bad, err)
		}
	}
}

func TestAptScripts(t *testing.T) {
	if !strings.Contains(aptUpgradeAllScript(), "apt-get -o Dpkg::Options::=--force-confold -y upgrade") {
		t.Errorf("upgrade-all-skript unerwartet: %s", aptUpgradeAllScript())
	}

	pkgScript := aptUpgradePackagesScript([]string{"htop", "nginx"})
	if !strings.Contains(pkgScript, "install --only-upgrade -y htop nginx") {
		t.Errorf("packages-skript unerwartet: %s", pkgScript)
	}

	verScript := aptInstallVersionScript("nginx", "1.24.0-1")
	if !strings.Contains(verScript, "install -y --allow-downgrades nginx=1.24.0-1") {
		t.Errorf("version-skript unerwartet: %s", verScript)
	}

	sec := aptSecurityUpgradeScript()
	if !strings.Contains(sec, "security") || !strings.Contains(sec, "install --only-upgrade -y $pkgs") {
		t.Errorf("security-skript unerwartet: %s", sec)
	}
}

func TestScriptForRuleApt(t *testing.T) {
	for _, tc := range []struct{ typ, cmd, want string }{
		{"update", "", "-y upgrade"},
		{"security", "", "security"},
		{"packages", "htop unzip", "--only-upgrade -y htop unzip"},
	} {
		script, ok := scriptForRule(pkgApt, tc.typ, tc.cmd)
		if !ok || !strings.Contains(script, tc.want) {
			t.Errorf("scriptForRule(apt, %q) → ok=%v script=%q", tc.typ, ok, script)
		}
	}
	// Nicht-Paket-Typen liefern false.
	if _, ok := scriptForRule(pkgApt, "health", ""); ok {
		t.Error("health ist kein paket-typ")
	}
}

func TestParseMadison(t *testing.T) {
	out := ` nginx | 1.24.0-1 | http://deb.debian.org/debian bookworm/main amd64 Packages
 nginx | 1.22.1-9 | http://deb.debian.org/debian bookworm/main amd64 Packages
 nginx | 1.22.1-9 | http://security.debian.org bookworm-security/main amd64 Packages`
	got := parseMadison(out)
	want := []string{"1.24.0-1", "1.22.1-9"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("madison-parse falsch: %v", got)
	}
}
