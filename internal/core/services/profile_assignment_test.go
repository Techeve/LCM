package services_test

import (
	"strings"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/storage/repositories"
)

// profileBySlug holt ein Profil über seinen Slug.
func profileBySlug(t *testing.T, env *testEnv, slug string) *domain.PrivilegeProfile {
	t.Helper()
	profiles, err := env.Profiles.List()
	if err != nil {
		t.Fatal(err)
	}
	for i := range profiles {
		if profiles[i].Slug == slug {
			return &profiles[i]
		}
	}
	t.Fatalf("profil %q nicht gefunden", slug)
	return nil
}

// TestStandardprofilLeitetSudoAb: Maßgeblich ist das Profil; das alte
// sudo-Bit bleibt als abgeleiteter Wert erhalten, damit bestehende Clients
// weiterlaufen.
func TestStandardprofilLeitetSudoAb(t *testing.T) {
	env := newTestEnv(t)
	admin := profileBySlug(t, env, domain.ProfileSlugFullAdmin)
	standard := profileBySlug(t, env, domain.ProfileSlugStandard)

	// Anlegen mit sudo=true ordnet dem Voll-Administrator zu.
	user, err := env.LinuxUsers.Create(services.LinuxUserCreateInput{Username: "anna", FullName: "Anna", Email: "", Shell: "/bin/bash", Sudo: true}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if user.DefaultProfileID == nil || *user.DefaultProfileID != admin.ID {
		t.Fatalf("erwartet voll-administrator, bekam %v", user.DefaultProfileID)
	}
	if !user.Sudo {
		t.Error("sudo muss aus dem profil abgeleitet true sein")
	}

	// Profilwechsel setzt das abgeleitete Bit mit.
	updated, err := env.LinuxUsers.Update(user.ID, services.LinuxUserUpdateInput{
		DefaultProfileID: &standard.ID,
	}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Sudo {
		t.Error("nach dem wechsel auf das standardprofil darf sudo nicht mehr gesetzt sein")
	}

	// Alt-Clients, die nur das Häkchen kennen, landen beim passenden Profil.
	back, err := env.LinuxUsers.Update(user.ID, services.LinuxUserUpdateInput{Sudo: boolPtr(true)}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if back.DefaultProfileID == nil || *back.DefaultProfileID != admin.ID {
		t.Errorf("das sudo-häkchen muss auf den voll-administrator abbilden, bekam %v", back.DefaultProfileID)
	}
}

func boolPtr(b bool) *bool { return &b }

// TestEffektivesProfilJeServer deckt die Auflösung ab: Direktzuweisung
// schlägt Gruppe, unter den Gruppen entscheidet der Vorrang, sonst gilt das
// Standardprofil des Benutzers.
func TestEffektivesProfilJeServer(t *testing.T) {
	env := newTestEnv(t)
	linux := repositories.NewLinuxUserRepository(env.DB())
	standard := profileBySlug(t, env, domain.ProfileSlugStandard)

	web, err := env.Profiles.Create(services.ProfileInput{Name: "Web", Slug: "web"}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	db, err := env.Profiles.Create(services.ProfileInput{Name: "Datenbank", Slug: "db"}, "admin")
	if err != nil {
		t.Fatal(err)
	}

	serverID := joinTestServer(t, env, "web01")
	user, err := env.LinuxUsers.Create(services.LinuxUserCreateInput{Username: "anna", FullName: "", Email: "", Shell: "/bin/bash", Sudo: false}, "admin")
	if err != nil {
		t.Fatal(err)
	}

	// Ohne jede Zuweisung gilt das Standardprofil des Benutzers.
	if got := effective(t, linux, serverID, user.ID); got != standard.ID {
		t.Fatalf("erwartet standardprofil %d, bekam %d", standard.ID, got)
	}

	// Zwei Gruppen mit demselben Server, unterschiedlicher Vorrang.
	stark, err := env.Groups.Create("Basis", "", intPtr(10), "admin")
	if err != nil {
		t.Fatal(err)
	}
	schwach, err := env.Groups.Create("Web-Prod", "", intPtr(100), "admin")
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range []uint{stark.ID, schwach.ID} {
		if err := env.Groups.AssignServer(repositories.ScopeAll(), g, serverID, "admin"); err != nil {
			t.Fatal(err)
		}
	}
	if err := env.LinuxUsers.AssignToGroup(repositories.ScopeAll(), user.ID, schwach.ID, &web.ID, "admin"); err != nil {
		t.Fatal(err)
	}
	if got := effective(t, linux, serverID, user.ID); got != web.ID {
		t.Fatalf("erwartet das profil der einzigen gruppe (%d), bekam %d", web.ID, got)
	}

	// Die stärkere Gruppe (kleinere Zahl) setzt sich durch.
	if err := env.LinuxUsers.AssignToGroup(repositories.ScopeAll(), user.ID, stark.ID, &db.ID, "admin"); err != nil {
		t.Fatal(err)
	}
	if got := effective(t, linux, serverID, user.ID); got != db.ID {
		t.Fatalf("erwartet das profil der stärkeren gruppe (%d), bekam %d", db.ID, got)
	}

	// Eine ausdrückliche Direktzuweisung schlägt beide Gruppen.
	if err := linux.AssignToServer(user.ID, serverID); err != nil {
		t.Fatal(err)
	}
	if err := linux.SetServerAssignmentProfile(user.ID, serverID, &web.ID); err != nil {
		t.Fatal(err)
	}
	if got := effective(t, linux, serverID, user.ID); got != web.ID {
		t.Fatalf("die direktzuweisung muss gewinnen (%d), bekam %d", web.ID, got)
	}
}

func intPtr(i int) *int { return &i }

// effective liest das wirksame Profil eines Benutzers auf einem Server.
func effective(t *testing.T, linux *repositories.LinuxUserRepository, serverID, userID uint) uint {
	t.Helper()
	profiles, err := linux.EffectiveProfilesForServer(serverID)
	if err != nil {
		t.Fatal(err)
	}
	return profiles[userID]
}

// TestEigenesProfilWirktAufDemServer ist die Probe aufs Ganze: Ein Benutzer
// mit eigenem Profil bekommt auf dem Zielsystem Gruppe, geprüfte sudoers-Datei
// und Mitgliedschaft - und KEINEN per-Benutzer-Grant mit vollen Rechten.
func TestEigenesProfilWirktAufDemServer(t *testing.T) {
	env := newTestEnv(t)
	serverID := joinTestServer(t, env, "web01")

	profile, err := env.Profiles.Create(services.ProfileInput{
		Name: "Webserver-Betrieb", Slug: "webserver",
		SudoRules: []domain.ProfileSudoRule{{Command: "/usr/bin/systemctl restart nginx"}},
	}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	user, err := env.LinuxUsers.Create(services.LinuxUserCreateInput{Username: "anna", FullName: "", Email: "", Shell: "/bin/bash", Sudo: false}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.LinuxUsers.Update(user.ID, services.LinuxUserUpdateInput{
		DefaultProfileID: &profile.ID,
	}, "admin"); err != nil {
		t.Fatal(err)
	}
	if err := repositories.NewLinuxUserRepository(env.DB()).AssignToServer(user.ID, serverID); err != nil {
		t.Fatal(err)
	}

	env.Dialer.Reset()
	server, err := env.Servers.Get(repositories.ScopeAll(), serverID)
	if err != nil {
		t.Fatal(err)
	}
	ctx := services.SessionContext{ServerID: serverID, Actor: "admin", Purpose: "test-sync"}
	if _, err := env.Prov.SyncServer(server, ctx); err != nil {
		t.Fatal(err)
	}
	commands, _ := env.Dialer.Recorded()
	all := strings.Join(commands, "\n")

	for _, want := range []string{
		"groupadd lcm-prof-webserver",
		"visudo -cf /etc/sudoers.d/lcm-prof-webserver.tmp",
		"usermod -aG lcm-prof-webserver anna",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("auf dem server fehlt: %s", want)
		}
	}
	// Der per-Benutzer-Grant mit vollen Rechten darf NICHT entstehen - das
	// wäre genau der Zustand, den die Profile ablösen.
	if strings.Contains(all, "NOPASSWD:ALL' anna") || strings.Contains(all, "NOPASSWD:ALL\\n' anna") {
		t.Errorf("es wurde ein voller sudo-grant geschrieben:\n%s", all)
	}
	if !strings.Contains(all, "rm -f /etc/sudoers.d/lcm-anna") {
		t.Error("ein früherer per-benutzer-grant wird nicht entfernt")
	}
}
