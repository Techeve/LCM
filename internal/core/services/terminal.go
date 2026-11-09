package services

import (
	"errors"
	"log/slog"
	"sync"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/core/events"
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
// Deshalb hängen an ihr drei Schlösser, und keines davon ist Zierrat:
//
//   - eine EIGENE Berechtigung (servers:console), die nicht in servers:write
//     mitgeliefert wird und im Auslieferungszustand nur bei admin liegt;
//   - ein globaler Schalter, mit dem ein Betreiber die Fähigkeit ganz aus dem
//     Haus nehmen kann;
//   - der Mitschnitt jeder Sitzung (siehe recordingTerminal) - ohne ihn hätte
//     die Beweiskette ihr Loch genau dort, wo am meisten möglich ist.
//
// Eine Sitzung sperrt den Server nicht: Sie belegt einen eigenen Konsolen-Platz
// (siehe connectTerminal), nicht den Exec-Slot. Zeitpläne, Updates und Aktionen
// laufen daneben weiter; treffen apt-Läufe aufeinander, wartet LCM auf die
// Paketverwaltung.

// ErrTerminalDisabled: Der Betreiber hat die Konsole global abgeschaltet.
var ErrTerminalDisabled = errors.New("die web-konsole ist auf dieser installation abgeschaltet")

// ErrTerminalNotPossible: Auf diesem Server gibt es keine Konsole - Demo-Server
// werden nie kontaktiert, Synology DSM hat keine Shell, und der Agent-Transport
// kennt nur Frage und Antwort.
var ErrTerminalNotPossible = errors.New("auf diesem server ist keine konsole möglich")

// TerminalIdleTimeout beendet eine Sitzung, in der nichts mehr getippt wird.
//
// Ohne sie hielte ein vergessener Browser-Tab eine Shell auf dem Server und
// einen seiner Konsolen-Plätze auf Dauer offen. Variable, damit Tests sie
// herunterdrehen können.
var TerminalIdleTimeout = 15 * time.Minute

// OpenTerminal öffnet eine interaktive Sitzung auf einem Server.
//
// Der Aufrufer MUSS die Sitzung schließen - daran hängen der Mitschnitt, die
// SSH-Verbindung, der Konsolen-Platz und der Eintrag in OpenConsoles.
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

	conn, err := s.connectTerminal(server)
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
	return &serverTerminal{Terminal: t, conn: conn, server: server.Name, actor: actor, audit: s.audit, id: id,
		unregister: s.registerConsole(id, actor)}, nil
}

// registerConsole trägt eine offene Konsole ein und meldet es als server.status:
// Wer die Seite des Servers offen hat, soll die Konsole sofort sehen (und nicht
// erst beim nächsten Nachfragen). Die Meldung heißt hier „an diesem Server hat
// sich etwas geändert, neu laden" - die Ampel selbst bleibt unberührt. Die
// zurückgegebene Funktion trägt wieder aus und meldet ebenso.
func (s *ServerService) registerConsole(id uint, actor string) func() {
	remove := s.consoles.add(id, actor)
	s.bus.Publish(events.ServerStatus(&id))
	return func() {
		remove()
		s.bus.Publish(events.ServerStatus(&id))
	}
}

// OpenConsoles liefert die gerade offenen Web-Konsolen auf den Servern, die
// der Aufrufer sehen darf.
func (s *ServerService) OpenConsoles(scope repositories.AccessScope) ([]OpenConsole, error) {
	all := s.consoles.list()
	if scope.All || len(all) == 0 {
		return all, nil
	}
	visible, err := s.servers.FindAll(scope)
	if err != nil {
		return nil, err
	}
	allowed := make(map[uint]bool, len(visible))
	for _, srv := range visible {
		allowed[srv.ID] = true
	}
	out := all[:0]
	for _, c := range all {
		if allowed[c.ServerID] {
			out = append(out, c)
		}
	}
	return out, nil
}

// SetConsoleDisabled schaltet die Web-Konsole für einen Server ab oder wieder
// frei.
//
// Eigener Weg statt eines Feldes in den Einstellungen: Jene Route verlangt
// servers:write, und wer Server konfigurieren darf, soll über die Konsole
// nicht mitbestimmen - das Recht dafür liegt allein bei admin
// (PermServersConsole). Getrennte Route, getrenntes Recht.
func (s *ServerService) SetConsoleDisabled(scope repositories.AccessScope, id uint, disabled bool, actor string) error {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return err
	}
	if err := s.servers.UpdateFields(id, map[string]any{"console_disabled": disabled}); err != nil {
		return err
	}
	zustand := "frei"
	if disabled {
		zustand = "gesperrt"
	}
	s.audit.Log(actor, "server.console."+zustand, "server", id, server.Name)
	return nil
}

// terminalPossible prüft, ob dieser Servertyp überhaupt eine Shell hat.
func terminalPossible(server *domain.Server) error {
	switch {
	case server.ConsoleDisabled:
		// Für diesen Server ausdrücklich abgeschaltet. Die Prüfung steht hier
		// und nicht nur in der Oberfläche: Wer die Adresse kennt, bekommt
		// sonst trotzdem eine Fahrkarte.
		return ErrTerminalNotPossible
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
// Mitschnitt und den Konsolen-Platz des Servers - und trägt sich aus
// OpenConsoles aus. Close ist idempotent: Das Ende der Sitzung kann von
// beiden Seiten kommen (Browser weg, Shell beendet).
type serverTerminal struct {
	sshx.Terminal
	conn       sshx.Conn
	server     string
	actor      string
	audit      *AuditService
	id         uint
	unregister func()
	closeOnce  sync.Once
	closeErr   error
}

func (t *serverTerminal) Close() error {
	t.closeOnce.Do(func() {
		t.closeErr = t.Terminal.Close() // schreibt den Mitschnitt
		t.conn.Close()                  // gibt den Konsolen-Platz frei
		t.unregister()
		if t.audit != nil {
			t.audit.Log(t.actor, "server.terminal.close", "server", t.id, t.server)
		}
		slog.Info("terminal closed", "server", t.server, "actor", t.actor)
	})
	return t.closeErr
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
	store *ticketStore[terminalTicket]
}

type terminalTicket struct {
	actor    string
	serverID uint
	// clientIP ist die Adresse, die die Fahrkarte geholt hat. Nur sie darf
	// sie einlösen - eine mitgelesene Fahrkarte nützt von woanders nichts.
	clientIP string
}

func NewTerminalTickets() *TerminalTickets {
	return &TerminalTickets{store: newTicketStore[terminalTicket]()}
}

// Issue stellt eine Fahrkarte für einen Benutzer und einen Server aus,
// gebunden an die Client-Adresse, die sie geholt hat.
func (t *TerminalTickets) Issue(actor string, serverID uint, clientIP string) (string, error) {
	return t.store.issue(terminalTicket{actor: actor, serverID: serverID, clientIP: clientIP})
}

// Redeem löst eine Fahrkarte ein. Sie gilt genau einmal, nur für den
// Server, für den sie ausgestellt wurde, und nur von der Adresse, die sie
// geholt hat.
func (t *TerminalTickets) Redeem(token string, serverID uint, clientIP string) (string, bool) {
	ticket, ok := t.store.redeem(token, func(tk terminalTicket) bool {
		return tk.serverID == serverID && tk.clientIP == clientIP
	})
	return ticket.actor, ok
}
