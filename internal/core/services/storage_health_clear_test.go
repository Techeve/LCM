package services_test

import (
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/storage/repositories"
)

// stelleSpeicherBefund legt die Antwort des Speicher-Scans fest. Der Fake
// trifft über Teilzeichenketten des Kommandos; "zpool list" kommt nur im
// Speicher-Scan vor.
func stelleSpeicherBefund(env *testEnv, tsv string) {
	env.Dialer.Responses["zpool list"] = sshx.FakeResponse{Output: tsv}
}

func speicherBefunde(t *testing.T, env *testEnv, id uint) []domain.StorageHealth {
	t.Helper()
	befunde, err := env.Servers.StorageHealth(repositories.ScopeAll(), id)
	if err != nil {
		t.Fatal(err)
	}
	return befunde
}

// hatBefund meldet, ob unter den Status-Hinweisen ein Speicher-Defekt steht.
func hatBefund(t *testing.T, env *testEnv, id uint) bool {
	t.Helper()
	_, hinweise, _, err := env.Servers.Status(repositories.ScopeAll(), id)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range hinweise {
		if h.Key == "storageDefect" {
			return true
		}
	}
	return false
}

// TestBehobenerDefektVerschwindetWieder ist die Gegenprobe zur Erkennung.
//
// Ein Prüfwerk, das einen Defekt meldet, ist nur die halbe Miete - es muss ihn
// auch zurücknehmen, sobald er behoben ist. Ein Befund, der hängenbleibt,
// nachdem die Platte längst getauscht wurde, kostet genauso viel Vertrauen wie
// ein übersehener Defekt: Beim nächsten Mal glaubt ihn niemand mehr.
//
// Der Test bildet den Ablauf am Testbett nach: Pool degradiert, Pool repariert.
func TestBehobenerDefektVerschwindetWieder(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	vollstaendigeErkennung(env)

	// 1. Der Störfall: ein Mirror ohne Redundanz, mit gezählten Fehlern.
	stelleSpeicherBefund(env, "zfs\ttank\tDEGRADED\t61\t0\t9\t7\t0\t\n")
	env.Executor.RunRule(findSystemSyncRule(t, env), "admin")

	befunde := speicherBefunde(t, env, id)
	if len(befunde) != 1 || befunde[0].State != domain.StorageStateDegraded {
		t.Fatalf("der degradierte Pool wurde nicht erfasst: %+v", befunde)
	}
	if !hatBefund(t, env, id) {
		t.Error("der degradierte Pool taucht nicht in den Status-Hinweisen auf")
	}

	// 2. Die Reparatur: vdev wieder online, Zähler zurückgesetzt.
	stelleSpeicherBefund(env, "zfs\ttank\tONLINE\t61\t0\t9\t0\t0\t\n")
	env.Executor.RunRule(findSystemSyncRule(t, env), "admin")

	befunde = speicherBefunde(t, env, id)
	if len(befunde) != 1 || befunde[0].State != domain.StorageStateHealthy {
		t.Fatalf("nach der Reparatur erwartet ein gesunder Pool, bekam %+v", befunde)
	}
	if befunde[0].Message != "" {
		t.Errorf("der gesunde Pool trägt noch eine Meldung: %q", befunde[0].Message)
	}
	if hatBefund(t, env, id) {
		t.Error("der Befund steht noch in den Status-Hinweisen, obwohl der Pool wieder in Ordnung ist")
	}
}

// TestVerschwundenerVerbundHinterlaesstKeineLeiche: Wird ein Pool exportiert
// oder eine Platte ausgebaut, meldet der Scan ihn gar nicht mehr. Dann darf
// auch kein Eintrag von gestern zurückbleiben - sonst zeigte die Oberfläche
// dauerhaft den Zustand eines Verbunds, den es nicht mehr gibt.
func TestVerschwundenerVerbundHinterlaesstKeineLeiche(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	vollstaendigeErkennung(env)

	stelleSpeicherBefund(env, "zfs\ttank\tDEGRADED\t61\t0\t9\t7\t0\t\n")
	env.Executor.RunRule(findSystemSyncRule(t, env), "admin")
	if len(speicherBefunde(t, env, id)) != 1 {
		t.Fatal("Vorbedingung: der Pool sollte erfasst sein")
	}

	stelleSpeicherBefund(env, "")
	env.Executor.RunRule(findSystemSyncRule(t, env), "admin")

	if befunde := speicherBefunde(t, env, id); len(befunde) != 0 {
		t.Errorf("der verschwundene Verbund steht noch in der Ablage: %+v", befunde)
	}
	if hatBefund(t, env, id) {
		t.Error("der Befund steht noch in den Status-Hinweisen")
	}
}
