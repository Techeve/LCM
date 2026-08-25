package services

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

// Alte Kernel entfernen.
//
// Jedes Kernel-Update legt einen weiteren Kernel neben die vorhandenen. Das
// ist gewollt - der vorige ist die Rueckfallebene, wenn der neue nicht bootet.
// Nach einem Jahr Updates liegen davon aber ein Dutzend in /boot, und genau
// diese Partition ist auf vielen Installationen klein. Volles /boot heisst:
// Das naechste Kernel-Update bricht ab, und zwar mitten im dpkg-Lauf.
//
// Was stehen bleibt, entscheidet domain.RemovableKernels: der laufende
// Kernel, alles Neuere und der naechstaeltere als Rueckfallebene.

var (
	// ErrNoOldKernels: Es gibt nichts zu entfernen.
	ErrNoOldKernels = errors.New("keine alten kernel zum entfernen - es bleiben ohnehin nur der laufende, neuere und die rückfallebene")
	// ErrKernelCleanupUnsupported: Der Aufraeum-Lauf ist bisher nur fuer
	// Debian/Ubuntu/Proxmox gebaut. Ein halb passendes Kommando auf einer
	// fremden Paketverwaltung waere schlimmer als keins.
	ErrKernelCleanupUnsupported = errors.New("alte kernel entfernen ist derzeit nur auf apt-systemen (debian, ubuntu, proxmox) möglich")
)

// reKernelReleaseSafe laesst nur Zeichen zu, die in einer Kernel-Fassung
// vorkommen. Die Werte gehen in ein Shell-Skript - was hier durchkommt, ist
// harmlos.
var reKernelReleaseSafe = regexp.MustCompile(`^[0-9][A-Za-z0-9._+-]*$`)

// RemoveOldKernels entfernt die nicht mehr benoetigten Kernel eines Servers
// samt der zu ihnen gehoerenden Pakete (Module, Header, Tools).
func (s *ServerService) RemoveOldKernels(scope repositories.AccessScope, id uint, actor string) (*domain.Job, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return nil, err
	}
	if err := ensureNotRouterOS(server); err != nil {
		return nil, err
	}
	// Kernel-Pakete stehen auf der Schutzliste der Paketverwaltung
	// (protectedPackagePrefixes) - mit gutem Grund: Ein von Hand entferntes
	// linux-image macht die Maschine unbootbar. Diese Aktion darf sie
	// anfassen, weil SIE die Auswahl trifft und den laufenden Kernel niemals
	// enthaelt. Volles sudo ist dafuer Voraussetzung.
	if err := ensureFullSudo(server); err != nil {
		return nil, err
	}
	if pkgFamily(server.PackageManager) != pkgApt {
		return nil, ErrKernelCleanupUnsupported
	}

	info := domain.BuildKernelInfoFor(server.KernelVersion, server.Virtualization,
		domain.ParseKernelPackages(server.InstalledKernels), !server.IsAPIDevice())
	releases, err := cleanupReleases(info)
	if err != nil {
		return nil, err
	}

	script := removeKernelsScript(releases)
	name := fmt.Sprintf("Alte Kernel entfernen (%d)", len(releases))
	job, err := s.startPackageJob(scope, id, domain.RuleTypePackages, name,
		func(string) string { return script }, actor)
	if err != nil {
		return nil, err
	}
	s.audit.Log(actor, "server.kernel-cleanup", "server", id, strings.Join(releases, ", "))
	return job, nil
}

// cleanupReleases zieht die zu entfernenden Kernel-Fassungen aus dem Inventar
// und prueft sie.
func cleanupReleases(info domain.KernelInfo) ([]string, error) {
	if !info.Managed {
		return nil, ErrKernelCleanupUnsupported
	}
	releases := make([]string, 0, len(info.Removable))
	for _, k := range info.Removable {
		release := strings.TrimSpace(k.Release)
		if release == "" || !reKernelReleaseSafe.MatchString(release) {
			continue
		}
		releases = append(releases, release)
	}
	if len(releases) == 0 {
		return nil, ErrNoOldKernels
	}
	return releases, nil
}

// removeKernelsScript baut den Entfernungs-Lauf.
//
// Die Paketnamen werden NICHT geraten, sondern auf dem Zielsystem gesucht:
// Je nach Distribution heissen die Begleitpakete anders (linux-modules-…,
// linux-headers-…, proxmox-headers-…), und eine hier gepflegte Liste waere
// schon bei der naechsten Ubuntu-Fassung unvollstaendig.
//
// Zwei Sicherungen sitzen im Skript selbst, nicht nur in der Auswahl davor:
// Der laufende Kernel wird ueber `uname -r` erneut ausgeschlossen, und ein
// Lauf ohne Treffer bricht nicht ab, sondern sagt es. Die Auswahl stammt aus
// dem zuletzt erfassten Inventar - zwischen Erfassung und Ausfuehrung kann
// ein Neustart den laufenden Kernel gewechselt haben.
func removeKernelsScript(releases []string) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	b.WriteString("RUNNING=$(uname -r)\n")
	b.WriteString("echo \"Laufender Kernel: $RUNNING (bleibt)\"\n")
	b.WriteString("REL_LIST='" + strings.Join(releases, " ") + "'\n")
	b.WriteString("TARGETS=\n")
	b.WriteString("for REL in $REL_LIST; do\n")
	b.WriteString("  if [ \"$REL\" = \"$RUNNING\" ]; then echo \"übersprungen: $REL ist der laufende Kernel\"; continue; fi\n")
	// BASE ist die Fassung ohne den Geschmacksrichtungs-Anhang
	// (6.8.0-40-generic -> 6.8.0-40). Ubuntu legt Header und Tools unter
	// diesem kuerzeren Namen ab.
	b.WriteString("  BASE=${REL%-*}\n")
	b.WriteString("  FOUND=$(dpkg-query -W -f '${db:Status-Abbrev} ${Package}\\n' 2>/dev/null | " +
		"awk -v r=\"$REL\" -v b=\"$BASE\" '$1 ~ /^ii/ && $2 ~ /^(linux|proxmox|pve)-/ && " +
		"(index($2, r) || $2 == \"linux-headers-\" b || $2 == \"linux-tools-\" b || $2 == \"linux-buildinfo-\" b) {print $2}')\n")
	b.WriteString("  if [ -z \"$FOUND\" ]; then echo \"nichts installiert für $REL\"; continue; fi\n")
	b.WriteString("  TARGETS=\"$TARGETS $FOUND\"\n")
	b.WriteString("done\n")
	// Letzte Sicherung: Was den laufenden Kernel im Namen traegt, wird
	// aussortiert - komme es, woher es wolle.
	b.WriteString("TARGETS=$(printf '%s\\n' $TARGETS | grep -v -F \"$RUNNING\" | sort -u || true)\n")
	b.WriteString("if [ -z \"$TARGETS\" ]; then echo 'Nichts zu entfernen.'; exit 0; fi\n")
	b.WriteString("echo 'Wird entfernt:'; printf '  %s\\n' $TARGETS\n")
	b.WriteString(aptNonInteractive + " -y purge $TARGETS\n")
	b.WriteString("df -h /boot 2>/dev/null || true\n")
	return b.String()
}
