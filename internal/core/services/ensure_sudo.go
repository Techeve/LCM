package services

import (
	"fmt"
	"strings"

	"LCM/internal/infrastructure/sshx"
)

// Auf Debian ist sudo kein Bestandteil der Grundinstallation, und die darauf
// aufbauenden Proxmox-Systeme (VE, PBS, PDM) liefern es ebenfalls nicht mit.
// Sie bringen aber /etc/sudoers.d/ mit - der beim Onboarding geschriebene
// Eintrag zeigte dort auf ein Programm, das es nicht gibt, und jede Aktion
// des Service-Users endete mit "sudo: command not found" (BUG-019/BUG-030).
//
// Der Ausweg ist nicht, das Onboarding abzubrechen und den Administrator zum
// Nachinstallieren zu schicken: LCM hat für die Provisionierung ohnehin
// Root-Rechte und legt dort bereits ein Konto, sudoers-Dateien und den Helper
// an. Ein fehlendes Standardpaket nachzuinstallieren gehört in dieselbe
// Reihe - sonst scheitert die Aufnahme genau an dem Punkt, an dem LCM sie
// selbst beheben kann.
//
// Entfernt wird sudo NIE wieder, auch nicht beim Rücknehmen einer
// gescheiterten Provisionierung: Das Paket ist Teil der Systemausstattung,
// andere Dienste und Anmeldungen können es inzwischen benutzen, und ein
// Deinstallieren könnte einen Administrator aussperren. Was LCM installiert,
// steht im Provisionierungs-Protokoll und in der Doku (Serveränderungen).

// installSudoScript installiert sudo über die Paketverwaltung des Systems.
// Läuft als root (siehe rootEscalation) und meldet per Exit-Code, ob sudo
// danach vorhanden ist.
func installSudoScript() string {
	return strings.Join([]string{
		// Schon da: nichts tun. Der Aufrufer prüft das zwar vorher, aber das
		// Skript soll für sich allein richtig sein.
		"if command -v sudo >/dev/null 2>&1; then exit 0; fi",
		"if command -v apt-get >/dev/null 2>&1; then",
		"  export DEBIAN_FRONTEND=noninteractive",
		// Erst aus den vorhandenen Paketlisten installieren. Nur wenn das
		// fehlschlägt (frisches System ohne Listen), vorher aktualisieren.
		// Das Ergebnis von "apt-get update" ist dabei egal: Proxmox ohne
		// Subscription quittiert das Enterprise-Repository mit 401 und liefert
		// Exit-Code 100, die übrigen Listen sind danach trotzdem aktuell.
		"  apt-get install -y sudo || { apt-get update || true; apt-get install -y sudo; }",
		"elif command -v dnf >/dev/null 2>&1; then dnf install -y sudo",
		"elif command -v yum >/dev/null 2>&1; then yum install -y sudo",
		"elif command -v zypper >/dev/null 2>&1; then zypper --non-interactive install sudo",
		"elif command -v pacman >/dev/null 2>&1; then pacman -Sy --noconfirm sudo",
		"elif command -v apk >/dev/null 2>&1; then apk add --no-cache sudo",
		"else echo 'keine unterstuetzte Paketverwaltung gefunden' >&2; exit 1",
		"fi",
		// Der Exit-Code der Paketverwaltung allein genügt nicht: Ein Paket
		// kann als "installiert" gemeldet werden und das Programm trotzdem
		// fehlen (abgebrochene Installation, leerer Container-Layer).
		"command -v sudo >/dev/null 2>&1",
	}, "\n")
}

// ensureSudo stellt sicher, dass auf dem Zielsystem sudo vorhanden ist -
// nachinstalliert wird nur, wenn es fehlt.
//
// Aufgerufen wird das VOR der Provisionierung: Scheitert die Installation,
// ist auf dem Zielsystem noch nichts angelegt, das zurückgenommen werden
// müsste, und die Meldung nennt den einen Befehl, der von Hand hilft.
func ensureSudo(conn sshx.Conn, esc *rootEscalation, host string, log *strings.Builder) error {
	if esc == nil || esc.method == "sudo" {
		// Die Rechte-Erkennung ist selbst über sudo gelaufen - dann gibt es
		// sudo offensichtlich.
		return nil
	}
	if out, code, err := conn.Run("command -v sudo"); err == nil && code == 0 && strings.TrimSpace(out) != "" {
		return nil
	}

	log.WriteString("### sudo nachinstallieren\nsudo ist nicht installiert (Debian/Proxmox liefern es nicht mit) - LCM installiert es\n")
	cmd, stdin := esc.wrap(installSudoScript())
	out, code, err := conn.RunStdin(cmd, stdin)
	log.WriteString("$ " + cmd + "\n" + out + "\n")
	if err == nil && code == 0 {
		return nil
	}

	detail := strings.TrimSpace(lastLines(out, 3))
	if detail == "" {
		detail = fmt.Sprintf("exit-code %d", code)
	}
	return fmt.Errorf(
		"sudo ist auf %s nicht installiert und ließ sich nicht nachinstallieren (%s). "+
			"Debian und die darauf aufbauenden Proxmox-Systeme (VE, PBS, PDM) liefern zwar "+
			"/etc/sudoers.d/ mit, aber nicht das Programm selbst. LCM braucht es, um auf dem "+
			"Server überhaupt arbeiten zu können. Installiere es von Hand (apt-get install sudo) "+
			"und verbinde erneut",
		host, detail)
}

// lastLines liefert die letzten n nicht-leeren Zeilen - bei einem
// fehlgeschlagenen Paketbefehl steht die Ursache am Ende der Ausgabe.
func lastLines(s string, n int) string {
	var kept []string
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			kept = append(kept, strings.TrimSpace(line))
		}
	}
	if len(kept) > n {
		kept = kept[len(kept)-n:]
	}
	return strings.Join(kept, " | ")
}
