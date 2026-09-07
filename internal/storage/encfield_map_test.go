package storage

import (
	"sort"
	"testing"

	"LCM/internal/core/domain"
)

// TestMapUpdateVerschluesseltEbenso: Der Weg über Updates(map) muss dieselbe
// Ablage erzeugen wie der über die Struktur.
//
// Vorher tat er das nicht: GORM übernimmt Map-Werte unverändert in die
// Anweisung und befragt den Serializer nicht. Der Scan schrieb seine Felder
// genau so - OS, Kernel, CPU, Board und die vollständige Portliste lagen damit
// im Klartext in der Datenbank. Auffallen konnte es nicht, weil das Lesen
// nicht entschlüsselbare Werte bewusst unverändert durchreicht.
func TestMapUpdateVerschluesseltEbenso(t *testing.T) {
	db, _ := setupEncDB(t)
	s := &domain.Server{Name: "web01", Host: "10.0.0.1"}
	if err := db.Create(s).Error; err != nil {
		t.Fatal(err)
	}

	klartext := map[string]string{
		"os_name":            "Ubuntu",
		"cpu_model":          "AMD Ryzen 7 3700X",
		"hardware_model":     "Micro-Star International Co., Ltd MS-7B86",
		"listening_packages": "lcm, openssh-server",
	}
	felder := map[string]any{}
	for spalte, wert := range klartext {
		felder[spalte] = wert
	}
	if err := db.Model(&domain.Server{}).Where("id = ?", s.ID).Updates(felder).Error; err != nil {
		t.Fatal(err)
	}

	for spalte, wert := range klartext {
		if roh := rawCol(t, db, "servers", spalte, s.ID); roh == wert {
			t.Errorf("servers.%s steht im Klartext in der Spalte: %q", spalte, roh)
		}
	}

	// Und der Wert muss unverändert zurückkommen - sonst hätten wir die
	// Vertraulichkeit gegen die Lesbarkeit eingetauscht.
	var zurueck domain.Server
	if err := db.First(&zurueck, s.ID).Error; err != nil {
		t.Fatal(err)
	}
	if zurueck.OSName != "Ubuntu" || zurueck.HardwareModel != klartext["hardware_model"] {
		t.Errorf("Rücklesen verändert: os_name=%q hardware_model=%q", zurueck.OSName, zurueck.HardwareModel)
	}
}

// TestMapUpdateLaesstAufruferMapUnberuehrt: Der Callback arbeitet auf einer
// Kopie. Täte er es nicht, stünde beim Aufrufer nach dem Schreiben Geheimtext
// in seiner eigenen Map - und ein zweites Update mit derselben Map
// verschlüsselte ihn ein weiteres Mal.
func TestMapUpdateLaesstAufruferMapUnberuehrt(t *testing.T) {
	db, _ := setupEncDB(t)
	s := &domain.Server{Name: "web02", Host: "10.0.0.2"}
	if err := db.Create(s).Error; err != nil {
		t.Fatal(err)
	}

	felder := map[string]any{"os_name": "Debian GNU/Linux", "cpu_cores": 4}
	if err := db.Model(&domain.Server{}).Where("id = ?", s.ID).Updates(felder).Error; err != nil {
		t.Fatal(err)
	}
	if felder["os_name"] != "Debian GNU/Linux" {
		t.Fatalf("die Map des Aufrufers wurde verändert: %v", felder["os_name"])
	}

	// Dieselbe Map ein zweites Mal: Es darf keine doppelte Verschlüsselung
	// entstehen.
	if err := db.Model(&domain.Server{}).Where("id = ?", s.ID).Updates(felder).Error; err != nil {
		t.Fatal(err)
	}
	var zurueck domain.Server
	if err := db.First(&zurueck, s.ID).Error; err != nil {
		t.Fatal(err)
	}
	if zurueck.OSName != "Debian GNU/Linux" {
		t.Errorf("nach zweitem Schreiben mit derselben Map: os_name=%q", zurueck.OSName)
	}
}

// TestNichtVerschluesselteSpaltenBleibenLesbar: Der Callback darf nur Felder
// mit Serializer anfassen. Zahlen, Flags und Zeitstempel müssen weiterhin als
// solche in der Spalte stehen - sonst bräche jede SQL-Filterung darauf.
func TestNichtVerschluesselteSpaltenBleibenLesbar(t *testing.T) {
	db, _ := setupEncDB(t)
	s := &domain.Server{Name: "web03", Host: "10.0.0.3"}
	if err := db.Create(s).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&domain.Server{}).Where("id = ?", s.ID).
		Updates(map[string]any{"cpu_cores": 8, "reboot_required": true, "timezone": "Europe/Berlin"}).Error; err != nil {
		t.Fatal(err)
	}
	var treffer int64
	if err := db.Model(&domain.Server{}).
		Where("cpu_cores = ? AND reboot_required = ? AND timezone = ?", 8, true, "Europe/Berlin").
		Count(&treffer).Error; err != nil {
		t.Fatal(err)
	}
	if treffer != 1 {
		t.Errorf("SQL-Filterung auf unverschlüsselten Spalten findet %d statt 1", treffer)
	}
}

// TestJedesAesgcmFeldIstRegistriert hält die Aufstellungen an den
// Struktur-Tags fest.
//
// Es gab drei von Hand gepflegte Listen verschlüsselter Server-Spalten, und
// jede war unvollständig - hardware_model stand in keiner. Eine fehlende
// Registrierung fällt im Betrieb nicht auf: Beim Schlüsselwechsel bleibt das
// Feld mit dem ALTEN Schlüssel liegen, und weil das Lesen nicht
// entschlüsselbare Werte durchreicht, erscheint danach der Geheimtext als
// Wert. Deshalb muss die Prüfung hier stattfinden und nicht im Betrieb.
func TestJedesAesgcmFeldIstRegistriert(t *testing.T) {
	db, _ := setupEncDB(t)

	registriert := map[string]bool{}
	for _, c := range serializerColumns {
		registriert[c.table+"."+c.column] = true
	}
	for _, c := range blindIndexedColumns {
		registriert[c.table+"."+c.column] = true
	}

	// Über ALLE migrierten Modelle, nicht über eine Handauswahl: Sonst wäre
	// die Prüfung selbst wieder eine gepflegte Liste, die zurückbleibt.
	for _, modell := range migratedModels {
		tabelle, err := tabellenName(db, modell)
		if err != nil {
			t.Fatal(err)
		}
		spalten, err := aesgcmColumns(db, modell)
		if err != nil {
			t.Fatal(err)
		}
		for _, spalte := range spalten {
			if !registriert[tabelle+"."+spalte] {
				t.Errorf("%s.%s ist verschlüsselt, steht aber weder in serializerColumns "+
					"noch in blindIndexedColumns - die Schlüsselrotation ließe es zurück",
					tabelle, spalte)
			}
		}
	}
}

// TestNachverschluesselungDecktAlleServerSpalten: Die beiden Durchgänge des
// Nachverschlüsselns müssen zusammen jede verschlüsselte Server-Spalte
// erfassen. Der Schnitt muss leer sein, sonst liefe eine Spalte doppelt.
func TestNachverschluesselungDecktAlleServerSpalten(t *testing.T) {
	db, _ := setupEncDB(t)

	frueh, err := serverEncColumns(db, false)
	if err != nil {
		t.Fatal(err)
	}
	spaet, err := serverEncColumns(db, true)
	if err != nil {
		t.Fatal(err)
	}

	gesehen := map[string]int{}
	for _, s := range append(append([]string{}, frueh...), spaet...) {
		gesehen[s]++
	}
	for spalte, n := range gesehen {
		if n > 1 {
			t.Errorf("%s läuft in beiden Durchgängen", spalte)
		}
	}

	alle, err := aesgcmColumns(db, &domain.Server{})
	if err != nil {
		t.Fatal(err)
	}
	var fehlend []string
	for _, spalte := range alle {
		// name trägt einen Blindindex und läuft über encryptServerNames.
		if spalte != "name" && gesehen[spalte] == 0 {
			fehlend = append(fehlend, spalte)
		}
	}
	sort.Strings(fehlend)
	if len(fehlend) > 0 {
		t.Errorf("kein Durchgang verschlüsselt diese Spalten nach: %v", fehlend)
	}
}

// TestNachverschluesselungHeiltKlartext: die Gegenprobe am Altbestand - eine
// Zeile, die noch aus der Zeit vor dem Fix stammt.
func TestNachverschluesselungHeiltKlartext(t *testing.T) {
	db, _ := setupEncDB(t)
	s := &domain.Server{Name: "alt01", Host: "10.0.0.9"}
	if err := db.Create(s).Error; err != nil {
		t.Fatal(err)
	}
	// Am Serializer vorbei schreiben - so lag der Altbestand da.
	if err := db.Exec("UPDATE servers SET hardware_model = ?, firewall_tool = ? WHERE id = ?",
		"Raspberry Pi 4 Model B", "ufw", s.ID).Error; err != nil {
		t.Fatal(err)
	}

	if err := migrateEncryptServerFirewall(db); err != nil {
		t.Fatal(err)
	}
	for _, f := range []struct{ spalte, wert string }{
		{"hardware_model", "Raspberry Pi 4 Model B"},
		{"firewall_tool", "ufw"},
	} {
		if roh := rawCol(t, db, "servers", f.spalte, s.ID); roh == f.wert {
			t.Errorf("servers.%s wurde nicht nachverschlüsselt: %q", f.spalte, roh)
		}
	}
	var zurueck domain.Server
	if err := db.First(&zurueck, s.ID).Error; err != nil {
		t.Fatal(err)
	}
	if zurueck.HardwareModel != "Raspberry Pi 4 Model B" || zurueck.FirewallTool != "ufw" {
		t.Errorf("nach dem Heilen nicht mehr lesbar: %q / %q", zurueck.HardwareModel, zurueck.FirewallTool)
	}
}
