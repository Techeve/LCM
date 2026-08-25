package domain

import "time"

// Standard-Rollen der LCM-LOGIN-Benutzer, die beim Seeding angelegt werden.
// Betriebssystem-Benutzer der verwalteten Server sind KEINE LCM-User, sondern
// ein eigenes Modell (domain.LinuxUser).
//
//   - admin:   Administrative User - voller Zugriff, globale Einstellungen,
//     Server hinzufügen, Gruppen erstellen, Benutzer & Rechte verwalten
//   - manager: Verwaltungs-User - verwalten NUR die Server der ihnen
//     zugewiesenen Gruppen und ordnen Linux-User darauf zu
//     (Tenant Isolation, erzwungen auf Query-Ebene)
//   - system:  interne Prozesse ohne User-Kontext; auch der Rechte-Kontext
//     für Service-Accounts (API-Keys) auf Manager-Level
const (
	RoleAdmin   = "admin"
	RoleManager = "manager"
	RoleSystem  = "system"
)

// Permission-Codes des LCM. Neue Features definieren hier ihre Codes
// und referenzieren sie in Routen via middlewares.RequirePermission(...).
const (
	PermUsersRead  = "users:read"
	PermUsersWrite = "users:write"
	PermRolesRead  = "roles:read"
	PermRolesWrite = "roles:write"

	PermServersRead  = "servers:read"  // Server-Listen, Details, Status, Pakete
	PermServersWrite = "servers:write" // Join, Settings, User-Zuordnung, Hardening, Decommission

	PermGroupsRead  = "groups:read"
	PermGroupsWrite = "groups:write"
	PermRulesManage = "rules:manage" // Rules definieren/ändern, Schedules schalten/triggern

	PermJobsRead     = "jobs:read"     // Job-Historie + SSH-Konsolen-Output
	PermPackagesRead = "packages:read" // globale Paket-/Vulnerability-Übersicht

	PermLinuxUsersRead  = "linuxusers:read"  // Linux-Benutzer lesen
	PermLinuxUsersWrite = "linuxusers:write" // Linux-Benutzer anlegen/ändern, Keys, Zuordnung

	PermProfilesRead = "profiles:read" // Berechtigungsprofile lesen
	// PermProfilesWrite gehört wie der Linux-Benutzer-Katalog allein zu admin:
	// Ein Profil beschreibt Root-Rechte auf Zielsystemen und gilt global über
	// alle Mandanten hinweg.
	PermProfilesWrite = "profiles:write" // Berechtigungsprofile anlegen/ändern

	PermSettingsManage = "settings:manage" // globale Einstellungen, SSL
	PermBackupsManage  = "backups:manage"
	PermAuditRead      = "audit:read"
	PermAPIKeysManage  = "apikeys:manage"

	PermNotificationsManage = "notifications:manage" // Benachrichtigungskanäle verwalten
	PermAlertsManage        = "alerts:manage"        // Alarmregeln und -historie verwalten
)

// ManagerPermissions sind die Rechte des Verwaltungs-Users (und damit
// auch von Service-Accounts). Die Daten-Sichtbarkeit wird zusätzlich
// pro Query auf die zugewiesenen Gruppen eingeschränkt.
//
// Der Linux-Benutzer-KATALOG (linuxusers:*) ist bewusst NICHT enthalten:
// er ist eine globale, mandantenübergreifende Ressource (ein Account samt
// SSH-Keys kann auf Servern beliebiger Gruppen liegen). Seine Verwaltung -
// insbesondere das Hinterlegen/Erzeugen von SSH-Keys - gehört daher allein
// zu admin; sonst könnte ein Manager fremden Accounts Schlüssel unterschieben,
// die beim Sync auf Server anderer Mandanten verteilt würden. Manager
// provisionieren OS-Benutzer weiterhin auf ihre EIGENEN (scope-geprüften)
// Server über POST /servers/:id/assign-user.
func ManagerPermissions() []string {
	return []string{
		PermServersRead, PermServersWrite,
		PermGroupsRead, PermRulesManage,
		PermJobsRead, PermPackagesRead,
	}
}

// Role bündelt Permissions und wird Usern zugewiesen (n:m).
type Role struct {
	ID          uint         `gorm:"primarykey" json:"id"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
	Name        string       `gorm:"uniqueIndex;not null" json:"name"`
	Description string       `json:"description"`
	Permissions []Permission `gorm:"many2many:role_permissions" json:"permissions"`
}

// Permission ist ein einzelnes, feingranulares Zugriffsrecht,
// identifiziert über einen stabilen Code (z.B. "servers:write").
type Permission struct {
	ID          uint      `gorm:"primarykey" json:"id"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Code        string    `gorm:"uniqueIndex;not null" json:"code"`
	Description string    `json:"description"`
}

// Scopes für API-Keys: read erlaubt nur GET/HEAD, readwrite alles.
// mcp ist ein eigener, isolierter Scope AUSSCHLIESSLICH für die MCP-
// Schnittstelle: solche Keys funktionieren nur auf dem separaten MCP-Listener
// (read-only Serverdaten) und werden auf der regulären REST-API/UI abgewiesen.
const (
	APIKeyScopeRead      = "read"
	APIKeyScopeReadWrite = "readwrite"
	APIKeyScopeMCP       = "mcp"
)

// APIKey erlaubt Service-zu-Service-Kommunikation ohne User-Login -
// die Grundlage der LCM-Service-Accounts: Der Key agiert im Rechte-
// Kontext seines Users (bei Service-Accounts ein unsichtbarer Bot-User
// mit Manager-Rolle und Gruppen-Zuordnung) und hat eine definierte
// Laufzeit (ExpiresAt). Der Schlüssel selbst wird nur als SHA-256-Hash
// gespeichert; der Klartext ist ausschließlich bei der Erstellung sichtbar.
type APIKey struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Name      string    `gorm:"not null" json:"name"`
	KeyHash   string    `gorm:"uniqueIndex;not null" json:"-"`
	Prefix    string    `gorm:"index" json:"prefix"` // erste Zeichen zur Identifikation
	UserID    uint      `json:"user_id"`             // Rechte-Kontext
	User      User      `json:"-"`
	// Scope begrenzt zusätzlich zur RBAC-Prüfung: "read" blockiert alle
	// schreibenden HTTP-Methoden, "readwrite" erlaubt sie.
	Scope      string     `gorm:"default:readwrite" json:"scope"`
	LastUsedAt *time.Time `json:"last_used_at"`
	ExpiresAt  *time.Time `json:"expires_at"`
	Revoked    bool       `gorm:"default:false" json:"revoked"`
}

// IsReadOnly meldet, ob der Key nur lesende Zugriffe erlaubt.
func (k *APIKey) IsReadOnly() bool {
	return k.Scope == APIKeyScopeRead
}

// IsMCP meldet, ob der Key ausschließlich für die MCP-Schnittstelle gilt.
func (k *APIKey) IsMCP() bool {
	return k.Scope == APIKeyScopeMCP
}
