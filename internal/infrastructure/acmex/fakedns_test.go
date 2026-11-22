package acmex

import (
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// Für TSIG-Tests: Schlüsselname und Secret des Test-DNS.
const (
	fakeTSIGKey    = "acme-key."
	fakeTSIGSecret = "c2VjcmV0LWZ1ZXItZGVuLXRlc3Q=" // base64
)

// fakeDNS ist ein kleiner DNS-Server auf Loopback: Er beantwortet TXT- und
// SOA-Fragen für eine Zone und nimmt dynamische Updates (RFC 2136) mit TSIG an.
// So lassen sich Anbieter und Probe ohne Netz bis zum Ende prüfen.
type fakeDNS struct {
	addr string
	zone string // mit Punkt am Ende

	mu      sync.Mutex
	txt     map[string][]string // Name -> Werte
	updates int
	badTSIG int // Updates mit fehlender oder falscher Signatur
}

func startFakeDNS(t *testing.T, zone string) *fakeDNS {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeDNS{addr: conn.LocalAddr().String(), zone: dns.Fqdn(zone), txt: map[string][]string{}}
	started := make(chan struct{})
	srv := &dns.Server{
		PacketConn:        conn,
		Handler:           f,
		TsigSecret:        map[string]string{fakeTSIGKey: fakeTSIGSecret},
		MsgAcceptFunc:     func(dns.Header) dns.MsgAcceptAction { return dns.MsgAccept }, // auch UPDATE
		NotifyStartedFunc: func() { close(started) },
	}
	go func() { _ = srv.ActivateAndServe() }()
	<-started
	t.Cleanup(func() { _ = srv.Shutdown() })
	return f
}

func (f *fakeDNS) ServeDNS(w dns.ResponseWriter, r *dns.Msg) {
	reply := new(dns.Msg).SetReply(r)
	switch r.Opcode {
	case dns.OpcodeUpdate:
		f.update(w, r, reply)
	default:
		f.query(r, reply)
	}
	if tsig := r.IsTsig(); tsig != nil && w.TsigStatus() == nil {
		reply.SetTsig(tsig.Hdr.Name, tsig.Algorithm, 300, time.Now().Unix())
	}
	_ = w.WriteMsg(reply)
}

func (f *fakeDNS) query(r, reply *dns.Msg) {
	q := r.Question[0]
	name := strings.ToLower(q.Name)
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case q.Qtype == dns.TypeTXT:
		for _, v := range f.txt[name] {
			reply.Answer = append(reply.Answer, &dns.TXT{
				Hdr: dns.RR_Header{Name: q.Name, Rrtype: dns.TypeTXT, Class: dns.ClassINET, Ttl: 60},
				Txt: []string{v},
			})
		}
	case q.Qtype == dns.TypeSOA && name == f.zone:
		reply.Answer = append(reply.Answer, &dns.SOA{
			Hdr: dns.RR_Header{Name: q.Name, Rrtype: dns.TypeSOA, Class: dns.ClassINET, Ttl: 60},
			Ns:  "ns." + f.zone, Mbox: "admin." + f.zone, Serial: 1, Refresh: 60, Retry: 60, Expire: 60, Minttl: 60,
		})
	}
}

func (f *fakeDNS) update(w dns.ResponseWriter, r, reply *dns.Msg) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updates++
	if r.IsTsig() == nil || w.TsigStatus() != nil {
		f.badTSIG++
		reply.Rcode = dns.RcodeNotAuth
		return
	}
	for _, rr := range r.Ns {
		hdr := rr.Header()
		name := strings.ToLower(hdr.Name)
		switch hdr.Class {
		case dns.ClassANY: // RRset entfernen
			delete(f.txt, name)
		case dns.ClassNONE: // einen Wert entfernen
			f.removeValue(name, strings.Join(rr.(*dns.TXT).Txt, ""))
		case dns.ClassINET:
			f.txt[name] = append(f.txt[name], strings.Join(rr.(*dns.TXT).Txt, ""))
		}
	}
}

func (f *fakeDNS) removeValue(name, value string) {
	var kept []string
	for _, v := range f.txt[name] {
		if v != value {
			kept = append(kept, v)
		}
	}
	if len(kept) == 0 {
		delete(f.txt, name)
		return
	}
	f.txt[name] = kept
}

func (f *fakeDNS) records() map[string][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string][]string{}
	for k, v := range f.txt {
		out[k] = append([]string(nil), v...)
	}
	return out
}

// stats: Zahl der Updates und davon die ohne gültige Signatur.
func (f *fakeDNS) stats() (updates, unsigned int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.updates, f.badTSIG
}

func (f *fakeDNS) set(name, value string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.txt[strings.ToLower(dns.Fqdn(name))] = []string{value}
}

// rfc2136Values sind die Werte, mit denen der Anbieter rfc2136 diesen Test-DNS
// erreicht.
func (f *fakeDNS) rfc2136Values() map[string]string {
	return map[string]string{
		"nameserver":     f.addr,
		"tsig_key":       strings.TrimSuffix(fakeTSIGKey, "."),
		"tsig_algorithm": "hmac-sha256",
		"tsig_secret":    fakeTSIGSecret,
	}
}
