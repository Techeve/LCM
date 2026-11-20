package storage

import (
	"fmt"
	"strings"
	"testing"

	"gorm.io/gorm"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

// schemaSpalte ist eine Zeile aus pragma_table_info.
type schemaSpalte struct {
	Name    string
	Type    string
	NotNull bool    `gorm:"column:notnull"`
	Dflt    *string `gorm:"column:dflt_value"`
	PK      int     `gorm:"column:pk"`
}

// schemaFK ist eine Zeile aus pragma_foreign_key_list.
type schemaFK struct {
	Table string `gorm:"column:table"`
	From  string `gorm:"column:from"`
	To    string `gorm:"column:to"`
}

func schemaTabellen(t *testing.T, db *gorm.DB) []string {
	t.Helper()
	var tabellen []string
	if err := db.Raw("SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name").
		Scan(&tabellen).Error; err != nil {
		t.Fatal(err)
	}
	return tabellen
}

func schemaSpalten(t *testing.T, db *gorm.DB, tabelle string) []schemaSpalte {
	t.Helper()
	var spalten []schemaSpalte
	if err := db.Raw(fmt.Sprintf("SELECT name, type, \"notnull\", dflt_value, pk FROM pragma_table_info('%s')", tabelle)).
		Scan(&spalten).Error; err != nil {
		t.Fatal(err)
	}
	return spalten
}

func hatSpalte(spalten []schemaSpalte, name string) bool {
	for _, s := range spalten {
		if s.Name == name {
			return true
		}
	}
	return false
}

// serverTabellen liest aus dem Schema jede Tabelle, deren Zeilen über
// server_id oder server_ref an einem Server hängen.
func serverTabellen(t *testing.T, db *gorm.DB) []string {
	t.Helper()
	var treffer []string
	for _, tabelle := range schemaTabellen(t, db) {
		spalten := schemaSpalten(t, db, tabelle)
		if hatSpalte(spalten, "server_id") || hatSpalte(spalten, "server_ref") {
			treffer = append(treffer, tabelle)
		}
	}
	return treffer
}

// serverBedingung liefert die WHERE-Bedingung, mit der eine Tabelle auf einen
// Server zeigt: server_ref ist das HMAC-Token der id, nicht die id selbst.
func serverBedingung(spalten []schemaSpalte, id uint) (string, any) {
	if hatSpalte(spalten, "server_ref") {
		return "server_ref = ?", repositories.ServerRef(id)
	}
	return "server_id = ?", id
}

// fuellwert liefert für eine Pflichtspalte einen Wert, den die Tabelle annimmt.
func fuellwert(tabelle string, s schemaSpalte, serverID uint) any {
	typ := strings.ToUpper(s.Type)
	switch {
	case s.PK > 0 && strings.Contains(typ, "TEXT"):
		return fmt.Sprintf("%s-%d", tabelle, serverID) // UUID-Schlüssel
	case strings.Contains(typ, "INT"), strings.Contains(typ, "NUM"), strings.Contains(typ, "REAL"):
		return serverID // je Server verschieden, falls die Spalte in einem Unique-Index steht
	case strings.Contains(typ, "DATE"):
		return "2026-01-01 00:00:00"
	default:
		return fmt.Sprintf("wert-%d", serverID)
	}
}

// eineZeileAnlegen schreibt eine Zeile, die auf den Server zeigt. Pflichtspalten
// bekommen Füllwerte, Fremdschlüssel die erste Zeile der Elterntabelle. Ist die
// Elterntabelle noch leer, meldet sie false - dann folgt der nächste Durchgang.
func eineZeileAnlegen(t *testing.T, db *gorm.DB, tabelle string, serverID uint) bool {
	t.Helper()
	var fks []schemaFK
	if err := db.Raw(fmt.Sprintf("SELECT \"table\", \"from\", \"to\" FROM pragma_foreign_key_list('%s')", tabelle)).
		Scan(&fks).Error; err != nil {
		t.Fatal(err)
	}
	eltern := map[string]schemaFK{}
	for _, fk := range fks {
		eltern[fk.From] = fk
	}

	spalten := schemaSpalten(t, db, tabelle)
	var namen, platzhalter []string
	var werte []any
	for _, s := range spalten {
		wert, ok := spaltenWert(t, db, tabelle, s, spalten, eltern, serverID)
		if !ok {
			return false
		}
		if wert == nil {
			continue
		}
		namen = append(namen, `"`+s.Name+`"`)
		platzhalter = append(platzhalter, "?")
		werte = append(werte, wert)
	}
	q := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", tabelle, strings.Join(namen, ", "), strings.Join(platzhalter, ", "))
	if err := db.Exec(q, werte...).Error; err != nil {
		t.Fatalf("%s: Zeile für Server %d nicht anlegbar: %v", tabelle, serverID, err)
	}
	return true
}

// spaltenWert entscheidet über den Wert einer Spalte; nil heißt weglassen,
// false heißt, die Elterntabelle ist noch leer.
func spaltenWert(t *testing.T, db *gorm.DB, tabelle string, s schemaSpalte, alle []schemaSpalte,
	eltern map[string]schemaFK, serverID uint) (any, bool) {
	t.Helper()
	switch {
	case s.Name == "server_id":
		return serverID, true
	case s.Name == "server_ref":
		return repositories.ServerRef(serverID), true
	}
	if fk, ok := eltern[s.Name]; ok {
		return elternID(t, db, fk, serverID)
	}
	if eigeneRowID(s, alle) {
		return nil, true
	}
	if s.PK > 0 || (s.NotNull && s.Dflt == nil) {
		return fuellwert(tabelle, s, serverID), true
	}
	return nil, true
}

// elternID wählt die Elternzeile eines Fremdschlüssels. Hängt die Elterntabelle
// selbst an einem Server, muss es dessen Zeile sein - ein Befund des einen
// Servers im Bericht des anderen verschwände sonst mit dem falschen.
func elternID(t *testing.T, db *gorm.DB, fk schemaFK, serverID uint) (any, bool) {
	t.Helper()
	q := db.Table(fk.Table).Select(fk.To)
	if hatSpalte(schemaSpalten(t, db, fk.Table), "server_id") && fk.Table != "servers" {
		q = q.Where("server_id = ?", serverID)
	}
	var ids []any
	if err := q.Limit(1).Pluck(fk.To, &ids).Error; err != nil {
		t.Fatal(err)
	}
	if len(ids) == 0 {
		return nil, false
	}
	return ids[0], true
}

// eigeneRowID: Ein einzelner INTEGER-Primärschlüssel vergibt SQLite selbst.
func eigeneRowID(s schemaSpalte, alle []schemaSpalte) bool {
	if s.PK != 1 || !strings.EqualFold(s.Type, "integer") {
		return false
	}
	for _, andere := range alle {
		if andere.PK > 1 {
			return false
		}
	}
	return true
}

// serverBestandAnlegen legt in jeder Server-Tabelle eine Zeile für den Server
// an. Abhängige Tabellen (Befund → Bericht) warten auf ihre Eltern.
func serverBestandAnlegen(t *testing.T, db *gorm.DB, tabellen []string, serverID uint) {
	t.Helper()
	offen := append([]string{}, tabellen...)
	for len(offen) > 0 {
		var spaeter []string
		for _, tabelle := range offen {
			if !eineZeileAnlegen(t, db, tabelle, serverID) {
				spaeter = append(spaeter, tabelle)
			}
		}
		if len(spaeter) == len(offen) {
			t.Fatalf("Für %v fehlt eine Elternzeile - im Test vorher anlegen", spaeter)
		}
		offen = spaeter
	}
}

func zeilenDesServers(t *testing.T, db *gorm.DB, tabelle string, id uint) int64 {
	t.Helper()
	bedingung, wert := serverBedingung(schemaSpalten(t, db, tabelle), id)
	var n int64
	if err := db.Table(tabelle).Where(bedingung, wert).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

// TestServerLoeschenRaeumtJedeServerTabelle liest die Server-Tabellen aus dem
// Schema, nicht aus einer Liste: Eine neue Tabelle mit server_id oder
// server_ref, die das Löschen vergisst, lässt diesen Test scheitern. Vorher
// blieben in rund einem Dutzend Tabellen Zeilen gelöschter Server liegen - und
// galten wieder, sobald SQLite die id neu vergab.
//
// Ein zweiter Server prüft die Gegenrichtung: Sein Bestand muss bleiben.
func TestServerLoeschenRaeumtJedeServerTabelle(t *testing.T) {
	db, _ := setupEncDB(t)
	repo := repositories.NewServerRepository(db)

	weg := &domain.Server{Name: "weg", Host: "10.0.0.1", ServiceUser: "lcm", HostKeyFingerprint: "f", PrivateKeyEnc: "k"}
	bleibt := &domain.Server{Name: "bleibt", Host: "10.0.0.2", ServiceUser: "lcm", HostKeyFingerprint: "f", PrivateKeyEnc: "k"}
	for _, s := range []*domain.Server{weg, bleibt} {
		if err := db.Create(s).Error; err != nil {
			t.Fatal(err)
		}
	}
	// Elternzeilen für die Zuordnungstabellen.
	if err := db.Create(&domain.ServerGroup{Name: "g"}).Error; err != nil {
		t.Fatal(err)
	}

	tabellen := serverTabellen(t, db)
	if len(tabellen) < 20 {
		t.Fatalf("nur %d Server-Tabellen gefunden (%v) - die Schema-Abfrage greift nicht", len(tabellen), tabellen)
	}
	for _, s := range []*domain.Server{weg, bleibt} {
		serverBestandAnlegen(t, db, tabellen, s.ID)
	}

	if err := repo.Delete(weg.ID); err != nil {
		t.Fatalf("Server löschen: %v", err)
	}
	for _, tabelle := range tabellen {
		if n := zeilenDesServers(t, db, tabelle, weg.ID); n != 0 {
			t.Errorf("%s: %d Zeile(n) des gelöschten Servers übrig", tabelle, n)
		}
		if n := zeilenDesServers(t, db, tabelle, bleibt.ID); n != 1 {
			t.Errorf("%s: vom anderen Server %d statt 1 Zeile übrig", tabelle, n)
		}
	}
}
