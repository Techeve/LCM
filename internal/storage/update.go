package storage

import (
	"fmt"
	"log/slog"
	"sort"
	"time"

	"gorm.io/gorm"

	"LCM/internal/storage/migrations"
	"LCM/internal/version"
)

// Der Update-Prozess des Templates.
//
// Jede Installation führt eine Versionsdatei (version.json) neben der
// Datenbank. Beim Start läuft RunUpdateMigrations und vergleicht die dort
// hinterlegte (= zuletzt gelaufene) Version mit der Version des Binaries:
//
//   - Erststart (keine DB, keine Versionsdatei): Die Datei wird mit der
//     aktuellen Version angelegt. Migrationen laufen NICHT - Schema und
//     Seeding sind bereits auf dem neuesten Stand; die anwendbaren
//     Migrationen werden als Baseline protokolliert.
//   - Gleiche Version: nichts zu tun (Ausnahme: noch nicht protokollierte
//     Migrationen, z.B. während der Entwicklung - die laufen nach).
//   - Unterschiedliche Version (Binary wurde aktualisiert): Alle noch
//     nicht gelaufenen Migrationsskripte bis zur neuen Version werden in
//     SemVer-Reihenfolge ausgeführt (erst 1.1.0, dann 1.2.0, ...) - jede
//     in einer eigenen Transaktion. Danach wird die Versionsdatei
//     fortgeschrieben.
//
// Übersprungene Versionen sind kein Problem: Bei einem Update von 1.0.0
// direkt auf 1.3.0 laufen die Skripte für 1.1.0, 1.2.0 und 1.3.0
// nacheinander. Eine Installation, die schon auf 1.3.0 stand, führt beim
// Update auf 1.4.0 nur noch das 1.4.0-Skript aus.
//
// Die Migrationsskripte selbst liegen im Migrationsordner
// internal/storage/migrations/ - eine Datei pro Version.

// appliedMigration protokolliert ausgeführte Migrationen in der DB -
// die zweite Sicherung neben der Versionsdatei: Selbst wenn die Datei
// verloren geht, läuft kein Skript doppelt.
type appliedMigration struct {
	ID        uint      `gorm:"primarykey"`
	Name      string    `gorm:"uniqueIndex;not null"`
	Version   string    `gorm:"not null"` // Ziel-Version der Migration
	Baseline  bool      // true: bei Erstinstallation übersprungen
	AppliedAt time.Time `gorm:"not null"`
}

func (appliedMigration) TableName() string { return "update_migrations" }

// UpdateOptions steuert RunUpdateMigrations.
type UpdateOptions struct {
	// VersionFilePath ist der Pfad zur version.json der Installation
	// (üblicherweise im Verzeichnis der Datenbank).
	VersionFilePath string
	// FreshInstall: true, wenn die Datenbank bei diesem Start neu
	// angelegt wurde (vorher keine DB-Datei vorhanden).
	FreshInstall bool
}

// UpdateResult hält fest, was der Start vorgefunden hat. Die Angabe ist über
// den Migrationslauf hinaus interessant: Nur wenn das Binary in einer anderen
// Version läuft als beim letzten Start, kann ein noch offener Job durch das
// eigene Paket-Update unterbrochen worden sein (siehe
// JobService.FailInterruptedOnStartup).
type UpdateResult struct {
	// PreviousVersion ist die Version aus der Versionsdatei - leer bei einer
	// Erstinstallation und bei Ständen aus der Zeit vor der Datei.
	PreviousVersion string
	// Updated: dieser Start läuft in einer anderen Version als der letzte.
	Updated bool
}

// RunUpdateMigrations ist der Update-Prozess beim Anwendungsstart.
// Muss nach Migrate() und vor Seed() laufen.
func RunUpdateMigrations(db *gorm.DB, opts UpdateOptions) (UpdateResult, error) {
	var result UpdateResult
	if err := db.AutoMigrate(&appliedMigration{}); err != nil {
		return result, fmt.Errorf("migrations-tabelle anlegen: %w", err)
	}

	installed, err := version.ReadInstalled(opts.VersionFilePath)
	if err != nil {
		return result, err
	}
	current := version.Version
	if installed != nil {
		result.PreviousVersion = installed.Version
		result.Updated = installed.Version != current
	}

	// isFuture: Migration gehört zu einer späteren Version als das
	// Binary (kann passieren, wenn ein Skript vor seinem Release schon
	// im Code liegt). Dev-Builds ("0.0.0-dev") haben keine Obergrenze.
	isFuture := func(m migrations.Migration) bool {
		return version.IsRelease(current) && version.Compare(m.Version, current) > 0
	}

	// Fall 1: Erstinstallation - Versionsdatei anlegen und die schon
	// anwendbaren Migrationen als Baseline markieren (die frische DB ist
	// bereits auf aktuellem Stand). Zukünftige Skripte bleiben offen,
	// damit sie beim späteren Update regulär laufen.
	if installed == nil && opts.FreshInstall {
		slog.Info("first installation - creating version file",
			"version", current, "file", opts.VersionFilePath)
		for _, m := range migrations.Registry {
			if isFuture(m) {
				continue
			}
			entry := appliedMigration{
				Name: m.Name, Version: m.Version,
				Baseline: true, AppliedAt: time.Now(),
			}
			if err := db.Create(&entry).Error; err != nil {
				return result, fmt.Errorf("baseline %s: %w", m.Name, err)
			}
		}
		return result, version.WriteInstalled(opts.VersionFilePath)
	}

	// Fall 2: Update-Erkennung.
	switch {
	case installed == nil:
		// DB existiert, aber keine Versionsdatei: Update von einem Stand
		// vor Einführung der Versionsdatei - alle Migrationen prüfen.
		slog.Info("update detected (installation without version file)",
			"to", current)
	case installed.Version != current:
		direction := "update"
		if version.Compare(current, installed.Version) < 0 {
			// Downgrade: nicht unterstützt, aber nicht fatal - loggen.
			direction = "downgrade (not supported!)"
		}
		slog.Info(direction+" detected - checking migrations",
			"from", installed.Version, "to", current)
	default:
		// Gleiche Version: normalerweise nichts zu tun; unprotokollierte
		// Migrationen (Entwicklung) laufen trotzdem nach.
	}

	// Bereits gelaufene Migrationen laden (Schutz vor Doppelausführung).
	var applied []appliedMigration
	if err := db.Find(&applied).Error; err != nil {
		return result, err
	}
	done := map[string]bool{}
	for _, m := range applied {
		done[m.Name] = true
	}

	// Ausstehende Migrationen: noch nicht gelaufen UND nicht zukünftig.
	var pending []migrations.Migration
	for _, m := range migrations.Registry {
		if done[m.Name] {
			continue
		}
		if isFuture(m) {
			slog.Debug("migration skipped (future version)",
				"name", m.Name, "version", m.Version)
			continue
		}
		pending = append(pending, m)
	}

	// In SemVer-Reihenfolge ausführen (erst 1.1.0, dann 1.2.0, ...),
	// jede in eigener Transaktion - Skript + Protokoll-Eintrag atomar.
	sort.SliceStable(pending, func(i, j int) bool {
		return version.Compare(pending[i].Version, pending[j].Version) < 0
	})
	for _, m := range pending {
		slog.Info("running update migration", "version", m.Version, "name", m.Name)
		err := db.Transaction(func(tx *gorm.DB) error {
			if err := m.Run(tx); err != nil {
				return err
			}
			return tx.Create(&appliedMigration{
				Name: m.Name, Version: m.Version, AppliedAt: time.Now(),
			}).Error
		})
		if err != nil {
			return result, fmt.Errorf("migration %s (%s): %w", m.Name, m.Version, err)
		}
	}
	if len(pending) > 0 {
		slog.Info("update migrations finished", "count", len(pending))
	}

	// Versionsdatei auf den aktuellen Stand bringen.
	return result, version.WriteInstalled(opts.VersionFilePath)
}
