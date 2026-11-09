package domain

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCapabilityCategoryFor(t *testing.T) {
	tests := map[string]string{
		RuleTypeUpdate:                CapabilityPackages,
		RuleTypePackages:              CapabilityPackages,
		RuleTypeSecurity:              CapabilityPackages,
		RuleTypePackageScan:           CapabilityPackages,
		RuleTypeAutoremove:            CapabilityPackages,
		RuleTypeCVEScan:               CapabilityCVEScan,
		RuleTypeHealth:                CapabilityConnection,
		RuleTypeScript:                CapabilityRules,
		RuleTypeFirewall:              CapabilityRules,
		RuleTypeRebootIfNeeded:        CapabilityRules,
		RuleTypeDockerContainerUpdate: CapabilityRules,
		RuleTypeDeepScan:              CapabilityRules,
		// System-Jobs und Reparaturen berühren keine Kategorie.
		RuleTypeBackup:      "",
		RuleTypeCleanup:     "",
		RuleTypeAlertCheck:  "",
		RuleTypeDockerCheck: "",
		"join":              "",
		"harden-ssh":        "",
		"":                  "",
	}
	for jobType, want := range tests {
		if got := CapabilityCategoryFor(jobType); got != want {
			t.Errorf("CapabilityCategoryFor(%q) = %q, erwartet %q", jobType, got, want)
		}
	}
}

func TestHeals(t *testing.T) {
	tests := []struct {
		name     string
		category string
		failed   string // LastJobType
		success  string
		want     bool
	}{
		{"gleicher Typ heilt", CapabilityPackages, RuleTypeAutoremove, RuleTypeAutoremove, true},
		{"anderer Typ heilt nicht", CapabilityPackages, RuleTypeAutoremove, RuleTypeSecurity, false},
		{"Paketlisten-Fehler durch jeden Paket-Erfolg erledigt", CapabilityPackages, RuleTypePackageScan, RuleTypeSecurity, true},
		{"Paketlisten-Fehler durch Update erledigt", CapabilityPackages, RuleTypePackageScan, RuleTypeUpdate, true},
		{"Update heilt security", CapabilityPackages, RuleTypeSecurity, RuleTypeUpdate, true},
		{"Update heilt packages", CapabilityPackages, RuleTypePackages, RuleTypeUpdate, true},
		{"security heilt kein Update", CapabilityPackages, RuleTypeUpdate, RuleTypeSecurity, false},
		{"Update heilt kein Autoremove", CapabilityPackages, RuleTypeAutoremove, RuleTypeUpdate, false},
		{"Health heilt Pakete nicht", CapabilityPackages, RuleTypeUpdate, RuleTypeHealth, false},
		{"Health heilt Paketlisten-Fehler nicht", CapabilityPackages, RuleTypePackageScan, RuleTypeHealth, false},
		{"System-Job heilt nichts", CapabilityPackages, RuleTypeUpdate, RuleTypeBackup, false},
		{"Regel nur durch denselben Typ", CapabilityRules, RuleTypeFirewall, RuleTypeScript, false},
		{"Regel durch denselben Typ", CapabilityRules, RuleTypeFirewall, RuleTypeFirewall, true},
		{"Update heilt Regeln nicht", CapabilityRules, RuleTypeScript, RuleTypeUpdate, false},
		{"CVE-Scan durch CVE-Scan", CapabilityCVEScan, RuleTypeCVEScan, RuleTypeCVEScan, true},
		{"Verbindung: jeder Erfolg heilt", CapabilityConnection, RuleTypeHealth, RuleTypeUpdate, true},
		{"unbekannter Fehlertyp: Kategorie genügt", CapabilityPackages, "", RuleTypeSecurity, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &ServerCapability{Category: tt.category, State: CapabilityFailed, LastJobType: tt.failed}
			if got := c.Heals(&Job{Type: tt.success}); got != tt.want {
				t.Errorf("Heals(%q) = %v, erwartet %v", tt.success, got, tt.want)
			}
		})
	}
}

// TestHealsRegelnNurDurchDieselbeRegel: Zwei Regeln desselben Typs tun
// Verschiedenes - der Erfolg der einen sagt nichts über die andere.
func TestHealsRegelnNurDurchDieselbeRegel(t *testing.T) {
	id := func(n uint) *uint { return &n }
	failed := func(category, jobType string, ruleID *uint, name string) *ServerCapability {
		return &ServerCapability{
			Category: category, State: CapabilityFailed,
			LastJobType: jobType, LastRuleID: ruleID, LastJobName: name,
		}
	}
	tests := []struct {
		name    string
		c       *ServerCapability
		success Job
		want    bool
	}{
		{"andere Regel gleichen Typs", failed(CapabilityRules, RuleTypeScript, id(1), "A"),
			Job{Type: RuleTypeScript, RuleID: id(2), Name: "B"}, false},
		{"dieselbe Regel", failed(CapabilityRules, RuleTypeScript, id(1), "A"),
			Job{Type: RuleTypeScript, RuleID: id(1), Name: "A"}, true},
		{"dieselbe Regel nach Umbenennung", failed(CapabilityRules, RuleTypeScript, id(1), "A"),
			Job{Type: RuleTypeScript, RuleID: id(1), Name: "A neu"}, true},
		{"Ad-hoc, anderer Name", failed(CapabilityRules, RuleTypeScript, nil, "CrowdSec einrichten"),
			Job{Type: RuleTypeScript, Name: "Logs rotieren"}, false},
		{"Ad-hoc, gleicher Name", failed(CapabilityRules, RuleTypeScript, nil, "CrowdSec einrichten"),
			Job{Type: RuleTypeScript, Name: "CrowdSec einrichten"}, true},
		{"Ad-hoc fehlgeschlagen, Regel gelingt", failed(CapabilityRules, RuleTypeScript, nil, "A"),
			Job{Type: RuleTypeScript, RuleID: id(2), Name: "A"}, false},
		{"Regel fehlgeschlagen, Ad-hoc gelingt", failed(CapabilityRules, RuleTypeScript, id(1), "A"),
			Job{Type: RuleTypeScript, Name: "A"}, false},
		{"gleiche Regel, anderer Typ", failed(CapabilityRules, RuleTypeScript, id(1), "A"),
			Job{Type: RuleTypeFirewall, RuleID: id(1), Name: "A"}, false},
		// Pakete behalten die Typ-Regeln: Ein Zeitplan-Update heilt das manuelle.
		{"Pakete: andere Regel gleichen Typs", failed(CapabilityPackages, RuleTypeUpdate, id(1), "A"),
			Job{Type: RuleTypeUpdate, RuleID: id(2), Name: "B"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.c.Heals(&tt.success); got != tt.want {
				t.Errorf("Heals = %v, erwartet %v", got, tt.want)
			}
		})
	}
}

func TestEffective(t *testing.T) {
	tests := []struct {
		name string
		c    ServerCapability
		want string
	}{
		{"ok", ServerCapability{Category: CapabilityPackages, State: CapabilityOK, ConsecutiveFailures: 5}, ""},
		{"Paketfehler rot", ServerCapability{Category: CapabilityPackages, State: CapabilityFailed, LastErrorClass: ErrClassBrokenDeps, ConsecutiveFailures: 1}, CapabilityCritical},
		{"Sperre beim ersten Mal gelb", ServerCapability{Category: CapabilityPackages, State: CapabilityFailed, LastErrorClass: ErrClassLockBusy, ConsecutiveFailures: 1}, CapabilityWarning},
		{"Sperre beim zweiten Mal rot", ServerCapability{Category: CapabilityPackages, State: CapabilityFailed, LastErrorClass: ErrClassLockBusy, ConsecutiveFailures: 2}, CapabilityCritical},
		{"Verbindung rot", ServerCapability{Category: CapabilityConnection, State: CapabilityFailed, LastErrorClass: ErrClassNetwork, ConsecutiveFailures: 1}, CapabilityCritical},
		{"Regelfehler gelb, auch wiederholt", ServerCapability{Category: CapabilityRules, State: CapabilityFailed, ConsecutiveFailures: 9}, CapabilityWarning},
		{"CVE-Scan gelb", ServerCapability{Category: CapabilityCVEScan, State: CapabilityFailed, ConsecutiveFailures: 1}, CapabilityWarning},
		{"Sperre in Regeln bleibt gelb", ServerCapability{Category: CapabilityRules, State: CapabilityFailed, LastErrorClass: ErrClassLockBusy, ConsecutiveFailures: 3}, CapabilityWarning},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.c.Effective(); got != tt.want {
				t.Errorf("Effective() = %q, erwartet %q", got, tt.want)
			}
		})
	}
}

// TestBeforeSaveVerdichtetUndKuerzt: Die Hooks sind die einzige Stelle, die
// Parameter und gekürzte Texte garantiert - ohne sie liefe jede Aufrufstelle
// Gefahr, 40 KB Ausgabe in die Datenbank zu schreiben.
func TestBeforeSaveVerdichtetUndKuerzt(t *testing.T) {
	c := &ServerCapability{
		LastErrorMessage: strings.Repeat("ä", 900),
		WarningMessage:   strings.Repeat("x", 501),
		LastErrorParams:  map[string]string{"pid": "1234", "process": "unattended-upgr"},
	}
	if err := c.BeforeSave(nil); err != nil {
		t.Fatal(err)
	}
	if n := utf8.RuneCountInString(c.LastErrorMessage); n != 500 || !utf8.ValidString(c.LastErrorMessage) {
		t.Errorf("LastErrorMessage: %d Zeichen, gültig=%v", n, utf8.ValidString(c.LastErrorMessage))
	}
	if n := utf8.RuneCountInString(c.WarningMessage); n != 500 {
		t.Errorf("WarningMessage: %d Zeichen", n)
	}

	// Rundreise über den Ablagewert.
	back := &ServerCapability{LastErrorParamsEnc: c.LastErrorParamsEnc}
	if err := back.AfterFind(nil); err != nil {
		t.Fatal(err)
	}
	assertParams(t, back.LastErrorParams, c.LastErrorParams)

	// Ohne Parameter bleibt der Ablagewert leer; ein beschädigter Wert wird
	// zu „keine Parameter" statt zu einem Ladefehler.
	empty := &ServerCapability{LastErrorParamsEnc: "stale"}
	_ = empty.BeforeSave(nil)
	if empty.LastErrorParamsEnc != "" {
		t.Errorf("Ablagewert = %q, erwartet leer", empty.LastErrorParamsEnc)
	}
	broken := &ServerCapability{LastErrorParamsEnc: "{kaputt"}
	if err := broken.AfterFind(nil); err != nil || broken.LastErrorParams != nil {
		t.Errorf("beschädigter Wert: err=%v params=%v", err, broken.LastErrorParams)
	}
}
