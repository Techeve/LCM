package services

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"LCM/internal/core/domain"
)

// stubWerkzeug legt ein ausführbares Skript namens name an, das text ausgibt -
// so lässt sich der Scan gegen zpool/btrfs/lvs prüfen, ohne dass eine dieser
// Techniken auf der Testmaschine vorhanden sein muss.
func stubWerkzeug(t *testing.T, bin, name, skript string) {
	t.Helper()
	pfad := filepath.Join(bin, name)
	if err := os.WriteFile(pfad, []byte("#!/bin/sh\n"+skript+"\n"), 0o755); err != nil {
		t.Fatalf("%s anlegen: %v", name, err)
	}
}

// laufeScan führt das Scan-Kommando mit gestellten Werkzeugen und einem
// gefälschten /proc aus.
func laufeScan(t *testing.T, bin, proc string) []domain.StorageHealth {
	t.Helper()
	cmd := exec.Command("sh", "-c", storageHealthCmdIn(proc))
	cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"))
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("Scan-Befehl: %v", err)
	}
	return parseStorageHealth(string(out))
}

// findeBefund sucht einen Befund nach Art und Namen.
func findeBefund(befunde []domain.StorageHealth, kind, name string) *domain.StorageHealth {
	for i := range befunde {
		if befunde[i].Kind == kind && befunde[i].Name == name {
			return &befunde[i]
		}
	}
	return nil
}

// TestScanErkenntZfsPoolZustaende ist der Fall, um den es hier eigentlich geht:
// Ein Pool kann wochenlang DEGRADED laufen, ohne dass irgendjemand es merkt -
// ZFS meldet sich von selbst nicht.
func TestScanErkenntZfsPoolZustaende(t *testing.T) {
	bin, proc := t.TempDir(), t.TempDir()
	stubWerkzeug(t, bin, "zpool", `
case "$1" in
list) printf 'tank\tONLINE\t45\t12\n'
      printf 'backup\tDEGRADED\t78\t3\n' ;;
status) if [ "$2" = backup ]; then
cat <<'EOF'
  pool: backup
 state: DEGRADED
  scan: scrub repaired 0B in 00:12:34 with 0 errors on Sun Sep  1 04:23:11 2026
config:

	NAME        STATE     READ WRITE CKSUM
	backup      DEGRADED     0     0     7
	  mirror-0  DEGRADED     0     0    14
	    sda     ONLINE       0     0     0
	    sdb     FAULTED      0     0     7

errors: No known data errors
EOF
        else
cat <<'EOF'
  pool: tank
 state: ONLINE
config:

	NAME        STATE     READ WRITE CKSUM
	tank        ONLINE       0     0     0

errors: No known data errors
EOF
        fi ;;
esac`)

	befunde := laufeScan(t, bin, proc)

	gesund := findeBefund(befunde, domain.StorageKindZFS, "tank")
	if gesund == nil || gesund.State != domain.StorageStateHealthy {
		t.Fatalf("tank: erwartet healthy, bekam %+v", gesund)
	}
	if gesund.UsagePercent != 45 || gesund.FragmentPercent != 12 {
		t.Errorf("tank: Kapazität/Fragmentierung falsch gelesen: %d%%/%d%%",
			gesund.UsagePercent, gesund.FragmentPercent)
	}
	if gesund.Message != "" {
		t.Errorf("tank ist in Ordnung, trägt aber eine Meldung: %q", gesund.Message)
	}

	krank := findeBefund(befunde, domain.StorageKindZFS, "backup")
	if krank == nil {
		t.Fatal("der Pool backup fehlt im Ergebnis")
	}
	if krank.State != domain.StorageStateDegraded {
		t.Errorf("backup: erwartet degraded, bekam %q", krank.State)
	}
	// Nur die Pool-Zeile zählt, nicht zusätzlich die vdev-Zeilen darunter -
	// sonst wären die Fehler doppelt gezählt.
	if krank.Errors != 7 {
		t.Errorf("backup: erwartet 7 Fehler aus der Pool-Zeile, bekam %d", krank.Errors)
	}
	if !strings.Contains(krank.Message, "DEGRADED") {
		t.Errorf("backup: Meldung nennt den Zustand nicht: %q", krank.Message)
	}
}

// TestScanErkenntBtrfsFehlerzaehler: btrfs zählt Gerätefehler still mit. Der
// Wert steht nur da - gemeldet wird er nirgends.
func TestScanErkenntBtrfsFehlerzaehler(t *testing.T) {
	bin, proc := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(proc, "mounts"), []byte(
		"/dev/sdc1 /daten btrfs rw,relatime 0 0\n"+
			"/dev/sdc1 /daten/unter btrfs rw,relatime 0 0\n"+
			"/dev/sda1 / ext4 rw,relatime 0 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stubWerkzeug(t, bin, "btrfs", `
case "$1 $2" in
"device stats") cat <<'EOF'
[/dev/sdc1].write_io_errs    0
[/dev/sdc1].read_io_errs     0
[/dev/sdc1].flush_io_errs    0
[/dev/sdc1].corruption_errs  3
[/dev/sdc1].generation_errs  0
EOF
;;
"filesystem show") echo "Label: none  uuid: 1234" ;;
"scrub status") echo "Scrub started:    Mon Sep  1 03:00:00 2026" ;;
esac`)

	befunde := laufeScan(t, bin, proc)

	// Zwei Mountpunkte, EIN Dateisystem - sonst stünde derselbe Verbund
	// doppelt in der Übersicht und jeder Fehler würde zweimal gemeldet.
	var anzahl int
	for _, b := range befunde {
		if b.Kind == domain.StorageKindBtrfs {
			anzahl++
		}
	}
	if anzahl != 1 {
		t.Errorf("erwartet ein Btrfs-Dateisystem, bekam %d", anzahl)
	}

	b := findeBefund(befunde, domain.StorageKindBtrfs, "/daten")
	if b == nil {
		t.Fatalf("Btrfs-Befund fehlt: %+v", befunde)
	}
	if b.Errors != 3 {
		t.Errorf("erwartet 3 gezählte Fehler, bekam %d", b.Errors)
	}
	if b.State != domain.StorageStateDegraded {
		t.Errorf("bei gezählten Fehlern erwartet degraded, bekam %q", b.State)
	}
	// Die Umrechnung des Scrub-Zeitpunkts macht `date -d` auf dem Zielsystem.
	// Das ist GNU-Verhalten; auf einer BSD-Maschine (Entwicklung auf macOS)
	// gibt es das nicht. Die Zusicherung greift daher dort, wo LCM tatsächlich
	// läuft - auf Linux und in der CI.
	if gnuDate() && b.LastScrub == nil {
		t.Error("der Zeitpunkt des letzten Scrubs wurde nicht übernommen")
	}
}

// gnuDate meldet, ob das `date` dieser Maschine -d versteht.
func gnuDate() bool {
	return exec.Command("date", "-d", "Mon Sep  1 03:00:00 2026", "+%s").Run() == nil
}

// TestScanErkenntDegradiertesMdRaid: [U_] heißt, der Verbund läuft weiter -
// aber ohne Redundanz. Genau deshalb fällt es niemandem auf.
func TestScanErkenntDegradiertesMdRaid(t *testing.T) {
	bin, proc := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(proc, "mdstat"), []byte(
		`Personalities : [raid1] [raid6]
md0 : active raid1 sdb1[1] sda1[0]
      976630464 blocks super 1.2 [2/2] [UU]

md1 : active raid6 sdd1[3] sdc1[2](F) sdb2[1] sda2[0]
      3906764800 blocks super 1.2 level 6, 512k chunk, algorithm 2 [4/3] [UU_U]

unused devices: <none>
`), 0o644); err != nil {
		t.Fatal(err)
	}

	befunde := laufeScan(t, bin, proc)

	gesund := findeBefund(befunde, domain.StorageKindMDRaid, "md0")
	if gesund == nil || gesund.State != domain.StorageStateHealthy {
		t.Errorf("md0: erwartet healthy, bekam %+v", gesund)
	}
	krank := findeBefund(befunde, domain.StorageKindMDRaid, "md1")
	if krank == nil || krank.State != domain.StorageStateDegraded {
		t.Fatalf("md1: erwartet degraded, bekam %+v", krank)
	}
	if !strings.Contains(krank.Message, "Redundanz") {
		t.Errorf("md1: die Meldung sagt nicht, was das Problem ist: %q", krank.Message)
	}
}

// TestScanErkenntVollenThinPool: Der Metadaten-Anteil ist der gefährlichere
// Wert - läuft er voll, ist der Pool nicht mehr zu retten.
func TestScanErkenntVollenThinPool(t *testing.T) {
	bin, proc := t.TempDir(), t.TempDir()
	stubWerkzeug(t, bin, "lvs", `
printf '  vg0\tpool0\ttwi-aotz--\t42.00\t11.30\t\n'
printf '  vg0\tpool1\ttwi-aotz--\t83.10\t97.40\t\n'
printf '  vg0\troot\t-wi-ao----\t\t\t\n'`)

	befunde := laufeScan(t, bin, proc)

	// Das gewöhnliche Volume ist kein Thin-Pool und gehört nicht ins Ergebnis.
	if b := findeBefund(befunde, domain.StorageKindLVMThin, "vg0/root"); b != nil {
		t.Errorf("ein gewöhnliches LV wurde als Thin-Pool geführt: %+v", b)
	}

	gesund := findeBefund(befunde, domain.StorageKindLVMThin, "vg0/pool0")
	if gesund == nil || gesund.State != domain.StorageStateHealthy {
		t.Errorf("pool0: erwartet healthy, bekam %+v", gesund)
	}

	krank := findeBefund(befunde, domain.StorageKindLVMThin, "vg0/pool1")
	if krank == nil {
		t.Fatalf("pool1 fehlt im Ergebnis: %+v", befunde)
	}
	if krank.MetaPercent != 97 {
		t.Errorf("pool1: Metadaten-Anteil falsch gelesen: %d", krank.MetaPercent)
	}
	if krank.State != domain.StorageStateFaulted {
		t.Errorf("pool1: volle Metadaten müssen als faulted gelten, bekam %q", krank.State)
	}
	if !strings.Contains(krank.Message, "Metadaten") {
		t.Errorf("pool1: die Meldung nennt die Metadaten nicht: %q", krank.Message)
	}
}

// TestScanSchweigtOhneDieseTechniken: Auf einem gewöhnlichen Server gibt es
// weder ZFS noch Btrfs noch RAID. Der Scan darf dann nichts melden - und vor
// allem nicht scheitern.
func TestScanSchweigtOhneDieseTechniken(t *testing.T) {
	if befunde := laufeScan(t, t.TempDir(), t.TempDir()); len(befunde) != 0 {
		t.Errorf("ohne die Techniken erwartet kein Befund, bekam %+v", befunde)
	}
}

// TestParseUebernimmtScrubZeitpunkt prüft die Umrechnung im Parser selbst -
// unabhängig davon, welches `date` die Testmaschine mitbringt.
func TestParseUebernimmtScrubZeitpunkt(t *testing.T) {
	befunde := parseStorageHealth("zfs\ttank\tONLINE\t10\t0\t0\t0\t1756699391\t\n")
	if len(befunde) != 1 {
		t.Fatalf("erwartet ein Ergebnis, bekam %d", len(befunde))
	}
	if befunde[0].LastScrub == nil || befunde[0].LastScrub.Unix() != 1756699391 {
		t.Errorf("Scrub-Zeitpunkt nicht übernommen: %+v", befunde[0].LastScrub)
	}
	// 0 heißt „unbekannt" und darf nicht als 1970 durchgehen.
	ohne := parseStorageHealth("zfs\ttank\tONLINE\t10\t0\t0\t0\t0\t\n")
	if ohne[0].LastScrub != nil {
		t.Errorf("Epoche 0 muss unbekannt bleiben, wurde aber zu %v", ohne[0].LastScrub)
	}
}

// TestBtrfsOhneRootMeldetUnbekanntStattGesund: In der Test-VM stand ein
// Btrfs mit fehlendem Gerät auf "healthy" - weil btrfs dem Dienstbenutzer den
// Zugriff verweigert, "filesystem show" nichts lieferte und der Zustand auf
// ONLINE stehen blieb. Ein falsches Gesund ist schlimmer als Nichtwissen.
func TestBtrfsOhneRootMeldetUnbekanntStattGesund(t *testing.T) {
	bin, proc := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(proc, "mounts"),
		[]byte("/dev/loop0 /mnt/btrfs btrfs rw,degraded 0 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stubWerkzeug(t, bin, "btrfs", `echo "ERROR: Operation not permitted" >&2; exit 1`)

	b := findeBefund(laufeScan(t, bin, proc), domain.StorageKindBtrfs, "/mnt/btrfs")
	if b == nil {
		t.Fatal("Btrfs-Befund fehlt")
	}
	if b.State != domain.StorageStateUnknown {
		t.Errorf("ohne Zugriff erwartet unknown, bekam %q", b.State)
	}
}
