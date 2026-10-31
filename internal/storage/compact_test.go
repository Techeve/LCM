package storage

import (
	"fmt"
	"strings"
	"testing"

	"gorm.io/gorm"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/crypto"
)

// aptOutput liefert eine typische, gut komprimierbare Konsolen-Ausgabe.
func aptOutput(lines int) string {
	var b strings.Builder
	for i := range lines {
		fmt.Fprintf(&b, "Unpacking libfoo%d (1.2.%d-1ubuntu0.%d) over (1.2.%d-1) ...\n", i%40, i, i%7, i)
	}
	return b.String()
}

// incompressible liefert n Bytes, die zstd nicht verkleinern kann.
func incompressible(n int) string {
	var b strings.Builder
	for b.Len() < n {
		b.Write(crypto.GenerateKey())
	}
	return b.String()
}

func openCipherDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&appliedMigration{}); err != nil {
		t.Fatal(err)
	}
	cipher, _ := crypto.NewCipher(crypto.GenerateKey())
	SetFieldCipher(cipher)
	t.Cleanup(func() { SetFieldCipher(nil) })
	return db
}

// storedPlain liefert den entschlüsselten, aber NICHT entpackten Rohwert.
func storedPlain(t *testing.T, db *gorm.DB, jobID string) string {
	t.Helper()
	var raw string
	if err := db.Raw("SELECT output FROM jobs WHERE id = ?", jobID).Scan(&raw).Error; err != nil {
		t.Fatal(err)
	}
	plain, err := fieldCipher.DecryptString(raw)
	if err != nil {
		t.Fatalf("rohwert nicht entschlüsselbar: %v", err)
	}
	return plain
}

func TestPackField(t *testing.T) {
	big := aptOutput(200)
	tests := []struct {
		name       string
		in         string
		wantPacked bool
	}{
		{"kurzer wert bleibt", "lcm-health-ok", false},
		{"große ausgabe wird gepackt", big, true},
		{"unkomprimierbares bleibt", incompressible(1024), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			packed := packField(tt.in)
			if isPacked(packed) != tt.wantPacked {
				t.Fatalf("gepackt = %v, erwartet %v", isPacked(packed), tt.wantPacked)
			}
			got, err := unpackField(packed)
			if err != nil || got != tt.in {
				t.Fatalf("roundtrip: %v, gleich = %v", err, got == tt.in)
			}
		})
	}
}

// TestSerializerPacksLargeOutput: Neue große Ausgaben liegen komprimiert
// in der Spalte und werden transparent gelesen.
func TestSerializerPacksLargeOutput(t *testing.T) {
	db := openCipherDB(t)
	out := aptOutput(500)
	job := &domain.Job{Type: "update", Name: "t", Status: domain.JobStatusSuccess, Output: out}
	if err := db.Create(job).Error; err != nil {
		t.Fatal(err)
	}
	stored := storedPlain(t, db, job.ID)
	if !isPacked(stored) || len(stored)*4 > len(out) {
		t.Fatalf("erwartet komprimiert und < 1/4 der Größe, gespeichert %d von %d Bytes", len(stored), len(out))
	}
	var got domain.Job
	if err := db.First(&got, "id = ?", job.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.Output != out {
		t.Fatal("gelesene ausgabe weicht ab")
	}
}

// TestCompactDatabasePacksLegacyOnce: Altbestand (unkomprimiert
// verschlüsselt) wird nachkomprimiert, bleibt lesbar, und der Durchgang
// läuft nur einmal.
func TestCompactDatabasePacksLegacyOnce(t *testing.T) {
	db := openCipherDB(t)
	out := aptOutput(300)
	job := &domain.Job{Type: "update", Name: "t", Status: domain.JobStatusSuccess}
	if err := db.Create(job).Error; err != nil {
		t.Fatal(err)
	}
	legacy, _ := fieldCipher.EncryptString(out) // so schrieb LCM bis 1.42
	db.Exec("UPDATE jobs SET output = ? WHERE id = ?", legacy, job.ID)

	noFreeSpaceCheck := func() (uint64, bool) { return 0, false }
	res, err := CompactDatabase(db, noFreeSpaceCheck)
	if err != nil {
		t.Fatal(err)
	}
	if res.Packed != 1 || !isPacked(storedPlain(t, db, job.ID)) {
		t.Fatalf("erwartet 1 nachkomprimierten Wert, bericht %+v", res)
	}
	var got domain.Job
	db.First(&got, "id = ?", job.ID)
	if got.Output != out {
		t.Fatal("nach dem nachkomprimieren nicht mehr lesbar")
	}

	db.Exec("UPDATE jobs SET output = ? WHERE id = ?", legacy, job.ID)
	if res, _ := CompactDatabase(db, noFreeSpaceCheck); res.Packed != 0 {
		t.Fatalf("zweiter durchgang hat erneut %d werte angefasst", res.Packed)
	}
}

// TestVacuumShrinksAfterDelete: Nach dem Löschen vieler Zeilen gibt VACUUM
// den Platz frei - aber nur, wenn der freie Plattenplatz reicht.
func TestVacuumShrinksAfterDelete(t *testing.T) {
	db := openCipherDB(t)
	for range 500 {
		db.Create(&domain.Job{Type: "update", Name: "t", Status: domain.JobStatusSuccess, Output: incompressible(4096)})
	}
	db.Exec("DELETE FROM jobs")

	tooLittle := func() (uint64, bool) { return 1, true }
	if res, err := CompactDatabase(db, tooLittle); err != nil || res.BytesBefore != 0 {
		t.Fatalf("vacuum trotz fehlendem platz: %+v, %v", res, err)
	}
	plenty := func() (uint64, bool) { return 1 << 40, true }
	res, err := CompactDatabase(db, plenty)
	if err != nil {
		t.Fatal(err)
	}
	if res.BytesBefore == 0 || res.BytesAfter >= res.BytesBefore {
		t.Fatalf("erwartet verkleinerte datei, bericht %+v", res)
	}
}
