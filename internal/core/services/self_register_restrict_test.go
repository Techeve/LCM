package services

import (
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"LCM/internal/core/domain"
)

// writeOnboardRestricted legt die Übergabedatei mit dem Wunsch „eingeschränkt"
// an - so schreibt sie postinstall.sh seit 1.39 bei einer Neuinstallation.
func writeOnboardRestricted(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, SelfOnboardFileName)
	b, _ := json.Marshal(selfOnboard{
		ServiceUser:    "lcm-svc",
		PrivateKeyPEM:  "-----BEGIN OPENSSH PRIVATE KEY-----\nTESTKEY\n-----END OPENSSH PRIVATE KEY-----\n",
		PublicKey:      "ssh-ed25519 AAAATEST lcm-self",
		RestrictedSudo: true,
	})
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatalf("übergabedatei schreiben: %v", err)
	}
	return path
}

// TestSelfRegisterSchraenktNeuenHostEin: Bei einer Neuinstallation legt das
// Paketskript das Konto mit vollen Rechten an - anders lassen sich Helper und
// Whitelist nicht installieren - und LCM nimmt sie ihm sofort wieder.
func TestSelfRegisterSchraenktNeuenHostEin(t *testing.T) {
	svc, servers, _, dir := selfTestEnv(t)
	writeOnboardRestricted(t, dir)

	var restrictedID uint
	svc.WithRestrict(func(id uint) error { restrictedID = id; return nil }).Run()

	all, _ := servers.FindAllUnscoped()
	if len(all) != 1 {
		t.Fatalf("erwartet 1 Server, bekommen %d", len(all))
	}
	if restrictedID != all[0].ID {
		t.Errorf("Einschränken wurde nicht für den neuen Host aufgerufen (id=%d)", restrictedID)
	}
}

// TestSelfRegisterBehaeltHostWennEinschraenkenScheitert: Schlägt das
// Einschränken fehl, bleibt der Host aufgenommen und im Voll-Modus. Ein
// Eintrag, der „eingeschränkt" behauptet, während die sudoers NOPASSWD:ALL
// trägt, wäre das schlechtere Ergebnis.
func TestSelfRegisterBehaeltHostWennEinschraenkenScheitert(t *testing.T) {
	svc, servers, _, dir := selfTestEnv(t)
	writeOnboardRestricted(t, dir)

	svc.WithRestrict(func(uint) error { return errors.New("kein sudo") }).Run()

	all, _ := servers.FindAllUnscoped()
	if len(all) != 1 {
		t.Fatalf("erwartet 1 Server, bekommen %d", len(all))
	}
	if all[0].RestrictedSudo {
		t.Error("gescheitertes Einschränken darf nicht als eingeschränkt gelten")
	}
}

// TestReportHostModeMeldetVollModus: Eine bestehende Installation bleibt im
// Voll-Modus, bis jemand sie umstellt - das muss im Protokoll stehen, sonst
// hält der Betreiber sie fälschlich für eingeschränkt.
func TestReportHostModeMeldetVollModus(t *testing.T) {
	svc, servers, _, _ := selfTestEnv(t)
	if err := servers.Create(&domain.Server{
		Name: SelfHostName, Host: "localhost", SSHPort: 22,
		ServiceUser: "lcm-svc", HostKeyFingerprint: "SHA256:x", PrivateKeyEnc: "x",
	}); err != nil {
		t.Fatal(err)
	}

	if got := captureLog(t, svc.ReportHostMode); !strings.Contains(got, "selfhost.full-sudo") {
		t.Errorf("Voll-Modus wurde nicht gemeldet: %s", got)
	}

	all, _ := servers.FindAllUnscoped()
	if err := servers.UpdateFields(all[0].ID, map[string]any{"restricted_sudo": true}); err != nil {
		t.Fatal(err)
	}
	got := captureLog(t, svc.ReportHostMode)
	if !strings.Contains(got, "selfhost.restricted") || strings.Contains(got, "full-sudo") {
		t.Errorf("eingeschränkter Modus wurde falsch gemeldet: %s", got)
	}
}

// captureLog sammelt ein, was fn über slog schreibt.
func captureLog(t *testing.T, fn func()) string {
	t.Helper()
	var buf strings.Builder
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prev)
	fn()
	return buf.String()
}

// TestPostinstallLaesstBestehendeEinrichtungInRuhe: Beim Upgrade darf das
// Paketskript weder die sudoers-Regel neu schreiben (das ersetzte eine
// eingeschränkte Whitelist durch NOPASSWD:ALL) noch einen weiteren Schlüssel
// in authorized_keys legen, den LCM nie benutzt.
func TestPostinstallLaesstBestehendeEinrichtungInRuhe(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "packaging", "scripts", "postinstall.sh"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(raw)
	guard := strings.Index(script, `elif [ -f /etc/sudoers.d/lcm-svc ]`)
	if guard < 0 {
		t.Fatal("die Upgrade-Weiche fehlt - jedes Upgrade würde die sudoers-Regel neu schreiben")
	}
	write := strings.Index(script, `NOPASSWD:ALL" > /etc/sudoers.d/lcm-svc`)
	if write < 0 {
		t.Fatal("die Zeile, die die sudoers-Regel schreibt, wurde nicht gefunden")
	}
	if guard > write {
		t.Error("die Weiche muss VOR dem Schreiben der sudoers-Regel stehen")
	}
}
