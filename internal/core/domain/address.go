package domain

// Die drei Adressmodi der öffentlichen Adressen (GlobalSettings.AddressMode).
const (
	// AddressModeSeparate: je Dienst ein Port und ein eigener Name (Vorgabe,
	// der Stand vor dem Adressmodus; typisch mit Reverse-Proxy davor).
	AddressModeSeparate = "separate"
	// AddressModeOneName: je Dienst ein Port, ein Name für alle.
	AddressModeOneName = "one_name"
	// AddressModeSharedPort: ein gemeinsamer TLS-Port, die Dienste trennt der
	// angefragte Name (SNI).
	AddressModeSharedPort = "shared_port"
)

// Die Schritte des Einrichtungsassistenten (GlobalSettings.SetupStep), in der
// Reihenfolge, in der der Assistent sie durchläuft. Leer heißt erledigt, auch
// bei bestehenden Installationen: Sie sehen den Assistenten nie.
const (
	SetupStepSession      = "session"      // Anmeldung im Browser: Cookie oder nur Sitzung
	SetupStepAddresses    = "addresses"    // Wie ist LCM erreichbar?
	SetupStepServices     = "services"     // Agent und MCP an oder aus
	SetupStepAccess       = "access"       // Zugriff aus welchen Netzen
	SetupStepCertificate  = "certificate"  // Let's Encrypt oder Reverse-Proxy
	SetupStepOnboarding   = "onboarding"   // Vorgabe-Benutzer und Onboarding-Schlüssel
	SetupStepMail         = "mail"         // E-Mail-Versand
	SetupStepRetention    = "retention"    // Aufbewahrung von Jobs, Protokollen, Speicherverlauf
	SetupStepBackup       = "backup"       // automatisches Backup
	SetupStepSubscription = "subscription" // Subscription und Kanal
	SetupStepIntro        = "intro"        // Einführung auf der Seite des LCM-Hosts
)

// setupSteps nennt die Schritte in der Reihenfolge des Assistenten.
var setupSteps = []string{
	SetupStepSession, SetupStepAddresses, SetupStepServices, SetupStepAccess,
	SetupStepCertificate, SetupStepOnboarding, SetupStepMail, SetupStepRetention,
	SetupStepBackup, SetupStepSubscription, SetupStepIntro,
}

// ValidSetupStep meldet, ob step ein Schritt des Assistenten oder leer
// (erledigt) ist.
func ValidSetupStep(step string) bool {
	return step == "" || setupStepIndex(step) >= 0
}

// SetupStepMoveAllowed meldet, ob der Assistent von current auf next gehen
// darf. Zwischen session und subscription geht es vor und zurück, von dort auch
// weiter auf intro oder leer (erledigt). Aus intro führt nur der Weg auf leer;
// aus leer gibt es keinen Weg.
func SetupStepMoveAllowed(current, next string) bool {
	switch current {
	case "":
		return false
	case SetupStepIntro:
		return next == ""
	}
	return ValidSetupStep(next)
}

func setupStepIndex(step string) int {
	for i, s := range setupSteps {
		if s == step {
			return i
		}
	}
	return -1
}

// Vorgaben der Ports. Die Ports von Oberfläche und Agent sind zugleich die
// Vorgaben der config.json (siehe cmd/app/config_sync.go).
const (
	DefaultUIPort        = 9310
	DefaultAgentPort     = 9320
	DefaultMCPPort       = 9330
	DefaultSharedTLSPort = 443
	DefaultACMEHTTPPort  = 80
)

// ValidAddressMode meldet, ob m einer der drei Modi ist.
func ValidAddressMode(m string) bool {
	switch m {
	case AddressModeSeparate, AddressModeOneName, AddressModeSharedPort:
		return true
	}
	return false
}

// Mode liefert den gespeicherten Adressmodus; leer (Altbestand) und alles
// Unbekannte gilt als getrennt - der Stand vor dem Adressmodus.
func (g *GlobalSettings) Mode() string {
	if ValidAddressMode(g.AddressMode) {
		return g.AddressMode
	}
	return AddressModeSeparate
}
