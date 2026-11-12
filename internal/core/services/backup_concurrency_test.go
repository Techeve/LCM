package services

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// fixedBackupClock stellt die Uhr des Archivnamens für die Dauer des Tests
// auf einen festen Zeitpunkt: Alle Läufe fallen in dieselbe Sekunde.
func fixedBackupClock(t *testing.T) {
	t.Helper()
	at := time.Date(2026, 11, 8, 5, 30, 0, 0, time.UTC)
	backupNow = func() time.Time { return at }
	t.Cleanup(func() { backupNow = time.Now })
}

// TestBackupZweiLaeufeInDerselbenSekunde: Zwei Sicherungen in derselben
// Sekunde ergeben zwei Archive. Vorher trugen beide denselben Namen, das
// zweite überschrieb das erste, und dessen Metadaten zeigten ins Leere.
func TestBackupZweiLaeufeInDerselbenSekunde(t *testing.T) {
	fixedBackupClock(t)
	bs, dir := staleEnv(t)
	first, err := bs.Create("test", "Korrekt-Pferd-Batterie-42")
	if err != nil {
		t.Fatal(err)
	}
	second, err := bs.Create("test", "Korrekt-Pferd-Batterie-42")
	if err != nil {
		t.Fatal(err)
	}
	if first.FileName == second.FileName {
		t.Fatalf("beide Läufe heißen %s", first.FileName)
	}
	for _, b := range []string{first.FileName, second.FileName} {
		if _, err := os.Stat(filepath.Join(dir, b)); err != nil {
			t.Errorf("archiv %s fehlt: %v", b, err)
		}
	}
}

// TestBackupGleichzeitigeLaeufe: Gleichzeitig angestoßene Läufe kommen sich
// nicht in die Quere - jeder endet mit einem eigenen Archiv.
func TestBackupGleichzeitigeLaeufe(t *testing.T) {
	fixedBackupClock(t)
	bs, dir := staleEnv(t)
	var wg sync.WaitGroup
	names := make([]string, 4)
	for i := range names {
		wg.Go(func() {
			b, err := bs.Create("test", "Korrekt-Pferd-Batterie-42")
			if err != nil {
				t.Errorf("lauf %d: %v", i, err)
				return
			}
			names[i] = b.FileName
		})
	}
	wg.Wait()
	seen := map[string]bool{}
	for _, n := range names {
		if seen[n] {
			t.Fatalf("archivname doppelt vergeben: %s (%v)", n, names)
		}
		seen[n] = true
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(names) {
		t.Errorf("%d Dateien im Backup-Verzeichnis, erwartet %d", len(entries), len(names))
	}
}

// TestBackupMomentaufnahmeSofort0600: Die unverschlüsselte Momentaufnahme
// der Datenbank ist vom ersten Byte an nur für den Dienst lesbar.
func TestBackupMomentaufnahmeSofort0600(t *testing.T) {
	bs, _ := staleEnv(t)
	var mode os.FileMode
	backupSnapshotDone = func(path string) {
		if info, err := os.Stat(path); err == nil {
			mode = info.Mode().Perm()
		}
	}
	t.Cleanup(func() { backupSnapshotDone = func(string) {} })
	if _, err := bs.Create("test", "Korrekt-Pferd-Batterie-42"); err != nil {
		t.Fatal(err)
	}
	if mode != 0o600 {
		t.Errorf("momentaufnahme mit %o angelegt, erwartet 600", mode)
	}
}
