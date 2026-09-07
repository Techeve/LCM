package services_test

import (
	"errors"
	"testing"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/storage/repositories"
)

// warteschlangeStart reiht einen Zeitplan-Lauf ein und liefert einen Kanal mit
// dem Ergebnis. StartOrQueue blockiert - der Test muss weiterlaufen können.
func warteschlangeStart(env *testEnv, serverID uint, ruleID uint, name string, prio int, maxWait time.Duration) <-chan error {
	done := make(chan error, 1)
	go func() {
		_, err := env.Jobs.StartOrQueue(services.QueuedStart{
			ServerID: serverID, RuleID: &ruleID, Type: domain.RuleTypeHealth,
			Name: name, TriggeredBy: "scheduler", Priority: prio, MaxWait: maxWait,
		})
		done <- err
	}()
	return done
}

// warteAufStatus wartet, bis ein Server die erwartete Zahl wartender Jobs hat.
func warteAufStatus(t *testing.T, env *testEnv, serverID uint, wanted int) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if len(env.Jobs.QueuedForServer(serverID)) == wanted {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("erwartete %d wartende Jobs, waren %d", wanted, len(env.Jobs.QueuedForServer(serverID)))
}

// laufendenJobStarten belegt den Server mit einem laufenden Job.
func laufendenJobStarten(t *testing.T, env *testEnv, serverID uint) *domain.Job {
	t.Helper()
	job, err := env.Jobs.Start(&serverID, nil, domain.RuleTypeHealth, "läuft gerade", "admin")
	if err != nil {
		t.Fatalf("Job starten: %v", err)
	}
	return job
}

// TestWartenderLaufWirdNachgeholt: der Kern von B. Bisher wurde ein
// Zeitplan-Lauf, der auf einen belegten Server traf, als „blocked" vermerkt
// und weggeworfen - der nächtliche Sync fiel damit für genau die Server aus,
// auf denen zufällig noch etwas anderes lief.
func TestWartenderLaufWirdNachgeholt(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	laufend := laufendenJobStarten(t, env, id)

	done := warteschlangeStart(env, id, 7, "Sync @ web01", domain.DefaultGroupPriority, 5*time.Second)
	warteAufStatus(t, env, id, 1)

	// Er steht sichtbar in der Liste - als wartend, nicht als abgewiesen.
	wartend := env.Jobs.QueuedForServer(id)[0]
	if wartend.Status != domain.JobStatusPending {
		t.Errorf("wartender Job hat Status %q, erwartet %q", wartend.Status, domain.JobStatusPending)
	}

	// Der laufende endet - der Wartende kommt dran.
	env.Jobs.Complete(laufend, "fertig", ptrIntTest(0), nil)
	if err := <-done; err != nil {
		t.Fatalf("der Wartende hätte laufen müssen: %v", err)
	}
	if n := len(env.Jobs.QueuedForServer(id)); n != 0 {
		t.Errorf("Warteschlange nicht leer: %d", n)
	}
}

// TestVorrangEntscheidetDieReihenfolge: Wofür die Gruppen-Prioritäten da sind.
// Steht ein dringender Lauf hinter einem beiläufigen, muss er trotzdem zuerst
// drankommen - auch wenn er später ausgelöst wurde.
func TestVorrangEntscheidetDieReihenfolge(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	laufend := laufendenJobStarten(t, env, id)

	// Zuerst der schwache Vorrang (System-Gruppe), dann der starke.
	schwach := warteschlangeStart(env, id, 1, "Health @ web01", domain.SystemGroupPriority, 5*time.Second)
	warteAufStatus(t, env, id, 1)
	stark := warteschlangeStart(env, id, 2, "Security @ web01", 10, 5*time.Second)
	warteAufStatus(t, env, id, 2)

	if erster := env.Jobs.QueuedForServer(id)[0]; erster.Name != "Security @ web01" {
		t.Errorf("stärkerer Vorrang steht nicht vorn: %q", erster.Name)
	}

	// Der laufende endet: Der starke Vorrang läuft, der schwache wartet weiter.
	env.Jobs.Complete(laufend, "fertig", ptrIntTest(0), nil)
	if err := <-stark; err != nil {
		t.Fatalf("der stärkere Vorrang hätte laufen müssen: %v", err)
	}
	select {
	case err := <-schwach:
		t.Fatalf("der schwächere Vorrang darf noch nicht laufen: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
}

// TestGleicherVorrangNachAusloesezeit: Zwei Gruppen mit demselben Rang bleiben
// untereinander fair - wer zuerst ausgelöst hat, kommt zuerst dran.
func TestGleicherVorrangNachAusloesezeit(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	laufendenJobStarten(t, env, id)

	warteschlangeStart(env, id, 1, "zuerst", domain.DefaultGroupPriority, 5*time.Second)
	warteAufStatus(t, env, id, 1)
	warteschlangeStart(env, id, 2, "danach", domain.DefaultGroupPriority, 5*time.Second)
	warteAufStatus(t, env, id, 2)

	queue := env.Jobs.QueuedForServer(id)
	if queue[0].Name != "zuerst" || queue[1].Name != "danach" {
		t.Errorf("Reihenfolge bei gleichem Vorrang falsch: %q, %q", queue[0].Name, queue[1].Name)
	}
}

// TestGleicheRegelStehtNurEinmalAn: Nach einem einstündigen Update stünden
// sonst vier Health-Checks in der Schlange, die alle dasselbe prüfen.
func TestGleicheRegelStehtNurEinmalAn(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	laufendenJobStarten(t, env, id)

	warteschlangeStart(env, id, 7, "Health @ web01", domain.DefaultGroupPriority, 5*time.Second)
	warteAufStatus(t, env, id, 1)

	// Derselbe Zeitplan feuert erneut, während der erste noch wartet.
	zweiter := warteschlangeStart(env, id, 7, "Health @ web01", domain.DefaultGroupPriority, 5*time.Second)
	if err := <-zweiter; !errors.Is(err, services.ErrJobSuperseded) {
		t.Errorf("erwartet ErrJobSuperseded, bekam %v", err)
	}
	if n := len(env.Jobs.QueuedForServer(id)); n != 1 {
		t.Errorf("die Regel steht %d-mal in der Schlange, erlaubt ist einmal", n)
	}
}

// TestZuLangesWartenWirdVerworfen: Wer länger wartet als sein eigener Takt,
// wird vom nächsten Durchgang überholt - dann ist der Wartende überflüssig.
func TestZuLangesWartenWirdVerworfen(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	laufendenJobStarten(t, env, id)

	done := warteschlangeStart(env, id, 7, "Sync @ web01", domain.DefaultGroupPriority, 80*time.Millisecond)
	if err := <-done; !errors.Is(err, services.ErrJobSuperseded) {
		t.Fatalf("erwartet ErrJobSuperseded, bekam %v", err)
	}
	if n := len(env.Jobs.QueuedForServer(id)); n != 0 {
		t.Errorf("der Verworfene steht noch in der Schlange: %d", n)
	}

	// Die Zeile bleibt erhalten und nennt den Grund - verschwiegen wird nichts.
	var job domain.Job
	if err := env.DB().Where("name = ?", "Sync @ web01").First(&job).Error; err != nil {
		t.Fatal(err)
	}
	if job.Status != domain.JobStatusBlocked {
		t.Errorf("verworfener Job hat Status %q, erwartet %q", job.Status, domain.JobStatusBlocked)
	}
}

// TestUnmittelbareAktionWartetNicht: Hinter einer Aktion von Hand steht
// jemand, der auf eine Antwort wartet - eine sofortige Absage ist dort
// ehrlicher als eine stille Verzögerung.
func TestUnmittelbareAktionWartetNicht(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	laufendenJobStarten(t, env, id)

	if _, err := env.Jobs.Start(&id, nil, domain.RuleTypeReboot, "Neustart @ web01", "admin"); !errors.Is(err, services.ErrServerBusy) {
		t.Errorf("erwartet ErrServerBusy, bekam %v", err)
	}
}

// TestLaufenderJobWirdNichtVerdraengt: Vorrang entscheidet über die
// Warteschlange, nicht über einen Abbruch. Alles andere machte ein halb
// eingespieltes Update möglich.
func TestLaufenderJobWirdNichtVerdraengt(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	laufend := laufendenJobStarten(t, env, id)

	warteschlangeStart(env, id, 2, "dringend", 1, 5*time.Second)
	warteAufStatus(t, env, id, 1)

	var neu domain.Job
	if err := env.DB().Where("id = ?", laufend.ID).First(&neu).Error; err != nil {
		t.Fatal(err)
	}
	if neu.Status != domain.JobStatusRunning {
		t.Errorf("der laufende Job wurde verdrängt: %q", neu.Status)
	}
}

func ptrIntTest(i int) *int { return &i }

// TestZeitplanLaufReihtSichEinStattVerworfenZuWerden: derselbe Fall wie oben,
// aber über den ganzen Weg - so, wie er nachts um vier entsteht. Auf dem
// Server läuft noch etwas, der Zeitplan feuert trotzdem.
func TestZeitplanLaufReihtSichEinStattVerworfenZuWerden(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	health := findSystemHealthRule(t, env)
	kontaktVeralten(t, env, id) // sonst entfiele der Ping ohnehin (siehe C)

	laufend := laufendenJobStarten(t, env, id)

	fertig := make(chan struct{})
	go func() {
		env.Executor.RunRule(health, "scheduler")
		close(fertig)
	}()
	warteAufStatus(t, env, id, 1)

	// Der laufende endet - der Zeitplan-Lauf kommt dran und läuft durch.
	env.Jobs.Complete(laufend, "fertig", ptrIntTest(0), nil)
	select {
	case <-fertig:
	case <-time.After(10 * time.Second):
		t.Fatal("der eingereihte Zeitplan-Lauf ist nicht gelaufen")
	}

	var job domain.Job
	if err := env.DB().Where("rule_id = ?", health.ID).Order("rowid desc").First(&job).Error; err != nil {
		t.Fatal(err)
	}
	if job.Status != domain.JobStatusSuccess {
		t.Errorf("der nachgeholte Lauf endete mit %q, erwartet %q", job.Status, domain.JobStatusSuccess)
	}
}

// TestWartenderLaufLaesstSichAbbrechen: Er steht sichtbar in der Liste - also
// muss man ihn auch wieder herausnehmen können.
func TestWartenderLaufLaesstSichAbbrechen(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	laufendenJobStarten(t, env, id)

	done := warteschlangeStart(env, id, 7, "Sync @ web01", domain.DefaultGroupPriority, 10*time.Second)
	warteAufStatus(t, env, id, 1)
	wartend := env.Jobs.QueuedForServer(id)[0]

	if _, err := env.Jobs.Abort(repositories.ScopeAll(), wartend.ID, "admin"); err != nil {
		t.Fatalf("Abbruch des Wartenden: %v", err)
	}
	if err := <-done; !errors.Is(err, services.ErrJobSuperseded) {
		t.Errorf("der abgebrochene Wartende sollte ErrJobSuperseded liefern, war %v", err)
	}
	if n := len(env.Jobs.QueuedForServer(id)); n != 0 {
		t.Errorf("der abgebrochene Wartende steht noch in der Schlange: %d", n)
	}
}
