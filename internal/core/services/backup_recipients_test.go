package services

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"

	"LCM/internal/core/domain"
)

// TestAgeArchivRoundtrip: An Empfänger verschlüsselt, mit dem privaten
// Schlüssel geöffnet - und der Klartext steht nicht im Archiv.
func TestAgeArchivRoundtrip(t *testing.T) {
	pub, priv, err := GenerateBackupRecipient()
	if err != nil {
		t.Fatal(err)
	}
	recipients, err := ParseBackupRecipients("# Tony\n" + pub + "\n\n")
	if err != nil {
		t.Fatal(err)
	}
	sources := []archiveSource{
		{Name: "app.db", Data: []byte("SQLite format 3\x00...")},
		{Name: "config.json", Data: []byte(`{"port":9310}`)},
	}
	var blob bytes.Buffer
	if err := writeAgeArchive(&blob, sources, recipients); err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(blob.Bytes(), []byte(ageMagic)) {
		t.Fatal("age-archiv muss mit der age-kennung beginnen")
	}
	if bytes.Contains(blob.Bytes(), []byte("SQLite format 3")) {
		t.Fatal("archiv enthält klartext")
	}

	got, err := openEncryptedArchive(blob.Bytes(), priv)
	if err != nil {
		t.Fatalf("öffnen mit privatem schlüssel: %v", err)
	}
	if len(got) != 2 || got[0].Name != "app.db" || string(got[1].Data) != `{"port":9310}` {
		t.Fatalf("unerwarteter inhalt: %+v", got)
	}

	// Fremder Schlüssel: kein Empfänger passt.
	_, other, _ := GenerateBackupRecipient()
	if _, err := openEncryptedArchive(blob.Bytes(), other); !errors.Is(err, ErrBackupPassphrase) {
		t.Errorf("fremder schlüssel: erwartet ErrBackupPassphrase, bekam %v", err)
	}
	// Nur eine Passphrase: klare Ansage, dass ein Schlüssel nötig ist.
	if _, err := openEncryptedArchive(blob.Bytes(), "irgendeine-passphrase"); !errors.Is(err, ErrBackupNoIdentity) {
		t.Errorf("passphrase statt schlüssel: erwartet ErrBackupNoIdentity, bekam %v", err)
	}
	// Abgeschnitten: age prüft jeden Block.
	if _, err := openEncryptedArchive(blob.Bytes()[:blob.Len()-7], priv); err == nil {
		t.Error("abgeschnittenes archiv wurde akzeptiert")
	}
}

// TestKeyFromSecretErkenntSchluessel: Ein Feld für beides - der private
// Schlüssel wird an seinem Präfix erkannt, alles andere ist Passphrase.
func TestKeyFromSecretErkenntSchluessel(t *testing.T) {
	_, priv, _ := GenerateBackupRecipient()
	if k := keyFromSecret("  " + strings.ToLower(priv) + "\n"); len(k.identities) != 1 || k.passphrase != "" {
		t.Errorf("privater schlüssel nicht erkannt: %+v", k)
	}
	if k := keyFromSecret("Korrekt-Pferd-Batterie-42"); k.passphrase == "" || len(k.identities) != 0 {
		t.Errorf("passphrase falsch eingeordnet: %+v", k)
	}
	// Sieht aus wie ein Schlüssel, ist aber kaputt: dann eben Passphrase -
	// das Öffnen scheitert danach mit der normalen Meldung.
	if k := keyFromSecret("AGE-SECRET-KEY-1KAPUTT"); len(k.identities) != 0 {
		t.Error("kaputter schlüssel darf keine identity liefern")
	}
	if !keyFromSecret("   ").empty() {
		t.Error("leer muss leer bleiben")
	}
}

// TestParseBackupRecipientsLehntMuellAb: Nur öffentliche X25519-Schlüssel;
// die Zeilennummer hilft beim Suchen.
func TestParseBackupRecipientsLehntMuellAb(t *testing.T) {
	pub, _, _ := GenerateBackupRecipient()
	_, err := ParseBackupRecipients(pub + "\nssh-ed25519 AAAA…\n")
	if !errors.Is(err, ErrInvalidBackupRecipient) || !strings.Contains(err.Error(), "zeile 2") {
		t.Errorf("erwartet ErrInvalidBackupRecipient mit zeile 2, bekam %v", err)
	}
	// Ein privater Schlüssel in der Empfänger-Liste wäre genau das Geheimnis
	// auf dem Server, das die Empfänger vermeiden sollen.
	_, priv, _ := GenerateBackupRecipient()
	if _, err := ParseBackupRecipients(priv); !errors.Is(err, ErrInvalidBackupRecipient) {
		t.Errorf("privater schlüssel als empfänger: erwartet fehler, bekam %v", err)
	}
	norm, err := NormalizeBackupRecipients("\n# kommentar\n " + pub + " \n\n")
	if err != nil || norm != pub {
		t.Errorf("normalisiert = %q, %v; erwartet %q", norm, err, pub)
	}
	if got, err := ParseBackupRecipients(""); err != nil || len(got) != 0 {
		t.Errorf("leere liste: %v %v", got, err)
	}
}

// TestBackupMitEmpfaengernBrauchtKeinePassphrase: Sind Empfänger hinterlegt,
// läuft das geplante Backup ohne jedes Geheimnis auf dem Server - und die
// Historie sagt, womit sich das Archiv öffnen lässt.
func TestBackupMitEmpfaengernBrauchtKeinePassphrase(t *testing.T) {
	t.Setenv(EnvBackupPassphrase, "")
	bs, _ := staleEnv(t)
	pub, priv, _ := GenerateBackupRecipient()
	if err := bs.settings.Save(&domain.GlobalSettings{BackupRecipients: pub}); err != nil {
		t.Fatal(err)
	}
	if !bs.RecipientsConfigured() {
		t.Fatal("empfänger sollten erkannt werden")
	}

	b, err := bs.Create("scheduler", "")
	if err != nil {
		t.Fatalf("backup mit empfängern: %v", err)
	}
	if b.Encryption != "recipients" {
		t.Errorf("encryption = %q", b.Encryption)
	}
	raw := readBackupFile(t, bs, b.FileName)
	if !bytes.HasPrefix(raw, []byte(ageMagic)) {
		t.Error("archiv ist kein age-archiv")
	}
	if _, err := openEncryptedArchive(raw, priv); err != nil {
		t.Errorf("öffnen mit privatem schlüssel: %v", err)
	}
	if err := bs.StageRestoreReader(bytes.NewReader(raw), priv, 64<<20); err != nil {
		t.Errorf("restore mit privatem schlüssel: %v", err)
	}
	// Eine ausdrücklich gewählte Passphrase gewinnt - der Mensch will genau
	// dieses Archiv mit genau dieser Passphrase.
	p, err := bs.Create("tony", "Korrekt-Pferd-Batterie-42")
	if err != nil {
		t.Fatal(err)
	}
	if p.Encryption != "passphrase" {
		t.Errorf("encryption = %q", p.Encryption)
	}
	if _, err := openEncryptedArchive(readBackupFile(t, bs, p.FileName), "Korrekt-Pferd-Batterie-42"); err != nil {
		t.Errorf("passphrase-archiv: %v", err)
	}
}

// TestAgeArchivMitPassphraseVonHand: Ein mit `age -p` erzeugtes Archiv lässt
// sich mit der Passphrase öffnen - kostet nichts und hilft beim Umzug.
func TestAgeArchivMitPassphraseVonHand(t *testing.T) {
	r, err := age.NewScryptRecipient("Korrekt-Pferd-Batterie-42")
	if err != nil {
		t.Fatal(err)
	}
	r.SetWorkFactor(10)
	var blob bytes.Buffer
	if err := writeAgeArchive(&blob, []archiveSource{{Name: "x", Data: []byte("y")}}, []age.Recipient{r}); err != nil {
		t.Fatal(err)
	}
	got, err := openEncryptedArchive(blob.Bytes(), "Korrekt-Pferd-Batterie-42")
	if err != nil || len(got) != 1 || string(got[0].Data) != "y" {
		t.Fatalf("age -p archiv: %v %+v", err, got)
	}
}

// readBackupFile liest ein fertiges Archiv aus dem Backup-Verzeichnis.
func readBackupFile(t *testing.T, bs *BackupService, name string) []byte {
	t.Helper()
	dir, err := bs.backupDir()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
