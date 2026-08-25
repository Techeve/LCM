package services

import (
	"errors"
	"testing"
)

func TestEnforceBackupPassphrase(t *testing.T) {
	cases := []struct {
		name string
		pass string
		ok   bool
	}{
		{"leer", "", false},
		{"zu kurz", "kurz1!", false},
		{"trivial", "password", false},
		{"nur klein, zu wenig klassen", "meine-passphrase", false}, // 16 Zeichen, 2 Klassen
		{"kontext-dominiert", "lcm-backup-123", false},
		{"stark gemischt", "Korrekt-Pferd-Batterie-42", true},
		{"lange passphrase, 2 klassen genuegen", "richtiges pferd batterie klammer", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := EnforceBackupPassphrase(tc.pass)
			if tc.ok && err != nil {
				t.Fatalf("erwartet OK, bekam %v", err)
			}
			if !tc.ok {
				if err == nil {
					t.Fatal("erwartet Ablehnung, bekam nil")
				}
				if !errors.Is(err, ErrWeakBackupPassphrase) {
					t.Fatalf("erwartet ErrWeakBackupPassphrase, bekam %v", err)
				}
			}
		})
	}
}

// TestEnforceBackupPassphraseCarriesProblems stellt sicher, dass die konkreten
// Regelverstöße bis zum Aufrufer durchgereicht werden (für die UI-Anzeige).
func TestEnforceBackupPassphraseCarriesProblems(t *testing.T) {
	err := EnforceBackupPassphrase("password")
	var bpe *BackupPassphraseError
	if !errors.As(err, &bpe) {
		t.Fatalf("erwartet *BackupPassphraseError, bekam %T", err)
	}
	if len(bpe.Check.Problems) == 0 {
		t.Fatal("erwartete konkrete Problem-Codes, bekam keine")
	}
}
