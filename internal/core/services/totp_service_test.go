package services_test

import (
	"errors"
	"testing"
	"time"

	"LCM/internal/config"
	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/infrastructure/crypto"
	"LCM/internal/infrastructure/totp"
	"LCM/internal/storage"
	"LCM/internal/storage/repositories"
)

// newTOTPEnv baut ein Minimal-Setup mit direktem Zugriff auf UserRepo und
// Cipher, um 2FA-Codes im Test berechnen zu können.
func newTOTPEnv(t *testing.T) (*services.TOTPService, *repositories.UserRepository, *crypto.Cipher, uint) {
	t.Helper()
	db, _ := storage.Open(":memory:")
	if err := storage.Migrate(db); err != nil {
		t.Fatal(err)
	}
	if err := storage.Seed(db, &config.Config{AdminInitialPassword: "test-admin-passwort", DemoMode: true, AccessTokenTTLMinutes: 60}); err != nil {
		t.Fatal(err)
	}
	userRepo := repositories.NewUserRepository(db)
	cipher, _ := crypto.NewCipher(crypto.GenerateKey())
	audit := services.NewAuditService(repositories.NewAuditRepository(db))
	svc := services.NewTOTPService(userRepo, cipher, audit)

	users, _ := userRepo.FindAll()
	var adminID uint
	for _, u := range users {
		if u.Username == domain.AdminUsername {
			adminID = u.ID
		}
	}
	return svc, userRepo, cipher, adminID
}

func TestTOTPSetupEnableVerify(t *testing.T) {
	svc, userRepo, cipher, adminID := newTOTPEnv(t)

	setup, err := svc.Setup(adminID)
	if err != nil {
		t.Fatal(err)
	}
	if setup.Secret == "" || setup.ProvisioningURI == "" {
		t.Fatal("setup lieferte kein secret/uri")
	}

	// Falscher Code aktiviert 2FA nicht.
	if err := svc.Enable(adminID, "000000"); !errors.Is(err, services.ErrTOTPInvalid) {
		t.Errorf("falscher code aktivierte 2fa: %v", err)
	}

	// Richtigen Code aus dem gespeicherten Secret berechnen.
	code, _ := totp.Code(setup.Secret, time.Now())
	if err := svc.Enable(adminID, code); err != nil {
		t.Fatalf("aktivierung mit gültigem code fehlgeschlagen: %v", err)
	}

	user, _ := userRepo.FindByID(adminID)
	if !user.TOTPEnabled {
		t.Error("totp_enabled wurde nicht gesetzt")
	}
	// Secret ist verschlüsselt gespeichert.
	if user.TOTPSecretEnc == setup.Secret {
		t.Error("secret liegt unverschlüsselt vor")
	}
	dec, _ := cipher.DecryptString(user.TOTPSecretEnc)
	if dec != setup.Secret {
		t.Error("verschlüsseltes secret stimmt nicht")
	}

	// Verify mit frischem Code.
	code2, _ := totp.Code(setup.Secret, time.Now())
	if err := svc.Verify(adminID, code2); err != nil {
		t.Errorf("verify mit gültigem code fehlgeschlagen: %v", err)
	}
}
