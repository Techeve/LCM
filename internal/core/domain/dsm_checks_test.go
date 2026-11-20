package domain

import (
	"strings"
	"testing"
)

func dsmServer(checks DSMChecks) Server {
	return Server{Reachable: true, OSID: OSIDSynologyDSM, OSName: "Synology DSM", DSMChecks: checks}
}

// TestDSMAbweichungenSindWarnungen: Jede Abweichung färbt gelb und nennt den
// Weg in der DSM-Oberfläche, wo der Admin sie behebt.
func TestDSMAbweichungenSindWarnungen(t *testing.T) {
	s := dsmServer(DSMChecks{
		{Key: DSMCheckFirewall, State: DSMCheckDeviation, Value: "aus"},
		{Key: DSMCheckAutoBlock, State: DSMCheckDeviation, Params: map[string]string{"attempts": "20", "minutes": "5"}},
		{Key: DSMCheckTLSProfile, State: DSMCheckDeviation, Value: "intermediate"},
		{Key: DSMCheckCSP, State: DSMCheckOK, Value: "an"},
	})
	status, insights := s.TrafficLight(TrafficLightInput{})
	if status != ServerStatusYellow {
		t.Fatalf("Abweichungen müssen gelb färben, got %s", status)
	}
	for _, key := range []string{"dsmFirewallOff", "dsmAutoBlockWeak", "dsmTlsProfile"} {
		got := findInsight(insights, key)
		if got == nil || got.Severity != "warning" || !strings.Contains(got.Message, "Systemsteuerung → Sicherheit") {
			t.Errorf("%s fehlt oder ohne Menüpfad: %+v", key, got)
		}
	}
	if got := findInsight(insights, "dsmAutoBlockWeak"); got.Params["attempts"] != "20" {
		t.Errorf("AutoBlock ohne Werte: %+v", got)
	}
	if findInsight(insights, "dsmCspOff") != nil {
		t.Error("erfüllter Sollwert darf keinen Befund ergeben")
	}
}

// TestDSMUnbekanntIstKeinAlarm: Nicht deutbare Werte erscheinen als ein
// Hinweis, färben die Ampel aber nicht.
func TestDSMUnbekanntIstKeinAlarm(t *testing.T) {
	s := dsmServer(DSMChecks{
		{Key: DSMCheckSpectre, State: DSMCheckUnknown},
		{Key: DSMCheckTLSProfile, State: DSMCheckUnknown},
	})
	status, insights := s.TrafficLight(TrafficLightInput{})
	if status != ServerStatusExcellent {
		t.Errorf("Unbekanntes darf die Ampel nicht färben, got %s", status)
	}
	got := findInsight(insights, "dsmChecksUnknown")
	if got == nil || got.Severity != "info" || got.Params["count"] != "2" {
		t.Errorf("Hinweis auf unbekannte Werte fehlt: %+v", insights)
	}
}

// TestDSMSpeicherBefundMitEigenemSchluessel: Ein DSM-Zustand trägt seinen
// genaueren Schlüssel in den Befund; ausgefallen heißt rot-kritisch.
func TestDSMSpeicherBefundMitEigenemSchluessel(t *testing.T) {
	s := dsmServer(nil)
	_, insights := s.TrafficLight(TrafficLightInput{StorageHealth: []StorageHealth{
		{Kind: StorageKindDSMDisk, Name: "Drive 2", State: StorageStateFaulted, Cause: "dsmDiskFailed", RawState: "crashed", Message: "Laufwerk ausgefallen"},
		{Kind: StorageKindDSMPool, Name: "reuse_1", State: StorageStateDegraded, Cause: "dsmPoolDegraded", RawState: "degraded"},
		{Kind: StorageKindMDRaid, Name: "md0", State: StorageStateDegraded, Message: "RAID ohne Redundanz"},
	}})
	disk := findInsight(insights, "dsmDiskFailed")
	if disk == nil || disk.Severity != "critical" || disk.Params["name"] != "Drive 2" || disk.Params["rawState"] != "crashed" {
		t.Errorf("Plattenausfall falsch: %+v", disk)
	}
	if !strings.HasPrefix(disk.Message, "Laufwerk Drive 2") {
		t.Errorf("Klartext ohne Art und Namen: %q", disk.Message)
	}
	if findInsight(insights, "dsmPoolDegraded") == nil || findInsight(insights, "storageDefect") == nil {
		t.Errorf("Pool- oder allgemeiner Befund fehlt: %+v", insights)
	}
}

// TestDSMChecksSpalte: Die Prüfungen gehen als JSON in die Textspalte und
// kommen unverändert zurück; leer heißt „nie geprüft".
func TestDSMChecksSpalte(t *testing.T) {
	in := DSMChecks{{Key: DSMCheckDoS, State: DSMCheckDeviation, Value: "aus"}}
	v, err := in.Value()
	if err != nil {
		t.Fatal(err)
	}
	var out DSMChecks
	if err := out.Scan(v); err != nil || len(out) != 1 || out[0].Key != in[0].Key || out[0].Value != in[0].Value || out[0].State != in[0].State {
		t.Errorf("Rundreise kaputt: %v %+v", err, out)
	}
	if err := out.Scan(""); err != nil || out != nil {
		t.Errorf("leerer Wert = nie geprüft, got %+v", out)
	}
	if v, _ := DSMChecks(nil).Value(); v != "" {
		t.Errorf("leere Liste = Leerstring, got %v", v)
	}
}

// TestFehlenderDoSSchutzIstNurEinHinweis: LCM weiß nicht, ob das Gerät aus dem
// Internet erreichbar ist - ein fehlender DoS-Schutz färbt die Ampel nicht.
func TestFehlenderDoSSchutzIstNurEinHinweis(t *testing.T) {
	befunde, hinweise := DSMChecks{{Key: DSMCheckDoS, State: DSMCheckDeviation}}.insights()
	if len(befunde) != 0 {
		t.Errorf("DoS als Befund gemeldet: %+v", befunde)
	}
	if len(hinweise) != 1 || hinweise[0].Severity != "info" || hinweise[0].Key != "dsmDosOff" {
		t.Errorf("erwartet ein Info-Hinweis dsmDosOff, bekam %+v", hinweise)
	}
}
