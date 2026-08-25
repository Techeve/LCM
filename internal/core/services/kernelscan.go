package services

import (
	"strings"

	"LCM/internal/core/domain"
)

// Erkennung der installierten Kernel-Pakete je Paketverwaltung.
//
// Der LAUFENDE Kernel kommt aus `uname -r` (server_scan.go) - die einzige
// Quelle, die nicht luegen kann. Hier geht es um die zweite Haelfte: welche
// Kernel ueberhaupt installiert sind. Erst beides zusammen beantwortet die
// betrieblich wichtigen Fragen: Laeuft der neueste? Gibt es noch eine
// Rueckfallebene, wenn er nicht bootet?

// kernelListCommand liefert das Kommando, das je Paketverwaltung die
// installierten Kernel-Pakete zeilenweise als "name version" ausgibt.
//
// Proxmox bekommt einen eigenen Zweig, weil dort NICHT linux-image die Kernel
// stellt, sondern proxmox-kernel-* (frueher pve-kernel-*). Ein reiner
// linux-image-Filter faende auf einem PVE-Host schlicht nichts.
func kernelListCommand(mgr string, proxmox bool) string {
	switch pkgFamily(mgr) {
	case pkgApt:
		// db:Status-Abbrev ist die zweistellige Statusspalte von dpkg; "ii"
		// heisst installiert und konfiguriert (das `grep ^ii` von Hand).
		filter := `$2 ~ /^linux-image-/`
		if proxmox {
			filter = `$2 ~ /^(proxmox-kernel|pve-kernel|linux-image)-/`
		}
		return `dpkg-query -W -f '${db:Status-Abbrev} ${Package} ${Version}\n' 2>/dev/null | ` +
			`awk '$1 ~ /^ii/ && ` + filter + ` {print $2" "$3}'`
	case pkgDnf:
		return `rpm -qa --qf '%{NAME}-%{VERSION}-%{RELEASE}.%{ARCH} %{VERSION}-%{RELEASE}.%{ARCH}\n' ` +
			`kernel kernel-core 2>/dev/null | sort -u`
	case pkgZypper:
		return `rpm -qa --qf '%{NAME}-%{VERSION}-%{RELEASE}.%{ARCH} %{VERSION}-%{RELEASE}\n' ` +
			`'kernel-default*' 2>/dev/null | sort -u`
	case pkgPacman:
		return `pacman -Q linux linux-lts linux-hardened linux-zen 2>/dev/null`
	case pkgApk:
		return `apk info -v 2>/dev/null | grep -E '^linux-(lts|virt|edge)' || true`
	}
	return ""
}

// kernelReleaseFromVersionOnly meldet, ob die Kernel-Fassung bei dieser
// Paketverwaltung nur in der Version steht (nicht im Paketnamen).
func kernelReleaseFromVersionOnly(mgr string) bool {
	switch pkgFamily(mgr) {
	case pkgPacman, pkgApk:
		return true
	}
	return false
}

// parseKernelList wandelt die Ausgabe in Kernel-Pakete.
//
// META-Pakete werden ausgelassen: linux-image-amd64 oder proxmox-kernel-6.8
// installieren keinen konkreten Kernel, sie zeigen nur auf den jeweils
// neuesten. Sie in der Liste zu fuehren wuerde die Zahl der tatsaechlich
// vorhandenen Rueckfall-Kernel verfaelschen. Erkennungsmerkmal ist die
// fehlende dreiteilige Version im Namen.
func parseKernelList(mgr, out string) []domain.KernelPackage {
	var pkgs []domain.KernelPackage
	seen := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) == 0 {
			continue
		}
		name := fields[0]
		version := ""
		if len(fields) > 1 {
			version = fields[1]
		}
		release := domain.KernelReleaseFromPackage(name)
		if release == "" {
			// Nur pacman und apk fuehren die Fassung ausschliesslich in der
			// Version ("linux 6.9.1.arch1-1") - dort ist ein Name ohne Version
			// normal und die Version die richtige Quelle.
			//
			// Bei apt/dnf/zypper waere derselbe Rueckgriff falsch: Dort steckt
			// die Fassung IMMER im Namen, und ein Name ohne sie ist genau das
			// Kennzeichen eines META-Pakets (linux-image-amd64,
			// proxmox-kernel-6.8). Deren Version zeigt auf den neuesten Kernel
			// - der Rueckgriff haette sie also als eigenstaendigen Kernel
			// gezaehlt und die Zahl der Rueckfall-Kernel verfaelscht.
			if !kernelReleaseFromVersionOnly(mgr) {
				continue
			}
			release = domain.KernelReleaseFromPackage(version)
			if release == "" {
				continue
			}
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		pkgs = append(pkgs, domain.KernelPackage{Name: name, Release: release, Version: version})
	}
	return pkgs
}

// scanKernels liest die installierten Kernel-Pakete vom Ziel.
//
// In Containern wird gar nicht erst gefragt: Dort laeuft der Kernel des Hosts,
// installierte Kernel-Pakete waeren wirkungslos. Ein Kommando abzusetzen und
// das Ergebnis dann zu verwerfen waere nur verschwendete Zeit auf einer
// fremden Maschine.
func scanKernels(mgr, virt string, proxmox bool, run func(string, string) string) []domain.KernelPackage {
	if domain.IsContainerVirt(virt) {
		return nil
	}
	cmd := kernelListCommand(mgr, proxmox)
	if cmd == "" {
		return nil
	}
	return parseKernelList(mgr, run("kernels", cmd))
}
