// Package totp implementiert zeitbasierte Einmalpasswörter (RFC 6238)
// mit der Go-Standardbibliothek - kein externes Paket (Zero-Bloat).
// Kompatibel mit Google Authenticator, Microsoft Authenticator etc.
package totp

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Defaults nach RFC 6238 / Authenticator-Konvention.
const (
	period    = 30 // Sekunden pro Zeitfenster
	digits    = 6
	secretLen = 20 // 160 Bit
)

// GenerateSecret erzeugt ein neues, base32-kodiertes TOTP-Secret.
func GenerateSecret() (string, error) {
	buf := make([]byte, secretLen)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf), nil
}

// Code berechnet den TOTP-Code für ein Secret zum Zeitpunkt t.
func Code(secret string, t time.Time) (string, error) {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).
		DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil {
		return "", fmt.Errorf("totp-secret dekodieren: %w", err)
	}
	counter := uint64(t.Unix()) / period

	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], counter)
	mac := hmac.New(sha1.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)

	// Dynamic Truncation (RFC 4226).
	offset := sum[len(sum)-1] & 0x0f
	value := (uint32(sum[offset]&0x7f) << 24) |
		(uint32(sum[offset+1]) << 16) |
		(uint32(sum[offset+2]) << 8) |
		uint32(sum[offset+3])

	mod := uint32(1)
	for range digits {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", digits, value%mod), nil
}

// Validate prüft einen Code gegen das Secret. Ein Fenster von ±1 Periode
// (skew) toleriert Uhr-Abweichungen zwischen Server und Authenticator.
// Der Vergleich ist konstant-zeitlich (kein Timing-Leak).
func Validate(secret, code string) bool {
	code = strings.TrimSpace(code)
	now := time.Now()
	for _, skew := range []time.Duration{0, -period * time.Second, period * time.Second} {
		expected, err := Code(secret, now.Add(skew))
		if err != nil {
			return false
		}
		if subtle.ConstantTimeCompare([]byte(expected), []byte(code)) == 1 {
			return true
		}
	}
	return false
}

// QRCodeDataURI kodiert eine otpauth://-URI als PNG-QR-Code und liefert
// ihn als "data:image/png;base64,..."-URI - direkt im <img src> nutzbar.
func QRCodeDataURI(otpauthURI string) (string, error) {
	png, err := qrEncode(otpauthURI, 256)
	if err != nil {
		return "", fmt.Errorf("qr-code erzeugen: %w", err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(png), nil
}

// ProvisioningURI erzeugt die otpauth://-URI für QR-Codes in
// Authenticator-Apps.
func ProvisioningURI(secret, accountName, issuer string) string {
	label := url.PathEscape(issuer + ":" + accountName)
	q := url.Values{}
	q.Set("secret", secret)
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", fmt.Sprintf("%d", digits))
	q.Set("period", fmt.Sprintf("%d", period))
	return "otpauth://totp/" + label + "?" + q.Encode()
}
