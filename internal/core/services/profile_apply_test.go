package services

import (
	"strings"
	"testing"

	"LCM/internal/core/domain"
)

func testProfile() *domain.PrivilegeProfile {
	return &domain.PrivilegeProfile{
		ID: 5, Slug: "webserver", Name: "Webserver-Betrieb",
		SudoRules: []domain.ProfileSudoRule{
			{Command: "/usr/bin/systemctl --no-pager restart nginx", RunAs: "root"},
			{Command: "/usr/bin/journalctl --no-pager -u nginx", RunAs: "root", RequirePassword: true},
		},
		EditRules: []domain.ProfileEditRule{{Path: "/etc/nginx/sites-available/kunde.conf"}},
	}
}

// TestSudoersInhaltIstEineWhitelist: Die erzeugte Datei darf niemals
// uneingeschränkte Rechte enthalten - das ist der ganze Zweck der Profile.
func TestSudoersInhaltIstEineWhitelist(t *testing.T) {
	content := profileSudoersContent(testProfile())

	if strings.Contains(content, "NOPASSWD:ALL") || strings.Contains(content, "=(ALL) ALL") {
		t.Fatalf("die datei gewährt volle rechte:\n%s", content)
	}
	for _, want := range []string{
		"%lcm-prof-webserver ALL=(root) NOPASSWD: /usr/bin/systemctl --no-pager restart nginx",
		"%lcm-prof-webserver ALL=(root) PASSWD: /usr/bin/journalctl --no-pager -u nginx",
		"%lcm-prof-webserver ALL=(root) NOPASSWD: sudoedit /etc/nginx/sites-available/kunde.conf",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("zeile fehlt: %s\nin:\n%s", want, content)
		}
	}
	// Je Regel eine eigene Zeile: In sudoers trennt das Komma Kommandos, eine
	// zusammengefasste Liste wäre fehleranfälliger und schlechter lesbar.
	if strings.Contains(content, ", /usr/bin/") {
		t.Errorf("regeln wurden zu einer liste zusammengefasst:\n%s", content)
	}
}

// TestSudoersWirdVorDemTauschGeprueft: Ein Syntaxfehler in /etc/sudoers.d legt
// sudo systemweit lahm - auch für LCMs eigenen Zugang. Deshalb erst .tmp,
// dann visudo, dann atomar tauschen.
func TestSudoersWirdVorDemTauschGeprueft(t *testing.T) {
	script := writeProfileSudoersScript(testProfile())
	for _, marker := range []string{
		"/etc/sudoers.d/lcm-prof-webserver.tmp",
		"visudo -cf /etc/sudoers.d/lcm-prof-webserver.tmp",
		"mv /etc/sudoers.d/lcm-prof-webserver.tmp /etc/sudoers.d/lcm-prof-webserver",
		"base64 -d",
	} {
		if !strings.Contains(script, marker) {
			t.Errorf("schritt fehlt: %s\nin: %s", marker, script)
		}
	}
	// Die Prüfung muss VOR dem Tausch stehen.
	if strings.Index(script, "visudo -cf") > strings.Index(script, "mv /etc/sudoers.d/") {
		t.Errorf("visudo läuft erst nach dem tausch: %s", script)
	}
}

// TestMitgliedschaftIstExklusiv: Ein Konto gehört in GENAU EINE Profilgruppe.
// Ohne das Lösen der übrigen summierten sich Rechte beim Profilwechsel auf -
// genau das soll dieses Feature abschaffen.
func TestMitgliedschaftIstExklusiv(t *testing.T) {
	script := setProfileMembershipScript("anna", "webserver")
	if !strings.Contains(script, "lcm-prof-*") {
		t.Errorf("fremde profilgruppen werden nicht gelöst: %s", script)
	}
	if !strings.Contains(script, "usermod -aG lcm-prof-webserver anna") {
		t.Errorf("aufnahme in die gewünschte gruppe fehlt: %s", script)
	}
	// Ohne Profil bleibt nur das Lösen übrig.
	ohne := setProfileMembershipScript("anna", "")
	if strings.Contains(ohne, "usermod -aG") {
		t.Errorf("ohne profil darf keine gruppe gesetzt werden: %s", ohne)
	}
	if !strings.Contains(ohne, "lcm-prof-*") {
		t.Errorf("ohne profil müssen bestehende mitgliedschaften gelöst werden: %s", ohne)
	}
}

// TestMitgliedschaftLoesenAufBusyBox: Auf BusyBox-Systemen (Alpine) gibt es
// kein gpasswd, und deluser kennt NUR die Form "deluser USER" - mit zwei
// Argumenten bricht es mit einer Usage-Meldung ab, ohne etwas zu tun. Fehlt
// der dritte Weg, bleibt das Konto beim Profilwechsel in der alten
// Profilgruppe und behält deren sudo-Rechte ZUSÄTZLICH zu den neuen. Genau das
// soll die Exklusivität verhindern.
//
// Im Langzeittest (Etappe G) auf Alpine 3.23 nachgewiesen: "deluser USER
// GROUP" endet mit Exit 1 und gelöster Mitgliedschaft - Fehlanzeige;
// "delgroup USER GROUP" endet mit Exit 0 und löst sie, ohne Benutzer oder
// Gruppe anzutasten.
func TestMitgliedschaftLoesenAufBusyBox(t *testing.T) {
	script := setProfileMembershipScript("anna", "webserver")
	for _, route := range []string{"gpasswd -d anna", "deluser anna", "delgroup anna"} {
		if !strings.Contains(script, route) {
			t.Errorf("weg %q fehlt - auf mindestens einer Distributionsfamilie bliebe die alte mitgliedschaft stehen: %s", route, script)
		}
	}
	// Die Reihenfolge ist nicht beliebig: delgroup ist der letzte Ausweg,
	// nicht der erste - auf Systemen mit shadow-utils gehört gpasswd zuerst.
	if strings.Index(script, "gpasswd -d anna") > strings.Index(script, "delgroup anna") {
		t.Errorf("delgroup steht vor gpasswd: %s", script)
	}
}

// TestAufraeumenNimmtNurEigeneSpuren: Aufgeräumt wird anhand des Präfixes auf
// dem Server selbst. Eine Gruppe mit Mitgliedern wird nicht gelöscht - LCM
// fasst fremde Zuordnungen nicht an.
func TestAufraeumenNimmtNurEigeneSpuren(t *testing.T) {
	script := pruneProfilesScript([]string{"webserver"})
	if !strings.Contains(script, "KEEP=' lcm-prof-webserver '") {
		t.Errorf("gewünschtes profil steht nicht in der behalten-liste: %s", script)
	}
	if !strings.Contains(script, "/etc/sudoers.d/lcm-prof-*") {
		t.Errorf("es wird nicht über das präfix aufgeräumt: %s", script)
	}
	if !strings.Contains(script, `cut -d: -f4`) {
		t.Errorf("mitglieder-prüfung vor dem löschen der gruppe fehlt: %s", script)
	}
}

// TestWirkungDerProfile: Die mitgelieferten Profile laufen weiter über den
// bisherigen per-Benutzer-Weg - nur so ändert das Update auf bestehenden
// Servern nichts. Eigene Profile wirken über die Gruppe.
func TestWirkungDerProfile(t *testing.T) {
	user := &domain.LinuxUser{Username: "anna"}
	admin := &domain.PrivilegeProfile{Slug: domain.ProfileSlugFullAdmin, Builtin: true, GrantsFullRoot: true}
	standard := &domain.PrivilegeProfile{Slug: domain.ProfileSlugStandard, Builtin: true}
	eigen := &domain.PrivilegeProfile{Slug: "webserver"}

	if got := effectFor(user, admin); !got.FullRoot || got.GroupSlug != "" {
		t.Errorf("voll-administrator: erwartet volle rechte ohne gruppe, bekam %+v", got)
	}
	if got := effectFor(user, standard); got.FullRoot || got.GroupSlug != "" {
		t.Errorf("standardbenutzer: erwartet keine rechte, bekam %+v", got)
	}
	if got := effectFor(user, eigen); got.FullRoot || got.GroupSlug != "webserver" {
		t.Errorf("eigenes profil: erwartet gruppenmitgliedschaft, bekam %+v", got)
	}
	// Altbestand ohne Profil: Das alte sudo-Bit entscheidet weiter - ein
	// Konto darf durch eine fehlende Zuordnung weder Rechte verlieren noch
	// welche dazubekommen.
	if got := effectFor(&domain.LinuxUser{Username: "alt", Sudo: true}, nil); !got.FullRoot {
		t.Errorf("ohne profil muss das alte sudo-bit gelten, bekam %+v", got)
	}
	if got := effectFor(&domain.LinuxUser{Username: "alt"}, nil); got.FullRoot {
		t.Errorf("ohne profil und ohne sudo darf es keine rechte geben, bekam %+v", got)
	}
}

// TestNurEigeneProfileWerdenAufDenServerGebracht: Für mitgelieferte Profile
// entsteht keine Gruppe und keine Datei.
func TestNurEigeneProfileWerdenAufDenServerGebracht(t *testing.T) {
	admin := &domain.PrivilegeProfile{ID: 1, Slug: domain.ProfileSlugFullAdmin, Builtin: true, GrantsFullRoot: true}
	eigen := &domain.PrivilegeProfile{ID: 2, Slug: "webserver"}
	got := ownProfiles(map[string]*domain.PrivilegeProfile{
		"anna": admin, "bert": eigen, "cara": eigen, "dora": nil,
	})
	if len(got) != 1 || got[0].Slug != "webserver" {
		t.Fatalf("erwartet genau das eigene profil, bekam %+v", got)
	}
}

// TestProfilSkriptRaeumtAuchOhneRegelnAuf: Ein Profil nur mit
// Verzeichnisrechten braucht die Gruppe (für die späteren ACLs), aber keine
// sudoers-Datei - eine vorhandene muss verschwinden.
func TestProfilSkriptRaeumtAuchOhneRegelnAuf(t *testing.T) {
	withoutRules := &domain.PrivilegeProfile{ID: 3, Slug: "nurdaten"}
	script := profileApplyScript([]*domain.PrivilegeProfile{withoutRules})
	if !strings.Contains(script, "groupadd lcm-prof-nurdaten") {
		t.Errorf("gruppe wird nicht angelegt: %s", script)
	}
	if !strings.Contains(script, "rm -f /etc/sudoers.d/lcm-prof-nurdaten") {
		t.Errorf("leere sudoers-datei wird nicht entfernt: %s", script)
	}
}

// TestPauschalRegelWirdGemeldet: openSUSE liefert ab Werk eine sudo-Zeile aus,
// die JEDEM alles erlaubt ("ALL ALL=(ALL) ALL" mit "Defaults targetpw"). Das
// Profil gilt dann zusätzlich, begrenzt aber nicht - und "sudo -l" zeigt für
// ein Profilkonto trotzdem "(ALL) ALL". Im Langzeittest (Etappe G) fiel das auf
// openSUSE Leap 16 auf. LCM ändert die Zeile nicht, muss sie aber benennen.
func TestPauschalRegelWirdGemeldet(t *testing.T) {
	profil := &domain.PrivilegeProfile{
		Slug: "web", Name: "Web",
		SudoRules: []domain.ProfileSudoRule{{Command: "/usr/bin/systemctl --no-pager restart nginx"}},
	}
	script := profileApplyScript([]*domain.PrivilegeProfile{profil})
	if !strings.Contains(script, "LCM-PAUSCHALREGEL") {
		t.Errorf("das Anwenden-Skript sucht gar nicht nach der Pauschalregel:\n%s", script)
	}
	// Auch /usr/etc muss geprüft werden - dort liegt sie bei openSUSE 16.
	if !strings.Contains(script, "/usr/etc/sudoers") {
		t.Errorf("openSUSE legt die Datei unter /usr/etc ab, dort wird nicht gesucht:\n%s", script)
	}

	hinweise := pauschalRegelHinweise("etwas anderes\nLCM-PAUSCHALREGEL: /usr/etc/sudoers\nnoch was\n")
	if len(hinweise) != 1 {
		t.Fatalf("erwartete genau einen Hinweis, bekam %v", hinweise)
	}
	for _, muss := range []string{"/usr/etc/sudoers", "begrenzt aber nicht", "root-Passwort"} {
		if !strings.Contains(hinweise[0], muss) {
			t.Errorf("der Hinweis sagt nicht, worum es geht (%q fehlt): %s", muss, hinweise[0])
		}
	}
	if len(pauschalRegelHinweise("alles ruhig\n")) != 0 {
		t.Error("ohne Marker darf kein Hinweis entstehen")
	}
}
