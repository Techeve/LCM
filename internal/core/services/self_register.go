package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/crypto"
	"LCM/internal/infrastructure/runtimeenv"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/storage/repositories"
)

// SelfHostName ist der Name, unter dem LCM den eigenen Rechner auffuehrt.
const SelfHostName = "lcm-host"

// SelfOnboardFileName liegt im Datenverzeichnis und ist die EINMALIGE
// Uebergabe des Installationsskripts an den Dienst: Es legt den
// Management-Benutzer an, hinterlegt dessen Public Key und schreibt den
// zugehoerigen Private Key hier hinein. Beim ersten Start liest LCM die Datei,
// verschluesselt den Schluessel in die Datenbank und loescht sie wieder -
// der Klartext-Schluessel liegt also nur zwischen Installation und erstem
// Start auf der Platte, lesbar allein fuer den Dienstbenutzer.
const SelfOnboardFileName = "self-onboard.json"

// selfOnboard ist das Format der Uebergabedatei (siehe postinstall.sh).
type selfOnboard struct {
	ServiceUser    string `json:"service_user"`
	PrivateKeyPEM  string `json:"private_key_pem"`
	PublicKey      string `json:"public_key"`
	RestrictedSudo bool   `json:"restricted_sudo"`
}

// SelfRegisterService nimmt den LCM-Host beim Start selbst in die Verwaltung
// auf. Ohne ihn muesste ein Administrator den eigenen Rechner von Hand ueber
// den Join-Wizard aufnehmen, bevor die host-spezifischen Funktionen
// (Trivy, apt-cacher-ng, CrowdSec-LAPI) ueberhaupt erreichbar sind.
type SelfRegisterService struct {
	servers  *repositories.ServerRepository
	settings *repositories.SettingsRepository
	dialer   sshx.Dialer
	cipher   *crypto.Cipher
	dataDir  string
	// containerCheck ist die Container-Erkennung als Feld, damit sie im Test
	// gesetzt werden kann. Ohne das haengt das Ergebnis an der Umgebung, in
	// der die Tests laufen: auf einem Entwicklerrechner falsch, in einem
	// CI-Container richtig - der Regelfall waere dort nie pruefbar.
	// Die Erkennung selbst liegt in runtimeenv (dort auch ihre Tests).
	containerCheck func() bool
}

func NewSelfRegisterService(
	servers *repositories.ServerRepository,
	settings *repositories.SettingsRepository,
	dialer sshx.Dialer,
	cipher *crypto.Cipher,
	dataDir string,
) *SelfRegisterService {
	return &SelfRegisterService{
		servers: servers, settings: settings,
		dialer: dialer, cipher: cipher, dataDir: dataDir,
		containerCheck: runtimeenv.InContainer,
	}
}

// onboardPath ist der volle Pfad der Uebergabedatei.
func (s *SelfRegisterService) onboardPath() string {
	return filepath.Join(s.dataDir, SelfOnboardFileName)
}

// Run fuehrt die Selbstregistrierung einmalig aus. Fehler sind nie fatal: Der
// Dienst muss auch dann starten, wenn er sich selbst nicht aufnehmen kann -
// die Verwaltung aller anderen Server haengt nicht daran.
func (s *SelfRegisterService) Run() {
	if err := s.register(); err != nil {
		slog.Warn("self-registration skipped", "error", err)
	}
}

func (s *SelfRegisterService) register() error {
	data, err := os.ReadFile(s.onboardPath())
	if errors.Is(err, os.ErrNotExist) {
		// Regelfall bei jedem Start nach dem ersten: nichts zu tun.
		return nil
	}
	if err != nil {
		return fmt.Errorf("read onboarding file: %w", err)
	}

	// Ab hier wird die Datei in JEDEM Fall entfernt - auch wenn die Aufnahme
	// scheitert oder bewusst unterbleibt. Sie enthaelt einen privaten
	// Schluessel im Klartext und darf nicht liegen bleiben, und ein erneuter
	// Versuch bei jedem Start wuerde denselben Fehler endlos wiederholen.
	defer s.removeOnboardFile()

	var ob selfOnboard
	if err := json.Unmarshal(data, &ob); err != nil {
		return fmt.Errorf("parse onboarding file: %w", err)
	}
	if strings.TrimSpace(ob.ServiceUser) == "" || strings.TrimSpace(ob.PrivateKeyPEM) == "" {
		return errors.New("onboarding file is incomplete")
	}

	// 1. Hat ein Administrator den Eintrag bewusst geloescht, bleibt er weg.
	//    Ohne diese Sperre kaeme er beim naechsten Paket-Update zurueck.
	set, err := s.settings.Get()
	if err != nil {
		return fmt.Errorf("load settings: %w", err)
	}
	if set.SelfServerDisabled {
		slog.Info("self-registration disabled by an administrator - not adding this host")
		return nil
	}

	// 2. Im Container ist "localhost" der Container selbst, nicht der Host.
	//    Ein Eintrag waere irrefuehrend und alle Host-Aktionen liefen ins Leere.
	if s.containerCheck != nil && s.containerCheck() {
		slog.Info("running in a container - this host is not added as a managed server")
		return nil
	}

	// 3. Ist der eigene Rechner bereits aufgenommen (ggf. unter anderem Namen),
	//    entsteht kein zweiter Eintrag.
	existing, err := s.servers.FindAllUnscoped()
	if err != nil {
		return fmt.Errorf("list servers: %w", err)
	}
	for i := range existing {
		if existing[i].IsLcmHost() {
			slog.Info("this host is already managed - skipping self-registration",
				"server", existing[i].Name)
			return nil
		}
	}

	// 4. Host-Key des eigenen SSH-Dienstes lesen. Er wird wie bei jedem
	//    anderen Server als Vertrauensanker gespeichert, damit spaetere
	//    Verbindungen striktes Host-Key-Checking machen.
	fingerprint, _, err := s.dialer.Probe("127.0.0.1", 22)
	if err != nil {
		return fmt.Errorf("no SSH service reachable on 127.0.0.1:22: %w", err)
	}

	privEnc, err := s.cipher.EncryptString(ob.PrivateKeyPEM)
	if err != nil {
		return fmt.Errorf("encrypt key: %w", err)
	}

	server := &domain.Server{
		Name:               SelfHostName,
		Host:               "localhost",
		SSHPort:            22,
		ServiceUser:        ob.ServiceUser,
		HostKeyFingerprint: fingerprint,
		PrivateKeyEnc:      privEnc,
		PublicKey:          ob.PublicKey,
		RestrictedSudo:     ob.RestrictedSudo,
		Transport:          domain.TransportSSH,
	}
	if err := s.servers.Create(server); err != nil {
		return fmt.Errorf("create server entry: %w", err)
	}

	slog.Info("=== this host was added as a managed server ===",
		"name", SelfHostName, "service_user", ob.ServiceUser,
		"hint", "remove it in the web interface if it is not wanted; it will not come back")
	return nil
}

// removeOnboardFile ueberschreibt die Uebergabedatei vor dem Loeschen. Das
// ist kein sicheres Loeschen auf modernen Dateisystemen (Journaling,
// Copy-on-Write, SSD-Wear-Leveling), verhindert aber, dass der Schluessel in
// einem einfach wiederherstellbaren geloeschten Block stehen bleibt.
func (s *SelfRegisterService) removeOnboardFile() {
	path := s.onboardPath()
	if info, err := os.Stat(path); err == nil && info.Size() > 0 {
		if f, err := os.OpenFile(path, os.O_WRONLY, 0o600); err == nil {
			zeros := make([]byte, info.Size())
			_, _ = f.Write(zeros)
			_ = f.Sync()
			_ = f.Close()
		}
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Warn("could not remove the onboarding file - it contains a private key",
			"path", path, "error", err)
	}
}
