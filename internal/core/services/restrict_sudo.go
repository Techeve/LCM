package services

import (
	"fmt"
	"log/slog"
	"strings"

	"LCM/internal/storage/repositories"
)

// RestrictSudo schränkt einen bereits gejointen Server NACHTRÄGLICH auf den
// eingeschränkten Rechte-Modus ein: der LCM-Management-Benutzer verliert seine
// vollen Root-Rechte (NOPASSWD:ALL) und darf danach nur noch die feste
// Whitelist (Paketverwaltung, Docker, ufw) plus den validierenden LCM-Helper
// (Repositories, apt-Cache, SSH-Konfiguration/-Port, Benutzer-Sync) über sudo
// ausführen - kein beliebiges Kommando, kein Root-Shell-/Dateisystemzugriff.
//
// EINWEG-Operation: Nach dem Einschränken hat der Service-User selbst nicht
// mehr die Rechte, sich wieder Voll-Root zu verschaffen. Ein Zurück ist nur
// über „Neu verbinden" (Reconnect mit Login-Passwort, re-onboarding) möglich.
//
// Das Umschalt-Skript läuft bewusst NOCH im Voll-Modus (server.RestrictedSudo
// ist bis zum erfolgreichen Persistieren false) - nur so gehen visudo/mv/…
// durch, die nicht auf der Whitelist stehen. Die sudoers-Datei wird atomar
// ersetzt (erst .tmp, visudo-Prüfung, dann mv), es bleibt nie eine kaputte
// Konfiguration zurück. Die laufende Root-Shell des einen sudo-Aufrufs behält
// ihre Rechte über den gesamten Lauf, auch nachdem die sudoers ersetzt wurde.
func (s *ServerService) RestrictSudo(scope repositories.AccessScope, id uint, actor string) (string, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return "", err
	}
	// RouterOS kennt weder sudo noch eine POSIX-Shell - das Provisionierungs-
	// Skript ergäbe dort keinen Sinn. Zugleich ist der Login-Benutzer nur bei
	// RouterOS frei wählbar; ohne diesen Riegel liefe genau dieser Wert in ein
	// als root ausgeführtes Skript (siehe restrictedProvisionScript).
	if err := ensureNotRouterOS(server); err != nil {
		return "", err
	}
	// Der eingeschränkte Modus begrenzt den SSH-Service-User über eine
	// sudo-Whitelist. Auf einem Agent-Server gibt es diesen Benutzer nicht:
	// der lcm-agent läuft als Root-Dienst, jedes Kommando bliebe Root - die
	// Umschaltung erzeugte nur eine wirkungslose sudo-Umleitung und meldete
	// „eingeschränkt", wo nichts eingeschränkt ist. Deshalb derselbe Riegel
	// wie bei den übrigen SSH-spezifischen Aktionen.
	if err := ensureSSHTransport(server); err != nil {
		return "", err
	}
	// ensureFullSudo ist hier die Vorbedingung: nur ein Server im Voll-Modus
	// kann eingeschränkt werden. Ist er bereits eingeschränkt, kommt
	// ErrRestrictedSudo - die passende „nichts zu tun / nicht erlaubt"-Semantik.
	if err := ensureFullSudo(server); err != nil {
		return "", err
	}

	conn, err := s.connectRec(server, "restrict-sudo", actor)
	if err != nil {
		return "", fmt.Errorf("verbindung: %w", err)
	}
	defer conn.Close()

	// Nach dem Umschalten wird nachgewiesen, dass der eingeschränkte Benutzer
	// den Helper UND die Paketverwaltung wirklich erreicht; misslingt das,
	// stellt dieselbe (noch privilegierte) Root-Shell den Voll-Modus wieder
	// her. Ohne diese Probe meldete LCM auf Arch „eingeschränkt: eingerichtet"
	// und hinterließ ein System, dessen Paketverwaltung tot war und dessen
	// Rückweg nur über die Serverkonsole führte (R2-020).
	script := strings.Join(restrictedProvisionScript(server.ServiceUser), " && ") +
		" && { " + restrictedSelfTestScript(server.ServiceUser) + "; } || { " +
		restrictedRollbackScript(server.ServiceUser) + fmt.Sprintf("; exit %d; }", restrictSelfTestExit)
	output, code, runErr := conn.Run(privRun(server, script))
	if runErr != nil {
		return output, runErr
	}
	if code != 0 {
		// Die Ausgabe des Zielsystems gehört in die Meldung. Zuvor kam nur
		// "exit 1" an der API an, und weil der Aufruf synchron läuft (kein
		// Job in der Historie), war die Ursache nicht mehr rekonstruierbar -
		// im Langzeittest blieb offen, warum ausgerechnet zwei Systeme
		// scheiterten (BUG-031). Der vollständige Verlauf steht zusätzlich im
		// SSH-Protokoll des Servers, auf log_level=debug auch im Anwendungslog.
		err := fmt.Errorf("rechte einschränken fehlgeschlagen (exit %d) - sudo-Konfiguration unverändert", code)
		if code == restrictSelfTestExit {
			// Die Wirkungsprobe hat den Modus verworfen und den Voll-Modus
			// wiederhergestellt. Die Ursache steht in der Ausgabe (welcher
			// Baustein nicht erreichbar war).
			err = fmt.Errorf("der eingeschränkte Modus wurde eingerichtet, trägt auf diesem System aber nicht - " +
				"LCM hat ihn deshalb sofort zurückgenommen; der Server läuft unverändert im Voll-Modus weiter")
		}
		slog.Warn("restrict-sudo failed", "server", server.Name, "exit_code", code,
			"output", truncateOutput(redactSecrets(output)))
		return output, withProvisionLog(err, output)
	}

	_ = s.servers.UpdateFields(id, map[string]any{"restricted_sudo": true})
	s.audit.Log(actor, "server.restrict-sudo", "server", id, server.Name)
	return output, nil
}
