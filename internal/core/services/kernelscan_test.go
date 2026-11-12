package services

import (
	"strings"
	"testing"

	"LCM/internal/core/domain"
)

// TestKernelListCommandProxmox: Auf einem Proxmox-Host stellen NICHT
// linux-image-Pakete die Kernel, sondern proxmox-kernel-* (frueher
// pve-kernel-*). Ein reiner linux-image-Filter faende dort nichts - genau
// deshalb bekommt Proxmox einen eigenen Zweig.
func TestKernelListCommandProxmox(t *testing.T) {
	pve := kernelListCommand(pkgApt, true)
	for _, want := range []string{"proxmox-kernel", "pve-kernel", "linux-image"} {
		if !strings.Contains(pve, want) {
			t.Errorf("Proxmox-Kommando erfasst %q nicht:\n%s", want, pve)
		}
	}
	// Nur installierte Pakete: dpkg-Status "ii" (das `grep ^ii` von Hand).
	if !strings.Contains(pve, "ii") {
		t.Errorf("Proxmox-Kommando filtert nicht auf installierte Pakete:\n%s", pve)
	}

	// Ohne Proxmox bleibt es beim Standard - proxmox-kernel taucht dort nicht
	// auf, sonst listete ein normales Debian Pakete, die es nie hat.
	plain := kernelListCommand(pkgApt, false)
	if strings.Contains(plain, "proxmox-kernel") {
		t.Errorf("Nicht-Proxmox-Kommando sollte proxmox-kernel nicht filtern:\n%s", plain)
	}
	if !strings.Contains(plain, "linux-image") {
		t.Errorf("Nicht-Proxmox-Kommando sollte linux-image filtern:\n%s", plain)
	}
}

// TestKernelListCommandProPaketverwaltung: Jede unterstuetzte Familie braucht
// ihr eigenes Mittel - und darf NICHT auf den apt-Zweig zurueckfallen (der
// stille Fallback aus BUG-012 endete dort mit exit 127).
func TestKernelListCommandProPaketverwaltung(t *testing.T) {
	cases := []struct{ mgr, want string }{
		{pkgApt, "dpkg-query"},
		{pkgDnf, "rpm -qa"},
		{pkgYum, "rpm -qa"},
		{pkgZypper, "kernel-default"},
		{pkgPacman, "pacman -Q"},
		{pkgApk, "apk info"},
	}
	for _, c := range cases {
		got := kernelListCommand(c.mgr, false)
		if !strings.Contains(got, c.want) {
			t.Errorf("kernelListCommand(%q) enthaelt %q nicht:\n%s", c.mgr, c.want, got)
		}
		if c.mgr != pkgApt && strings.Contains(got, "dpkg") {
			t.Errorf("kernelListCommand(%q) faellt faelschlich auf dpkg zurueck:\n%s", c.mgr, got)
		}
	}
}

// TestParseKernelListProxmox arbeitet mit echter dpkg-Ausgabe.
func TestParseKernelListProxmox(t *testing.T) {
	out := `proxmox-kernel-6.8.12-4-pve-signed 6.8.12-4
proxmox-kernel-6.8.4-2-pve-signed 6.8.4-2
proxmox-kernel-6.8 6.8.12-4
pve-kernel-5.15.108-1-pve 5.15.108-1
`
	pkgs := parseKernelList(pkgApt, out)
	// Das META-Paket proxmox-kernel-6.8 installiert keinen konkreten Kernel
	// und darf die Zahl der Rueckfall-Kernel nicht verfaelschen.
	if len(pkgs) != 3 {
		t.Fatalf("erwartete 3 echte Kernel-Pakete, bekam %d: %+v", len(pkgs), pkgs)
	}
	for _, p := range pkgs {
		if p.Name == "proxmox-kernel-6.8" {
			t.Error("META-Paket proxmox-kernel-6.8 haette ausgelassen werden muessen")
		}
		if p.Release == "" {
			t.Errorf("%s: Kernel-Fassung nicht abgeleitet", p.Name)
		}
	}
	if pkgs[0].Release != "6.8.12-4-pve" {
		t.Errorf("Signatur-Suffix nicht entfernt: %q", pkgs[0].Release)
	}
}

// TestParseKernelListDebian: Das Meta-Paket linux-image-amd64 faellt raus,
// die konkreten Kernel bleiben.
func TestParseKernelListDebian(t *testing.T) {
	out := `linux-image-6.1.0-13-amd64 6.1.55-1
linux-image-6.1.0-18-amd64 6.1.76-1
linux-image-amd64 6.1.76-1
`
	pkgs := parseKernelList(pkgApt, out)
	if len(pkgs) != 2 {
		t.Fatalf("erwartete 2 Kernel, bekam %d: %+v", len(pkgs), pkgs)
	}
}

// TestParseKernelListPacman: Dort steht die Fassung in der VERSION, nicht im
// Namen - ohne diesen Zweig faende der Parser auf Arch gar nichts.
func TestParseKernelListPacman(t *testing.T) {
	pkgs := parseKernelList(pkgPacman, "linux 6.9.1.arch1-1\nlinux-lts 6.6.30-1\n")
	if len(pkgs) != 2 {
		t.Fatalf("erwartete 2 Kernel, bekam %d: %+v", len(pkgs), pkgs)
	}
	if pkgs[0].Release == "" {
		t.Errorf("Fassung aus der Version nicht abgeleitet: %+v", pkgs[0])
	}
}

// TestKernelFassungWieUname: pacman schreibt "7.2.14.arch1-1", der Kernel
// meldet sich als "7.2.14-arch1-1"; apk "linux-lts-6.12.115-r0" gegen
// "6.12.115-0-lts". Ohne Angleichung fand der Abgleich auf Arch und Alpine
// den laufenden Kernel nie, und ein x.y.0-Kernel ("6.12.arch1-1") fiel
// ganz heraus. Die Paare stammen aus archlinux und Alpine 3.22
// (Paketversion gegen /usr/lib/modules).
func TestKernelFassungWieUname(t *testing.T) {
	arch := parseKernelList(pkgPacman, "linux 7.2.14.arch1-1\nlinux-lts 6.18.60-1\nlinux-hardened 6.12.hardened1-1\n")
	alpine := parseKernelList(pkgApk, "linux-lts-6.12.115-r0\nlinux-virt-6.12.115-r0\n")
	want := map[string]string{
		"linux": "7.2.14-arch1-1", "linux-lts": "6.18.60-1-lts", "linux-hardened": "6.12.0-hardened1-1-hardened",
		"linux-lts-6.12.115-r0": "6.12.115-0-lts", "linux-virt-6.12.115-r0": "6.12.115-0-virt",
	}
	got := map[string]string{}
	for _, p := range append(arch, alpine...) {
		got[p.Name] = p.Release
	}
	for name, release := range want {
		if got[name] != release {
			t.Errorf("%s: fassung %q, erwartet %q", name, got[name], release)
		}
	}
	// Läuft der virt-Kernel, ist nur er markiert - nicht auch linux-lts
	// derselben Version.
	info := domain.BuildKernelInfo("6.12.115-0-virt", "kvm", alpine)
	for _, p := range info.Installed {
		if p.Running != (p.Name == "linux-virt-6.12.115-r0") {
			t.Errorf("%s: running=%v", p.Name, p.Running)
		}
	}
	if info := domain.BuildKernelInfo("6.18.60-1-lts", "kvm", arch); !info.Installed[1].Running {
		t.Errorf("laufender arch-kernel nicht erkannt: %+v", info.Installed)
	}
}

// TestScanKernelsUeberspringtContainer: In einem Container wird gar nicht erst
// gefragt - dort laeuft der Kernel des Hosts, ein Kommando auf einer fremden
// Maschine waere verschwendete Zeit fuer ein Ergebnis, das ohnehin verworfen
// wird.
func TestScanKernelsUeberspringtContainer(t *testing.T) {
	calls := 0
	run := func(_, _ string) string {
		calls++
		return "linux-image-5.15.0-91-generic 5.15.0-91\n"
	}
	if got := scanKernels(pkgApt, "lxc", false, run); got != nil {
		t.Errorf("im Container sollte nichts erfasst werden, bekam %+v", got)
	}
	if calls != 0 {
		t.Errorf("im Container sollte kein Kommando laufen, es waren %d", calls)
	}
	// Auf Blech/VM dagegen schon.
	if got := scanKernels(pkgApt, "none", false, run); len(got) != 1 {
		t.Errorf("auf Blech sollte erfasst werden, bekam %+v", got)
	}
}
