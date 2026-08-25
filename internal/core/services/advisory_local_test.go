package services_test

import (
	"context"
	"strings"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/storage/repositories"
)

// setLocalCopy schaltet die Frühwarnung auf die lokale Kopie um.
func setLocalCopy(t *testing.T, env *testEnv, on bool) {
	t.Helper()
	if _, err := env.Settings.UpdateGlobal(services.GlobalSettingsInput{AdvisoryLocalCopy: &on}, "admin"); err != nil {
		t.Fatalf("Einstellungen: %v", err)
	}
}

// TestLokaleKopieOhneDatenGiltAlsAus ist der zentrale Schutz dieser
// Betriebsart: Ist die lokale Kopie eingestellt, aber noch nie gespiegelt
// worden, darf die Frühwarnung NICHT als eingeschaltet gelten. Sonst liefe
// sie durch, fände nichts und meldete ein sauberes Ergebnis für etwas, das
// nie geprüft wurde.
func TestLokaleKopieOhneDatenGiltAlsAus(t *testing.T) {
	env := newTestEnv(t)
	enableAdvisories(t, env, 10)
	if !env.Advisories.Enabled() {
		t.Fatal("Vorbedingung: online eingeschaltet")
	}

	setLocalCopy(t, env, true)
	if env.Advisories.Enabled() {
		t.Error("ohne gespiegelte Daten darf die Frühwarnung nicht als eingeschaltet gelten")
	}
	if !env.Advisories.UsesLocalCopy() {
		t.Error("die Betriebsart selbst ist gesetzt und sollte auch so gemeldet werden")
	}
}

// TestLokaleKopieFragtNichtOnline: Ist sie eingestellt, darf kein purl mehr
// an den fremden Dienst gehen - das ist der ganze Zweck der Betriebsart.
func TestLokaleKopieFragtNichtOnline(t *testing.T) {
	env := newTestEnv(t)
	enableAdvisories(t, env, 0)
	seedPackages(t, env, "web01", domain.Package{Name: "openssl", Version: "3.0.11"})
	setLocalCopy(t, env, true)

	out, err := env.Advisories.Poll(context.Background())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if env.AdvSource.QueryCalls != 0 {
		t.Errorf("im lokalen Betrieb darf nichts nach außen gehen, waren %d Abfragen", env.AdvSource.QueryCalls)
	}
	if !strings.Contains(out, "ausgeschaltet") {
		t.Errorf("ohne gespiegelte Daten sollte der Lauf das benennen: %q", out)
	}
}

// TestSpiegelLaufSammeltNurGebrauchteOekosysteme: Gespiegelt wird, was der
// Bestand hergibt - nicht die ganze Datenbank. Ohne Server gibt es also
// nichts zu tun, und das wird auch so gesagt.
func TestSpiegelLaufOhneBestand(t *testing.T) {
	env := newTestEnv(t)
	setLocalCopy(t, env, true)

	out, err := env.Advisories.RefreshLocalCopy(context.Background())
	if err != nil {
		t.Fatalf("RefreshLocalCopy: %v", err)
	}
	if !strings.Contains(out, "nichts zu spiegeln") {
		t.Errorf("leerer Bestand sollte benannt werden: %q", out)
	}
}

// TestSpiegelLaufErzeugtJob: Anders als Poll und Anreicherung ist dieser
// Lauf sichtbar - er dauert Minuten und lädt zig Megabyte, das gehört ins
// Protokoll.
func TestSpiegelLaufErzeugtJob(t *testing.T) {
	env := newTestEnv(t)
	setLocalCopy(t, env, true)

	env.Executor.RunAdvisoryMirror("scheduler")

	jobs, total, err := env.Jobs.HistoryFiltered(repositories.ScopeAll(),
		repositories.JobFilter{NameQuery: "lokale Kopie", Limit: 10})
	if err != nil || total != 1 {
		t.Fatalf("erwartet genau 1 Job, bekam %d (%v)", total, err)
	}
	if jobs[0].Status != "success" {
		t.Errorf("Job sollte success sein, war %q", jobs[0].Status)
	}
}

// TestSpiegelLaufNurImLokalenBetrieb: Wer online abfragt, braucht keinen
// Spiegel - der Lauf würde nur täglich zig Megabyte ohne Zweck laden.
func TestSpiegelLaufNurImLokalenBetrieb(t *testing.T) {
	env := newTestEnv(t)
	enableAdvisories(t, env, 10) // online

	env.Executor.RunAdvisoryMirror("scheduler")

	_, total, _ := env.Jobs.HistoryFiltered(repositories.ScopeAll(),
		repositories.JobFilter{NameQuery: "lokale Kopie", Limit: 10})
	if total != 0 {
		t.Errorf("ohne lokale Betriebsart darf nicht gespiegelt werden, fand %d Jobs", total)
	}
}
