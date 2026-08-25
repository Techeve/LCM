package storage

import (
	"testing"

	"gorm.io/gorm"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/crypto"
	"LCM/internal/storage/repositories"
)

// setupEncDB liefert eine migrierte In-Memory-DB mit aktivem At-Rest-Cipher.
func setupEncDB(t *testing.T) (*gorm.DB, *crypto.Cipher) {
	t.Helper()
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	cipher, _ := crypto.NewCipher(crypto.GenerateKey())
	SetFieldCipher(cipher)
	t.Cleanup(func() { SetFieldCipher(nil) })
	return db, cipher
}

func rawCol(t *testing.T, db *gorm.DB, table, col string, id any) string {
	t.Helper()
	var v string
	if err := db.Raw("SELECT "+col+" FROM "+table+" WHERE id = ?", id).Scan(&v).Error; err != nil {
		t.Fatalf("roh %s.%s lesen: %v", table, col, err)
	}
	return v
}

// TestUserFieldEncryption: Username, E-Mail, Vor-/Nachname und Passwort-Hash
// liegen at rest verschlüsselt; Login/E-Mail-Suche laufen über den Blindindex;
// die Eindeutigkeit greift weiterhin.
func TestUserFieldEncryption(t *testing.T) {
	db, _ := setupEncDB(t)
	u := &domain.User{
		Username: "alice", Email: "Alice@Example.com", PasswordHash: "$argon2id$hash",
		FirstName: "Alice", LastName: "Anders",
	}
	if err := db.Create(u).Error; err != nil {
		t.Fatal(err)
	}

	for _, col := range []struct{ name, plain string }{
		{"username", "alice"}, {"email", "Alice@Example.com"},
		{"password_hash", "$argon2id$hash"}, {"first_name", "Alice"}, {"last_name", "Anders"},
	} {
		if raw := rawCol(t, db, "users", col.name, u.ID); raw == col.plain || raw == "" {
			t.Errorf("users.%s ist nicht verschlüsselt: %q", col.name, raw)
		}
	}
	// Blindindex vorhanden und != Klartext.
	if b := rawCol(t, db, "users", "username_bidx", u.ID); b == "alice" || b == "" {
		t.Errorf("username_bidx sollte HMAC sein, ist %q", b)
	}

	repo := repositories.NewUserRepository(db)
	got, err := repo.FindByUsername("alice")
	if err != nil || got.Username != "alice" || got.Email != "Alice@Example.com" {
		t.Fatalf("FindByUsername: %+v / %v", got, err)
	}
	// E-Mail-Suche case-insensitiv.
	if g, err := repo.FindByEmail("alice@example.com"); err != nil || g.ID != u.ID {
		t.Fatalf("FindByEmail (case-insensitiv): %v", err)
	}
	// Eindeutigkeit: gleicher Username → Unique-Verletzung.
	if err := db.Create(&domain.User{Username: "alice", PasswordHash: "x"}).Error; err == nil {
		t.Error("doppelter Username sollte am Unique-Index scheitern")
	}
}

// TestUsersWithoutEmailNoCollision: zwei Benutzer ohne E-Mail dürfen sich am
// partiellen Unique-Index NICHT gegenseitig blockieren (email_bidx muss ""
// sein, nicht HMAC("")).
func TestUsersWithoutEmailNoCollision(t *testing.T) {
	db, _ := setupEncDB(t)
	if err := db.Create(&domain.User{Username: "u1", PasswordHash: "h"}).Error; err != nil {
		t.Fatalf("erster User ohne E-Mail: %v", err)
	}
	if err := db.Create(&domain.User{Username: "u2", PasswordHash: "h"}).Error; err != nil {
		t.Fatalf("zweiter User ohne E-Mail sollte gehen (Kollision am email_bidx?): %v", err)
	}
	var bidx string
	db.Raw("SELECT email_bidx FROM users WHERE username_bidx = ?", repositories.BlindIndex("u1")).Scan(&bidx)
	if bidx != "" {
		t.Errorf("email_bidx bei leerer E-Mail sollte \"\" sein, ist %q", bidx)
	}
}

// TestLinuxUserFieldEncryption: Username verschlüsselt, Suche über Blindindex.
func TestLinuxUserFieldEncryption(t *testing.T) {
	db, _ := setupEncDB(t)
	lu := &domain.LinuxUser{Username: "deploy", FullName: "Deploy Bot", Email: "d@x.de"}
	if err := db.Create(lu).Error; err != nil {
		t.Fatal(err)
	}
	if raw := rawCol(t, db, "linux_users", "username", lu.ID); raw == "deploy" || raw == "" {
		t.Errorf("linux_users.username nicht verschlüsselt: %q", raw)
	}
	got, err := repositories.NewLinuxUserRepository(db).FindByUsername("deploy")
	if err != nil || got.Username != "deploy" || got.FullName != "Deploy Bot" {
		t.Fatalf("FindByUsername: %+v / %v", got, err)
	}
}

// TestServerFirewallEncryption: Firewall-/Port-Felder liegen at rest
// verschlüsselt, werden aber transparent im Klartext gelesen.
func TestServerFirewallEncryption(t *testing.T) {
	db, _ := setupEncDB(t)
	srv := &domain.Server{
		Name: "web01", Host: "10.0.0.1", ServiceUser: "lcm-svc", HostKeyFingerprint: "SHA256:x",
		PrivateKeyEnc: "x", FirewallTool: "nftables", FirewallAllowedPorts: "80,443",
		FirewallRules: `[{"port":443}]`, ListeningPorts: `[{"port":22,"proto":"tcp"}]`,
	}
	if err := db.Create(srv).Error; err != nil {
		t.Fatal(err)
	}
	for _, col := range []struct{ name, plain string }{
		{"firewall_tool", "nftables"}, {"firewall_allowed_ports", "80,443"},
		{"listening_ports", `[{"port":22,"proto":"tcp"}]`},
	} {
		if raw := rawCol(t, db, "servers", col.name, srv.ID); raw == col.plain || raw == "" {
			t.Errorf("servers.%s nicht verschlüsselt: %q", col.name, raw)
		}
	}
	var got domain.Server
	if err := db.First(&got, srv.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.ListeningPorts != `[{"port":22,"proto":"tcp"}]` || got.FirewallTool != "nftables" {
		t.Fatalf("entschlüsselt: ports=%q tool=%q", got.ListeningPorts, got.FirewallTool)
	}
}

// TestServerProfileEncryption: OS/Kernel/CPU liegen at rest verschlüsselt,
// werden aber transparent gelesen (OSID-abhängige Logik bleibt intakt).
func TestServerProfileEncryption(t *testing.T) {
	db, _ := setupEncDB(t)
	srv := &domain.Server{
		Name: "p1", Host: "h", ServiceUser: "lcm-svc", HostKeyFingerprint: "f", PrivateKeyEnc: "k",
		OSName: "Debian GNU/Linux", OSVersion: "12 (bookworm)", OSID: "debian",
		KernelVersion: "6.1.0-13-amd64", CPUModel: "Intel Xeon", InstalledKernels: `["6.1.0-13"]`,
	}
	if err := db.Create(srv).Error; err != nil {
		t.Fatal(err)
	}
	for _, col := range []struct{ name, plain string }{
		{"os_name", "Debian GNU/Linux"}, {"os_id", "debian"},
		{"kernel_version", "6.1.0-13-amd64"}, {"cpu_model", "Intel Xeon"},
	} {
		if raw := rawCol(t, db, "servers", col.name, srv.ID); raw == col.plain || raw == "" {
			t.Errorf("servers.%s nicht verschlüsselt: %q", col.name, raw)
		}
	}
	var got domain.Server
	if err := db.First(&got, srv.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.OSName != "Debian GNU/Linux" || got.OSID != "debian" || got.KernelVersion != "6.1.0-13-amd64" {
		t.Fatalf("entschlüsselt: os=%q id=%q kernel=%q", got.OSName, got.OSID, got.KernelVersion)
	}
}

// TestServerProfileBackfill: bestehende Klartext-Profilfelder werden durch
// EncryptServerProfileFields nachträglich verschlüsselt (idempotent).
func TestServerProfileBackfill(t *testing.T) {
	db, _ := setupEncDB(t)
	// Legacy-Server roh (am Serializer vorbei) mit Klartext-os_name.
	srv := &domain.Server{Name: "leg", Host: "h", ServiceUser: "lcm-svc", HostKeyFingerprint: "f", PrivateKeyEnc: "k"}
	if err := db.Create(srv).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("UPDATE servers SET os_name = ?, kernel_version = ? WHERE id = ?",
		"Ubuntu", "5.15.0", srv.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := EncryptServerProfileFields(db); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if raw := rawCol(t, db, "servers", "os_name", srv.ID); raw == "Ubuntu" || raw == "" {
		t.Errorf("os_name nach Backfill nicht verschlüsselt: %q", raw)
	}
	var got domain.Server
	db.First(&got, srv.ID)
	if got.OSName != "Ubuntu" || got.KernelVersion != "5.15.0" {
		t.Fatalf("nach Backfill entschlüsselt: os=%q kernel=%q", got.OSName, got.KernelVersion)
	}
	// Zweiter Lauf ist idempotent (überspringt bereits verschlüsselte Werte).
	if err := EncryptServerProfileFields(db); err != nil {
		t.Fatalf("zweiter Backfill: %v", err)
	}
	db.First(&got, srv.ID)
	if got.OSName != "Ubuntu" {
		t.Fatalf("Idempotenz verletzt: os=%q", got.OSName)
	}
}

// TestVulnPackageTokenization: vulnerabilities/packages führen KEINE
// Klartext-server_id mehr, sondern server_ref (HMAC); Repo-Abfragen und die
// globale CVE-Sicht (server_id per JOIN) funktionieren weiter.
func TestVulnPackageTokenization(t *testing.T) {
	db, _ := setupEncDB(t)
	// server_id-Spalte ist in beiden Tabellen entfernt.
	if columnExists(db, "vulnerabilities", "server_id") || columnExists(db, "packages", "server_id") {
		t.Fatal("server_id sollte in vulnerabilities/packages entfernt sein")
	}
	repo := repositories.NewServerRepository(db)
	srv := &domain.Server{Name: "db01", Host: "10.0.0.2", ServiceUser: "lcm-svc", HostKeyFingerprint: "SHA256:y", PrivateKeyEnc: "y"}
	if err := db.Create(srv).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplacePackages(srv.ID, []domain.Package{{Name: "openssl", Version: "3.0.11"}}); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceVulnerabilities(srv.ID, domain.VulnSourceOS, []domain.Vulnerability{
		{CVEID: "CVE-2023-0286", PackageName: "openssl", Severity: domain.SeverityCritical},
	}); err != nil {
		t.Fatal(err)
	}
	// Die Rohspalte server_ref entspricht dem HMAC-Token des Servers.
	wantRef := repositories.ServerRef(srv.ID)
	var pkgRef, vulnRef string
	db.Raw("SELECT server_ref FROM packages LIMIT 1").Scan(&pkgRef)
	db.Raw("SELECT server_ref FROM vulnerabilities LIMIT 1").Scan(&vulnRef)
	if pkgRef != wantRef || vulnRef != wantRef {
		t.Fatalf("server_ref falsch: pkg=%q vuln=%q want=%q", pkgRef, vulnRef, wantRef)
	}
	// Repo-Abfragen je Server.
	if pkgs, err := repo.FindPackages(srv.ID); err != nil || len(pkgs) != 1 {
		t.Fatalf("FindPackages: %d / %v", len(pkgs), err)
	}
	if vulns, err := repo.FindVulnerabilities(srv.ID); err != nil || len(vulns) != 1 {
		t.Fatalf("FindVulnerabilities: %d / %v", len(vulns), err)
	}
	// Globale CVE-Sicht: server_id kommt per JOIN auf servers.id zurück.
	rows, err := repo.GlobalVulnerabilities(repositories.ScopeAll())
	if err != nil || len(rows) != 1 || rows[0].ServerID != srv.ID {
		t.Fatalf("GlobalVulnerabilities: %+v / %v", rows, err)
	}
	if rows[0].ServerName != "db01" {
		t.Errorf("ServerName aus JOIN = %q, erwartet db01", rows[0].ServerName)
	}
	// Server löschen räumt Pakete + CVEs mit weg.
	if err := repo.Delete(srv.ID); err != nil {
		t.Fatal(err)
	}
	var nP, nV int64
	db.Raw("SELECT COUNT(*) FROM packages").Scan(&nP)
	db.Raw("SELECT COUNT(*) FROM vulnerabilities").Scan(&nV)
	if nP != 0 || nV != 0 {
		t.Errorf("nach Server-Delete: packages=%d vulns=%d (erwartet 0/0)", nP, nV)
	}
}

// TestUserBackfillMigration: eine vor der Verschlüsselung angelegte (Klartext-)
// Benutzerzeile ohne Blindindex wird durch die self-healing-Migration
// verschlüsselt und indexiert.
func TestUserBackfillMigration(t *testing.T) {
	db, _ := setupEncDB(t)
	// Legacy-Zeile roh (am Serializer vorbei) mit Klartext + leerem bidx.
	if err := db.Exec("INSERT INTO users (username, email, password_hash, username_bidx, email_bidx, active) VALUES (?,?,?,?,?,1)",
		"legacy", "legacy@x.de", "$argon2id$old", "", "").Error; err != nil {
		t.Fatal(err)
	}
	if err := migrateEncryptUserFields(db); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	var raw string
	db.Raw("SELECT username FROM users WHERE username_bidx = ?", repositories.BlindIndex("legacy")).Scan(&raw)
	if raw == "legacy" || raw == "" {
		t.Errorf("Backfill hat username nicht verschlüsselt: %q", raw)
	}
	if got, err := repositories.NewUserRepository(db).FindByUsername("legacy"); err != nil || got.Email != "legacy@x.de" {
		t.Fatalf("FindByUsername nach Backfill: %+v / %v", got, err)
	}
}

// TestTokenizeBackfillMigration: eine Kind-Zeile mit noch vorhandener
// Klartext-server_id-Spalte wird auf server_ref umgestellt, die Spalte entfernt.
func TestTokenizeBackfillMigration(t *testing.T) {
	db, _ := setupEncDB(t)
	srv := &domain.Server{Name: "s1", Host: "h", ServiceUser: "lcm-svc", HostKeyFingerprint: "f", PrivateKeyEnc: "k"}
	if err := db.Create(srv).Error; err != nil {
		t.Fatal(err)
	}
	// Legacy-Zustand simulieren: server_id-Spalte wieder anlegen + Zeile mit
	// leerem server_ref und gesetztem server_id.
	if err := db.Exec("ALTER TABLE packages ADD COLUMN server_id INTEGER").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO packages (id, name, version, server_ref, server_id) VALUES (?,?,?,?,?)",
		"pkg-legacy", "curl", "8.0", "", srv.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := migrateTokenizeServerRefs(db); err != nil {
		t.Fatalf("tokenize backfill: %v", err)
	}
	if columnExists(db, "packages", "server_id") {
		t.Error("server_id sollte nach dem Backfill entfernt sein")
	}
	var ref string
	db.Raw("SELECT server_ref FROM packages WHERE id = ?", "pkg-legacy").Scan(&ref)
	if ref != repositories.ServerRef(srv.ID) {
		t.Errorf("server_ref nicht befüllt: %q != %q", ref, repositories.ServerRef(srv.ID))
	}
}

// TestRotationCarriesEncryptionAndRefs: Master-Key-Rotation nimmt die neuen
// verschlüsselten Felder, die Blindindizes UND server_ref mit - Login und
// CVE-Abfragen funktionieren danach mit dem neuen Schlüssel.
func TestRotationCarriesEncryptionAndRefs(t *testing.T) {
	db, oldCipher := setupEncDB(t)
	if err := db.Create(&domain.User{Username: "bob", PasswordHash: "$h"}).Error; err != nil {
		t.Fatal(err)
	}
	repo := repositories.NewServerRepository(db)
	// PrivateKeyEnc muss echter (mit oldCipher verschlüsselter) Ciphertext sein -
	// die strikte Rotationspassage entschlüsselt ihn mit dem alten Key.
	pkEnc, _ := oldCipher.EncryptString("PEM-PRIVATE-KEY")
	srv := &domain.Server{Name: "srv", Host: "h", ServiceUser: "lcm-svc", HostKeyFingerprint: "f", PrivateKeyEnc: pkEnc}
	if err := db.Create(srv).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceVulnerabilities(srv.ID, domain.VulnSourceOS, []domain.Vulnerability{
		{CVEID: "CVE-x", PackageName: "p", Severity: domain.SeverityHigh},
	}); err != nil {
		t.Fatal(err)
	}

	newCipher, _ := crypto.NewCipher(crypto.GenerateKey())
	if err := RotateEncryptedFields(db, oldCipher, newCipher); err != nil {
		t.Fatalf("rotation: %v", err)
	}
	SetFieldCipher(newCipher) // ab jetzt neuer Schlüssel (Blindindex/Ref neu abgeleitet)

	if got, err := repositories.NewUserRepository(db).FindByUsername("bob"); err != nil || got.ID == 0 {
		t.Fatalf("Login nach Rotation gebrochen: %v", err)
	}
	if vulns, err := repo.FindVulnerabilities(srv.ID); err != nil || len(vulns) != 1 {
		t.Fatalf("CVE-Abfrage nach Rotation gebrochen: %d / %v", len(vulns), err)
	}
	if rows, err := repo.GlobalVulnerabilities(repositories.ScopeAll()); err != nil || len(rows) != 1 || rows[0].ServerID != srv.ID {
		t.Fatalf("globale CVE-Sicht nach Rotation gebrochen: %+v / %v", rows, err)
	}
}
