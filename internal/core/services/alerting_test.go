package services_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/infrastructure/crypto"
	"LCM/internal/storage"
	"LCM/internal/storage/repositories"
)

// alertEnv bündelt die für die Alerting-Tests nötigen Bausteine.
type alertEnv struct {
	db           *gorm.DB
	servers      *repositories.ServerRepository
	groups       *repositories.GroupRepository
	alertRepo    *repositories.AlertRepository
	notification *services.NotificationService
	alerts       *services.AlertService
}

func newAlertEnv(t *testing.T) *alertEnv {
	t.Helper()
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("db öffnen: %v", err)
	}
	if err := storage.Migrate(db); err != nil {
		t.Fatalf("migrieren: %v", err)
	}
	cipher, err := crypto.NewCipher(crypto.GenerateKey())
	if err != nil {
		t.Fatal(err)
	}
	audit := services.NewAuditService(repositories.NewAuditRepository(db))
	serverRepo := repositories.NewServerRepository(db)
	groupRepo := repositories.NewGroupRepository(db)
	alertRepo := repositories.NewAlertRepository(db)
	notificationRepo := repositories.NewNotificationRepository(db)
	notification := services.NewNotificationService(notificationRepo, alertRepo, cipher, audit)
	alerts := services.NewAlertService(alertRepo, serverRepo, groupRepo, notification, audit)
	return &alertEnv{
		db: db, servers: serverRepo, groups: groupRepo, alertRepo: alertRepo,
		notification: notification, alerts: alerts,
	}
}

func (e *alertEnv) createServer(t *testing.T, name string, mut func(*domain.Server)) *domain.Server {
	t.Helper()
	s := &domain.Server{
		Name: name, Host: name + ".local", SSHPort: 22, ServiceUser: "lcm-svc",
		HostKeyFingerprint: "SHA256:test", PrivateKeyEnc: "enc", Reachable: true,
	}
	if mut != nil {
		mut(s)
	}
	if err := e.servers.Create(s); err != nil {
		t.Fatalf("server anlegen: %v", err)
	}
	return s
}

// --- Notification-Service ----------------------------------------------------

const validEmailConfig = `{"host":"smtp.example.com","port":587,"from":"lcm@example.com","recipients":["ops@example.com"]}`

func TestNotificationChannelCreateEncryptsSecret(t *testing.T) {
	env := newAlertEnv(t)
	ch, err := env.notification.Create(services.ChannelInput{
		Name: "Ops-Mail", Type: domain.ChannelTypeEmail, Enabled: true,
		Config: validEmailConfig, Secret: "smtp-passwort",
	}, "admin")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if ch.SecretEnc == "" || ch.SecretEnc == "smtp-passwort" {
		t.Errorf("Secret nicht verschlüsselt: %q", ch.SecretEnc)
	}
}

func TestNotificationChannelCreateValidatesConfig(t *testing.T) {
	env := newAlertEnv(t)
	// Fehlende Empfänger => Validierung schlägt fehl.
	_, err := env.notification.Create(services.ChannelInput{
		Name: "Kaputt", Type: domain.ChannelTypeEmail, Enabled: true,
		Config: `{"host":"h","port":25,"from":"a@b.c"}`,
	}, "admin")
	if err == nil {
		t.Fatal("erwartete Validierungsfehler bei fehlenden Empfängern")
	}
	// Unbekannter Typ => Fehler.
	if _, err := env.notification.Create(services.ChannelInput{
		Name: "SMS", Type: "sms",
	}, "admin"); err == nil {
		t.Fatal("erwartete Fehler bei unbekanntem Kanaltyp")
	}
}

func TestNotificationChannelDeleteInUse(t *testing.T) {
	env := newAlertEnv(t)
	ch, err := env.notification.Create(services.ChannelInput{
		Name: "Ops-Mail", Type: domain.ChannelTypeEmail, Enabled: true, Config: validEmailConfig,
	}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.alerts.Create(services.AlertRuleInput{
		Name: "Disk", Type: domain.AlertTypeDiskCapacity, Enabled: true,
		ChannelID: &ch.ID, ThresholdPercent: 90,
	}, "admin"); err != nil {
		t.Fatal(err)
	}
	if err := env.notification.Delete(ch.ID, "admin"); err != services.ErrChannelInUse {
		t.Errorf("erwartete ErrChannelInUse, bekam: %v", err)
	}
}

// --- Alert-Service: Auswertung -----------------------------------------------

func TestAlertDiskCapacityFiresWithCooldown(t *testing.T) {
	env := newAlertEnv(t)
	env.createServer(t, "db-01", func(s *domain.Server) {
		s.DiskTotalMB = 1000
		s.DiskUsedMB = 950 // 95%
	})
	if _, err := env.alerts.Create(services.AlertRuleInput{
		Name: "Disk 90%", Type: domain.AlertTypeDiskCapacity, Enabled: true,
		ThresholdPercent: 90, Severity: domain.AlertSeverityWarning,
		CooldownMinutes: 360, // explizite Sperrfrist (0 = keine, R2-063)
	}, "admin"); err != nil {
		t.Fatal(err)
	}

	if _, err := env.alerts.Evaluate("test"); err != nil {
		t.Fatal(err)
	}
	events, _ := env.alerts.ListEvents(10)
	if len(events) != 1 {
		t.Fatalf("erwartete 1 Alarm-Event, bekam %d", len(events))
	}
	// Zweite Auswertung innerhalb des Cooldowns erzeugt keinen zweiten Event,
	// weist ihn aber als „unterdrückt" aus.
	summary, err := env.alerts.Evaluate("test")
	if err != nil {
		t.Fatal(err)
	}
	events, _ = env.alerts.ListEvents(10)
	if len(events) != 1 {
		t.Errorf("Cooldown verletzt: erwartete 1 Event, bekam %d", len(events))
	}
	if !strings.Contains(summary, "unterdrückt") {
		t.Errorf("die Zusammenfassung soll unterdrückte Alarme ausweisen (R2-063): %q", summary)
	}
}

// TestAlertCooldownNullKeineSperre (R2-063): cooldown_minutes = 0 heißt
// KEINE Sperre - jede Auswertung meldet den fortbestehenden Zustand erneut.
func TestAlertCooldownNullKeineSperre(t *testing.T) {
	env := newAlertEnv(t)
	env.createServer(t, "db-02", func(s *domain.Server) {
		s.DiskTotalMB = 1000
		s.DiskUsedMB = 950
	})
	if _, err := env.alerts.Create(services.AlertRuleInput{
		Name: "Disk sofort", Type: domain.AlertTypeDiskCapacity, Enabled: true,
		ThresholdPercent: 90, Severity: domain.AlertSeverityWarning,
		CooldownMinutes: 0,
	}, "admin"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := env.alerts.Evaluate("test"); err != nil {
			t.Fatal(err)
		}
	}
	if events, _ := env.alerts.ListEvents(10); len(events) != 3 {
		t.Errorf("ohne Sperre muss jede Auswertung feuern: erwartete 3, bekam %d", len(events))
	}
}

func TestAlertDiskCapacityBelowThresholdDoesNotFire(t *testing.T) {
	env := newAlertEnv(t)
	env.createServer(t, "web-01", func(s *domain.Server) {
		s.DiskTotalMB = 1000
		s.DiskUsedMB = 500 // 50%
	})
	if _, err := env.alerts.Create(services.AlertRuleInput{
		Name: "Disk 90%", Type: domain.AlertTypeDiskCapacity, Enabled: true, ThresholdPercent: 90,
	}, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := env.alerts.Evaluate("test"); err != nil {
		t.Fatal(err)
	}
	events, _ := env.alerts.ListEvents(10)
	if len(events) != 0 {
		t.Errorf("erwartete keinen Alarm, bekam %d", len(events))
	}
}

func TestAlertRebootRequiredFires(t *testing.T) {
	env := newAlertEnv(t)
	env.createServer(t, "ubuntu-01", func(s *domain.Server) {
		s.RebootRequired = true
	})
	env.createServer(t, "clean-01", nil) // kein Neustart nötig → kein Event
	if _, err := env.alerts.Create(services.AlertRuleInput{
		Name: "Neustart nötig", Type: domain.AlertTypeRebootRequired, Enabled: true,
		Severity: domain.AlertSeverityWarning,
	}, "admin"); err != nil {
		t.Fatal(err)
	}

	if _, err := env.alerts.Evaluate("test"); err != nil {
		t.Fatal(err)
	}
	events, _ := env.alerts.ListEvents(10)
	if len(events) != 1 {
		t.Fatalf("erwartete 1 Alarm-Event, bekam %d", len(events))
	}
	if events[0].ServerID == nil || *events[0].ServerID != 1 {
		t.Errorf("Event sollte auf ubuntu-01 (ID 1) zeigen, bekam %v", events[0].ServerID)
	}
}

// --- Alert-Service: apt_cacher_down ------------------------------------------

func TestAlertAptCacherDownFiresWhenUnreachable(t *testing.T) {
	env := newAlertEnv(t)
	env.createServer(t, "web-01", nil)
	env.createServer(t, "web-02", nil)
	env.alerts.WithAptCacheChecker(func() (string, error) { return "http://127.0.0.1:1", nil })

	if _, err := env.alerts.Create(services.AlertRuleInput{
		Name: "APT-Cache tot", Type: domain.AlertTypeAptCacherDown, Enabled: true,
		Severity: domain.AlertSeverityCritical,
	}, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := env.alerts.Evaluate("test"); err != nil {
		t.Fatal(err)
	}
	events, _ := env.alerts.ListEvents(10)
	// Ein Dienst, ein Alarm - auch bei zwei Servern im Bestand.
	if len(events) != 1 {
		t.Fatalf("erwartete 1 Alarm-Event, bekam %d", len(events))
	}
	if events[0].ServerID != nil {
		t.Errorf("Selbstbeobachtung darf auf keinen Server zeigen, bekam %v", *events[0].ServerID)
	}
}

func TestAlertAptCacherDownDoesNotFireWhenRunning(t *testing.T) {
	env := newAlertEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// Report-Seite eines echten apt-cacher-ng - die gehärtete Prüfung verlangt
		// den Inhalts-Marker, nicht nur HTTP 200.
		_, _ = w.Write([]byte("<html><head><title>Apt-Cacher NG</title></head><body>ok</body></html>"))
	}))
	defer srv.Close()
	env.alerts.WithAptCacheChecker(func() (string, error) { return srv.URL, nil })

	if _, err := env.alerts.Create(services.AlertRuleInput{
		Name: "APT-Cache tot", Type: domain.AlertTypeAptCacherDown, Enabled: true,
	}, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := env.alerts.Evaluate("test"); err != nil {
		t.Fatal(err)
	}
	events, _ := env.alerts.ListEvents(10)
	if len(events) != 0 {
		t.Errorf("erwartete keinen Alarm bei laufendem Dienst, bekam %d", len(events))
	}
}

func TestAlertAptCacherDownSkipsWithoutConfiguredURL(t *testing.T) {
	env := newAlertEnv(t)
	env.alerts.WithAptCacheChecker(func() (string, error) { return "", nil })

	if _, err := env.alerts.Create(services.AlertRuleInput{
		Name: "APT-Cache tot", Type: domain.AlertTypeAptCacherDown, Enabled: true,
	}, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := env.alerts.Evaluate("test"); err != nil {
		t.Fatal(err)
	}
	events, _ := env.alerts.ListEvents(10)
	if len(events) != 0 {
		t.Errorf("ohne konfigurierte URL sollte kein Alarm ausgelöst werden, bekam %d", len(events))
	}
}

func TestAlertHeartbeatFiresForStaleServer(t *testing.T) {
	env := newAlertEnv(t)
	stale := time.Now().Add(-48 * time.Hour)
	env.createServer(t, "old-01", func(s *domain.Server) {
		s.LastSeenAt = &stale
	})
	env.createServer(t, "fresh-01", func(s *domain.Server) {
		now := time.Now()
		s.LastSeenAt = &now
	})
	if _, err := env.alerts.Create(services.AlertRuleInput{
		Name: "Heartbeat 24h", Type: domain.AlertTypeHeartbeat, Enabled: true,
		HeartbeatHours: 24, Severity: domain.AlertSeverityCritical,
	}, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := env.alerts.Evaluate("test"); err != nil {
		t.Fatal(err)
	}
	events, _ := env.alerts.ListEvents(10)
	if len(events) != 1 {
		t.Fatalf("erwartete 1 Heartbeat-Alarm, bekam %d", len(events))
	}
	if events[0].ServerName != "old-01" {
		t.Errorf("falscher Server im Alarm: %q", events[0].ServerName)
	}
}

func TestAlertGroupScopedRuleOnlyChecksGroupServers(t *testing.T) {
	env := newAlertEnv(t)
	inGroup := env.createServer(t, "prod-db", func(s *domain.Server) {
		s.DiskTotalMB = 1000
		s.DiskUsedMB = 990
	})
	env.createServer(t, "staging-db", func(s *domain.Server) {
		s.DiskTotalMB = 1000
		s.DiskUsedMB = 990
	})
	group := &domain.ServerGroup{Name: "Produktion"}
	if err := env.groups.Create(group); err != nil {
		t.Fatal(err)
	}
	if err := env.groups.AddServer(group, inGroup); err != nil {
		t.Fatal(err)
	}
	if _, err := env.alerts.Create(services.AlertRuleInput{
		Name: "Prod Disk", Type: domain.AlertTypeDiskCapacity, Enabled: true,
		GroupIDs: []uint{group.ID}, ThresholdPercent: 90,
	}, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := env.alerts.Evaluate("test"); err != nil {
		t.Fatal(err)
	}
	events, _ := env.alerts.ListEvents(10)
	if len(events) != 1 {
		t.Fatalf("erwartete genau 1 Alarm (nur Gruppen-Server), bekam %d", len(events))
	}
	if events[0].ServerName != "prod-db" {
		t.Errorf("Alarm für falschen Server: %q", events[0].ServerName)
	}
}

func TestAlertDisabledRuleIsSkipped(t *testing.T) {
	env := newAlertEnv(t)
	env.createServer(t, "db-01", func(s *domain.Server) {
		s.DiskTotalMB = 1000
		s.DiskUsedMB = 999
	})
	if _, err := env.alerts.Create(services.AlertRuleInput{
		Name: "Disk", Type: domain.AlertTypeDiskCapacity, Enabled: false, ThresholdPercent: 90,
	}, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := env.alerts.Evaluate("test"); err != nil {
		t.Fatal(err)
	}
	events, _ := env.alerts.ListEvents(10)
	if len(events) != 0 {
		t.Errorf("deaktivierte Regel sollte nicht auslösen, bekam %d Events", len(events))
	}
}

// --- Alert-Service: crowdsec_lapi_down ---------------------------------------

func TestAlertCrowdSecLapiDownFiresWhenUnreachable(t *testing.T) {
	env := newAlertEnv(t)
	env.createServer(t, "web-01", nil)
	env.createServer(t, "web-02", nil)
	env.alerts.WithCrowdSecLapiChecker(func() (*services.CrowdSecLapiStatus, error) {
		return &services.CrowdSecLapiStatus{Configured: true, Message: "nicht erreichbar"}, nil
	})

	if _, err := env.alerts.Create(services.AlertRuleInput{
		Name: "LAPI tot", Type: domain.AlertTypeCrowdSecLapiDown, Enabled: true,
		Severity: domain.AlertSeverityCritical,
	}, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := env.alerts.Evaluate("test"); err != nil {
		t.Fatal(err)
	}
	events, _ := env.alerts.ListEvents(10)
	if len(events) != 1 {
		t.Fatalf("erwartete 1 Alarm-Event, bekam %d", len(events))
	}
	if events[0].ServerID != nil {
		t.Errorf("Selbstbeobachtung darf auf keinen Server zeigen, bekam %v", *events[0].ServerID)
	}
}

func TestAlertCrowdSecLapiDownDoesNotFireWhenRunning(t *testing.T) {
	env := newAlertEnv(t)
	env.alerts.WithCrowdSecLapiChecker(func() (*services.CrowdSecLapiStatus, error) {
		return &services.CrowdSecLapiStatus{Configured: true, Reachable: true, Running: true}, nil
	})

	if _, err := env.alerts.Create(services.AlertRuleInput{
		Name: "LAPI tot", Type: domain.AlertTypeCrowdSecLapiDown, Enabled: true,
	}, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := env.alerts.Evaluate("test"); err != nil {
		t.Fatal(err)
	}
	events, _ := env.alerts.ListEvents(10)
	if len(events) != 0 {
		t.Errorf("erwartete keinen Alarm bei erreichbarer LAPI, bekam %d", len(events))
	}
}

func TestAlertCrowdSecLapiDownSkipsWithoutConfig(t *testing.T) {
	env := newAlertEnv(t)
	env.alerts.WithCrowdSecLapiChecker(func() (*services.CrowdSecLapiStatus, error) {
		return &services.CrowdSecLapiStatus{Configured: false, Message: "keine CrowdSec-LAPI konfiguriert"}, nil
	})

	if _, err := env.alerts.Create(services.AlertRuleInput{
		Name: "LAPI tot", Type: domain.AlertTypeCrowdSecLapiDown, Enabled: true,
	}, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := env.alerts.Evaluate("test"); err != nil {
		t.Fatal(err)
	}
	events, _ := env.alerts.ListEvents(10)
	if len(events) != 0 {
		t.Errorf("ohne konfigurierte LAPI sollte kein Alarm ausgelöst werden, bekam %d", len(events))
	}
}

// cveDB baut einen Datenbank-Stand mit dem gewuenschten Alter (bereits
// bewertet, wie ihn der ServerService liefert).
func cveDB(age time.Duration) domain.CVEDBStatus {
	updated := time.Now().Add(-age)
	st := domain.CVEDBStatus{Available: true, Version: "0.72.0", UpdatedAt: &updated}
	st.EvaluateCVEDB(time.Now())
	return st
}

// TestAlertCVEDBStaleFeuertGenauEinmal: Scanner und Datenbank gibt es einmal.
// Wuerde die Regel je Server feuern, kaeme EINE Ursache als vielfacher Alarm
// an - deshalb zaehlt der Test die Server bewusst hoch.
func TestAlertCVEDBStaleFeuertGenauEinmal(t *testing.T) {
	env := newAlertEnv(t)
	for _, name := range []string{"web-01", "web-02", "web-03"} {
		env.createServer(t, name, nil)
	}
	env.alerts.WithCVEDBChecker(func() domain.CVEDBStatus { return cveDB(10 * 24 * time.Hour) })

	if _, err := env.alerts.Create(services.AlertRuleInput{
		Name: "CVE-Datenbank veraltet", Type: domain.AlertTypeCVEDBStale, Enabled: true,
		Severity: domain.AlertSeverityWarning,
	}, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := env.alerts.Evaluate("test"); err != nil {
		t.Fatal(err)
	}
	events, _ := env.alerts.ListEvents(10)
	if len(events) != 1 {
		t.Fatalf("erwartete genau 1 Alarm-Event, bekam %d", len(events))
	}
	if events[0].ServerID != nil {
		t.Errorf("Selbstbeobachtung darf auf keinen Server zeigen, bekam %v", *events[0].ServerID)
	}
}

// TestAlertCVEDBStaleSchweigtBeiAktuellerDatenbank: Ein Dauer-Alarm wuerde
// abstumpfen - bei frischer Datenbank muss es still bleiben.
func TestAlertCVEDBStaleSchweigtBeiAktuellerDatenbank(t *testing.T) {
	env := newAlertEnv(t)
	env.alerts.WithCVEDBChecker(func() domain.CVEDBStatus { return cveDB(3 * time.Hour) })

	if _, err := env.alerts.Create(services.AlertRuleInput{
		Name: "CVE-Datenbank veraltet", Type: domain.AlertTypeCVEDBStale, Enabled: true,
	}, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := env.alerts.Evaluate("test"); err != nil {
		t.Fatal(err)
	}
	events, _ := env.alerts.ListEvents(10)
	if len(events) != 0 {
		t.Errorf("bei aktueller Datenbank sollte kein Alarm feuern, bekam %d", len(events))
	}
}

// TestAlertCVEDBStaleSchweigtOhneScanner: Ohne installiertes Trivy ist das
// Feature nicht in Benutzung - ein Alarm, den niemand abstellen kann, waere
// nur Laerm.
func TestAlertCVEDBStaleSchweigtOhneScanner(t *testing.T) {
	env := newAlertEnv(t)
	env.alerts.WithCVEDBChecker(func() domain.CVEDBStatus {
		return domain.CVEDBStatus{Available: false, Freshness: domain.CVEDBUnknown}
	})

	if _, err := env.alerts.Create(services.AlertRuleInput{
		Name: "CVE-Datenbank veraltet", Type: domain.AlertTypeCVEDBStale, Enabled: true,
	}, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := env.alerts.Evaluate("test"); err != nil {
		t.Fatal(err)
	}
	events, _ := env.alerts.ListEvents(10)
	if len(events) != 0 {
		t.Errorf("ohne Scanner sollte kein Alarm feuern, bekam %d", len(events))
	}
}

// TestAlertCVEDBNiemalsGeladenFeuert: Der schlimmere Fall als „alt" - dann hat
// noch kein Scan echte Daten gesehen.
func TestAlertCVEDBNiemalsGeladenFeuert(t *testing.T) {
	env := newAlertEnv(t)
	env.alerts.WithCVEDBChecker(func() domain.CVEDBStatus {
		return domain.CVEDBStatus{Available: true, Version: "0.72.0", Freshness: domain.CVEDBUnknown,
			Error: "keine Schwachstellen-Datenbank vorhanden"}
	})

	if _, err := env.alerts.Create(services.AlertRuleInput{
		Name: "CVE-Datenbank veraltet", Type: domain.AlertTypeCVEDBStale, Enabled: true,
	}, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := env.alerts.Evaluate("test"); err != nil {
		t.Fatal(err)
	}
	events, _ := env.alerts.ListEvents(10)
	if len(events) != 1 {
		t.Fatalf("nie geladene Datenbank sollte einen Alarm ausloesen, bekam %d", len(events))
	}
}

// TestAlertRuleGruppenAenderbar (R2-030): Die Gruppenzuordnung einer
// Alarmregel wurde beim PATCH still verworfen - die Regel überwachte
// weiter die alte Gruppe. Nachweis, dass die Zuordnung übernommen wird:
// setzen, auf mehrere Gruppen erweitern und wieder lösen.
func TestAlertRuleGruppenAenderbar(t *testing.T) {
	env := newAlertEnv(t)
	grpA := &domain.ServerGroup{Name: "alert-a"}
	grpB := &domain.ServerGroup{Name: "alert-b"}
	if err := env.groups.Create(grpA); err != nil {
		t.Fatal(err)
	}
	if err := env.groups.Create(grpB); err != nil {
		t.Fatal(err)
	}
	g1 := grpA.ID
	g2 := grpB.ID
	rule, err := env.alerts.Create(services.AlertRuleInput{
		Name: "R2-030", Type: "disk_capacity", Enabled: true, GroupIDs: []uint{g1}, ThresholdPercent: 90,
	}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if len(rule.Groups) != 1 || rule.Groups[0].ID != g1 {
		t.Fatalf("Anlage: falsche Gruppe: %+v", rule.Groups)
	}

	// Umhängen auf Gruppe 2 - genau der Befund-Fall.
	updated, err := env.alerts.Update(rule.ID, services.AlertRuleInput{
		Name: "R2-030", Type: "disk_capacity", Enabled: true, GroupIDs: []uint{g2}, ThresholdPercent: 90,
	}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Groups) != 1 || updated.Groups[0].ID != g2 {
		t.Fatalf("Gruppe 1→2 nicht übernommen: %+v", updated.Groups)
	}

	// Beide Gruppen zugleich - dafür brauchte es vorher zwei Regeln.
	updated, err = env.alerts.Update(rule.ID, services.AlertRuleInput{
		Name: "R2-030", Type: "disk_capacity", Enabled: true, GroupIDs: []uint{g1, g2}, ThresholdPercent: 90,
	}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Groups) != 2 {
		t.Fatalf("Mehrfachauswahl nicht übernommen: %+v", updated.Groups)
	}

	// Und lösen (leer = alle Server).
	updated, err = env.alerts.Update(rule.ID, services.AlertRuleInput{
		Name: "R2-030", Type: "disk_capacity", Enabled: true, ThresholdPercent: 90,
	}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Groups) != 0 {
		t.Fatalf("Zuordnung sollte lösbar sein, ist %+v", updated.Groups)
	}

	// Unbekannte Gruppe: klare Ablehnung statt stiller Ausweitung auf alle Server.
	if _, err := env.alerts.Update(rule.ID, services.AlertRuleInput{
		Name: "R2-030", Type: "disk_capacity", Enabled: true, GroupIDs: []uint{9999}, ThresholdPercent: 90,
	}, "admin"); !errors.Is(err, services.ErrAlertGroupUnknown) {
		t.Fatalf("unbekannte Gruppe muss abgelehnt werden, bekam: %v", err)
	}
}

// TestAlertRuleMitGruppeLoeschbar: Die Verknüpfungstabelle der
// Gruppenzuordnung hat einen Fremdschlüssel auf die Regel - ohne vorheriges
// Lösen scheiterte das Löschen jeder zugeordneten Regel mit einem
// Datenbankfehler (500), während die Oberfläche nur „interner Serverfehler"
// zeigte.
func TestAlertRuleMitGruppeLoeschbar(t *testing.T) {
	env := newAlertEnv(t)
	group := &domain.ServerGroup{Name: "loeschtest"}
	if err := env.groups.Create(group); err != nil {
		t.Fatal(err)
	}
	rule, err := env.alerts.Create(services.AlertRuleInput{
		Name: "mit Gruppe", Type: domain.AlertTypeDiskCapacity, Enabled: true,
		GroupIDs: []uint{group.ID}, ThresholdPercent: 90,
	}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := env.alerts.Delete(rule.ID, "admin"); err != nil {
		t.Fatalf("Regel mit Gruppe nicht löschbar: %v", err)
	}
	rules, err := env.alerts.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rules {
		if r.ID == rule.ID {
			t.Fatal("Regel steht nach dem Löschen noch in der Liste")
		}
	}
}

// TestAlertBackupStale: Im Langzeittest fehlte das Backup wochenlang, ohne
// dass irgendein Kanal es meldete (R2-027/028/034). Der Alarm misst deshalb
// das ALTER des juengsten Backups - egal, auf welchem Weg es ausblieb.
// Kulanz ein Intervall: Alarm erst ab dem doppelten Intervall-Alter.
func TestAlertBackupStale(t *testing.T) {
	env := newAlertEnv(t)
	env.createServer(t, "web-01", nil)

	mkRule := func() {
		if _, err := env.alerts.Create(services.AlertRuleInput{
			Name: "Backup ueberfaellig", Type: domain.AlertTypeBackupStale, Enabled: true,
		}, "admin"); err != nil {
			t.Fatal(err)
		}
	}
	evaluate := func() []domain.AlertEvent {
		t.Helper()
		if _, err := env.alerts.Evaluate("test"); err != nil {
			t.Fatal(err)
		}
		events, _ := env.alerts.ListEvents(10)
		return events
	}

	// Noch nie ein Backup, Backups aktiviert → genau ein Alarm, ohne Serverbezug.
	env.alerts.WithBackupStatus(func() (bool, int, *time.Time, error) {
		return true, 24, nil, nil
	})
	mkRule()
	events := evaluate()
	if len(events) != 1 || events[0].ServerID != nil {
		t.Fatalf("erwartete genau 1 Alarm ohne Serverbezug, bekam %d: %+v", len(events), events)
	}

	// Juengstes Backup 30 h alt bei 24-h-Intervall: innerhalb der Kulanz -
	// der Nachhol-Watchdog bekommt seine Chance, kein Alarm.
	env2 := newAlertEnv(t)
	fresh := time.Now().Add(-30 * time.Hour)
	env2.alerts.WithBackupStatus(func() (bool, int, *time.Time, error) {
		return true, 24, &fresh, nil
	})
	if _, err := env2.alerts.Create(services.AlertRuleInput{
		Name: "Backup ueberfaellig", Type: domain.AlertTypeBackupStale, Enabled: true,
	}, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := env2.alerts.Evaluate("test"); err != nil {
		t.Fatal(err)
	}
	if ev, _ := env2.alerts.ListEvents(10); len(ev) != 0 {
		t.Errorf("30 h bei 24-h-Intervall liegt in der Kulanz - kein Alarm erwartet, bekam %d", len(ev))
	}

	// 50 h alt (mehr als das doppelte Intervall) → Alarm mit Altersangabe.
	old := time.Now().Add(-50 * time.Hour)
	env2.alerts.WithBackupStatus(func() (bool, int, *time.Time, error) {
		return true, 24, &old, nil
	})
	if _, err := env2.alerts.Evaluate("test"); err != nil {
		t.Fatal(err)
	}
	ev, _ := env2.alerts.ListEvents(10)
	if len(ev) != 1 {
		t.Fatalf("50 h bei 24-h-Intervall muss feuern, bekam %d Events", len(ev))
	}
	if ev[0].Description != "System-Backup überfällig" {
		t.Errorf("unerwartete Beschreibung: %q", ev[0].Description)
	}

	// Abgeschaltete Backups sind eine Entscheidung, kein Vorfall.
	env3 := newAlertEnv(t)
	env3.alerts.WithBackupStatus(func() (bool, int, *time.Time, error) {
		return false, 24, nil, nil
	})
	if _, err := env3.alerts.Create(services.AlertRuleInput{
		Name: "Backup ueberfaellig", Type: domain.AlertTypeBackupStale, Enabled: true,
	}, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := env3.alerts.Evaluate("test"); err != nil {
		t.Fatal(err)
	}
	if ev, _ := env3.alerts.ListEvents(10); len(ev) != 0 {
		t.Errorf("bei deaktivierten Backups darf kein Alarm feuern, bekam %d", len(ev))
	}
}

// --- Alert-Service: Selbstbeobachtung ohne Servereintrag ---------------------

// TestSelbstalarmFeuertOhneServerbestand haelt den Fehler fest, der diesen
// Umbau ausgeloest hat: Backup-Stand, CVE-Datenbank, Paket-Cache und LAPI
// beschreiben LCM SELBST - bis v1.23 hingen sie aber an einem Servereintrag
// fuer den eigenen Rechner (`if !server.IsLcmHost()`).
//
// Im Container nimmt LCM sich bewusst nicht selbst auf (self_register.go:
// localhost waere der Container, nicht der Host). Damit lief die Bewertung
// fuer JEDEN Server ins Leere: keine Events, keine Benachrichtigung, und in
// der Oberflaeche sah das aus wie „alles in Ordnung". Ein wochenlang
// ausbleibendes Backup blieb so unbemerkt - dasselbe Fehlerbild wie
// R2-027/028/034, nur diesmal strukturell.
//
// Der Test faehrt deshalb den haertesten Fall: eine voellig leere
// Server-Tabelle.
func TestSelbstalarmFeuertOhneServerbestand(t *testing.T) {
	env := newAlertEnv(t)
	env.alerts.WithBackupStatus(func() (bool, int, *time.Time, error) {
		return true, 24, nil, nil // aktiviert, aber noch nie gelaufen
	})
	if _, err := env.alerts.Create(services.AlertRuleInput{
		Name: "Backup ueberfaellig", Type: domain.AlertTypeBackupStale, Enabled: true,
	}, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := env.alerts.Evaluate("test"); err != nil {
		t.Fatal(err)
	}
	events, _ := env.alerts.ListEvents(10)
	if len(events) != 1 {
		t.Fatalf("ohne Servereintrag muss die Selbstbeobachtung trotzdem feuern, bekam %d Events", len(events))
	}
	if events[0].ServerID != nil {
		t.Errorf("Ereignis darf auf keinen Server zeigen, bekam %v", *events[0].ServerID)
	}
	if events[0].ServerName == "" {
		t.Error("die Spalte braucht einen lesbaren Namen - sonst steht dort nur ein Strich")
	}
}

// TestSelbstalarmIgnoriertGruppen: Servergruppen sind fuer Selbstbeobachtung
// gegenstandslos - es gibt nichts einzugrenzen, wenn das Geprueffte nur einmal
// existiert. Wichtiger als die Kosmetik ist die Wirkung: Bliebe die Gruppe
// gespeichert, haenge die Regel wieder an Servereintraegen und waere damit
// erneut abschaltbar, indem man einen unbeteiligten Datensatz loescht.
func TestSelbstalarmIgnoriertGruppen(t *testing.T) {
	env := newAlertEnv(t)
	group := &domain.ServerGroup{Name: "Produktion"}
	if err := env.groups.Create(group); err != nil {
		t.Fatal(err)
	}
	env.alerts.WithBackupStatus(func() (bool, int, *time.Time, error) {
		return true, 24, nil, nil
	})
	rule, err := env.alerts.Create(services.AlertRuleInput{
		Name: "Backup ueberfaellig", Type: domain.AlertTypeBackupStale, Enabled: true,
		GroupIDs: []uint{group.ID},
	}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if len(rule.Groups) != 0 {
		t.Errorf("Selbstbeobachtung darf keine Gruppen speichern, bekam %d", len(rule.Groups))
	}
	if _, err := env.alerts.Evaluate("test"); err != nil {
		t.Fatal(err)
	}
	if events, _ := env.alerts.ListEvents(10); len(events) != 1 {
		t.Fatalf("die Regel muss trotz gewaehlter Gruppe feuern, bekam %d Events", len(events))
	}
}

// TestSelbstalarmSperrfrist: Die Entprellung muss auch ohne Serverbezug
// greifen. Sie schlaegt im Repository ueber `server_id IS NULL` nach - ein
// Pfad, den vorher nichts benutzt hat.
func TestSelbstalarmSperrfrist(t *testing.T) {
	env := newAlertEnv(t)
	env.alerts.WithBackupStatus(func() (bool, int, *time.Time, error) {
		return true, 24, nil, nil
	})
	if _, err := env.alerts.Create(services.AlertRuleInput{
		Name: "Backup ueberfaellig", Type: domain.AlertTypeBackupStale, Enabled: true,
		CooldownMinutes: 360,
	}, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := env.alerts.Evaluate("test"); err != nil {
		t.Fatal(err)
	}
	summary, err := env.alerts.Evaluate("test")
	if err != nil {
		t.Fatal(err)
	}
	if events, _ := env.alerts.ListEvents(10); len(events) != 1 {
		t.Errorf("Sperrfrist verletzt: erwartete 1 Event, bekam %d", len(events))
	}
	if !strings.Contains(summary, "unterdrückt") {
		t.Errorf("unterdrueckte Alarme gehoeren ins Lagebild (R2-063): %q", summary)
	}
}
