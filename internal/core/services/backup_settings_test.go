package services_test

import (
	"errors"
	"strings"
	"testing"

	"LCM/internal/core/services"
	"LCM/internal/storage/repositories"
)

// TestBackupDirDefaultPrefill prüft, dass das Backup-Verzeichnis in den
// Einstellungen immer gesetzt ist: WithDefaultBackupDir belegt ein leeres
// backup_dir sofort vor, und ein über das Formular geleertes Feld fällt auf
// den Standard zurück - die UI zeigt so stets das tatsächliche Backup-Ziel.
func TestBackupDirDefaultPrefill(t *testing.T) {
	svc, repo := newSettingsService(t)

	// Frische Instanz: backup_dir ist leer → wird mit dem Standard vorbelegt.
	svc.WithDefaultBackupDir("/srv/lcm/backups")
	got, err := repo.Get()
	if err != nil {
		t.Fatal(err)
	}
	if got.BackupDir != "/srv/lcm/backups" {
		t.Fatalf("backup_dir = %q, erwartet Vorbelegung mit Standard", got.BackupDir)
	}

	// Eigener Pfad bleibt erhalten.
	if _, err := svc.UpdateBackupSettings(services.BackupSettingsInput{
		Enabled: true, IntervalHours: 24, Retention: 14, Dir: "/mnt/extern",
		Passphrase: "test-backup-passphrase",
	}, "admin"); err != nil {
		t.Fatal(err)
	}
	got, _ = repo.Get()
	if got.BackupDir != "/mnt/extern" {
		t.Fatalf("backup_dir = %q, erwartet eigenen Pfad", got.BackupDir)
	}

	// Geleertes Feld (auch nur Whitespace) fällt auf den Standard zurück.
	if _, err := svc.UpdateBackupSettings(services.BackupSettingsInput{
		Enabled: true, IntervalHours: 24, Retention: 14, Dir: "   ",
	}, "admin"); err != nil {
		t.Fatal(err)
	}
	got, _ = repo.Get()
	if got.BackupDir != "/srv/lcm/backups" {
		t.Fatalf("backup_dir = %q, erwartet Rückfall auf Standard", got.BackupDir)
	}

	// Ein bereits gesetzter Wert wird von WithDefaultBackupDir NICHT überschrieben.
	svc.WithDefaultBackupDir("/anderswo")
	got, _ = repo.Get()
	if got.BackupDir != "/srv/lcm/backups" {
		t.Fatalf("backup_dir = %q, Vorbelegung darf gesetzte Werte nicht überschreiben", got.BackupDir)
	}
}

// TestBackupPassphraseHinterlegbar (R2-027): Die Passphrase ist über die
// Einstellungen setzbar (verschlüsselt), das GEPLANTE Backup (leerer
// Parameter, keine Umgebungsvariable) benutzt sie - und Aktivieren ohne
// jede Passphrase wird abgelehnt statt still auf ewig zu scheitern.
func TestBackupPassphraseHinterlegbar(t *testing.T) {
	env := newTestEnv(t)
	t.Setenv(services.EnvBackupPassphrase, "") // Umgebung ausdrücklich leer

	// Aktivieren ohne Passphrase: klare Ablehnung.
	_, err := env.Settings.UpdateBackupSettings(services.BackupSettingsInput{
		Enabled: true, IntervalHours: 24, Retention: 14,
	}, "admin")
	if !errors.Is(err, services.ErrBackupNeedsPassphrase) {
		t.Fatalf("aktivieren ohne Passphrase muss abgelehnt werden, bekam: %v", err)
	}

	// Mit Passphrase: angenommen, verschlüsselt gespeichert (nie Klartext).
	if _, err := env.Settings.UpdateBackupSettings(services.BackupSettingsInput{
		Enabled: true, IntervalHours: 24, Retention: 14, Passphrase: "korrekt-pferd-batterie",
	}, "admin"); err != nil {
		t.Fatalf("mit Passphrase: %v", err)
	}
	st, _ := repositories.NewSettingsRepository(env.DB()).Get()
	if st.BackupPassphraseEnc == "" || strings.Contains(st.BackupPassphraseEnc, "korrekt-pferd") {
		t.Fatalf("Passphrase muss verschlüsselt gespeichert sein: %q", st.BackupPassphraseEnc)
	}
	if !env.Settings.BackupPassphraseStored() {
		t.Error("BackupPassphraseStored muss true melden")
	}
}
