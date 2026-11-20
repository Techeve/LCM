package services

import (
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/tlsx"
	"LCM/internal/storage/repositories"
)

// Erneuerung des Server-Zertifikats durch die eigene CA.
//
// Die CA (tlsx.CA) stellt das Server-Zertifikat für 90 Tage aus; LCM
// erneuert es, sobald weniger als 60 Tage übrig sind, also monatlich, und
// zwar mit neuem Schlüssel. Agenten pinnen die CA und folgen dem Wechsel.
//
// Der Umstieg vom selbstsignierten Zertifikat früherer Fassungen läuft in
// zwei Schritten, damit kein Agent die Verbindung verliert:
//
//  1. Das erste Zertifikat der CA behält den bisherigen Schlüssel. Agenten
//     ab R2 nehmen es über ihren Schlüssel-Pin an und lernen die CA aus der
//     Kette. Agenten bis 2.0 kennen nur das alte Zertifikat und könnten
//     nicht folgen - solange einer davon aktiv ist, bleibt es beim alten
//     Zertifikat, und die Warnung nennt ihn.
//  2. Erst wenn jeder aktive Agent den Pin der CA meldet, bekommt das
//     Zertifikat bei der Erneuerung einen neuen Schlüssel. Bis dahin
//     erneuert LCM mit demselben Schlüssel.
//
// Aktiv heißt: jetzt verbunden oder in den letzten 14 Tagen gesehen. Wer
// länger fehlt, hält den Umstieg nicht auf (Entscheidung Tony) und muss
// danach gegebenenfalls neu eingerichtet werden; das Protokoll nennt ihn.
//
// Derselbe Weg greift, wenn die CA wechselt (neue CA des Betreibers): Das
// Zertifikat der alten CA gilt dann als noch nicht umgestellt.

// Fristen. Variablen, damit Tests sie verschieben können.
var (
	// CertRenewBefore: so lange vor Ablauf erneuert LCM sein Zertifikat.
	CertRenewBefore = 60 * 24 * time.Hour
	// CertWarnBefore: ab hier warnt LCM vor dem Ablauf eines Zertifikats,
	// das es nicht selbst erneuern kann.
	CertWarnBefore = 60 * 24 * time.Hour
	// CAWarnBefore: ab hier warnt LCM vor dem Ablauf der CA - LCM erneuert
	// sie nicht selbst.
	CAWarnBefore = 365 * 24 * time.Hour
	// AgentStaleAfter: ein länger nicht gesehener Agent hält den Umstieg
	// nicht auf.
	AgentStaleAfter = 14 * 24 * time.Hour
)

// ErrNoCA: Es gibt keine CA (--dev ohne TLS).
var ErrNoCA = errors.New("keine tls-ca eingerichtet")

// CertState ist der Stand des Zertifikats im Datenverzeichnis.
type CertState struct {
	NotAfter time.Time
	// Own: von LCM verwaltet (nur das erneuert LCM).
	Own bool
	// FromCA: von der aktuellen CA ausgestellt (Schritt 1 erledigt).
	FromCA bool
	// CANotAfter ist das Ende der CA, CAOwn sagt, ob LCM sie erzeugt hat.
	CANotAfter time.Time
	CAOwn      bool
	// WithoutKeyPin sind die aktiven Agenten, die den Schlüssel des
	// Zertifikats nicht führen - sie halten Schritt 1 auf.
	WithoutKeyPin []string
	// WithoutCAPin sind die aktiven Agenten ohne den Pin der CA - solange es
	// welche gibt, behält das Zertifikat seinen Schlüssel (Schritt 2).
	WithoutCAPin []string
	// StaleWithoutCAPin sind die länger nicht gesehenen Agenten ohne den Pin
	// der CA: Nach dem Schlüsselwechsel brauchen sie ein neues Token.
	StaleWithoutCAPin []string
}

// CertRenewal prüft das Zertifikat und erneuert es rechtzeitig.
type CertRenewal struct {
	dataDir string
	servers *repositories.ServerRepository
	// reload lädt das erneuerte Zertifikat in die Listener (tlsx.CertStore).
	reload func() error
	// online meldet, ob ein Agent gerade verbunden ist (AgentHub.Online).
	online func(agentID string) bool
	now    func() time.Time
	// ca setzt der HTTPS-Start; bis dahin (und im --dev-Modus) gibt es keine.
	ca atomic.Pointer[tlsx.CA]
}

// NewCertRenewal baut die Erneuerung für das Datenverzeichnis.
func NewCertRenewal(dataDir string, servers *repositories.ServerRepository, reload func() error) *CertRenewal {
	return &CertRenewal{dataDir: dataDir, servers: servers, reload: reload, now: time.Now}
}

// WithOnline verdrahtet die Abfrage, ob ein Agent gerade verbunden ist.
func (r *CertRenewal) WithOnline(fn func(agentID string) bool) *CertRenewal {
	r.online = fn
	return r
}

// SetCA legt die CA fest, die das Zertifikat ausstellt.
func (r *CertRenewal) SetCA(ca *tlsx.CA) {
	r.ca.Store(ca)
}

// CAPEM liefert den öffentlichen Teil der CA zum Herunterladen.
func (r *CertRenewal) CAPEM() ([]byte, error) {
	ca := r.ca.Load()
	if ca == nil {
		return nil, ErrNoCA
	}
	return ca.PEM(), nil
}

// TokenCAFingerprint liefert den CA-Pin fürs Enrollment-Token - leer, wenn
// LCM ein Zertifikat des Betreibers ausliefert, das die CA nicht ausstellt.
func (r *CertRenewal) TokenCAFingerprint() (string, error) {
	ca := r.ca.Load()
	if ca == nil {
		return "", ErrNoCA
	}
	info, err := tlsx.Inspect(r.dataDir, ca)
	if err != nil || !info.Own {
		return "", err
	}
	return ca.Fingerprint(), nil
}

// State liest den Stand, ohne etwas zu ändern.
func (r *CertRenewal) State() (CertState, error) {
	ca := r.ca.Load()
	if ca == nil {
		return CertState{}, ErrNoCA
	}
	info, err := tlsx.Inspect(r.dataDir, ca)
	if err != nil {
		return CertState{}, err
	}
	st := CertState{NotAfter: info.NotAfter, Own: info.Own, FromCA: info.FromCA,
		CANotAfter: ca.Cert.NotAfter, CAOwn: ca.Own}
	if !info.Own {
		return st, nil
	}
	active, stale, err := r.agents()
	if err != nil {
		return st, err
	}
	if !info.FromCA {
		st.WithoutKeyPin = names(active, func(s *domain.Server) bool { return s.AgentKeyPin != info.KeyPin })
		return st, nil
	}
	withoutCA := func(s *domain.Server) bool { return s.AgentCAPin != ca.Fingerprint() }
	st.WithoutCAPin = names(active, withoutCA)
	st.StaleWithoutCAPin = names(stale, withoutCA)
	return st, nil
}

// agents teilt die Agenten-Server in aktive und länger nicht gesehene.
// Ein Agent, der sich nie verbunden hat, zählt zu keiner Gruppe.
func (r *CertRenewal) agents() (active, stale []domain.Server, err error) {
	servers, err := r.servers.FindAllUnscoped()
	if err != nil {
		return nil, nil, err
	}
	for i := range servers {
		s := servers[i]
		switch {
		case !s.IsAgent():
		case r.online != nil && r.online(s.AgentID):
			active = append(active, s)
		case s.AgentLastSeenAt == nil:
		case r.now().Sub(*s.AgentLastSeenAt) <= AgentStaleAfter:
			active = append(active, s)
		default:
			stale = append(stale, s)
		}
	}
	return active, stale, nil
}

// names liefert die sortierten Namen der Server, auf die match zutrifft.
func names(servers []domain.Server, match func(*domain.Server) bool) []string {
	var out []string
	for i := range servers {
		if match(&servers[i]) {
			out = append(out, servers[i].Name)
		}
	}
	slices.Sort(out)
	return out
}

// Run prüft das Zertifikat, erneuert es, wenn es so weit ist, und meldet
// den Stand im Protokoll. Läuft beim Start vor den Listenern - dann
// verbinden sich die Agenten gleich mit dem neuen Zertifikat - und danach
// täglich (RunDaily).
func (r *CertRenewal) Run() {
	st, err := r.State()
	if err != nil {
		slog.Debug("tls certificate not checked", "error", err)
		return
	}
	r.step(st)
	if len(st.WithoutCAPin) > 0 {
		slog.Warn("tls certificate keeps its key until every agent knows the lcm ca - update lcm-agent there",
			"agents_without_ca_pin", strings.Join(st.WithoutCAPin, ", "))
	}
	if msg := r.Warning(st); msg != "" {
		slog.Warn("tls certificate needs attention", "hint", msg)
	}
}

// step führt den nächsten Schritt aus, wenn er ansteht.
func (r *CertRenewal) step(st CertState) {
	switch {
	case !st.Own:
		return // Zertifikat des Betreibers
	case !st.FromCA && len(st.WithoutKeyPin) > 0:
		slog.Warn("tls certificate stays on the old key chain until every agent knows its key",
			"agents_without_key_pin", strings.Join(st.WithoutKeyPin, ", "))
	case !st.FromCA:
		r.issue(false, "tls certificate now issued by the lcm ca, with the same key")
	case st.NotAfter.Sub(r.now()) > CertRenewBefore, !st.NotAfter.Before(st.CANotAfter):
		return // noch nicht fällig, oder es endet schon mit der CA
	case len(st.WithoutCAPin) > 0:
		r.issue(false, "tls certificate renewed with the same key")
	default:
		if len(st.StaleWithoutCAPin) > 0 {
			slog.Warn("agents offline for more than 14 days without the lcm ca pin need a new token",
				"agents", strings.Join(st.StaleWithoutCAPin, ", "))
		}
		r.issue(true, "tls certificate renewed with a new key")
	}
}

// RunDaily prüft einmal am Tag (den ersten Lauf macht der Start mit Run).
func (r *CertRenewal) RunDaily() {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		r.Run()
	}
}

// issue stellt das Nachfolge-Zertifikat aus und nimmt es in Betrieb.
func (r *CertRenewal) issue(newKey bool, done string) {
	// Eine Stunde Vorlauf gegen Uhren, die etwas nachgehen.
	if err := tlsx.IssueServerCert(r.dataDir, r.ca.Load(), r.now().Add(-time.Hour), newKey); err != nil {
		slog.Error("tls certificate could not be issued", "error", err)
		return
	}
	if r.reload != nil {
		if err := r.reload(); err != nil {
			slog.Error("issued tls certificate could not be loaded - takes effect after the next restart", "error", err)
			return
		}
	}
	slog.Info(done)
}

// Warning ist der Hinweis an den Admin - leer, solange nichts zu tun ist.
func (r *CertRenewal) Warning(st CertState) string {
	var parts []string
	for _, msg := range []string{r.certWarning(st), r.caWarning(st)} {
		if msg != "" {
			parts = append(parts, msg)
		}
	}
	return strings.Join(parts, " ")
}

// certWarning: Das Zertifikat läuft bald ab, und LCM kann es nicht selbst
// erneuern.
func (r *CertRenewal) certWarning(st CertState) string {
	if st.NotAfter.IsZero() || st.NotAfter.Sub(r.now()) > CertWarnBefore {
		return ""
	}
	date := st.NotAfter.Format("02.01.2006")
	if !st.Own {
		return fmt.Sprintf("Das TLS-Zertifikat von LCM läuft am %s ab. Es ist ein eigenes Zertifikat - "+
			"LCM erneuert es nicht selbst, bitte rechtzeitig ersetzen.", date)
	}
	if st.FromCA || len(st.WithoutKeyPin) == 0 {
		return "" // erneuert sich selbst
	}
	return fmt.Sprintf("Das TLS-Zertifikat von LCM läuft am %s ab. LCM stellt es erst auf die eigene CA um, wenn "+
		"jeder Agent den Schlüssel-Pin führt - sonst verlören diese Server die Verbindung. Noch ohne: %s. "+
		"Den Pin melden Agenten ab R2 bei der nächsten Verbindung; Agenten bis 2.0 kennen nur das alte "+
		"Zertifikat und können nicht folgen - dort bitte zuerst lcm-agent aktualisieren.",
		date, strings.Join(st.WithoutKeyPin, ", "))
}

// caWarning: Die CA läuft in absehbarer Zeit ab - LCM erneuert sie nicht.
func (r *CertRenewal) caWarning(st CertState) string {
	if st.CANotAfter.IsZero() || st.CANotAfter.Sub(r.now()) > CAWarnBefore {
		return ""
	}
	date := st.CANotAfter.Format("02.01.2006")
	if !st.CAOwn {
		return fmt.Sprintf("Die CA aus tls_ca_cert läuft am %s ab. Bitte rechtzeitig eine neue hinterlegen und LCM "+
			"neu starten; die Agenten übernehmen sie über den Schlüssel-Pin.", date)
	}
	return fmt.Sprintf("Die CA von LCM läuft am %s ab. Zum Wechsel %s und %s im Datenverzeichnis entfernen und "+
		"LCM neu starten; die Agenten übernehmen die neue CA über den Schlüssel-Pin.",
		date, tlsx.CAFileName, tlsx.CAKeyFileName)
}
