package services_test

import (
	"strings"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/storage/repositories"
)

// aclSyncSetup: Server mit nutzbaren ACLs, ein Profil mit Verzeichnisrecht
// und ein Benutzer, der es als Standardprofil trägt.
func aclSyncSetup(t *testing.T, restricted bool) (*testEnv, *domain.Server, *domain.LinuxUser) {
	t.Helper()
	return profileSyncSetup(t, restricted, services.ProfileInput{
		Name: "Web", Slug: "web",
		PathRules: []domain.ProfilePathRule{{Path: "/srv/www", Mode: domain.PathModeReadWrite}},
	})
}

// profileSyncSetup: Server mit nutzbaren ACLs und ein Benutzer, der das
// angelegte Profil als Standardprofil trägt.
func profileSyncSetup(t *testing.T, restricted bool, in services.ProfileInput) (*testEnv, *domain.Server, *domain.LinuxUser) {
	t.Helper()
	env := newTestEnv(t)
	serverID := joinTestServer(t, env, "acl01")
	profile, err := env.Profiles.Create(in, "admin")
	if err != nil {
		t.Fatal(err)
	}
	user, err := env.LinuxUsers.Create(services.LinuxUserCreateInput{Username: "anna", Shell: "/bin/bash"}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	linux := repositories.NewLinuxUserRepository(env.DB())
	if err := linux.UpdateFields(user.ID, map[string]any{"default_profile_id": profile.ID}); err != nil {
		t.Fatal(err)
	}
	if err := linux.AssignToServer(user.ID, serverID); err != nil {
		t.Fatal(err)
	}
	if err := env.DB().Model(&domain.Server{}).Where("id = ?", serverID).Updates(map[string]any{
		"acl_usable": true, "has_acl": true, "restricted_sudo": restricted,
	}).Error; err != nil {
		t.Fatal(err)
	}
	server, err := env.Servers.Get(repositories.ScopeAll(), serverID)
	if err != nil {
		t.Fatal(err)
	}
	return env, server, user
}

// syncCommands gleicht die Benutzer ab und liefert die ausgeführten Kommandos.
func syncCommands(t *testing.T, env *testEnv, server *domain.Server) (string, string) {
	t.Helper()
	env.Dialer.Reset()
	out, err := env.Prov.SyncUsers(server, "admin")
	if err != nil {
		t.Fatalf("sync: %v\n%s", err, out)
	}
	commands, _ := env.Dialer.Recorded()
	return out, strings.Join(commands, "\n")
}

// unassignProfile setzt den Benutzer auf das Standardprofil zurück - das
// eigene Profil wird auf dem Server nicht mehr gebraucht.
func unassignProfile(t *testing.T, env *testEnv, user *domain.LinuxUser) {
	t.Helper()
	standard := profileBySlug(t, env, domain.ProfileSlugStandard)
	if err := repositories.NewLinuxUserRepository(env.DB()).UpdateFields(user.ID, map[string]any{"default_profile_id": standard.ID}); err != nil {
		t.Fatal(err)
	}
}

// TestProfilACLWirdVorDemGruppenLoeschenZurueckgenommen: Wird ein Profil vom
// Server abgezogen, nimmt der Abgleich dessen ACL zurück - mit dem Namen der
// Gruppe und BEVOR das Aufräumen sie löscht. Vorher fehlte der Slug
// ("g:lcm-prof-"), setfacl scheiterte still, und nach groupdel hing der
// Eintrag an der nackten GID: Eine spätere Gruppe mit derselben GID erbte die
// Rechte.
func TestProfilACLWirdVorDemGruppenLoeschenZurueckgenommen(t *testing.T) {
	env, server, user := aclSyncSetup(t, false)
	if _, all := syncCommands(t, env, server); !strings.Contains(all, "setfacl -RP -m g:lcm-prof-web:rwX /srv/www") {
		t.Fatalf("verzeichnisrecht nicht gesetzt:\n%s", all)
	}
	applied, err := repositories.NewPrivilegeProfileRepository(env.DB()).AppliedPathsForServer(server.ID)
	if err != nil || len(applied) != 1 || applied[0].Slug != "web" {
		t.Fatalf("slug nicht mitgeschrieben: %+v (%v)", applied, err)
	}

	unassignProfile(t, env, user)
	_, all := syncCommands(t, env, server)
	revoke := strings.Index(all, "setfacl -RP -x g:lcm-prof-web /srv/www")
	prune := strings.Index(all, "groupdel")
	if revoke < 0 || prune < 0 {
		t.Fatalf("rücknahme oder aufräumen fehlt (rücknahme %d, groupdel %d):\n%s", revoke, prune, all)
	}
	if revoke > prune {
		t.Errorf("die ACL wird erst nach groupdel zurückgenommen:\n%s", all)
	}
}

// TestProfilACLImEingeschraenktenModusUeberDenHelper: Ohne root scheitert
// setfacl an fremden Dateien und brach den ganzen Benutzer-Sync ab. Im
// eingeschränkten Modus laufen Setzen und Zurücknehmen über den Helper.
func TestProfilACLImEingeschraenktenModusUeberDenHelper(t *testing.T) {
	env, server, user := aclSyncSetup(t, true)
	_, all := syncCommands(t, env, server)
	// Die Parameter stehen shell-quotiert in einem sh -c '…'.
	if !strings.Contains(all, `lcm-helper profile-acl '\''set'\''`) || strings.Contains(all, "setfacl -RP -m") {
		t.Fatalf("verzeichnisrecht nicht über den helper gesetzt:\n%s", all)
	}

	unassignProfile(t, env, user)
	_, all = syncCommands(t, env, server)
	revoke := strings.Index(all, `lcm-helper profile-acl '\''remove'\''`)
	prune := strings.Index(all, "lcm-helper profile-prune")
	if revoke < 0 || prune < 0 || revoke > prune {
		t.Errorf("rücknahme fehlt oder läuft nach dem aufräumen (rücknahme %d, aufräumen %d):\n%s", revoke, prune, all)
	}
}

// TestAlteACLOhneSlug: Einträge aus der Zeit vor dem gespeicherten Slug
// nehmen den Namen aus dem Profil, solange es besteht. Ist es gelöscht, lässt
// sich die Gruppe nicht mehr benennen - das steht im Bericht statt still
// liegen zu bleiben.
func TestAlteACLOhneSlug(t *testing.T) {
	env, server, user := aclSyncSetup(t, false)
	unassignProfile(t, env, user)
	web := profileBySlug(t, env, "web")
	if err := repositories.NewPrivilegeProfileRepository(env.DB()).ReplaceAppliedPaths(server.ID, []domain.AppliedProfilePath{
		{ServerID: server.ID, ProfileID: web.ID, Path: "/srv/alt", Mode: domain.PathModeRead},
		{ServerID: server.ID, ProfileID: 9999, Path: "/srv/weg", Mode: domain.PathModeRead},
	}); err != nil {
		t.Fatal(err)
	}
	out, all := syncCommands(t, env, server)
	if !strings.Contains(all, "setfacl -RP -x g:lcm-prof-web /srv/alt") {
		t.Errorf("alter eintrag des bestehenden profils nicht zurückgenommen:\n%s", all)
	}
	if !strings.Contains(out, "/srv/weg NICHT zurückgenommen") {
		t.Errorf("nicht zurücknehmbarer eintrag nicht gemeldet:\n%s", out)
	}
}
