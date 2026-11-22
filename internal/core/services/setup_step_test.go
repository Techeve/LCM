package services_test

import (
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
)

func setupStepOf(t *testing.T, svc *services.SettingsService) string {
	t.Helper()
	st, err := svc.Get()
	if err != nil {
		t.Fatal(err)
	}
	return st.SetupStep
}

func stepInput(value string) services.GlobalSettingsInput {
	return services.GlobalSettingsInput{SetupStep: &value}
}

func serviceAtStep(t *testing.T, step string) *services.SettingsService {
	t.Helper()
	svc, repo := newSettingsService(t)
	if err := repo.Save(&domain.GlobalSettings{ID: 1, SetupStep: step}); err != nil {
		t.Fatal(err)
	}
	return svc
}

// Der Assistent geht Schritt für Schritt vorwärts, auch mit Sprüngen, und am
// Ende auf leer (erledigt).
func TestSetupStepGehtVorwaerts(t *testing.T) {
	svc := serviceAtStep(t, domain.SetupStepSession)
	for _, step := range []string{
		domain.SetupStepAddresses, domain.SetupStepServices, domain.SetupStepAccess,
		domain.SetupStepMail, domain.SetupStepBackup, domain.SetupStepSubscription,
		domain.SetupStepIntro, "",
	} {
		mustUpdate(t, svc, stepInput(step))
		if got := setupStepOf(t, svc); got != step {
			t.Fatalf("Stand = %q, erwartet %q", got, step)
		}
	}
}

// Zwischen session und subscription geht es von jedem Schritt auf jeden anderen,
// auch zurück. Derselbe Schritt noch einmal ist ein Nichts.
func TestSetupStepGehtZurueck(t *testing.T) {
	steps := []string{
		domain.SetupStepSession, domain.SetupStepAddresses, domain.SetupStepServices,
		domain.SetupStepAccess, domain.SetupStepCertificate, domain.SetupStepOnboarding,
		domain.SetupStepMail, domain.SetupStepRetention, domain.SetupStepBackup,
		domain.SetupStepSubscription,
	}
	for _, from := range steps {
		for _, to := range steps {
			svc := serviceAtStep(t, from)
			mustUpdate(t, svc, stepInput(to))
			if got := setupStepOf(t, svc); got != to {
				t.Errorf("von %q auf %q: Stand = %q", from, to, got)
			}
		}
	}
}

// Aus intro führt nur der Weg auf leer, aus leer gar keiner.
func TestSetupStepKeinZurueckAusIntroUndErledigt(t *testing.T) {
	intro := serviceAtStep(t, domain.SetupStepIntro)
	for _, back := range []string{domain.SetupStepSession, domain.SetupStepAddresses, domain.SetupStepSubscription} {
		_, err := intro.UpdateGlobal(stepInput(back), "admin")
		wantSettingInvalid(t, err, "aus intro auf "+back)
	}
	mustUpdate(t, intro, stepInput(domain.SetupStepIntro))
	if got := setupStepOf(t, intro); got != domain.SetupStepIntro {
		t.Errorf("Stand = %q", got)
	}
	mustUpdate(t, intro, stepInput(""))
	if got := setupStepOf(t, intro); got != "" {
		t.Errorf("Stand = %q, erwartet leer", got)
	}

	done := serviceAtStep(t, "")
	for _, step := range []string{domain.SetupStepSession, domain.SetupStepAddresses, domain.SetupStepIntro} {
		_, err := done.UpdateGlobal(stepInput(step), "admin")
		wantSettingInvalid(t, err, "erledigter Assistent auf "+step)
	}
	mustUpdate(t, done, stepInput(""))
	if got := setupStepOf(t, done); got != "" {
		t.Errorf("Stand = %q, erwartet leer", got)
	}
}

// Aus jedem Schritt des Assistenten geht es auch direkt auf intro oder leer.
func TestSetupStepSpringtAufIntroUndErledigt(t *testing.T) {
	for _, from := range []string{domain.SetupStepSession, domain.SetupStepMail, domain.SetupStepSubscription} {
		for _, to := range []string{domain.SetupStepIntro, ""} {
			svc := serviceAtStep(t, from)
			mustUpdate(t, svc, stepInput(to))
			if got := setupStepOf(t, svc); got != to {
				t.Errorf("von %q auf %q: Stand = %q", from, to, got)
			}
		}
	}
}

func TestSetupStepUnbekannterWert(t *testing.T) {
	svc := serviceAtStep(t, domain.SetupStepAddresses)
	for _, bad := range []string{"open", "skipped", "done", "alles"} {
		_, err := svc.UpdateGlobal(stepInput(bad), "admin")
		wantSettingInvalid(t, err, "setup_step="+bad)
	}
}

// Andere Einstellungen lassen den Schritt stehen, auch die Adressen: Der
// Assistent rückt nur auf Anweisung vor.
func TestSetupStepBleibtBeiAnderenEinstellungen(t *testing.T) {
	svc := serviceAtStep(t, domain.SetupStepAddresses)
	port, user := 9312, "ops"
	mustUpdate(t, svc, services.GlobalSettingsInput{AgentPort: &port, DefaultSSHUser: &user})
	if got := setupStepOf(t, svc); got != domain.SetupStepAddresses {
		t.Errorf("Stand = %q, erwartet addresses", got)
	}
}
