package services

import (
	"strings"
	"testing"

	"LCM/internal/core/domain"
)

func TestParseSnapListAndRefresh(t *testing.T) {
	list := `Name    Version   Rev    Tracking       Publisher   Notes
core22  20240111  1122   latest/stable  canonical✓  base
lxd     5.0.2     27948  5.0/stable     canonical✓  -
snapd   2.61.1    20671  latest/stable  canonical✓  snapd`

	snaps := parseSnapList(list)
	if len(snaps) != 3 {
		t.Fatalf("erwartete 3 snaps, bekam %d: %+v", len(snaps), snaps)
	}
	if snaps[0].Name != "core22" || snaps[0].Version != "20240111" ||
		snaps[0].Revision != "1122" || snaps[0].Channel != "latest/stable" {
		t.Errorf("core22 falsch geparst: %+v", snaps[0])
	}
	// Verifiziert-Häkchen am Publisher wird entfernt.
	if snaps[0].Publisher != "canonical" {
		t.Errorf("publisher sollte 'canonical' sein, ist %q", snaps[0].Publisher)
	}

	refresh := `Name  Version  Rev    Publisher   Notes
lxd   5.0.3    28000  canonical✓  -`
	applySnapRefresh(snaps, refresh)

	if snaps[1].Name != "lxd" || snaps[1].CandidateVersion != "5.0.3" || !snaps[1].Outdated() {
		t.Errorf("lxd sollte Update 5.0.3 haben: %+v", snaps[1])
	}
	if snaps[0].CandidateVersion != "" || snaps[0].Outdated() {
		t.Errorf("core22 sollte kein Update haben: %+v", snaps[0])
	}

	// Leere / fehlende snap-Ausgabe ⇒ keine Snaps, kein Absturz.
	if got := parseSnapList(""); len(got) != 0 {
		t.Errorf("leere Ausgabe sollte 0 snaps liefern, bekam %d", len(got))
	}
	applySnapRefresh(nil, "All snaps up to date.")
}

func TestParseDiskVolumes(t *testing.T) {
	// TSV wie von diskVolumesCmd: mountpoint \t device \t fstype \t total \t used
	out := "/\t/dev/sda1\text4\t40960\t12800\n" +
		"/boot\t/dev/sda2\text4\t976\t180\n" +
		"/data\t/dev/sdb1\txfs\t512000\t384000\n" +
		"\t\t\t\t\n" + // kaputte Zeile (falsche Feldzahl bzw. leer) → übersprungen
		"/mnt/x\t/dev/sdc1\text4\t0\t0\n" // total 0 → übersprungen

	vols := parseDiskVolumes(out)
	if len(vols) != 3 {
		t.Fatalf("erwartete 3 Volumes, bekam %d: %+v", len(vols), vols)
	}
	if vols[0].Mountpoint != "/" || vols[0].Device != "/dev/sda1" || vols[0].Fstype != "ext4" ||
		vols[0].TotalMB != 40960 || vols[0].UsedMB != 12800 {
		t.Errorf("Root-Volume falsch geparst: %+v", vols[0])
	}
	if !vols[0].IsRoot() || vols[2].IsRoot() {
		t.Error("IsRoot-Erkennung falsch")
	}
	if p := vols[2].UsagePercent(); p != 75 {
		t.Errorf("/data sollte 75%% belegt sein, bekam %d", p)
	}
	if got := parseDiskVolumes(""); len(got) != 0 {
		t.Errorf("leere Ausgabe sollte 0 Volumes liefern, bekam %d", len(got))
	}
}

// RPM führt jeden importierten Repository-Signaturschlüssel als Pseudo-Paket
// "gpg-pubkey". Mehrere Fremdquellen liefern den Namen also mehrfach - das
// ließ die Bestandsaufnahme am Unique-Index (server_id, name) scheitern und
// den Server als leere Hülle zurück (BUG-006). Datengrundlage: die sechs
// Schlüssel eines openSUSE Leap 16 im Auslieferungszustand.
func TestParseRPMListDropsGPGPseudoPackages(t *testing.T) {
	out := `gpg-pubkey 25db7ae0-63c8b8f1
bash 5.2.15-150600.4.1
gpg-pubkey 287a0027-6465e3d2
zypper 1.14.68-150600.1.1
gpg-pubkey 29b700a4-62b07e22
gpg-pubkey 09d9ea69-65b8a4c5
gpg-pubkey 3fa1d6ce-63c8b8f1
gpg-pubkey 39db7c82-5847eb1f
openssh 9.6p1-150600.3.3`

	pkgs := parseRPMList(out)

	if len(pkgs) != 3 {
		t.Fatalf("erwartete 3 echte Pakete, bekam %d: %+v", len(pkgs), pkgs)
	}
	for _, p := range pkgs {
		if p.Name == "gpg-pubkey" {
			t.Error("gpg-pubkey ist kein Paket und darf nicht im Bestand landen")
		}
	}
	if pkgs[0].Name != "bash" || pkgs[0].Version != "5.2.15-150600.4.1" {
		t.Errorf("erstes Paket falsch geparst: %+v", pkgs[0])
	}
}

// Ein doppelter Paketname darf die Aufnahme nie zum Scheitern bringen -
// unabhängig von der Paketverwaltung (Sicherheitsnetz zu BUG-006).
func TestParseDpkgListDeduplicatesByName(t *testing.T) {
	pkgs := parseDpkgList("bash 5.2.15\nopenssl 3.0.11\nbash 5.2.16\n")

	if len(pkgs) != 2 {
		t.Fatalf("erwartete 2 Pakete nach Deduplizierung, bekam %d: %+v", len(pkgs), pkgs)
	}
	if pkgs[0].Name != "bash" || pkgs[0].Version != "5.2.15" {
		t.Errorf("erster Treffer je Name soll gewinnen, bekam %+v", pkgs[0])
	}
}

func TestSplitApkNameVersion(t *testing.T) {
	cases := []struct{ token, name, version string }{
		{"busybox-1.36.1-r15", "busybox", "1.36.1-r15"},
		{"py3-foo-1.2-r0", "py3-foo", "1.2-r0"}, // bindestrichhaltiger Name
		{"musl-1.2.4_git20230717-r4", "musl", "1.2.4_git20230717-r4"},
		{"kein-treffer", "kein-treffer", ""}, // keine -rN-Revision
	}
	for _, c := range cases {
		n, v := splitApkNameVersion(c.token)
		if n != c.name || v != c.version {
			t.Errorf("splitApkNameVersion(%q) = (%q,%q), erwartet (%q,%q)", c.token, n, v, c.name, c.version)
		}
	}
}

func TestParseApkListAndUpgradesAndRepos(t *testing.T) {
	pkgs := parseApkList("busybox-1.36.1-r15 x86_64 {busybox} (GPL-2.0-only) [installed]\nopenssl-3.1.4-r5 x86_64 {openssl} (Apache-2.0) [installed]\n")
	if len(pkgs) != 2 || pkgs[0].Name != "busybox" || pkgs[0].Version != "1.36.1-r15" {
		t.Fatalf("apk-liste falsch geparst: %+v", pkgs)
	}
	applyApkUpgrades(pkgs, "openssl-3.1.4-r5 < 3.1.4-r6\n")
	if pkgs[1].Name != "openssl" || pkgs[1].CandidateVersion != "3.1.4-r6" {
		t.Errorf("apk-upgrade nicht übernommen: %+v", pkgs[1])
	}
	repos := parseApkRepos("#/media/cdrom/apks\nhttps://dl-cdn.alpinelinux.org/alpine/v3.19/main\nhttp://dl-cdn.alpinelinux.org/alpine/v3.19/community\n")
	if len(repos) != 2 {
		t.Fatalf("erwartete 2 apk-repos (Kommentar übersprungen), bekam %d", len(repos))
	}
	if repos[0].Insecure || !repos[1].Insecure { // https sicher, http unsicher
		t.Errorf("apk-repo-sicherheit falsch bewertet: %+v", repos)
	}
}

func TestApplyPacmanUpgrades(t *testing.T) {
	pkgs := []domain.Package{{Name: "openssl", Version: "3.2.1-1"}, {Name: "bash", Version: "5.2.021-1"}}
	applyPacmanUpgrades(pkgs, "openssl 3.2.1-1 -> 3.2.2-1\n")
	if pkgs[0].CandidateVersion != "3.2.2-1" || pkgs[1].CandidateVersion != "" {
		t.Errorf("pacman-upgrade nicht korrekt übernommen: %+v", pkgs)
	}
}

func TestRepoAddScriptPerPackageManager(t *testing.T) {
	repo := domain.KnownRepo{Key: "acme", Name: "ACME", KeyURL: "https://acme.example/key", Line: "https://acme.example/repo"}
	cases := []struct {
		mgr, want string
	}{
		{pkgDnf, "/etc/yum.repos.d/lcm-acme.repo"},
		{pkgZypper, "zypper --non-interactive addrepo"},
		{pkgPacman, "pacman-key --lsign-key"},
		{pkgApk, "/etc/apk/repositories"},
	}
	for _, c := range cases {
		if s := repoAddScript(c.mgr, repo); !strings.Contains(s, c.want) {
			t.Errorf("repoAddScript(%s) sollte %q enthalten:\n%s", c.mgr, c.want, s)
		}
	}
	// apt bleibt die deb-Zeile mit Keyring.
	aptRepo := domain.KnownRepo{Key: "acme", Line: "deb https://acme.example/repo stable main", KeyURL: "https://acme.example/key"}
	if s := repoAddScript(pkgApt, aptRepo); !strings.Contains(s, "/etc/apt/keyrings/acme.asc") || !strings.Contains(s, "apt-get update") {
		t.Errorf("apt repoAddScript unerwartet:\n%s", s)
	}
}

// Proxmox Datacenter Manager fehlte in der Erkennung: er wurde als gewöhnliches
// Debian geführt, sodass die Schutzsperre für Proxmox-Systeme dort nicht griff
// und LCM z.B. dessen Firewall-Konfiguration hätte überschreiben können
// (BUG-025).
func TestDetectProxmoxRecognizesDatacenterManager(t *testing.T) {
	cases := []struct {
		name        string
		dpkgOutput  string
		wantType    string
		wantVersion string
	}{
		{"Datacenter Manager", "proxmox-datacenter-manager 0.9.1-1\n", "pdm", "0.9.1"},
		{"Virtual Environment", "pve-manager 8.2.4\n", "pve", "8.2.4"},
		{"Mail Gateway", "pmg-api 9.1.0\n", "pmg", "9.1.0"},
		{"kein Proxmox", "\n", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			typ, version := detectProxmox(func(label, cmd string) string { return tc.dpkgOutput })
			if typ != tc.wantType || version != tc.wantVersion {
				t.Errorf("erwartet (%q, %q), bekam (%q, %q)", tc.wantType, tc.wantVersion, typ, version)
			}
		})
	}
}
