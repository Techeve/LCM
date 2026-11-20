package portscan

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

// lauscher öffnet einen Port auf 127.0.0.1 und nimmt Verbindungen an.
func lauscher(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port
}

// freierPort liefert einen Port, auf dem sicher niemand lauscht.
func freierPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}

var lokal = netip.MustParseAddr("127.0.0.1")

func schnellerScanner() *Scanner {
	s := New()
	s.Gap = 0
	s.Timeout = 300 * time.Millisecond
	return s
}

func TestScanErkenntOffenUndGeschlossen(t *testing.T) {
	offen, zu := lauscher(t), freierPort(t)
	probes, err := schnellerScanner().Scan(context.Background(), []netip.Addr{lokal}, []int{zu, offen})
	if err != nil {
		t.Fatal(err)
	}
	got := map[int]State{}
	for _, p := range probes {
		got[p.Port] = p.State
	}
	if got[offen] != Open || got[zu] != Closed {
		t.Fatalf("offen=%s zu=%s, erwartet open/closed", got[offen], got[zu])
	}
}

// Ein Dialer, der nie antwortet, steht für eine Firewall, die Pakete
// verwirft: Nach der Wartezeit gilt der Port als gefiltert.
func TestScanWertetZeitueberschreitungAlsGefiltert(t *testing.T) {
	s := schnellerScanner()
	s.Timeout = 50 * time.Millisecond
	s.Dial = func(ctx context.Context, _, _ string) (net.Conn, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	probes, err := s.Scan(context.Background(), []netip.Addr{netip.MustParseAddr("192.0.2.1")}, []int{80})
	if err != nil {
		t.Fatal(err)
	}
	if len(probes) != 1 || probes[0].State != Filtered {
		t.Fatalf("erwartet ein gefiltertes Ergebnis, bekam %+v", probes)
	}
}

// Ein echtes nicht routbares Ziel (TEST-NET-1) mit kurzer Wartezeit: kein
// Verbindungsaufbau, also gefiltert - nie offen.
func TestScanUnroutbaresZielIstNichtOffen(t *testing.T) {
	s := schnellerScanner()
	s.Timeout = 100 * time.Millisecond
	probes, err := s.Scan(context.Background(), []netip.Addr{netip.MustParseAddr("192.0.2.1")}, []int{22})
	if err != nil {
		t.Fatal(err)
	}
	if len(probes) != 1 || probes[0].State == Open {
		t.Fatalf("unroutbares Ziel darf nicht offen sein: %+v", probes)
	}
}

func TestScanBegrenztGleichzeitigeVerbindungen(t *testing.T) {
	var aktiv, spitze atomic.Int32
	s := schnellerScanner()
	s.Workers = 2
	s.Dial = func(context.Context, string, string) (net.Conn, error) {
		n := aktiv.Add(1)
		for {
			alt := spitze.Load()
			if n <= alt || spitze.CompareAndSwap(alt, n) {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
		aktiv.Add(-1)
		return nil, errors.New("abgelehnt")
	}
	if _, err := s.Scan(context.Background(), []netip.Addr{lokal}, Ports()); err != nil {
		t.Fatal(err)
	}
	if spitze.Load() > 2 {
		t.Fatalf("%d Verbindungen zugleich, erlaubt sind 2", spitze.Load())
	}
}

func TestScanHaeltDenAbstandEin(t *testing.T) {
	s := schnellerScanner()
	s.Gap = 20 * time.Millisecond
	s.Dial = func(context.Context, string, string) (net.Conn, error) { return nil, errors.New("x") }
	start := time.Now()
	if _, err := s.Scan(context.Background(), []netip.Addr{lokal}, []int{1, 2, 3, 4, 5}); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d < 80*time.Millisecond {
		t.Fatalf("fünf Versuche in %v - der Abstand von 20 ms greift nicht", d)
	}
}

func TestScanBrichtBeiAbbruchAb(t *testing.T) {
	s := schnellerScanner()
	s.Gap = 50 * time.Millisecond
	var versuche atomic.Int32
	s.Dial = func(context.Context, string, string) (net.Conn, error) {
		versuche.Add(1)
		return nil, errors.New("x")
	}
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(120*time.Millisecond, cancel)
	probes, err := s.Scan(ctx, []netip.Addr{lokal}, Ports())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("erwartet context.Canceled, bekam %v", err)
	}
	if len(probes) >= len(Ports()) || versuche.Load() >= int32(len(Ports())) {
		t.Fatalf("Scan lief nach dem Abbruch weiter: %d Ergebnisse", len(probes))
	}
}

func TestScanSortiertNachAdresseUndPort(t *testing.T) {
	s := schnellerScanner()
	s.Dial = func(context.Context, string, string) (net.Conn, error) { return nil, errors.New("x") }
	b, a := netip.MustParseAddr("192.0.2.2"), netip.MustParseAddr("192.0.2.1")
	probes, _ := s.Scan(context.Background(), []netip.Addr{b, a}, []int{443, 22})
	var got []string
	for _, p := range probes {
		got = append(got, p.Addr.String()+":"+strconv.Itoa(p.Port))
	}
	want := []string{"192.0.2.1:22", "192.0.2.1:443", "192.0.2.2:22", "192.0.2.2:443"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestPortsOhneDoppelte(t *testing.T) {
	ports := Ports(22, 2222, 0, 70000, 2222)
	seen := map[int]bool{}
	for _, p := range ports {
		if seen[p] {
			t.Fatalf("Port %d doppelt", p)
		}
		seen[p] = true
	}
	if !seen[2222] || seen[0] || seen[70000] {
		t.Fatalf("Zusatzports falsch übernommen: %v", ports)
	}
	if Lookup(2375).Name != "docker" || !Lookup(2375).Risky || Lookup(2376).Risky {
		t.Fatal("Docker ohne TLS muss riskant sein, mit TLS nicht")
	}
}
