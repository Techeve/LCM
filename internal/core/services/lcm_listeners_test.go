package services_test

import (
	"fmt"
	"reflect"
	"slices"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
)

func lcmPorts(t *testing.T, svc *services.SettingsService) []string {
	t.Helper()
	ports, err := svc.LcmListeners()
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, p := range ports {
		got = append(got, fmt.Sprintf("%d/%s", p.Port, p.Proto))
	}
	slices.Sort(got)
	return got
}

func wantPorts(t *testing.T, got []string, want ...string) {
	t.Helper()
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("Ports = %v, erwartet %v", got, want)
	}
}

// Der Standard: Oberfläche und Agent; MCP ist aus.
func TestLcmListenersStandard(t *testing.T) {
	svc, _ := newSettingsService(t)
	wantPorts(t, lcmPorts(t, svc), "9310/tcp", "9320/tcp")
}

// MCP kommt dazu, sobald es eingeschaltet ist, der Agent geht, wenn er aus ist.
func TestLcmListenersFolgenDenSchaltern(t *testing.T) {
	svc, repo := newSettingsService(t)
	st, _ := repo.Get()
	st.MCPEnabled = true
	if err := repo.Save(st); err != nil {
		t.Fatal(err)
	}
	wantPorts(t, lcmPorts(t, svc), "9310/tcp", "9320/tcp", "9330/tcp")

	st.AgentEnabled = false
	st.MCPPort = 9340
	if err := repo.Save(st); err != nil {
		t.Fatal(err)
	}
	wantPorts(t, lcmPorts(t, svc), "9310/tcp", "9340/tcp")
}

// Eigene Ports aus den Einstellungen gelten; LCM_AGENT_PORT=0 schaltet den
// Agent-Listener ab.
func TestLcmListenersEigenePortsUndUmgebung(t *testing.T) {
	svc, repo := newSettingsService(t)
	st, _ := repo.Get()
	st.UIPort, st.AgentPort = 9400, 9401
	if err := repo.Save(st); err != nil {
		t.Fatal(err)
	}
	wantPorts(t, lcmPorts(t, svc), "9400/tcp", "9401/tcp")

	svc.WithAddressConfig(services.AddressConfig{Agent: services.FixedPort{Set: true, Value: 0}})
	wantPorts(t, lcmPorts(t, svc), "9400/tcp")
}

// Modus C: dazu der gemeinsame Port.
func TestLcmListenersModusC(t *testing.T) {
	svc, repo := newSettingsService(t)
	st, _ := repo.Get()
	st.AddressMode = domain.AddressModeSharedPort
	st.SharedTLSPort = 443
	if err := repo.Save(st); err != nil {
		t.Fatal(err)
	}
	wantPorts(t, lcmPorts(t, svc), "443/tcp", "9310/tcp", "9320/tcp")
}

// Port 80 steht nur mit Let's Encrypt über HTTP-01 dabei, nicht mit DNS-01.
func TestLcmListenersPort80NurMitHTTP01(t *testing.T) {
	svc, repo := newSettingsService(t)
	st, _ := repo.Get()
	st.ACMEEnabled = true
	st.ACMEChallenge = services.ACMEChallengeHTTP01
	if err := repo.Save(st); err != nil {
		t.Fatal(err)
	}
	wantPorts(t, lcmPorts(t, svc), "80/tcp", "9310/tcp", "9320/tcp")

	st.ACMEChallenge = services.ACMEChallengeDNS01
	if err := repo.Save(st); err != nil {
		t.Fatal(err)
	}
	wantPorts(t, lcmPorts(t, svc), "9310/tcp", "9320/tcp")
}

// lcmRoles liefert je Port die Schnittstellen, wie LcmListeners sie meldet.
func lcmRoles(t *testing.T, svc *services.SettingsService) map[int][]string {
	t.Helper()
	ports, err := svc.LcmListeners()
	if err != nil {
		t.Fatal(err)
	}
	got := map[int][]string{}
	for _, p := range ports {
		got[p.Port] = p.LcmRoles
	}
	return got
}

func wantRoles(t *testing.T, got, want map[int][]string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Rollen = %v, erwartet %v", got, want)
	}
}

// Modus A und B: jede Schnittstelle auf ihrem eigenen Port.
func TestLcmListenersRollenAufGetrenntenPorts(t *testing.T) {
	svc, repo := newSettingsService(t)
	st, _ := repo.Get()
	st.MCPEnabled = true
	if err := repo.Save(st); err != nil {
		t.Fatal(err)
	}
	wantRoles(t, lcmRoles(t, svc), map[int][]string{
		9310: {"ui"}, 9320: {"agent"}, 9330: {"mcp"},
	})
}

// Modus C: Auf dem gemeinsamen Port steht die Oberfläche, Agent und MCP nur,
// wenn sie an sind; ihre eigenen Ports laufen daneben weiter.
func TestLcmListenersRollenImModusC(t *testing.T) {
	svc, repo := newSettingsService(t)
	st, _ := repo.Get()
	st.AddressMode = domain.AddressModeSharedPort
	st.SharedTLSPort = 443
	st.AgentEnabled = false
	if err := repo.Save(st); err != nil {
		t.Fatal(err)
	}
	wantRoles(t, lcmRoles(t, svc), map[int][]string{443: {"ui"}, 9310: {"ui"}})

	st.AgentEnabled, st.MCPEnabled = true, true
	if err := repo.Save(st); err != nil {
		t.Fatal(err)
	}
	wantRoles(t, lcmRoles(t, svc), map[int][]string{
		443: {"ui", "agent", "mcp"}, 9310: {"ui"}, 9320: {"agent"}, 9330: {"mcp"},
	})
}

// Port 80 trägt die Rolle der Let's-Encrypt-Prüfung; teilt sich ein Dienst den
// Port, werden die Rollen in fester Reihenfolge zusammengeführt.
func TestLcmListenersRollePort80(t *testing.T) {
	svc, repo := newSettingsService(t)
	st, _ := repo.Get()
	st.ACMEEnabled = true
	st.ACMEChallenge = services.ACMEChallengeHTTP01
	if err := repo.Save(st); err != nil {
		t.Fatal(err)
	}
	wantRoles(t, lcmRoles(t, svc), map[int][]string{80: {"acme"}, 9310: {"ui"}, 9320: {"agent"}})

	svc.WithAddressConfig(services.AddressConfig{ACMEHTTPPort: 9310})
	wantRoles(t, lcmRoles(t, svc), map[int][]string{9310: {"ui", "acme"}, 9320: {"agent"}})
}
