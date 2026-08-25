package services_test

import (
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"

	"LCM/internal/config"
	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/infrastructure/advisories"
	"LCM/internal/infrastructure/crypto"
	"LCM/internal/infrastructure/registry"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/infrastructure/trivy"
	"LCM/internal/storage"
	"LCM/internal/storage/repositories"
)

// testEnv baut eine frische In-Memory-Datenbank mit Seeding und allen
// Services - das Standard-Setup für Integrationstests in diesem Template.
type testEnv struct {
	Auth          *services.AuthService
	Users         *services.UserService
	APIKeys       *services.APIKeyService
	Servers       *services.ServerService
	Jobs          *services.JobService
	Groups        *services.GroupService
	Scheduler     *services.Scheduler
	Executor      *services.Executor
	Prov          *services.ProvisioningService
	Pending       *repositories.PendingUserSyncRepository
	LinuxUsers    *services.LinuxUserService
	Activation    *services.ActivationService
	SSHLogs       *services.SSHLogService
	CustomActions *services.CustomActionService
	Profiles      *services.PrivilegeProfileService
	Blocks        *services.ProfileBlockService
	Apps          *services.AppService
	Settings      *services.SettingsService
	Advisories    *services.AdvisoryService
	AdvSource     *advisories.Fake
	AdvLocal      *advisories.LocalOSV
	Dialer        *sshx.FakeDialer
	Scanner       *trivy.Fake
	Registry      *registry.Fake
	db            *gorm.DB
}

// DB gibt die rohe Datenbank-Verbindung für Assertions frei.
func (e *testEnv) DB() *gorm.DB { return e.db }

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	// Die Rückkehr-Überwachung nach einem Neustart wartet in Echtzeit
	// (Anlaufsperre 15s, Zeitfenster 10min). Für Tests auf Millisekunden
	// herunterdrehen - geprüft wird das Verhalten, nicht die Uhr. Einzelne
	// Tests setzen die Werte danach nach Bedarf erneut.
	settle, ping, maxWait := services.RebootSettleDelay, services.RebootPingInterval, services.RebootMaxWait
	services.RebootSettleDelay = time.Millisecond
	services.RebootPingInterval = time.Millisecond
	services.RebootMaxWait = 200 * time.Millisecond
	t.Cleanup(func() {
		services.RebootSettleDelay, services.RebootPingInterval, services.RebootMaxWait = settle, ping, maxWait
	})

	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("db öffnen: %v", err)
	}
	if err := storage.Migrate(db); err != nil {
		t.Fatalf("migrieren: %v", err)
	}
	cfg := &config.Config{
		AdminInitialPassword:  "test-admin-passwort",
		DemoMode:              false, // Testdaten legen die Tests selbst an
		AccessTokenTTLMinutes: 60,
	}
	if err := storage.Seed(db, cfg); err != nil {
		t.Fatalf("seeden: %v", err)
	}

	userRepo := repositories.NewUserRepository(db)
	roleRepo := repositories.NewRoleRepository(db)

	cipher, err := crypto.NewCipher(crypto.GenerateKey())
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	audit := services.NewAuditService(repositories.NewAuditRepository(db))
	jobs := services.NewJobService(repositories.NewJobRepository(db)).WithAudit(audit)
	dialer := sshx.NewFakeDialer()
	serverRepo := repositories.NewServerRepository(db)
	groupRepo := repositories.NewGroupRepository(db)
	settingsRepo := repositories.NewSettingsRepository(db)
	linuxRepo := repositories.NewLinuxUserRepository(db)
	sshLogRepo := repositories.NewSSHLogRepository(db)
	recorder := services.NewSSHRecorder(sshLogRepo)
	scanner := &trivy.Fake{IsAvailable: true}
	knownRepoRepo := repositories.NewKnownRepoRepository(db)
	ipAllowlistRepo := repositories.NewIPAllowlistRepository(db)
	settingsSvc := services.NewSettingsService(settingsRepo, cipher, audit, func() error { return nil }).
		WithKnownRepos(knownRepoRepo).WithIPAllowlists(ipAllowlistRepo).WithRoles(roleRepo)
	// Löschsperre für referenzierte Allowlists (R2-072) - wie in main.go.
	settingsSvc.WithIPAllowlistUsage(func(id uint) []string {
		return services.IPAllowlistUsage(serverRepo, groupRepo, id)
	})
	servers := services.NewServerService(serverRepo, jobs, audit, cipher, dialer).WithRecorder(recorder).WithLinux(linuxRepo).WithGroups(groupRepo).WithScanner(scanner).
		WithOnboardingKey(settingsSvc.OnboardingPrivateKey).WithKnownRepos(knownRepoRepo).WithIPAllowlists(settingsSvc.ExpandIPAllowlists).
		WithAptCacheURL(func() (string, error) {
			st, err := settingsRepo.Get()
			if err != nil {
				return "", err
			}
			return st.AptCacheURL, nil
		})

	// Betriebsart festnageln: Die Tests prüfen die LCM-Host-Aktionen, die es
	// nur auf einem Host gibt. Ohne diese Zeile entschiede der Ausführungsort
	// über das Ergebnis. Den Container-Zweig prüft lcm_host_container_test.go
	// mit eigener Naht.
	servers.SetContainerCheckForTest(func() bool { return false })

	pendingRepo := repositories.NewPendingUserSyncRepository(db)
	profileRepo := repositories.NewPrivilegeProfileRepository(db)
	prov := services.NewProvisioningService(linuxRepo, serverRepo, cipher, servers.Connect).
		WithAudit(audit).WithRecorder(recorder).WithPendingUserSyncs(pendingRepo).
		WithSSHLog(sshLogRepo).WithProfiles(profileRepo)
	backups := services.NewBackupService(db, settingsRepo, t.TempDir(), ":memory:", "").WithCipher(cipher)
	customActionRepo := repositories.NewCustomActionRepository(db)
	reg := &registry.Fake{Results: map[string]registry.Result{}}
	advSource := &advisories.Fake{IsAvailable: true}
	advLocal := advisories.NewLocalOSV(t.TempDir(), "")
	advisoryService := services.NewAdvisoryService(serverRepo,
		repositories.NewAdvisoryRepository(db), repositories.NewAdvisoryCacheRepository(db),
		settingsRepo, advSource).WithLocalSource(advLocal).WithScanCacheStats(scanner.CacheStats)
	executor := services.NewExecutor(serverRepo, groupRepo, jobs, audit, prov, backups, settingsRepo, servers.Connect).WithRecorder(recorder).WithCustomActions(customActionRepo).WithScanner(scanner).WithRegistry(reg).WithIPAllowlists(settingsSvc.ExpandIPAllowlists).WithAdvisories(advisoryService).WithProfiles(profileRepo)
	scheduler := services.NewScheduler(groupRepo, settingsRepo, executor)
	groups := services.NewGroupService(groupRepo, serverRepo, audit, scheduler.Reload).
		WithCustomActions(customActionRepo).WithUsers(userRepo).WithProvisioning(prov)
	customActions := services.NewCustomActionService(customActionRepo, audit)
	profiles := services.NewPrivilegeProfileService(profileRepo, audit)
	blocks := services.NewProfileBlockService(repositories.NewProfileBlockRepository(db), audit)
	appRepo := repositories.NewAppRepository(db)
	apps := services.NewAppService(appRepo, customActionRepo, audit)
	executor.WithApps(apps)
	servers.WithApps(appRepo)
	linuxUsers := services.NewLinuxUserService(linuxRepo, groupRepo, audit, cipher, prov).WithProfiles(profileRepo)
	activation := services.NewActivationService(repositories.NewActivationRepository(db), userRepo, audit)

	return &testEnv{
		Auth:          services.NewAuthService(userRepo, "test-secret-mit-mindestens-32-zeichen!!", time.Hour),
		Users:         services.NewUserService(userRepo, roleRepo).WithAudit(audit),
		APIKeys:       services.NewAPIKeyService(repositories.NewAPIKeyRepository(db)).WithAudit(audit),
		Servers:       servers,
		Jobs:          jobs,
		Groups:        groups,
		Scheduler:     scheduler,
		Executor:      executor,
		Prov:          prov,
		Pending:       pendingRepo,
		LinuxUsers:    linuxUsers,
		Activation:    activation,
		SSHLogs:       services.NewSSHLogService(sshLogRepo, serverRepo, repositories.NewJobRepository(db)),
		CustomActions: customActions,
		Profiles:      profiles,
		Blocks:        blocks,
		Apps:          apps,
		Settings:      settingsSvc,
		Advisories:    advisoryService,
		AdvSource:     advSource,
		AdvLocal:      advLocal,
		Dialer:        dialer,
		Scanner:       scanner,
		Registry:      reg,
		db:            db,
	}
}

func TestLoginSuccess(t *testing.T) {
	env := newTestEnv(t)

	token, user, err := env.Auth.Login("admin", "test-admin-passwort")
	if err != nil {
		t.Fatalf("login fehlgeschlagen: %v", err)
	}
	if token == "" {
		t.Fatal("kein Token erhalten")
	}
	if user.Username != domain.AdminUsername {
		t.Errorf("falscher User: %s", user.Username)
	}
	if !user.HasPermission(domain.PermServersWrite) {
		t.Error("admin sollte servers:write besitzen")
	}
}

func TestLoginWrongPassword(t *testing.T) {
	env := newTestEnv(t)

	_, _, err := env.Auth.Login("admin", "falsches-passwort")
	if !errors.Is(err, services.ErrInvalidCredentials) {
		t.Fatalf("erwartet ErrInvalidCredentials, bekam: %v", err)
	}
}

func TestLoginUnknownUserSameError(t *testing.T) {
	env := newTestEnv(t)

	// Unbekannter User und falsches Passwort müssen denselben Fehler
	// liefern (kein User-Enumeration-Leak).
	_, _, err := env.Auth.Login("gibt-es-nicht", "egal")
	if !errors.Is(err, services.ErrInvalidCredentials) {
		t.Fatalf("erwartet ErrInvalidCredentials, bekam: %v", err)
	}
}

func TestSystemUserCannotLogin(t *testing.T) {
	env := newTestEnv(t)

	_, _, err := env.Auth.Login("system", "irgendwas")
	if !errors.Is(err, services.ErrInvalidCredentials) {
		t.Fatalf("system-User darf sich nicht einloggen, bekam: %v", err)
	}
}

func TestTokenRoundtrip(t *testing.T) {
	env := newTestEnv(t)

	token, _, err := env.Auth.Login("admin", "test-admin-passwort")
	if err != nil {
		t.Fatal(err)
	}
	user, err := env.Auth.ValidateToken(token)
	if err != nil {
		t.Fatalf("gültiges Token abgelehnt: %v", err)
	}
	if user.Username != domain.AdminUsername {
		t.Errorf("falscher User aus Token: %s", user.Username)
	}
}

func TestInvalidTokenRejected(t *testing.T) {
	env := newTestEnv(t)

	if _, err := env.Auth.ValidateToken("kaputt.token.hier"); !errors.Is(err, services.ErrInvalidToken) {
		t.Fatalf("erwartet ErrInvalidToken, bekam: %v", err)
	}
}

// authTestUser legt in einer frischen In-Memory-DB einen aktiven User an und
// liefert Repo + User - Basis für die Session-Invalidierungstests.
func authTestUser(t *testing.T) (*repositories.UserRepository, *domain.User) {
	t.Helper()
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.Migrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repositories.NewUserRepository(db)
	hash, err := services.HashPassword("egal-hauptsache-lang")
	if err != nil {
		t.Fatal(err)
	}
	u := &domain.User{Username: "chef", PasswordHash: hash, Active: true}
	if err := repo.Create(u); err != nil {
		t.Fatal(err)
	}
	return repo, u
}

// TestSessionsInvalidatedOnRestart bildet den gemeldeten Angriff ab: gleiches
// jwt_secret, gleiche DB/User-ID, aber ein neuer Prozess (= neue AuthService-
// Instanz). Ein vor dem "Neustart" ausgestelltes Token darf danach NICHT mehr
// akzeptiert werden, weil der Signaturschlüssel an den Prozessstart gebunden ist.
func TestSessionsInvalidatedOnRestart(t *testing.T) {
	repo, u := authTestUser(t)
	const secret = "test-secret-mit-mindestens-32-zeichen!!"

	first := services.NewAuthService(repo, secret, time.Hour)
	token, err := first.IssueToken(u)
	if err != nil {
		t.Fatal(err)
	}
	// Dieselbe Instanz akzeptiert das eigene Token.
	if _, err := first.ValidateToken(token); err != nil {
		t.Fatalf("gültiges Token in derselben Instanz abgelehnt: %v", err)
	}

	// "Neustart": neue Instanz, IDENTISCHES Secret, dieselbe DB.
	second := services.NewAuthService(repo, secret, time.Hour)
	if _, err := second.ValidateToken(token); !errors.Is(err, services.ErrInvalidToken) {
		t.Fatalf("Alt-Token muss nach Neustart abgelehnt werden, bekam: %v", err)
	}
	// Ein frisch ausgestelltes Token der neuen Instanz funktioniert normal.
	fresh, err := second.IssueToken(u)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := second.ValidateToken(fresh); err != nil {
		t.Fatalf("frisches Token der neuen Instanz abgelehnt: %v", err)
	}
}

// TestTokenRejectedBeforePasswordChangedAt sichert die zweite Verteidigungs-
// linie: Tokens, die vor der als Basislinie gesetzten Passwortänderungszeit
// ausgestellt wurden, werden auch innerhalb derselben Instanz abgelehnt.
func TestTokenRejectedBeforePasswordChangedAt(t *testing.T) {
	repo, u := authTestUser(t)
	auth := services.NewAuthService(repo, "test-secret-mit-mindestens-32-zeichen!!", time.Hour)

	token, err := auth.IssueToken(u)
	if err != nil {
		t.Fatal(err)
	}
	// Passwortänderung NACH der Token-Ausstellung datieren.
	if err := repo.UpdateFields(u.ID, map[string]any{
		"password_changed_at": time.Now().Add(time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.ValidateToken(token); !errors.Is(err, services.ErrInvalidToken) {
		t.Fatalf("Token von vor der Passwortänderung muss abgelehnt werden, bekam: %v", err)
	}
}

func TestCreateUserHashesPassword(t *testing.T) {
	env := newTestEnv(t)

	user, err := env.Users.CreateUser("neuer", "n@example.com", "Anker5-Leuchtturm!Wind", "Neuer", "User", []string{domain.RoleManager}, "test-admin")
	if err != nil {
		t.Fatalf("user anlegen: %v", err)
	}
	if user.PasswordHash == "Anker5-Leuchtturm!Wind" || user.PasswordHash == "" {
		t.Fatal("Passwort wurde nicht gehasht")
	}
	if got := user.PasswordHash[:10]; got != "$argon2id$" {
		t.Errorf("kein argon2id-Hash: %s...", got)
	}

	// Login mit dem neuen User funktioniert, RBAC-Rechte stimmen.
	_, logged, err := env.Auth.Login("neuer", "Anker5-Leuchtturm!Wind")
	if err != nil {
		t.Fatalf("login neuer User: %v", err)
	}
	// Manager haben Server-/Linux-User-Rechte, aber KEINE Admin-Rechte.
	if !logged.HasPermission(domain.PermServersWrite) {
		t.Error("Manager sollte servers:write besitzen")
	}
	if logged.HasPermission(domain.PermSettingsManage) {
		t.Error("Manager darf settings:manage NICHT besitzen")
	}
	if logged.HasPermission(domain.PermUsersWrite) {
		t.Error("Manager darf users:write NICHT besitzen")
	}
}

func TestCreateUserValidation(t *testing.T) {
	env := newTestEnv(t)

	if _, err := env.Users.CreateUser("kurz", "", "zu-kurz", "", "", nil, "test-admin"); !errors.Is(err, services.ErrWeakPassword) {
		t.Errorf("schwaches Passwort nicht abgelehnt: %v", err)
	}
	// Erst einen echten Zweituser anlegen, um den Duplikat-Fall zu prüfen
	// (admin/system sind reserviert und liefern einen eigenen Fehler).
	if _, err := env.Users.CreateUser("doppelt", "", "Zeder8-Kastanie!Brunnen", "", "", nil, "test-admin"); err != nil {
		t.Fatalf("ersten user anlegen: %v", err)
	}
	if _, err := env.Users.CreateUser("doppelt", "", "Zeder8-Kastanie!Brunnen", "", "", nil, "test-admin"); !errors.Is(err, services.ErrUsernameTaken) {
		t.Errorf("doppelter Username nicht abgelehnt: %v", err)
	}
	if _, err := env.Users.CreateUser("x", "", "Zeder8-Kastanie!Brunnen", "", "", nil, "test-admin"); !errors.Is(err, services.ErrInvalidUsername) {
		t.Errorf("ungültiger Username nicht abgelehnt: %v", err)
	}
	if _, err := env.Users.CreateUser("okname", "", "Zeder8-Kastanie!Brunnen", "", "", []string{"gibtsnicht"}, "test-admin"); !errors.Is(err, services.ErrUnknownRole) {
		t.Errorf("unbekannte Rolle nicht abgelehnt: %v", err)
	}
}

// TestCannotCreateReservedUser stellt sicher, dass die geschützten
// Basis-Konten (system, admin) nicht als neue Benutzer anlegbar sind.
func TestCannotCreateReservedUser(t *testing.T) {
	env := newTestEnv(t)
	for _, name := range []string{"system", "admin", "System", "  ADMIN "} {
		if _, err := env.Users.CreateUser(name, "", "Zeder8-Kastanie!Brunnen", "", "", nil, "test-admin"); !errors.Is(err, services.ErrReservedUsername) {
			t.Errorf("reservierter username %q nicht abgelehnt: %v", name, err)
		}
	}
}

func TestDeleteProtectedUsers(t *testing.T) {
	env := newTestEnv(t)

	users, err := env.Users.ListUsers()
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range users {
		if err := env.Users.DeleteUser(u.ID, "test-admin"); !errors.Is(err, services.ErrProtectedUser) {
			t.Errorf("löschen von %s sollte ErrProtectedUser liefern, bekam: %v", u.Username, err)
		}
	}
}

func TestAPIKeyLifecycle(t *testing.T) {
	env := newTestEnv(t)

	_, admin, err := env.Auth.Login("admin", "test-admin-passwort")
	if err != nil {
		t.Fatal(err)
	}

	plaintext, key, err := env.APIKeys.Create("ci", admin.ID, "", nil, "test-admin")
	if err != nil {
		t.Fatal(err)
	}
	if plaintext == "" || plaintext[:4] != "lcm_" {
		t.Fatalf("unerwartetes Key-Format: %q", plaintext)
	}
	if key.Scope != domain.APIKeyScopeReadWrite {
		t.Errorf("leerer Scope sollte auf readwrite fallen, ist %q", key.Scope)
	}

	// Gültiger Key liefert den User samt Permissions und den Key.
	user, validated, err := env.APIKeys.Validate(plaintext)
	if err != nil {
		t.Fatalf("gültiger Key abgelehnt: %v", err)
	}
	if !user.HasPermission(domain.PermUsersRead) {
		t.Error("Key im Admin-Kontext sollte users:read haben")
	}
	if validated.IsReadOnly() {
		t.Error("readwrite-Key darf nicht read-only sein")
	}

	// Ungültiger Key wird abgelehnt.
	if _, _, err := env.APIKeys.Validate("lcm_falsch"); !errors.Is(err, services.ErrAPIKeyInvalid) {
		t.Errorf("ungültiger Key nicht abgelehnt: %v", err)
	}

	// Widerrufener Key wird abgelehnt.
	if err := env.APIKeys.Revoke(key.ID, "test-admin"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := env.APIKeys.Validate(plaintext); !errors.Is(err, services.ErrAPIKeyInvalid) {
		t.Errorf("widerrufener Key nicht abgelehnt: %v", err)
	}
	// Der erste Aufruf ist ein WIDERRUF - die Zeile bleibt als Historie
	// sichtbar (revoked=true), verschwindet also nicht spurlos (R2-053).
	keys, err := env.APIKeys.List()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, k := range keys {
		if k.ID == key.ID {
			found = true
			if !k.Revoked {
				t.Error("Key sollte als widerrufen gelistet sein")
			}
		}
	}
	if !found {
		t.Fatal("widerrufener Key muss in der Liste bleiben (Historie)")
	}
	// Der ZWEITE Aufruf auf dem widerrufenen Key räumt ihn endgültig ab.
	if err := env.APIKeys.Revoke(key.ID, "test-admin"); err != nil {
		t.Fatal(err)
	}
	keys, _ = env.APIKeys.List()
	for _, k := range keys {
		if k.ID == key.ID {
			t.Error("nach dem zweiten Löschen darf der Key nicht mehr existieren")
		}
	}
}

func TestAPIKeyScopes(t *testing.T) {
	env := newTestEnv(t)
	_, admin, _ := env.Auth.Login("admin", "test-admin-passwort")

	// read-Scope wird gespeichert und erkannt.
	_, readKey, err := env.APIKeys.Create("lesend", admin.ID, domain.APIKeyScopeRead, nil, "test-admin")
	if err != nil {
		t.Fatal(err)
	}
	if !readKey.IsReadOnly() {
		t.Error("read-Key sollte read-only sein")
	}

	// Ungültiger Scope wird abgelehnt.
	if _, _, err := env.APIKeys.Create("kaputt", admin.ID, "superadmin", nil, "test-admin"); !errors.Is(err, services.ErrInvalidScope) {
		t.Errorf("ungültiger Scope nicht abgelehnt: %v", err)
	}
}

func TestAPIKeyExpiry(t *testing.T) {
	env := newTestEnv(t)

	_, admin, _ := env.Auth.Login("admin", "test-admin-passwort")
	past := time.Now().Add(-time.Hour)
	plaintext, _, err := env.APIKeys.Create("abgelaufen", admin.ID, "", &past, "test-admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := env.APIKeys.Validate(plaintext); !errors.Is(err, services.ErrAPIKeyInvalid) {
		t.Errorf("abgelaufener Key nicht abgelehnt: %v", err)
	}
}

// Pointer-Helfer für PATCH-Inputs (R2-029: GlobalSettingsInput ist jetzt
// zeigerbasiert - nil = Feld nicht mitgeschickt).
func sp(s string) *string { return &s }
func ip(i int) *int       { return &i }
func bp(b bool) *bool     { return &b }
