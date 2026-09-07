// Package storage kapselt Datenbank-Verbindung und Migrationen.
package storage

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

// Open öffnet die SQLite-Datenbank (CGO-frei via modernc.org/sqlite).
// path kann ein Dateipfad oder ":memory:" (für Tests) sein.
func Open(path string) (*gorm.DB, error) {
	dsn := path
	if dsn == ":memory:" {
		// Auch In-Memory-DBs (Tests) erzwingen Foreign Keys, damit Tests
		// dieselben Constraint-Verletzungen aufdecken wie der Produktivbetrieb.
		dsn = ":memory:?_pragma=foreign_keys(1)"
	} else {
		// WAL + busy_timeout für stabilen Betrieb bei parallelen Requests.
		dsn = fmt.Sprintf("%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)", path)
	}
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		// ErrRecordNotFound ist bei uns ein normaler Fall (Repositories
		// übersetzen ihn in ErrNotFound) - nicht als Fehler loggen.
		Logger: logger.New(log.New(os.Stdout, "gorm: ", log.LstdFlags), logger.Config{
			LogLevel:                  logger.Warn,
			IgnoreRecordNotFoundError: true,
			SlowThreshold:             200 * time.Millisecond,
		}),
	})
	if err != nil {
		return nil, fmt.Errorf("datenbank öffnen: %w", err)
	}
	// Feldverschlüsselung auch auf dem Weg über Updates(map) - siehe
	// encryptMapAssignments. Muss an der Verbindung hängen, nicht an den
	// Aufrufstellen: Von denen gibt es über achtzig.
	if err := registerMapUpdateEncryption(db); err != nil {
		return nil, fmt.Errorf("feldverschlüsselung einhängen: %w", err)
	}
	if path != ":memory:" {
		// Die DB-Datei (samt WAL/SHM) enthält neben feldverschlüsselten
		// Secrets auch reichlich Klartext (Job-Output, SSH-Protokolle,
		// Hosts/IPs, Audit-Log) - daher strikt auf 0600, nicht dem umask
		// überlassen. Best effort: -wal/-shm entstehen erst beim ersten
		// Schreibzugriff, ein fehlender Chmod ist dann unkritisch.
		for _, suffix := range []string{"", "-wal", "-shm"} {
			_ = os.Chmod(path+suffix, 0o600)
		}
	}
	if path == ":memory:" {
		// Eine In-Memory-DB ist bei modernc/sqlite pro Verbindung privat.
		// Ohne Begrenzung auf EINE Verbindung sähen nebenläufige Goroutinen
		// (z.B. asynchrone Jobs in Tests) unterschiedliche, leere DBs. Für
		// Tests unkritisch; der Produktivbetrieb nutzt eine Datei-DB.
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.SetMaxOpenConns(1)
		}
	}
	return db, nil
}

// Migrate führt die GORM-AutoMigrationen für alle Entitäten aus und
// zieht anschließend Alt-Datenbestände nach (siehe migrate_rules.go).
// Neue Entitäten müssen hier registriert werden.
func Migrate(db *gorm.DB) error {
	// Strukturelle Vor-Migrationen, die AutoMigrate nicht leisten kann
	// (z.B. den Typ eines Primärschlüssels ändern), müssen VOR AutoMigrate
	// laufen - sonst gliche es die neuen Modelle gegen das Alt-Schema ab.
	if err := migrateToUUID(db); err != nil {
		return err
	}
	if err := migrateAlertEventsToUUID(db); err != nil {
		return err
	}
	if err := addServerRefColumns(db); err != nil {
		return err
	}
	if err := autoMigrateSchema(db); err != nil {
		return err
	}
	if err := migrateEncryptUserFields(db); err != nil {
		return err
	}
	if err := migrateEncryptLinuxUserFields(db); err != nil {
		return err
	}
	if err := migrateAlertRuleGroups(db); err != nil {
		return err
	}
	if err := migrateGroupPriorities(db); err != nil {
		return err
	}
	if err := migrateRulesToSchedules(db); err != nil {
		return err
	}
	// Server-Namen at rest verschlüsseln + Blindindex befüllen (self-healing:
	// nur noch nicht migrierte Zeilen). Läuft hier statt als versionierte
	// Migration, weil letztere bei Neuinstallation als Baseline übersprungen
	// würde - der Unique-Index auf name_bidx muss aber immer entstehen.
	if err := encryptServerNames(db); err != nil {
		return err
	}
	// Firewall-/Port-Felder der Server at rest verschlüsseln (self-healing,
	// idempotent über Decrypt-Probe).
	if err := migrateEncryptServerFirewall(db); err != nil {
		return err
	}
	// Baustein-Parameter auf englische Namen ziehen. MUSS vor dem Seeden
	// laufen: Das Seeden schreibt den mitgelieferten Bausteinen die neuen
	// Namen, kennt aber die Verwendungen nicht - liefe es zuerst, fände die
	// Migration keinen deutschen Namen mehr und die Werte blieben zurück.
	if err := migrateBlockParamsToEnglish(db); err != nil {
		return err
	}
	// Fremdschlüssel CVE/Paket→Server tokenisieren (server_id → server_ref).
	return migrateTokenizeServerRefs(db)
}

// encField verschlüsselt einen Wert mit dem At-Rest-Cipher (leer bleibt leer,
// ohne Cipher - schlanke Tests - bleibt der Klartext).
func encField(s string) (string, error) {
	if s == "" || fieldCipher == nil {
		return s, nil
	}
	return fieldCipher.EncryptString(s)
}

// migrateEncryptUserFields überführt bestehende Klartext-Benutzerfelder
// (Username, E-Mail, Vor-/Nachname, Passwort-Hash) in die verschlüsselte
// Ablage und befüllt die Blindindizes; danach werden die alten Klartext-
// Indizes durch Unique-Indizes auf den Blindindizes ersetzt. Idempotent
// (nur Zeilen ohne username_bidx) und transaktional.
func migrateEncryptUserFields(db *gorm.DB) error {
	if err := db.Transaction(func(tx *gorm.DB) error {
		var rows []struct {
			ID           uint
			Username     string
			Email        string
			FirstName    string
			LastName     string
			PasswordHash string
		}
		if err := tx.Raw("SELECT id, username, email, first_name, last_name, password_hash FROM users WHERE username_bidx IS NULL OR username_bidx = ''").
			Scan(&rows).Error; err != nil {
			return err
		}
		for _, r := range rows {
			ubidx := repositories.BlindIndex(r.Username)
			ebidx := ""
			if strings.TrimSpace(r.Email) != "" {
				ebidx = repositories.BlindIndex(r.Email)
			}
			enc := map[string]string{}
			for k, v := range map[string]string{
				"username": r.Username, "email": r.Email, "first_name": r.FirstName,
				"last_name": r.LastName, "password_hash": r.PasswordHash,
			} {
				e, err := encField(v)
				if err != nil {
					return err
				}
				enc[k] = e
			}
			if err := tx.Exec(
				"UPDATE users SET username=?, email=?, first_name=?, last_name=?, password_hash=?, username_bidx=?, email_bidx=? WHERE id=?",
				enc["username"], enc["email"], enc["first_name"], enc["last_name"], enc["password_hash"], ubidx, ebidx, r.ID,
			).Error; err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	for _, stmt := range []string{
		"DROP INDEX IF EXISTS idx_users_username", // alter Klartext-Unique-Index
		"DROP INDEX IF EXISTS idx_users_email_ci", // alter partieller E-Mail-Index
		"DROP INDEX IF EXISTS idx_users_email",    // ganz alter E-Mail-Index (Vor-CI)
		"CREATE UNIQUE INDEX IF NOT EXISTS idx_users_username_bidx ON users(username_bidx)",
		"CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email_bidx ON users(email_bidx) WHERE email_bidx <> ''",
	} {
		if err := db.Exec(stmt).Error; err != nil {
			return err
		}
	}
	return nil
}

// migrateEncryptLinuxUserFields verschlüsselt Username/FullName/E-Mail der
// Linux-Benutzer und ersetzt den Klartext-Unique-Index durch einen auf dem
// Username-Blindindex. Idempotent (nur Zeilen ohne username_bidx).
func migrateEncryptLinuxUserFields(db *gorm.DB) error {
	if err := db.Transaction(func(tx *gorm.DB) error {
		var rows []struct {
			ID       uint
			Username string
			FullName string
			Email    string
		}
		if err := tx.Raw("SELECT id, username, full_name, email FROM linux_users WHERE username_bidx IS NULL OR username_bidx = ''").
			Scan(&rows).Error; err != nil {
			return err
		}
		for _, r := range rows {
			bidx := repositories.BlindIndex(r.Username)
			u, err := encField(r.Username)
			if err != nil {
				return err
			}
			f, err := encField(r.FullName)
			if err != nil {
				return err
			}
			e, err := encField(r.Email)
			if err != nil {
				return err
			}
			if err := tx.Exec("UPDATE linux_users SET username=?, full_name=?, email=?, username_bidx=? WHERE id=?",
				u, f, e, bidx, r.ID).Error; err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	for _, stmt := range []string{
		"DROP INDEX IF EXISTS idx_linux_users_username",
		"CREATE UNIQUE INDEX IF NOT EXISTS idx_linux_users_username_bidx ON linux_users(username_bidx)",
	} {
		if err := db.Exec(stmt).Error; err != nil {
			return err
		}
	}
	return nil
}

// spaetVerschluesselteServerSpalten dürfen beim Nachverschlüsseln NICHT im
// ersten Durchgang mitlaufen: Die v0.3.0-Migration leitet die Paketverwaltung
// per `LIKE` aus os_name/os_id ab und braucht dort noch Klartext. Sie werden
// erst nach den versionierten Migrationen behandelt
// (EncryptServerProfileFields, aufgerufen aus main.go).
//
// Alles ANDERE ergibt sich aus dem Schema - siehe serverEncColumns.
var spaetVerschluesselteServerSpalten = []string{
	"os_name", "os_version", "os_id", "os_version_id",
	"kernel_version", "installed_kernels", "cpu_model",
}

// serverEncColumns liefert die nachzuverschlüsselnden Server-Spalten für einen
// der beiden Durchgänge. Die Grundmenge kommt aus dem GORM-Schema, damit hier
// nichts mehr fehlen kann - zuvor standen dieselben Spalten in drei von Hand
// gepflegten Listen, und jede war unvollständig.
//
// "name" bleibt außen vor: Der Server-Name trägt einen Blindindex und wird
// samt Index von encryptServerNames behandelt.
func serverEncColumns(db *gorm.DB, spaet bool) ([]string, error) {
	alle, err := aesgcmColumns(db, &domain.Server{})
	if err != nil {
		return nil, err
	}
	istSpaet := make(map[string]bool, len(spaetVerschluesselteServerSpalten))
	for _, s := range spaetVerschluesselteServerSpalten {
		istSpaet[s] = true
	}
	var spalten []string
	for _, s := range alle {
		if s == "name" || istSpaet[s] != spaet {
			continue
		}
		spalten = append(spalten, s)
	}
	return spalten, nil
}

// encryptServerColumns verschlüsselt bestehende Klartext-Werte der genannten
// Server-Spalten. Idempotent über eine Decrypt-Probe: Was sich entschlüsseln
// lässt, ist bereits verschlüsselt und bleibt unberührt.
func encryptServerColumns(db *gorm.DB, spalten []string) error {
	if fieldCipher == nil {
		return nil
	}
	return db.Transaction(func(tx *gorm.DB) error {
		for _, col := range spalten {
			var rows []struct {
				ID uint
				V  string
			}
			if err := tx.Raw(fmt.Sprintf("SELECT id, %s AS v FROM servers WHERE %s IS NOT NULL AND %s != ''", col, col, col)).
				Scan(&rows).Error; err != nil {
				return err
			}
			for _, r := range rows {
				if _, err := fieldCipher.DecryptString(r.V); err == nil {
					continue // bereits verschlüsselt
				}
				enc, err := fieldCipher.EncryptString(r.V)
				if err != nil {
					return err
				}
				if err := tx.Exec(fmt.Sprintf("UPDATE servers SET %s = ? WHERE id = ?", col), enc, r.ID).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// migrateEncryptServerFirewall verschlüsselt bestehende Klartext-Werte aller
// Server-Spalten, die nicht auf den späten Durchgang warten müssen. Ohne
// Cipher (Tests) ein No-Op.
func migrateEncryptServerFirewall(db *gorm.DB) error {
	spalten, err := serverEncColumns(db, false)
	if err != nil {
		return err
	}
	return encryptServerColumns(db, spalten)
}

// EncryptServerProfileFields verschlüsselt bestehende Klartext-Werte der OS-/
// Kernel-/CPU-Profilfelder (idempotent via Decrypt-Probe). Wird BEWUSST erst
// NACH den versionierten Migrationen aufgerufen (main.go): die v0.3.0-Migration
// leitet die Paketverwaltung per `LIKE` aus os_name/os_id ab und braucht dort
// noch Klartext. Ohne Cipher (Tests) ein No-Op.
func EncryptServerProfileFields(db *gorm.DB) error {
	spalten, err := serverEncColumns(db, true)
	if err != nil {
		return err
	}
	return encryptServerColumns(db, spalten)
}

// addServerRefColumns legt die Spalte server_ref in den Kindtabellen an, BEVOR
// AutoMigrate sie anfassen kann.
//
// Grund: das Modell führt server_ref als `not null` ohne Default. AutoMigrate
// setzt das 1:1 in „ALTER TABLE packages ADD server_ref text NOT NULL" um -
// und SQLite lehnt das an einer gefüllten Tabelle ab („Cannot add a NOT NULL
// column with default value NULL"). Auf einer bestehenden Installation
// scheiterte damit der Start, noch bevor migrateTokenizeServerRefs die Werte
// hätte nachtragen können; nur eine leere Datenbank kam durch.
//
// Deshalb hier von Hand mit DEFAULT ” anlegen: AutoMigrate findet die Spalte
// danach vor und lässt sie in Ruhe, und die leeren Werte füllt
// migrateTokenizeServerRefs unmittelbar danach aus server_id.
func addServerRefColumns(db *gorm.DB) error {
	for _, table := range []string{"packages", "vulnerabilities"} {
		if !tableExists(db, table) || columnExists(db, table, "server_ref") {
			continue
		}
		if err := db.Exec(fmt.Sprintf(
			"ALTER TABLE %s ADD COLUMN server_ref text NOT NULL DEFAULT ''", table)).Error; err != nil {
			return fmt.Errorf("%s.server_ref anlegen: %w", table, err)
		}
		// Sofort befüllen, nicht erst in migrateTokenizeServerRefs: mit
		// DEFAULT '' stünden sonst ALLE Zeilen auf demselben Wert, und der
		// Unique-Index (server_ref, name), den AutoMigrate gleich danach
		// anlegt, kollidiert beim ersten gleichnamigen Paket auf zwei Servern.
		if !columnExists(db, table, "server_id") {
			continue // frisch angelegt - es gibt nichts nachzutragen
		}
		var ids []uint
		if err := db.Raw(fmt.Sprintf("SELECT DISTINCT server_id FROM %s", table)).Scan(&ids).Error; err != nil {
			return fmt.Errorf("%s.server_id lesen: %w", table, err)
		}
		for _, id := range ids {
			if err := db.Exec(fmt.Sprintf("UPDATE %s SET server_ref = ? WHERE server_id = ?", table),
				repositories.ServerRef(id), id).Error; err != nil {
				return fmt.Errorf("%s.server_ref befüllen: %w", table, err)
			}
		}
	}
	return nil
}

// tableExists meldet, ob eine Tabelle existiert (SQLite). Bei einer
// Neuinstallation gibt es sie noch nicht - dann legt AutoMigrate sie samt
// Spalte an und die Vor-Migration hat nichts zu tun.
func tableExists(db *gorm.DB, table string) bool {
	var n int
	if err := db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?", table).
		Scan(&n).Error; err != nil {
		return false
	}
	return n > 0
}

// migrateTokenizeServerRefs ersetzt den Klartext-Fremdschlüssel server_id in
// vulnerabilities/packages durch das deterministische Server-Token server_ref
// (HMAC), sodass die DB die Zuordnung CVE/Paket→Server nicht mehr direkt
// preisgibt. servers.ref wird befüllt und eindeutig indexiert (partiell -
// der Wert wird erst im AfterCreate-Hook gesetzt, ist beim Insert kurz leer).
func migrateTokenizeServerRefs(db *gorm.DB) error {
	if err := db.Transaction(func(tx *gorm.DB) error {
		var ids []uint
		if err := tx.Raw("SELECT id FROM servers WHERE ref IS NULL OR ref = ''").Scan(&ids).Error; err != nil {
			return err
		}
		for _, id := range ids {
			if err := tx.Exec("UPDATE servers SET ref = ? WHERE id = ?", repositories.ServerRef(id), id).Error; err != nil {
				return err
			}
		}
		return tx.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_servers_ref ON servers(ref) WHERE ref <> ''").Error
	}); err != nil {
		return err
	}
	if err := tokenizeChildTable(db, "vulnerabilities", "idx_vuln_server",
		"CREATE INDEX IF NOT EXISTS idx_vuln_server ON vulnerabilities(server_ref)"); err != nil {
		return err
	}
	return tokenizeChildTable(db, "packages", "idx_pkg_server_name",
		"CREATE UNIQUE INDEX IF NOT EXISTS idx_pkg_server_name ON packages(server_ref, name)")
}

// tokenizeChildTable befüllt server_ref aus dem (noch vorhandenen) server_id,
// entfernt den alten Index und die server_id-Spalte und legt den neuen Index
// auf server_ref an. No-Op, sobald server_id bereits entfernt ist (idempotent).
func tokenizeChildTable(db *gorm.DB, table, oldIndex, createIndexSQL string) error {
	if !columnExists(db, table, "server_id") {
		return nil
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var rows []struct {
			ID       string
			ServerID uint
		}
		if err := tx.Raw(fmt.Sprintf("SELECT CAST(id AS TEXT) AS id, server_id FROM %s WHERE server_ref IS NULL OR server_ref = ''", table)).
			Scan(&rows).Error; err != nil {
			return err
		}
		for _, r := range rows {
			if err := tx.Exec(fmt.Sprintf("UPDATE %s SET server_ref = ? WHERE id = ?", table),
				repositories.ServerRef(r.ServerID), r.ID).Error; err != nil {
				return err
			}
		}
		if err := tx.Exec("DROP INDEX IF EXISTS " + oldIndex).Error; err != nil {
			return err
		}
		if err := tx.Exec(fmt.Sprintf("ALTER TABLE %s DROP COLUMN server_id", table)).Error; err != nil {
			return err
		}
		return tx.Exec(createIndexSQL).Error
	})
}

// columnExists meldet, ob eine Spalte in einer Tabelle existiert (SQLite).
func columnExists(db *gorm.DB, table, col string) bool {
	var n int
	if err := db.Raw(fmt.Sprintf("SELECT COUNT(*) FROM pragma_table_info('%s') WHERE name = ?", table), col).
		Scan(&n).Error; err != nil {
		return false
	}
	return n > 0
}

// encryptServerNames überführt bestehende Klartext-Servernamen in die
// verschlüsselte Ablage: pro noch nicht migrierter Zeile (name_bidx leer)
// wird der Blindindex gesetzt und der Name at rest verschlüsselt; danach der
// Unique-Index auf name_bidx angelegt. Idempotent und transaktional - bricht
// etwas ab, bleibt der Klartext unangetastet.
func encryptServerNames(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		var rows []struct {
			ID   uint
			Name string
		}
		if err := tx.Raw("SELECT id, name FROM servers WHERE name_bidx IS NULL OR name_bidx = ''").
			Scan(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			bidx := repositories.BlindIndex(row.Name)
			enc := row.Name
			if fieldCipher != nil {
				var err error
				if enc, err = fieldCipher.EncryptString(row.Name); err != nil {
					return err
				}
			}
			if err := tx.Exec("UPDATE servers SET name = ?, name_bidx = ? WHERE id = ?",
				enc, bidx, row.ID).Error; err != nil {
				return err
			}
		}
		return tx.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_servers_name_bidx ON servers(name_bidx)").Error
	})
}

// autoMigrateSchema fuehrt AutoMigrate mit ausgesetzter Fremdschluessel-
// Pruefung aus.
//
// SQLite kann Constraints nicht per ALTER TABLE ergaenzen. Kommt an einem
// Modell ein Fremdschluessel dazu, baut GORM die Tabelle deshalb neu: Kopie
// anlegen, Daten umkopieren, alte Tabelle loeschen, umbenennen. Genau dieses
// DROP TABLE scheitert bei scharfer Pruefung, sobald eine andere Tabelle auf
// die neu gebaute zeigt und Zeilen haelt - der Start bricht dann mit
// "FOREIGN KEY constraint failed" ab.
//
// Auf einer leeren Datenbank passiert das nie, weil die Tabelle gleich mit
// Constraint entsteht. Es trifft ausschliesslich bestehende Installationen
// beim Update - und lief deshalb an allen Tests vorbei, die gegen frische
// Datenbanken laufen (siehe TestMigrateBestandsdatenbank).
func autoMigrateSchema(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	// PRAGMA-Einstellungen gelten pro Verbindung. Ohne Begrenzung auf EINE
	// Verbindung landete ein Teil der Migrations-Anweisungen auf einer
	// anderen, auf der die Pruefung noch scharf ist - der Fehler traete dann
	// sporadisch auf, was schlimmer waere als zuverlaessig. Waehrend der
	// Migration laeuft ohnehin nichts nebenher.
	// Stats().MaxOpenConnections liefert 0 fuer "unbegrenzt"; SetMaxOpenConns(0)
	// stellt genau das wieder her.
	vorher := sqlDB.Stats().MaxOpenConnections
	defer sqlDB.SetMaxOpenConns(vorher)
	sqlDB.SetMaxOpenConns(1)

	if err := db.Exec("PRAGMA foreign_keys = OFF").Error; err != nil {
		return fmt.Errorf("fremdschluessel-pruefung aussetzen: %w", err)
	}
	migErr := autoMigrate(db)
	if err := db.Exec("PRAGMA foreign_keys = ON").Error; err != nil && migErr == nil {
		migErr = fmt.Errorf("fremdschluessel-pruefung wieder einschalten: %w", err)
	}
	if migErr != nil {
		return migErr
	}
	return checkForeignKeys(db)
}

// pruefeFremdschluessel rechnet nach, was die ausgesetzte Pruefung waehrend
// des Tabellen-Neuaufbaus durchgelassen haben koennte. Lieber hier mit klarer
// Aussage abbrechen als mit einer stillschweigend inkonsistenten Datenbank
// weiterlaufen.
func checkForeignKeys(db *gorm.DB) error {
	var verletzungen []struct {
		Table  string `gorm:"column:table"`
		Parent string `gorm:"column:parent"`
	}
	if err := db.Raw("PRAGMA foreign_key_check").Scan(&verletzungen).Error; err != nil {
		return fmt.Errorf("fremdschluessel pruefen: %w", err)
	}
	if len(verletzungen) == 0 {
		return nil
	}
	betroffen := make([]string, 0, len(verletzungen))
	for _, v := range verletzungen {
		betroffen = append(betroffen, v.Table+" -> "+v.Parent)
	}
	return fmt.Errorf("migration hinterliess %d verwaiste zeile(n): %s",
		len(verletzungen), strings.Join(betroffen, ", "))
}

// migratedModels sind alle Entitäten, die AutoMigrate anlegt. Als eigene
// Liste, damit auch Prüfungen darüber laufen können - etwa, ob jedes
// verschlüsselte Feld für die Schlüsselrotation registriert ist.
// Neue Entitäten gehören hierher.
var migratedModels = []any{
	&domain.HealthProbe{},
	&domain.User{},
	&domain.Role{},
	&domain.Permission{},
	&domain.APIKey{},
	&domain.Server{},
	&domain.ServerGroup{},
	&domain.Schedule{},
	&domain.Rule{},
	&domain.Job{},
	&domain.SSHSession{},
	&domain.SSHCommand{},
	&domain.AuditLog{},
	&domain.Package{},
	&domain.SnapPackage{},
	&domain.DockerContainer{},
	&domain.DockerImage{},
	&domain.Vulnerability{},
	&domain.DeepScanReport{},
	&domain.DeepScanFinding{},
	&domain.StorageHistory{},
	&domain.DiskVolume{},
	&domain.ServerUser{},
	&domain.ServerUserBlock{},
	&domain.ServerUserLogin{},
	&domain.AptRepository{},
	&domain.KnownRepo{},
	&domain.AppCatalogEntry{},
	&domain.DetectedApp{},
	&domain.UnknownApp{},
	&domain.PackagePin{},
	&domain.IPAllowlist{},
	&domain.LinuxUser{},
	&domain.LinuxUserSSHKey{},
	&domain.LinuxUserActivation{},
	&domain.PrivilegeProfile{},
	&domain.ProfileSudoRule{},
	&domain.ProfileEditRule{},
	&domain.ProfilePathRule{},
	&domain.ProfileBlock{},
	&domain.ProfileBlockVariant{},
	&domain.ProfileBlockUse{},
	&domain.AppliedProfilePath{},
	&domain.HardenedPath{},
	// Die beiden Zuordnungstabellen bestehen bereits als
	// many2many-Verknüpfung; AutoMigrate ergänzt hier nur die Spalte
	// profile_id. Deshalb MÜSSEN sie nach LinuxUser stehen.
	&domain.ServerLinuxUser{},
	&domain.ServerGroupLinuxUser{},
	&domain.ActivationLink{},
	&domain.StorageHealth{},
	&domain.VolumeMonitor{},
	&domain.GlobalSettings{},
	&domain.Backup{},
	&domain.CustomAction{},
	&domain.NotificationChannel{},
	&domain.AlertRule{},
	&domain.PendingUserSync{},
	&domain.AlertEvent{},
	&domain.AdvisoryFinding{},
	&domain.AdvisoryCacheEntry{},
	&domain.AdvisoryDetail{},
	&domain.AdvisoryCacheStats{},
}

func autoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(migratedModels...)
}
