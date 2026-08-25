package controllers

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"LCM/internal/core/services"
)

// Der PATCH-Rumpf der globalen Einstellungen und die Service-Eingabe sind zwei
// getrennte Strukturen - und genau da ging etwas verloren: ntp_server_presets
// und default_timezone fehlten im Rumpf, die Seite „Zeit & NTP" antwortete mit
// 200 und speicherte nichts. Ein Feld zu vergessen ist beim Erweitern der
// Einstellungen die wahrscheinlichste Änderung, also prüfen die Tests hier den
// gesamten Weg (JSON → Request → Service-Eingabe) und nicht nur die zwei Felder.

// TestGlobalSettingsRequestForwardsEveryField setzt JEDES Feld des Requests und
// prüft, dass keines auf dem Weg in die Service-Eingabe verlorengeht.
func TestGlobalSettingsRequestForwardsEveryField(t *testing.T) {
	var req globalSettingsRequest
	fillPointers(reflect.ValueOf(&req).Elem())

	in := req.toInput()
	v := reflect.ValueOf(in)
	for i := 0; i < v.NumField(); i++ {
		name := v.Type().Field(i).Name
		if v.Field(i).IsNil() {
			t.Errorf("GlobalSettingsInput.%s wird vom Request nicht befüllt - "+
				"Feld im PATCH-Rumpf oder in toInput() vergessen?", name)
		}
	}
}

// TestGlobalSettingsRequestCoversServiceInput hält Rumpf und Service-Eingabe
// namentlich deckungsgleich. Schlägt fehl, sobald die Eingabe ein Feld bekommt,
// das über die API gar nicht erreichbar wäre.
func TestGlobalSettingsRequestCoversServiceInput(t *testing.T) {
	reqFields := map[string]bool{}
	rt := reflect.TypeOf(globalSettingsRequest{})
	for i := 0; i < rt.NumField(); i++ {
		reqFields[rt.Field(i).Name] = true
	}
	it := reflect.TypeOf(services.GlobalSettingsInput{})
	for i := 0; i < it.NumField(); i++ {
		if name := it.Field(i).Name; !reqFields[name] {
			t.Errorf("GlobalSettingsInput.%s hat kein Gegenstück in globalSettingsRequest", name)
		}
	}
}

// TestGlobalSettingsRequestJSONTags prüft die Schreibweise der JSON-Namen: Das
// Frontend schickt snake_case, ein abweichender Tag liefe still ins Leere.
func TestGlobalSettingsRequestJSONTags(t *testing.T) {
	rt := reflect.TypeOf(globalSettingsRequest{})
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		tag, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if tag == "" || tag == "-" {
			t.Errorf("%s hat keinen json-Tag", f.Name)
			continue
		}
		if tag != strings.ToLower(tag) || strings.Contains(tag, " ") {
			t.Errorf("%s: json-Tag %q ist kein snake_case", f.Name, tag)
		}
	}
}

// TestGlobalSettingsTimeFieldsBind bindet den Rumpf der Zeit-Seite so, wie das
// Frontend ihn schickt - der konkrete Fall aus dem Fehlerbericht.
func TestGlobalSettingsTimeFieldsBind(t *testing.T) {
	body := `{"ntp_server_presets":"NTP-Pool = 0.pool.ntp.org","default_timezone":"Europe/Berlin"}`
	var req globalSettingsRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	in := req.toInput()
	if in.NTPServerPresets == nil || *in.NTPServerPresets != "NTP-Pool = 0.pool.ntp.org" {
		t.Errorf("ntp_server_presets kommt nicht im Service an: %v", in.NTPServerPresets)
	}
	if in.DefaultTimezone == nil || *in.DefaultTimezone != "Europe/Berlin" {
		t.Errorf("default_timezone kommt nicht im Service an: %v", in.DefaultTimezone)
	}
	// Nicht mitgeschickte Felder bleiben nil (PATCH-Semantik, R2-029).
	if in.BackupEnabled != nil || in.AptCacheURL != nil {
		t.Error("nicht mitgeschickte Felder dürfen nicht gesetzt werden")
	}
}

// fillPointers belegt jedes Zeiger-Feld einer Struktur mit einem Nullwert.
func fillPointers(v reflect.Value) {
	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		if f.Kind() == reflect.Ptr && f.CanSet() {
			f.Set(reflect.New(f.Type().Elem()))
		}
	}
}
