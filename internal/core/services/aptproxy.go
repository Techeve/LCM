package services

import (
	"errors"
	"fmt"
	"strings"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

// ErrNoAptCacheURL: Anbindung angefordert, aber in den globalen Einstellungen
// ist keine APT-Cache-URL hinterlegt.
var ErrNoAptCacheURL = errors.New("keine apt-cache-url konfiguriert - bitte unter Einstellungen → APT-Cache hinterlegen")

// aptProxyDropin ist das von LCM verwaltete apt-Drop-in, das APT-Anfragen über
// den zentralen Cache leitet.
//
// HTTPS ist dabei NICHT selbstverständlich: apt-cacher-ng lässt CONNECT-Tunnel
// nur zu, wenn `PassThroughPattern` gesetzt ist - in der Standardkonfiguration
// ist die Direktive auskommentiert. Bis hierher setzte LCM trotzdem beide
// Richtungen, und jede HTTPS-Paketquelle starb an „403 CONNECT denied": auf
// den angebundenen Systemen fielen Docker, Trivy und sogar LCMs eigenes Repo
// stillschweigend aus Inventar, Update- und CVE-Sicht (R2-038). Deshalb wird
// die Fähigkeit jetzt gemessen statt vorausgesetzt.
const aptProxyDropin = "/etc/apt/apt.conf.d/02lcm-apt-cache"

// aptProxyDropinBody baut den Drop-in-Inhalt. Ohne CONNECT-Tunnel bleibt HTTPS
// bewusst am Proxy vorbei (DIRECT) - lieber ungecacht als unerreichbar.
//
// Doppelte Anführungszeichen wörtlich (das printf-Format steht im Skript in
// einfachen Quotes) - ein \" würde dash-printf literal ausgeben und apt sähe
// "Unsupported proxy \http://…" (Live-Befund).
func aptProxyDropinBody(cacheURL string, https bool) string {
	if https {
		return fmt.Sprintf(`Acquire::http::Proxy "%s";\nAcquire::https::Proxy "%s";\n`, cacheURL, cacheURL)
	}
	return fmt.Sprintf(`Acquire::http::Proxy "%s";\nAcquire::https::Proxy "DIRECT";\n`, cacheURL)
}

// aptProxyEnableScript schreibt das Drop-in und belegt seine Tauglichkeit mit
// einem echten apt-Update. Kann der Cache keine HTTPS-Tunnel, fällt LCM auf
// „nur HTTP" zurück und sagt das; bleiben danach Quellen kaputt, wird das
// Drop-in entfernt - der Server bleibt arbeitsfähig.
//
// Entscheidend ist die Auswertung der AUSGABE: `apt-get update` endet auch
// dann mit exit 0, wenn einzelne Quellen mit „Err:" scheitern. Genau daran lag
// es, dass LCM eine halb kaputte Anbindung als Erfolg meldete (R2-038).
func aptProxyEnableScript(cacheURL string) string {
	return strings.Join([]string{
		fmt.Sprintf("printf '%s' > %s", aptProxyDropinBody(cacheURL, true), aptProxyDropin),
		"LCM_OUT=$(apt-get update 2>&1)",
		"LCM_MODE='HTTP und HTTPS'",
		// Erkennt der Cache CONNECT nicht, meldet er 403 - dann HTTPS direkt.
		`if printf '%s' "$LCM_OUT" | grep -qiE 'CONNECT denied|Invalid response from proxy'; then`,
		fmt.Sprintf("  printf '%s' > %s", aptProxyDropinBody(cacheURL, false), aptProxyDropin),
		"  LCM_OUT=$(apt-get update 2>&1)",
		"  LCM_MODE='nur HTTP - der Cache erlaubt keine HTTPS-Tunnel (PassThroughPattern nicht gesetzt), HTTPS-Quellen laufen daher ungecacht direkt'",
		"fi",
		// Jetzt zählt die Ausgabe, nicht der Exit-Code.
		`if printf '%s' "$LCM_OUT" | grep -qE '^(Err|E):'; then`,
		fmt.Sprintf("  rm -f %s", aptProxyDropin),
		"  apt-get update >/dev/null 2>&1 || true",
		`  printf '%s\n' "$LCM_OUT" | grep -E '^(Err|E|W): ' | head -n 10`,
		"  echo 'LCM: apt-update ueber den cache meldete fehlerhafte paketquellen - drop-in wieder entfernt'",
		"  exit 1",
		"fi",
		fmt.Sprintf(`echo "LCM: apt-anfragen laufen jetzt ueber den cache %s ($LCM_MODE)"`, cacheURL),
	}, "\n")
}

// aptProxyDisableScript entfernt das Drop-in - APT spricht wieder direkt.
func aptProxyDisableScript() string {
	return fmt.Sprintf("rm -f %s && echo 'LCM: apt-cache-anbindung entfernt'", aptProxyDropin)
}

// aptProxyStatusCommand meldet "yes"/"no", ob das Drop-in vorhanden ist.
func aptProxyStatusCommand() string {
	return fmt.Sprintf("test -f %s && echo yes || echo no", aptProxyDropin)
}

// ConfigureAptProxy bindet einen Server an den zentralen APT-Cache an
// (enable) oder löst ihn davon (disable). Synchrone Aktion mit Output,
// Muster ConfigureFirewall/HardenSSH.
func (s *ServerService) ConfigureAptProxy(scope repositories.AccessScope, id uint, enable bool, actor string) (string, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return "", err
	}
	var script string
	if enable {
		if s.aptCacheURL == nil {
			return "", ErrNoAptCacheURL
		}
		cacheURL, err := s.aptCacheURL()
		if err != nil {
			return "", err
		}
		if cacheURL == "" {
			return "", ErrNoAptCacheURL
		}
		script = aptProxyEnableScript(cacheURL)
		// Eingeschränkter Modus: das Drop-in schreibt der LCM-Helper
		// (validiert die URL, gleiche apt-update-Probe mit Rollback).
		if server.RestrictedSudo {
			script = helperCmd("apt-proxy", cacheURL)
		}
	} else {
		script = aptProxyDisableScript()
		if server.RestrictedSudo {
			script = helperCmd("apt-proxy", "off")
		}
	}

	conn, err := s.connectRec(server, "apt-proxy", actor)
	if err != nil {
		return "", fmt.Errorf("verbindung: %w", err)
	}
	defer conn.Close()

	output, code, runErr := conn.Run(privRun(server, script))
	if runErr != nil {
		return output, runErr
	}
	if code != 0 {
		return output, fmt.Errorf("apt-cache-anbindung fehlgeschlagen (exit %d)", code)
	}
	_ = s.servers.UpdateFields(id, map[string]any{"apt_proxy_active": enable})
	verb := "getrennt"
	if enable {
		verb = "angebunden"
	}
	if !enable {
		if hint := s.aptProxyEnforceHint(id); hint != "" {
			output += hint
		}
	}
	s.audit.Log(actor, "server.apt-proxy", "server", id, server.Name+": "+verb)
	return output, nil
}

// aptProxyEnforceHint warnt beim Trennen, wenn eine aktive apt-proxy-
// Grundsatz-Regel den Server beim nächsten Health-Check wieder an den Cache
// anbinden würde - sonst wirkte die Trennung „unerklärlich" nur ein paar
// Minuten. Rein informativ; die Trennung selbst wird nicht verhindert.
func (s *ServerService) aptProxyEnforceHint(id uint) string {
	if s.groups == nil {
		return ""
	}
	rules, err := s.groups.FindEnforceRulesForServer(id)
	if err != nil {
		return ""
	}
	var hints []string
	for i := range rules {
		if rules[i].Type == domain.RuleTypeAptProxy {
			hints = append(hints, fmt.Sprintf(
				"\nLCM: ACHTUNG - die Grundsatz-Regel %q bindet diesen Server beim nächsten Health-Check wieder an den APT-Cache an. Für eine dauerhafte Trennung die Regel in der Gruppe deaktivieren oder löschen.",
				rules[i].Name))
		}
	}
	return strings.Join(hints, "")
}
