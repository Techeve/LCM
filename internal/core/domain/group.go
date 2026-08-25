package domain

import "time"

// SystemGroupName ist die beim Seeding angelegte, nicht löschbare Gruppe,
// in der die systemrelevanten Basis-Jobs (Health-Check, Sync, Backup,
// Log-Cleanup) verankert sind.
const SystemGroupName = "System"

// Vorrang-Stufen einer Servergruppe. KLEINERE Zahl = stärkerer Vorrang -
// dieselbe Leserichtung wie MX- und SRV-Records.
const (
	// DefaultGroupPriority ist der Vorrang jeder normal angelegten Gruppe.
	DefaultGroupPriority = 100
	// SystemGroupPriority ist bewusst der SCHWÄCHSTE Vorrang: Die Regeln der
	// System-Gruppe gelten für alle Server und sind damit die Grundlinie, die
	// eine spezifischere Gruppe überstimmen darf.
	SystemGroupPriority = 1000
	// MaxGroupPriority begrenzt die Eingabe nach oben (0 und negative Werte
	// sind ebenfalls unzulässig - siehe ValidGroupPriority).
	MaxGroupPriority = 9999
)

// ValidGroupPriority meldet, ob der Wert als Vorrang zulässig ist.
func ValidGroupPriority(p int) bool { return p > 0 && p <= MaxGroupPriority }

// ServerGroup organisiert Server für Rules und Berechtigungen.
//
//   - Servers:    Mitglieds-Server der Gruppe
//   - Managers:   LCM-Verwaltungs-User, die genau diese Gruppe administrieren
//     dürfen (Tenant Isolation - Manager sehen nur ihre Gruppen)
//   - LinuxUsers: Betriebssystem-Benutzer, deren Accounts/SSH-Keys auf alle
//     Server der Gruppe provisioniert werden
type ServerGroup struct {
	ID          uint      `gorm:"primarykey" json:"id"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Name        string    `gorm:"uniqueIndex;not null" json:"name"`
	Description string    `json:"description"`
	// IsSystem markiert die geschützte System-Gruppe (nicht löschbar,
	// ihre System-Rules sind nicht entfernbar).
	IsSystem bool `gorm:"default:false" json:"is_system"`
	// Priority entscheidet, welche Gruppe sich durchsetzt, wenn für einen
	// Server MEHRERE Gruppen dasselbe regeln - ein Server darf in beliebig
	// vielen Gruppen sein. Kleinere Zahl gewinnt.
	//
	// Ohne diesen Vorrang setzten zwei Firewall-Grundsatz-Regeln aus zwei
	// Gruppen sich bei JEDEM Health-Ping gegenseitig zurück: beide liefen
	// nacheinander, die zuletzt ausgeführte gewann, und im Protokoll stand
	// zweimal „neu angewendet", ohne dass der Konflikt erkennbar war.
	Priority int `gorm:"not null;default:100" json:"priority"`

	// ACLServers/ACLCapable sind abgeleitet (nicht gespeichert): wie viele
	// Mitglieds-Server ACLs tragen. Der Zustand entscheidet, ob ein Profil mit
	// Verzeichnisrechten auf dieser Gruppe überhaupt wirken kann - und er
	// gehört dorthin, wo man das Profil zuweist, nicht in ein Serverdetail.
	ACLServers int `gorm:"-" json:"acl_servers"`
	ACLCapable int `gorm:"-" json:"acl_capable"`

	Servers    []Server    `gorm:"many2many:server_group_servers" json:"servers,omitempty"`
	Managers   []User      `gorm:"many2many:server_group_managers" json:"managers,omitempty"`
	LinuxUsers []LinuxUser `gorm:"many2many:server_group_linux_users" json:"linux_users,omitempty"`
	Rules      []Rule      `gorm:"foreignKey:GroupID" json:"rules,omitempty"`
}
