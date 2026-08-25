package services

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/storage/repositories"
)

// LCM Remote-Onboarding für MikroTik RouterOS.
//
// RouterOS wird NICHT wie ein Linux-Server provisioniert (kein Service-User,
// keine sudo-Rechte, kein authorized_keys-Deployment). LCM verbindet sich rein
// lesend und überwacht die Versions-Aktualität. Zwei Authentifizierungswege:
//   - Passwort: LCM verbindet sofort, scannt und legt das Gerät online an; das
//     Login-Passwort wird AES-GCM-verschlüsselt gespeichert.
//   - Public-Key: LCM erzeugt ein Schlüsselpaar und legt das Gerät zunächst
//     offline an; der Admin importiert den Public Key auf dem Router
//     (/user ssh-keys import), danach verbindet der nächste Refresh.

// RouterOSRequest bündelt die Eingaben des RouterOS-Onboarding-Formulars.
type RouterOSRequest struct {
	Name                 string
	Host                 string
	Port                 int
	LoginUser            string
	LoginPassword        string
	AuthMethod           string // "password" (Default) oder "key"
	ConfirmedFingerprint string // vom Admin in der UI bestätigt
	Actor                string
}

// RouterOSResult ist das Ergebnis des Onboardings. PublicKey ist nur im
// Key-Modus gesetzt - er muss auf dem RouterOS-Gerät importiert werden.
type RouterOSResult struct {
	Server    *domain.Server `json:"server"`
	PublicKey string         `json:"public_key,omitempty"`
}

// CreateRouterOSServer legt ein RouterOS-Gerät an (OSID=routeros) und richtet
// die reine Überwachung ein.
func (s *ServerService) CreateRouterOSServer(req RouterOSRequest) (*RouterOSResult, error) {
	req.Name = strings.TrimSpace(req.Name)
	req.Host = strings.TrimSpace(req.Host)
	req.LoginUser = strings.TrimSpace(req.LoginUser)
	if req.Name == "" || req.Host == "" || req.LoginUser == "" {
		return nil, errors.New("name, host und benutzer sind erforderlich")
	}
	// LoginUser ist der EINZIGE frei wählbare Service-Benutzer im ganzen
	// System (überall sonst ist er die Konstante lcm-svc bzw. root). Er wird
	// als ServiceUser gespeichert und von dort in Skripte eingesetzt, die als
	// root laufen. Deshalb hier eine strikte Whitelist statt bloßem TrimSpace:
	// ein Wert wie "x; rm -rf /; #" darf gar nicht erst in die Datenbank
	// gelangen.
	if !domain.ValidLinuxUsername(req.LoginUser) {
		return nil, errors.New("ungültiger benutzername: erlaubt sind 1-32 zeichen aus a-z, 0-9, _, - (beginnend mit buchstabe oder _)")
	}
	if req.Port < 1 || req.Port > 65535 {
		if req.Port != 0 {
			return nil, errors.New("ungültiger port: erwartet wird 1-65535")
		}
	}
	if req.Port <= 0 {
		req.Port = 22
	}
	if req.AuthMethod == "" {
		req.AuthMethod = AuthMethodPassword
	}
	if _, err := s.servers.FindByName(req.Name); err == nil {
		return nil, ErrServerNameTaken
	} else if !errors.Is(err, repositories.ErrNotFound) {
		return nil, err
	}
	if taken, err := s.servers.HostExists(req.Host, req.Port, 0); err != nil {
		return nil, err
	} else if taken {
		return nil, ErrServerHostTaken
	}

	// Host-Key-Fingerprint absichern (MitM-Schutz, Trust-on-First-Use).
	fp, _, err := s.dialer.Probe(req.Host, req.Port)
	if err != nil {
		return nil, fmt.Errorf("host-key auslesen: %w", err)
	}
	if req.ConfirmedFingerprint != "" && fp != req.ConfirmedFingerprint {
		return nil, ErrFingerprintMismatch
	}

	server := &domain.Server{
		Name:               req.Name,
		Host:               req.Host,
		SSHPort:            req.Port,
		ServiceUser:        req.LoginUser,
		HostKeyFingerprint: fp,
		OSID:               domain.OSIDRouterOS,
		OSName:             "MikroTik RouterOS",
		Transport:          domain.TransportSSH,
	}

	if req.AuthMethod == AuthMethodKey {
		// Schlüsselpaar erzeugen; der Admin importiert den Public Key auf dem
		// Router. Bis dahin bleibt das Gerät offline.
		privPEM, pubLine, err := sshx.GenerateKeyPair("lcm@" + req.Name)
		if err != nil {
			return nil, fmt.Errorf("schlüsselpaar erzeugen: %w", err)
		}
		enc, err := s.cipher.EncryptString(privPEM)
		if err != nil {
			return nil, fmt.Errorf("private key verschlüsseln: %w", err)
		}
		server.PrivateKeyEnc = enc
		server.PublicKey = pubLine
		server.Reachable = false
		server.LastError = "SSH-Public-Key noch nicht auf dem RouterOS-Gerät importiert"
		if err := s.servers.Create(server); err != nil {
			return nil, err
		}
		s.assignToSystemGroup(server)
		s.recordRouterOSJoin(server, req.Actor, "Public Key erzeugt - auf dem Router importieren, dann verbindet der nächste Refresh.")
		return &RouterOSResult{Server: server, PublicKey: pubLine}, nil
	}

	// Passwort-Modus: sofort verbinden, scannen, online anlegen.
	if req.LoginPassword == "" {
		return nil, errors.New("passwort erforderlich (oder Key-Modus wählen)")
	}
	enc, err := s.cipher.EncryptString(req.LoginPassword)
	if err != nil {
		return nil, fmt.Errorf("passwort verschlüsseln: %w", err)
	}
	server.LoginPasswordEnc = enc

	rawConn, err := s.dialer.DialPassword(req.Host, req.Port, req.LoginUser, req.LoginPassword, fp)
	if err != nil {
		return nil, fmt.Errorf("login fehlgeschlagen: %w", err)
	}
	conn := s.recorder.Record(rawConn, SessionContext{
		Actor: req.Actor, Purpose: "join:" + req.Name, Host: req.Host, User: req.LoginUser,
	})
	defer conn.Close()

	scan := scanRouterOS(conn)
	if scan.OSVersion == "" {
		return nil, errors.New("kein RouterOS erkannt - /system resource print lieferte keine Version (ist das ein MikroTik-Gerät mit erlaubtem SSH-Zugang?)")
	}
	applyScan(server, scan)
	server.OSID = domain.OSIDRouterOS // applyScan übernimmt scan.OSID = routeros
	server.Reachable = true
	now := time.Now()
	server.LastSeenAt = &now
	if err := s.servers.Create(server); err != nil {
		return nil, err
	}
	s.assignToSystemGroup(server)
	s.recordRouterOSJoin(server, req.Actor,
		fmt.Sprintf("RouterOS erkannt: %s (%s), Board %s.", server.OSVersion, server.RouterOSChannel, server.RouterBoardModel))
	return &RouterOSResult{Server: server}, nil
}

// recordRouterOSJoin schreibt den Onboarding-Job und den Audit-Eintrag.
func (s *ServerService) recordRouterOSJoin(server *domain.Server, actor, detail string) {
	now := time.Now()
	job := &domain.Job{
		ServerID: &server.ID, Type: "join", Name: "RouterOS-Onboarding: " + server.Name,
		Status: domain.JobStatusSuccess, TriggeredBy: actor,
		StartedAt: &now, FinishedAt: &now,
		Output: "MikroTik-RouterOS-Gerät angelegt (reine Überwachung).\n" + detail,
	}
	_ = s.jobs.jobs.Create(job)
	s.audit.Log(actor, "server.routeros_create", "server", server.ID, server.Host)
}
