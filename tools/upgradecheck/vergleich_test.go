package main

import "testing"

func inv(version string, tabellen map[string]int, ident map[string][]string, verschl map[string]bool) *Inventar {
	if verschl == nil {
		verschl = map[string]bool{}
	}
	return &Inventar{Version: version, Tabellen: tabellen, Identitaet: ident, Verschluesselt: verschl}
}

func unerklaert(befunde []Befund) []Befund {
	var offen []Befund
	for _, b := range befunde {
		if !b.Erklaert {
			offen = append(offen, b)
		}
	}
	return offen
}

// Ein Upgrade, das nichts wegnimmt, darf nicht anschlagen - sonst wird der
// Test nach dem dritten Fehlalarm ignoriert.
func TestNeueTabellenUndSpaltenSindKeinBefund(t *testing.T) {
	vorher := inv("1.11.0", map[string]int{"servers": 3}, map[string][]string{"servers": {"a", "b", "c"}}, nil)
	nachher := inv("neu", map[string]int{"servers": 3, "privilege_profiles": 2},
		map[string][]string{"servers": {"a", "b", "c"}}, nil)
	if offen := unerklaert(vergleiche(vorher, nachher, nil)); len(offen) != 0 {
		t.Errorf("neue Tabellen wurden als Abweichung gemeldet: %+v", offen)
	}
}

// Der Fall, für den es den Test gibt: Zeilen sind weg und niemand hat es
// angekündigt.
func TestVerloreneZeilenSindRot(t *testing.T) {
	vorher := inv("1.11.0", map[string]int{"servers": 5}, nil, nil)
	nachher := inv("neu", map[string]int{"servers": 2}, nil, nil)
	offen := unerklaert(vergleiche(vorher, nachher, nil))
	if len(offen) != 1 {
		t.Fatalf("erwartete genau einen Befund, bekam %+v", offen)
	}
}

// Ein angekündigter Umzug ist in Ordnung - aber nur, wenn die Zeilen am Ziel
// auch ankommen. Sonst wäre die Ankündigung ein Freibrief.
func TestUmzugWirdNachgerechnet(t *testing.T) {
	erwartung := []Erwartung{{
		AbVersion: "1.19.0", Betrifft: "rules", Art: artUmgezogen, Nach: "schedules",
		Begruendung: "Regeln hängen jetzt an Zeitplänen",
	}}

	angekommen := vergleiche(
		inv("alt", map[string]int{"rules": 5, "schedules": 0}, nil, nil),
		inv("neu", map[string]int{"rules": 0, "schedules": 5}, nil, nil), erwartung)
	if offen := unerklaert(angekommen); len(offen) != 0 {
		t.Errorf("vollständiger Umzug wurde beanstandet: %+v", offen)
	}

	verschwunden := vergleiche(
		inv("alt", map[string]int{"rules": 5, "schedules": 0}, nil, nil),
		inv("neu", map[string]int{"rules": 0, "schedules": 2}, nil, nil), erwartung)
	if offen := unerklaert(verschwunden); len(offen) != 1 {
		t.Errorf("unvollständiger Umzug hätte auffallen müssen: %+v", verschwunden)
	}
}

// Ab 1.15 sind Namen verschlüsselt; Klartext und Blindindex sind nicht
// vergleichbar. Statt Namensgleichheit wird dann Vollständigkeit geprüft.
func TestVerschluesselungsgrenze(t *testing.T) {
	vorher := inv("1.11.0", map[string]int{"users": 3},
		map[string][]string{"users": {"admin", "ops", "system"}}, nil)

	vollstaendig := inv("neu", map[string]int{"users": 3},
		map[string][]string{"users": {"h1", "h2", "h3"}}, map[string]bool{"users": true})
	if offen := unerklaert(vergleiche(vorher, vollstaendig, nil)); len(offen) != 0 {
		t.Errorf("verschlüsselte, vollständige Kennungen wurden beanstandet: %+v", offen)
	}

	// Eine Zeile ohne Kennung muss auffallen - sonst wäre die Grenze ein Loch.
	luecke := inv("neu", map[string]int{"users": 3},
		map[string][]string{"users": {"h1", "h2"}}, map[string]bool{"users": true})
	if offen := unerklaert(vergleiche(vorher, luecke, nil)); len(offen) != 1 {
		t.Errorf("fehlende Kennung hätte auffallen müssen: %+v", vergleiche(vorher, luecke, nil))
	}

	// Doppelte Kennungen ebenso: Zwei Zeilen mit demselben Blindindex heißen,
	// dass eine Identität verlorengegangen ist.
	doppelt := inv("neu", map[string]int{"users": 3},
		map[string][]string{"users": {"h1", "h1", "h2"}}, map[string]bool{"users": true})
	if offen := unerklaert(vergleiche(vorher, doppelt, nil)); len(offen) != 1 {
		t.Errorf("doppelte Kennung hätte auffallen müssen: %+v", vergleiche(vorher, doppelt, nil))
	}
}

// Eine verschwundene Tabelle ist rot - es sei denn, jemand hat es erklärt.
func TestVerschwundeneTabelle(t *testing.T) {
	vorher := inv("alt", map[string]int{"alt_tabelle": 4}, nil, nil)
	nachher := inv("neu", map[string]int{}, nil, nil)

	if offen := unerklaert(vergleiche(vorher, nachher, nil)); len(offen) != 1 {
		t.Errorf("verschwundene Tabelle hätte auffallen müssen: %+v", offen)
	}
	erklaert := []Erwartung{{AbVersion: "1.20.0", Betrifft: "alt_tabelle",
		Art: artTabelleEntfernt, Begruendung: "durch neues Modell ersetzt"}}
	if offen := unerklaert(vergleiche(vorher, nachher, erklaert)); len(offen) != 0 {
		t.Errorf("erklärte Entfernung wurde trotzdem beanstandet: %+v", offen)
	}
}
