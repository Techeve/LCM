package services

import (
	"strings"
	"testing"
	"time"

	"LCM/internal/core/domain"
)

// TestRechteSollGreiftNurBeiAbweichung: Eine Grundsatz-Regel prüft zuerst und
// greift NUR bei Drift ein - sonst liefe sie alle 15 Minuten über jeden Pfad.
func TestRechteSollGreiftNurBeiAbweichung(t *testing.T) {
	script := hardeningDriftScript([]domain.HardenedPath{
		{Path: "/srv/kundendaten", Mode: "750", Group: "app"},
	})
	if !strings.Contains(script, "stat -c '%a' /srv/kundendaten") {
		t.Errorf("ist-zustand wird nicht geprüft: %s", script)
	}
	if !strings.Contains(script, "chmod o-rwx /srv/kundendaten") {
		t.Errorf("wiederherstellung fehlt: %s", script)
	}
	if !strings.Contains(script, "chgrp app /srv/kundendaten") {
		t.Errorf("gruppe wird nicht wiederhergestellt: %s", script)
	}
	if !strings.Contains(script, "LCM-DRIFT:") {
		t.Errorf("eingriff wird nicht gemeldet: %s", script)
	}
}

// TestACLDriftIstEineStichprobe: Nur der Wurzelpfad wird geprüft. Den ganzen
// Baum bei jedem Health-Ping durchzugehen wäre reine Last - der Voll-Abgleich
// läuft ohnehin beim Benutzer-Sync.
func TestACLDriftIstEineStichprobe(t *testing.T) {
	script := aclDriftScript(
		[]domain.AppliedProfilePath{{ProfileID: 5, Path: "/srv/www"}},
		map[uint]string{5: "web"},
	)
	if !strings.Contains(script, "getfacl -p /srv/www") {
		t.Errorf("acl wird nicht geprüft: %s", script)
	}
	if strings.Contains(script, "-R") {
		t.Errorf("die stichprobe darf nicht rekursiv sein: %s", script)
	}
	if !strings.Contains(script, "group:lcm-prof-web:") {
		t.Errorf("es wird nicht auf die profilgruppe geprüft: %s", script)
	}
	// Ohne bekanntes Profil (gelöscht) entsteht keine Zeile - sonst prüfte
	// LCM gegen einen Gruppennamen, den es nicht mehr gibt.
	if got := aclDriftScript([]domain.AppliedProfilePath{{ProfileID: 9, Path: "/srv/x"}}, nil); got != "" {
		t.Errorf("unbekanntes profil darf keine prüfung erzeugen: %s", got)
	}
}

// TestDriftMeldungenWerdenGesammelt.
func TestDriftMeldungenWerdenGesammelt(t *testing.T) {
	got := driftLines("egal\nLCM-DRIFT: /srv/www neu abgeschottet\nauch egal\nLCM-DRIFT: ACL fehlt auf /srv/daten\n")
	if len(got) != 2 || got[0] != "/srv/www neu abgeschottet" {
		t.Fatalf("meldungen falsch gesammelt: %+v", got)
	}
}

// TestACLEinrichtungLaeuftNichtInSchleife: Scheitert die Installation, darf
// sie nicht bei jedem Health-Ping erneut versucht und jedes Mal als Eingriff
// protokolliert werden.
func TestACLEinrichtungLaeuftNichtInSchleife(t *testing.T) {
	if wait := aclRetryWait(&domain.Server{}); wait != "" {
		t.Errorf("ohne fehlversuch darf nicht gesperrt sein: %s", wait)
	}
	future := time.Now().Add(time.Hour)
	if wait := aclRetryWait(&domain.Server{ACLRetryAfter: &future}); wait == "" {
		t.Error("innerhalb der sperrfrist muss gewartet werden")
	}
	past := time.Now().Add(-time.Hour)
	if wait := aclRetryWait(&domain.Server{ACLRetryAfter: &past}); wait != "" {
		t.Errorf("nach der sperrfrist darf es weitergehen: %s", wait)
	}
}

// TestNeueEnforceTypenWerdenEntdoppelt: Fordern mehrere Gruppen dasselbe
// „vorhanden/abgeglichen", ist das kein Konflikt, sondern eine Dopplung - die
// zweite Ausführung wäre wirkungslos.
func TestNeueEnforceTypenWerdenEntdoppelt(t *testing.T) {
	for _, typ := range []string{domain.RuleTypeACLSetup, domain.RuleTypePermSync} {
		if !enforceableRuleType(typ) {
			t.Errorf("%s muss als grundsatz-regel zulässig sein", typ)
		}
		if !dedupedEnforceType(typ) {
			t.Errorf("%s muss entdoppelt werden", typ)
		}
		if conflictingEnforceType(typ) {
			t.Errorf("%s trägt keinen widersprüchlichen soll-zustand", typ)
		}
	}
	active, superseded := resolveEnforceRules([]domain.Rule{
		ruleIn(1, 3, "A", 10, domain.RuleTypePermSync, "Soll A"),
		ruleIn(2, 7, "B", 100, domain.RuleTypePermSync, "Soll B"),
	})
	if len(active) != 1 || active[0].Name != "Soll A" {
		t.Fatalf("erwartet genau eine ausführung: %+v", active)
	}
	// Kein Konflikt - also auch keine „übersprungen"-Meldung, die nur Lärm wäre.
	if len(superseded) != 0 {
		t.Errorf("dopplung darf nicht als konflikt gemeldet werden: %+v", superseded)
	}
}

// TestDriftRechnetOktal hält den Fund der Distributions-Prüfung fest:
// `stat -c %a` liefert die Rechte OKTAL, und die Shell liest „0750" mit
// führender Null ebenfalls als Oktalzahl. Die Welt-Bits sind damit der Rest
// modulo 8. Mit modulo 10 galt ein korrekt gehärtetes 750 (dezimal 488,
// Rest 8) als Abweichung - die Regel hätte bei JEDEM Health-Ping
// „eingegriffen" und jedes Mal einen Audit-Eintrag erzeugt.
func TestDriftRechnetOktal(t *testing.T) {
	script := hardeningDriftScript([]domain.HardenedPath{{Path: "/srv/daten", Mode: "750"}})
	if !strings.Contains(script, "%% 8") && !strings.Contains(script, "% 8") {
		t.Errorf("die welt-bits müssen modulo 8 geprüft werden: %s", script)
	}
	if strings.Contains(script, "% 10") {
		t.Errorf("modulo 10 wertet oktale rechte falsch aus: %s", script)
	}
	// Gegenprobe der Rechnung selbst, damit die Absicht dokumentiert bleibt.
	for mode, wantDrift := range map[int]bool{0o750: false, 0o700: false, 0o755: true, 0o751: true} {
		if got := mode%8 != 0; got != wantDrift {
			t.Errorf("modus %o: drift %v erwartet, bekam %v", mode, wantDrift, got)
		}
	}
}
