package storage

import (
	"testing"
	"time"

	"gorm.io/gorm"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

// TestMigrateToUUIDPreservesData baut eine Datenbank im Alt-Schema
// (fortlaufende Ganzzahl-IDs) mit verknüpften Zeilen und einer gültigen
// Audit-Hash-Kette auf, führt die UUID-Migration aus und prüft: alle IDs
// sind jetzt UUIDs, kein Datensatz ging verloren, die Fremdschlüssel
// zeigen weiterhin auf die richtigen Zeilen und die Hash-Kette bleibt
// intakt.
func TestMigrateToUUIDPreservesData(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}

	// 1. Volles Zielschema anlegen (liefert servers & Co. für die FKs) …
	if err := autoMigrate(db); err != nil {
		t.Fatal(err)
	}
	// … dann die fünf Tabellen ins Alt-Schema (INTEGER-id) zurückversetzen.
	for _, name := range []string{"ssh_commands", "ssh_sessions", "jobs", "packages", "audit_logs"} {
		if err := db.Migrator().DropTable(name); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.AutoMigrate(&legacyJob{}, &legacySession{}, &legacyCommand{}, &legacyPackage{}, &legacyAudit{}); err != nil {
		t.Fatal(err)
	}
	if got := idColumnType(db, "jobs"); got != "integer" {
		t.Fatalf("vorbedingung: jobs.id sollte INTEGER sein, ist %q", got)
	}

	// 2. Referenzierten Server anlegen (jobs/sessions verweisen darauf).
	if err := db.Create(&domain.Server{Name: "srv1", Host: "h1", ServiceUser: "lcm-svc"}).Error; err != nil {
		t.Fatal(err)
	}
	var srvID uint = 1

	// 3. Alt-Daten mit expliziten Ganzzahl-IDs und Fremdschlüsseln.
	j1 := legacyJob{ID: 10, ServerID: &srvID, Type: "update", Name: "Job A", Status: "success", TriggeredBy: "admin", CreatedAt: time.Now()}
	j2 := legacyJob{ID: 20, ServerID: &srvID, Type: "health", Name: "Job B", Status: "success", TriggeredBy: "scheduler", CreatedAt: time.Now()}
	must(t, db.Create(&j1).Error)
	must(t, db.Create(&j2).Error)

	// Session s1 hängt an Job 10, s2 ist jobfrei (Ad-hoc).
	j10 := int64(10)
	s1 := legacySession{ID: 100, ServerID: &srvID, JobID: &j10, Purpose: "rule:Update", Actor: "admin", OpenedAt: time.Now()}
	s2 := legacySession{ID: 200, ServerID: &srvID, JobID: nil, Purpose: "harden-ssh", Actor: "admin", OpenedAt: time.Now()}
	must(t, db.Create(&s1).Error)
	must(t, db.Create(&s2).Error)

	// Kommandos hängen an den Sessions.
	must(t, db.Create(&legacyCommand{ID: 1000, SSHSessionID: 100, Seq: 1, Command: "apt-get update", ExitCode: 0}).Error)
	must(t, db.Create(&legacyCommand{ID: 1001, SSHSessionID: 100, Seq: 2, Command: "apt-get upgrade", ExitCode: 0}).Error)
	must(t, db.Create(&legacyCommand{ID: 1002, SSHSessionID: 200, Seq: 1, Command: "sshd -t", ExitCode: 0}).Error)

	// Pakete.
	must(t, db.Create(&legacyPackage{ID: 5, ServerID: srvID, Name: "nginx", Version: "1.24"}).Error)
	must(t, db.Create(&legacyPackage{ID: 6, ServerID: srvID, Name: "openssl", Version: "3.0", CandidateVersion: "3.1", Security: true}).Error)

	// Audit-Log als gültige Hash-Kette (drei Einträge).
	seedLegacyAuditChain(t, db)

	// 4. Migration ausführen.
	if err := migrateToUUID(db); err != nil {
		t.Fatalf("migrateToUUID: %v", err)
	}
	if got := idColumnType(db, "jobs"); got != "text" {
		t.Fatalf("nach migration: jobs.id sollte TEXT sein, ist %q", got)
	}
	// Idempotenz: zweiter Lauf ist ein No-op.
	if err := migrateToUUID(db); err != nil {
		t.Fatalf("zweiter migrateToUUID-Lauf: %v", err)
	}

	// 5a. Zeilenzahlen erhalten.
	assertCount(t, db, &domain.Job{}, 2)
	assertCount(t, db, &domain.SSHSession{}, 2)
	assertCount(t, db, &domain.SSHCommand{}, 3)
	assertCount(t, db, &domain.Package{}, 2)
	assertCount(t, db, &domain.AuditLog{}, 3)

	// 5b. Alle IDs sind jetzt UUIDs (36 Zeichen, nicht "10"/"20"…).
	var jobs []domain.Job
	must(t, db.Order("name").Find(&jobs).Error)
	for _, j := range jobs {
		if len(j.ID) != 36 {
			t.Errorf("job %q hat keine UUID: %q", j.Name, j.ID)
		}
	}

	// 5c. Fremdschlüssel: Session „Job A“ zeigt auf die neue Job-UUID.
	jobAUUID := jobs[0].ID // "Job A"
	var sess1 domain.SSHSession
	must(t, db.Where("purpose = ?", "rule:Update").First(&sess1).Error)
	if sess1.JobID == nil || *sess1.JobID != jobAUUID {
		t.Errorf("session.job_id nicht korrekt umgesetzt: %v (erwartet %s)", sess1.JobID, jobAUUID)
	}
	if len(sess1.ID) != 36 {
		t.Errorf("session hat keine UUID: %q", sess1.ID)
	}
	// Die jobfreie Session bleibt jobfrei.
	var sess2 domain.SSHSession
	must(t, db.Where("purpose = ?", "harden-ssh").First(&sess2).Error)
	if sess2.JobID != nil {
		t.Errorf("jobfreie session hat plötzlich job_id %v", *sess2.JobID)
	}

	// 5d. Kommandos zeigen auf die neuen Session-UUIDs (Cascade-FK intakt).
	var cmds []domain.SSHCommand
	must(t, db.Where("ssh_session_id = ?", sess1.ID).Order("seq").Find(&cmds).Error)
	if len(cmds) != 2 {
		t.Fatalf("erwartete 2 kommandos für session, bekam %d", len(cmds))
	}
	if cmds[0].Command != "apt-get update" || cmds[1].Command != "apt-get upgrade" {
		t.Errorf("kommando-inhalt/-reihenfolge verloren: %+v", cmds)
	}

	// 5e. Audit-Hash-Kette weiterhin intakt, Seq bewahrt die Reihenfolge.
	auditRepo := repositories.NewAuditRepository(db)
	broken, err := auditRepo.VerifyChain()
	if err != nil {
		t.Fatal(err)
	}
	if broken != "" {
		t.Errorf("audit-kette nach migration gebrochen bei %s", broken)
	}
	var audit []domain.AuditLog
	must(t, db.Order("seq").Find(&audit).Error)
	if len(audit) != 3 || audit[0].Seq != 10 || audit[1].Seq != 20 || audit[2].Seq != 30 {
		t.Errorf("audit-seq nicht aus alten IDs übernommen: %+v", seqs(audit))
	}
	for _, a := range audit {
		if len(a.ID) != 36 {
			t.Errorf("audit-eintrag hat keine UUID: %q", a.ID)
		}
	}

	// 5f. Eine neue Audit-Zeile reiht sich lückenlos ein (Seq = 31).
	auditRepo.Append(&domain.AuditLog{Actor: "admin", Action: "test.append", Entity: "test"})
	var latest domain.AuditLog
	must(t, db.Order("seq DESC").First(&latest).Error)
	if latest.Seq != 31 {
		t.Errorf("neue audit-seq sollte 31 sein, ist %d", latest.Seq)
	}
	if broken, _ := auditRepo.VerifyChain(); broken != "" {
		t.Errorf("audit-kette nach neuem Eintrag gebrochen bei %s", broken)
	}
}

// seedLegacyAuditChain schreibt drei gültig verkettete Audit-Einträge mit
// den IDs 10/20/30 ins Alt-Schema.
func seedLegacyAuditChain(t *testing.T, db *gorm.DB) {
	t.Helper()
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	specs := []struct {
		id     int64
		action string
	}{
		{10, "server.join"},
		{20, "rule.define"},
		{30, "server.harden-ssh"},
	}
	prev := domain.AuditChainStart
	for i, sp := range specs {
		// Hash über ein domain-Objekt berechnen (identische Feldmenge).
		tmp := domain.AuditLog{
			CreatedAt: base.Add(time.Duration(i) * time.Minute),
			Actor:     "admin", Action: sp.action, Entity: "server",
			EntityID: 1, Details: "", PrevHash: prev,
		}
		hash := tmp.ComputeHash()
		must(t, db.Create(&legacyAudit{
			ID: sp.id, CreatedAt: tmp.CreatedAt, Actor: tmp.Actor,
			Action: tmp.Action, Entity: tmp.Entity, EntityID: tmp.EntityID,
			Details: tmp.Details, PrevHash: prev, Hash: hash,
		}).Error)
		prev = hash
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func assertCount(t *testing.T, db *gorm.DB, model any, want int64) {
	t.Helper()
	var n int64
	must(t, db.Model(model).Count(&n).Error)
	if n != want {
		t.Errorf("%T: erwartete %d zeilen, bekam %d", model, want, n)
	}
}

func seqs(a []domain.AuditLog) []uint {
	out := make([]uint, len(a))
	for i := range a {
		out[i] = a[i].Seq
	}
	return out
}
