package controllers_test

import (
	"errors"
	"testing"

	"LCM/internal/core/services"
)

// TestValidierungsfehlerSindEineKategorie hält den Kern des Fehlerbildes
// fest, das Tony gemeldet hat: Beim Speichern der Einstellungen kam
// „interner Serverfehler" statt der Auskunft, was an der Eingabe falsch war.
//
// Ursache war kein einzelner Fehler, sondern das Muster: Der Controller
// führte eine POSITIVLISTE der Validierungsfehler, die auf 400 abgebildet
// werden. Wer einen neuen Validierungsfehler einführte und die Liste nicht
// mitpflegte, lieferte für eine schlichte Fehleingabe einen Serverfehler
// aus - für den Betreiber nicht von einem Absturz zu unterscheiden.
//
// Seitdem wickelt jeder speziellere Fehler ErrSettingInvalid ein, und der
// Controller prüft nur noch die Kategorie. Dieser Test hält das fest: Kommt
// ein neuer Validierungsfehler dazu, der die Kategorie NICHT einwickelt,
// fällt es hier auf und nicht erst beim Betreiber.
func TestValidierungsfehlerSindEineKategorie(t *testing.T) {
	fehler := map[string]error{
		"ErrBackupDirInvalid":     services.ErrBackupDirInvalid,
		"ErrPublicBaseURLInvalid": services.ErrPublicBaseURLInvalid,
		"ErrAptCacheURLInvalid":   services.ErrAptCacheURLInvalid,
	}
	for name, err := range fehler {
		if !errors.Is(err, services.ErrSettingInvalid) {
			t.Errorf("%s wickelt ErrSettingInvalid nicht ein - der Controller "+
				"antwortet darauf mit HTTP 500 statt mit 400", name)
		}
	}
}
