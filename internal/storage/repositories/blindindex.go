package repositories

import (
	"strconv"
	"strings"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/crypto"
)

// blindCipher liefert den deterministischen Blindindex für verschlüsselte,
// aber suchbare Felder (v.a. der Server-Name). Wird beim Start gesetzt
// (über storage.SetFieldCipher); ohne Cipher - z.B. in schlanken Tests -
// dient der normalisierte Klartext selbst als Index, damit Eindeutigkeits-
// prüfungen weiterhin greifen.
var blindCipher *crypto.Cipher

// SetCipher hinterlegt den Master-Cipher für die Blindindex-Berechnung und
// verdrahtet zugleich die domain-BeforeSave-Hooks (Server-Name, Benutzer- und
// Linux-Benutzer-Felder, Server-Ref) auf die HMAC-Variante.
func SetCipher(c *crypto.Cipher) {
	blindCipher = c
	domain.ServerBlindIndex = BlindIndex
	domain.UserBlindIndex = BlindIndex
	domain.ServerRef = ServerRef
}

// ServerRef liefert das deterministische, nicht umkehrbare Token eines Servers
// (HMAC über "server:<id>") - der Fremdschlüssel-Ersatz in vulnerabilities/
// packages, damit die DB die Zuordnung CVE/Paket→Server nicht im Klartext (als
// server_id) preisgibt. Ohne Cipher (Tests) ein stabiler Klartext-Token je id.
func ServerRef(id uint) string {
	v := "server:" + strconv.FormatUint(uint64(id), 10)
	if blindCipher == nil {
		return v
	}
	return blindCipher.BlindIndex(v)
}

// BlindIndex normalisiert einen Wert (klein, getrimmt) und bildet den
// deterministischen HMAC-Index - gleicher Klartext ergibt denselben Index.
func BlindIndex(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if blindCipher == nil {
		return value
	}
	return blindCipher.BlindIndex(value)
}

// decryptField entschlüsselt einen at-rest-verschlüsselten Wert (z.B. den
// Servernamen), der über eine JOIN-Aggregat-Query am GORM-Serializer vorbei
// geladen wurde. Tolerant: Legacy-Klartext / fehlender Cipher werden
// unverändert zurückgegeben.
func decryptField(value string) string {
	if value == "" || blindCipher == nil {
		return value
	}
	if plain, err := blindCipher.DecryptString(value); err == nil {
		return plain
	}
	return value
}
