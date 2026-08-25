package agent

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"time"

	"LCM/internal/i18n"
	"LCM/internal/remote/wire"
)

// Installationsorte des Binaries und der systemd-Unit.
const (
	// debBinaryPath ist der Pfad aus dem lcm-agent-Debian-Paket - liegt das
	// Binary dort, übernimmt dpkg die Verwaltung und enroll kopiert nichts.
	debBinaryPath = "/usr/bin/lcm-agent"
	// selfInstallPath ist das Ziel der Selbstinstallation (manuell
	// heruntergeladenes Binary, z.B. per curl vom LCM-Server).
	selfInstallPath = "/usr/local/bin/lcm-agent"
	// unitPath ist die von enroll geschriebene Unit (nur wenn das Paket
	// keine mitgebracht hat).
	unitPath = "/etc/systemd/system/lcm-agent.service"
	// openrcPath ist das Init-Skript auf Systemen ohne systemd (Alpine).
	openrcPath     = "/etc/init.d/lcm-agent"
	serviceName    = "lcm-agent"
	enrollTestWait = 20 * time.Second
)

// packagedUnits sind die Orte, an denen ein Paket seine systemd-Unit ablegt.
// Beide werden geprüft: Das Agent-Paket liefert sie seit 1.27 unter /usr/lib
// (auf Arch gehört /lib dem Paket „filesystem"), ältere Installationen haben
// sie unter /lib.
var packagedUnits = []string{
	"/usr/lib/systemd/system/lcm-agent.service",
	"/lib/systemd/system/lcm-agent.service",
}

// unitTemplate ist die Unit der Selbstinstallation - inhaltlich identisch
// mit packaging/lcm-agent.service (dort für das Debian-Paket).
const unitTemplate = `[Unit]
Description=LCM Remote Agent (verbindet diesen Server ausgehend mit dem LCM-Server)
Documentation=https://gitlab.techeve.de/techeve/lcm
After=network-online.target
Wants=network-online.target
# Ohne Enrollment (Konfiguration) startet der Dienst nicht.
ConditionPathExists=%s

[Service]
Type=simple
ExecStart=%s run
Restart=always
RestartSec=10
# Der Agent führt Verwaltungs-Kommandos des LCM-Servers als root aus
# (Paketverwaltung, Docker, Firewall) - wie eine SSH-Root-Session.
User=root
ProtectHostname=no
NoNewPrivileges=no

[Install]
WantedBy=multi-user.target
`

// openrcTemplate ist das Init-Skript der Selbstinstallation - inhaltlich
// identisch mit packaging/lcm-agent.openrc (dort für das apk-Paket).
const openrcTemplate = `#!/sbin/openrc-run
name="lcm-agent"
description="LCM Remote Agent"

command="%s"
command_args="run"
output_log="/var/log/lcm-agent.log"
error_log="/var/log/lcm-agent.log"

# supervise-daemon: nur damit wirkt der Wiederanlauf (Gegenstueck zu
# Restart=always in der systemd-Unit).
supervisor="supervise-daemon"
pidfile="/run/supervise-lcm-agent.pid"
respawn_delay=10
respawn_max=0

depend() {
	need net
}

start_pre() {
	if [ ! -e %s ]; then
		eerror "lcm-agent ist noch nicht eingerichtet (lcm-agent enroll <url> <token>)."
		return 1
	fi
}
`

// Enroll richtet den Agent ein: Token prüfen, Verbindungstest, Konfiguration
// schreiben, ggf. Binary+Unit installieren, Dienst aktivieren und starten.
func Enroll(serverURL, token, version string, log *slog.Logger) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("enroll benötigt root (sudo lcm-agent enroll …)")
	}
	agentID, secret, certFP, err := wire.ParseToken(token)
	if err != nil {
		return fmt.Errorf("token ungültig: %w", err)
	}
	normalized, err := NormalizeServerURL(serverURL)
	if err != nil {
		return err
	}
	cfg := &Config{URL: normalized, AgentID: agentID, Secret: secret, CertFingerprint: certFP}

	// Erst verbinden, dann installieren - ein Tippfehler in Adresse oder
	// Token fällt so sofort auf, nicht erst im Dienst-Log.
	fmt.Println(i18n.Tf("Testing connection to %s ...", "Verbindungstest zu %s …", normalized))
	if err := NewClient(cfg, log, version).TestConnection(enrollTestWait); err != nil {
		return fmt.Errorf("verbindungstest fehlgeschlagen: %w", err)
	}
	fmt.Println(i18n.T("Connection and authentication succeeded.", "Verbindung und Anmeldung erfolgreich."))

	if err := cfg.Save(ConfigPath); err != nil {
		return fmt.Errorf("konfiguration schreiben: %w", err)
	}
	binPath, err := ensureInstalled()
	if err != nil {
		return err
	}
	if err := ensureUnit(binPath); err != nil {
		return err
	}
	if err := enableService(); err != nil {
		return err
	}
	fmt.Println(i18n.Tf(
		"lcm-agent set up and started (configuration: %s).",
		"lcm-agent eingerichtet und gestartet (Konfiguration: %s).",
		ConfigPath))
	fmt.Println(i18n.T(
		"The server will show up as online in LCM within a few seconds.",
		"Der Server erscheint in LCM in wenigen Sekunden als online."))
	return nil
}

// ensureInstalled sorgt dafür, dass das Binary an einem festen Pfad liegt
// (Selbstkopie, falls es z.B. aus /tmp oder ~ gestartet wurde) und liefert
// den Pfad für die Unit.
func ensureInstalled() (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	if self == debBinaryPath || self == selfInstallPath {
		return self, nil
	}
	if _, err := os.Stat(debBinaryPath); err == nil {
		return debBinaryPath, nil // Paket-Installation vorhanden
	}
	if err := copyFile(self, selfInstallPath, 0o755); err != nil {
		return "", fmt.Errorf("binary nach %s installieren: %w", selfInstallPath, err)
	}
	fmt.Println(i18n.Tf("Binary installed to %s.", "Binary nach %s installiert.", selfInstallPath))
	return selfInstallPath, nil
}

// ensureUnit hinterlegt die Dienstbeschreibung, wenn keine aus einem Paket
// vorliegt - je nach Init-System die systemd-Unit oder das OpenRC-Skript.
func ensureUnit(binPath string) error {
	if usesOpenRC() {
		if _, err := os.Stat(openrcPath); err == nil {
			return nil // Skript kommt aus dem apk-Paket
		}
		script := fmt.Sprintf(openrcTemplate, binPath, ConfigPath)
		if err := os.WriteFile(openrcPath, []byte(script), 0o755); err != nil {
			return fmt.Errorf("init-skript schreiben: %w", err)
		}
		return nil
	}
	for _, p := range packagedUnits {
		if _, err := os.Stat(p); err == nil {
			return nil // Unit kommt aus dem Paket
		}
	}
	if _, err := os.Stat(unitPath); err == nil {
		return nil
	}
	unit := fmt.Sprintf(unitTemplate, ConfigPath, binPath)
	if err := os.WriteFile(unitPath, []byte(unit), 0o644); err != nil {
		return fmt.Errorf("unit schreiben: %w", err)
	}
	return nil
}

// usesOpenRC meldet, ob dieses System OpenRC statt systemd fährt.
//
// Die Frage ist nicht akademisch: Auf Alpine gibt es kein systemctl, und jeder
// Aufruf endete mit „executable file not found" - der Agent wäre installiert
// gewesen und hätte nie gelaufen.
func usesOpenRC() bool {
	if _, err := exec.LookPath("systemctl"); err == nil {
		return false
	}
	_, err := exec.LookPath("rc-update")
	return err == nil
}

// enableService aktiviert den Dienst und startet ihn - mit dem Mittel des
// jeweiligen Init-Systems.
func enableService() error {
	if usesOpenRC() {
		for _, c := range [][]string{
			{"rc-update", "add", serviceName, "default"},
			{"rc-service", serviceName, "start"},
		} {
			if out, err := exec.Command(c[0], c[1:]...).CombinedOutput(); err != nil {
				return fmt.Errorf("%v: %v - %s", c, err, string(out))
			}
		}
		return nil
	}
	for _, args := range [][]string{
		{"daemon-reload"},
		{"enable", "--now", serviceName},
	} {
		if out, err := exec.Command("systemctl", args...).CombinedOutput(); err != nil {
			return fmt.Errorf("systemctl %v: %v - %s", args, err, string(out))
		}
	}
	return nil
}

// disableService stoppt und deaktiviert den Dienst (Fehler sind hier egal -
// er kann längst weg sein).
func disableService() {
	if usesOpenRC() {
		_ = exec.Command("rc-service", serviceName, "stop").Run()
		_ = exec.Command("rc-update", "del", serviceName, "default").Run()
		return
	}
	_ = exec.Command("systemctl", "disable", "--now", serviceName).Run()
}

// Uninstall entfernt den Agent vom System: Dienst stoppen/deaktivieren,
// Konfiguration und (bei Selbstinstallation) Binary + Unit löschen.
// Ein per Debian-Paket installiertes Binary bleibt dpkg überlassen.
func Uninstall() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("uninstall benötigt root (sudo lcm-agent uninstall)")
	}
	disableService()
	_ = os.RemoveAll(ConfigDir)
	if _, err := os.Stat(unitPath); err == nil {
		_ = os.Remove(unitPath)
		_ = exec.Command("systemctl", "daemon-reload").Run()
	}
	// Selbstinstalliertes Binary entfernen (unter Linux auch während es
	// selbst läuft möglich); /usr/bin/lcm-agent gehört dem Paket.
	_ = os.Remove(selfInstallPath)
	fmt.Println(i18n.T(
		"lcm-agent removed. If installed via apt: `apt remove lcm-agent`.",
		"lcm-agent entfernt. Falls per apt installiert: `apt remove lcm-agent`."))
	fmt.Println(i18n.T(
		"Afterwards delete the server entry in the LCM web interface.",
		"Den Server-Eintrag in LCM anschließend über die Oberfläche löschen."))
	return nil
}

// copyFile kopiert src nach dst (atomar über eine temporäre Datei).
func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}
