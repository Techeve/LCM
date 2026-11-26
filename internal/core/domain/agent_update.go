package domain

import "time"

// Agent-Updates je Gruppe.
//
// Ein Agent aktualisiert sich nie von selbst und nicht über ein gewöhnliches
// `apt upgrade` (er ist dort angehalten). Auslöser ist der Knopf am Server oder
// die Regel einer Gruppe: Ist sie eingeschaltet, zieht LCM die Agenten der
// Gruppe in einem Zeitfenster auf die Version nach, die zu diesem LCM passt.
//
// Das Zeitfenster folgt dem Modell der Paket-Zeitpläne: eine Startzeit (Cron)
// und ein Fenster in Minuten, über das sich die Server gleichmäßig verteilen.
const (
	// AgentUpdateOff: LCM aktualisiert die Agenten der Gruppe nicht von selbst.
	AgentUpdateOff = "off"
	// AgentUpdateAuto: LCM aktualisiert die Agenten im Zeitfenster.
	AgentUpdateAuto = "auto"
)

const (
	// DefaultAgentUpdateCron ist der Fensterbeginn einer neuen Regel: nachts um
	// drei, wenn die Pakete ohnehin dran sind.
	DefaultAgentUpdateCron = "0 3 * * *"
	// DefaultAgentUpdateWindowMinutes ist die Fensterlänge einer neuen Regel.
	DefaultAgentUpdateWindowMinutes = 120
	// MaxAgentUpdateWindowMinutes begrenzt die Eingabe (ein Tag).
	MaxAgentUpdateWindowMinutes = 24 * 60
)

// AgentUpdatePolicy ist die Agent-Update-Regel einer Servergruppe. Es gibt
// höchstens eine je Gruppe; ohne Zeile gilt „aus“.
type AgentUpdatePolicy struct {
	ID        uint      `gorm:"primarykey" json:"-"`
	CreatedAt time.Time `json:"-"`
	UpdatedAt time.Time `json:"-"`

	GroupID uint `gorm:"not null;uniqueIndex" json:"group_id"`

	// Mode ist AgentUpdateOff oder AgentUpdateAuto.
	Mode string `gorm:"not null;default:off" json:"mode"`
	// Staged: Erst ein Server der Gruppe, der Rest erst, wenn der erste sich mit
	// der neuen Version zurückgemeldet hat und antwortet.
	//
	// Ohne gorm-Default: Ein Default „true“ überschriebe beim Anlegen ein
	// ausdrückliches false.
	Staged bool `gorm:"not null" json:"staged"`
	// CronExpr ist der Beginn des Zeitfensters (Cron wie bei den Zeitplänen).
	CronExpr string `gorm:"not null" json:"cron_expr"`
	// WindowMinutes ist die Länge des Fensters: Die übrigen Server beginnen
	// gleichmäßig verteilt darin (wie SpreadMinutes eines Zeitplans). 0 = alle
	// zur Startzeit.
	WindowMinutes int `gorm:"not null" json:"window_minutes"`

	// Stand des letzten Laufs. Die Oberfläche zeigt ihn; der Lauf liest daraus,
	// ob er angehalten ist (BlockedFor).

	// LastRunAt und LastResult beschreiben den letzten Lauf.
	LastRunAt  *time.Time `json:"last_run_at"`
	LastResult string     `json:"last_result"`
	// ProvenFor/ProvenVersion: Im letzten Lauf unter dieser LCM-Version
	// (ProvenFor) hat der erste Server die Agent-Version ProvenVersion erreicht
	// und antwortet. Gestaffelt wird der Rest auf höchstens diese Version
	// gezogen; jeder Lauf schickt den ersten Server erneut voraus, damit auch
	// ein neuer Bugfix erst dort ankommt.
	ProvenFor     string `json:"proven_for"`
	ProvenVersion string `json:"proven_version"`
	// BlockedFor: Der erste Server ist mit dieser LCM-Version gescheitert; die
	// Regel hält an, bis die Version wechselt oder der Betreiber fortsetzt.
	BlockedFor string `json:"blocked_for"`
}

// Enabled meldet, ob die Regel Agenten aktualisiert.
func (p *AgentUpdatePolicy) Enabled() bool {
	return p != nil && p.Mode == AgentUpdateAuto
}

// DefaultAgentUpdatePolicy ist, was eine Gruppe ohne gespeicherte Regel hat.
func DefaultAgentUpdatePolicy(groupID uint) *AgentUpdatePolicy {
	return &AgentUpdatePolicy{
		GroupID: groupID, Mode: AgentUpdateOff, Staged: true,
		CronExpr: DefaultAgentUpdateCron, WindowMinutes: DefaultAgentUpdateWindowMinutes,
	}
}
