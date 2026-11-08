package dbmetrics

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type item struct {
	ID   uint
	Name string
}

// openTestDB öffnet eine Datei-Datenbank wie storage.Open: WAL und
// _txlock=immediate, damit BEGIN die Schreibsperre holt.
func openTestDB(t *testing.T, busyMS int) (*gorm.DB, *Collector) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	dsn := path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(" + itoa(busyMS) + ")&_txlock=immediate"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&item{}); err != nil {
		t.Fatal(err)
	}
	c := New()
	if err := Install(db, c); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})
	return db, c
}

func itoa(n int) string { return strconv.Itoa(n) }

func TestHistogramStufen(t *testing.T) {
	h := newHistogram(StatementBounds)
	at := time.Now()
	for _, d := range []time.Duration{500 * time.Microsecond, time.Millisecond, 5 * time.Millisecond, 2 * time.Second, 20 * time.Second} {
		h.observe(d, at)
	}
	want := []uint64{2, 1, 0, 0, 1, 1}
	for i, n := range want {
		if h.Counts[i] != n {
			t.Fatalf("Eimer %d: %d, erwartet %d (%v)", i, h.Counts[i], n, h.Counts)
		}
	}
	if h.Count != 5 || h.MaxMS != 20000 {
		t.Fatalf("Count %d, Max %v", h.Count, h.MaxMS)
	}
}

func TestAnweisungenJeTabelleUndArt(t *testing.T) {
	db, c := openTestDB(t, 5000)
	if err := db.Create(&item{Name: "a"}).Error; err != nil {
		t.Fatal(err)
	}
	var items []item
	db.Where("name = ?", "a").Find(&items)
	db.Exec("UPDATE items SET name = 'geheim' WHERE id = 1")

	snap := c.snapshot()
	got := map[string]uint64{}
	for _, s := range snap.Statements {
		got[s.Table+" "+s.Kind] = s.Count
	}
	for _, key := range []string{"items INSERT", "items SELECT", "items UPDATE"} {
		if got[key] == 0 {
			t.Errorf("%s fehlt: %v", key, got)
		}
	}
	if snap.RowsWritten != 2 {
		t.Errorf("geschriebene Zeilen %d, erwartet 2", snap.RowsWritten)
	}
	for _, s := range snap.Slowest {
		if strings.Contains(s.SQL, "geheim") || strings.Contains(s.SQL, "= 1") {
			t.Errorf("Wert im Anweisungstext: %q", s.SQL)
		}
	}
}

func TestWartezeitAufDieSchreibsperre(t *testing.T) {
	db, c := openTestDB(t, 5000)
	held := make(chan struct{})
	done := make(chan error)
	go func() {
		done <- db.Transaction(func(tx *gorm.DB) error {
			close(held)
			time.Sleep(200 * time.Millisecond)
			return tx.Create(&item{Name: "erster"}).Error
		})
	}()
	<-held
	if err := db.Transaction(func(tx *gorm.DB) error { return tx.Create(&item{Name: "zweiter"}).Error }); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	snap := c.snapshot()
	if snap.LockWait.MaxMS < 100 {
		t.Fatalf("Wartezeit %v ms, erwartet über 100", snap.LockWait.MaxMS)
	}
	if snap.Transactions.MaxMS < 150 {
		t.Fatalf("längste Transaktion %v ms, erwartet über 150", snap.Transactions.MaxMS)
	}
	if len(snap.LongestTransactions) == 0 || !strings.Contains(snap.LongestTransactions[0].Caller, "dbmetrics_test.go") {
		t.Fatalf("Aufrufstelle fehlt: %+v", snap.LongestTransactions)
	}
	if snap.Errors.Busy != 0 {
		t.Fatalf("busy %d, erwartet 0", snap.Errors.Busy)
	}
}

func TestDatabaseIsLockedWirdGezaehlt(t *testing.T) {
	db, c := openTestDB(t, 20)
	held := make(chan struct{})
	release := make(chan struct{})
	go func() {
		_ = db.Transaction(func(tx *gorm.DB) error {
			close(held)
			<-release
			return nil
		})
	}()
	<-held
	err := db.Transaction(func(tx *gorm.DB) error { return nil })
	close(release)
	if err == nil {
		t.Fatal("zweite Transaktion hätte an der Sperre scheitern müssen")
	}
	if got := c.snapshot().Errors.Busy; got != 1 {
		t.Fatalf("busy %d, erwartet 1 (%v)", got, err)
	}
}

func TestDBFunktioniertMitWrapper(t *testing.T) {
	db, _ := openTestDB(t, 5000)
	if _, err := db.DB(); err != nil {
		t.Fatalf("db.DB() mit Wrapper: %v", err)
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		if _, err := tx.DB(); err != nil {
			return err
		}
		// Verschachtelt: GORM nimmt einen Savepoint, wenn die Verbindung
		// eine Transaktion ist.
		return tx.Transaction(func(inner *gorm.DB) error {
			inner.Create(&item{Name: "innen"})
			return errors.New("zurück")
		})
	})
	if err == nil {
		t.Fatal("Fehler aus der inneren Transaktion fehlt")
	}
	var n int64
	db.Model(&item{}).Count(&n)
	if n != 0 {
		t.Fatalf("%d Zeilen, die innere Transaktion hätte zurückrollen müssen", n)
	}
}

func TestCommitFehlerZaehltEinmal(t *testing.T) {
	c := New()
	db, _ := openTestDB(t, 5000)
	sqlDB, _ := db.DB()
	tx, err := sqlDB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	conn := &txConn{counting: counting{inner: tx, c: c}, tx: tx, db: sqlDB, begun: time.Now()}
	tx.Rollback() // Commit danach scheitert mit ErrTxDone
	if conn.Commit() == nil {
		t.Fatal("Commit hätte scheitern müssen")
	}
	conn.Rollback()
	if got := c.snapshot().Transactions.Count; got != 1 {
		t.Fatalf("%d Transaktionen gezählt, erwartet 1", got)
	}
}

func TestNormalizeSQL(t *testing.T) {
	if got := normalizeSQL("SAVEPOINT sp0xc0001a2b40"); got != "SAVEPOINT sp?" {
		t.Errorf("Savepoint: %q", got)
	}
	got := normalizeSQL("SELECT * FROM jobs WHERE id IN (?,?, ?) AND name = 'x''y' LIMIT 50")
	want := "SELECT * FROM jobs WHERE id IN (?…) AND name = ? LIMIT ?"
	if got != want {
		t.Fatalf("%q, erwartet %q", got, want)
	}
	if tableOf(`DELETE FROM "ssh_commands" WHERE x`) != "ssh_commands" {
		t.Fatal("Tabelle nicht erkannt")
	}
}

func TestBestenlisteHaeltJedenTextEinmal(t *testing.T) {
	c := New()
	at := time.Now()
	for i := range 60 {
		c.observeStatement("t", "SELECT", "SELECT "+itoa(i)+" FROM t"+itoa(i%55), time.Duration(i)*time.Millisecond, at)
	}
	c.observeStatement("t", "SELECT", "SELECT 1 FROM t3", time.Second, at)
	snap := c.snapshot()
	if len(snap.Slowest) != slowestKept {
		t.Fatalf("%d Einträge, erwartet %d", len(snap.Slowest), slowestKept)
	}
	if snap.Slowest[0].MaxMS != 1000 {
		t.Fatalf("langsamste zuerst erwartet, war %+v", snap.Slowest[0])
	}
	seen := map[string]bool{}
	for i, s := range snap.Slowest {
		if seen[s.SQL] {
			t.Fatalf("Text doppelt: %q", s.SQL)
		}
		seen[s.SQL] = true
		if i > 0 && s.MaxMS > snap.Slowest[i-1].MaxMS {
			t.Fatal("nicht absteigend sortiert")
		}
	}
}

func TestTrackZaehltLaeufe(t *testing.T) {
	c := New()
	done := c.Track("vacuum", "")
	c.rowsWritten.Add(7)
	done(errors.New("kaputt"))
	ops := c.snapshot().Operations
	if len(ops) != 1 || ops[0].Kind != "vacuum" || ops[0].RowsWritten != 7 || ops[0].Error != "kaputt" {
		t.Fatalf("%+v", ops)
	}
}

func TestReporterUndPrometheus(t *testing.T) {
	db, c := openTestDB(t, 5000)
	db.Create(&item{Name: "a"})
	r := NewReporter(c, db, "")
	sqlDB, _ := db.DB()
	r.c.setTableRows(countTables(context.Background(), sqlDB))
	snap := r.Snapshot()
	if snap.File.PageCount == 0 || snap.File.PageSize == 0 {
		t.Fatalf("Seitenzahlen fehlen: %+v", snap.File)
	}
	if snap.TableRows.Tables["items"] != 1 {
		t.Fatalf("Zeilen je Tabelle: %+v", snap.TableRows)
	}
	var buf bytes.Buffer
	if err := WritePrometheus(&buf, snap); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		"# TYPE lcm_db_lock_wait_seconds histogram",
		`lcm_db_statement_seconds_bucket{table="items",kind="INSERT",le="0.001"}`,
		`lcm_db_statement_seconds_bucket{table="items",kind="INSERT",le="+Inf"} 1`,
		`lcm_db_errors_total{code="busy"} 0`,
		`lcm_db_table_rows{table="items"} 1`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("fehlt: %s", want)
		}
	}
}
