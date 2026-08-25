package services

import (
	"strconv"
	"strings"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/sshx"
)

// LCM Remote-Geräte des Typs MikroTik RouterOS.
//
// RouterOS ist KEIN Linux mit POSIX-Shell/Paketverwaltung, sondern eine
// eigene CLI, die einzelne Kommandos über SSH-Exec entgegennimmt. Deshalb
// laufen die RouterOS-Kommandos ROH (kein wrapSudo/sh -c) und der reguläre
// Linux-Scan (os-release, dpkg, ss, Docker …) entfällt vollständig. LCM
// überwacht hier ausschließlich die Basis-Identität und die Aktualität der
// RouterOS-Version.

// scanRouterOS führt den RouterOS-Scan über eine bestehende SSH-Verbindung
// aus. Die Kommandos werden ROH ausgeführt (RouterOS-CLI, keine Shell). Ein
// einzelnes fehlschlagendes Kommando bricht den Scan nicht ab.
func scanRouterOS(conn sshx.Conn) *scanResult {
	res := &scanResult{OSName: "MikroTik RouterOS", OSID: domain.OSIDRouterOS}
	var log strings.Builder

	run := func(cmd string) string {
		out, code, err := conn.Run(cmd)
		log.WriteString("$ " + cmd + "\n")
		if err != nil {
			log.WriteString("(transportfehler: " + err.Error() + ")\n")
			return ""
		}
		if code != 0 {
			log.WriteString(out + "(exit " + strconv.Itoa(code) + ")\n")
			return ""
		}
		log.WriteString(out)
		return out
	}

	// /system resource print: Version, Board, Architektur, CPU, RAM, Uptime.
	resource := parseRouterOSKV(run("/system resource print"))
	res.OSVersion, res.RouterOSChannel = parseRouterOSVersion(resource["version"])
	res.RouterBoardModel = firstNonEmpty(resource["board-name"], resource["platform"])
	res.KernelVersion = resource["architecture-name"] // z.B. "arm64"/"x86_64"
	res.CPUModel = resource["cpu"]
	res.CPUCores, _ = strconv.Atoi(strings.TrimSpace(resource["cpu-count"]))
	res.MemTotalMB = parseRouterOSMemMB(resource["total-memory"])
	if free := parseRouterOSMemMB(resource["free-memory"]); free > 0 && res.MemTotalMB > 0 {
		res.MemUsedMB = res.MemTotalMB - free
	}
	res.DiskTotalMB = parseRouterOSMemMB(resource["total-hdd-space"])
	if freeHDD := parseRouterOSMemMB(resource["free-hdd-space"]); freeHDD > 0 && res.DiskTotalMB > 0 {
		res.DiskUsedMB = res.DiskTotalMB - freeHDD
	}

	// RouterBOARD-Geräte melden ein konkretes Modell (CHR/x86 nicht) - genauer
	// als board-name, deshalb bevorzugt übernehmen, wenn vorhanden.
	board := parseRouterOSKV(run("/system routerboard print"))
	if m := strings.TrimSpace(board["model"]); m != "" {
		res.RouterBoardModel = m
	}

	// Aktualität: der Router fragt selbst bei MikroTik nach der neuesten Version
	// seines Kanals. installed-/latest-version + Kanal sind hier autoritativ.
	upd := parseRouterOSKV(run("/system package update check-for-updates"))
	installed := strings.TrimSpace(upd["installed-version"])
	latest := strings.TrimSpace(upd["latest-version"])
	if ch := strings.TrimSpace(upd["channel"]); ch != "" {
		res.RouterOSChannel = ch
	}
	if installed != "" {
		res.OSVersion = installed
	}
	if latest != "" {
		res.RouterOSLatestVersion = latest
		// Neuere Version verfügbar, wenn latest != installed. RouterOS meldet
		// bei aktuellem System latest == installed (bzw. leer).
		res.RouterOSUpdateAvailable = installed != "" && latest != installed
	}

	res.Output = log.String()
	return res
}

// parseRouterOSKV zerlegt die RouterOS-CLI-Ausgabe im Format "  key: value"
// (rechtsbündige Schlüssel, je Zeile ein Feld) in eine Map. Werte werden
// getrimmt; ein fehlender/leerer Schlüssel wird übersprungen.
func parseRouterOSKV(out string) map[string]string {
	kv := make(map[string]string)
	for _, line := range strings.Split(out, "\n") {
		i := strings.Index(line, ":")
		if i <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:i])
		val := strings.TrimSpace(line[i+1:])
		// Schlüssel können durch führende Spaces mehrere Wörter enthalten
		// (z.B. bei umgebrochenen Werten) - nur echte Feldnamen ohne
		// Binnenleerzeichen übernehmen; sonst würde eine Fortsetzungszeile
		// einen Schein-Schlüssel erzeugen.
		if key == "" || strings.ContainsAny(key, " \t") {
			continue
		}
		if _, exists := kv[key]; !exists {
			kv[key] = val
		}
	}
	return kv
}

// parseRouterOSVersion trennt "7.15.3 (stable)" in Version und Kanal.
// Fehlt der Kanal in Klammern, bleibt er leer.
func parseRouterOSVersion(s string) (version, channel string) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", ""
	}
	if open := strings.Index(s, "("); open >= 0 {
		version = strings.TrimSpace(s[:open])
		rest := s[open+1:]
		if end := strings.Index(rest, ")"); end >= 0 {
			channel = strings.TrimSpace(rest[:end])
		}
		return version, channel
	}
	return s, ""
}

// parseRouterOSMemMB wandelt RouterOS-Speicherangaben ("890.0MiB", "1024.0KiB",
// "2.0GiB", auch mit Leerzeichen) in Megabyte (gerundet). Unbekannt → 0.
func parseRouterOSMemMB(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	// Einheit vom Zahlenteil trennen.
	var num strings.Builder
	unit := ""
	for i, r := range s {
		if (r >= '0' && r <= '9') || r == '.' {
			num.WriteRune(r)
			continue
		}
		unit = strings.TrimSpace(s[i:])
		break
	}
	val, err := strconv.ParseFloat(num.String(), 64)
	if err != nil {
		return 0
	}
	switch strings.ToLower(unit) {
	case "kib", "kb", "k":
		val /= 1024
	case "gib", "gb", "g":
		val *= 1024
	case "tib", "tb", "t":
		val *= 1024 * 1024
	default: // MiB/MB oder ohne Einheit → bereits MB
	}
	return int64(val + 0.5)
}

// firstNonEmpty liefert den ersten nicht-leeren (getrimmten) Wert.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if t := strings.TrimSpace(v); t != "" {
			return t
		}
	}
	return ""
}
