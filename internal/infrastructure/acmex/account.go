package acmex

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	legoacme "github.com/go-acme/lego/v5/acme"
	"github.com/go-acme/lego/v5/lego"
	"github.com/go-acme/lego/v5/registration"

	"LCM/internal/infrastructure/tlsx"
)

// ErrTermsNotAccepted: Für diese CA gibt es noch kein Konto, und der Admin
// hat ihre Nutzungsbedingungen nicht angenommen. LCM registriert nie ohne.
var ErrTermsNotAccepted = errors.New("die nutzungsbedingungen der ca sind nicht angenommen")

// account ist das Konto bei einer CA (registration.User von lego).
type account struct {
	key   *ecdsa.PrivateKey
	email string
	reg   *legoacme.ExtendedAccount
}

func (a *account) GetEmail() string                           { return a.email }
func (a *account) GetRegistration() *legoacme.ExtendedAccount { return a.reg }
func (a *account) GetPrivateKey() crypto.Signer               { return a.key }

// registrationFile ist, was LCM je Verzeichnis-URL über das Konto ablegt.
type registrationFile struct {
	Directory string                    `json:"directory"`
	Email     string                    `json:"email"`
	Account   *legoacme.ExtendedAccount `json:"account"`
}

func registrationPath(dataDir, directoryURL string) string {
	return filepath.Join(accountsDir(dataDir), shortID(directoryURL)+".json")
}

// loadAccountKey liest den Kontoschlüssel und legt ihn beim ersten Mal an.
func loadAccountKey(dataDir string) (*ecdsa.PrivateKey, error) {
	path := filepath.Join(baseDir(dataDir), accountKeyFile)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return createAccountKey(dataDir, path)
	}
	if err != nil {
		return nil, fmt.Errorf("kontoschlüssel lesen: %w", err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("kontoschlüssel %s: keine pem-daten", path)
	}
	key, err := x509.ParseECPrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("kontoschlüssel lesen: %w", err)
	}
	return key, nil
}

func createAccountKey(dataDir, path string) (*ecdsa.PrivateKey, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("kontoschlüssel erzeugen: %w", err)
	}
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("kontoschlüssel kodieren: %w", err)
	}
	if err := os.MkdirAll(baseDir(dataDir), 0o700); err != nil {
		return nil, fmt.Errorf("acme-verzeichnis anlegen: %w", err)
	}
	if err := tlsx.ReplaceFile(path, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}), 0o600); err != nil {
		return nil, err
	}
	return key, nil
}

// loadRegistration liest die Registrierung bei der CA; nil, wenn es keine gibt.
func loadRegistration(dataDir, directoryURL string) (*registrationFile, error) {
	data, err := os.ReadFile(registrationPath(dataDir, directoryURL))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("registrierung lesen: %w", err)
	}
	var reg registrationFile
	if err := json.Unmarshal(data, &reg); err != nil {
		return nil, fmt.Errorf("registrierung lesen: %w", err)
	}
	if reg.Account == nil || reg.Account.Location == "" {
		return nil, nil
	}
	return &reg, nil
}

func saveRegistration(dataDir, directoryURL, email string, acc *legoacme.ExtendedAccount) error {
	data, err := json.MarshalIndent(registrationFile{Directory: directoryURL, Email: email, Account: acc}, "", "  ")
	if err != nil {
		return fmt.Errorf("registrierung kodieren: %w", err)
	}
	if err := os.MkdirAll(accountsDir(dataDir), 0o700); err != nil {
		return fmt.Errorf("acme-verzeichnis anlegen: %w", err)
	}
	return tlsx.ReplaceFile(registrationPath(dataDir, directoryURL), data, 0o600)
}

// openAccount liefert das Konto bei der CA. Bei einer CA ohne Konto wird es
// registriert, aber nur mit angenommenen Nutzungsbedingungen. Eine geänderte
// Kontakt-Mail wird bei der CA nachgezogen.
func openAccount(ctx context.Context, dataDir string, cfg Config, newClient func(*account) (*lego.Client, error)) (*lego.Client, error) {
	key, err := loadAccountKey(dataDir)
	if err != nil {
		return nil, err
	}
	saved, err := loadRegistration(dataDir, cfg.DirectoryURL)
	if err != nil {
		return nil, err
	}
	acc := &account{key: key, email: cfg.Email}
	if saved != nil {
		acc.reg = saved.Account
	}
	client, err := newClient(acc)
	if err != nil {
		return nil, fmt.Errorf("verbindung zur ca %s: %w", cfg.DirectoryURL, err)
	}
	switch {
	case saved == nil:
		err = register(ctx, dataDir, cfg, client, acc)
	case saved.Email != cfg.Email:
		err = updateContact(ctx, dataDir, cfg, client, acc)
	}
	if err != nil {
		return nil, err
	}
	return client, nil
}

func register(ctx context.Context, dataDir string, cfg Config, client *lego.Client, acc *account) error {
	if !cfg.TermsAccepted {
		return ErrTermsNotAccepted
	}
	reg, err := client.Registration.Register(ctx, registration.RegisterOptions{TermsOfServiceAgreed: true})
	if err != nil {
		return fmt.Errorf("konto bei der ca anlegen: %w", err)
	}
	acc.reg = reg
	return saveRegistration(dataDir, cfg.DirectoryURL, cfg.Email, reg)
}

func updateContact(ctx context.Context, dataDir string, cfg Config, client *lego.Client, acc *account) error {
	reg, err := client.Registration.UpdateRegistration(ctx, registration.RegisterOptions{TermsOfServiceAgreed: cfg.TermsAccepted})
	if err != nil {
		return fmt.Errorf("kontakt-mail bei der ca ändern: %w", err)
	}
	acc.reg = reg
	return saveRegistration(dataDir, cfg.DirectoryURL, cfg.Email, reg)
}
