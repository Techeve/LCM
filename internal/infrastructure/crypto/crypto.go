// Package crypto stellt die At-Rest-Verschlüsselung des LCM bereit.
//
// Alle sensiblen Werte (SSH-Private-Keys, Passwörter, TOTP-Secrets) werden
// mit AES-256-GCM verschlüsselt in der Datenbank gespeichert. Der 32-Byte
// Master-Key stammt aus der Datei lcm.key im Datenverzeichnis (chmod 600)
// oder aus der Umgebungsvariable LCM_ENCRYPTION_KEY (base64). Geht der Key
// verloren, sind die verschlüsselten Daten unwiederbringlich unbrauchbar.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
)

// KeySize ist die Länge des Master-Keys in Bytes (AES-256).
const KeySize = 32

var (
	ErrInvalidKeySize    = errors.New("master-key muss exakt 32 bytes lang sein")
	ErrInvalidCiphertext = errors.New("ungültiger oder manipulierter ciphertext")
)

// Cipher kapselt AES-256-GCM mit einem festen Master-Key.
// Instanzen sind nach der Erstellung nebenläufig nutzbar.
type Cipher struct {
	aead     cipher.AEAD
	blindKey []byte // abgeleiteter HMAC-Schlüssel für BlindIndex
}

// NewCipher erstellt einen Cipher aus einem 32-Byte-Key.
func NewCipher(key []byte) (*Cipher, error) {
	if len(key) != KeySize {
		return nil, ErrInvalidKeySize
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes initialisieren: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("gcm initialisieren: %w", err)
	}
	// Blind-Index-Schlüssel in einer getrennten Domäne vom Master-Key
	// ableiten (verrät nichts über den AES-Key).
	bk := sha256.Sum256(append([]byte("lcm-blind-index-v1\x00"), key...))
	return &Cipher{aead: aead, blindKey: bk[:]}, nil
}

// BlindIndex liefert einen deterministischen, nicht umkehrbaren Index
// (HMAC-SHA256) für Gleichheits-/Eindeutigkeitsabfragen auf sonst zufällig
// verschlüsselten Feldern: gleicher Klartext → gleicher Index (anders als
// EncryptString, das pro Aufruf einen frischen Nonce nutzt). So bleibt die
// Namens-Eindeutigkeit über einen Unique-Index prüfbar, ohne den Klartext
// zu speichern.
func (c *Cipher) BlindIndex(value string) string {
	mac := hmac.New(sha256.New, c.blindKey)
	mac.Write([]byte(value))
	return hex.EncodeToString(mac.Sum(nil))
}

// EncryptString verschlüsselt einen Klartext. Ergebnisformat:
// base64(nonce || ciphertext+tag). Ein leerer Klartext bleibt leer,
// damit optionale Felder ohne Sonderbehandlung funktionieren.
func (c *Cipher) EncryptString(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("nonce erzeugen: %w", err)
	}
	sealed := c.aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// DecryptString entschlüsselt einen mit EncryptString erzeugten Wert.
// Manipulationen am Ciphertext führen zu ErrInvalidCiphertext (GCM-Tag).
func (c *Cipher) DecryptString(encoded string) (string, error) {
	if encoded == "" {
		return "", nil
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", ErrInvalidCiphertext
	}
	ns := c.aead.NonceSize()
	if len(raw) < ns {
		return "", ErrInvalidCiphertext
	}
	plain, err := c.aead.Open(nil, raw[:ns], raw[ns:], nil)
	if err != nil {
		return "", ErrInvalidCiphertext
	}
	return string(plain), nil
}

// GenerateKey erzeugt einen kryptografisch zufälligen 32-Byte-Master-Key.
func GenerateKey() []byte {
	key := make([]byte, KeySize)
	if _, err := rand.Read(key); err != nil {
		panic(fmt.Sprintf("crypto/rand nicht verfügbar: %v", err))
	}
	return key
}
