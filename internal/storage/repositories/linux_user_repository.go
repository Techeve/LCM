package repositories

import (
	"sort"
	"strings"

	"gorm.io/gorm"

	"LCM/internal/core/domain"
)

// LinuxUserRepository verwaltet die Betriebssystem-Benutzer (Katalog +
// SSH-Keys + Zuordnung zu Servern/Gruppen). Getrennt von den LCM-Login-
// Usern (UserRepository).
type LinuxUserRepository struct {
	db *gorm.DB
}

func NewLinuxUserRepository(db *gorm.DB) *LinuxUserRepository {
	return &LinuxUserRepository{db: db}
}

func (r *LinuxUserRepository) Create(u *domain.LinuxUser) error {
	return r.db.Create(u).Error
}

func (r *LinuxUserRepository) Update(u *domain.LinuxUser) error {
	return r.db.Save(u).Error
}

func (r *LinuxUserRepository) FindByID(id uint) (*domain.LinuxUser, error) {
	var u domain.LinuxUser
	if err := r.db.Preload("SSHKeys").Preload("Servers").Preload("Groups").First(&u, id).Error; err != nil {
		return nil, translate(err)
	}
	return &u, nil
}

func (r *LinuxUserRepository) FindByUsername(name string) (*domain.LinuxUser, error) {
	var u domain.LinuxUser
	// Username ist verschlüsselt - Suche über den deterministischen Blindindex.
	if err := r.db.Where("username_bidx = ?", BlindIndex(name)).First(&u).Error; err != nil {
		return nil, translate(err)
	}
	return &u, nil
}

func (r *LinuxUserRepository) FindAll() ([]domain.LinuxUser, error) {
	var users []domain.LinuxUser
	// Nicht per SQL nach dem (verschlüsselten) Username sortieren - das ergäbe
	// Ciphertext-Reihenfolge. Nach dem Laden (entschlüsselt) in Go sortieren.
	if err := r.db.Preload("SSHKeys").Preload("Servers").Preload("Groups").
		Find(&users).Error; err != nil {
		return nil, err
	}
	sort.Slice(users, func(i, j int) bool {
		return strings.ToLower(users[i].Username) < strings.ToLower(users[j].Username)
	})
	return users, nil
}

func (r *LinuxUserRepository) Delete(id uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("linux_user_id = ?", id).Delete(&domain.LinuxUserSSHKey{}).Error; err != nil {
			return err
		}
		// Aktivierungstoken mitentfernen: sie blieben sonst als verwaiste
		// Zeilen stehen - einlösbar waren sie nach dem Löschen zwar nicht
		// mehr (404), aber ein Datenrest ohne Aufräumweg (R2-047).
		if err := tx.Where("linux_user_id = ?", id).Delete(&domain.LinuxUserActivation{}).Error; err != nil {
			return err
		}
		lu := domain.LinuxUser{ID: id}
		if err := tx.Model(&lu).Association("Servers").Clear(); err != nil {
			return err
		}
		if err := tx.Model(&lu).Association("Groups").Clear(); err != nil {
			return err
		}
		res := tx.Delete(&domain.LinuxUser{}, id)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// ---- SSH-Keys ----------------------------------------------------------------

func (r *LinuxUserRepository) AddSSHKey(key *domain.LinuxUserSSHKey) error {
	return r.db.Create(key).Error
}

// FindSSHKeyOwner liefert den Linux-Benutzer, dem ein Schlüssel gehört -
// nötig, bevor der Schlüssel gelöscht wird (danach ist die Spur weg).
func (r *LinuxUserRepository) FindSSHKeyOwner(keyID uint) (uint, error) {
	var key domain.LinuxUserSSHKey
	if err := r.db.First(&key, keyID).Error; err != nil {
		return 0, translate(err)
	}
	return key.LinuxUserID, nil
}

func (r *LinuxUserRepository) DeleteSSHKey(id uint) error {
	res := r.db.Delete(&domain.LinuxUserSSHKey{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- Zuordnung zu Servern / Gruppen -----------------------------------------

func (r *LinuxUserRepository) AssignToServer(linuxUserID, serverID uint) error {
	lu := domain.LinuxUser{ID: linuxUserID}
	return r.db.Model(&lu).Association("Servers").Append(&domain.Server{ID: serverID})
}

// DirectlyAssigned meldet, ob der Benutzer dem Server DIREKT zugeordnet
// ist (nicht nur über eine Gruppe). Nur direkte Zuordnungen lassen sich
// je Server lösen - Gruppen-Zuordnungen gelten für die ganze Gruppe.
func (r *LinuxUserRepository) DirectlyAssigned(linuxUserID, serverID uint) (bool, error) {
	var n int64
	err := r.db.Table("server_linux_users").
		Where("linux_user_id = ? AND server_id = ?", linuxUserID, serverID).Count(&n).Error
	return n > 0, err
}

func (r *LinuxUserRepository) RemoveFromServer(linuxUserID, serverID uint) error {
	lu := domain.LinuxUser{ID: linuxUserID}
	return r.db.Model(&lu).Association("Servers").Delete(&domain.Server{ID: serverID})
}

func (r *LinuxUserRepository) AssignToGroup(linuxUserID, groupID uint) error {
	lu := domain.LinuxUser{ID: linuxUserID}
	return r.db.Model(&lu).Association("Groups").Append(&domain.ServerGroup{ID: groupID})
}

func (r *LinuxUserRepository) RemoveFromGroup(linuxUserID, groupID uint) error {
	lu := domain.LinuxUser{ID: linuxUserID}
	return r.db.Model(&lu).Association("Groups").Delete(&domain.ServerGroup{ID: groupID})
}

// ForServer liefert alle aktiven LinuxUser (inkl. SSH-Keys), die auf dem
// Server provisioniert werden sollen: direkt zugeordnet ODER über eine
// Gruppe, die den Server enthält.
func (r *LinuxUserRepository) ForServer(serverID uint) ([]domain.LinuxUser, error) {
	var users []domain.LinuxUser
	err := r.db.Preload("SSHKeys").
		Where("active = ? AND id IN ("+
			"SELECT linux_user_id FROM server_linux_users WHERE server_id = ?"+
			" UNION "+
			"SELECT glu.linux_user_id FROM server_group_linux_users glu"+
			" JOIN server_group_servers sgs ON sgs.server_group_id = glu.server_group_id"+
			" WHERE sgs.server_id = ?)", true, serverID, serverID).
		Order("username").Find(&users).Error
	return users, err
}

// AssignedForServer liefert ALLE dem Server zugeordneten Linux-Benutzer -
// auch deaktivierte. Der Sync braucht beide: Aktive werden provisioniert,
// Deaktivierte auf dem Zielsystem GESPERRT. Würden Deaktivierte nur aus der
// Soll-Liste gefiltert (ForServer), bliebe ihr Konto samt Schlüsseln und
// sudo unangetastet - und der Sync meldete trotzdem Erfolg (R2-039).
func (r *LinuxUserRepository) AssignedForServer(serverID uint) ([]domain.LinuxUser, error) {
	var users []domain.LinuxUser
	err := r.db.Preload("SSHKeys").
		Where("id IN ("+
			"SELECT linux_user_id FROM server_linux_users WHERE server_id = ?"+
			" UNION "+
			"SELECT glu.linux_user_id FROM server_group_linux_users glu"+
			" JOIN server_group_servers sgs ON sgs.server_group_id = glu.server_group_id"+
			" WHERE sgs.server_id = ?)", serverID, serverID).
		Order("username").Find(&users).Error
	return users, err
}

// ServersForUser liefert alle Server, auf denen der Benutzer effektiv
// provisioniert ist - direkt zugeordnet ODER über eine Servergruppe.
func (r *LinuxUserRepository) ServersForUser(linuxUserID uint) ([]domain.Server, error) {
	var servers []domain.Server
	err := r.db.Where("id IN ("+
		"SELECT server_id FROM server_linux_users WHERE linux_user_id = ?"+
		" UNION "+
		"SELECT sgs.server_id FROM server_group_servers sgs"+
		" JOIN server_group_linux_users glu ON glu.server_group_id = sgs.server_group_id"+
		" WHERE glu.linux_user_id = ?)", linuxUserID, linuxUserID).
		Find(&servers).Error
	// In Go sortieren - servers.name liegt verschlüsselt in der DB, ein
	// SQL-ORDER BY wäre bedeutungslos (siehe sortByName im ServerRepository).
	sortByName(servers)
	return servers, err
}

// ClearAssignments löst alle Server- und Gruppen-Zuordnungen des Benutzers.
func (r *LinuxUserRepository) ClearAssignments(linuxUserID uint) error {
	lu := domain.LinuxUser{ID: linuxUserID}
	if err := r.db.Model(&lu).Association("Servers").Clear(); err != nil {
		return err
	}
	return r.db.Model(&lu).Association("Groups").Clear()
}

// ---- Aktivierungslinks -------------------------------------------------------

func (r *LinuxUserRepository) CreateActivation(a *domain.LinuxUserActivation) error {
	return r.db.Create(a).Error
}

func (r *LinuxUserRepository) FindActivationByTokenHash(hash string) (*domain.LinuxUserActivation, error) {
	var a domain.LinuxUserActivation
	if err := r.db.Where("token_hash = ?", hash).First(&a).Error; err != nil {
		return nil, translate(err)
	}
	return &a, nil
}

func (r *LinuxUserRepository) MarkActivationConsumed(a *domain.LinuxUserActivation) error {
	return r.db.Save(a).Error
}

// UpdateFields aktualisiert einzelne Spalten (z.B. password_enc beim
// Einlösen des Aktivierungslinks).
func (r *LinuxUserRepository) UpdateFields(id uint, fields map[string]any) error {
	return r.db.Model(&domain.LinuxUser{}).Where("id = ?", id).Updates(fields).Error
}

// ---- Berechtigungsprofile ----------------------------------------------------

// SetServerAssignmentProfile setzt das Profil einer DIREKTEN Zuweisung
// (nil = Standardprofil des Benutzers).
func (r *LinuxUserRepository) SetServerAssignmentProfile(linuxUserID, serverID uint, profileID *uint) error {
	return r.db.Table("server_linux_users").
		Where("linux_user_id = ? AND server_id = ?", linuxUserID, serverID).
		Update("profile_id", profileID).Error
}

// SetGroupAssignmentProfile setzt das Profil einer Gruppen-Zuweisung
// (nil = Standardprofil des Benutzers).
func (r *LinuxUserRepository) SetGroupAssignmentProfile(linuxUserID, groupID uint, profileID *uint) error {
	return r.db.Table("server_group_linux_users").
		Where("linux_user_id = ? AND server_group_id = ?", linuxUserID, groupID).
		Update("profile_id", profileID).Error
}

// GroupAssignments liefert die Gruppen-Zuweisungen eines Benutzers samt
// gesetztem Profil - die Grundlage der Anzeige „welches Profil gilt wo".
func (r *LinuxUserRepository) GroupAssignments(linuxUserID uint) ([]domain.ServerGroupLinuxUser, error) {
	var rows []domain.ServerGroupLinuxUser
	err := r.db.Where("linux_user_id = ?", linuxUserID).Find(&rows).Error
	return rows, err
}

// EffectiveProfilesForServer liefert je Benutzer das auf DIESEM Server
// wirksame Profil (Benutzer-ID → Profil-ID).
//
// Reihenfolge der Auflösung - von der spezifischsten Aussage zur
// allgemeinsten:
//
//  1. ein ausdrücklich an der Direktzuweisung gesetztes Profil,
//  2. das Profil der Servergruppe mit dem stärksten Vorrang,
//  3. das Standardprofil des Benutzers.
//
// Ein Benutzer, der über mehrere Gruppen auf denselben Server kommt, hat
// damit genau EIN wirksames Profil. Ohne Punkt 2 entschiede die Reihenfolge
// der Zeilen, und dieselbe Zuordnung ergäbe je nach Datenbankzustand
// unterschiedliche Rechte.
func (r *LinuxUserRepository) EffectiveProfilesForServer(serverID uint) (map[uint]uint, error) {
	type row struct {
		LinuxUserID uint
		ProfileID   uint
	}
	var rows []row
	// Kandidaten je Benutzer mit Rangstufe; die kleinste Stufe gewinnt, bei
	// Gleichstand innerhalb der Gruppen der stärkere (kleinere) Vorrang.
	err := r.db.Raw(`
		WITH kandidaten AS (
			SELECT slu.linux_user_id AS linux_user_id, slu.profile_id AS profile_id,
			       0 AS stufe, 0 AS vorrang, 0 AS gruppe
			  FROM server_linux_users slu
			 WHERE slu.server_id = ? AND slu.profile_id IS NOT NULL
			UNION ALL
			SELECT glu.linux_user_id, glu.profile_id,
			       1 AS stufe, sg.priority AS vorrang, sg.id AS gruppe
			  FROM server_group_linux_users glu
			  JOIN server_group_servers sgs ON sgs.server_group_id = glu.server_group_id
			  JOIN server_groups sg ON sg.id = glu.server_group_id
			 WHERE sgs.server_id = ? AND glu.profile_id IS NOT NULL
			UNION ALL
			SELECT lu.id, lu.default_profile_id, 2 AS stufe, 0 AS vorrang, 0 AS gruppe
			  FROM linux_users lu
			 WHERE lu.default_profile_id IS NOT NULL
		)
		SELECT linux_user_id, profile_id FROM kandidaten k
		 WHERE NOT EXISTS (
			SELECT 1 FROM kandidaten b
			 WHERE b.linux_user_id = k.linux_user_id
			   AND (b.stufe < k.stufe
			        OR (b.stufe = k.stufe AND b.vorrang < k.vorrang)
			        OR (b.stufe = k.stufe AND b.vorrang = k.vorrang AND b.gruppe < k.gruppe))
		 )`, serverID, serverID).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make(map[uint]uint, len(rows))
	for _, r := range rows {
		out[r.LinuxUserID] = r.ProfileID
	}
	return out, nil
}
