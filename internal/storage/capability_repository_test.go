package storage

import (
	"strings"
	"testing"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

func TestCapabilitySaveIstUpsert(t *testing.T) {
	db, _ := setupEncDB(t)
	repo := repositories.NewCapabilityRepository(db)
	since := time.Date(2026, 10, 1, 4, 0, 0, 0, time.UTC)
	ruleID := uint(42)

	first := &domain.ServerCapability{
		ServerID: 7, Category: domain.CapabilityPackages, State: domain.CapabilityFailed,
		FailingSince: &since, ConsecutiveFailures: 1, LastErrorClass: domain.ErrClassLockBusy,
		LastErrorMessage: "PID 1234 (unattended-upgr)",
		LastErrorParams:  map[string]string{"pid": "1234", "process": "unattended-upgr"},
		LastJobID:        "job-1", LastJobType: domain.RuleTypeUpdate, LastJobName: "Updates", LastRuleID: &ruleID,
	}
	if err := repo.Save(first); err != nil {
		t.Fatal(err)
	}

	// Zweiter Fehlschlag: dieselbe Zeile, neue Werte - auch aus einem geladenen
	// Objekt mit ID, wie der Jobablauf es speichern wird.
	loaded, err := repo.Find(7, domain.CapabilityPackages)
	if err != nil || loaded == nil {
		t.Fatalf("Find: %v / %v", loaded, err)
	}
	created := loaded.CreatedAt
	loaded.ConsecutiveFailures = 2
	loaded.LastErrorMessage = "PID 1234 (unattended-upgr) hält die Sperre"
	loaded.LastErrorParams = nil
	if err := repo.Save(loaded); err != nil {
		t.Fatal(err)
	}

	rows, err := repo.FindForServer(7)
	if err != nil || len(rows) != 1 {
		t.Fatalf("FindForServer: %d Zeilen, %v", len(rows), err)
	}
	got := rows[0]
	if got.ConsecutiveFailures != 2 || got.LastErrorMessage != loaded.LastErrorMessage {
		t.Errorf("Update nicht übernommen: %+v", got)
	}
	if got.LastErrorParams != nil {
		t.Errorf("Parameter müssen mit dem Update verschwinden, sind %v", got.LastErrorParams)
	}
	if !got.CreatedAt.Equal(created) {
		t.Errorf("CreatedAt hat sich geändert: %v -> %v", created, got.CreatedAt)
	}
	if got.FailingSince == nil || !got.FailingSince.Equal(since) {
		t.Errorf("FailingSince = %v, erwartet %v", got.FailingSince, since)
	}
	if got.LastJobType != domain.RuleTypeUpdate {
		t.Errorf("LastJobType = %q", got.LastJobType)
	}
	if got.LastRuleID == nil || *got.LastRuleID != ruleID {
		t.Errorf("LastRuleID = %v, erwartet %d", got.LastRuleID, ruleID)
	}
}

func TestCapabilityParameterUndTexteLiegenVerschluesselt(t *testing.T) {
	db, _ := setupEncDB(t)
	repo := repositories.NewCapabilityRepository(db)
	c := &domain.ServerCapability{
		ServerID: 1, Category: domain.CapabilityPackages, State: domain.CapabilityFailed,
		LastErrorClass:   domain.ErrClassRepoError,
		LastErrorMessage: "Err:3 https://intern.example.org/repo jammy InRelease",
		LastErrorParams:  map[string]string{"repo": "https://intern.example.org/repo"},
		WarningMessage:   "W: Quelle intern.example.org",
	}
	if err := repo.Save(c); err != nil {
		t.Fatal(err)
	}

	var id uint
	if err := db.Raw("SELECT id FROM server_capabilities").Scan(&id).Error; err != nil {
		t.Fatal(err)
	}
	for _, col := range []string{"last_error_message", "last_error_params", "warning_message"} {
		raw := rawCol(t, db, "server_capabilities", col, id)
		if raw == "" || strings.Contains(raw, "intern.example.org") {
			t.Errorf("server_capabilities.%s ist nicht verschlüsselt: %q", col, raw)
		}
	}

	got, err := repo.Find(1, domain.CapabilityPackages)
	if err != nil || got == nil {
		t.Fatalf("Find: %v / %v", got, err)
	}
	if got.LastErrorMessage != c.LastErrorMessage || got.WarningMessage != c.WarningMessage {
		t.Errorf("Texte nach dem Rücklesen verändert: %+v", got)
	}
	if got.LastErrorParams["repo"] != "https://intern.example.org/repo" {
		t.Errorf("Parameter = %v", got.LastErrorParams)
	}
}

func TestCapabilityFindAbsentIstNil(t *testing.T) {
	db, _ := setupEncDB(t)
	repo := repositories.NewCapabilityRepository(db)
	got, err := repo.Find(99, domain.CapabilityRules)
	if got != nil || err != nil {
		t.Errorf("Find = %v, %v; erwartet nil, nil", got, err)
	}
}

func TestCapabilityFindForServersTrenntNachServer(t *testing.T) {
	db, _ := setupEncDB(t)
	repo := repositories.NewCapabilityRepository(db)
	for _, c := range []domain.ServerCapability{
		{ServerID: 1, Category: domain.CapabilityPackages, State: domain.CapabilityOK},
		{ServerID: 1, Category: domain.CapabilityRules, State: domain.CapabilityFailed},
		{ServerID: 2, Category: domain.CapabilityPackages, State: domain.CapabilityFailed},
		{ServerID: 3, Category: domain.CapabilityPackages, State: domain.CapabilityOK},
	} {
		if err := repo.Save(&c); err != nil {
			t.Fatal(err)
		}
	}

	got, err := repo.FindForServers([]uint{1, 2, 42})
	if err != nil {
		t.Fatal(err)
	}
	if len(got[1]) != 2 || len(got[2]) != 1 {
		t.Errorf("Server 1/2: %d/%d Zeilen, erwartet 2/1", len(got[1]), len(got[2]))
	}
	if _, ok := got[3]; ok {
		t.Error("Server 3 wurde nicht angefragt, steht aber im Ergebnis")
	}
	if _, ok := got[42]; ok {
		t.Error("Server ohne Zeilen darf nicht im Ergebnis stehen")
	}
	if got[2][0].State != domain.CapabilityFailed {
		t.Errorf("Zeile des Servers 2: %+v", got[2][0])
	}
}

// TestCapabilityFindForServersBlockweise: mehr IDs als SQLite Variablen je
// Anweisung zulässt, ohne dass Zeilen verloren gehen.
func TestCapabilityFindForServersBlockweise(t *testing.T) {
	db, _ := setupEncDB(t)
	repo := repositories.NewCapabilityRepository(db)
	ids := make([]uint, 0, 1200)
	for id := uint(1); id <= 1200; id++ {
		ids = append(ids, id)
	}
	for _, id := range []uint{1, 600, 1200} {
		if err := repo.Save(&domain.ServerCapability{ServerID: id, Category: domain.CapabilityPackages, State: domain.CapabilityOK}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := repo.FindForServers(ids)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Errorf("%d Server im Ergebnis, erwartet 3", len(got))
	}
}

// TestServerLoeschenRaeumtBetriebsfaehigkeitAuf: Der Zustand eines gelöschten
// Servers bliebe sonst verwaist liegen - und eine neu vergebene ID erbte ihn.
func TestServerLoeschenRaeumtBetriebsfaehigkeitAuf(t *testing.T) {
	db, _ := setupEncDB(t)
	servers := repositories.NewServerRepository(db)
	caps := repositories.NewCapabilityRepository(db)
	s := &domain.Server{Name: "web01", Host: "10.0.0.1"}
	if err := db.Create(s).Error; err != nil {
		t.Fatal(err)
	}
	if err := caps.Save(&domain.ServerCapability{ServerID: s.ID, Category: domain.CapabilityPackages, State: domain.CapabilityFailed}); err != nil {
		t.Fatal(err)
	}
	if err := servers.Delete(s.ID); err != nil {
		t.Fatal(err)
	}
	if rows, _ := caps.FindForServer(s.ID); len(rows) != 0 {
		t.Errorf("%d Zeilen nach dem Löschen des Servers", len(rows))
	}
}
