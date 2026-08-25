package crypto

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
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

	key1, created, err := LoadOrCreateMasterKey(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !created || len(key1) != KeySize {
		t.Fatalf("erststart: created=%v len=%d", created, len(key1))
	}
	// Datei mit 0600 angelegt?
	info, err := os.Stat(filepath.Join(dir, KeyFileName))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("lcm.key hat rechte %v, erwartet 0600", info.Mode().Perm())
	}

	// Zweiter Aufruf liest denselben Key.
	key2, created, err := LoadOrCreateMasterKey(dir)
	if err != nil || created {
		t.Fatalf("zweiter aufruf: err=%v created=%v", err, created)
	}
	if string(key1) != string(key2) {
		t.Error("key muss stabil bleiben")
	}
}

func TestMasterKeyFromEnv(t *testing.T) {
	key := GenerateKey()
	t.Setenv(EnvKeyName, " "+base64.StdEncoding.EncodeToString(key)+" ")
	got, created, err := LoadOrCreateMasterKey(t.TempDir())
	if err != nil || created {
		t.Fatalf("env-key: err=%v created=%v", err, created)
	}
	if string(got) != string(key) {
		t.Error("env-key wurde nicht übernommen")
	}
}
