package services

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/crypto"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/storage/repositories"
)

// fakeSelfDialer liefert einen festen Host-Key, ohne wirklich zu verbinden.
type fakeSelfDialer struct{ err error }

func (d fakeSelfDialer) Probe(string, int) (string, string, error) {
	if d.err != nil {
		return "", "", d.err
	}
	return "SHA256:TESTselfhostkey", "ssh-ed25519", nil
}
func (d fakeSelfDialer) DialPassword(string, int, string, string, string) (sshx.Conn, error) {
	return nil, nil
}
func (d fakeSelfDialer) DialKey(string, int, string, string, string) (sshx.Conn, error) {
	return nil, nil
}

// selfTestEnv baut eine Wegwerf-Umgebung: eigene DB, eigenes Datenverzeichnis
// und eine bereits geschriebene Übergabedatei.
func selfTestEnv(t *testing.T) (*SelfRegisterService, *repositories.ServerRepository, *repositories.SettingsRepository, string) {
	t.Helper()
	// DB direkt aufbauen: storage importiert services - ein storage-Import
	// hier wäre ein Import-Zyklus (wie in job_panic_test.go).
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("test-db: %v", err)
	}
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	if err := db.AutoMigrate(&domain.Server{}, &domain.GlobalSettings{}); err != nil {
		t.Fatalf("migration: %v", err)
	}
	servers := repositories.NewServerRepository(db)
	settings := repositories.NewSettingsRepository(db)
	// Einstellungs-Zeile anlegen - in der echten Instanz entsteht sie beim
	// Start, bevor die Selbstaufnahme läuft. Ohne sie liest der Dienst die
	// Einstellungen nicht und nimmt bewusst nichts auf (im Zweifel nicht
	// handeln, weil der Abschalter unbekannt bliebe).
	if err := settings.Save(&domain.GlobalSettings{}); err != nil {
		t.Fatalf("einstellungen anlegen: %v", err)
	}

	cipher, err := crypto.NewCipher(make([]byte, 32))
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	dir := t.TempDir()
	svc := newSelfSvc(servers, settings, fakeSelfDialer{}, cipher, dir)
	return svc, servers, settings, dir
}

// newSelfSvc baut den Dienst so, dass er NICHT von der Umgebung abhaengt, in
// der die Tests laufen: Die Container-Erkennung wird fest auf "kein Container"
// gesetzt. Sonst uebersprang der Dienst in der CI (Docker-Runner) die Aufnahme
// und der Regelfall-Test schlug dort fehl, obwohl er lokal gruen war.
func newSelfSvc(
	servers *repositories.ServerRepository,
	settings *repositories.SettingsRepository,
	dialer sshx.Dialer,
	cipher *crypto.Cipher,
	dir string,
) *SelfRegisterService {
	svc := NewSelfRegisterService(servers, settings, dialer, cipher, dir)
	svc.containerCheck = func() bool { return false }
	return svc
}

// writeOnboard legt die Übergabedatei an, wie es das Installationsskript tut.
func writeOnboard(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, SelfOnboardFileName)
	b, _ := json.Marshal(selfOnboard{
		ServiceUser:   "lcm-svc",
		PrivateKeyPEM: "-----BEGIN OPENSSH PRIVATE KEY-----\nTESTKEY\n-----END OPENSSH PRIVATE KEY-----\n",
		PublicKey:     "ssh-ed25519 AAAATEST lcm-self",
	})
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatalf("übergabedatei schreiben: %v", err)
	}
	return path
}

func countServers(t *testing.T, r *repositories.ServerRepository) int {
	t.Helper()
	all, err := r.FindAllUnscoped()
	if err != nil {
		t.Fatalf("server lesen: %v", err)
	}
	return len(all)
}

// TestSelfRegisterCreatesHost ist der Regelfall: Die Übergabedatei liegt vor,
// nichts spricht dagegen - der Host wird aufgenommen, der Schlüssel landet
// VERSCHLÜSSELT in der Datenbank und die Datei ist danach weg.
func TestSelfRegisterCreatesHost(t *testing.T) {
	svc, servers, _, dir := selfTestEnv(t)
	path := writeOnboard(t, dir)

	svc.Run()

	all, _ := servers.FindAllUnscoped()
	if len(all) != 1 {
		t.Fatalf("erwartet 1 server, bekommen %d", len(all))
	}
	got := all[0]
	if got.Name != SelfHostName {
		t.Errorf("name = %q, erwartet %q", got.Name, SelfHostName)
	}
	if !got.IsLcmHost() {
		t.Errorf("der angelegte server gilt nicht als LCM-Host (host=%q port=%d)", got.Host, got.SSHPort)
	}
	if got.HostKeyFingerprint != "SHA256:TESTselfhostkey" {
		t.Errorf("host-key nicht uebernommen: %q", got.HostKeyFingerprint)
	}
	// Der Schlüssel darf nicht im Klartext in der Datenbank stehen.
	if got.PrivateKeyEnc == "" || got.PrivateKeyEnc == "-----BEGIN OPENSSH PRIVATE KEY-----\nTESTKEY\n-----END OPENSSH PRIVATE KEY-----\n" {
		t.Errorf("privater schluessel wurde nicht verschluesselt abgelegt")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("uebergabedatei liegt noch da - der klartext-schluessel muss weg sein")
	}
}

// TestSelfRegisterRespectsDisabled deckt „bewusst geloescht" ab: Wer den
// Eintrag entfernt, will ihn los - das naechste Paket-Update darf ihn nicht
// zurueckbringen.
func TestSelfRegisterRespectsDisabled(t *testing.T) {
	svc, servers, settings, dir := selfTestEnv(t)
	if err := settings.UpdateFields(map[string]any{"self_server_disabled": true}); err != nil {
		t.Fatalf("schalter setzen: %v", err)
	}
	path := writeOnboard(t, dir)

	svc.Run()

	if n := countServers(t, servers); n != 0 {
		t.Errorf("trotz abschalter %d server angelegt", n)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("uebergabedatei muss auch dann geloescht werden, wenn nichts angelegt wird")
	}
}

// TestSelfRegisterSkipsInContainer: Laeuft LCM selbst im Container, ist
// „localhost" der Container und nicht der Host - ein Eintrag waere
// irrefuehrend und alle Host-Aktionen liefen ins Leere. Der Fall wurde bisher
// nur zufaellig mitgeprueft (naemlich dann, wenn die Tests selbst in einem
// Container liefen); hier steht er ausdruecklich.
func TestSelfRegisterSkipsInContainer(t *testing.T) {
	svc, servers, _, dir := selfTestEnv(t)
	svc.containerCheck = func() bool { return true }
	path := writeOnboard(t, dir)

	svc.Run()

	if n := countServers(t, servers); n != 0 {
		t.Errorf("im container wurden %d server angelegt", n)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("uebergabedatei blieb liegen - der klartext-schluessel muss auch hier weg")
	}
}

// TestSelfRegisterSkipsExistingHost deckt „schon aufgenommen" ab - erkannt
// ueber IsLcmHost, nicht ueber den Namen: Ein manuell unter anderem Namen
// aufgenommener localhost darf keinen Doppeleintrag ausloesen.
func TestSelfRegisterSkipsExistingHost(t *testing.T) {
	svc, servers, _, dir := selfTestEnv(t)
	existing := &domain.Server{
		Name: "mein-server", Host: "127.0.0.1", SSHPort: 22,
		ServiceUser: "lcm-svc", HostKeyFingerprint: "SHA256:x", PrivateKeyEnc: "x",
	}
	if err := servers.Create(existing); err != nil {
		t.Fatalf("server anlegen: %v", err)
	}
	writeOnboard(t, dir)

	svc.Run()

	if n := countServers(t, servers); n != 1 {
		t.Errorf("erwartet 1 server (kein doppelter), bekommen %d", n)
	}
}

// TestSelfRegisterWithoutFileIsNoop: der Regelfall bei jedem Start nach dem
// ersten. Es darf weder etwas angelegt werden noch ein Fehler entstehen.
func TestSelfRegisterWithoutFileIsNoop(t *testing.T) {
	svc, servers, _, _ := selfTestEnv(t)

	svc.Run()

	if n := countServers(t, servers); n != 0 {
		t.Errorf("ohne uebergabedatei wurden %d server angelegt", n)
	}
}

// TestSelfRegisterRemovesFileOnBadInput: Auch bei kaputter Uebergabedatei muss
// der Klartext-Schluessel verschwinden - und der Versuch darf sich nicht bei
// jedem Start wiederholen.
func TestSelfRegisterRemovesFileOnBadInput(t *testing.T) {
	svc, servers, _, dir := selfTestEnv(t)
	path := filepath.Join(dir, SelfOnboardFileName)
	if err := os.WriteFile(path, []byte("kein json"), 0o600); err != nil {
		t.Fatalf("datei schreiben: %v", err)
	}

	svc.Run()

	if n := countServers(t, servers); n != 0 {
		t.Errorf("aus kaputter datei wurden %d server angelegt", n)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("kaputte uebergabedatei blieb liegen")
	}
}

// TestSelfRegisterSkipsWhenSSHUnreachable: Ohne erreichbaren SSH-Dienst gibt es
// nichts aufzunehmen. Der Dienst muss trotzdem sauber weiterlaufen.
func TestSelfRegisterSkipsWhenSSHUnreachable(t *testing.T) {
	_, servers, settings, dir := selfTestEnv(t)
	cipher, _ := crypto.NewCipher(make([]byte, 32))
	svc := newSelfSvc(servers, settings,
		fakeSelfDialer{err: os.ErrDeadlineExceeded}, cipher, dir)
	path := writeOnboard(t, dir)

	svc.Run()

	if n := countServers(t, servers); n != 0 {
		t.Errorf("ohne ssh wurden %d server angelegt", n)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("uebergabedatei blieb nach fehlgeschlagenem versuch liegen")
	}
}

// TestDecommissionLcmHostDisablesSelfRegistration schliesst die Luecke
// zwischen Loeschen und Wiederkommen: Der Vermerk wird beim ENTFERNEN des
// LCM-Hosts gesetzt. Ohne diesen Test waere nur geprueft, dass der Schalter
// WIRKT - nicht, dass ihn ueberhaupt jemand umlegt. Das Installationsskript
// legt die Uebergabedatei bei jedem Lauf neu an; ohne den Vermerk kaeme der
// Eintrag beim naechsten Paket-Update zurueck.
func TestDecommissionLcmHostDisablesSelfRegistration(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("test-db: %v", err)
	}
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	if err := db.AutoMigrate(&domain.Server{}, &domain.GlobalSettings{}, &domain.Job{},
		&domain.Package{}, &domain.SnapPackage{}, &domain.DockerContainer{},
		&domain.DockerImage{}, &domain.Vulnerability{}, &domain.StorageHistory{},
		&domain.AptRepository{}, &domain.ServerGroup{}, &domain.LinuxUser{},
		&domain.SSHSession{}, &domain.SSHCommand{}, &domain.AuditLog{},
		&domain.PendingUserSync{}, &domain.AdvisoryFinding{}); err != nil {
		t.Fatalf("migration: %v", err)
	}
	servers := repositories.NewServerRepository(db)

	called := false
	svc := NewServerService(servers, nil,
		NewAuditService(repositories.NewAuditRepository(db)), nil, nil).
		WithSelfRegisterOff(func() error { called = true; return nil })

	host := &domain.Server{
		Name: SelfHostName, Host: "localhost", SSHPort: 22, ServiceUser: "lcm-svc",
		HostKeyFingerprint: "SHA256:x", PrivateKeyEnc: "x", IsDemo: true,
	}
	if err := servers.Create(host); err != nil {
		t.Fatalf("host anlegen: %v", err)
	}

	if _, err := svc.Decommission(repositories.ScopeAll(), host.ID, "admin",
		DecommissionOptions{}); err != nil {
		t.Fatalf("decommission: %v", err)
	}
	if !called {
		t.Error("beim loeschen des LCM-Hosts wurde die selbstaufnahme NICHT abgeschaltet - " +
			"der eintrag kaeme beim naechsten paket-update zurueck")
	}
}

// TestDecommissionOtherServerKeepsSelfRegistration: Das Abschalten darf NUR
// beim LCM-Host greifen - sonst wuerde das Entfernen eines beliebigen Servers
// die Selbstaufnahme stilllegen.
func TestDecommissionOtherServerKeepsSelfRegistration(t *testing.T) {
	db, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	if err := db.AutoMigrate(&domain.Server{}, &domain.GlobalSettings{}, &domain.Job{},
		&domain.Package{}, &domain.SnapPackage{}, &domain.DockerContainer{},
		&domain.DockerImage{}, &domain.Vulnerability{}, &domain.StorageHistory{},
		&domain.AptRepository{}, &domain.ServerGroup{}, &domain.LinuxUser{},
		&domain.SSHSession{}, &domain.SSHCommand{}, &domain.AuditLog{},
		&domain.PendingUserSync{}, &domain.AdvisoryFinding{}); err != nil {
		t.Fatalf("migration: %v", err)
	}
	servers := repositories.NewServerRepository(db)

	called := false
	svc := NewServerService(servers, nil,
		NewAuditService(repositories.NewAuditRepository(db)), nil, nil).
		WithSelfRegisterOff(func() error { called = true; return nil })

	other := &domain.Server{
		Name: "web01", Host: "10.0.0.11", SSHPort: 22, ServiceUser: "lcm-svc",
		HostKeyFingerprint: "SHA256:y", PrivateKeyEnc: "y", IsDemo: true,
	}
	if err := servers.Create(other); err != nil {
		t.Fatalf("server anlegen: %v", err)
	}

	if _, err := svc.Decommission(repositories.ScopeAll(), other.ID, "admin",
		DecommissionOptions{}); err != nil {
		t.Fatalf("decommission: %v", err)
	}
	if called {
		t.Error("das entfernen eines fremden servers hat die selbstaufnahme abgeschaltet")
	}
}
