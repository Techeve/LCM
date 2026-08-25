package services

import (
	"strings"
	"testing"

	"LCM/internal/core/domain"
)

func TestScriptForRuleMultiDistro(t *testing.T) {
	cases := []struct {
		mgr, typ, cmd, want string
	}{
		{pkgDnf, "update", "", "dnf -y --refresh upgrade"},
		{pkgYum, "update", "", "yum -y --refresh upgrade"},
		{pkgZypper, "update", "", "zypper --non-interactive update"},
		{pkgDnf, "security", "", "--security upgrade"},
		{pkgZypper, "security", "", "patch --category security"},
		{pkgDnf, "package-scan", "", "dnf -y makecache"},
		{pkgZypper, "package-scan", "", "zypper --non-interactive refresh"},
		{pkgDnf, "packages", "htop curl", "dnf -y --refresh upgrade htop curl"},
		{pkgZypper, "packages", "htop curl", "zypper --non-interactive update htop curl"},
	}
	for _, c := range cases {
		got, ok := scriptForRule(c.mgr, c.typ, c.cmd)
		if !ok || !strings.Contains(got, c.want) {
			t.Errorf("scriptForRule(%s,%s) = %q, sollte %q enthalten", c.mgr, c.typ, got, c.want)
		}
	}

	// Versionsgenaue Installation (Downgrade erlaubt).
	if s := pkgInstallVersionScript(pkgDnf, "nginx", "1.20.1-14.el9"); !strings.Contains(s, "downgrade") {
		t.Errorf("dnf install-version sollte downgrade-fallback haben: %q", s)
	}
	if s := pkgInstallVersionScript(pkgZypper, "nginx", "1.20.1"); !strings.Contains(s, "--oldpackage") {
		t.Errorf("zypper install-version sollte --oldpackage nutzen: %q", s)
	}
	// apt bleibt die Vorgabe für unbekannte Manager.
	if s := pkgUpgradeAllScript(""); !strings.Contains(s, "apt-get") {
		t.Errorf("unbekannter mgr sollte auf apt fallen: %q", s)
	}
}

func TestScriptForRulePacmanApk(t *testing.T) {
	// pacman und apk sind vollwertig unterstützt.
	for _, m := range []string{pkgPacman, pkgApk} {
		if !PackageManagerSupported(m) {
			t.Errorf("%s sollte unterstützt sein", m)
		}
	}
	cases := []struct {
		mgr, typ, want string
	}{
		{pkgPacman, "update", "pacman -Syu --noconfirm"},
		{pkgPacman, "package-scan", "pacman -Sy"},
		{pkgApk, "update", "apk upgrade"},
		{pkgApk, "package-scan", "apk update"},
	}
	for _, c := range cases {
		got, ok := scriptForRule(c.mgr, c.typ, "")
		if !ok || !strings.Contains(got, c.want) {
			t.Errorf("scriptForRule(%s,%s) = %q, sollte %q enthalten", c.mgr, c.typ, got, c.want)
		}
	}
	// pacman/apk haben keinen Security-Kanal → Security == vollständiges Update.
	if s, _ := scriptForRule(pkgPacman, "security", ""); s != pkgUpgradeAllScript(pkgPacman) {
		t.Errorf("pacman security sollte ein Vollupdate sein: %q", s)
	}
	if s, _ := scriptForRule(pkgApk, "security", ""); s != pkgUpgradeAllScript(pkgApk) {
		t.Errorf("apk security sollte ein Vollupdate sein: %q", s)
	}
	// pacman kennt keine Versions-Fixierung → ehrlicher Abbruch.
	if s := pkgInstallVersionScript(pkgPacman, "nginx", "1.2.3-1"); !strings.Contains(s, "exit 1") {
		t.Errorf("pacman install-version sollte mit Fehler abbrechen: %q", s)
	}
	// apk kann pinnen: apk add name=version.
	if s := pkgInstallVersionScript(pkgApk, "nginx", "1.2.3-r0"); !strings.Contains(s, "apk add nginx=1.2.3-r0") {
		t.Errorf("apk install-version sollte name=version nutzen: %q", s)
	}
}

// TestAutoremoveScripts prüft je Paketverwaltung den Autoremove-Befehl und
// dass „autoremove" als Rule-Typ über scriptForRule läuft.
func TestAutoremoveScripts(t *testing.T) {
	cases := []struct{ mgr, want string }{
		{pkgApt, "apt-get -o Dpkg::Options::=--force-confold -y autoremove"},
		{pkgDnf, "dnf -y autoremove"},
		{pkgYum, "yum -y autoremove"},
		{pkgZypper, "packages --unneeded"},
		{pkgPacman, "pacman -Qdtq"},
		{pkgApk, "einen separaten autoremove-Befehl gibt es nicht"},
		{"", "apt-get"}, // unbekannt → apt
	}
	for _, c := range cases {
		if got := pkgAutoremoveScript(c.mgr); !strings.Contains(got, c.want) {
			t.Errorf("pkgAutoremoveScript(%q) = %q, sollte %q enthalten", c.mgr, got, c.want)
		}
		// Auch über die Rule-Dispatch-Tabelle (Gruppen-Regel-Pfad).
		if s, ok := scriptForRule(c.mgr, "autoremove", ""); !ok || s != pkgAutoremoveScript(c.mgr) {
			t.Errorf("scriptForRule(%q, autoremove) inkonsistent: ok=%v", c.mgr, ok)
		}
	}
	// pacman/zypper-Autoremove sind guarded (No-op ohne Treffer).
	if s := pkgAutoremoveScript(pkgPacman); !strings.Contains(s, "keine verwaisten Pakete") {
		t.Errorf("pacman-autoremove sollte No-op-Meldung haben: %q", s)
	}
}

// TestRemovePackagesScripts prüft das gezielte Entfernen je Paketverwaltung.
func TestRemovePackagesScripts(t *testing.T) {
	names := []string{"altpaket", "unnötig"}
	cases := []struct{ mgr, want string }{
		{pkgApt, "-y remove altpaket unnötig"},
		{pkgDnf, "dnf -y remove altpaket unnötig"},
		{pkgZypper, "zypper --non-interactive remove altpaket unnötig"},
		{pkgPacman, "pacman -R --noconfirm altpaket unnötig"},
		{pkgApk, "apk del altpaket unnötig"},
	}
	for _, c := range cases {
		if got := pkgRemovePackagesScript(c.mgr, names); !strings.Contains(got, c.want) {
			t.Errorf("pkgRemovePackagesScript(%q) = %q, sollte %q enthalten", c.mgr, got, c.want)
		}
	}
}

// TestProtectedPackage sichert, dass kritische Systempakete nicht entfernbar
// sind, gewöhnliche Pakete aber schon.
func TestProtectedPackage(t *testing.T) {
	protected := []string{"openssh-server", "sudo", "systemd", "apt", "dpkg", "dnf", "pacman", "apk-tools", "libc6", "bash",
		"linux-image-6.1.0-18-amd64", "kernel-core", "linux-headers-generic"}
	for _, p := range protected {
		if !isProtectedPackage(p) {
			t.Errorf("%q sollte geschützt sein", p)
		}
	}
	for _, p := range []string{"htop", "nginx", "curl", "vim", "linux-cowsay", "my-app"} {
		if isProtectedPackage(p) {
			t.Errorf("%q sollte NICHT geschützt sein", p)
		}
	}
}

func TestParsePacmanAndApkVersions(t *testing.T) {
	pac := parsePacmanVersions("Repository      : extra\nName            : nginx\nVersion         : 1.25.3-1\nDescription     : ...")
	if len(pac) != 1 || pac[0] != "1.25.3-1" {
		t.Errorf("pacman-version falsch: %v", pac)
	}
	// apk policy: aufsteigend gelistet → neueste zuerst zurück.
	apk := parseApkVersions("nginx policy:\n  1.24.0-r15:\n    lib/apk/db/installed\n  1.24.0-r16:\n    https://dl-cdn.alpinelinux.org/alpine/v3.19/main\n")
	if strings.Join(apk, ",") != "1.24.0-r16,1.24.0-r15" {
		t.Errorf("apk-versionen (neueste zuerst) falsch: %v", apk)
	}
}

func TestParseDnfVersions(t *testing.T) {
	out := `Last metadata expiration check: 0:12:01 ago.
Installed Packages
nginx.x86_64        1:1.20.1-14.el9        @appstream
Available Packages
nginx.x86_64        1:1.20.1-14.el9        appstream
nginx.x86_64        1:1.22.1-1.el9         appstream`
	got := parseDnfVersions(out)
	want := []string{"1:1.20.1-14.el9", "1:1.22.1-1.el9"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("dnf-versionen falsch: %v", got)
	}
}

func TestParseZypperVersions(t *testing.T) {
	out := `S | Name  | Type    | Version       | Arch   | Repository
--+-------+---------+---------------+--------+-----------
i | nginx | package | 1.20.1-1.5    | x86_64 | repo-oss
  | nginx | package | 1.21.0-1.3    | x86_64 | repo-update
  | nginx | srcpackage | 1.21.0-1.3 | noarch | repo-src`
	got := parseZypperVersions("nginx", out)
	want := []string{"1.20.1-1.5", "1.21.0-1.3"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("zypper-versionen falsch: %v", got)
	}
}

func TestApplyDnfUpgradesAndZypperUpdates(t *testing.T) {
	pkgs := []domain.Package{{Name: "nginx", Version: "1.20.1-14.el9"}, {Name: "curl", Version: "7.76.1-23.el9"}}
	applyDnfUpgrades(pkgs, `Last metadata expiration…
nginx.x86_64   1:1.22.1-1.el9   appstream`)
	if pkgs[0].CandidateVersion != "1.22.1-1.el9" || !pkgs[0].Outdated() {
		t.Errorf("dnf-upgrade nicht übernommen (epoch entfernt?): %+v", pkgs[0])
	}
	if pkgs[1].CandidateVersion != "" {
		t.Errorf("curl sollte kein update haben: %+v", pkgs[1])
	}

	zp := []domain.Package{{Name: "nginx", Version: "1.20.1-1.5"}}
	applyZypperUpdates(zp, `S | Repository | Name | Current Version | Available Version | Arch
v | repo | nginx | 1.20.1-1.5 | 1.20.2-1.5 | x86_64`)
	if zp[0].CandidateVersion != "1.20.2-1.5" {
		t.Errorf("zypper-update nicht übernommen: %+v", zp[0])
	}
}

func TestParseRepoURIs(t *testing.T) {
	out := `baseurl=https://download.example.com/9/BaseOS/x86_64/os/
baseurl=http://mirror.example.com/rocky/9/AppStream/x86_64/os/
mirrorlist=https://mirrors.rockylinux.org/mirrorlist?repo=BaseOS`
	repos := parseRepoURIs(out)
	if len(repos) != 3 {
		t.Fatalf("erwartete 3 repos, bekam %d", len(repos))
	}
	// Die http://-Quelle ist als unsicher markiert.
	var insecure int
	for _, r := range repos {
		if r.Insecure {
			insecure++
		}
	}
	if insecure != 1 {
		t.Errorf("genau eine unsichere quelle erwartet, bekam %d", insecure)
	}
}

// TestJederUpdatePfadFrischtDieListeAuf: Ein Update-Lauf gegen veraltete
// Metadaten meldet „erfolgreich" und laesst ein gerade veroeffentlichtes
// Paket liegen. Innerhalb einer Paketverwaltung machten das die drei Pfade
// (alle / Security / benannte Pakete) unterschiedlich - dnf gab beim
// Voll-Update `--refresh` mit, bei benannten Paketen nicht; zypper refreshte
// vor `update`, aber nicht vor `patch`.
func TestJederUpdatePfadFrischtDieListeAuf(t *testing.T) {
	// Erwartet wird je Familie das Kommando, das die Paketliste auffrischt.
	// apt setzt zwischen Befehl und Unterbefehl noch Optionen - deshalb der
	// zusammengesetzte Marker statt „apt-get update".
	refresh := map[string]string{
		pkgApt:    aptNonInteractive + " update",
		pkgDnf:    "--refresh",
		pkgZypper: "refresh",
		pkgApk:    "apk update",
	}
	for mgr, marker := range refresh {
		for name, script := range map[string]string{
			"alle":     pkgUpgradeAllScript(mgr),
			"security": pkgSecurityUpgradeScript(mgr),
			"benannt":  pkgUpgradePackagesScript(mgr, []string{"htop"}),
		} {
			if !strings.Contains(script, marker) {
				t.Errorf("%s/%s frischt die paketliste nicht auf (%q fehlt): %s", mgr, name, marker, script)
			}
		}
	}

	// pacman ist die begruendete Ausnahme: `-Syu` frischt beim Voll-Update auf,
	// aber `pacman -Sy <paket>` waere auf einem Rolling Release der klassische
	// Weg in ein halb aktualisiertes System - das Paket kaeme aus der frischen
	// Datenbank, seine Abhaengigkeiten blieben alt.
	if !strings.Contains(pkgUpgradeAllScript(pkgPacman), "-Syu") {
		t.Error("pacman voll-update muss synchronisieren")
	}
	if strings.Contains(pkgUpgradePackagesScript(pkgPacman, []string{"htop"}), "-Sy ") {
		t.Error("pacman darf bei benannten paketen NICHT synchronisieren (partial upgrade)")
	}
}
