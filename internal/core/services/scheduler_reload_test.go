package services_test

import (
	"testing"
	"time"

	"LCM/internal/core/services"
)

// naechsteLaeufe liefert den nächsten Lauf je System-Schedule der Übersicht.
func naechsteLaeufe(t *testing.T, env *testEnv) map[string]string {
	t.Helper()
	overview, err := env.Scheduler.Overview()
	if err != nil {
		t.Fatalf("übersicht: %v", err)
	}
	out := map[string]string{}
	for _, ov := range overview {
		if ov.Kind != services.KindSchedule {
			out[ov.Kind] = ov.NextRun
		}
	}
	return out
}

// abseitsEinesLaufs wartet, falls gerade ein Takt fällig wird: Läge ein Lauf
// in den nächsten Sekunden, verschöbe er den nächsten Termin während des Tests
// zu Recht - das wäre kein Fehler des Reloads.
func abseitsEinesLaufs(t *testing.T, laeufe map[string]string) {
	t.Helper()
	for _, next := range laeufe {
		at, err := time.Parse(time.RFC3339, next)
		if err == nil && time.Until(at) < 5*time.Second {
			time.Sleep(time.Until(at) + time.Second)
			return
		}
	}
}

// TestReloadVerschiebtSystemTakteNicht: Jedes Speichern einer Regel lädt den
// Scheduler neu. Bisher begann die Update-Prüfung („@every 3h") damit jedes Mal
// von vorn; die Übersicht zeigte „in drei Stunden" und hatte recht. Jetzt
// hängen die Takte an der Uhr: Nach beliebig vielen Reloads bleibt der nächste
// Lauf derselbe.
func TestReloadVerschiebtSystemTakteNicht(t *testing.T) {
	env := newTestEnv(t)
	env.Scheduler.WithUpdateCheck(func() {})
	if err := env.Scheduler.Start(); err != nil {
		t.Fatalf("scheduler starten: %v", err)
	}
	t.Cleanup(env.Scheduler.Stop)

	abseitsEinesLaufs(t, naechsteLaeufe(t, env))
	vorher := naechsteLaeufe(t, env)
	// Über eine Sekundengrenze hinweg: @every rechnet sekundengenau ab der Anlage.
	time.Sleep(1100 * time.Millisecond)
	for i := 0; i < 5; i++ {
		if err := env.Scheduler.Reload(); err != nil {
			t.Fatalf("reload: %v", err)
		}
	}
	nachher := naechsteLaeufe(t, env)

	for _, kind := range []string{services.KindUpdate, services.KindCVEDB} {
		if vorher[kind] == "" {
			t.Fatalf("%s fehlt in der Übersicht", kind)
		}
		if nachher[kind] != vorher[kind] {
			t.Errorf("%s: nächster Lauf vor den Reloads %s, danach %s", kind, vorher[kind], nachher[kind])
		}
	}
}

// TestUebersichtZeigtDenEchtenNaechstenLauf: Ein Backup mit krummem Intervall
// (36 h) bleibt bei „@every". Die Übersicht rechnete seinen nächsten Lauf aus
// dem Ausdruck - also „jetzt plus 36 Stunden", bei jedem Aufruf neu. Der echte
// Termin steht im Cron-Eintrag und rückt zwischen zwei Aufrufen nicht weiter.
func TestUebersichtZeigtDenEchtenNaechstenLauf(t *testing.T) {
	env := newTestEnv(t)
	settings, err := env.Settings.Get()
	if err != nil {
		t.Fatalf("einstellungen: %v", err)
	}
	settings.BackupEnabled = true
	settings.BackupIntervalHours = 36
	if err := env.DB().Save(settings).Error; err != nil {
		t.Fatalf("einstellungen speichern: %v", err)
	}
	if err := env.Scheduler.Start(); err != nil {
		t.Fatalf("scheduler starten: %v", err)
	}
	t.Cleanup(env.Scheduler.Stop)

	erster := naechsteLaeufe(t, env)[services.KindBackup]
	time.Sleep(1100 * time.Millisecond)
	zweiter := naechsteLaeufe(t, env)[services.KindBackup]
	if erster == "" || zweiter != erster {
		t.Errorf("nächstes Backup erst %q, eine Sekunde später %q - erwartet derselbe Termin", erster, zweiter)
	}
}
