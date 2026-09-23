// Package creds liest Geheimnisse, die systemd dem Dienst beim Start
// entschlüsselt übergibt (LoadCredentialEncrypted=, SetCredentialEncrypted=).
//
// systemd legt sie unter $CREDENTIALS_DIRECTORY ab - ein Verzeichnis im
// Arbeitsspeicher, das nur dieser eine Dienst sieht. Auf der Platte liegt
// das Geheimnis verschlüsselt, gebunden an das TPM der Maschine und/oder an
// einen root-eigenen Host-Schlüssel. Weder die Konfigurationsdatei noch das
// Datenverzeichnis tragen es dann im Klartext, und eine Kopie davon ist ohne
// die Maschine wertlos.
package creds

import (
	"os"
	"path/filepath"
	"strings"
)

// DirEnv ist die Umgebungsvariable, über die systemd das Verzeichnis nennt.
const DirEnv = "CREDENTIALS_DIRECTORY"

// Namen der Credentials, die LCM kennt.
const (
	MasterKey        = "lcm.key"
	TrivyToken       = "trivy_token"
	BackupPassphrase = "backup_passphrase"
)

// Dir liefert das Credential-Verzeichnis oder "" außerhalb von systemd.
func Dir() string {
	return os.Getenv(DirEnv)
}

// Path liefert den Pfad eines Credentials oder "", wenn es keines gibt.
func Path(name string) string {
	dir := Dir()
	if dir == "" {
		return ""
	}
	p := filepath.Join(dir, name)
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}

// Read liefert den Inhalt eines Credentials ohne umgebende Leerzeichen und
// Zeilenumbrüche - so, wie ein Betreiber es mit `systemd-creds encrypt`
// abgelegt hat. ok=false, wenn es das Credential nicht gibt.
func Read(name string) (string, bool) {
	p := Path(name)
	if p == "" {
		return "", false
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(raw)), true
}
