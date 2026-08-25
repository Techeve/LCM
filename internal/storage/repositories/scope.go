package repositories

import "gorm.io/gorm"

// AccessScope ist die Daten-Sichtbarkeit des anfragenden Users und wird
// bei JEDER Server-/Gruppen-Query unsichtbar als Filter angehängt
// (Tenant Isolation, Security-Konzept 9.2):
//
//   - Admins sehen alles (All = true).
//   - Manager (und Service-Accounts) sehen ausschließlich Server und
//     Gruppen, bei denen sie als Manager eingetragen sind - wer keine
//     Rechte auf einen Datensatz hat, bekommt ErrNotFound (=> 404).
type AccessScope struct {
	All    bool
	UserID uint
}

// ScopeAll ist der uneingeschränkte Zugriff (Admin/System).
func ScopeAll() AccessScope { return AccessScope{All: true} }

// ScopeManager beschränkt auf die Gruppen des angegebenen Users.
func ScopeManager(userID uint) AccessScope { return AccessScope{UserID: userID} }

// scopeServers hängt den Sichtbarkeits-Filter für Server an eine Query.
func (s AccessScope) scopeServers(db *gorm.DB) *gorm.DB {
	if s.All {
		return db
	}
	return db.Where(
		"servers.id IN (SELECT sgs.server_id FROM server_group_servers sgs"+
			" JOIN server_group_managers sgm ON sgm.server_group_id = sgs.server_group_id"+
			" WHERE sgm.user_id = ?)", s.UserID)
}

// scopeGroups hängt den Sichtbarkeits-Filter für Servergruppen an.
func (s AccessScope) scopeGroups(db *gorm.DB) *gorm.DB {
	if s.All {
		return db
	}
	return db.Where(
		"server_groups.id IN (SELECT server_group_id FROM server_group_managers WHERE user_id = ?)",
		s.UserID)
}
