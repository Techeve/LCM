package services

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// dnsSandbox führt die DNS-Skripte gegen ein Verzeichnis statt gegen das
// echte System aus. Die absoluten Pfade werden umgebogen, systemctl,
// resolvectl, getent und systemd-detect-virt sind Attrappen, die über
// Umgebungsvariablen antworten.
type dnsSandbox struct {
	t    *testing.T
	root string
	env  []string
}

func newDNSSandbox(t *testing.T, resolved, lxc, resolveOK bool) *dnsSandbox {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("kein sh")
	}
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	for _, dir := range []string{"bin", "etc", "run/systemd/resolve", "proc"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	fakes := map[string]string{
		"systemctl":           `[ "$1" = is-active ] && [ "$FAKE_RESOLVED" = yes ] && exit 0; [ "$1" = is-active ] && exit 3; exit 0`,
		"resolvectl":          `exit 0`,
		"getent":              `[ "$FAKE_RESOLVE_OK" = yes ]`,
		"nslookup":            `exit 1`,
		"systemd-detect-virt": `[ "$FAKE_LXC" = yes ] && echo lxc || echo none`,
	}
	for name, body := range fakes {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	s := &dnsSandbox{t: t, root: root, env: append(os.Environ(),
		"PATH="+bin+":"+os.Getenv("PATH"),
		"FAKE_RESOLVED="+yesNo(resolved), "FAKE_LXC="+yesNo(lxc), "FAKE_RESOLVE_OK="+yesNo(resolveOK),
	)}
	s.write("run/systemd/resolve/stub-resolv.conf", "nameserver 127.0.0.53\n")
	s.write("proc/1-environ", "")
	return s
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func (s *dnsSandbox) path(rel string) string { return filepath.Join(s.root, rel) }

func (s *dnsSandbox) write(rel, content string) {
	s.t.Helper()
	if err := os.MkdirAll(filepath.Dir(s.path(rel)), 0o755); err != nil {
		s.t.Fatal(err)
	}
	if err := os.WriteFile(s.path(rel), []byte(content), 0o644); err != nil {
		s.t.Fatal(err)
	}
}

func (s *dnsSandbox) read(rel string) string {
	s.t.Helper()
	b, err := os.ReadFile(s.path(rel))
	if err != nil {
		return "<fehlt>"
	}
	return string(b)
}

func (s *dnsSandbox) exists(rel string) bool {
	_, err := os.Lstat(s.path(rel))
	return err == nil
}

func (s *dnsSandbox) run(script string) (string, error) {
	s.t.Helper()
	// Nur absolute Pfade umbiegen: Der Symlink auf den Stub ist relativ
	// (../run/...) und muss so bleiben.
	r := strings.NewReplacer(
		"/etc/", s.root+"/etc/",
		" /run/systemd/", " "+s.root+"/run/systemd/",
		"/proc/1/environ", s.root+"/proc/1-environ",
		"/usr/share/udhcpc/", s.root+"/usr/share/udhcpc/",
	)
	// set -u wie im Helper: Eine ungesetzte Variable fiele sonst erst dort auf.
	cmd := exec.Command("sh", "-c", "set -u\n"+r.Replace(script))
	cmd.Env = s.env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

const pveResolvConf = "search local\nnameserver 8.8.8.8\nnameserver 1.1.1.1\n"

// Der Fall aus dem Betrieb: Ubuntu im Proxmox-Container, systemd-resolved
// aktiv, Proxmox hat den Symlink durch eine eigene Datei mit den Servern des
// Hosts ersetzt.
func TestDNSSetResolvedImContainer(t *testing.T) {
	s := newDNSSandbox(t, true, true, true)
	s.write("etc/resolv.conf", pveResolvConf)

	out, err := s.run(dnsApplyScript([]string{"192.0.2.53", "192.0.2.54"}, "example.com"))
	if err != nil {
		t.Fatalf("setzen fehlgeschlagen: %v\n%s", err, out)
	}
	if got := s.read("etc/systemd/resolved.conf.d/lcm-dns.conf"); got != "[Resolve]\nDNS=192.0.2.53 192.0.2.54\nDomains=~.\n" {
		t.Errorf("Drop-in = %q", got)
	}
	if link, _ := os.Readlink(s.path("etc/resolv.conf")); link != "../run/systemd/resolve/stub-resolv.conf" {
		t.Errorf("resolv.conf zeigt auf %q, erwartet den Stub von systemd-resolved", link)
	}
	if got := s.read("etc/.pve-ignore.resolv.conf"); got != "# LCM-DNS\n" {
		t.Errorf("pve-ignore = %q", got)
	}
	if got := s.read("etc/resolv.conf.lcm-bak"); got != pveResolvConf {
		t.Errorf("Sicherung = %q", got)
	}
	for _, want := range []string{"Proxmox ueberschreibt", "fuehrt jetzt ueber systemd-resolved", "DNS gesetzt (systemd-resolved)"} {
		if !strings.Contains(out, want) {
			t.Errorf("Ausgabe nennt %q nicht:\n%s", want, out)
		}
	}

	out, err = s.run(dnsUnmanageScript())
	if err != nil {
		t.Fatalf("Rückbau fehlgeschlagen: %v\n%s", err, out)
	}
	if got := s.read("etc/resolv.conf"); got != pveResolvConf {
		t.Errorf("resolv.conf nach Rückbau = %q", got)
	}
	for _, rel := range []string{"etc/.pve-ignore.resolv.conf", "etc/systemd/resolved.conf.d/lcm-dns.conf", "etc/resolv.conf.lcm-bak"} {
		if s.exists(rel) {
			t.Errorf("%s nach Rückbau noch da", rel)
		}
	}
}

// Ohne systemd-resolved: Datei schreiben und die DHCP-Clients bremsen.
// Scheitert der Test, ist alles wie vorher, auch die Schutzdateien.
func TestDNSSetDateiMitDHCPClients(t *testing.T) {
	s := newDNSSandbox(t, false, false, true)
	s.write("etc/resolv.conf", pveResolvConf)
	s.write("etc/dhcp/dhclient.conf", "")
	s.write("etc/NetworkManager/conf.d/dhcp-client.conf", "[main]\ndhcp=internal\n")

	out, err := s.run(dnsApplyScript([]string{"192.0.2.53"}, "example.com"))
	if err != nil {
		t.Fatalf("setzen fehlgeschlagen: %v\n%s", err, out)
	}
	if got := s.read("etc/resolv.conf"); got != "nameserver 192.0.2.53\n" {
		t.Errorf("resolv.conf = %q", got)
	}
	if got := s.read("etc/dhcp/dhclient-enter-hooks.d/zz-lcm-dns"); !strings.Contains(got, "make_resolv_conf() { :; }") {
		t.Errorf("dhclient-Hook = %q", got)
	}
	if got := s.read("etc/NetworkManager/conf.d/90-lcm-dns.conf"); !strings.Contains(got, "dns=none") {
		t.Errorf("NetworkManager-Drop-in = %q", got)
	}
	if s.exists("etc/.pve-ignore.resolv.conf") {
		t.Error("pve-ignore außerhalb eines Containers angelegt")
	}

	failing := newDNSSandbox(t, false, true, false)
	failing.write("etc/resolv.conf", pveResolvConf)
	failing.write("etc/dhcp/dhclient.conf", "")
	out, err = failing.run(dnsApplyScript([]string{"192.0.2.99"}, "example.com"))
	if err == nil || !strings.Contains(out, "zurueckgerollt") {
		t.Fatalf("gescheiterter Test hätte zurückrollen müssen: %v\n%s", err, out)
	}
	if got := failing.read("etc/resolv.conf"); got != pveResolvConf {
		t.Errorf("resolv.conf nach Rollback = %q", got)
	}
	for _, rel := range []string{"etc/.pve-ignore.resolv.conf", "etc/dhcp/dhclient-enter-hooks.d/zz-lcm-dns", "etc/resolv.conf.lcm-bak", "etc/resolv.conf.lcm-prev"} {
		if failing.exists(rel) {
			t.Errorf("%s nach Rollback noch da", rel)
		}
	}
}

// Eine pve-ignore-Datei des Betreibers bleibt beim Rückbau liegen.
func TestDNSUnsetLaesstFremdeDateien(t *testing.T) {
	s := newDNSSandbox(t, true, true, true)
	s.write("etc/resolv.conf", pveResolvConf)
	s.write("etc/.pve-ignore.resolv.conf", "")
	if out, err := s.run(dnsApplyScript([]string{"192.0.2.53"}, "example.com")); err != nil {
		t.Fatalf("setzen fehlgeschlagen: %v\n%s", err, out)
	}
	if out, err := s.run(dnsUnmanageScript()); err != nil {
		t.Fatalf("Rückbau fehlgeschlagen: %v\n%s", err, out)
	}
	if !s.exists("etc/.pve-ignore.resolv.conf") {
		t.Error("pve-ignore des Betreibers gelöscht")
	}
}

// Erst ohne systemd-resolved verwaltet, dann mit: Scheitert der Test, darf
// kein Drop-in mit den unerreichbaren Servern zurückbleiben.
func TestDNSRollbackNachWechselZuResolved(t *testing.T) {
	s := newDNSSandbox(t, false, false, true)
	s.write("etc/resolv.conf", pveResolvConf)
	if out, err := s.run(dnsApplyScript([]string{"192.0.2.53"}, "example.com")); err != nil {
		t.Fatalf("setzen fehlgeschlagen: %v\n%s", err, out)
	}
	s.env = append(s.env, "FAKE_RESOLVED=yes", "FAKE_RESOLVE_OK=no")
	if out, err := s.run(dnsApplyScript([]string{"192.0.2.99"}, "example.com")); err == nil {
		t.Fatalf("gescheiterter Test hätte zurückrollen müssen:\n%s", out)
	}
	if s.exists("etc/systemd/resolved.conf.d/lcm-dns.conf") {
		t.Error("Drop-in mit unerreichbarem Server blieb nach dem Rollback liegen")
	}
	if got := s.read("etc/resolv.conf"); got != "nameserver 192.0.2.53\n" {
		t.Errorf("resolv.conf nach Rollback = %q", got)
	}
	if got := s.read("etc/resolv.conf.lcm-bak"); got != pveResolvConf {
		t.Errorf("Sicherung des Ausgangszustands = %q", got)
	}
}

// Gleichnamige Dateien des Betreibers ohne Marke bleiben liegen.
func TestDNSUnsetLaesstFremdeHooks(t *testing.T) {
	s := newDNSSandbox(t, false, false, true)
	s.write("etc/resolv.conf", pveResolvConf)
	s.write("etc/dhcp/dhclient-enter-hooks.d/zz-lcm-dns", "eigener Hook\n")
	s.write("etc/NetworkManager/conf.d/90-lcm-dns.conf", "[main]\n")
	for _, script := range []string{dnsApplyScript([]string{"192.0.2.53"}, "example.com"), dnsUnmanageScript()} {
		if out, err := s.run(script); err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
	}
	if got := s.read("etc/dhcp/dhclient-enter-hooks.d/zz-lcm-dns"); got != "eigener Hook\n" {
		t.Errorf("fremder dhclient-Hook = %q", got)
	}
	if got := s.read("etc/NetworkManager/conf.d/90-lcm-dns.conf"); got != "[main]\n" {
		t.Errorf("fremde NetworkManager-Datei = %q", got)
	}
	if got := s.read("etc/resolv.conf"); got != pveResolvConf {
		t.Errorf("resolv.conf nach Rückbau = %q", got)
	}
}

// dnsCurrentScript zeigt, was Anwendungen fragen, nicht was systemd-resolved
// gern hätte.
func TestDNSCurrentScriptZeigtWirksameServer(t *testing.T) {
	cases := []struct {
		name, resolv, resolvectl, globalDomains, want string
	}{
		{"Datei umgeht systemd-resolved", pveResolvConf,
			"Global: 192.0.2.53\nLink 2 (eth0): 192.0.2.1", "", "8.8.8.8 1.1.1.1"},
		{"Stub mit Domains=~.", "nameserver 127.0.0.53\n",
			"Global: 192.0.2.53#dns.example 192.0.2.54\nLink 2 (eth0): 192.0.2.1", "~.", "192.0.2.53 192.0.2.54"},
		{"Stub ohne Domains=~.", "nameserver 127.0.0.53\n",
			"Global: 192.0.2.53\nLink 2 (eth0): 192.0.2.1", "", "192.0.2.53 192.0.2.1"},
		{"glibc liest nur drei", "nameserver 192.0.2.1\nnameserver 192.0.2.2\nnameserver 192.0.2.3\nnameserver 192.0.2.4\n",
			"", "", "192.0.2.1 192.0.2.2 192.0.2.3"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newDNSSandbox(t, true, false, true)
			s.write("etc/resolv.conf", c.resolv)
			domains := "Global: " + c.globalDomains + "\nLink 2 (eth0): "
			fake := "[ \"$1\" = dns ] && printf '%s\\n' '" + strings.ReplaceAll(c.resolvectl, "\n", "' '") + "'\n" +
				"[ \"$1\" = domain ] && printf '%s\\n' '" + strings.ReplaceAll(domains, "\n", "' '") + "'\nexit 0\n"
			s.write("bin/resolvectl", "#!/bin/sh\n"+fake)
			if err := os.Chmod(s.path("bin/resolvectl"), 0o755); err != nil {
				t.Fatal(err)
			}
			out, err := s.run(dnsCurrentScript())
			if err != nil {
				t.Fatalf("%v\n%s", err, out)
			}
			if got := strings.Join(strings.Fields(out), " "); got != c.want {
				t.Errorf("wirksame Server = %q, erwartet %q", got, c.want)
			}
		})
	}
}

// Voll-Modus und Helper teilen sich denselben Text - sonst läuft das eine
// dem anderen davon.
func TestDNSHelperNutztGemeinsameFunktionen(t *testing.T) {
	if !strings.Contains(lcmHelperScript, dnsShellFuncs) {
		t.Fatal("Helper enthält dnsShellFuncs nicht")
	}
	if !strings.Contains(dnsApplyScript([]string{"192.0.2.53"}, "x.example"), dnsShellFuncs) {
		t.Fatal("Inline-Skript enthält dnsShellFuncs nicht")
	}
}
