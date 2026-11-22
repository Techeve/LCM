package services

import (
	"reflect"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

func port(p int, proto, process string) domain.ListeningPort {
	return domain.ListeningPort{Port: p, Proto: proto, Bind: "0.0.0.0", IPVersion: "v4", Process: process}
}

// Der Scan behält den Vorrang: Was er kennt, bleibt unverändert; fehlende
// eigene Ports kommen dazu, sortiert nach Port.
func TestWithLcmListeners(t *testing.T) {
	scanned := []domain.ListeningPort{port(22, "tcp", "sshd"), port(9310, "tcp", "lcm"), port(9320, "udp", "x")}
	own := []domain.ListeningPort{port(9310, "tcp", lcmProcess), port(9320, "tcp", lcmProcess), port(9330, "tcp", lcmProcess)}

	got := withLcmListeners(scanned, own)

	want := []domain.ListeningPort{
		port(22, "tcp", "sshd"), port(9310, "tcp", "lcm"), port(9320, "tcp", lcmProcess),
		port(9320, "udp", "x"), port(9330, "tcp", lcmProcess),
	}
	if len(got) != len(want) {
		t.Fatalf("%d Einträge, erwartet %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Errorf("Eintrag %d = %+v, erwartet %+v", i, got[i], want[i])
		}
	}
}

// Der LCM-Host bekommt seine eigenen Ports in der Antwort, auch ohne Scan und
// ohne dass sie in der Datenbank stehen; ein anderer Server nicht.
func TestServerGetErgaenztEigenePortenNurImLcmHost(t *testing.T) {
	db := decommissionTestDB(t)
	repo := repositories.NewServerRepository(db)
	svc := NewServerService(repo, nil, NewAuditService(repositories.NewAuditRepository(db)), nil, nil).
		WithLcmListeners(func() ([]domain.ListeningPort, error) {
			return []domain.ListeningPort{port(9330, "tcp", lcmProcess)}, nil
		})
	host := &domain.Server{Name: "lcm", Host: "localhost", SSHPort: 22, ServiceUser: "lcm-svc", PrivateKeyEnc: "x", HostKeyFingerprint: "x"}
	other := &domain.Server{Name: "web", Host: "10.0.0.9", SSHPort: 22, ServiceUser: "lcm-svc", PrivateKeyEnc: "x", HostKeyFingerprint: "x"}
	for _, s := range []*domain.Server{host, other} {
		if err := repo.Create(s); err != nil {
			t.Fatal(err)
		}
	}

	got, err := svc.Get(repositories.ScopeAll(), host.ID)
	if err != nil {
		t.Fatal(err)
	}
	ports := parseListeningPortsJSON(got.ListeningPorts)
	if len(ports) != 1 || ports[0].Port != 9330 {
		t.Errorf("LCM-Host: %+v", ports)
	}
	if stored, _ := repo.FindByID(repositories.ScopeAll(), host.ID); stored.ListeningPorts != "" {
		t.Errorf("in der Datenbank steht %q, erwartet nichts", stored.ListeningPorts)
	}

	got, err = svc.Get(repositories.ScopeAll(), other.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ListeningPorts != "" {
		t.Errorf("fremder Server: %q", got.ListeningPorts)
	}
}

func withRoles(p domain.ListeningPort, roles ...string) domain.ListeningPort {
	p.LcmRoles = roles
	return p
}

// Ein gescannter Port behält Bind, IP-Version und Prozess aus dem Scan, bekommt
// aber die Schnittstellen von LCM - für jeden gescannten Socket, IPv4 wie IPv6.
func TestWithLcmListenersUebernimmtRollenInGescannte(t *testing.T) {
	v6 := port(9310, "tcp", "lcm")
	v6.Bind, v6.IPVersion = "::", "v6"
	scanned := []domain.ListeningPort{port(9310, "tcp", "lcm"), v6, port(22, "tcp", "sshd")}
	own := []domain.ListeningPort{withRoles(port(9310, "tcp", lcmProcess), domain.LcmRoleUI, domain.LcmRoleMCP)}

	got := withLcmListeners(scanned, own)

	want := []domain.ListeningPort{
		port(22, "tcp", "sshd"),
		withRoles(port(9310, "tcp", "lcm"), domain.LcmRoleUI, domain.LcmRoleMCP),
		withRoles(v6, domain.LcmRoleUI, domain.LcmRoleMCP),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, erwartet %+v", got, want)
	}
}

// Ein ungescannter eigener Port kommt samt Rollen dazu.
func TestWithLcmListenersHaengtUngesehenePortsMitRollenAn(t *testing.T) {
	own := []domain.ListeningPort{withRoles(port(80, "tcp", lcmProcess), domain.LcmRoleACME)}

	got := withLcmListeners(nil, own)

	if !reflect.DeepEqual(got, own) {
		t.Errorf("got %+v, erwartet %+v", got, own)
	}
}
