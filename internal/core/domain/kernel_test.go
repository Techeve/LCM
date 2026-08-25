package domain

import "testing"

// TestKernelReleaseFromPackage prueft die Ableitung der Kernel-Fassung aus
// echten Paketnamen der unterstuetzten Distributionen - inklusive der
// META-Pakete, die KEINEN konkreten Kernel installieren und deshalb leer
// zurueckkommen muessen.
func TestKernelReleaseFromPackage(t *testing.T) {
	cases := map[string]string{
		// Proxmox: aktuelle und alte Namensgebung, auch signiert.
		"proxmox-kernel-6.8.12-4-pve":        "6.8.12-4-pve",
		"proxmox-kernel-6.8.12-4-pve-signed": "6.8.12-4-pve",
		"pve-kernel-5.15.108-1-pve":          "5.15.108-1-pve",
		// Debian/Ubuntu.
		"linux-image-6.1.0-13-amd64":    "6.1.0-13-amd64",
		"linux-image-5.15.0-91-generic": "5.15.0-91-generic",
		// META-Pakete: zeigen nur auf den neuesten Kernel, installieren keinen.
		"linux-image-amd64":   "",
		"linux-image-generic": "",
		"proxmox-kernel-6.8":  "",
		"pve-kernel-5.15":     "",
		"linux":               "",
		"kernel":              "",
	}
	for name, want := range cases {
		if got := KernelReleaseFromPackage(name); got != want {
			t.Errorf("KernelReleaseFromPackage(%q) = %q, erwartet %q", name, got, want)
		}
	}
}

// TestBuildKernelInfoProxmox ist der vom Anwender genannte Hauptfall: mehrere
// Proxmox-Kernel installiert, genau einer laeuft. Die Liste muss den
// laufenden markieren und die neueste Fassung zuerst zeigen.
func TestBuildKernelInfoProxmox(t *testing.T) {
	pkgs := []KernelPackage{
		{Name: "proxmox-kernel-6.8.4-2-pve", Version: "6.8.4-2"},
		{Name: "proxmox-kernel-6.8.12-4-pve", Version: "6.8.12-4"},
		{Name: "pve-kernel-5.15.108-1-pve", Version: "5.15.108-1"},
	}
	info := BuildKernelInfo("6.8.4-2-pve", "none", pkgs)

	if !info.Managed {
		t.Error("ein Proxmox-Host ist kein Container - Managed muss true sein")
	}
	if len(info.Installed) != 3 {
		t.Fatalf("erwartete 3 Kernel, bekam %d", len(info.Installed))
	}
	// Neueste zuerst - 6.8.12 vor 6.8.4 (ein String-Vergleich haette 6.8.12
	// faelschlich fuer kleiner gehalten).
	want := []string{"6.8.12-4-pve", "6.8.4-2-pve", "5.15.108-1-pve"}
	for i, w := range want {
		if info.Installed[i].Release != w {
			t.Errorf("Position %d: %q, erwartet %q", i, info.Installed[i].Release, w)
		}
	}
	// Genau EINER ist markiert, und zwar der laufende.
	running := 0
	for _, p := range info.Installed {
		if p.Running {
			running++
			if p.Release != "6.8.4-2-pve" {
				t.Errorf("falscher Kernel als laufend markiert: %q", p.Release)
			}
		}
	}
	if running != 1 {
		t.Errorf("erwartete genau 1 laufenden Kernel, bekam %d", running)
	}
	// Es ist ein neuerer installiert als der laufende → Neustart steht aus.
	if !info.RebootPending {
		t.Error("neuerer Kernel installiert, aber RebootPending ist false")
	}
}

// TestBuildKernelInfoNeuesterLaeuft: Laeuft bereits der neueste Kernel, darf
// KEIN Neustart-Hinweis entstehen - sonst waere er Dauerzustand.
func TestBuildKernelInfoNeuesterLaeuft(t *testing.T) {
	pkgs := []KernelPackage{
		{Name: "proxmox-kernel-6.8.12-4-pve"},
		{Name: "proxmox-kernel-6.8.4-2-pve"},
	}
	info := BuildKernelInfo("6.8.12-4-pve", "none", pkgs)
	if info.RebootPending {
		t.Error("der neueste Kernel laeuft - RebootPending muss false sein")
	}
	if !info.Installed[0].Running {
		t.Error("der erste (neueste) Kernel sollte als laufend markiert sein")
	}
}

// TestBuildKernelInfoContainer ist die ausdrueckliche Vorgabe: In einem LXC
// laeuft der Kernel des HOSTS. Installierte Kernel-Pakete waeren dort
// wirkungslos - sie zu listen wuerde einen Handlungsspielraum vorspiegeln,
// den es nicht gibt. Nur die Version wird gezeigt.
func TestBuildKernelInfoContainer(t *testing.T) {
	pkgs := []KernelPackage{{Name: "linux-image-5.15.0-91-generic"}}
	for _, virt := range []string{"lxc", "LXC", "lxc-libvirt", "docker", "openvz", "systemd-nspawn"} {
		info := BuildKernelInfo("5.15.0-91-generic", virt, pkgs)
		if info.Managed {
			t.Errorf("%s: Managed muss false sein (Kernel kommt vom Host)", virt)
		}
		if len(info.Installed) != 0 {
			t.Errorf("%s: Kernel-Liste muss leer bleiben, bekam %d", virt, len(info.Installed))
		}
		if info.RebootPending {
			t.Errorf("%s: ohne eigenen Kernel gibt es nichts neu zu starten", virt)
		}
		if info.Running != "5.15.0-91-generic" {
			t.Errorf("%s: die Version muss trotzdem sichtbar bleiben, bekam %q", virt, info.Running)
		}
		if info.Container == "" {
			t.Errorf("%s: Container-Typ sollte festgehalten werden", virt)
		}
	}
	// Volle VMs und Blech haben einen EIGENEN Kernel - dort gilt das nicht.
	for _, virt := range []string{"none", "kvm", "qemu", "vmware", "microsoft", ""} {
		if info := BuildKernelInfo("6.1.0-13-amd64", virt, pkgs); !info.Managed {
			t.Errorf("%q sollte als verwaltbar gelten", virt)
		}
	}
}

// TestBuildKernelInfoRPMVersionsvergleich: Bei der RPM-Familie steckt die
// Fassung in der Paket-VERSION, nicht im Namen - die Markierung muss auch
// darueber greifen.
func TestBuildKernelInfoRPM(t *testing.T) {
	pkgs := []KernelPackage{
		{Name: "kernel-5.14.0-362.el9.x86_64", Version: "5.14.0-362.el9.x86_64"},
		{Name: "kernel-5.14.0-284.el9.x86_64", Version: "5.14.0-284.el9.x86_64"},
	}
	info := BuildKernelInfo("5.14.0-362.el9.x86_64", "kvm", pkgs)
	if !info.Installed[0].Running {
		t.Errorf("laufender RPM-Kernel nicht erkannt: %+v", info.Installed)
	}
	if info.RebootPending {
		t.Error("der neueste laeuft - kein Neustart noetig")
	}
}

// TestBuildKernelInfoFremdKernel: Laeuft ein Kernel, der in keinem Paket
// steckt (Eigenbau, Fremdquelle), darf LCM keinen Neustart behaupten - die
// Aussage „ein neuerer ist installiert" haette dann keine Grundlage.
func TestBuildKernelInfoFremdKernel(t *testing.T) {
	pkgs := []KernelPackage{{Name: "linux-image-6.1.0-13-amd64"}}
	info := BuildKernelInfo("6.9.3-eigenbau", "none", pkgs)
	if info.RebootPending {
		t.Error("unbekannter laufender Kernel: RebootPending darf nicht gesetzt sein")
	}
	if info.Running != "6.9.3-eigenbau" {
		t.Errorf("laufender Kernel ging verloren: %q", info.Running)
	}
}

// TestKernelPackagesRoundTrip: Die Liste wird als JSON-Spalte gespeichert;
// eine kaputte Spalte darf die Detailansicht nicht kippen.
func TestKernelPackagesRoundTrip(t *testing.T) {
	pkgs := []KernelPackage{{Name: "proxmox-kernel-6.8.12-4-pve", Release: "6.8.12-4-pve"}}
	raw := MarshalKernelPackages(pkgs)
	back := ParseKernelPackages(raw)
	if len(back) != 1 || back[0].Name != pkgs[0].Name {
		t.Errorf("Rundlauf fehlgeschlagen: %q -> %+v", raw, back)
	}
	if MarshalKernelPackages(nil) != "" {
		t.Error("leere Liste sollte eine leere Spalte ergeben")
	}
	for _, broken := range []string{"", "   ", "kein json", "{}"} {
		if got := ParseKernelPackages(broken); got != nil {
			t.Errorf("unlesbare Spalte %q sollte nil liefern, bekam %+v", broken, got)
		}
	}
}
