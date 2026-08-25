package services_test

import (
	"errors"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/storage/repositories"
)

// proxmoxTestServer legt einen als Proxmox VE erkannten Server an.
func proxmoxTestServer(t *testing.T, env *testEnv) *domain.Server {
	t.Helper()
	s := &domain.Server{
		Name: "pve01", Host: "pve01.local", SSHPort: 22, ServiceUser: "lcm-svc",
		HostKeyFingerprint: "SHA256:test", PrivateKeyEnc: "enc", Reachable: true,
		ProxmoxType: domain.ProxmoxPVE, ProxmoxVersion: "8.2.4",
	}
	if err := repositories.NewServerRepository(env.DB()).Create(s); err != nil {
		t.Fatalf("proxmox-server anlegen: %v", err)
	}
	return s
}

// TestProxmoxGuards: auf Proxmox-Systemen sind Firewall, fremde
// Repositories und der Linux-Benutzer-Sync gesperrt - jeweils mit
// ErrProxmoxRestricted, bevor irgendein SSH-Kontakt stattfindet.
func TestProxmoxGuards(t *testing.T) {
	env := newTestEnv(t)
	server := proxmoxTestServer(t, env)
	scope := repositories.ScopeAll()

	if _, err := env.Servers.ConfigureFirewall(scope, server.ID, true, "80,443", domain.FirewallSSHSources{}, "admin"); !errors.Is(err, services.ErrProxmoxRestricted) {
		t.Fatalf("ConfigureFirewall: erwartet ErrProxmoxRestricted, bekam %v", err)
	}
	if _, err := env.Servers.AddKnownRepository(scope, server.ID, "docker", "admin"); !errors.Is(err, services.ErrProxmoxRestricted) {
		t.Fatalf("AddKnownRepository: erwartet ErrProxmoxRestricted, bekam %v", err)
	}
	if _, err := env.Prov.SyncUsers(server, "admin"); !errors.Is(err, services.ErrProxmoxRestricted) {
		t.Fatalf("SyncUsers: erwartet ErrProxmoxRestricted, bekam %v", err)
	}
	if err := env.Prov.AssignLinuxUserToServer(server, 1, "admin"); !errors.Is(err, services.ErrProxmoxRestricted) {
		t.Fatalf("AssignLinuxUserToServer: erwartet ErrProxmoxRestricted, bekam %v", err)
	}
}
