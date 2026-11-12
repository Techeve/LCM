package repositories_test

import (
	"testing"

	"gorm.io/gorm"

	"LCM/internal/core/domain"
	"LCM/internal/storage"
	"LCM/internal/storage/repositories"
)

// migrierteDB liefert eine vollständig migrierte In-Memory-DB. storage.Open
// schaltet die Fremdschlüssel ein wie im Betrieb - sonst fiele kein Fehler
// auf, den die Produktion mit einem 500 quittiert.
func migrierteDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.Migrate(db); err != nil {
		t.Fatal(err)
	}
	var fk int
	if err := db.Raw("PRAGMA foreign_keys").Scan(&fk).Error; err != nil || fk != 1 {
		t.Fatalf("Fremdschlüssel sind im Test aus (%d, %v) - der Test bewiese nichts", fk, err)
	}
	return db
}

func gruppe(t *testing.T, db *gorm.DB, name string) *domain.ServerGroup {
	t.Helper()
	g := &domain.ServerGroup{Name: name}
	if err := db.Create(g).Error; err != nil {
		t.Fatal(err)
	}
	return g
}

// Ein Benutzer, der eine Gruppe betreut, ließ sich nicht löschen: Die Zeile
// in server_group_managers hielt ihren Fremdschlüssel auf ihn, das Löschen
// endete mit einem 500.
func TestBenutzerMitBetreutenGruppenLaesstSichLoeschen(t *testing.T) {
	db := migrierteDB(t)
	users := repositories.NewUserRepository(db)
	groups := repositories.NewGroupRepository(db)

	u := &domain.User{Username: "manager1", PasswordHash: "h"}
	if err := users.Create(u); err != nil {
		t.Fatal(err)
	}
	g := gruppe(t, db, "web")
	if err := groups.AddManager(g, u); err != nil {
		t.Fatal(err)
	}

	if err := users.Delete(u.ID); err != nil {
		t.Fatalf("Benutzer mit betreuter Gruppe nicht löschbar: %v", err)
	}
	var rest int64
	db.Table("server_group_managers").Where("user_id = ?", u.ID).Count(&rest)
	if rest != 0 {
		t.Errorf("%d Verwalter-Zuordnungen des gelöschten Benutzers übrig", rest)
	}
	if _, err := groups.GroupByID(g.ID); err != nil {
		t.Errorf("Die Gruppe selbst muss bleiben: %v", err)
	}
}

// Eine Gruppe, auf die eine Alarmregel eingegrenzt ist, ließ sich nicht
// auflösen (Fremdschlüssel aus alert_rule_groups). Die Regel verliert die
// Gruppe. War es ihre letzte, gälte sie danach für ALLE Server - deshalb wird
// sie abgeschaltet statt still ausgeweitet.
func TestGruppeMitAlarmregelLaesstSichAufloesen(t *testing.T) {
	db := migrierteDB(t)
	groups := repositories.NewGroupRepository(db)
	weg, bleibt := gruppe(t, db, "weg"), gruppe(t, db, "bleibt")

	nurDiese := &domain.AlertRule{Name: "nur weg", Type: domain.AlertTypeHeartbeat, Enabled: true,
		Groups: []domain.ServerGroup{*weg}}
	beide := &domain.AlertRule{Name: "beide", Type: domain.AlertTypeHeartbeat, Enabled: true,
		Groups: []domain.ServerGroup{*weg, *bleibt}}
	for _, r := range []*domain.AlertRule{nurDiese, beide} {
		if err := db.Create(r).Error; err != nil {
			t.Fatal(err)
		}
	}

	if err := groups.Disband(weg.ID); err != nil {
		t.Fatalf("Gruppe mit Alarmregel nicht auflösbar: %v", err)
	}

	var a, b domain.AlertRule
	if err := db.Preload("Groups").First(&a, nurDiese.ID).Error; err != nil {
		t.Fatalf("Regel ohne weitere Gruppe darf nicht verschwinden: %v", err)
	}
	if a.Enabled || len(a.Groups) != 0 {
		t.Errorf("Regel ohne Gruppe: enabled=%v, %d Gruppen - sie gälte sonst für alle Server", a.Enabled, len(a.Groups))
	}
	if err := db.Preload("Groups").First(&b, beide.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !b.Enabled || len(b.Groups) != 1 || b.Groups[0].ID != bleibt.ID {
		t.Errorf("Regel mit zweiter Gruppe: enabled=%v, Gruppen=%v - sie muss an der zweiten Gruppe weiterlaufen", b.Enabled, b.Groups)
	}
}
