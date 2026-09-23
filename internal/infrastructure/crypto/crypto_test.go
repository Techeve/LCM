package crypto

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"LCM/internal/infrastructure/creds"
)

func TestEncryptDecryptRoundtrip(t *testing.T) {
	c, err := NewCipher(GenerateKey())
	if err != nil {
		t.Fatal(err)
	}
	for _, plain := range []string{"geheim", "", "-----BEGIN OPENSSH PRIVATE KEY-----\nlangertext\n"} {
		enc, err := c.EncryptString(plain)
		if err != nil {
			t.Fatal(err)
		}
		if plain != "" && enc == plain {
			t.Errorf("ciphertext darf nicht dem klartext entsprechen")
		}
		dec, err := c.DecryptString(enc)
		if err != nil {
			t.Fatal(err)
		}
		if dec != plain {
			t.Errorf("roundtrip: bekam %q, erwartet %q", dec, plain)
		}
	}
}

func TestEncryptIsNonDeterministic(t *testing.T) {
	c, _ := NewCipher(GenerateKey())
	a, _ := c.EncryptString("gleicher text")
	b, _ := c.EncryptString("gleicher text")
	if a == b {
		t.Error("zwei verschlüsselungen desselben klartexts dürfen nicht identisch sein (zufällige nonce)")
	}
}

func TestDecryptRejectsTampering(t *testing.T) {
	c, _ := NewCipher(GenerateKey())
	enc, _ := c.EncryptString("wichtig")
	// letztes Zeichen kippen
	tampered := enc[:len(enc)-2] + "AA"
	if _, err := c.DecryptString(tampered); err == nil {
		t.Error("manipulierter ciphertext muss abgelehnt werden")
	}
	// falscher Key
	other, _ := NewCipher(GenerateKey())
	if _, err := other.DecryptString(enc); err == nil {
		t.Error("entschlüsselung mit falschem key muss fehlschlagen")
	}
}

func TestInvalidKeySize(t *testing.T) {
	if _, err := NewCipher([]byte("zu-kurz")); err == nil {
		t.Error("key mit falscher länge muss abgelehnt werden")
	}
}

func TestLoadOrCreateMasterKey(t *testing.T) {
	dir := t.TempDir()

	key1, source, err := LoadOrCreateMasterKey(dir)
	if err != nil {
		t.Fatal(err)
	}
	if source != SourceGenerated || len(key1) != KeySize {
		t.Fatalf("erststart: source=%v len=%d", source, len(key1))
	}
	// Datei mit 0600 angelegt?
	info, err := os.Stat(filepath.Join(dir, KeyFileName))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("lcm.key hat rechte %v, erwartet 0600", info.Mode().Perm())
	}

	// Zweiter Aufruf liest denselben Key aus der Datei.
	key2, source, err := LoadOrCreateMasterKey(dir)
	if err != nil || source != SourceFile {
		t.Fatalf("zweiter aufruf: err=%v source=%v", err, source)
	}
	if string(key1) != string(key2) {
		t.Error("key muss stabil bleiben")
	}
}

func TestMasterKeyFromEnv(t *testing.T) {
	key := GenerateKey()
	t.Setenv(EnvKeyName, " "+base64.StdEncoding.EncodeToString(key)+" ")
	got, source, err := LoadOrCreateMasterKey(t.TempDir())
	if err != nil || source != SourceEnv {
		t.Fatalf("env-key: err=%v source=%v", err, source)
	}
	if string(got) != string(key) {
		t.Error("env-key wurde nicht übernommen")
	}
}

// TestMasterKeyAusSystemdCredential: Nach der Umstellung liegt kein lcm.key
// mehr im Datenverzeichnis - der Schlüssel kommt aus dem Verzeichnis, das
// systemd dem Dienst beim Start hinstellt. Beide Ablageformen gelten:
// base64 wie in der Datei und die rohen 32 Bytes.
func TestMasterKeyAusSystemdCredential(t *testing.T) {
	key := GenerateKey()
	credDir := t.TempDir()
	t.Setenv(creds.DirEnv, credDir)
	t.Setenv(EnvKeyName, "")
	if err := os.WriteFile(filepath.Join(credDir, creds.MasterKey), KeyFileContent(key), 0o600); err != nil {
		t.Fatal(err)
	}
	dataDir := t.TempDir()
	got, source, err := LoadOrCreateMasterKey(dataDir)
	if err != nil || source != SourceCredential {
		t.Fatalf("credential: err=%v source=%v", err, source)
	}
	if string(got) != string(key) {
		t.Error("Schlüssel aus dem Credential stimmt nicht")
	}
	if _, err := os.Stat(filepath.Join(dataDir, KeyFileName)); !os.IsNotExist(err) {
		t.Error("mit Credential darf keine lcm.key entstehen")
	}

	// Rohe Bytes statt base64.
	if err := os.WriteFile(filepath.Join(credDir, creds.MasterKey), key, 0o600); err != nil {
		t.Fatal(err)
	}
	if got, _, err := LoadOrCreateMasterKey(dataDir); err != nil || string(got) != string(key) {
		t.Errorf("rohe 32 Bytes: err=%v", err)
	}
}

// TestDateiUeberstimmtCredential: Nach einem Restore oder einer Rotation liegt
// wieder eine lcm.key - sie gehört zur Datenbank und gewinnt. Der Zustand ist
// als „veraltetes Credential" erkennbar.
func TestDateiUeberstimmtCredential(t *testing.T) {
	credDir := t.TempDir()
	t.Setenv(creds.DirEnv, credDir)
	t.Setenv(EnvKeyName, "")
	if err := os.WriteFile(filepath.Join(credDir, creds.MasterKey), KeyFileContent(GenerateKey()), 0o600); err != nil {
		t.Fatal(err)
	}
	dataDir := t.TempDir()
	fileKey := GenerateKey()
	if err := WriteKeyFile(filepath.Join(dataDir, KeyFileName), fileKey); err != nil {
		t.Fatal(err)
	}
	got, source, err := LoadOrCreateMasterKey(dataDir)
	if err != nil || source != SourceFile || string(got) != string(fileKey) {
		t.Fatalf("Datei muss gewinnen: err=%v source=%v", err, source)
	}
	if !CredentialStale(source) {
		t.Error("Datei neben Credential muss als veraltet gemeldet werden")
	}
	if err := ShredKeyFile(filepath.Join(dataDir, KeyFileName)); err != nil {
		t.Fatal(err)
	}
	if _, source, _ := LoadOrCreateMasterKey(dataDir); source != SourceCredential {
		t.Errorf("nach dem Vernichten der Datei gilt das Credential, bekam %v", source)
	}
}
