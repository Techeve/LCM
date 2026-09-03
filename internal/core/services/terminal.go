package services

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"log/slog"
	"sync"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/storage/repositories"
)

// Web-Konsole: eine interaktive Shell auf einem verwalteten Server, im Browser.
//
// Das ist die eingriffsstärkste Funktion, die LCM hat, und sie verändert seine
// Zusage: Bis hierher führte LCM DEFINIERTE Aktionen aus, jede einzelne im
// Protokoll. Eine Konsole heißt, dass jemand mit dem passenden Recht über
// LCMs Schlüssel eine Root-Shell auf jedem verwalteten Server bekommt.
//
// Deshalb hängen an ihr vier Schlösser, und keines davon ist Zierrat:
//
//   - eine EIGENE Berechtigung (servers:console), die nicht in servers:write
//     mitgeliefert wird und im Auslieferungszustand nur bei admin liegt;
//   - ein globaler Schalter, mit dem ein Betreiber die Fähigkeit ganz aus dem
//     Haus nehmen kann;
//   - der Mitschnitt jeder Sitzung (siehe recordingTerminal) - ohne ihn hätte
//     die Beweiskette ihr Loch genau dort, wo am meisten möglich ist;
//   - der Exec-Slot des Servers, den die Sitzung belegt: Solange die Konsole
//     offen ist, laufen dort keine Zeitpläne. Ein apt-Lauf neben einer
//     tippenden Hand ist der Konflikt, den die Server-Sperre verhindert.

// ErrTerminalDisabled: Der Betreiber hat die Konsole global abgeschaltet.
var ErrTerminalDisabled = errors.New("die web-konsole ist auf dieser installation abgeschaltet")

// ErrTerminalNotPossible: Auf diesem Server gibt es keine Konsole - Demo-Server
// werden nie kontaktiert, Synology DSM hat keine Shell, und der Agent-Transport
// kennt nur Frage und Antwort.
var ErrTerminalNotPossible = errors.New("auf diesem server ist keine konsole möglich")

// TerminalIdleTimeout beendet eine Sitzung, in der nichts mehr geschieht.
//
// Ohne sie hielte ein vergessener Browser-Tab den Exec-Slot des Servers auf
// Dauer besetzt: Zeitplan-Läufe würden sich einreihen und nach ihrem halben
// Takt verfallen - der Server fiele still aus der Verwaltung, ohne dass es
// jemandem auffiele. Variable, damit Tests sie herunterdrehen können.
var TerminalIdleTimeout = 15 * time.Minute

// OpenTerminal öffnet eine interaktive Sitzung auf einem Server.
//
// Der Aufrufer MUSS die Sitzung schließen - daran hängen der Mitschnitt, die
// SSH-Verbindung und der Exec-Slot des Servers.
func (s *ServerService) OpenTerminal(scope repositories.AccessScope, id uint, actor, term string, cols, rows int) (sshx.Terminal, error) {
	// Ohne verdrahtete Einstellungen bleibt die Konsole zu - siehe
	// WithSettings. Lieber eine Funktion, die nicht geht, als eine, die
	// entgegen dem Willen des Betreibers doch geht.
	if s.settings == nil {
		return nil, ErrTerminalDisabled
	}
	st, err := s.settings.Get()
	if err != nil {
		return nil, err
	}
	if !st.TerminalEnabled {
		return nil, ErrTerminalDisabled
	}
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return nil, err
	}
	if err := terminalPossible(server); err != nil {
		return nil, err
	}

	conn, err := s.connect(server)
	if err != nil {
		return nil, err
	}
	// Über den Recorder, nicht daran vorbei: Er legt die Sitzungszeile an und
	// hängt sich in den Ausgabestrom (siehe recordingConn.Terminal).
	conn = s.recorder.Record(conn, SessionContext{
		ServerID: server.ID, Actor: actor, Purpose: "terminal",
		Host: server.Host, User: server.ServiceUser,
	})

	tc, ok := conn.(sshx.TerminalConn)
	if !ok {
		conn.Close()
		return nil, ErrTerminalUnsupported
	}
	t, err := tc.Terminal(term, cols, rows)
	if err != nil {
		conn.Close()
		return nil, err
	}

	s.audit.Log(actor, "server.terminal.open", "server", id, server.Name)
	slog.Info("terminal opened", "server", server.Name, "actor", actor)
	return &serverTerminal{Terminal: t, conn: conn, server: server.Name, actor: actor, audit: s.audit, id: id}, nil
}

// terminalPossible prüft, ob dieser Servertyp überhaupt eine Shell hat.
func terminalPossible(server *domain.Server) error {
	switch {
	case server.IsDemo:
		// Demo-Server sind erfunden - es gibt nichts, womit man sprechen könnte.
		return ErrTerminalNotPossible
	case server.IsSynologyDSM():
		// DSM verwaltet LCM über die Web-API; eine Shell ist dort nicht
		// vorgesehen und wäre auch nicht die Art, ein DSM zu bedienen.
		return ErrTerminalNotPossible
	case server.IsAgent():
		// Der Agent-Transport führt keinen Strom (siehe sshx.TerminalConn).
		return ErrTerminalNotPossible
	case server.InMaintenance():
		// In Wartung heißt: bewusst außer Betrieb. Wer trotzdem heranwill,
		// beendet erst die Wartung - sonst hebelt die Konsole eine
		// Entscheidung aus, die jemand ausdrücklich getroffen hat.
		return ErrTerminalNotPossible
	}
	return nil
}

// serverTerminal schließt beim Beenden auch die Verbindung - und damit den
// Mitschnitt und den Exec-Slot des Servers.
type serverTerminal struct {
	sshx.Terminal
	conn   sshx.Conn
	server string
	actor  string
	audit  *AuditService
	id     uint
}

func (t *serverTerminal) Close() error {
	err := t.Terminal.Close() // schreibt den Mitschnitt
	t.conn.Close()            // gibt den Exec-Slot frei
	if t.audit != nil {
		t.audit.Log(t.actor, "server.terminal.close", "server", t.id, t.server)
	}
	slog.Info("terminal closed", "server", t.server, "actor", t.actor)
	return err
}

// --- Einmal-Fahrkarten für den Verbindungsaufbau ------------------------------

// TerminalTickets vergibt kurzlebige Einmal-Fahrkarten, mit denen der Browser
// die WebSocket-Verbindung aufbaut.
//
// Der Umweg ist nötig, weil die WebSocket-Schnittstelle des Browsers keine
// eigenen Kopfzeilen setzen kann - der Anmelde-Token käme dort nicht an. Der
// naheliegende Ausweg wäre, ihn an die URL zu hängen; genau das verbietet
// sich: URLs stehen in Zugriffs- und Proxy-Protokollen, und dieser Token
// öffnet eine Root-Shell.
//
// Eine Fahrkarte ist deshalb das Gegenteil davon: Sie gilt dreißig Sekunden,
// genau einmal, für genau einen Server und genau den Benutzer, der sie geholt
// hat. Steht sie später in einem Protokoll, ist sie längst wertlos.
type TerminalTickets struct {
	mu      sync.Mutex
	tickets map[string]terminalTicket
}

type terminalTicket struct {
	actor    string
	serverID uint
	expires  time.Time
}

// ticketTTL ist die Lebensdauer einer Fahrkarte - lang genug für den
// unmittelbar folgenden Verbindungsaufbau, zu kurz für alles andere.
const ticketTTL = 30 * time.Second

func NewTerminalTickets() *TerminalTickets {
	return &TerminalTickets{tickets: map[string]terminalTicket{}}
}

// Issue stellt eine Fahrkarte für einen Benutzer und einen Server aus.
func (t *TerminalTickets) Issue(actor string, serverID uint) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)

	t.mu.Lock()
	defer t.mu.Unlock()
	t.purgeLocked()
	t.tickets[token] = terminalTicket{actor: actor, serverID: serverID, expires: time.Now().Add(ticketTTL)}
	return token, nil
}

// Redeem löst eine Fahrkarte ein. Sie gilt genau einmal und nur für den
// Server, für den sie ausgestellt wurde.
func (t *TerminalTickets) Redeem(token string, serverID uint) (string, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.purgeLocked()
	ticket, ok := t.tickets[token]
	if !ok || ticket.serverID != serverID {
		return "", false
	}
	delete(t.tickets, token) // einmal heißt einmal
	return ticket.actor, true
}

// purgeLocked räumt abgelaufene Fahrkarten weg. Erwartet den gehaltenen Mutex.
func (t *TerminalTickets) purgeLocked() {
	now := time.Now()
	for token, ticket := range t.tickets {
		if now.After(ticket.expires) {
			delete(t.tickets, token)
		}
	}
}
