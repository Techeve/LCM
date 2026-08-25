package services

import (
	"errors"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

// Die vier Einrichtungs-Aktionen der LCM-Host-Karte (Trivy, Sandbox,
// apt-cacher-ng, CrowdSec-LAPI) fuehren am Ende dasselbe aus: ein Skript per
// SSH als root auf dem vermeintlich EIGENEN Host. Im Container gibt es dort
// weder apt noch einen Dienst, der den Neustart ueberlebt - die Aktion muesste
// scheitern.
//
// Dass ein LCM-Host-Eintrag im Container ueberhaupt existiert, ist kein
// konstruierter Fall: Die Selbstaufnahme unterbleibt dort zwar
// (self_register.go), aber ein von Hand angelegter localhost-Eintrag oder ein
// in den Container zurueckgespieltes Backup einer Host-Installation bringt ihn
// mit.

// containerTestService baut einen ServerService mit eigener DB und einem
// gesetzten Container-Schalter. Ohne diesen Schalter haenge das Ergebnis
// daran, wo der Test gerade laeuft - auf einem Entwicklerrechner „Host", im
// CI-Container „Container"; der jeweils andere Zweig waere nie geprueft.
func containerTestService(t *testing.T, imContainer bool) (*ServerService, uint) {
	t.Helper()
	// DB direkt aufbauen: storage importiert services - ein storage-Import
	// waere hier ein Import-Zyklus (wie in self_register_test.go).
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("test-db: %v", err)
	}
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	if err := db.AutoMigrate(&domain.Server{}, &domain.Package{}); err != nil {
		t.Fatalf("migration: %v", err)
	}
	repo := repositories.NewServerRepository(db)
	// Ein Eintrag, der als LCM-Host durchgeht: localhost auf Port 22, apt.
	host := &domain.Server{
		Name: "lcm-host", Host: "localhost", SSHPort: 22, ServiceUser: "lcm-svc",
		HostKeyFingerprint: "SHA256:test", PrivateKeyEnc: "enc", PackageManager: "apt",
	}
	if err := repo.Create(host); err != nil {
		t.Fatalf("server anlegen: %v", err)
	}
	svc := &ServerService{servers: repo, containerCheck: func() bool { return imContainer }}
	return svc, host.ID
}

func TestHostAktionenImContainerAbgelehnt(t *testing.T) {
	svc, id := containerTestService(t, true)

	// Eine Stelle deckt alle vier ab (requireLcmHostApt) - genau deshalb sitzt
	// die Pruefung dort und nicht in jeder Aktion einzeln.
	if _, err := svc.requireLcmHostApt(repositories.ScopeAll(), id); !errors.Is(err, ErrLcmHostInContainer) {
		t.Fatalf("im Container erwartete ich ErrLcmHostInContainer, bekam %v", err)
	}
}

func TestHostAktionenAufDemHostErlaubt(t *testing.T) {
	svc, id := containerTestService(t, false)

	if _, err := svc.requireLcmHostApt(repositories.ScopeAll(), id); err != nil {
		t.Fatalf("auf einem Host muss die Einrichtung moeglich bleiben, bekam %v", err)
	}
}

// TestLcmHostStatusMeldetDenContainer: Die Oberflaeche braucht den Grund, um
// eine Erklaerung zu zeigen statt Schaltflaechen, die scheitern muessen.
// Ebenso wichtig ist, was NICHT gemeldet wird: „Trivy nicht installiert"
// zusammen mit einer Installations-Schaltflaeche waere im Container eine
// Aufforderung ins Leere.
func TestLcmHostStatusMeldetDenContainer(t *testing.T) {
	svc, id := containerTestService(t, true)

	st, err := svc.LcmHostStatus(repositories.ScopeAll(), id)
	if err != nil {
		t.Fatal(err)
	}
	if !st.InContainer {
		t.Error("der Status muss die Betriebsart melden")
	}
	if st.SandboxRetrofit {
		t.Error("im Container gibt es nichts nachzuruesten - das darf nicht angeboten werden")
	}
}

// TestJoinLoopbackImContainerAbgelehnt: Der Join-Wizard nimmt jede Adresse an,
// auch localhost. Im Container zeigt die aber auf den Container selbst - der
// Eintrag verwaltete LCMs eigenes Wegwerf-Dateisystem und bekaeme obendrein
// die LCM-Host-Sonderrolle mit Karten und Aktionen, die dort nichts
// ausrichten koennen. Deshalb gar nicht erst anlegen.
func TestJoinLoopbackImContainerAbgelehnt(t *testing.T) {
	svc, _ := containerTestService(t, true)

	for _, host := range []string{"localhost", "127.0.0.1", "::1", " LocalHost "} {
		_, err := svc.Join(JoinRequest{Name: "neu", Host: host, Port: 22, Actor: "admin"})
		if !errors.Is(err, ErrLoopbackInContainer) {
			t.Errorf("%q: erwartete ErrLoopbackInContainer, bekam %v", host, err)
		}
	}
}

// TestJoinLoopbackAufDemHostErlaubt grenzt die Sperre ab: Auf einer
// Host-Installation ist genau das der Normalfall - LCM nimmt sich selbst so
// auf. Ein Test, der nur die Ablehnung prueft, koennte auch eine Sperre
// durchgehen lassen, die IMMER greift.
func TestJoinLoopbackAufDemHostErlaubt(t *testing.T) {
	svc, _ := containerTestService(t, false)

	// Weiter kommt der Aufruf ohne Dialer nicht - entscheidend ist, dass er
	// NICHT an der Container-Sperre scheitert.
	_, err := svc.Join(JoinRequest{Name: "neu", Host: "localhost", Port: 22, Actor: "admin"})
	if errors.Is(err, ErrLoopbackInContainer) {
		t.Error("auf einem Host darf localhost nicht gesperrt sein - so nimmt LCM sich selbst auf")
	}
}
