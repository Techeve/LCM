package storage

import (
	"fmt"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"LCM/internal/infrastructure/crypto"
)

// encryptedColumns listet alle AES-GCM-verschlüsselten Spalten der
// Datenbank. Neue verschlüsselte Felder MÜSSEN hier registriert werden,
// damit die Master-Key-Rotation sie erfasst.
var encryptedColumns = []struct {
	table, pk, column string
}{
	{"servers", "id", "private_key_enc"},
	{"servers", "id", "login_password_enc"}, // RouterOS-Login-Passwort
	{"users", "id", "totp_secret_enc"},
	{"linux_users", "id", "password_enc"},
	{"global_settings", "id", "default_ssh_password_enc"},
	{"global_settings", "id", "tls_key_pem_enc"},
	{"global_settings", "id", "onboarding_key_enc"}, // System-Onboarding-SSH-Key
	{"global_settings", "id", "mail_password_enc"},  // System-Mailer (SMTP)
	// Achtung Spaltenname: gorm trennt CrowdSec → crowd_sec (anders als die
	// explizit gepinnten crowdsec_*-Spalten am Server-Modell).
	{"global_settings", "id", "crowd_sec_lapi_password_enc"}, // CrowdSec-LAPI-Maschinenkonto
	{"global_settings", "id", "crowd_sec_console_key_enc"},   // CrowdSec-Console-Key
	{"notification_channels", "id", "secret_enc"},            // SMTP-Passwort bzw. Webhook-URL
}

// serializerColumns sind die großvolumigen, über den `aesgcm`-GORM-Serializer
// verschlüsselten Felder (SSH-/Job-Konsolen-Output). Anders als
// encryptedColumns können sie Legacy-Klartext enthalten (Zeilen aus der Zeit
// vor Aktivierung der Verschlüsselung) - die Rotation ist hier daher TOLERANT:
// Zeilen, die sich mit dem alten Key nicht entschlüsseln lassen, werden
// übersprungen (der Klartext bleibt, der Serializer liest ihn per Fallback).
// Der Primärschlüssel ist eine Text-UUID.
var serializerColumns = []struct {
	table, pk, column string
}{
	{"jobs", "id", "output"},
	{"ssh_commands", "id", "command"},
	{"ssh_commands", "id", "output"},
	{"servers", "id", "host"},
	{"servers", "id", "ip_addresses"},
	// Firewall-/Port-Felder der Server.
	{"servers", "id", "firewall_allowed_ports"},
	{"servers", "id", "firewall_rules"},
	{"servers", "id", "firewall_ssh_sources"},
	{"servers", "id", "firewall_tool"},
	{"servers", "id", "listening_ports"},
	{"servers", "id", "listening_packages"},
	// OS-/Kernel-/CPU-Profilfelder der Server.
	{"servers", "id", "os_name"},
	{"servers", "id", "os_version"},
	{"servers", "id", "os_id"},
	{"servers", "id", "os_version_id"},
	{"servers", "id", "kernel_version"},
	{"servers", "id", "installed_kernels"},
	{"servers", "id", "cpu_model"},
	{"servers", "id", "hardware_model"},
	// Zustandsfelder, die der Scan schreibt.
	{"servers", "id", "rhsm_status"},
	{"servers", "id", "https_revert_urls"},
	// Benutzer-/Linux-Benutzer-Felder ohne Blindindex (die mit Blindindex -
	// username/email - laufen weiter unten als Sonderfall).
	{"users", "id", "first_name"},
	{"users", "id", "last_name"},
	{"users", "id", "password_hash"},
	{"linux_users", "id", "full_name"},
	{"linux_users", "id", "email"},
	// Auf den Servern vorgefundene Benutzer (Bestand, nicht die von LCM
	// verwalteten linux_users). Die Tabellen mit Blindindex auf dem Namen
	// laufen weiter unten als Sonderfall.
	{"server_users", "id", "username"},
	{"server_user_logins", "id", "from_host"},
	// Zustand der Speicher-Verbünde (ZFS/Btrfs/MD-RAID/LVM-Thin).
	{"storage_healths", "id", "name"},
	{"storage_healths", "id", "raw_state"},
	{"storage_healths", "id", "message"},
}

// blindIndexedColumns sind verschlüsselte Felder mit einem MITZUFÜHRENDEN
// Blindindex: bei der Rotation muss neben dem Ciphertext auch der Index mit
// dem neuen (aus dem Master-Key abgeleiteten) Schlüssel neu berechnet werden,
// sonst finden die Lookups (Login, E-Mail, Namenssuche) nichts mehr.
var blindIndexedColumns = []struct {
	table, column, bidxColumn string
}{
	{"servers", "name", "name_bidx"},
	{"users", "username", "username_bidx"},
	{"users", "email", "email_bidx"},
	{"linux_users", "username", "username_bidx"},
	// Benutzer-Bestand der Server: Sperren, Anmeldungen und ausstehende
	// Abgleiche werden über den Namen gesucht - ohne neuen Index nach der
	// Rotation fände die Sperrprüfung keinen gesperrten Benutzer mehr.
	{"server_user_blocks", "username", "username_bidx"},
	{"server_user_logins", "username", "username_bidx"},
	{"pending_user_syncs", "username", "username_bidx"},
}

// RotateEncryptedFields entschlüsselt alle verschlüsselten Spalten mit
// dem alten Master-Key und verschlüsselt sie mit dem neuen - transaktional,
// damit die DB nie in einem gemischten Zustand zurückbleibt (Grundlage
// des CLI-Befehls "lcm rotate-db-key").
func RotateEncryptedFields(db *gorm.DB, oldCipher, newCipher *crypto.Cipher) error {
	return db.Transaction(func(tx *gorm.DB) error {
		for _, col := range encryptedColumns {
			type row struct {
				ID    uint
				Value string
			}
			var rows []row
			query := fmt.Sprintf("SELECT %s AS id, %s AS value FROM %s WHERE %s IS NOT NULL AND %s != ''",
				col.pk, col.column, col.table, col.column, col.column)
			if err := tx.Raw(query).Scan(&rows).Error; err != nil {
				return fmt.Errorf("%s.%s lesen: %w", col.table, col.column, err)
			}
			for _, r := range rows {
				plain, err := oldCipher.DecryptString(r.Value)
				if err != nil {
					return fmt.Errorf("%s.%s (id %d) entschlüsseln: %w - falscher alter Key?",
						col.table, col.column, r.ID, err)
				}
				reenc, err := newCipher.EncryptString(plain)
				if err != nil {
					return err
				}
				if err := tx.Table(col.table).Where(col.pk+" = ?", r.ID).
					Update(col.column, reenc).Error; err != nil {
					return fmt.Errorf("%s.%s (id %d) schreiben: %w", col.table, col.column, r.ID, err)
				}
			}
		}

		// Tolerante Passage für die Serializer-Spalten (evtl. Legacy-Klartext):
		// nicht entschlüsselbare Zeilen werden übersprungen. Der PK wird als
		// TEXT gelesen (CAST), damit sowohl UUID-PKs (jobs/ssh_commands) als
		// auch INTEGER-PKs (servers) einheitlich funktionieren - SQLite bringt
		// den String-Wert im WHERE per Spalten-Affinität zurück zum Integer.
		for _, col := range serializerColumns {
			type row struct {
				ID    string
				Value string
			}
			var rows []row
			query := fmt.Sprintf("SELECT CAST(%s AS TEXT) AS id, %s AS value FROM %s WHERE %s IS NOT NULL AND %s != ''",
				col.pk, col.column, col.table, col.column, col.column)
			if err := tx.Raw(query).Scan(&rows).Error; err != nil {
				return fmt.Errorf("%s.%s lesen: %w", col.table, col.column, err)
			}
			for _, r := range rows {
				plain, err := oldCipher.DecryptString(r.Value)
				if err != nil {
					continue // Legacy-Klartext/Fremdformat - überspringen
				}
				reenc, err := newCipher.EncryptString(plain)
				if err != nil {
					return err
				}
				if err := tx.Table(col.table).Where(col.pk+" = ?", r.ID).
					Update(col.column, reenc).Error; err != nil {
					return fmt.Errorf("%s.%s (id %s) schreiben: %w", col.table, col.column, r.ID, err)
				}
			}
		}

		// Blindindex-Felder: neben der Re-Verschlüsselung muss auch der Index mit
		// dem NEUEN Schlüssel neu berechnet werden (er wird aus dem Master-Key
		// abgeleitet), sonst finden Login/E-Mail-/Namenssuche nach der Rotation
		// nichts mehr. Tolerant: Legacy-Klartext wird übersprungen.
		for _, col := range blindIndexedColumns {
			var rows []struct {
				ID    string
				Value string
			}
			q := fmt.Sprintf("SELECT CAST(%s AS TEXT) AS id, %s AS value FROM %s WHERE %s IS NOT NULL AND %s != ''",
				"id", col.column, col.table, col.column, col.column)
			if err := tx.Raw(q).Scan(&rows).Error; err != nil {
				return fmt.Errorf("%s.%s lesen: %w", col.table, col.column, err)
			}
			for _, r := range rows {
				plain, err := oldCipher.DecryptString(r.Value)
				if err != nil {
					continue // Legacy-Klartext - überspringen
				}
				reenc, err := newCipher.EncryptString(plain)
				if err != nil {
					return err
				}
				bidx := newCipher.BlindIndex(strings.ToLower(strings.TrimSpace(plain)))
				if err := tx.Exec(fmt.Sprintf("UPDATE %s SET %s = ?, %s = ? WHERE id = ?", col.table, col.column, col.bidxColumn),
					reenc, bidx, r.ID).Error; err != nil {
					return fmt.Errorf("%s.%s (id %s) schreiben: %w", col.table, col.column, r.ID, err)
				}
			}
		}

		// server_ref (HMAC der id) hängt ebenfalls am Master-Key und muss auf
		// den neuen Schlüssel umgeschrieben werden - in servers.ref UND in den
		// Kind-Tabellen. Die Zuordnung läuft über die (unveränderte) server_id.
		var serverIDs []uint
		if err := tx.Raw("SELECT id FROM servers").Scan(&serverIDs).Error; err != nil {
			return fmt.Errorf("server-ids lesen: %w", err)
		}
		for _, id := range serverIDs {
			base := "server:" + strconv.FormatUint(uint64(id), 10)
			oldRef := oldCipher.BlindIndex(base)
			newRef := newCipher.BlindIndex(base)
			if oldRef == newRef {
				continue
			}
			if err := tx.Exec("UPDATE servers SET ref = ? WHERE id = ?", newRef, id).Error; err != nil {
				return fmt.Errorf("servers.ref (id %d) schreiben: %w", id, err)
			}
			for _, table := range []string{"vulnerabilities", "packages"} {
				if err := tx.Exec(fmt.Sprintf("UPDATE %s SET server_ref = ? WHERE server_ref = ?", table), newRef, oldRef).Error; err != nil {
					return fmt.Errorf("%s.server_ref (server %d) schreiben: %w", table, id, err)
				}
			}
		}
		return nil
	})
}
