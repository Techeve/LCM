package services_test

import (
	"encoding/base64"
	"regexp"
	"strings"
	"testing"

	"LCM/internal/infrastructure/sshx"
	"LCM/internal/storage/repositories"
)

// reB64Block greift die base64-Blöcke, mit denen LCM Dateiinhalte und das
// Probe-Skript quoting-sicher auf das Zielsystem bringt.
var reB64Block = regexp.MustCompile(`[A-Za-z0-9+/]{40,}={0,2}`)

// decodedPayload dekodiert ALLE base64-Blöcke eines Kommandos und liefert sie
// zusammengefügt. Nur so prüft ein Test, was auf dem Zielsystem tatsächlich
// ankommt - die sudoers-Datei und das Probe-Skript stehen im Kommando sonst
// nur als undurchsichtiger Block.
func decodedPayload(t *testing.T, cmd string) string {
	t.Helper()
	var out strings.Builder
	for _, m := range reB64Block.FindAllString(cmd, -1) {
		if raw, err := base64.StdEncoding.DecodeString(m); err == nil {
			out.Write(raw)
			out.WriteString("\n")
		}
	}
	return out.String()
}

// Der eingeschränkte Modus scheiterte auf mehreren Distributionen daran, dass
// LCM Annahmen über das Zielsystem traf, statt sie zu prüfen: auf RHEL-Klonen
// und openSUSE fand sudo den LCM-Helper nicht (secure_path ohne
// /usr/local/sbin, R2-019), auf Arch fehlte pacman in der Whitelist (R2-020)
// und auf Alpine gab es das Zielverzeichnis des Helpers gar nicht (R2-021).
// Gemeinsam ist allen drei Fällen: LCM meldete Erfolg oder einen
// nichtssagenden Fehler, und erst der nächste echte Lauf fiel auf die Nase.

// TestRestrictedSudoersDeckungFuerAlleDistributionen: die sudoers-Datei muss
// den Suchpfad selbst setzen (RHEL 10 liefert /usr/local/sbin nicht mit) und
// targetpw abschalten (openSUSE verlangt sonst das root-Passwort).
func TestRestrictedSudoersDeckungFuerAlleDistributionen(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	if _, err := env.Servers.RestrictSudo(repositories.ScopeAll(), id, "admin"); err != nil {
		t.Fatalf("einschränken: %v", err)
	}
	cmds := strings.Join(env.Dialer.Commands, "\n")
	all := cmds + "\n" + decodedPayload(t, cmds)

	for _, want := range []string{
		// R2-019: eigener Suchpfad, sonst findet sudo den Helper nicht.
		"secure_path=",
		"/usr/local/sbin",
		// R2-019 (openSUSE): ohne !targetpw fragt sudo nach dem root-Passwort.
		"!targetpw",
		// R2-021: Alpine bringt /usr/local/sbin nicht mit.
		"install -d -m 755 /usr/local/sbin",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("das Umschalt-Skript enthält %q nicht", want)
		}
	}
}

// TestRestrictedWhitelistKenntAlleFuenfPaketverwaltungen: pacman und apk
// fehlten in der Whitelist, obwohl LCM beide Paketverwaltungen unterstützt -
// auf Arch war nach dem „empfohlenen" Modus die Paketverwaltung tot (R2-020).
func TestRestrictedWhitelistKenntAlleFuenfPaketverwaltungen(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	if _, err := env.Servers.RestrictSudo(repositories.ScopeAll(), id, "admin"); err != nil {
		t.Fatalf("einschränken: %v", err)
	}
	cmds := strings.Join(env.Dialer.Commands, "\n")
	all := cmds + "\n" + decodedPayload(t, cmds)

	for _, bin := range []string{"apt-get", "dnf", "zypper", "pacman", "apk"} {
		if !strings.Contains(all, "/"+bin) {
			t.Errorf("die sudo-Whitelist kennt %q nicht - auf dieser Paketverwaltung wäre der Modus funktionslos", bin)
		}
	}
	// pacman-key gehört dazu: die Repository-Einrichtung importiert Schlüssel.
	if !strings.Contains(all, "pacman-key") {
		t.Error("pacman-key fehlt - Repository-Einrichtung auf Arch würde scheitern")
	}
}

// TestRestrictSudoProbtDieWirkung: nach dem Umschalten muss LCM belegen, dass
// der eingeschränkte Benutzer Helper UND Paketverwaltung wirklich erreicht.
func TestRestrictSudoProbtDieWirkung(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	if _, err := env.Servers.RestrictSudo(repositories.ScopeAll(), id, "admin"); err != nil {
		t.Fatalf("einschränken: %v", err)
	}
	cmds := strings.Join(env.Dialer.Commands, "\n")
	all := cmds + "\n" + decodedPayload(t, cmds)

	if !strings.Contains(all, "su -s /bin/sh") {
		t.Error("die Wirkungsprobe läuft nicht als Service-User")
	}
	if !strings.Contains(all, "base64 -d") {
		t.Error("das Probe-Skript wird nicht base64-kodiert übergeben (Quoting-Falle)")
	}
	// Und der Rückweg muss im selben Lauf vorbereitet sein.
	if !strings.Contains(all, "NOPASSWD:ALL") {
		t.Error("kein Rollback auf den Voll-Modus im Umschalt-Skript")
	}
}

// TestRestrictSudoNimmtSichZurueckWennDieProbeScheitert: schlägt die Probe
// fehl, darf kein halb eingeschränkter Server zurückbleiben - sonst wäre die
// Kernfunktion tot und der Rückweg ginge nur noch über die Serverkonsole
// (R2-020). Der Exit-Code der Probe steuert das.
func TestRestrictSudoNimmtSichZurueckWennDieProbeScheitert(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")

	// Das Umschalt-Skript endet mit dem vereinbarten Probe-Exit-Code. Das
	// Muster muss länger sein als die Standardmuster des Test-Dialers (der
	// längste Treffer gewinnt) - „su -s /bin/sh" kommt nur in der
	// Wirkungsprobe vor.
	env.Dialer.Responses["su -s /bin/sh"] = sshx.FakeResponse{
		Output:   "su: Permission denied",
		ExitCode: 9,
	}

	_, err := env.Servers.RestrictSudo(repositories.ScopeAll(), id, "admin")
	if err == nil {
		t.Fatal("erwartete einen Fehler - die Wirkungsprobe ist fehlgeschlagen")
	}
	if !strings.Contains(err.Error(), "zurückgenommen") {
		t.Errorf("die Meldung soll die Rücknahme benennen, bekam: %v", err)
	}
	srv, _ := env.Servers.Get(repositories.ScopeAll(), id)
	if srv.RestrictedSudo {
		t.Error("restricted_sudo darf nach verworfener Probe nicht gesetzt sein")
	}
}

// TestServiceUserProbeIstUnabhaengigVomShim: die Probe sucht die
// Paketverwaltung im System-PATH, nicht im Shim-Verzeichnis - dort liegt für
// JEDES Whitelist-Binary ein Wrapper, `command -v apt-get` fände also auch auf
// einem Arch-System einen Treffer und die Probe wäre wertlos.
func TestServiceUserProbeIstUnabhaengigVomShim(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	if _, err := env.Servers.RestrictSudo(repositories.ScopeAll(), id, "admin"); err != nil {
		t.Fatalf("einschränken: %v", err)
	}
	// Das Probe-Skript steckt base64-kodiert im Kommando - dekodieren und
	// nachsehen, statt auf die Kodierung zu vertrauen.
	probe := decodedPayload(t, strings.Join(env.Dialer.Commands, "\n"))
	if probe == "" {
		t.Fatal("kein Probe-Skript im Umschalt-Kommando gefunden")
	}
	if !strings.Contains(probe, "PATH=/usr/sbin:/usr/bin:/sbin:/bin command -v") {
		t.Errorf("die Paketverwaltung wird nicht im System-PATH gesucht:\n%s", probe)
	}
	if !strings.Contains(probe, "lcm-helper selftest") {
		t.Error("die Probe prüft den LCM-Helper nicht")
	}
}
