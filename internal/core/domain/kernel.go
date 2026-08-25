package domain

import (
	"encoding/json"
	"regexp"
	"strings"
)

// Kernel-Inventar: welcher Kernel LAEUFT und welche installiert sind.
//
// Die beiden Angaben sind nicht dasselbe, und genau diese Differenz ist der
// Punkt: Nach einem Kernel-Update ist der neue Kernel installiert, laeuft aber
// erst nach einem Neustart. Bis dahin arbeitet die Maschine weiter mit dem
// alten - auch wenn die Paketliste behauptet, alles sei aktuell. Umgekehrt
// sind die zusaetzlich installierten (aelteren) Kernel die Rueckfallebene,
// wenn ein neuer nicht bootet; sie sollen sichtbar sein, damit man weiss, ob
// es sie ueberhaupt noch gibt (siehe auch die Paket-Pins).
//
// Der laufende Kernel kommt aus `uname -r` - das ist die einzige Quelle, die
// nicht luegen kann: Sie liest den tatsaechlich gebooteten Kernel, nicht das,
// was die Paketverwaltung vorhaelt.

// KernelPackage ist ein installiertes Kernel-Paket.
type KernelPackage struct {
	// Name des Pakets, z.B. "proxmox-kernel-6.8.12-4-pve" oder
	// "linux-image-6.1.0-13-amd64".
	Name string `json:"name"`
	// Release ist die Kernel-Fassung, wie `uname -r` sie meldet
	// (z.B. "6.8.12-4-pve") - leer, wenn sie sich nicht ableiten liess.
	Release string `json:"release,omitempty"`
	// Version ist die Paket-Version der Paketverwaltung.
	Version string `json:"version,omitempty"`
	// Running: genau dieses Paket stellt den gerade laufenden Kernel.
	Running bool `json:"running"`
}

// KernelInfo ist die Gesamtsicht fuer Oberflaeche und API.
type KernelInfo struct {
	// Running ist die Ausgabe von `uname -r` - der tatsaechlich gebootete
	// Kernel. Immer gefuellt, sofern der Server ueberhaupt erreichbar war.
	Running string `json:"running"`
	// Installed sind die installierten Kernel-Pakete, neueste zuerst.
	// In Containern bewusst LEER (siehe Managed).
	Installed []KernelPackage `json:"installed"`
	// Managed meldet, ob der Kernel auf diesem System ueberhaupt verwaltbar
	// ist. In einem Container (LXC, Docker, …) ist er es NICHT: Dort laeuft
	// der Kernel des Hosts, und `uname -r` zeigt genau diesen. Installierte
	// Kernel-Pakete waeren dort wirkungslos - sie zu listen wuerde einen
	// Handlungsspielraum vorspiegeln, den es nicht gibt.
	Managed bool `json:"managed"`
	// Container haelt den erkannten Container-Typ fest (lxc, docker, …) -
	// Grundlage des Hinweises in der Oberflaeche.
	Container string `json:"container,omitempty"`
	// RebootPending: es ist ein neuerer Kernel installiert als der laufende.
	// Abgeleitet aus dem Inventar - unabhaengig davon, ob needrestart auf dem
	// System vorhanden ist.
	RebootPending bool `json:"reboot_pending"`
	// Removable sind die Kernel, die entfernt werden koennen, ohne die
	// Betriebsfaehigkeit anzutasten: alles unterhalb des laufenden Kernels
	// AUSSER dem naechstaelteren, der als Rueckfallebene stehen bleibt.
	// Siehe RemovableKernels.
	Removable []KernelPackage `json:"removable"`
}

// containerVirtTypes sind die systemd-detect-virt-Werte, die einen Container
// bezeichnen (im Gegensatz zu einer vollen VM mit eigenem Kernel).
var containerVirtTypes = map[string]bool{
	"lxc": true, "lxc-libvirt": true, "openvz": true, "docker": true,
	"podman": true, "systemd-nspawn": true, "rkt": true, "wsl": true, "proot": true,
}

// IsContainerVirt meldet, ob der Virtualisierungstyp einen Container
// bezeichnet - dann kommt der Kernel vom Host.
func IsContainerVirt(virt string) bool {
	return containerVirtTypes[strings.ToLower(strings.TrimSpace(virt))]
}

// reKernelRelease findet die Kernel-Fassung in einem Paketnamen: alles ab der
// ersten Versionsziffer. Aus "proxmox-kernel-6.8.12-4-pve" wird
// "6.8.12-4-pve", aus "linux-image-6.1.0-13-amd64" wird "6.1.0-13-amd64".
var reKernelRelease = regexp.MustCompile(`[0-9]+\.[0-9]+\.[0-9]+.*$`)

// KernelReleaseFromPackage leitet die Kernel-Fassung aus dem Paketnamen ab.
// Liefert "", wenn keine dreiteilige Version enthalten ist - das trifft die
// META-Pakete (linux-image-amd64, proxmox-kernel-6.8), die keinen konkreten
// Kernel installieren, sondern nur auf den jeweils neuesten zeigen.
func KernelReleaseFromPackage(name string) string {
	rel := reKernelRelease.FindString(name)
	// Signierte Proxmox-Pakete tragen ein Suffix, das `uname -r` nicht kennt.
	return strings.TrimSuffix(rel, "-signed")
}

// matchesRunning meldet, ob dieses Paket den laufenden Kernel stellt.
//
// Der Vergleich laeuft ueber die Kernel-Fassung, nicht ueber den Paketnamen:
// Debian steckt sie in den Namen (linux-image-6.1.0-13-amd64), die
// RPM-Familie in die Paket-Version (kernel 5.14.0-362.el9.x86_64). Beide Wege
// werden geprueft, damit die Markierung distributionsuebergreifend stimmt.
func (p KernelPackage) matchesRunning(running string) bool {
	running = strings.TrimSpace(running)
	if running == "" {
		return false
	}
	if p.Release != "" && p.Release == running {
		return true
	}
	if strings.Contains(p.Name, running) {
		return true
	}
	// RPM: `uname -r` enthaelt die Architektur (5.14.0-362.el9.x86_64), die
	// Paket-Version je nach Abfrage nicht - beide Richtungen zulassen.
	if p.Version != "" && (p.Version == running || strings.HasPrefix(running, p.Version)) {
		return true
	}
	return false
}

// BuildKernelInfo baut die Gesamtsicht aus den Rohdaten des Scans.
//
// running ist `uname -r`, packages die erkannten Kernel-Pakete, virt der
// Virtualisierungstyp. In Containern wird die Paketliste bewusst VERWORFEN:
// Sie beschreibt dort nichts, was auf dem System wirkt.
func BuildKernelInfo(running, virt string, packages []KernelPackage) KernelInfo {
	return BuildKernelInfoFor(running, virt, packages, true)
}

// BuildKernelInfoFor ist die Variante mit ausdrücklicher Angabe, ob der
// Kernel auf diesem Gerät überhaupt LCM-verwaltbar ist. Auf reinen
// API-Geräten (RouterOS, Synology DSM) ist er es nicht: dort gibt es weder
// `uname -r` über eine Shell noch Kernel-Pakete - „verwaltbar" zu melden
// spiegelte einen Handlungsspielraum vor, den es nicht gibt.
func BuildKernelInfoFor(running, virt string, packages []KernelPackage, manageable bool) KernelInfo {
	info := KernelInfo{
		Running: strings.TrimSpace(running),
		Managed: manageable && !IsContainerVirt(virt),
	}
	if !info.Managed {
		info.Container = strings.ToLower(strings.TrimSpace(virt))
		info.Installed = []KernelPackage{}
		return info
	}
	info.Installed = make([]KernelPackage, 0, len(packages))
	for _, p := range packages {
		if p.Release == "" {
			p.Release = KernelReleaseFromPackage(p.Name)
		}
		p.Running = p.matchesRunning(info.Running)
		info.Installed = append(info.Installed, p)
	}
	SortKernelPackages(info.Installed)
	// Ein Neustart steht an, wenn ein installierter Kernel NEUER ist als der
	// laufende. Bewusst nur, wenn der laufende Kernel ueberhaupt in der Liste
	// auftaucht: Steht er nicht drin (Fremd-Kernel, eigenes Build), waere
	// „neuer vorhanden" eine Behauptung ohne Grundlage.
	if runningIdx := indexOfRunning(info.Installed); runningIdx > 0 {
		info.RebootPending = true
	}
	info.Removable = RemovableKernels(info.Installed)
	return info
}

// RemovableKernels waehlt die Kernel aus, die weg koennen.
//
// Behalten wird:
//
//   - der LAUFENDE Kernel - ihn zu entfernen macht das System beim naechsten
//     Start unbootbar;
//   - alles, was NEUER ist als er - das ist der Kernel, den der naechste
//     Neustart aktiviert;
//   - der naechstaeltere als RUECKFALLEBENE - genau dafuer sind mehrere
//     Kernel da: Bootet der neue nicht, ist das der Weg zurueck.
//
// Alles darunter ist Ballast: Es belegt /boot, wird nie gebootet und taucht
// nur noch im Bootmenue auf.
//
// Ist der laufende Kernel nicht im Inventar (Fremd-Kernel, eigenes Build),
// wird NICHTS zum Entfernen vorgeschlagen: Ohne den Bezugspunkt liesse sich
// „aelter als der laufende" nicht bestimmen, und geraten wird hier nicht.
func RemovableKernels(installed []KernelPackage) []KernelPackage {
	runningIdx := indexOfRunning(installed)
	if runningIdx < 0 {
		return nil
	}
	// Liste ist absteigend sortiert: Der Rueckfall-Kernel steht direkt hinter
	// dem laufenden, alles danach kann weg.
	first := runningIdx + 2
	if first >= len(installed) {
		return nil
	}
	out := make([]KernelPackage, 0, len(installed)-first)
	out = append(out, installed[first:]...)
	return out
}

// indexOfRunning liefert die Position des laufenden Kernels in der bereits
// sortierten Liste (neueste zuerst); -1, wenn er nicht enthalten ist.
func indexOfRunning(pkgs []KernelPackage) int {
	for i, p := range pkgs {
		if p.Running {
			return i
		}
	}
	return -1
}

// SortKernelPackages sortiert absteigend nach Kernel-Fassung (neueste zuerst).
func SortKernelPackages(pkgs []KernelPackage) {
	// Einfaches Insertion-Sort: Die Listen sind kurz (selten mehr als eine
	// Handvoll Kernel), dafuer bleibt der Vergleich hier lesbar.
	for i := 1; i < len(pkgs); i++ {
		for j := i; j > 0 && kernelReleaseLess(pkgs[j-1].sortKey(), pkgs[j].sortKey()); j-- {
			pkgs[j-1], pkgs[j] = pkgs[j], pkgs[j-1]
		}
	}
}

// sortKey ist der Wert, nach dem sortiert wird: die Kernel-Fassung, ersatzweise
// die Paket-Version.
func (p KernelPackage) sortKey() string {
	if p.Release != "" {
		return p.Release
	}
	return p.Version
}

// reNum trennt Zahlengruppen von allem anderen.
var reNum = regexp.MustCompile(`[0-9]+|[^0-9]+`)

// kernelReleaseLess vergleicht zwei Kernel-Fassungen natuerlich: Zahlengruppen
// numerisch, alles andere alphabetisch. Ein reiner String-Vergleich hielte
// "6.8.12" faelschlich fuer kleiner als "6.8.9".
func kernelReleaseLess(a, b string) bool {
	pa, pb := reNum.FindAllString(a, -1), reNum.FindAllString(b, -1)
	for i := 0; i < len(pa) && i < len(pb); i++ {
		x, y := pa[i], pb[i]
		if x == y {
			continue
		}
		nx, okx := atoiOK(x)
		ny, oky := atoiOK(y)
		if okx && oky {
			return nx < ny
		}
		return x < y
	}
	return len(pa) < len(pb)
}

// atoiOK wandelt eine reine Ziffernfolge; ok=false bei allem anderen.
func atoiOK(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = n*10 + int(r-'0')
	}
	return n, true
}

// MarshalKernelPackages serialisiert die Liste fuer die JSON-Spalte am Server.
func MarshalKernelPackages(pkgs []KernelPackage) string {
	if len(pkgs) == 0 {
		return ""
	}
	b, err := json.Marshal(pkgs)
	if err != nil {
		return ""
	}
	return string(b)
}

// ParseKernelPackages liest die JSON-Spalte zurueck. Unlesbares gilt als
// „nichts erfasst" - eine kaputte Spalte darf die Detailansicht nicht kippen.
func ParseKernelPackages(raw string) []KernelPackage {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var pkgs []KernelPackage
	if err := json.Unmarshal([]byte(raw), &pkgs); err != nil {
		return nil
	}
	return pkgs
}
