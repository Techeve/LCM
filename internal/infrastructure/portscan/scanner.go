package portscan

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"sort"
	"sync"
	"syscall"
	"time"
)

// State ist das Ergebnis eines einzelnen Verbindungsversuchs.
type State string

const (
	// Open: Der Verbindungsaufbau gelang - ein Dienst nimmt an.
	Open State = "open"
	// Closed: Die Gegenseite lehnte ab (RST). Der Host ist erreichbar, auf
	// dem Port lauscht nichts.
	Closed State = "closed"
	// Filtered: Keine Antwort in der Wartezeit oder ein anderer Fehler -
	// typischerweise eine Firewall, die Pakete verwirft.
	Filtered State = "filtered"
)

// Probe ist ein geprüfter Port an einer Adresse.
type Probe struct {
	Addr  netip.Addr
	Port  int
	State State
}

// DialFunc baut eine Verbindung auf (net.Dialer.DialContext; in Tests ein
// Ersatz).
type DialFunc func(ctx context.Context, network, address string) (net.Conn, error)

// Vorgaben: höchstens vier Verbindungen zugleich, zwei Sekunden Wartezeit je
// Port und 50 ms Abstand zwischen zwei Verbindungsaufbauten (höchstens 20
// je Sekunde). Ein Durchgang über die Liste dauert so wenige Sekunden und
// fällt in keinem Netz als Flut auf.
const (
	DefaultWorkers = 4
	DefaultTimeout = 2 * time.Second
	DefaultGap     = 50 * time.Millisecond
)

// Scanner prüft Ports per TCP connect.
type Scanner struct {
	Dial    DialFunc
	Workers int
	Timeout time.Duration
	// Gap ist der Mindestabstand zwischen zwei Verbindungsaufbauten.
	Gap time.Duration
	// OnProbe meldet jeden abgeschlossenen Versuch (Lebenszeichen für den
	// Job-Watchdog). Optional.
	OnProbe func()
}

// New liefert einen Scanner mit den Vorgaben.
func New() *Scanner {
	var d net.Dialer
	return &Scanner{Dial: d.DialContext, Workers: DefaultWorkers, Timeout: DefaultTimeout, Gap: DefaultGap}
}

type task struct {
	addr netip.Addr
	port int
}

// Scan prüft jeden Port an jeder Adresse. Das Ergebnis ist nach Adresse und
// Port sortiert. Wird ctx beendet, liefert Scan die bis dahin fertigen
// Versuche und ctx.Err() - abgebrochene Versuche zählen nicht als gefiltert.
func (s *Scanner) Scan(ctx context.Context, addrs []netip.Addr, ports []int) ([]Probe, error) {
	tasks := make(chan task)
	results := make(chan Probe)
	var wg sync.WaitGroup
	for range max(s.Workers, 1) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.work(ctx, tasks, results)
		}()
	}
	go func() {
		s.feed(ctx, tasks, addrs, ports)
		close(tasks)
		wg.Wait()
		close(results)
	}()

	var probes []Probe
	for p := range results {
		probes = append(probes, p)
	}
	sortProbes(probes)
	return probes, ctx.Err()
}

// feed verteilt die Versuche im Abstand Gap an die Arbeiter.
func (s *Scanner) feed(ctx context.Context, tasks chan<- task, addrs []netip.Addr, ports []int) {
	first := true
	for _, addr := range addrs {
		for _, port := range ports {
			if !first && !sleep(ctx, s.Gap) {
				return
			}
			first = false
			select {
			case tasks <- task{addr: addr, port: port}:
			case <-ctx.Done():
				return
			}
		}
	}
}

// work arbeitet Versuche ab, bis keine mehr kommen.
func (s *Scanner) work(ctx context.Context, tasks <-chan task, results chan<- Probe) {
	for t := range tasks {
		state, ok := s.probe(ctx, t.addr, t.port)
		if s.OnProbe != nil {
			s.OnProbe()
		}
		if ok {
			results <- Probe{Addr: t.addr, Port: t.port, State: state}
		}
	}
}

// probe versucht einen Verbindungsaufbau. ok=false: Der Auftrag wurde
// abgebrochen, das Ergebnis sagt nichts über den Port.
func (s *Scanner) probe(ctx context.Context, addr netip.Addr, port int) (State, bool) {
	dctx, cancel := context.WithTimeout(ctx, s.Timeout)
	defer cancel()
	conn, err := s.Dial(dctx, "tcp", netip.AddrPortFrom(addr, uint16(port)).String())
	if ctx.Err() != nil {
		if conn != nil {
			_ = conn.Close()
		}
		return "", false
	}
	if err == nil {
		_ = conn.Close()
		return Open, true
	}
	return classify(err), true
}

// classify ordnet einen Verbindungsfehler ein: nur eine ausdrückliche
// Ablehnung heißt geschlossen, alles andere (Zeitüberschreitung, nicht
// erreichbar) gilt als gefiltert.
func classify(err error) State {
	if errors.Is(err, syscall.ECONNREFUSED) {
		return Closed
	}
	return Filtered
}

// sleep wartet d oder bis ctx endet; false heißt abgebrochen.
func sleep(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func sortProbes(probes []Probe) {
	sort.Slice(probes, func(i, j int) bool {
		if c := probes[i].Addr.Compare(probes[j].Addr); c != 0 {
			return c < 0
		}
		return probes[i].Port < probes[j].Port
	})
}
