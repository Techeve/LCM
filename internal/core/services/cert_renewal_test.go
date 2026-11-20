package services

import (
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/tlsx"
	"LCM/internal/infrastructure/tlsx/tlsxtest"
	"LCM/internal/storage/repositories"
)

const day = 24 * time.Hour

// certEnv baut die Erneuerung mit eigener CA über einem leeren
// Datenverzeichnis; das Zertifikat legt der Test an.
func certEnv(t *testing.T) (*CertRenewal, *repositories.ServerRepository, string, *tlsx.CA) {
	t.Helper()
	dir := t.TempDir()
	ca, err := tlsx.EnsureCA(dir, time.Now().AddDate(-1, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.Server{}); err != nil {
		t.Fatal(err)
	}
	servers := repositories.NewServerRepository(db)
	r := NewCertRenewal(dir, servers, nil)
	r.SetCA(ca)
	return r, servers, dir, ca
}

// caCert lässt die CA ein Zertifikat ausstellen, das in daysLeft Tagen
// abläuft.
func caCert(t *testing.T, dir string, ca *tlsx.CA, daysLeft int) {
	t.Helper()
	notBefore := time.Now().Add(time.Duration(daysLeft)*day - tlsx.ServerCertValidity)
	if err := tlsx.IssueServerCert(dir, ca, notBefore, true); err != nil {
		t.Fatal(err)
	}
}

// agentSpec beschreibt einen Agenten-Server: seine Pins und wann LCM ihn
// zuletzt gesehen hat.
type agentSpec struct {
	name, keyPin, caPin string
	seenDaysAgo         int
}

func addAgent(t *testing.T, servers *repositories.ServerRepository, a agentSpec) {
	t.Helper()
	seen := time.Now().Add(-time.Duration(a.seenDaysAgo) * day)
	s := &domain.Server{Name: a.name, Transport: domain.TransportAgent, ServiceUser: "root",
		AgentID: a.name + "-id", AgentKeyPin: a.keyPin, AgentCAPin: a.caPin, AgentLastSeenAt: &seen}
	if err := servers.Create(s); err != nil {
		t.Fatal(err)
	}
}

func inspectCert(t *testing.T, dir string, ca *tlsx.CA) tlsx.CertInfo {
	t.Helper()
	info, err := tlsx.Inspect(dir, ca)
	if err != nil {
		t.Fatal(err)
	}
	return info
}

func state(t *testing.T, r *CertRenewal) CertState {
	t.Helper()
	st, err := r.State()
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// TestSchritt1StelltMitGleichemSchluesselUm: Das alte selbstsignierte
// Zertifikat wird sofort durch eins der CA mit demselben Schlüssel ersetzt -
// Agenten ab R2 haben den Schlüssel-Pin. Ein Agent bis 2.0, der seit 20 Tagen
// fehlt, hält das nicht auf.
func TestSchritt1StelltMitGleichemSchluesselUm(t *testing.T) {
	r, servers, dir, ca := certEnv(t)
	tlsxtest.LegacySelfSigned(t, dir, time.Now().AddDate(0, -6, 0))
	before := inspectCert(t, dir, ca)
	addAgent(t, servers, agentSpec{name: "r2", keyPin: before.KeyPin, seenDaysAgo: 1})
	addAgent(t, servers, agentSpec{name: "alt-offline", seenDaysAgo: 20})
	reloaded := false
	r.reload = func() error { reloaded = true; return nil }

	r.Run()

	after := inspectCert(t, dir, ca)
	if !after.FromCA || after.KeyPin != before.KeyPin {
		t.Fatalf("nach Schritt 1: %+v (Schlüssel vorher %s)", after, before.KeyPin)
	}
	if !reloaded {
		t.Error("neues Zertifikat nicht geladen")
	}
	if got := after.NotAfter.Sub(time.Now()); got < 89*day || got > 90*day {
		t.Errorf("Laufzeit %s, erwartet 90 Tage", got)
	}
}

// TestSchritt1WartetAufAlteAgenten: Ein aktiver Agent bis 2.0 (kein
// Schlüssel-Pin) hält den Umstieg auf; die Warnung nennt ihn und sagt, dass
// er zuerst ein Update braucht - aber erst in der Warnfrist.
func TestSchritt1WartetAufAlteAgenten(t *testing.T) {
	r, servers, dir, ca := certEnv(t)
	tlsxtest.LegacySelfSigned(t, dir, time.Now().AddDate(-1, 0, 45))
	before := inspectCert(t, dir, ca)
	addAgent(t, servers, agentSpec{name: "neu", keyPin: before.KeyPin})
	addAgent(t, servers, agentSpec{name: "alt", seenDaysAgo: 3})

	r.Run()

	if inspectCert(t, dir, ca).Fingerprint != before.Fingerprint {
		t.Fatal("umgestellt, obwohl ein Agent nur das alte Zertifikat kennt")
	}
	st := state(t, r)
	if len(st.WithoutKeyPin) != 1 || st.WithoutKeyPin[0] != "alt" {
		t.Errorf("WithoutKeyPin = %v", st.WithoutKeyPin)
	}
	msg := r.Warning(st)
	if !strings.Contains(msg, "alt") || !strings.Contains(msg, "aktualisieren") {
		t.Errorf("Warnung: %q", msg)
	}
	r.now = func() time.Time { return time.Now().Add(-30 * day) }
	if msg := r.Warning(state(t, r)); msg != "" {
		t.Errorf("Warnung vor der Frist: %q", msg)
	}
	// Neue Agenten bekommen den CA-Pin schon jetzt ins Token.
	if fp, _ := r.TokenCAFingerprint(); fp != ca.Fingerprint() {
		t.Errorf("Token-CA-Pin %q", fp)
	}
}

// TestVerbundenerAgentZaehltAlsAktiv: Ein seit Wochen verbundener Agent hat
// ein altes „zuletzt gesehen" - er zählt trotzdem.
func TestVerbundenerAgentZaehltAlsAktiv(t *testing.T) {
	r, servers, dir, _ := certEnv(t)
	tlsxtest.LegacySelfSigned(t, dir, time.Now().AddDate(0, -6, 0))
	addAgent(t, servers, agentSpec{name: "dauerläufer", seenDaysAgo: 40})
	r.WithOnline(func(id string) bool { return id == "dauerläufer-id" })

	if st := state(t, r); len(st.WithoutKeyPin) != 1 {
		t.Errorf("verbundener Agent nicht berücksichtigt: %v", st.WithoutKeyPin)
	}
}

// TestSchritt2NeuerSchluesselErstMitCAPin: Fällig (50 Tage übrig) und jeder
// aktive Agent meldet den CA-Pin - neuer Schlüssel. Ein Agent, der seit 20
// Tagen fehlt, hält das nicht auf.
func TestSchritt2NeuerSchluesselErstMitCAPin(t *testing.T) {
	r, servers, dir, ca := certEnv(t)
	caCert(t, dir, ca, 50)
	before := inspectCert(t, dir, ca)
	addAgent(t, servers, agentSpec{name: "a1", keyPin: before.KeyPin, caPin: ca.Fingerprint()})
	addAgent(t, servers, agentSpec{name: "weg", keyPin: before.KeyPin, seenDaysAgo: 20})

	r.Run()

	after := inspectCert(t, dir, ca)
	if after.KeyPin == before.KeyPin || !after.FromCA {
		t.Fatalf("nicht mit neuem Schlüssel erneuert: %+v", after)
	}
	if st := state(t, r); len(st.StaleWithoutCAPin) != 1 || st.StaleWithoutCAPin[0] != "weg" {
		t.Errorf("StaleWithoutCAPin = %v", st.StaleWithoutCAPin)
	}
}

// TestSchritt2BehaeltSchluesselOhneCAPin: Ein aktiver Agent ohne CA-Pin (R2,
// kennt nur den Schlüssel) - erneuert wird mit demselben Schlüssel.
func TestSchritt2BehaeltSchluesselOhneCAPin(t *testing.T) {
	r, servers, dir, ca := certEnv(t)
	caCert(t, dir, ca, 50)
	before := inspectCert(t, dir, ca)
	addAgent(t, servers, agentSpec{name: "a1", keyPin: before.KeyPin, caPin: ca.Fingerprint()})
	addAgent(t, servers, agentSpec{name: "r2", keyPin: before.KeyPin, seenDaysAgo: 2})

	r.Run()

	after := inspectCert(t, dir, ca)
	if after.Fingerprint == before.Fingerprint || after.KeyPin != before.KeyPin {
		t.Fatalf("erwartet: erneuert mit gleichem Schlüssel, bekam %+v", after)
	}
	if st := state(t, r); len(st.WithoutCAPin) != 1 || st.WithoutCAPin[0] != "r2" {
		t.Errorf("WithoutCAPin = %v", st.WithoutCAPin)
	}
}

// TestErneuerungMonatlich: Ein frisches Zertifikat bleibt 30 Tage, danach
// (weniger als 60 übrig) kommt das nächste mit neuem Schlüssel.
func TestErneuerungMonatlich(t *testing.T) {
	r, _, dir, ca := certEnv(t)
	start := time.Now()
	r.now = func() time.Time { return start }
	caCert(t, dir, ca, 90)
	first := inspectCert(t, dir, ca)

	r.now = func() time.Time { return start.Add(29 * day) }
	r.Run()
	if inspectCert(t, dir, ca).Fingerprint != first.Fingerprint {
		t.Fatal("nach 29 Tagen schon erneuert")
	}
	if msg := r.Warning(state(t, r)); msg != "" {
		t.Errorf("Warnung ohne Anlass: %q", msg)
	}

	r.now = func() time.Time { return start.Add(31 * day) }
	r.Run()
	second := inspectCert(t, dir, ca)
	if second.KeyPin == first.KeyPin {
		t.Fatal("nach 31 Tagen nicht mit neuem Schlüssel erneuert")
	}
	if got := second.NotAfter.Sub(start.Add(31 * day)); got < 89*day {
		t.Errorf("neues Zertifikat gilt nur noch %s", got)
	}
}

// TestCAWechselLaeuftWieSchritt1: Stammt das Zertifikat von einer früheren
// CA (neue CA des Betreibers), stellt die aktuelle es mit demselben
// Schlüssel neu aus - Agenten mit dem Schlüssel-Pin lernen so die neue CA.
func TestCAWechselLaeuftWieSchritt1(t *testing.T) {
	r, servers, dir, oldCA := certEnv(t)
	caCert(t, dir, oldCA, 80)
	before := inspectCert(t, dir, oldCA)
	addAgent(t, servers, agentSpec{name: "a1", keyPin: before.KeyPin, caPin: oldCA.Fingerprint()})
	newCA, err := tlsx.EnsureCA(t.TempDir(), time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	r.SetCA(newCA)

	r.Run()

	after := inspectCert(t, dir, newCA)
	if !after.FromCA || after.KeyPin != before.KeyPin {
		t.Fatalf("CA-Wechsel: %+v", after)
	}
}

// TestTokenOhneCAPinBeiFremdemZertifikat: Liefert LCM ein Zertifikat des
// Betreibers aus, gehört kein CA-Pin ins Token, LCM erneuert nichts und
// warnt vor dem Ablauf.
func TestTokenOhneCAPinBeiFremdemZertifikat(t *testing.T) {
	r, _, dir, _ := certEnv(t)
	tlsxtest.ForeignCert(t, dir, time.Now().Add(20*day))
	before := inspectCert(t, dir, nil)

	r.Run()

	if inspectCert(t, dir, nil).Fingerprint != before.Fingerprint {
		t.Error("Betreiber-Zertifikat angefasst")
	}
	if fp, err := r.TokenCAFingerprint(); err != nil || fp != "" {
		t.Errorf("Token-CA-Pin %q, err %v", fp, err)
	}
	if msg := r.Warning(state(t, r)); !strings.Contains(msg, "eigenes Zertifikat") {
		t.Errorf("Warnung: %q", msg)
	}
}

// TestCAWarnung: Ein Jahr vor dem Ende der CA warnt LCM - es erneuert sie
// nicht selbst.
func TestCAWarnung(t *testing.T) {
	r, _, dir, ca := certEnv(t)
	caCert(t, dir, ca, 80)
	if msg := r.Warning(state(t, r)); msg != "" {
		t.Fatalf("Warnung ohne Anlass: %q", msg)
	}
	r.now = func() time.Time { return ca.Cert.NotAfter.Add(-200 * day) }
	if msg := r.Warning(state(t, r)); !strings.Contains(msg, tlsx.CAFileName) {
		t.Errorf("Warnung zur CA fehlt: %q", msg)
	}
}

// TestZertifikatAmEndeDerCA: Endet das Zertifikat schon mit der CA, stellt
// LCM nicht täglich ein neues aus.
func TestZertifikatAmEndeDerCA(t *testing.T) {
	r, _, dir, ca := certEnv(t)
	r.now = func() time.Time { return ca.Cert.NotAfter.Add(-30 * day) }
	if err := tlsx.IssueServerCert(dir, ca, r.now(), true); err != nil {
		t.Fatal(err)
	}
	before := inspectCert(t, dir, ca)
	r.Run()
	if inspectCert(t, dir, ca).Fingerprint != before.Fingerprint {
		t.Error("Zertifikat am Ende der CA erneut ausgestellt")
	}
}
