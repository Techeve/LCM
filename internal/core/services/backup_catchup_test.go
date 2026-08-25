package services

import (
	"testing"
	"time"

	"LCM/internal/core/domain"
)

// TestBackupOverdue prüft die Nachhol-Entscheidung des Backup-Watchdogs:
// ohne jedes Backup oder mit zu altem jüngsten Backup ist ein automatisches
// Backup überfällig - ein frisches (auch manuelles) Backup deckt das
// Intervall ab.
func TestBackupOverdue(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	mk := func(age time.Duration) []domain.Backup {
		return []domain.Backup{{CreatedAt: now.Add(-age)}}
	}

	cases := []struct {
		name    string
		backups []domain.Backup
		hours   int
		want    bool
	}{
		{"noch nie gesichert", nil, 24, true},
		{"jüngstes backup frisch", mk(2 * time.Hour), 24, false},
		{"jüngstes backup älter als intervall", mk(30 * time.Hour), 24, true},
		{"exakt am intervall", mk(24 * time.Hour), 24, true},
		{"kurzes intervall", mk(90 * time.Minute), 1, true},
	}
	for _, tc := range cases {
		if got := backupOverdue(tc.backups, tc.hours, now); got != tc.want {
			t.Errorf("%s: backupOverdue = %v, erwartet %v", tc.name, got, tc.want)
		}
	}
}

// TestBackupCronFesteUhrzeiten: teilt das Intervall den Tag, entstehen feste,
// aus backup_time abgeleitete Uhrzeiten - der @every-Anker wanderte mit jedem
// Einstellungs-Speichern und Neustart, und wer öfter speicherte, als das
// Intervall lang war, bekam nie ein Backup (R2-034).
func TestBackupCronFesteUhrzeiten(t *testing.T) {
	cases := []struct {
		hours int
		at    string
		want  string
	}{
		{24, "03:30", "30 3 * * *"},
		{12, "03:30", "30 3,15 * * *"},
		{6, "04:15", "15 4,10,16,22 * * *"},
		{1, "00:05", "5 0,1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23 * * *"},
		// Krumme Intervalle lassen sich nicht als feste Uhrzeit ausdrücken -
		// @every-Fallback, den der Nachhol-Watchdog absichert.
		{36, "03:30", "@every 36h"},
		{5, "03:30", "@every 5h"},
		// Ungültige Uhrzeit darf den Zeitplan nicht zerreißen.
		{24, "kaputt", "@every 24h"},
	}
	for _, tc := range cases {
		if got := backupCron(tc.hours, tc.at); got != tc.want {
			t.Errorf("backupCron(%d, %q) = %q, erwartet %q", tc.hours, tc.at, got, tc.want)
		}
	}
}
