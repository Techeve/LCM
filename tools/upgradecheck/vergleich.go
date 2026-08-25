package main

import "fmt"

// Erwartung erklärt eine Abweichung, die beim Upgrade AUFTRETEN SOLL.
//
// Der Test schlägt bei jeder Abweichung fehl, die keine Erwartung deckt. Damit
// kostet eine bewusste Datenänderung drei Zeilen Erklärung - und eine
// unbewusste fällt auf. Ohne diesen Mechanismus wäre der Test entweder
// dauerhaft rot (weil sich die Anwendung weiterentwickelt) oder so weich
// eingestellt, dass er nichts mehr findet.
type Erwartung struct {
	AbVersion   string `json:"ab_version"`
	Betrifft    string `json:"betrifft"` // Tabellenname
	Art         string `json:"art"`      // siehe unten
	Nach        string `json:"nach,omitempty"`
	Begruendung string `json:"begruendung"`
}

// Zulässige Arten:
//
//	tabelle_entfernt        Die Tabelle gibt es nicht mehr.
//	zeilen_sinken           Die Zeilenzahl darf sinken (z.B. Aufräum-Migration).
//	umgezogen               Inhalt ist in eine andere Tabelle gewandert
//	                        ("nach"); dort muss die Zahl entsprechend steigen.
//	identitaeten_geaendert  Namen/Kennungen werden umgeschrieben.
const (
	artTabelleEntfernt = "tabelle_entfernt"
	artZeilenSinken    = "zeilen_sinken"
	artUmgezogen       = "umgezogen"
	artIdentGeaendert  = "identitaeten_geaendert"
)

// Befund ist eine einzelne Abweichung samt Bewertung.
type Befund struct {
	Tabelle  string
	Text     string
	Erklaert bool
	Grund    string // welche Erwartung greift
}

func vergleiche(vorher, nachher *Inventar, erwartungen []Erwartung) []Befund {
	var befunde []Befund

	deckt := func(tabelle, art string) (Erwartung, bool) {
		for _, e := range erwartungen {
			if e.Betrifft == tabelle && e.Art == art {
				return e, true
			}
		}
		return Erwartung{}, false
	}
	report := func(tabelle, text, art string) {
		e, ok := deckt(tabelle, art)
		b := Befund{Tabelle: tabelle, Text: text, Erklaert: ok}
		if ok {
			b.Grund = fmt.Sprintf("erklärt ab %s: %s", e.AbVersion, e.Begruendung)
		}
		befunde = append(befunde, b)
	}

	for tabelle, beforeLines := range vorher.Tabellen {
		nachZeilen, existing := nachher.Tabellen[tabelle]
		if !existing {
			report(tabelle, fmt.Sprintf("Tabelle verschwunden (vorher %d Zeilen)", beforeLines), artTabelleEntfernt)
			continue
		}
		if nachZeilen < beforeLines {
			// Umzug prüfen: Ist der Verlust in der Zieltabelle angekommen?
			if e, ok := deckt(tabelle, artUmgezogen); ok && e.Nach != "" {
				zielVor, zielNach := vorher.Tabellen[e.Nach], nachher.Tabellen[e.Nach]
				if zielNach-zielVor >= beforeLines-nachZeilen {
					befunde = append(befunde, Befund{
						Tabelle: tabelle, Erklaert: true,
						Text:  fmt.Sprintf("%d Zeilen nach %s umgezogen", beforeLines-nachZeilen, e.Nach),
						Grund: fmt.Sprintf("erklärt ab %s: %s", e.AbVersion, e.Begruendung),
					})
					continue
				}
				befunde = append(befunde, Befund{
					Tabelle: tabelle, Erklaert: false,
					Text: fmt.Sprintf("Umzug nach %s angekündigt, dort kamen aber nur %d von %d Zeilen an",
						e.Nach, zielNach-zielVor, beforeLines-nachZeilen),
				})
				continue
			}
			report(tabelle, fmt.Sprintf("Zeilen verloren: %d -> %d", beforeLines, nachZeilen), artZeilenSinken)
		}
	}

	for tabelle, vorIdent := range vorher.Identitaet {
		nachIdent := nachher.Identitaet[tabelle]

		// Wurde die Spalte zwischenzeitlich verschluesselt, sind die Kennungen
		// nicht mehr vergleichbar - vorher Klartext, nachher Blindindex. Statt
		// Namen wird dann geprueft, dass JEDE Zeile weiterhin eine eigene,
		// nicht leere Kennung traegt. Ein stiller Verlust faellt damit
		// weiterhin auf: Er zeigte sich als fehlende oder doppelte Kennung.
		if !vorher.Verschluesselt[tabelle] && nachher.Verschluesselt[tabelle] {
			befunde = append(befunde, checkIdentifiers(tabelle, nachher))
			continue
		}
		existing := map[string]bool{}
		for _, w := range nachIdent {
			existing[w] = true
		}
		var missing []string
		for _, w := range vorIdent {
			if !existing[w] {
				missing = append(missing, w)
			}
		}
		if len(missing) > 0 {
			report(tabelle, fmt.Sprintf("nicht mehr auffindbar: %v", gekuerzt(missing)), artIdentGeaendert)
		}
	}
	return befunde
}

func gekuerzt(values []string) []string {
	if len(values) <= 5 {
		return values
	}
	return append(append([]string{}, values[:5]...), fmt.Sprintf("… und %d weitere", len(values)-5))
}

// pruefeKennungen prueft eine Tabelle, deren Kennung inzwischen verschluesselt
// ist: Jede Zeile muss genau eine eigene, nicht leere Kennung haben.
func checkIdentifiers(tabelle string, nachher *Inventar) Befund {
	kennungen := nachher.Identitaet[tabelle]
	lines := nachher.Tabellen[tabelle]
	eindeutig := map[string]bool{}
	for _, k := range kennungen {
		if k != "" {
			eindeutig[k] = true
		}
	}
	switch {
	case len(kennungen) != lines:
		return Befund{Tabelle: tabelle, Erklaert: false,
			Text: fmt.Sprintf("%d Zeilen, aber nur %d Kennungen - Zeilen ohne Kennung", lines, len(kennungen))}
	case len(eindeutig) != lines:
		return Befund{Tabelle: tabelle, Erklaert: false,
			Text: fmt.Sprintf("%d Zeilen, aber nur %d verschiedene Kennungen - Kennungen doppelt oder leer", lines, len(eindeutig))}
	}
	return Befund{Tabelle: tabelle, Erklaert: true,
		Text:  fmt.Sprintf("Kennung inzwischen verschluesselt; %d Zeilen tragen je eine eigene", lines),
		Grund: "Klartext und Blindindex sind nicht vergleichbar - geprueft wird Vollstaendigkeit statt Namensgleichheit"}
}
