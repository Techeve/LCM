package services_test

import (
	"errors"
	"strings"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
)

// findProfile sucht ein Profil über seinen Slug.
func findProfile(t *testing.T, env *testEnv, slug string) *domain.PrivilegeProfile {
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

// TestEingebauteProfileSindGeschuetzt: Die zwei mitgelieferten Profile bilden
// den Zustand ab, den es vor den Profilen schon gab („sudo" und „kein sudo").
// Wären sie änderbar, änderte sich unter der Hand, was das für jeden
// bestehenden Benutzer bedeutet.
func TestEingebauteProfileSindGeschuetzt(t *testing.T) {
	env := newTestEnv(t)

	admin := findProfile(t, env, domain.ProfileSlugFullAdmin)
	if !admin.Builtin || !admin.GrantsFullRoot {
		t.Errorf("voll-administrator muss mitgeliefert sein und volle root-rechte tragen: %+v", admin)
	}
	standard := findProfile(t, env, domain.ProfileSlugStandard)
	if !standard.Builtin || standard.GrantsFullRoot {
		t.Errorf("standardbenutzer muss mitgeliefert sein und KEINE root-rechte tragen: %+v", standard)
	}

	for _, p := range []*domain.PrivilegeProfile{admin, standard} {
		_, err := env.Profiles.Update(p.ID, services.ProfileInput{Name: "Umbenannt"}, "admin")
		if !errors.Is(err, services.ErrProfileBuiltin) {
			t.Errorf("%s darf nicht änderbar sein, bekam %v", p.Slug, err)
		}
		if err := env.Profiles.Delete(p.ID, "admin"); !errors.Is(err, services.ErrProfileBuiltin) {
			t.Errorf("%s darf nicht löschbar sein, bekam %v", p.Slug, err)
		}
	}
}

// TestEigenesProfilAnlegenUndAendern deckt den Regelfall ab: anlegen, Regeln
// ändern, löschen.
func TestEigenesProfilAnlegenUndAendern(t *testing.T) {
	env := newTestEnv(t)

	created, err := env.Profiles.Create(services.ProfileInput{
		Name: "Webserver-Betrieb", Slug: "webserver", Description: "nginx betreiben",
		SudoRules: []domain.ProfileSudoRule{
			{Command: "/usr/bin/systemctl restart nginx"},
			{Command: "/usr/bin/journalctl -u nginx"},
		},
		EditRules: []domain.ProfileEditRule{{Path: "/etc/nginx/sites-available/kunde.conf"}},
		PathRules: []domain.ProfilePathRule{{Path: "/srv/www", Mode: domain.PathModeReadWrite}},
	}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if len(created.SudoRules) != 2 || len(created.EditRules) != 1 || len(created.PathRules) != 1 {
		t.Fatalf("regeln nicht vollständig gespeichert: %+v", created)
	}
	// Der Pager-Schutz greift schon beim Speichern.
	if created.SudoRules[0].Command != "/usr/bin/systemctl --no-pager restart nginx" {
		t.Errorf("--no-pager fehlt: %q", created.SudoRules[0].Command)
	}
	// Ohne Angabe läuft ein Kommando als root.
	if created.SudoRules[0].RunAs != "root" {
		t.Errorf("zielbenutzer sollte root sein, ist %q", created.SudoRules[0].RunAs)
	}

	// Ändern ERSETZT die Regeln - ein Profil beschreibt einen Soll-Zustand,
	// eine entfernte Regel darf nicht als Rest zurückbleiben.
	updated, err := env.Profiles.Update(created.ID, services.ProfileInput{
		Name:      "Webserver-Betrieb",
		SudoRules: []domain.ProfileSudoRule{{Command: "/usr/bin/systemctl reload nginx"}},
	}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.SudoRules) != 1 || updated.SudoRules[0].Command != "/usr/bin/systemctl --no-pager reload nginx" {
		t.Fatalf("regeln wurden nicht ersetzt: %+v", updated.SudoRules)
	}
	if len(updated.EditRules) != 0 || len(updated.PathRules) != 0 {
		t.Errorf("entfernte regeln sind zurückgeblieben: %d dateien, %d pfade",
			len(updated.EditRules), len(updated.PathRules))
	}

	if err := env.Profiles.Delete(created.ID, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := env.Profiles.Get(created.ID); err == nil {
		t.Error("gelöschtes profil ist noch abrufbar")
	}
}

// TestProfilPruefungenGreifenBeimSpeichern: Die Eingabeprüfung ist die
// eigentliche Sicherheitsgrenze dieses Features - sie muss am Service hängen
// und nicht erst in der Oberfläche.
func TestProfilPruefungenGreifenBeimSpeichern(t *testing.T) {
	env := newTestEnv(t)

	cases := []struct {
		name string
		in   services.ProfileInput
	}{
		{"ohne name", services.ProfileInput{Slug: "leer"}},
		{"nacktes systemctl", services.ProfileInput{Name: "A", Slug: "a",
			SudoRules: []domain.ProfileSudoRule{{Command: "/usr/bin/systemctl"}}}},
		{"platzhalter", services.ProfileInput{Name: "B", Slug: "b",
			SudoRules: []domain.ProfileSudoRule{{Command: "/usr/bin/apt-get install *"}}}},
		{"shell ohne bestätigung", services.ProfileInput{Name: "C", Slug: "c",
			SudoRules: []domain.ProfileSudoRule{{Command: "/bin/bash"}}}},
		{"gesperrter pfad", services.ProfileInput{Name: "D", Slug: "d",
			PathRules: []domain.ProfilePathRule{{Path: "/etc/sudoers.d", Mode: domain.PathModeReadWrite}}}},
		{"ungültiger slug", services.ProfileInput{Name: "E", Slug: "Groß_Geschrieben"}},
	}
	for _, f := range cases {
		if _, err := env.Profiles.Create(f.in, "admin"); err == nil {
			t.Errorf("%s: muss abgewiesen werden", f.name)
		}
	}

	// Mit ausdrücklicher Bestätigung geht die Shell durch - sie ist nicht
	// verboten, sie ist bestätigungspflichtig.
	if _, err := env.Profiles.Create(services.ProfileInput{
		Name: "Notfall", Slug: "notfall",
		SudoRules: []domain.ProfileSudoRule{{Command: "/bin/bash", AllowRootEquivalent: true}},
	}, "admin"); err != nil {
		t.Errorf("bestätigte shell-regel abgewiesen: %v", err)
	}
}

// TestProfilNameUndSlugSindEindeutig: Aus dem Slug entsteht auf dem
// Zielsystem der Gruppenname - zwei Profile mit demselben Slug wären
// dieselbe Gruppe.
func TestProfilNameUndSlugSindEindeutig(t *testing.T) {
	env := newTestEnv(t)

	if _, err := env.Profiles.Create(services.ProfileInput{Name: "Web", Slug: "web"}, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := env.Profiles.Create(services.ProfileInput{Name: "Web 2", Slug: "web"}, "admin"); !errors.Is(err, services.ErrProfileSlugTaken) {
		t.Errorf("doppelter slug muss abgewiesen werden, bekam %v", err)
	}
	if _, err := env.Profiles.Create(services.ProfileInput{Name: "web", Slug: "web-2"}, "admin"); !errors.Is(err, services.ErrProfileNameTaken) {
		t.Errorf("doppelter name muss abgewiesen werden, bekam %v", err)
	}
	// Der eigene Name blockiert das Speichern nicht.
	profile := findProfile(t, env, "web")
	if _, err := env.Profiles.Update(profile.ID, services.ProfileInput{Name: "Web"}, "admin"); err != nil {
		t.Errorf("unveränderter name beim speichern abgewiesen: %v", err)
	}
}

// TestProfilKopieren: Der vorgesehene Weg, ein mitgeliefertes Profil
// anzupassen. Die Kopie traegt dieselben Regeln, ist aber aenderbar - und sie
// haengt nicht mehr am Original.
func TestProfilKopieren(t *testing.T) {
	env := newTestEnv(t)
	source, err := env.Profiles.Create(services.ProfileInput{
		Name: "Webserver", Slug: "webserver", Description: "Dienste des Webservers",
		SudoRules: []domain.ProfileSudoRule{{Command: "/usr/bin/systemctl restart nginx"}},
		PathRules: []domain.ProfilePathRule{{Path: "/var/www", Mode: domain.PathModeReadWrite}},
	}, "admin")
	if err != nil {
		t.Fatalf("quellprofil anlegen: %v", err)
	}

	kopie, err := env.Profiles.Clone(source.ID, "webserver-kopie", "Webserver (Kopie)", "admin")
	if err != nil {
		t.Fatalf("kopieren: %v", err)
	}
	if kopie.ID == source.ID {
		t.Fatal("die kopie muss ein eigenes profil sein")
	}
	if kopie.Builtin {
		t.Error("eine kopie ist nie mitgeliefert - sonst waere sie wieder schreibgeschuetzt")
	}
	// Der Vergleich laeuft ueber den Dienstnamen: Die Pruefung normalisiert
	// das Kommando (systemctl bekommt --no-pager), und genau die normalisierte
	// Fassung steht auch im Original.
	if len(kopie.SudoRules) != 1 || !strings.Contains(kopie.SudoRules[0].Command, "restart nginx") {
		t.Errorf("die kommando-regeln fehlen in der kopie: %+v", kopie.SudoRules)
	}
	if len(kopie.PathRules) != 1 || kopie.PathRules[0].Path != "/var/www" {
		t.Errorf("die pfadregeln fehlen in der kopie: %+v", kopie.PathRules)
	}
	// Entscheidend: Die Regeln haengen an der KOPIE. Waeren die Schluessel
	// mitkopiert worden, haette die Kopie dem Original seine Regeln entzogen.
	for _, r := range kopie.SudoRules {
		if r.ProfileID != kopie.ID {
			t.Errorf("regel haengt an profil %d statt an der kopie %d", r.ProfileID, kopie.ID)
		}
	}
	nachher, err := env.Profiles.Get(source.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(nachher.SudoRules) != 1 {
		t.Errorf("das quellprofil hat seine regeln verloren: %+v", nachher.SudoRules)
	}
}

// TestMitgeliefertesProfilKopieren: Genau dafuer ist die Kopie da.
func TestMitgeliefertesProfilKopieren(t *testing.T) {
	env := newTestEnv(t)
	admin := findProfile(t, env, domain.ProfileSlugFullAdmin)

	kopie, err := env.Profiles.Clone(admin.ID, "voll-admin-kopie", "Voll-Administrator (Kopie)", "admin")
	if err != nil {
		t.Fatalf("mitgeliefertes profil kopieren: %v", err)
	}
	if kopie.Builtin {
		t.Error("die kopie eines mitgelieferten profils muss aenderbar sein")
	}
	// GrantsFullRoot ist kein Eingabefeld - die Kopie erbt es NICHT. Sonst
	// liesse sich die ganze Feinsteuerung mit einem Klick aushebeln.
	if kopie.GrantsFullRoot {
		t.Error("volle root-rechte duerfen sich nicht ueber eine kopie vervielfaeltigen")
	}
	// Und die Beschreibung muss das sagen: Die des Originals verspricht
	// uneingeschraenkte Root-Rechte - fuer die Kopie waere das schlicht falsch.
	if strings.Contains(kopie.Description, "NOPASSWD:ALL") || !strings.Contains(kopie.Description, "OHNE") {
		t.Errorf("die kopie behauptet die rechte des originals: %q", kopie.Description)
	}
}
