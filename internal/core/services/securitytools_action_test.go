package services_test

import (
	"strings"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/storage/repositories"
)

// TestSecurityToolAllowlistExpansion prüft, dass benannte Allowlists (per ID)
// in die fail2ban-ignoreip expandiert werden - zusätzlich zu Ad-hoc-IPs.
func TestSecurityToolAllowlistExpansion(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")

	list, err := env.Settings.SaveIPAllowlist(domain.IPAllowlist{
		Name:    "Büro",
		Entries: "203.0.113.0/24\n2001:db8::5",
	}, "admin")
	if err != nil {
		t.Fatal(err)
	}

	env.Dialer.Commands = nil
	job, err := env.Servers.ConfigureSecurityTool(repositories.ScopeAll(), id, services.SecurityToolInput{
		Tool:         "fail2ban",
		AllowlistIPs: []string{"198.51.100.7"},
		AllowlistIDs: []uint{list.ID},
	}, "admin")
	if err != nil {
		t.Fatalf("security-tool: %v", err)
	}
	if done := waitForJob(t, env, job.ID); done.Status != domain.JobStatusSuccess {
		t.Fatalf("job fehlgeschlagen: %s", done.Output)
	}

	all := strings.Join(env.Dialer.Commands, "\n")
	// ignoreip enthält Ad-hoc-IP UND die aufgelösten Allowlist-Einträge.
	for _, want := range []string{"198.51.100.7", "203.0.113.0/24", "2001:db8::5"} {
		if !strings.Contains(all, want) {
			t.Errorf("ignoreip enthält %q nicht:\n%s", want, all)
		}
	}
	// Loopback ist immer dabei (Aussperr-Schutz).
	if !strings.Contains(all, "127.0.0.1/8") {
		t.Errorf("loopback fehlt in ignoreip:\n%s", all)
	}
}
