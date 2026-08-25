package services_test

import (
	"strings"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

// TestBenutzerUndKeyOperationenImAudit (R2-048): Genau die Operationen, die
// Rechte vergeben - Benutzer anlegen/ändern/löschen, Rollen setzen,
// Passwörter zurücksetzen, API-Keys erzeugen/widerrufen - müssen in der
// Audit-Kette stehen. Vorher gab es dafür keinen einzigen Eintrag.
func TestBenutzerUndKeyOperationenImAudit(t *testing.T) {
	env := newTestEnv(t)
	auditRepo := repositories.NewAuditRepository(env.DB())

	// Der komplette Lebenszyklus eines Kontos.
	u, err := env.Users.CreateUser("audituser", "a@example.org", "Anker5-Leuchtturm!Wind", "Au", "Dit", []string{domain.RoleManager}, "test-admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.Users.UpdateUserRoles(u.ID, []string{domain.RoleAdmin}, "test-admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := env.Users.UpdateProfile(u.ID, "b@example.org", "Neu", "Dit", "test-admin"); err != nil {
		t.Fatal(err)
	}
	if err := env.Users.ResetPassword(u.ID, "Regen9-Amsel!Turmfalk", false, "test-admin"); err != nil {
		t.Fatal(err)
	}
	plain, key, err := env.APIKeys.Create("audit-key", u.ID, domain.APIKeyScopeRead, nil, "test-admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := env.APIKeys.Revoke(key.ID, "test-admin"); err != nil {
		t.Fatal(err)
	}
	if err := env.Users.DeleteUser(u.ID, "test-admin"); err != nil {
		t.Fatal(err)
	}

	entries, err := auditRepo.FindRecent(200)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]string{} // action -> details
	for _, e := range entries {
		found[e.Action] = e.Details
	}
	for _, action := range []string{
		"user.create", "user.set-roles", "user.update-profile",
		"user.reset-password", "user.delete", "apikey.create", "apikey.revoke",
	} {
		if _, ok := found[action]; !ok {
			t.Errorf("Audit-Eintrag %q fehlt (vorhanden: %v)", action, keysOf(found))
		}
	}
	// Die Details müssen tragen, was ein Prüfer wissen will - und NIE das
	// Geheimnis selbst.
	if !strings.Contains(found["user.set-roles"], domain.RoleAdmin) {
		t.Errorf("Rollenvergabe ohne Rollenliste: %q", found["user.set-roles"])
	}
	if !strings.Contains(found["user.reset-password"], "durch Administrator") {
		t.Errorf("Fremd-Reset nicht als solcher gekennzeichnet: %q", found["user.reset-password"])
	}
	if !strings.Contains(found["apikey.create"], "Scope "+domain.APIKeyScopeRead) {
		t.Errorf("Key-Erzeugung ohne Scope: %q", found["apikey.create"])
	}
	// Das oeffentliche Key-PREFIX ist im Audit erwuenscht (es verknuepft den
	// Eintrag mit Access-Log und Key-Liste, R2-053) - das Geheimnis dahinter
	// darf nie auftauchen: geprueft wird alles JENSEITS des Prefixes.
	secretPart := plain[len(key.Prefix):]
	for action, details := range found {
		if strings.Contains(details, "Regen9-Amsel") || strings.Contains(details, secretPart) ||
			strings.Contains(details, plain) {
			t.Errorf("%s: Geheimnis im Audit-Log: %q", action, details)
		}
	}
	if !strings.Contains(found["apikey.revoke"], key.Prefix) {
		t.Errorf("apikey.revoke ohne Prefix - Eintrag nicht mit Key-Liste/Access-Log verknuepfbar: %q", found["apikey.revoke"])
	}
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
