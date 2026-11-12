package controllers

import (
	"encoding/json"
	"errors"
	"testing"

	"LCM/internal/api/apierr"
	"LCM/internal/core/domain"
	"LCM/internal/core/services"
)

// TestProfileInUseIsConflict: Ein noch zugewiesenes Profil zu löschen ist ein
// Konflikt (409) mit eigenem Code - kein interner Fehler. Ebenso bekommen die
// neuen Pfad-Sperren ihre eigenen Codes.
func TestProfileInUseIsConflict(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{services.ErrProfileInUse, 409, "profile.in_use"},
		{domain.ErrPathRootEquivalent, 422, "path.root_equivalent"},
		{domain.ErrPathParentRef, 422, "path.parent_reference"},
	} {
		var mapped *apierr.Error
		if !errors.As(mapProfileError(tc.err), &mapped) || mapped.Status != tc.status || mapped.Code != tc.code {
			t.Errorf("%v: erwartet %d %s, bekam %v", tc.err, tc.status, tc.code, mapped)
		}
	}
}

// TestProfileRequestKeepsBlockUses: Die eingesetzten Regelbausteine müssen im
// Service ankommen. Fehlten sie in der Eingabe, verlöre jedes Speichern eines
// Profils alle seine Bausteine.
func TestProfileRequestKeepsBlockUses(t *testing.T) {
	body := `{"name":"web","block_uses":[{"block_id":3,"values":"dir=/srv/www"},{"block_id":5,"values":""}]}`
	var req profileRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatal(err)
	}
	in := req.toInput()
	if len(in.BlockUses) != 2 {
		t.Fatalf("erwartet 2 Bausteine, bekam %d", len(in.BlockUses))
	}
	if in.BlockUses[0].BlockID != 3 || in.BlockUses[0].Values != "dir=/srv/www" {
		t.Errorf("erster Baustein falsch übernommen: %+v", in.BlockUses[0])
	}
}
