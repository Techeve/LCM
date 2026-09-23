package crypto

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"LCM/internal/infrastructure/creds"
)

// KeyFileName ist der Dateiname des Master-Keys im Datenverzeichnis.
const KeyFileName = "lcm.key"

// EnvKeyName ist die Umgebungsvariable, die den Master-Key (base64)
// alternativ zur Key-Datei bereitstellen kann.
const EnvKeyName = "LCM_ENCRYPTION_KEY"

// KeySource sagt, woher der Master-Key kam. Das Startprotokoll nennt sie,
// damit ein Betreiber sieht, ob die Umstellung auf Credentials gegriffen hat.
type KeySource string

const (
	SourceEnv        KeySource = "env"        // LCM_ENCRYPTION_KEY
	SourceFile       KeySource = "file"       // lcm.key im Datenverzeichnis
	SourceCredential KeySource = "credential" // systemd-Credential (LoadCredentialEncrypted=)
	SourceGenerated  KeySource = "generated"  // Erststart: neu erzeugt und als Datei abgelegt
)

// LoadOrCreateMasterKey liefert den Master-Key der Installation.
//
// Reihenfolge:
//  1. LCM_ENCRYPTION_KEY - die ausdrückliche Vorgabe des Betreibers.
//  2. lcm.key im Datenverzeichnis. Sie liegt dort bei jeder Installation,
//     die nicht auf Credentials umgestellt ist - und nach einem Restore oder
//     einer Rotation auch bei einer umgestellten: beide schreiben die Datei.
//     Dass sie das Credential dann überstimmt, ist Absicht: Der Schlüssel in
//     der Datei ist der, zu dem die Datenbank gerade passt. Der Aufrufer
//     meldet den Zustand (siehe CredentialStale), und `lcm credentials init`
//     räumt ihn auf.
//  3. Das systemd-Credential lcm.key - der Normalfall nach der Umstellung:
//     kein Klartext auf der Platte, der Schlüssel liegt verschlüsselt im
//     Credential-Speicher und kommt beim Start nur in den Speicher des
//     Dienstes.
//  4. Neu erzeugen und als Datei ablegen (Erststart).
func LoadOrCreateMasterKey(dataDir string) (key []byte, source KeySource, err error) {
	if env := os.Getenv(EnvKeyName); env != "" {
		key, err := decodeKey(env)
		if err != nil {
			return nil, "", fmt.Errorf("%s: %w", EnvKeyName, err)
		}
		return key, SourceEnv, nil
	}

	path := filepath.Join(dataDir, KeyFileName)
	if data, err := os.ReadFile(path); err == nil {
		key, err := decodeKey(string(data))
		if err != nil {
			return nil, "", fmt.Errorf("%s: %w", path, err)
		}
		return key, SourceFile, nil
	} else if !os.IsNotExist(err) {
		return nil, "", fmt.Errorf("%s lesen: %w", path, err)
	}

	if raw, ok := creds.Read(creds.MasterKey); ok {
		key, err := decodeKey(raw)
		if err != nil {
			return nil, "", fmt.Errorf("systemd-credential %s: %w", creds.MasterKey, err)
		}
		return key, SourceCredential, nil
	}

	key = GenerateKey()
	if err := WriteKeyFile(path, key); err != nil {
		return nil, "", err
	}
	return key, SourceGenerated, nil
}

// CredentialStale meldet, dass der Schlüssel aus der Datei kam, obwohl ein
// systemd-Credential hinterlegt ist - nach einem Restore oder einer Rotation.
// Der Betreiber muss `lcm credentials init` erneut ausführen, sonst liegt der
// Schlüssel wieder im Klartext auf der Platte, und das Credential passt
// womöglich nicht mehr zur Datenbank.
func CredentialStale(source KeySource) bool {
	return source == SourceFile && creds.Path(creds.MasterKey) != ""
}

// decodeKey nimmt den Schlüssel so, wie er abgelegt wurde: base64 (Datei,
// Umgebung) oder die rohen 32 Bytes (ein per `systemd-creds encrypt`
// abgelegter Rohschlüssel).
func decodeKey(raw string) ([]byte, error) {
	if len(raw) == KeySize {
		return []byte(raw), nil
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("kein gültiges base64: %w", err)
	}
	if len(key) != KeySize {
		return nil, ErrInvalidKeySize
	}
	return key, nil
}

// KeyFileContent liefert den Schlüssel in der Form der lcm.key-Datei
// (base64 mit Zeilenumbruch) - für die Datei selbst und für das Backup-Archiv,
// das den Schlüssel auch dann tragen muss, wenn er nur als Credential lebt.
func KeyFileContent(key []byte) []byte {
	return []byte(base64.StdEncoding.EncodeToString(key) + "\n")
}

// WriteKeyFile schreibt einen Master-Key base64-kodiert mit strikten
// Rechten (0600) - die Datei gehört ausschließlich dem Service-User.
func WriteKeyFile(path string, key []byte) error {
	if err := os.WriteFile(path, KeyFileContent(key), 0o600); err != nil {
		return fmt.Errorf("master-key speichern: %w", err)
	}
	return nil
}

// ShredKeyFile überschreibt die Schlüsseldatei und entfernt sie - nach der
// Umstellung auf ein Credential. Kein sicheres Löschen auf modernen
// Dateisystemen, aber der Schlüssel bleibt nicht in einem trivial
// wiederherstellbaren Block stehen.
func ShredKeyFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if f, err := os.OpenFile(path, os.O_WRONLY, 0o600); err == nil {
		zeros := make([]byte, info.Size())
		_, _ = f.Write(zeros)
		_ = f.Sync()
		_ = f.Close()
	}
	return os.Remove(path)
}
