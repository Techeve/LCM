package storage

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/gorm"

	"LCM/internal/version"
)

// Der Speicher der Datenbank wird im nächtlichen Bereinigungslauf verdichtet
// (CompactDatabase), nicht beim Start: Auf einer gewachsenen Installation
// dauert beides Minuten, und ein Dienst, der so lange nicht hochkommt, fällt
// systemd und dem Betreiber zur Unzeit auf.
//
// Zwei Schritte:
//
//  1. Altbestand nachkomprimieren (einmalig): Werte aus der Zeit vor
//     fieldpack.go liegen unkomprimiert verschlüsselt vor. Sie werden in
//     kleinen Transaktionen neu geschrieben, damit das WAL klein bleibt und
//     der Dienst dazwischen weiterarbeitet. Ist der Durchgang komplett, hält
//     ein Eintrag in update_migrations das fest - der Aufwand fällt kein
//     zweites Mal an.
//  2. VACUUM, wenn es sich lohnt: Gelöschte Zeilen geben ihre Seiten nur
//     innerhalb der Datei frei, die Datei selbst schrumpft ohne VACUUM nie.

// packMigrationName markiert den abgeschlossenen Nachkomprimier-Durchgang.
const packMigrationName = "pack-log-fields"

// packBatchSize ist die Zahl der Zeilen je Transaktion.
const packBatchSize = 200

// packMinStoredBytes ist die gespeicherte Länge (Base64 von Nonce, Siegel und
// Tag), unterhalb derer ein Wert gar nicht erst packMinBytes erreicht haben
// kann - kleinere Zeilen muss der Durchgang nicht entschlüsseln.
const packMinStoredBytes = packMinBytes * 4 / 3

// vacuumFreeShare ist der Anteil freier Seiten, ab dem VACUUM läuft. Darunter
// füllen neue Zeilen die Lücken ohnehin wieder auf.
const vacuumFreeShare = 0.25

// CompactResult berichtet, was CompactDatabase getan hat.
type CompactResult struct {
	Packed      int   // nachkomprimierte Werte
	BytesBefore int64 // Dateigröße vor dem VACUUM (0 = kein VACUUM)
	BytesAfter  int64
}

// CompactDatabase komprimiert den Altbestand nach und verkleinert die Datei.
// freeBytes liefert den freien Platz im Datenverzeichnis (ok=false: nicht
// ermittelbar) - VACUUM braucht vorübergehend etwa das Doppelte der Nutzdaten.
func CompactDatabase(db *gorm.DB, freeBytes func() (uint64, bool)) (CompactResult, error) {
	var res CompactResult
	packed, err := packLegacyFields(db)
	res.Packed = packed
	if err != nil {
		return res, err
	}
	before, after, err := vacuumIfWorthwhile(db, freeBytes)
	res.BytesBefore, res.BytesAfter = before, after
	return res, err
}

// packLegacyFields schreibt unkomprimierte große Werte komprimiert neu.
func packLegacyFields(db *gorm.DB) (int, error) {
	if fieldCipher == nil {
		return 0, nil
	}
	var done int64
	if err := db.Model(&appliedMigration{}).Where("name = ?", packMigrationName).Count(&done).Error; err != nil {
		return 0, err
	}
	if done > 0 {
		return 0, nil
	}
	total := 0
	for _, col := range serializerColumns {
		n, err := packColumn(db, col.table, col.column)
		total += n
		if err != nil {
			return total, fmt.Errorf("%s.%s komprimieren: %w", col.table, col.column, err)
		}
	}
	slog.Info("legacy log fields packed", "values", total)
	return total, db.Create(&appliedMigration{
		Name: packMigrationName, Version: version.Version, AppliedAt: time.Now(),
	}).Error
}

// packColumn arbeitet eine Spalte in rowid-Reihenfolge ab - über den
// rowid-Baum, damit jeder Schritt dort weitermacht, wo der vorige aufhörte,
// statt die Tabelle erneut zu durchsuchen.
func packColumn(db *gorm.DB, table, column string) (int, error) {
	query := fmt.Sprintf("SELECT rowid AS row_id, %s AS value FROM %s WHERE rowid > ? AND length(%s) >= ? ORDER BY rowid LIMIT ?",
		column, table, column)
	update := fmt.Sprintf("UPDATE %s SET %s = ? WHERE rowid = ?", table, column)
	packed := 0
	var last int64
	for {
		var rows []struct {
			RowID int64
			Value string
		}
		if err := db.Raw(query, last, packMinStoredBytes, packBatchSize).Scan(&rows).Error; err != nil {
			return packed, err
		}
		if len(rows) == 0 {
			return packed, nil
		}
		err := db.Transaction(func(tx *gorm.DB) error {
			for _, r := range rows {
				value, ok, err := repackValue(r.Value)
				if err != nil {
					return err
				}
				if !ok {
					continue
				}
				if err := tx.Exec(update, value, r.RowID).Error; err != nil {
					return err
				}
				packed++
			}
			return nil
		})
		if err != nil {
			return packed, err
		}
		last = rows[len(rows)-1].RowID
	}
}

// repackValue liefert den komprimiert neu verschlüsselten Wert - ok=false,
// wenn nichts zu tun ist (Legacy-Klartext, schon komprimiert, nicht
// komprimierbar).
func repackValue(stored string) (string, bool, error) {
	plain, err := fieldCipher.DecryptString(stored)
	if err != nil || isPacked(plain) {
		return "", false, nil
	}
	packed := packField(plain)
	if packed == plain {
		return "", false, nil
	}
	value, err := fieldCipher.EncryptString(packed)
	return value, err == nil, err
}

// vacuumIfWorthwhile verkleinert die Datei, wenn genug Seiten frei sind und
// der Platz für den Umbau reicht. Liefert die Größe davor und danach (0, 0:
// kein VACUUM).
func vacuumIfWorthwhile(db *gorm.DB, freeBytes func() (uint64, bool)) (int64, int64, error) {
	pages, freePages, pageSize, err := pageStats(db)
	if err != nil || pages == 0 || float64(freePages)/float64(pages) < vacuumFreeShare {
		return 0, 0, err
	}
	// VACUUM baut die Nutzdaten in einer temporären Kopie neu auf und
	// schreibt sie im WAL-Modus über das WAL zurück - vorübergehend also
	// zweimal die Nutzdaten zusätzlich.
	need := uint64(pages-freePages) * uint64(pageSize) * 2
	if avail, ok := freeBytes(); ok && avail < need {
		slog.Warn("vacuum skipped - not enough free disk space", "need_bytes", need, "free_bytes", avail)
		return 0, 0, nil
	}
	before := pages * pageSize
	started := time.Now()
	if err := db.Exec("VACUUM").Error; err != nil {
		return 0, 0, fmt.Errorf("vacuum: %w", err)
	}
	// Das WAL hält nach dem VACUUM die komplette neue Datei - zurückschreiben
	// und kürzen, sonst liegt der gesparte Platz dort.
	if err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE)").Error; err != nil {
		return 0, 0, fmt.Errorf("wal checkpoint: %w", err)
	}
	pages, _, pageSize, err = pageStats(db)
	if err != nil {
		return 0, 0, err
	}
	slog.Info("database vacuumed", "bytes_before", before, "bytes_after", pages*pageSize, "took", time.Since(started))
	return before, pages * pageSize, nil
}

func pageStats(db *gorm.DB) (pages, freePages, pageSize int64, err error) {
	err = errors.Join(
		db.Raw("PRAGMA page_count").Scan(&pages).Error,
		db.Raw("PRAGMA freelist_count").Scan(&freePages).Error,
		db.Raw("PRAGMA page_size").Scan(&pageSize).Error,
	)
	return pages, freePages, pageSize, err
}

// String fasst das Ergebnis für den Bericht der Log-Bereinigung zusammen.
func (r CompactResult) String() string {
	s := fmt.Sprintf("datenbank: %d ältere einträge nachkomprimiert", r.Packed)
	if r.BytesBefore == 0 {
		return s + ", kein vacuum nötig"
	}
	return s + fmt.Sprintf(", vacuum %.1f → %.1f mb", float64(r.BytesBefore)/1e6, float64(r.BytesAfter)/1e6)
}
