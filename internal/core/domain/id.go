package domain

import "github.com/google/uuid"

// newUUID erzeugt eine zufällige UUID (v4) als Primärschlüssel. Seit
// v0.2.0 nutzen die Protokoll- und Bestandstabellen (Jobs, Audit-Log,
// Pakete, SSH-Sessions/-Kommandos) UUIDs statt fortlaufender Zahlen:
// Sie sind nicht ratbar und geben keine Mengengerüste preis.
func newUUID() string {
	return uuid.NewString()
}
