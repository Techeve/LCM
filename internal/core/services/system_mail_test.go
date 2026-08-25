package services_test

import (
	"errors"
	"strings"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/infrastructure/crypto"
	"LCM/internal/infrastructure/notify"
	"LCM/internal/storage"
	"LCM/internal/storage/repositories"
)

// --- Verwalteter System-E-Mail-Kanal -------------------------------------------

func TestEnsureSystemChannelLifecycle(t *testing.T) {
	env := newAlertEnv(t)

	// Erstes Aktivieren legt den verwalteten Kanal an.
	if err := env.notification.EnsureSystemChannel(true, "admin"); err != nil {
		t.Fatalf("EnsureSystemChannel(true): %v", err)
	}
	enabled, err := env.notification.SystemChannelEnabled()
	if err != nil || !enabled {
		t.Fatalf("erwarteter aktiver System-Kanal, bekam enabled=%v err=%v", enabled, err)
	}
	channels, _ := env.notification.List()
	var system *domain.NotificationChannel
	for i := range channels {
		if channels[i].Type == domain.ChannelTypeSystemEmail {
			system = &channels[i]
		}
	}
	if system == nil || system.Name != domain.SystemEmailChannelName {
		t.Fatalf("verwalteter Kanal fehlt oder falsch benannt: %+v", system)
	}

	// Abschalten deaktiviert nur - der Kanal bleibt bestehen (Alarmregeln
	// referenzieren ihn evtl. weiter).
	if err := env.notification.EnsureSystemChannel(false, "admin"); err != nil {
		t.Fatalf("EnsureSystemChannel(false): %v", err)
	}
	if enabled, _ := env.notification.SystemChannelEnabled(); enabled {
		t.Fatal("Kanal muss deaktiviert sein")
	}
	if _, err := env.notification.Get(system.ID); err != nil {
		t.Fatalf("Kanal darf beim Abschalten nicht gelöscht werden: %v", err)
	}

	// Wieder aktivieren schaltet dieselbe Zeile zurück (keine zweite Zeile).
	if err := env.notification.EnsureSystemChannel(true, "admin"); err != nil {
		t.Fatalf("EnsureSystemChannel(true) erneut: %v", err)
	}
	channels, _ = env.notification.List()
	count := 0
	for _, ch := range channels {
		if ch.Type == domain.ChannelTypeSystemEmail {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("erwartete genau einen System-Kanal, bekam %d", count)
	}
}

func TestSystemChannelIsManaged(t *testing.T) {
	env := newAlertEnv(t)
	if err := env.notification.EnsureSystemChannel(true, "admin"); err != nil {
		t.Fatal(err)
	}
	channels, _ := env.notification.List()
	var id uint
	for _, ch := range channels {
		if ch.Type == domain.ChannelTypeSystemEmail {
			id = ch.ID
		}
	}

	// Manuelles Ändern/Löschen über die Kanal-API ist gesperrt …
	if _, err := env.notification.Update(id, services.ChannelInput{
		Name: "umbenannt", Type: domain.ChannelTypeEmail, Config: validEmailConfig,
	}, "admin"); !errors.Is(err, services.ErrChannelManaged) {
		t.Fatalf("Update: erwartetes ErrChannelManaged, bekam %v", err)
	}
	if err := env.notification.Delete(id, "admin"); !errors.Is(err, services.ErrChannelManaged) {
		t.Fatalf("Delete: erwartetes ErrChannelManaged, bekam %v", err)
	}

	// … und der Typ ist über Create nicht anlegbar.
	if _, err := env.notification.Create(services.ChannelInput{
		Name: "fake", Type: domain.ChannelTypeSystemEmail,
	}, "admin"); !errors.Is(err, services.ErrChannelTypeInvalid) {
		t.Fatalf("Create: erwartetes ErrChannelTypeInvalid, bekam %v", err)
	}
}

// fakeProvider zählt Sendungen - ersetzt den System-Mailer im Test.
type fakeProvider struct {
	sent []domain.NotificationEvent
	fail error
}

func (f *fakeProvider) Type() string    { return domain.ChannelTypeSystemEmail }
func (f *fakeProvider) Validate() error { return nil }
func (f *fakeProvider) Send(e domain.NotificationEvent) error {
	if f.fail != nil {
		return f.fail
	}
	f.sent = append(f.sent, e)
	return nil
}

func TestSystemChannelSendsViaSystemMailer(t *testing.T) {
	env := newAlertEnv(t)
	fake := &fakeProvider{}
	env.notification.WithSystemMailer(func() (notify.Provider, error) { return fake, nil })
	if err := env.notification.EnsureSystemChannel(true, "admin"); err != nil {
		t.Fatal(err)
	}
	channels, _ := env.notification.List()
	for _, ch := range channels {
		if ch.Type != domain.ChannelTypeSystemEmail {
			continue
		}
		if err := env.notification.Send(&ch, domain.NotificationEvent{
			Severity: domain.AlertSeverityInfo, Description: "test",
		}); err != nil {
			t.Fatalf("Send über System-Kanal: %v", err)
		}
	}
	if len(fake.sent) != 1 {
		t.Fatalf("erwartete 1 Sendung über den System-Mailer, bekam %d", len(fake.sent))
	}
}

// --- Standard-E-Mail-Versand (SettingsService) ----------------------------------

func newSettingsService(t *testing.T) (*services.SettingsService, *repositories.SettingsRepository) {
	t.Helper()
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.Migrate(db); err != nil {
		t.Fatal(err)
	}
	cipher, err := crypto.NewCipher(crypto.GenerateKey())
	if err != nil {
		t.Fatal(err)
	}
	audit := services.NewAuditService(repositories.NewAuditRepository(db))
	repo := repositories.NewSettingsRepository(db)
	// Die Einstellungs-Zeile (ID 1) legt in Produktion das Seeding an.
	if err := repo.Save(&domain.GlobalSettings{}); err != nil {
		t.Fatal(err)
	}
	return services.NewSettingsService(repo, cipher, audit, nil), repo
}

func mailInput(mut func(*services.GlobalSettingsInput)) services.GlobalSettingsInput {
	in := services.GlobalSettingsInput{
		LogRetentionDays: ip(90), StorageHistoryRetentionDays: ip(90),
		BackupEnabled: bp(true), BackupIntervalHours: ip(24), BackupRetention: ip(14),
		CVEScanEnabled: bp(true),
		MailEnabled:    bp(true), MailHost: sp("smtp.example.com"), MailPort: ip(587),
		MailUsername: sp("lcm"), MailPassword: sp("geheim"),
		MailFrom: sp("lcm@example.com"), MailUseTLS: bp(true),
		MailAdminRecipients: sp("admin@example.com, ops@example.com"),
	}
	if mut != nil {
		mut(&in)
	}
	return in
}

func TestUpdateGlobalMailSettings(t *testing.T) {
	svc, repo := newSettingsService(t)

	// Aktiviert + unvollständig → klare Fehlermeldung.
	if _, err := svc.UpdateGlobal(mailInput(func(in *services.GlobalSettingsInput) {
		in.MailHost = sp("")
	}), "admin"); !errors.Is(err, services.ErrMailerIncomplete) {
		t.Fatalf("erwartetes ErrMailerIncomplete, bekam %v", err)
	}

	// Vollständig: Passwort landet verschlüsselt, nie im Klartext.
	if _, err := svc.UpdateGlobal(mailInput(nil), "admin"); err != nil {
		t.Fatalf("UpdateGlobal: %v", err)
	}
	settings, err := repo.Get()
	if err != nil {
		t.Fatal(err)
	}
	if settings.MailPasswordEnc == "" || settings.MailPasswordEnc == "geheim" {
		t.Fatalf("mail-passwort muss verschlüsselt gespeichert sein: %q", settings.MailPasswordEnc)
	}

	// Leeres Passwort im Folge-Update lässt das gespeicherte unangetastet.
	before := settings.MailPasswordEnc
	if _, err := svc.UpdateGlobal(mailInput(func(in *services.GlobalSettingsInput) {
		in.MailPassword = sp("")
	}), "admin"); err != nil {
		t.Fatal(err)
	}
	settings, _ = repo.Get()
	if settings.MailPasswordEnc != before {
		t.Fatal("leeres passwort darf das gespeicherte nicht überschreiben")
	}

	// Provider-Fabrik: liefert einen validen Provider mit Admin-Empfängern.
	provider, err := svc.SystemMailProvider()
	if err != nil {
		t.Fatalf("SystemMailProvider: %v", err)
	}
	if err := provider.Validate(); err != nil {
		t.Fatalf("provider muss valide sein: %v", err)
	}

	// Mailer deaktiviert → ErrMailerDisabled.
	if _, err := svc.UpdateGlobal(mailInput(func(in *services.GlobalSettingsInput) {
		in.MailEnabled = bp(false)
	}), "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SystemMailProvider(); !errors.Is(err, services.ErrMailerDisabled) {
		t.Fatalf("erwartetes ErrMailerDisabled, bekam %v", err)
	}
}

// --- Einladungs-Mail & Passwort-Reset (ActivationService) -----------------------

type mailRecorder struct {
	subjects []string
	bodies   []string
	to       [][]string
	fail     error
}

func (m *mailRecorder) send(subject, body string, to []string) error {
	if m.fail != nil {
		return m.fail
	}
	m.subjects = append(m.subjects, subject)
	m.bodies = append(m.bodies, body)
	m.to = append(m.to, to)
	return nil
}

type activationEnv struct {
	users      *repositories.UserRepository
	links      *repositories.ActivationRepository
	activation *services.ActivationService
	mails      *mailRecorder
}

func newActivationEnv(t *testing.T) *activationEnv {
	t.Helper()
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.Migrate(db); err != nil {
		t.Fatal(err)
	}
	audit := services.NewAuditService(repositories.NewAuditRepository(db))
	users := repositories.NewUserRepository(db)
	links := repositories.NewActivationRepository(db)
	mails := &mailRecorder{}
	activation := services.NewActivationService(links, users, audit).WithMailer(mails.send).
		WithLinkBase(func() string { return "https://lcm.example.com" })
	return &activationEnv{users: users, links: links, activation: activation, mails: mails}
}

func (e *activationEnv) createUser(t *testing.T, username, email string, active bool) *domain.User {
	t.Helper()
	u := &domain.User{Username: username, Email: email, PasswordHash: "x", Active: active}
	if err := e.users.Create(u); err != nil {
		t.Fatalf("user anlegen: %v", err)
	}
	// Zero-Value-Falle: Active hat DB-Default true - ein bewusstes false
	// überlebt den Insert nicht und muss per Update nachgezogen werden.
	if !active {
		if err := e.users.UpdateFields(u.ID, map[string]any{"active": false}); err != nil {
			t.Fatal(err)
		}
		u.Active = false
	}
	return u
}

func TestMailInvitation(t *testing.T) {
	env := newActivationEnv(t)
	user := env.createUser(t, "neuer.user", "neu@example.com", false)

	token, link, err := env.activation.Generate(user.ID, 0, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := env.activation.MailInvitation(user.ID, token, link.ExpiresAt); err != nil {
		t.Fatalf("MailInvitation: %v", err)
	}
	if len(env.mails.to) != 1 || env.mails.to[0][0] != "neu@example.com" {
		t.Fatalf("empfänger falsch: %v", env.mails.to)
	}
	if !strings.Contains(env.mails.bodies[0], "https://lcm.example.com/#/aktivierung?token="+token) {
		t.Errorf("aktivierungslink fehlt im rumpf:\n%s", env.mails.bodies[0])
	}

	// Ohne E-Mail-Adresse: klare Fehlermeldung.
	noMail := env.createUser(t, "ohne.mail", "", false)
	token2, link2, _ := env.activation.Generate(noMail.ID, 0, "admin")
	if err := env.activation.MailInvitation(noMail.ID, token2, link2.ExpiresAt); !errors.Is(err, services.ErrUserNoEmail) {
		t.Fatalf("erwartetes ErrUserNoEmail, bekam %v", err)
	}
}

// TestEmptyEmailNotUnique: mehrere User OHNE E-Mail-Adresse dürfen
// koexistieren (partieller Unique-Index statt striktem uniqueIndex);
// doppelte nicht-leere Adressen bleiben verboten - auch case-insensitiv.
func TestEmptyEmailNotUnique(t *testing.T) {
	env := newActivationEnv(t)
	env.createUser(t, "user.a", "", true)
	env.createUser(t, "user.b", "", true) // schlug vor dem Fix mit UNIQUE-Fehler fehl
	env.createUser(t, "user.c", "c@example.com", true)

	dup := &domain.User{Username: "user.d", Email: "C@EXAMPLE.COM", PasswordHash: "x", Active: true}
	if err := env.users.Create(dup); err == nil {
		t.Fatal("doppelte e-mail (case-insensitiv) muss am index scheitern")
	}
}

func TestRequestPasswordReset(t *testing.T) {
	env := newActivationEnv(t)
	env.createUser(t, "aktiver.user", "user@example.com", true)
	env.createUser(t, "inaktiver.user", "inaktiv@example.com", false)

	// Unbekannte Adresse: kein Fehler, keine Mail (keine User-Enumeration).
	if err := env.activation.RequestPasswordReset("unbekannt@example.com"); err != nil {
		t.Fatalf("unbekannte adresse darf keinen fehler liefern: %v", err)
	}
	if len(env.mails.to) != 0 {
		t.Fatal("für unbekannte adressen darf keine mail rausgehen")
	}

	// Inaktiver User: ebenfalls stiller No-Op.
	if err := env.activation.RequestPasswordReset("inaktiv@example.com"); err != nil {
		t.Fatal(err)
	}
	if len(env.mails.to) != 0 {
		t.Fatal("inaktive user dürfen keinen reset-link bekommen")
	}

	// Aktiver User (Groß-/Kleinschreibung egal): Mail mit Reset-Link.
	if err := env.activation.RequestPasswordReset("USER@example.com"); err != nil {
		t.Fatal(err)
	}
	if len(env.mails.to) != 1 || env.mails.to[0][0] != "user@example.com" {
		t.Fatalf("reset-mail fehlt: %v", env.mails.to)
	}
	body := env.mails.bodies[0]
	idx := strings.Index(body, "/#/aktivierung?token=")
	if idx < 0 {
		t.Fatalf("reset-link fehlt:\n%s", body)
	}
	token := body[idx+len("/#/aktivierung?token="):]
	token = strings.Fields(token)[0]

	// Der gemailte Link ist einlösbar und setzt das neue Passwort.
	user, err := env.activation.Consume(token, "NeuesPasswort123!")
	if err != nil {
		t.Fatalf("Consume: %v", err)
	}
	if user.Username != "aktiver.user" {
		t.Fatalf("falscher user: %s", user.Username)
	}

	// Mailer-Fehler bleiben nach außen unsichtbar (kein Enumerations-Kanal).
	env.mails.fail = errors.New("smtp kaputt")
	if err := env.activation.RequestPasswordReset("user@example.com"); err != nil {
		t.Fatalf("mailer-fehler darf nicht durchschlagen: %v", err)
	}
}
