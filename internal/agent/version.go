package agent

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"LCM/internal/remote/wire"
)

// VersionInfo ist die Ausgabe von `lcm-agent version --json`: was das
// Update-Skript des Servers und der Support über diesen Agenten wissen wollen.
type VersionInfo struct {
	Version string `json:"version"`
	// FullVersion ist die Version samt Build, wie das Inventar sie meldet.
	FullVersion  string   `json:"full_version"`
	Protocol     int      `json:"protocol"`
	Capabilities []string `json:"capabilities"`
	// Pinned: Der Paketmanager lässt den Agenten bei normalen Upgrades in Ruhe.
	// PinnedVersion ist die Version, die gehalten wird (leer, wenn der Mechanismus
	// keine nennt, etwa pacman), PinSource der Mechanismus.
	Pinned        bool         `json:"pinned"`
	PinnedVersion string       `json:"pinned_version,omitempty"`
	PinSource     string       `json:"pin_source,omitempty"`
	Rollback      RollbackInfo `json:"rollback"`
}

// RollbackInfo beschreibt den Rückfall auf das vorige Binary.
type RollbackInfo struct {
	// Guard: Das Startskript des Pakets ist installiert - nur dann gibt es einen Rückfall.
	Guard bool `json:"guard"`
	// PrevBinary: Es liegt ein vorheriges Binary bereit, PrevVersion nennt dessen
	// Version (leer, wenn es nicht antwortet).
	PrevBinary  bool   `json:"prev_binary"`
	PrevVersion string `json:"prev_version,omitempty"`
	// Mode ist „normal" oder „prev" (das vorige Binary läuft).
	Mode string `json:"mode"`
	// Starts sind die Starts in Folge ohne „gesund" (siehe update.go).
	Starts int `json:"starts"`
	// RolledBack ist gesetzt, solange das vorige Binary nach einem gescheiterten
	// Update läuft.
	RolledBack *RolledBack `json:"rolled_back,omitempty"`
}

// Pfade, aus denen der Pin gelesen wird (relativ zu root; Tests setzen eine Attrappe).
const (
	aptPinFile      = "etc/apt/preferences.d/lcm-agent"
	apkWorldFile    = "etc/apk/world"
	pacmanConfFile  = "etc/pacman.conf"
	rpmVersionLock  = "etc/dnf/plugins/versionlock.list"
	yumVersionLock  = "etc/yum/pluginconf.d/versionlock.list"
	dnf5VersionLock = "etc/dnf/versionlock.toml"
	dnfRepoFile     = "etc/yum.repos.d/techeve-lcm.repo"
	zypperLocksFile = "etc/zypp/locks"
	prevVersionWait = 3 * time.Second
)

// CollectVersionInfo sammelt die Angaben. root ist "" im Betrieb.
func CollectVersionInfo(root, version, fullVersion string) VersionInfo {
	info := VersionInfo{
		Version:     version,
		FullVersion: fullVersion,
		Protocol:    wire.ProtocolVersion,
		Capabilities: agentCapabilities(
			ttyCapabilities(TerminalDisabled(filepath.Join(root, ConfigDir))), guardInstalledAt(root)),
	}
	info.Pinned, info.PinnedVersion, info.PinSource = readPin(root)

	state := updateState{dir: UpdateDir}
	info.Rollback = RollbackInfo{
		Guard:      guardInstalledAt(root),
		Mode:       state.mode(),
		Starts:     state.starts(),
		RolledBack: state.rolledBack(),
	}
	if _, err := os.Stat(state.prevPath()); err == nil {
		info.Rollback.PrevBinary = true
		info.Rollback.PrevVersion = binaryVersion(state.prevPath())
	}
	return info
}

func ttyCapabilities(disabled bool) []string {
	if disabled {
		return []string{wire.CapabilityTTYOff}
	}
	return []string{wire.CapabilityTTY}
}

func guardInstalledAt(root string) bool {
	fi, err := os.Stat(filepath.Join(root, guardScriptPath))
	return err == nil && fi.Mode().IsRegular() && fi.Mode()&0o111 != 0
}

// binaryVersion fragt ein Binary nach seiner Version („lcm-agent 2.4.0"); leer,
// wenn es nicht antwortet.
func binaryVersion(path string) string {
	ctx, cancel := context.WithTimeout(context.Background(), prevVersionWait)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "version").Output()
	if err != nil {
		return ""
	}
	fields := strings.Fields(string(out))
	if len(fields) < 2 {
		return ""
	}
	return fields[1]
}

// readPin erkennt, ob und wie der Paketmanager den Agenten hält - nur lesend.
//
//	apt     /etc/apt/preferences.d/lcm-agent (schreibt das Paket selbst, postinst)
//	apk     lcm-agent=<version> in /etc/apk/world (setzt `apk add lcm-agent=<version>`)
//	dnf     exclude=lcm-agent in der Repo-Datei techeve-lcm (Paketskript, setup.sh),
//	        Eintrag in der Liste des versionlock-Plugins (dnf4) oder in
//	        versionlock.toml (dnf5)
//	zypper  Paketschloss lcm-agent in /etc/zypp/locks
//	pacman  IgnorePkg in /etc/pacman.conf
func readPin(root string) (pinned bool, version, source string) {
	if v, ok := aptPin(filepath.Join(root, aptPinFile)); ok {
		return true, v, "apt-preferences"
	}
	for _, line := range fileLines(filepath.Join(root, apkWorldFile)) {
		if v, ok := strings.CutPrefix(line, "lcm-agent="); ok {
			return true, v, "apk-world"
		}
	}
	for _, f := range []string{rpmVersionLock, yumVersionLock} {
		for _, line := range fileLines(filepath.Join(root, f)) {
			if strings.HasPrefix(line, "lcm-agent-") || strings.HasPrefix(line, "0:lcm-agent-") {
				return true, "", "versionlock"
			}
		}
	}
	if v, ok := dnf5Lock(filepath.Join(root, dnf5VersionLock)); ok {
		return true, v, "versionlock"
	}
	if dnfExclude(filepath.Join(root, dnfRepoFile)) {
		return true, "", "dnf-exclude"
	}
	for _, line := range fileLines(filepath.Join(root, zypperLocksFile)) {
		if line == "solvable_name: lcm-agent" {
			return true, "", "zypper-lock"
		}
	}
	for _, line := range fileLines(filepath.Join(root, pacmanConfFile)) {
		key, val, ok := strings.Cut(line, "=")
		if ok && strings.TrimSpace(key) == "IgnorePkg" && containsWord(val, "lcm-agent") {
			return true, "", "pacman-ignorepkg"
		}
	}
	return false, "", ""
}

func aptPin(path string) (string, bool) {
	pkg, ver := false, ""
	for _, line := range fileLines(path) {
		switch {
		case line == "Package: lcm-agent":
			pkg = true
		case strings.HasPrefix(line, "Pin: version "):
			ver = strings.TrimSpace(strings.TrimPrefix(line, "Pin: version "))
		}
	}
	return ver, pkg && ver != ""
}

// dnfExclude: Die Repo-Datei hält lcm-agent über exclude= oder excludepkgs=
// (Leerzeichen oder Komma getrennt).
func dnfExclude(path string) bool {
	for _, line := range fileLines(path) {
		key, val, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if ok && (key == "exclude" || key == "excludepkgs") && containsWord(strings.ReplaceAll(val, ",", " "), "lcm-agent") {
			return true
		}
	}
	return false
}

// dnf5Lock liest versionlock.toml von dnf5: ein Block [[packages]] mit
// name = "lcm-agent" und, wenn dnf die Version festhält, value = "<Version>-<Release>".
// Zurück kommt die Version ohne Release (leer, wenn der Block keine nennt).
func dnf5Lock(path string) (string, bool) {
	named, version := false, ""
	for _, line := range fileLines(path) {
		key, val, _ := strings.Cut(line, "=")
		val = strings.Trim(strings.TrimSpace(val), `"`)
		switch {
		case line == "[[packages]]":
			if named {
				return version, true
			}
			named, version = false, ""
		case strings.TrimSpace(key) == "name":
			named = val == "lcm-agent"
		case named && strings.TrimSpace(key) == "value":
			version = val
			if i := strings.LastIndex(val, "-"); i > 0 {
				version = val[:i]
			}
		}
	}
	return version, named
}

func containsWord(s, word string) bool {
	for _, w := range strings.Fields(s) {
		if w == word {
			return true
		}
	}
	return false
}

// fileLines liest die Zeilen einer Textdatei ohne Kommentare und Leerzeilen
// (leer, wenn sie fehlt).
func fileLines(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			lines = append(lines, line)
		}
	}
	return lines
}
