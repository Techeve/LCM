package services

import (
	"errors"
	"strings"
)

// Backup-Passphrase-Stärke. Ein .lcmbak trägt die komplette DB samt Master-Key;
// die Passphrase ist die EINZIGE Hürde für einen Angreifer mit einem geleakten
// Archiv (scrypt bremst ihn, aber eine kurze/geratene Passphrase fällt
// trotzdem). Geprüft wird mit DERSELBEN Engine wie für Benutzerpasswörter -
// bewusst kein zweiter Regelsatz, der an einer Stelle veralten könnte. Eine
// Backup-Passphrase hat keinen Personenbezug, daher leere PasswordIdentity.
//
// Enforced wird ausschließlich dort, wo eine Passphrase NEU vom Nutzer gewählt
// wird (manuelles Backup mit Passphrase-Parameter; Hinterlegen der geplanten
// Passphrase). Aus Umgebung/Einstellungen bereits aufgelöste Passphrasen werden
// zur Laufzeit NICHT erneut geprüft - sonst könnte ein geplantes Backup nach
// einer Policy-Verschärfung still zu scheitern beginnen.

// ErrWeakBackupPassphrase signalisiert eine Backup-Passphrase, welche die
// Stärke-Policy nicht erfüllt. Controller mappen darüber auf HTTP 422.
var ErrWeakBackupPassphrase = errors.New("backup-passphrase erfüllt die sicherheitsanforderungen nicht")

// BackupPassphraseError transportiert die konkreten Regelverstöße bis zur API,
// damit die Oberfläche dieselben Problem-Codes anzeigen kann wie bei
// Benutzerpasswörtern. errors.Is(err, ErrWeakBackupPassphrase) bleibt wahr.
type BackupPassphraseError struct {
	Check PasswordCheck
}

func (e *BackupPassphraseError) Error() string {
	msgs := make([]string, 0, len(e.Check.Problems))
	for _, p := range e.Check.Problems {
		msgs = append(msgs, p.Message)
	}
	if len(msgs) == 0 {
		return ErrWeakBackupPassphrase.Error()
	}
	return "backup-passphrase zu schwach: " + strings.Join(msgs, "; ")
}

func (e *BackupPassphraseError) Is(target error) bool { return target == ErrWeakBackupPassphrase }

// EnforceBackupPassphrase gibt eine Passphrase nur frei, wenn sie die
// Stärke-Policy erfüllt. Einziger Weg, eine Backup-Passphrase freizugeben.
func EnforceBackupPassphrase(passphrase string) error {
	if check := CheckPasswordPolicy(passphrase, PasswordIdentity{}); !check.OK {
		return &BackupPassphraseError{Check: check}
	}
	return nil
}
