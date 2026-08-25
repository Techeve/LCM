package main

import (
	"database/sql"
	"fmt"
	"sort"

	_ "modernc.org/sqlite"
)

// Inventar ist die Bestandsaufnahme einer Datenbank - bewusst grob.
//
// Verglichen werden ZUSAGEN, keine Zustände: Welche Tabellen gibt es, wie
// viele Zeilen tragen sie, und welche fachlichen Identitäten stecken darin.
// IDs bleiben absichtlich außen vor - die UUID-Migration hat sie schon einmal
// reihenweise geändert, ohne dass ein einziger Datensatz verloren ging.
type Inventar struct {
	Version    string              `json:"version"`
	Tabellen   map[string]int      `json:"tabellen"`   // Name -> Zeilenzahl
	Spalten    map[string][]string `json:"spalten"`    // Name -> Spaltennamen
	Identitaet map[string][]string `json:"identitaet"` // Name -> sortierte Kennungen
	// Verschluesselt merkt sich, ob die Kennung ein Blindindex ist statt
	// Klartext. Ueber diese Grenze hinweg sind Kennungen nicht vergleichbar -
	// Klartext von vorher gegen Hashwerte von nachher ergaebe nur Rauschen.
	// Der Schutz geht dadurch nicht verloren: Statt der Namen prueft der
	// Vergleich dann, dass JEDE Zeile weiterhin eine eigene, nicht leere
	// Kennung traegt.
	Verschluesselt map[string]bool `json:"verschluesselt"`
}

// identitaetsSpalten benennt je Tabelle das Feld, das einen Datensatz für
// einen Menschen wiedererkennbar macht. Nur diese Tabellen werden auf
// Vollständigkeit geprüft - bei Protokollen und Zwischenspeichern wäre das
// weder möglich noch sinnvoll.
var identitaetsSpalten = map[string]string{
	"servers":       "name",
	"server_groups": "name",
	"users":         "username",
	"linux_users":   "username",
	"schedules":     "name",
}

func erfasse(path, version string) (*Inventar, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return nil, err
	}
	defer db.Close()

	inv := &Inventar{
		Version:        version,
		Tabellen:       map[string]int{},
		Spalten:        map[string][]string{},
		Identitaet:     map[string][]string{},
		Verschluesselt: map[string]bool{},
	}

	names, err := tabellennamen(db)
	if err != nil {
		return nil, err
	}
	for _, name := range names {
		count, err := zeilenzahl(db, name)
		if err != nil {
			return nil, fmt.Errorf("zeilen von %s: %w", name, err)
		}
		inv.Tabellen[name] = count

		spalten, err := spaltennamen(db, name)
		if err != nil {
			return nil, fmt.Errorf("spalten von %s: %w", name, err)
		}
		inv.Spalten[name] = spalten

		spalte, gewuenscht := identitaetsSpalten[name]
		if !gewuenscht {
			continue
		}
		// Verschlüsselte Felder liefern bei jedem Lauf andere Bytes. Steht
		// neben der Spalte ein Blindindex, ist DER die stabile Kennung.
		if enthaelt(spalten, spalte+"_bidx") {
			spalte += "_bidx"
			inv.Verschluesselt[name] = true
		}
		values, err := identitaeten(db, name, spalte)
		if err != nil {
			return nil, fmt.Errorf("identitäten von %s: %w", name, err)
		}
		inv.Identitaet[name] = values
	}
	return inv, nil
}

func tabellennamen(db *sql.DB) ([]string, error) {
	rows, err := db.Query(`SELECT name FROM sqlite_master
		WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		names = append(names, n)
	}
	return names, rows.Err()
}

func zeilenzahl(db *sql.DB, tabelle string) (int, error) {
	var n int
	// Tabellennamen kommen aus sqlite_master, nicht von außen - Quoting
	// genügt hier, ein Platzhalter ist für Bezeichner nicht möglich.
	err := db.QueryRow(`SELECT count(*) FROM "` + tabelle + `"`).Scan(&n)
	return n, err
}

func spaltennamen(db *sql.DB, tabelle string) ([]string, error) {
	rows, err := db.Query(`SELECT name FROM pragma_table_info("` + tabelle + `") ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		names = append(names, n)
	}
	return names, rows.Err()
}

func identitaeten(db *sql.DB, tabelle, spalte string) ([]string, error) {
	rows, err := db.Query(`SELECT "` + spalte + `" FROM "` + tabelle + `"`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var values []string
	for rows.Next() {
		var w sql.NullString
		if err := rows.Scan(&w); err != nil {
			return nil, err
		}
		if w.Valid {
			values = append(values, w.String)
		}
	}
	sort.Strings(values)
	return values, rows.Err()
}

func enthaelt(liste []string, wert string) bool {
	for _, e := range liste {
		if e == wert {
			return true
		}
	}
	return false
}
