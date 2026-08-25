package services_test

import (
	"errors"
	"testing"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/storage/repositories"
)

func TestUpdateProfileAndResetPassword(t *testing.T) {
	env := newTestEnv(t)
	user, err := env.Users.CreateUser("editme", "old@example.com", "Wolke7-Nordlicht!Kx", "Alt", "Name", []string{domain.RoleManager}, "test-admin")
	if err != nil {
		t.Fatal(err)
	}

	// Profil ändern.
	updated, err := env.Users.UpdateProfile(user.ID, "neu@example.com", "Neu", "Name2", "test-admin")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Email != "neu@example.com" || updated.FirstName != "Neu" || updated.LastName != "Name2" {
		t.Errorf("profil nicht aktualisiert: %+v", updated)
	}

	// Passwort zurücksetzen - Login mit neuem Passwort funktioniert.
	if err := env.Users.ResetPassword(user.ID, "Regen9-Amsel!Turmfalk", true, "test-admin"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := env.Auth.Login("editme", "Regen9-Amsel!Turmfalk"); err != nil {
		t.Errorf("login mit neuem passwort fehlgeschlagen: %v", err)
	}
	// Schwaches Passwort abgelehnt.
	if err := env.Users.ResetPassword(user.ID, "kurz", true, "test-admin"); !errors.Is(err, services.ErrWeakPassword) {
		t.Errorf("schwaches passwort nicht abgelehnt: %v", err)
	}
}

func TestSystemUserNotEditable(t *testing.T) {
	env := newTestEnv(t)
	users, _ := env.Users.ListUsers()
	var systemID uint
	for _, u := range users {
		if u.Username == domain.SystemUsername {
			systemID = u.ID
		}
	}
	if systemID == 0 {
		t.Fatal("system-user nicht gefunden")
	}

	if _, err := env.Users.UpdateProfile(systemID, "x@y.z", "X", "Y", "test-admin"); !errors.Is(err, services.ErrProtectedUser) {
		t.Errorf("system-user-profil darf nicht änderbar sein: %v", err)
	}
	if err := env.Users.ResetPassword(systemID, "Fjord4-Muschel!Wandel", true, "test-admin"); !errors.Is(err, services.ErrProtectedUser) {
		t.Errorf("system-user-passwort darf nicht änderbar sein: %v", err)
	}
}

// TestSettingsPatchNulltNichts (R2-029): Ein PATCH mit EINEM Feld darf keine
// unbeteiligten Einstellungen kippen - genau das passierte: {"mail_port":2525}
// schaltete Backup und CVE-Scan ab und verwarf die APT-Cache-URL.
func TestSettingsPatchNulltNichts(t *testing.T) {
	env := newTestEnv(t)
	// Ausgangszustand mit gesetzten Werten.
	if _, err := env.Settings.UpdateGlobal(services.GlobalSettingsInput{
		LogRetentionDays: ip(90), BackupEnabled: bp(true), BackupRetention: ip(14),
		CVEScanEnabled: bp(true), JobMaxRuntimeMinutes: ip(120),
		AptCacheURL: sp("http://192.168.1.10:3142"),
	}, "admin"); err != nil {
		t.Fatal(err)
	}

	// Der Befund-Aufruf: nur mail_port.
	updated, err := env.Settings.UpdateGlobal(services.GlobalSettingsInput{MailPort: ip(2525)}, "admin")
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	if updated.MailPort != 2525 {
		t.Errorf("mail_port nicht übernommen: %d", updated.MailPort)
	}
	// Die zehn Opfer aus dem Befund - keines darf kippen.
	if updated.LogRetentionDays != 90 || !updated.BackupEnabled || updated.BackupRetention != 14 ||
		!updated.CVEScanEnabled || updated.JobMaxRuntimeMinutes != 120 ||
		updated.AptCacheURL != "http://192.168.1.10:3142" {
		t.Errorf("Ein-Feld-PATCH hat unbeteiligte Felder verändert: retention=%d backup=%v cve=%v jobmax=%d apt=%q",
			updated.LogRetentionDays, updated.BackupEnabled, updated.CVEScanEnabled,
			updated.JobMaxRuntimeMinutes, updated.AptCacheURL)
	}
}

// TestLinuxUserPatchSudoDeaktiviertNicht (R2-041): {"sudo":true} darf den
// Benutzer weder deaktivieren noch Name/E-Mail leeren.
func TestLinuxUserPatchSudoDeaktiviertNicht(t *testing.T) {
	env := newTestEnv(t)
	u, err := env.LinuxUsers.Create(services.LinuxUserCreateInput{Username: "patchtest", FullName: "Voller Name", Email: "p@example.org", Shell: "/bin/bash", Sudo: false}, "admin")
	if err != nil {
		t.Fatal(err)
	}

	updated, err := env.LinuxUsers.Update(u.ID, services.LinuxUserUpdateInput{Sudo: bp(true)}, "admin")
	if err != nil {
		t.Fatalf("sudo-grant: %v", err)
	}
	if !updated.Sudo {
		t.Error("sudo nicht übernommen")
	}
	if !updated.Active || updated.FullName != "Voller Name" || updated.Email != "p@example.org" {
		t.Errorf("Sudo-Grant hat unbeteiligte Felder gekippt: active=%v name=%q mail=%q",
			updated.Active, updated.FullName, updated.Email)
	}
}

// TestBenutzerSperren (R2-036): Ein Konto lässt sich sperren und entsperren,
// ohne es zu löschen; der letzte aktive Admin ist geschützt.
func TestBenutzerSperren(t *testing.T) {
	env := newTestEnv(t)
	u, err := env.Users.CreateUser("blockme", "b@example.org", "Anker5-Leuchtturm!Wind", "B", "M", []string{domain.RoleManager}, "admin")
	if err != nil {
		t.Fatal(err)
	}

	// Sperren.
	blocked, err := env.Users.SetActive(u.ID, false, "admin")
	if err != nil {
		t.Fatalf("sperren: %v", err)
	}
	if blocked.Active {
		t.Error("Konto sollte nach dem Sperren inaktiv sein")
	}
	// Entsperren.
	unblocked, err := env.Users.SetActive(u.ID, true, "admin")
	if err != nil {
		t.Fatalf("entsperren: %v", err)
	}
	if !unblocked.Active {
		t.Error("Konto sollte nach dem Entsperren wieder aktiv sein")
	}

	// Den einzigen Admin (den geseedeten) darf man nicht sperren.
	admin, err := repositories.NewUserRepository(env.DB()).FindByUsername("admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.Users.SetActive(admin.ID, false, "admin"); !errors.Is(err, services.ErrLastAdmin) {
		t.Errorf("den letzten Admin sperren muss ErrLastAdmin liefern, bekam: %v", err)
	}
	// Und ihm die admin-Rolle nicht entziehen.
	if _, err := env.Users.UpdateUserRoles(admin.ID, []string{domain.RoleManager}, "admin"); !errors.Is(err, services.ErrLastAdmin) {
		t.Errorf("dem letzten Admin die Rolle entziehen muss ErrLastAdmin liefern, bekam: %v", err)
	}
}

// TestReservierteLinuxNamen (R2-043) + Require2FARolesValidierung (R2-060) +
// AktivierungsTTLGedeckelt (R2-044): drei Validierungs-Befunde, service-nah.
func TestValidierungsBefunde(t *testing.T) {
	env := newTestEnv(t)

	// R2-043: reservierte Namen abgelehnt, normaler Name geht.
	for _, name := range []string{"root", "lcm-svc", "www-data"} {
		if _, err := env.LinuxUsers.Create(services.LinuxUserCreateInput{Username: name, FullName: "", Email: "", Shell: "", Sudo: false}, "admin"); !errors.Is(err, services.ErrReservedLinuxUsername) {
			t.Errorf("R2-043 %s: erwartete ErrReservedLinuxUsername, bekam %v", name, err)
		}
	}
	if _, err := env.LinuxUsers.Create(services.LinuxUserCreateInput{Username: "appuser", FullName: "", Email: "", Shell: "", Sudo: false}, "admin"); err != nil {
		t.Errorf("R2-043: normaler Name muss angelegt werden, bekam %v", err)
	}

	// R2-060: unbekannte Rolle in require_2fa_roles → ErrSettingInvalid.
	if _, err := env.Settings.UpdateGlobal(services.GlobalSettingsInput{Require2FARoles: sp("gibtsnicht")}, "admin"); !errors.Is(err, services.ErrSettingInvalid) {
		t.Errorf("R2-060: erwartete ErrSettingInvalid, bekam %v", err)
	}
	if _, err := env.Settings.UpdateGlobal(services.GlobalSettingsInput{Require2FARoles: sp("admin")}, "admin"); err != nil {
		t.Errorf("R2-060: gültige Rolle muss durchgehen, bekam %v", err)
	}

	// R2-044: Aktivierungslink-TTL wird nach oben gedeckelt.
	lu, err := env.LinuxUsers.Create(services.LinuxUserCreateInput{Username: "ttluser", FullName: "", Email: "", Shell: "", Sudo: false}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	_, act, err := env.LinuxUsers.GenerateActivation(lu.ID, 100000*time.Hour, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if until := time.Until(act.ExpiresAt); until > services.MaxActivationTTL+time.Minute {
		t.Errorf("R2-044: TTL nicht gedeckelt, läuft noch %s", until)
	}
}
