package services_test

import (
	"testing"

	"LCM/internal/core/domain"
)

// TestKlonenNimmtDemOriginalNichtDieRegeln: Der Klon-Knopf ist der vorgesehene
// Weg, einen mitgelieferten Baustein anzupassen. Er schrieb die Varianten
// jedoch auf die Kopie UM, statt sie zu kopieren - das Original blieb mit
// leeren Regeln zurück, und jedes Profil, das ihn verwendet, verlor beim
// nächsten Abgleich stillschweigend seine Rechte.
func TestKlonenNimmtDemOriginalNichtDieRegeln(t *testing.T) {
	env := newTestEnv(t)
	blocks, err := env.Blocks.List()
	if err != nil {
		t.Fatal(err)
	}
	var source *domain.ProfileBlock
	for i := range blocks {
		if blocks[i].Slug == "apache-betreiben" {
			source = &blocks[i]
			break
		}
	}
	if source == nil {
		t.Fatal("mitgelieferter Baustein apache-betreiben fehlt")
	}
	vorher := len(source.Variants)
	if vorher == 0 {
		t.Fatal("der Ausgangsbaustein hat keine Varianten - der Test prüft nichts")
	}

	copy1, err := env.Blocks.Clone(source.ID, "apache-betreiben-kopie", "Apache betreiben (Kopie)", "admin")
	if err != nil {
		t.Fatalf("klonen: %v", err)
	}
	if len(copy1.Variants) != vorher {
		t.Errorf("die Kopie hat %d Varianten, erwartet %d", len(copy1.Variants), vorher)
	}

	nachher, err := env.Blocks.Get(source.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(nachher.Variants) != vorher {
		t.Fatalf("das Original hat nach dem Klonen %d Varianten statt %d - die Regeln wurden verschoben",
			len(nachher.Variants), vorher)
	}

	// Und deshalb lässt sich derselbe Baustein auch ein zweites Mal kopieren.
	if _, err := env.Blocks.Clone(source.ID, "apache-betreiben-kopie-2", "Apache betreiben (Kopie) 2", "admin"); err != nil {
		t.Errorf("zweites Klonen: %v", err)
	}
}
