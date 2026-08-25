package storage

import (
	"context"
	"reflect"

	"gorm.io/gorm/schema"

	"LCM/internal/infrastructure/crypto"
	"LCM/internal/storage/repositories"
)

// fieldCipher ist der Cipher für den "aesgcm"-GORM-Serializer. Er wird beim
// Start gesetzt (SetFieldCipher); ist er nil (z.B. in schlanken Tests), fällt
// der Serializer auf Klartext zurück.
var fieldCipher *crypto.Cipher

// SetFieldCipher hinterlegt den Master-Cipher für die feldweise
// At-Rest-Verschlüsselung (Serializer-Tag `aesgcm`) UND für die
// Blindindex-Berechnung in der repositories-Ebene. MUSS vor DB-Operationen
// auf verschlüsselten Feldern aufgerufen werden.
func SetFieldCipher(c *crypto.Cipher) {
	fieldCipher = c
	repositories.SetCipher(c)
}

// aesgcmSerializer verschlüsselt String-Felder transparent mit AES-256-GCM
// beim Schreiben und entschlüsselt sie beim Lesen - für großvolumige,
// sensible Klartext-Felder (SSH-/Job-Konsolen-Output) at rest.
//
// Robustheit: Schlägt das Entschlüsseln fehl (Legacy-Klartext aus der Zeit
// vor Aktivierung der Verschlüsselung oder ein Fremdformat), wird der Wert
// UNVERÄNDERT zurückgegeben, statt den Lesevorgang zu brechen. Neue Schreib-
// vorgänge sind stets verschlüsselt; Altbestände laufen über die Retention aus.
type aesgcmSerializer struct{}

func (aesgcmSerializer) Scan(ctx context.Context, field *schema.Field, dst reflect.Value, dbValue interface{}) error {
	var s string
	switch v := dbValue.(type) {
	case nil:
		s = ""
	case string:
		s = v
	case []byte:
		s = string(v)
	}
	if s != "" && fieldCipher != nil {
		if plain, err := fieldCipher.DecryptString(s); err == nil {
			s = plain
		}
		// Fehler: s bleibt der Rohwert (Legacy-Klartext) - bewusst tolerant.
	}
	field.ReflectValueOf(ctx, dst).SetString(s)
	return nil
}

func (aesgcmSerializer) Value(ctx context.Context, field *schema.Field, dst reflect.Value, fieldValue interface{}) (interface{}, error) {
	s, _ := fieldValue.(string)
	if s == "" || fieldCipher == nil {
		return s, nil
	}
	return fieldCipher.EncryptString(s)
}

func init() {
	schema.RegisterSerializer("aesgcm", aesgcmSerializer{})
}
