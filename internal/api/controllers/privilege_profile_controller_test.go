package controllers

import (
	"encoding/json"
	"testing"
)

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
