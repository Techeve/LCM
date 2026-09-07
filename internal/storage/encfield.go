package storage

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"sync"

	"gorm.io/gorm"
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

// --- Map-Updates -------------------------------------------------------------

// encryptMapAssignments schließt eine Lücke im Update-Weg von GORM.
//
// Bei Updates(map[string]any) übernimmt GORM die Map-Werte UNVERÄNDERT in die
// Anweisung (callbacks/update.go, ConvertToAssignments) - der Serializer eines
// Feldes wird dort nicht befragt; das passiert nur auf dem Weg über die
// Struktur. Ein `aesgcm`-Feld, das über eine Map geschrieben wird, stünde also
// im Klartext in der Spalte.
//
// Auffallen konnte das nirgends: Scan gibt einen nicht entschlüsselbaren Wert
// bewusst unverändert zurück (Legacy-Klartext), Klartext liest sich damit
// völlig unauffällig. Genau so lagen OS-, Kernel-, CPU- und Portfelder der
// Server offen in der Datenbank - geschrieben vom Scan über UpdateFields.
//
// Der Callback stellt den Weg über die Map dem über die Struktur gleich.
func encryptMapAssignments(tx *gorm.DB) {
	if tx.Error != nil || tx.Statement == nil || tx.Statement.Schema == nil {
		return
	}
	werte, ok := tx.Statement.Dest.(map[string]any)
	if !ok {
		return
	}
	// Auf einer Kopie arbeiten: Die Map gehört dem Aufrufer. Sie an Ort und
	// Stelle zu verschlüsseln würde ihm den Klartext unter den Händen
	// wegziehen - und ein zweites Update mit derselben Map verschlüsselte
	// den Geheimtext ein weiteres Mal.
	var kopie map[string]any
	for spalte, wert := range werte {
		feld := tx.Statement.Schema.LookUpField(spalte)
		if feld == nil || feld.Serializer == nil {
			continue
		}
		valuer, ok := feld.Serializer.(schema.SerializerValuerInterface)
		if !ok {
			continue
		}
		verschluesselt, err := valuer.Value(tx.Statement.Context, feld, reflect.Value{}, wert)
		if err != nil {
			tx.AddError(fmt.Errorf("feld %q verschlüsseln: %w", spalte, err))
			return
		}
		if kopie == nil {
			kopie = make(map[string]any, len(werte))
			for k, v := range werte {
				kopie[k] = v
			}
		}
		kopie[spalte] = verschluesselt
	}
	if kopie != nil {
		tx.Statement.Dest = kopie
	}
}

// registerMapUpdateEncryption hängt encryptMapAssignments vor das
// Update-Statement. Wird von Open für jede Verbindung aufgerufen, damit die
// Verschlüsselung nicht an der Aufrufstelle hängt: Sie greift für JEDES
// Map-Update, auch für künftige, ohne dass jemand daran denken muss.
func registerMapUpdateEncryption(db *gorm.DB) error {
	return db.Callback().Update().Before("gorm:update").
		Register("lcm:encrypt_map_assignments", encryptMapAssignments)
}

// schemaCache hält die für aesgcmColumns gelesenen Schemata.
var schemaCache sync.Map

// aesgcmColumns liefert die Spalten eines Modells, die über den
// aesgcm-Serializer laufen - abgeleitet aus dem GORM-Schema, nicht aus einer
// von Hand gepflegten Liste. Damit kann keine Aufstellung mehr hinter den
// Struktur-Tags zurückbleiben: Ein neues verschlüsseltes Feld ist automatisch
// dabei, sobald es sein Tag trägt.
func aesgcmColumns(db *gorm.DB, model any) ([]string, error) {
	// Eigener Schema-Cache statt db.Statement: Ein Parse auf dem Statement des
	// Handles hinterlässt dort das gelesene Schema und lenkt nachfolgende
	// Schreibvorgänge auf die falsche Tabelle.
	sch, err := schema.Parse(model, &schemaCache, db.NamingStrategy)
	if err != nil {
		return nil, fmt.Errorf("schema lesen: %w", err)
	}
	spalten := make([]string, 0, len(sch.Fields))
	for _, feld := range sch.Fields {
		if feld.Serializer != nil && feld.DBName != "" {
			spalten = append(spalten, feld.DBName)
		}
	}
	sort.Strings(spalten)
	return spalten, nil
}

// tabellenName liefert den Tabellennamen eines Modells aus dem Schema.
func tabellenName(db *gorm.DB, model any) (string, error) {
	sch, err := schema.Parse(model, &schemaCache, db.NamingStrategy)
	if err != nil {
		return "", fmt.Errorf("schema lesen: %w", err)
	}
	return sch.Table, nil
}
