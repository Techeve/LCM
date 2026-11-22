package tlsx

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

// Source sagt, woher ein Zertifikat im Pool stammt.
type Source string

const (
	SourceConfig Source = "config" // tls_certificates in der config.json
	SourceUpload Source = "upload" // über die Oberfläche hochgeladen
	SourceACME   Source = "acme"   // von LCM bei einer ACME-CA bezogen
	SourceLCMCA  Source = "lcm-ca" // das Zertifikat der eigenen CA (Rückfall)
)

// sourceOrder ist die Rangfolge bei gleichen Namen: Was der Betreiber in die
// config.json schreibt, geht vor dem Upload, und der geht vor ACME.
var sourceOrder = []Source{SourceConfig, SourceUpload, SourceACME}

// PoolSpec nennt ein Zertifikat, das der Pool laden soll. Die ID ist stabil
// über Neustarts und Neuladen: Daran erkennt der Pool, welcher bisherige
// Eintrag bei einem Ladefehler aktiv bleibt.
type PoolSpec struct {
	ID       string
	Source   Source
	CertPath string
	KeyPath  string
}

// poolEntry ist ein geladenes und geprüftes Zertifikat.
type poolEntry struct {
	spec     PoolSpec
	pair     *ValidatedPair
	names    []string
	issuer   string
	notAfter time.Time
}

// Pool liefert pro Handshake das Zertifikat, das zum angefragten Namen
// passt (SNI). Ohne SNI oder ohne Treffer gilt der Rückfall, das Zertifikat
// der eigenen CA - so reißen bestehende Agenten nie ab.
type Pool struct {
	fallback *CertStore
	entries  atomic.Pointer[[]*poolEntry] // nach Rangfolge sortiert
	loadMu   sync.Mutex                   // ein Load zur Zeit
	now      func() time.Time             // die Uhr; in Tests verstellbar
}

// NewPool legt einen Pool an, der zunächst nur den Rückfall ausliefert.
func NewPool(fallback *CertStore) *Pool {
	return &Pool{fallback: fallback, now: time.Now}
}

// TLSConfig ist die Server-Konfiguration für einen Listener.
func (p *Pool) TLSConfig() *tls.Config {
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			return p.CertificateFor(hello.ServerName), nil
		},
	}
}

// CertificateFor wählt das Zertifikat für einen Namen, genau wie der
// Handshake. Ohne Treffer (und für einen leeren Namen) ist es der Rückfall.
func (p *Pool) CertificateFor(name string) *tls.Certificate {
	cert, _ := p.Lookup(name)
	return cert
}

// Lookup wählt wie CertificateFor und sagt dazu, ob es der Rückfall ist, das
// Zertifikat der eigenen CA, und nicht ein eigenes aus dem Pool.
func (p *Pool) Lookup(name string) (cert *tls.Certificate, fallback bool) {
	if e := p.match(name); e != nil {
		return &e.pair.Certificate, false
	}
	return p.fallback.current.Load(), true
}

// StatusFor beschreibt das Zertifikat, das CertificateFor für den Namen
// wählt.
func (p *Pool) StatusFor(name string) CertStatus {
	if e := p.match(name); e != nil {
		return *e.status(p.now())
	}
	return p.fallbackStatus()
}

// match ist der erste Eintrag nach Rangfolge, der zum Namen passt und noch
// gilt. Ein Zertifikat, das nach dem Laden abgelaufen ist, wird übergangen:
// Ein anderes mit demselben Namen oder der Rückfall ist besser als eines,
// das jeder Client abweist.
func (p *Pool) match(name string) *poolEntry {
	entries := p.entries.Load()
	if name == "" || entries == nil {
		return nil
	}
	now := p.now()
	for _, e := range *entries {
		if e.expired(now) {
			continue
		}
		if e.pair.Leaf.VerifyHostname(name) == nil {
			return e
		}
	}
	return nil
}

// LoadResult ist der Ausgang für ein Zertifikat beim Laden.
type LoadResult struct {
	ID string
	// Err: Das Zertifikat ließ sich nicht laden oder prüfen.
	Err error
	// Kept: trotz Err bleibt der bisherige Stand dieses Zertifikats aktiv.
	Kept bool
	// Cert ist der jetzt aktive Eintrag samt Warnungen; nil, wenn es für die
	// ID keinen gibt.
	Cert *CertStatus
}

// LoadReport hat einen Eintrag je PoolSpec, in deren Reihenfolge.
type LoadReport []LoadResult

// Load lädt alle Zertifikate neu. Gelingt die Prüfung, gilt der neue Stand.
// Scheitert sie, bleibt der bisherige Eintrag derselben ID aktiv, und der
// Fehler steht im Bericht. Einträge, die in specs nicht mehr vorkommen,
// fallen weg.
func (p *Pool) Load(specs []PoolSpec) LoadReport {
	p.loadMu.Lock()
	defer p.loadMu.Unlock()

	previous := map[string]*poolEntry{}
	if old := p.entries.Load(); old != nil {
		for _, e := range *old {
			previous[e.spec.ID] = e
		}
	}

	report := make(LoadReport, 0, len(specs))
	entries := make([]*poolEntry, 0, len(specs))
	for _, spec := range specs {
		now := p.now()
		entry, err := loadEntry(spec, now)
		result := LoadResult{ID: spec.ID, Err: err}
		if err != nil {
			entry = previous[spec.ID]
			result.Kept = entry != nil
		}
		if entry != nil {
			entries = append(entries, entry)
			result.Cert = entry.status(now)
		}
		report = append(report, result)
	}
	slices.SortStableFunc(entries, func(a, b *poolEntry) int {
		return slices.Index(sourceOrder, a.spec.Source) - slices.Index(sourceOrder, b.spec.Source)
	})
	p.entries.Store(&entries)
	return report
}

// loadEntry liest und prüft ein Zertifikat samt Schlüssel.
func loadEntry(spec PoolSpec, now time.Time) (*poolEntry, error) {
	certPEM, err := os.ReadFile(spec.CertPath)
	if err != nil {
		return nil, fmt.Errorf("zertifikat lesen: %w", err)
	}
	keyPEM, err := os.ReadFile(spec.KeyPath)
	if err != nil {
		return nil, fmt.Errorf("schlüssel lesen: %w", err)
	}
	pair, err := ValidatePair(certPEM, keyPEM, now)
	if err != nil {
		return nil, err
	}
	return &poolEntry{
		spec:     spec,
		pair:     pair,
		names:    CertNames(pair.Leaf),
		issuer:   IssuerName(pair.Leaf),
		notAfter: pair.Leaf.NotAfter,
	}, nil
}

// CertStatus beschreibt ein Zertifikat im Pool für CLI und Oberfläche.
type CertStatus struct {
	ID          string    `json:"id"`
	Source      Source    `json:"source"`
	Names       []string  `json:"names"`
	Issuer      string    `json:"issuer"`
	NotAfter    time.Time `json:"not_after"`
	Expired     bool      `json:"expired"`     // NotAfter ist vorbei; der Pool übergeht das Zertifikat
	Fingerprint string    `json:"fingerprint"` // SHA-256 des Zertifikats, hex
	Warnings    []string  `json:"warnings,omitempty"`
}

func (e *poolEntry) expired(now time.Time) bool {
	return !now.Before(e.notAfter)
}

func (e *poolEntry) status(now time.Time) *CertStatus {
	return &CertStatus{
		ID:          e.spec.ID,
		Source:      e.spec.Source,
		Names:       e.names,
		Issuer:      e.issuer,
		NotAfter:    e.notAfter,
		Expired:     e.expired(now),
		Fingerprint: fingerprintOf(e.pair.Leaf.Raw),
		Warnings:    e.pair.Warnings,
	}
}

// Status listet die Zertifikate in der Reihenfolge, in der der Pool sie
// wählt, und zuletzt den Rückfall (Quelle lcm-ca).
func (p *Pool) Status() []CertStatus {
	var list []CertStatus
	if entries := p.entries.Load(); entries != nil {
		now := p.now()
		for _, e := range *entries {
			list = append(list, *e.status(now))
		}
	}
	return append(list, p.fallbackStatus())
}

func (p *Pool) fallbackStatus() CertStatus {
	leaf := p.fallback.current.Load().Leaf
	return CertStatus{
		ID:          string(SourceLCMCA),
		Source:      SourceLCMCA,
		Names:       CertNames(leaf),
		Issuer:      IssuerName(leaf),
		NotAfter:    leaf.NotAfter,
		Expired:     !p.now().Before(leaf.NotAfter),
		Fingerprint: fingerprintOf(leaf.Raw),
	}
}

// CertNames sind die DNS- und IP-Namen, für die das Zertifikat gilt.
func CertNames(leaf *x509.Certificate) []string {
	names := slices.Clone(leaf.DNSNames)
	for _, ip := range leaf.IPAddresses {
		names = append(names, ip.String())
	}
	return names
}

// IssuerName nennt den Aussteller: Name, sonst Organisation.
func IssuerName(leaf *x509.Certificate) string {
	if cn := leaf.Issuer.CommonName; cn != "" {
		return cn
	}
	if len(leaf.Issuer.Organization) > 0 {
		return leaf.Issuer.Organization[0]
	}
	return leaf.Issuer.String()
}
