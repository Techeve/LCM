package services

import (
	"strings"
	"testing"

	"LCM/internal/core/domain"
)

// TestHelperProfilKommandosSchreibenKeinenFreitext: Im eingeschränkten Modus
// hat der Service-User keine Root-Shell. Die Profile laufen deshalb über den
// Helper - und der bekommt Slug und Inhalt als getrennte, geprüfte Parameter,
// nicht als zusammengebautes Shell-Kommando.
func TestHelperProfilKommandosSchreibenKeinenFreitext(t *testing.T) {
	profile := &domain.PrivilegeProfile{
		ID: 1, Slug: "webserver", Name: "Web",
		SudoRules: []domain.ProfileSudoRule{{Command: "/usr/bin/systemctl --no-pager restart nginx"}},
	}
	cmds := helperProfileApplyCmds([]*domain.PrivilegeProfile{profile})
	if len(cmds) != 2 {
		t.Fatalf("erwartet anwenden + aufräumen, bekam %d: %v", len(cmds), cmds)
	}
	if !strings.Contains(cmds[0], "profile-apply 'webserver'") {
		t.Errorf("anwenden fehlt: %s", cmds[0])
	}
	// Der sudoers-Inhalt geht base64-kodiert - so gibt es über die
	// verschachtelten sudo/sh-Ebenen keine Quoting-Fallen.
	if strings.Contains(cmds[0], "systemctl") {
		t.Errorf("der inhalt darf nicht im klartext im kommando stehen: %s", cmds[0])
	}
	if !strings.Contains(cmds[1], "profile-prune 'webserver'") {
		t.Errorf("aufräumen nennt das behaltene profil nicht: %s", cmds[1])
	}
}

// TestHelperAufraeumenOhneProfile: Bleibt kein Profil übrig, muss der Helper
// trotzdem aufräumen - sonst behielte der Server Gruppe und sudoers-Datei
// eines längst abgezogenen Profils.
func TestHelperAufraeumenOhneProfile(t *testing.T) {
	cmds := helperProfileApplyCmds(nil)
	if len(cmds) != 1 || !strings.Contains(cmds[0], "profile-prune '-'") {
		t.Fatalf("erwartet ein aufräum-kommando, bekam %v", cmds)
	}
}

// TestHelperMitgliedschaft: Auch im eingeschränkten Modus gehört ein Konto in
// genau eine Profilgruppe.
func TestHelperMitgliedschaft(t *testing.T) {
	if got := helperProfileMemberCmd("anna", "webserver"); !strings.Contains(got, "profile-member 'anna' 'webserver'") {
		t.Errorf("mitgliedschaft falsch: %s", got)
	}
	// Ohne Profil wird die Mitgliedschaft gelöst - der Helper erwartet dafür
	// den Platzhalter.
	if got := helperProfileMemberCmd("anna", ""); !strings.Contains(got, "profile-member 'anna' '-'") {
		t.Errorf("lösen der mitgliedschaft falsch: %s", got)
	}
}

// TestHelperPruefungImSkript: Der Helper übernimmt den sudoers-Inhalt nicht
// blind. Ohne diese Prüfung könnte ein kompromittiertes LCM dem
// eingeschränkten Service-User über eine Profil-Datei volle Rechte
// zurückgeben - genau das, was der eingeschränkte Modus verhindern soll.
func TestHelperPruefungImSkript(t *testing.T) {
	for _, marker := range []string{
		"fremde gruppe in profil-regel",
		"uneingeschraenkte regel abgelehnt",
		"visudo -cf",
	} {
		if !strings.Contains(lcmHelperScript, marker) {
			t.Errorf("prüfung fehlt im helper: %s", marker)
		}
	}
	// Die neuen Unterkommandos müssen im Dispatcher stehen, sonst sind sie
	// unerreichbar.
	for _, cmd := range []string{"profile-apply)", "profile-member)", "profile-prune)"} {
		if !strings.Contains(lcmHelperScript, cmd) {
			t.Errorf("unterkommando fehlt im dispatcher: %s", cmd)
		}
	}
}
