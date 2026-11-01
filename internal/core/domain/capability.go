package domain

import (
	"encoding/json"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
)

// Betriebsfähigkeit eines Servers.
//
// Ein Job-Status sagt, wie EIN Lauf ausging. Ob ein Server noch gepflegt
// werden kann, sagt er nicht: Ein Server, dessen apt seit Tagen an einem
// kaputten dpkg-Zustand scheitert, hat lauter einzelne, gleich aussehende
// Fehlschläge - und niemand sieht, dass er seit einer Woche keine Updates mehr
// bekommt. Deshalb wird je Server und Fähigkeit (Kategorie) festgehalten, ob
// sie funktioniert, seit wann nicht und woran es liegt.

// Kategorien der Betriebsfähigkeit.
const (
	CapabilityConnection = "connection" // Der Server ist per SSH erreichbar.
	CapabilityPackages   = "packages"   // Paketverwaltung funktioniert (Listen, Updates).
	CapabilityRules      = "rules"      // Regeln und Aktionen laufen durch.
	CapabilityCVEScan    = "cve_scan"   // Der Schwachstellen-Scan liefert Ergebnisse.
)

// CapabilityCategories in Anzeigereihenfolge.
var CapabilityCategories = []string{
	CapabilityConnection, CapabilityPackages, CapabilityRules, CapabilityCVEScan,
}

// Zustände einer Kategorie.
const (
	CapabilityOK     = "ok"
	CapabilityFailed = "failed"
)

// Ampelstufen, die Effective liefert.
const (
	CapabilityCritical = "critical"
	CapabilityWarning  = "warning"
)

// Fehlerklassen. Sie sind der stabile Schlüssel, über den die Oberfläche den
// Hinweis in der Sprache des Benutzers formuliert - der Rohtext des Werkzeugs
// ist nur Beleg.
const (
	ErrClassLockBusy           = "lock_busy"           // Ein anderer Prozess hält die Paketsperre.
	ErrClassDpkgInterrupted    = "dpkg_interrupted"    // dpkg wurde mitten in einer Installation unterbrochen.
	ErrClassBrokenDeps         = "broken_deps"         // Abhängigkeiten sind unerfüllbar.
	ErrClassDiskFull           = "disk_full"           // Kein Speicherplatz mehr.
	ErrClassRepoError          = "repo_error"          // Eine Paketquelle ist kaputt (404, Signatur, abgelaufen).
	ErrClassNetwork            = "network"             // Namensauflösung oder Verbindung scheitern.
	ErrClassTimeout            = "timeout"             // Der Lauf hing und wurde abgebrochen.
	ErrClassScannerUnavailable = "scanner_unavailable" // Der CVE-Scanner (Trivy) fehlt auf dem LCM-Host.
	ErrClassUnknown            = "unknown"
)

// capabilityTextMax begrenzt gespeicherte Fehlertexte. Sie sollen den Anlass
// zeigen, nicht das Protokoll ersetzen - das steht am Job.
const capabilityTextMax = 500

// ServerCapability ist der Zustand EINER Fähigkeit eines Servers.
//
// Eine Zeile je (Server, Kategorie): Die Abfrage „alle Kategorien vieler
// Server" ist damit EINE Abfrage über server_id.
type ServerCapability struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	ServerID uint   `gorm:"not null;uniqueIndex:idx_capability_server_category,priority:1" json:"server_id"`
	Category string `gorm:"not null;uniqueIndex:idx_capability_server_category,priority:2" json:"category"`
	State    string `gorm:"not null" json:"state"` // CapabilityOK / CapabilityFailed

	FailingSince        *time.Time `json:"failing_since"`
	ConsecutiveFailures int        `json:"consecutive_failures"`
	LastSuccessAt       *time.Time `json:"last_success_at"`

	LastErrorClass string `json:"last_error_class"`
	// Die Texte stammen aus der Ausgabe des Servers und sind deshalb so
	// schützenswert wie Job.Output - at rest verschlüsselt. Die API gibt sie
	// nur an Benutzer mit Zugriff auf den Server aus.
	LastErrorMessage string `gorm:"serializer:aesgcm" json:"last_error_message"`
	// LastErrorParamsEnc hält die Parameter als verschlüsseltes JSON: Es gibt
	// keinen Serializer für Maps, und einen neuen anzulegen lohnt für ein
	// Feld nicht. LastErrorParams ist die benutzbare Form; die Hooks unten
	// halten beide gleich.
	LastErrorParamsEnc string            `gorm:"column:last_error_params;serializer:aesgcm" json:"-"`
	LastErrorParams    map[string]string `gorm:"-" json:"last_error_params,omitempty"`

	LastJobID string `json:"last_job_id"`
	// LastJobType braucht die Heilungsregel: Ein fehlgeschlagenes „security"
	// heilt ein „update", ein „health" heilt es nicht.
	LastJobType string `json:"last_job_type"`
	LastJobName string `json:"last_job_name"`
	// LastRuleID: Regeln heilen nur durch einen Erfolg DERSELBEN Regel; nil bei
	// Ad-hoc-Läufen.
	LastRuleID *uint `json:"last_rule_id"`

	// Warnung eines ERFOLGREICHEN Laufs mit Teilproblem - etwa eine kaputte
	// Paketquelle unter vielen. Die Kategorie bleibt ok, der Hinweis gelb.
	WarningClass   string `json:"warning_class"`
	WarningMessage string `gorm:"serializer:aesgcm" json:"warning_message"`
}

// BeforeSave verdichtet die Parameter zum Ablagewert und kürzt die Texte. Hier
// statt an jeder Aufrufstelle, damit keine Zeile ungekürzt oder mit
// auseinanderlaufenden Parametern in die Datenbank gelangt.
func (c *ServerCapability) BeforeSave(*gorm.DB) error {
	c.LastErrorMessage = truncateRunes(c.LastErrorMessage, capabilityTextMax)
	c.WarningMessage = truncateRunes(c.WarningMessage, capabilityTextMax)
	c.LastErrorParamsEnc = ""
	if len(c.LastErrorParams) == 0 {
		return nil
	}
	raw, err := json.Marshal(c.LastErrorParams)
	if err != nil {
		return err
	}
	c.LastErrorParamsEnc = string(raw)
	return nil
}

// AfterFind stellt die Parameter aus dem Ablagewert wieder her. Ein
// unlesbarer Wert (beschädigt, Fremdformat) ergibt keine Parameter - der
// Hinweis bleibt auch ohne sie verständlich, das Laden darf daran nicht
// scheitern.
func (c *ServerCapability) AfterFind(*gorm.DB) error {
	c.LastErrorParams = nil
	if c.LastErrorParamsEnc == "" {
		return nil
	}
	var params map[string]string
	if json.Unmarshal([]byte(c.LastErrorParamsEnc), &params) == nil {
		c.LastErrorParams = params
	}
	return nil
}

// truncateRunes kürzt auf höchstens max Zeichen. Nach Zeichen statt nach
// Bytes, damit kein Multibyte-Zeichen zerschnitten wird.
func truncateRunes(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max])
}

// CapabilityCategoryFor ordnet einen Job-Typ einer Kategorie zu. "" heißt: Der
// Typ berührt keine Kategorie.
//
// System-Jobs (Backup, Aufräumen, Alarmprüfung, Docker-/Hardware-Abgleich,
// Beitritt, Reparaturen) sind absichtlich ausgenommen: Sie laufen ohne
// Server oder sagen nichts darüber, ob der Server gepflegt werden kann. Ein
// fehlgeschlagenes Backup würde sonst einen gesunden Server rot färben.
func CapabilityCategoryFor(jobType string) string {
	switch jobType {
	case RuleTypeUpdate, RuleTypePackages, RuleTypeSecurity, RuleTypePackageScan, RuleTypeAutoremove:
		return CapabilityPackages
	case RuleTypeCVEScan:
		return CapabilityCVEScan
	case RuleTypeHealth:
		return CapabilityConnection
	case RuleTypeScript, RuleTypeCustom, RuleTypeSync, RuleTypeFirewall, RuleTypePermSync,
		RuleTypeACLSetup, RuleTypeAptProxy, RuleTypeReboot, RuleTypeRebootIfNeeded, RuleTypeDNSTest,
		RuleTypeDockerPrune, RuleTypeDockerUpdate, RuleTypeDockerUpdateUnused,
		RuleTypeDockerComposeUpdate, RuleTypeDockerContainerUpdate, RuleTypeDeepScan, RuleTypeAppCheck:
		return CapabilityRules
	default:
		return ""
	}
}

// Heals sagt, ob der erfolgreiche Job die fehlgeschlagene Kategorie
// zurücksetzt.
//
// Connection: Jeder Erfolg heilt, gleich welchen Typs - wer einen Job
// ausführen konnte, erreicht den Server. Ob ein Job überhaupt zählt, entscheidet
// der Aufrufer (CapabilityCategoryFor), nicht diese Methode.
//
// Sonst heilt derselbe Typ: Ein erfolgreiches „security" sagt nichts darüber,
// ob das fehlgeschlagene „autoremove" jetzt geht. Ausnahmen in packages, wo
// die Läufe aufeinander aufbauen:
//   - Jeder Paket-Job beginnt mit `apt-get update`. Ein fehlgeschlagener
//     Paketlisten-Lauf (package-scan) ist daher durch JEDEN Erfolg dieser
//     Kategorie erledigt.
//   - Ein erfolgreiches „update" (volles Upgrade) deckt auch „security" und
//     „packages" ab, denn es spielt deren Pakete mit ein.
//
// Regeln (rules) sind enger: Zwei Regeln desselben Typs (zwei Skripte, zwei
// Firewall-Regeln) tun Verschiedenes - gleicher Typ genügt nicht, es muss
// dieselbe Regel sein (siehe sameSource).
func (c *ServerCapability) Heals(success *Job) bool {
	if c.Category == CapabilityConnection {
		return true
	}
	if CapabilityCategoryFor(success.Type) != c.Category {
		return false
	}
	if c.LastJobType == "" {
		return true
	}
	if c.Category == CapabilityRules {
		return c.sameSource(success)
	}
	if c.LastJobType == success.Type {
		return true
	}
	if c.Category != CapabilityPackages {
		return false
	}
	if c.LastJobType == RuleTypePackageScan {
		return true
	}
	return success.Type == RuleTypeUpdate &&
		(c.LastJobType == RuleTypeSecurity || c.LastJobType == RuleTypePackages)
}

// sameSource: Der Job stammt von derselben Regel wie der letzte Fehlschlag.
// Ad-hoc-Läufe haben keine Regel; dort unterscheidet nur der Jobname - sonst
// würde ein unbeteiligtes Ad-hoc-Skript das fehlgeschlagene
// CrowdSec-Einrichtungsskript „heilen".
func (c *ServerCapability) sameSource(job *Job) bool {
	if c.LastJobType != job.Type {
		return false
	}
	if c.LastRuleID == nil || job.RuleID == nil {
		return c.LastRuleID == nil && job.RuleID == nil && c.LastJobName == job.Name
	}
	return *c.LastRuleID == *job.RuleID
}

// HealsRules: Ein Health-Ping heilt einen Regel-Fehler nur, wenn er aus
// derselben Quelle stammt wie der Fehlschlag. Der Typ allein genügt nicht.
func (c *ServerCapability) HealsRules(ping *Job) bool {
	return c.Category == CapabilityRules && c.sameSource(ping)
}

// Effective ist die Ampelstufe der Kategorie: "critical", "warning" oder ""
// (in Ordnung).
//
// Ohne Paketverwaltung und Verbindung ist der Server nicht zu pflegen - rot.
// Ausnahme ist die belegte Sperre: Meist hält sie ein harmloser Prozess
// (unattended-upgrades), der beim nächsten Lauf fertig ist. Erst der zweite
// Fehlschlag in Folge deutet auf ein Dauerproblem. Regeln und CVE-Scan
// beeinträchtigen die Pflege nicht grundsätzlich - gelb.
func (c *ServerCapability) Effective() string {
	if c.State != CapabilityFailed {
		return ""
	}
	switch c.Category {
	case CapabilityPackages, CapabilityConnection:
		if c.LastErrorClass == ErrClassLockBusy && c.ConsecutiveFailures < 2 {
			return CapabilityWarning
		}
		return CapabilityCritical
	default:
		return CapabilityWarning
	}
}
