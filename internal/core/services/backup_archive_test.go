package services

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestBackupArchiveRoundtrip(t *testing.T) {
	files := []archiveFile{
		{Name: "app.db", Data: []byte("SQLite format 3\x00...binary...")},
		{Name: "lcm.key", Data: bytes.Repeat([]byte{0xAB}, 32)},
		{Name: "config.json", Data: []byte(`{"jwt_secret":"geheim"}`)},
	}
	blob, err := buildEncryptedArchive(files, "korrekte-passphrase")
	if err != nil {
		t.Fatal(err)
	}
	// Das Archiv darf den Klartext nicht enthalten.
	if bytes.Contains(blob, []byte("geheim")) || bytes.Contains(blob, []byte("SQLite format 3")) {
		t.Fatal("Archiv enthält Klartext - nicht verschlüsselt")
	}

	got, err := openEncryptedArchive(blob, "korrekte-passphrase")
	if err != nil {
		t.Fatalf("öffnen mit korrekter Passphrase: %v", err)
	}
	if len(got) != len(files) {
		t.Fatalf("erwartete %d Dateien, bekam %d", len(files), len(got))
	}
	byName := map[string][]byte{}
	for _, f := range got {
		byName[f.Name] = f.Data
	}
	for _, f := range files {
		if !bytes.Equal(byName[f.Name], f.Data) {
			t.Errorf("Datei %q weicht ab", f.Name)
		}
	}
}

func TestBackupArchiveWrongPassphrase(t *testing.T) {
	blob, err := buildEncryptedArchive([]archiveFile{{Name: "x", Data: []byte("y")}}, "richtig")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := openEncryptedArchive(blob, "falsch"); !errors.Is(err, ErrBackupPassphrase) {
		t.Errorf("falsche Passphrase: erwartet ErrBackupPassphrase, bekam %v", err)
	}
}

func TestBackupArchiveTamperDetected(t *testing.T) {
	blob, err := buildEncryptedArchive([]archiveFile{{Name: "x", Data: []byte("y")}}, "pw")
	if err != nil {
		t.Fatal(err)
	}
	blob[len(blob)-1] ^= 0xFF // letztes Ciphertext-Byte kippen
	if _, err := openEncryptedArchive(blob, "pw"); !errors.Is(err, ErrBackupPassphrase) {
		t.Errorf("manipuliertes Archiv: erwartet ErrBackupPassphrase, bekam %v", err)
	}
}

func TestBackupArchiveBadMagic(t *testing.T) {
	if _, err := openEncryptedArchive([]byte("nicht-lcm-datei-abcdefghijklmnop"), "pw"); !errors.Is(err, ErrBackupFormat) {
		t.Errorf("fremde Datei: erwartet ErrBackupFormat, bekam %v", err)
	}
}

// TestBackupArchiveWritesV3 stellt sicher, dass neue Archive das aktuelle,
// blockweise verschlüsselte Format tragen.
func TestBackupArchiveWritesV3(t *testing.T) {
	blob, err := buildEncryptedArchive([]archiveFile{{Name: "x", Data: []byte("y")}}, "pw")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(blob[:backupMagicLen], backupMagicV3) {
		t.Fatalf("neues Archiv trägt Magic %q, erwartet LCMBAK3", blob[:backupMagicLen])
	}
	if scryptNForMagic(blob[:backupMagicLen]) != scryptNStream {
		t.Fatalf("Magic → N=%d, erwartet %d", scryptNForMagic(blob[:backupMagicLen]), scryptNStream)
	}
}

// TestBackupArchiveLegacyV1Readable stellt sicher, dass Alt-Backups im
// LCMBAK1-Format (scrypt N=2^15) weiter entschlüsselbar bleiben - sonst wären
// bestehende Backups nach dem Formatwechsel unrestaurierbar.
func TestBackupArchiveLegacyV1Readable(t *testing.T) {
	want := []archiveFile{{Name: "app.db", Data: []byte("alt-inhalt")}}
	blob := buildLegacyV1Archive(t, want, "alte-passphrase")
	if !bytes.Equal(blob[:backupMagicLen], backupMagicV1) {
		t.Fatalf("Test-Fixture ist nicht LCMBAK1: %q", blob[:backupMagicLen])
	}
	got, err := openEncryptedArchive(blob, "alte-passphrase")
	if err != nil {
		t.Fatalf("LCMBAK1 öffnen: %v", err)
	}
	if len(got) != 1 || got[0].Name != "app.db" || !bytes.Equal(got[0].Data, want[0].Data) {
		t.Fatalf("LCMBAK1-Inhalt weicht ab: %+v", got)
	}
	if _, err := openEncryptedArchive(blob, "falsch"); !errors.Is(err, ErrBackupPassphrase) {
		t.Errorf("LCMBAK1 falsche Passphrase: erwartet ErrBackupPassphrase, bekam %v", err)
	}
}

// buildLegacyV1Archive erzeugt ein Archiv im alten LCMBAK1-Format (N=2^15).
func buildLegacyV1Archive(t *testing.T, files []archiveFile, passphrase string) []byte {
	t.Helper()
	return buildSealedArchive(t, files, passphrase, backupMagicV1, scryptNLegacy)
}

// buildLegacyV2Archive erzeugt ein Archiv im LCMBAK2-Format (N=2^17) - das
// Format, das v1.12.0 geschrieben hat. Solche Archive liegen im Feld und
// müssen lesbar bleiben.
func buildLegacyV2Archive(t *testing.T, files []archiveFile, passphrase string) []byte {
	t.Helper()
	return buildSealedArchive(t, files, passphrase, backupMagicV2, scryptNSealed)
}

// buildSealedArchive repliziert den früheren Schreibpfad (EIN GCM-Siegel über
// dem ganzen ZIP), um Alt-Bestand zu simulieren.
func buildSealedArchive(t *testing.T, files []archiveFile, passphrase string, magic []byte, scN int) []byte {
	t.Helper()
	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)
	for _, f := range files {
		w, err := zw.Create(f.Name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(f.Data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	salt := make([]byte, backupSaltLen)
	if _, err := rand.Read(salt); err != nil {
		t.Fatal(err)
	}
	aead, err := deriveAEAD(passphrase, salt, scN)
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal(err)
	}
	sealed := aead.Seal(nil, nonce, zipBuf.Bytes(), nil)
	var out bytes.Buffer
	out.Write(magic)
	out.Write(salt)
	out.Write(nonce)
	out.Write(sealed)
	return out.Bytes()
}

// TestBackupArchiveLegacyV2Readable: v1.12.0 hat LCMBAK2 geschrieben. Solche
// Archive liegen bereits im Feld und müssen restaurierbar bleiben.
func TestBackupArchiveLegacyV2Readable(t *testing.T) {
	want := []archiveFile{{Name: "app.db", Data: []byte("stand-aus-1.12.0")}}
	blob := buildLegacyV2Archive(t, want, "alte-passphrase")
	if !bytes.Equal(blob[:backupMagicLen], backupMagicV2) {
		t.Fatalf("Test-Fixture ist nicht LCMBAK2: %q", blob[:backupMagicLen])
	}
	got, err := openEncryptedArchive(blob, "alte-passphrase")
	if err != nil {
		t.Fatalf("LCMBAK2 öffnen: %v", err)
	}
	if len(got) != 1 || got[0].Name != "app.db" || !bytes.Equal(got[0].Data, want[0].Data) {
		t.Fatalf("LCMBAK2-Inhalt weicht ab: %+v", got)
	}
	if _, err := openEncryptedArchive(blob, "falsch"); !errors.Is(err, ErrBackupPassphrase) {
		t.Errorf("LCMBAK2 falsche Passphrase: erwartet ErrBackupPassphrase, bekam %v", err)
	}
}

// TestBackupArchiveMultipleChunks schickt mehr als einen Block durch den
// Strom - der Fall, den die frühere Ein-Siegel-Fassung gar nicht kannte.
func TestBackupArchiveMultipleChunks(t *testing.T) {
	// Zufallsdaten: nicht komprimierbar, also wirklich mehrere Blöcke.
	payload := make([]byte, 3*archiveChunkSize+1234)
	if _, err := rand.Read(payload); err != nil {
		t.Fatal(err)
	}
	blob, err := buildEncryptedArchive([]archiveFile{{Name: "app.db", Data: payload}}, "pw")
	if err != nil {
		t.Fatal(err)
	}
	got, err := openEncryptedArchive(blob, "pw")
	if err != nil {
		t.Fatalf("mehrblock-archiv öffnen: %v", err)
	}
	if len(got) != 1 || !bytes.Equal(got[0].Data, payload) {
		t.Fatal("Inhalt über mehrere Blöcke hinweg weicht ab")
	}
}

// TestBackupArchiveTruncatedRejected: Ein abgeschnittenes Archiv darf nicht als
// vollständig durchgehen - sonst führte ein halb übertragenes Backup zu einem
// Restore mit halber Datenbank.
func TestBackupArchiveTruncatedRejected(t *testing.T) {
	payload := make([]byte, 2*archiveChunkSize)
	if _, err := rand.Read(payload); err != nil {
		t.Fatal(err)
	}
	blob, err := buildEncryptedArchive([]archiveFile{{Name: "app.db", Data: payload}}, "pw")
	if err != nil {
		t.Fatal(err)
	}
	// Letzten Block abschneiden (der Abschlussblock fehlt damit).
	cut := blob[:len(blob)-4096]
	if _, err := openEncryptedArchive(cut, "pw"); err == nil {
		t.Fatal("abgeschnittenes Archiv wurde akzeptiert")
	}
}

// TestBackupArchiveStreamsWithoutBuffering ist der eigentliche Regressionstest
// zum Speicher-Zwischenfall: Der Backup-Lauf legte die Datenbank und das fertige
// Archiv mehrfach vollständig in den RAM (ReadFile + ZIP-Puffer + Siegel +
// Ausgabepuffer). Auf einer kleinen VM riss das den Prozess mitsamt Maschine
// mit. Jetzt fließt alles blockweise - der Verbrauch darf nicht mehr mit der
// Archivgröße wachsen.
//
// Gemessen wird die DIFFERENZ zwischen einem kleinen und einem großen Archiv.
// Der feste Sockel (vor allem die 64 MiB der scrypt-Ableitung) fällt damit
// heraus, und übrig bleibt genau die Frage: wächst der Bedarf mit den Daten?
func TestBackupArchiveStreamsWithoutBuffering(t *testing.T) {
	const small = 4 << 20
	const large = 36 << 20

	allocFor := func(size int) uint64 {
		path := filepath.Join(t.TempDir(), "big.db")
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		buf := make([]byte, 1<<20)
		if _, err := rand.Read(buf); err != nil { // nicht komprimierbar
			t.Fatal(err)
		}
		for written := 0; written < size; written += len(buf) {
			if _, err := f.Write(buf); err != nil {
				t.Fatal(err)
			}
		}
		f.Close()

		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		if err := writeEncryptedArchive(io.Discard, []archiveSource{{Name: "app.db", Path: path}}, "pw"); err != nil {
			t.Fatal(err)
		}
		runtime.ReadMemStats(&after)
		return after.TotalAlloc - before.TotalAlloc
	}

	growth := int64(allocFor(large)) - int64(allocFor(small))
	// Streamend ist der Zuwachs praktisch null; die frühere Fassung teilte ein
	// Vielfaches der Nutzdaten zu. Die Schranke ist bewusst großzügig.
	if limit := int64(large-small) / 4; growth > limit {
		t.Fatalf("Zuteilung wächst um %d MiB, wenn das Archiv um %d MiB wächst - läuft nicht mehr streamend?",
			growth>>20, (large-small)>>20)
	}
}
