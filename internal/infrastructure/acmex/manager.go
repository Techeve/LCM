package acmex

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"LCM/internal/infrastructure/tlsx"
)

// Action sagt, was ein Lauf mit einem Zertifikat getan hat.
type Action string

const (
	ActionIssued  Action = "issued"  // neu ausgestellt
	ActionRenewed Action = "renewed" // durch ein Nachfolge-Zertifikat ersetzt
	ActionKept    Action = "kept"    // noch nicht fällig
)

// Outcome ist der Ausgang für ein Zertifikat.
type Outcome struct {
	ID     string
	Names  []string
	Action Action
	Err    error
}

// Changed: Das Zertifikat auf der Platte ist neu.
func (o Outcome) Changed() bool {
	return o.Err == nil && o.Action != ActionKept
}

// Failures fasst die Fehler eines Laufs zusammen; nil, wenn alles gelang.
func Failures(outcomes []Outcome) error {
	var errs []error
	for _, o := range outcomes {
		if o.Err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", strings.Join(o.Names, ", "), o.Err))
		}
	}
	return errors.Join(errs...)
}

// Manager hält die ausgestellten Zertifikate auf dem Stand, den die Config
// verlangt: stellt fehlende aus, erneuert fällige und entfernt, was nicht
// mehr gebraucht wird. Den Takt gibt der Aufrufer vor (täglich), und er
// sorgt dafür, dass nie zwei Läufe gleichzeitig laufen.
type Manager struct {
	dataDir string
	now     func() time.Time // die Uhr; in Tests verstellbar
	connect func(ctx context.Context, cfg Config) (authority, error)

	mu      sync.Mutex
	renewAt map[string]time.Time // zuletzt ermittelter Erneuerungszeitpunkt je Zertifikat
}

func NewManager(dataDir string) *Manager {
	return &Manager{
		dataDir: dataDir,
		now:     time.Now,
		connect: func(ctx context.Context, cfg Config) (authority, error) {
			return connect(ctx, dataDir, cfg)
		},
		renewAt: map[string]time.Time{},
	}
}

// target ist ein Zertifikat, das der Lauf haben will.
type target struct {
	id    string
	names []string
}

// plan teilt die Namen auf: ein Zertifikat mit allen oder je Name eines.
func plan(cfg Config) []target {
	if !cfg.PerName {
		return []target{{id: CertID(cfg.DirectoryURL, cfg.Names), names: cfg.Names}}
	}
	targets := make([]target, 0, len(cfg.Names))
	for _, name := range cfg.Names {
		targets = append(targets, target{id: CertID(cfg.DirectoryURL, []string{name}), names: []string{name}})
	}
	return targets
}

// Reconcile bringt die Zertifikate auf den Stand der Config. Ein Fehler bei
// einem Zertifikat hält die anderen nicht auf; er steht im Ausgang. Was nicht
// mehr gebraucht wird, entfernt der Lauf nur, wenn alle gewünschten
// Zertifikate da sind: Bis dahin liefert das alte weiter.
func (m *Manager) Reconcile(ctx context.Context, cfg Config) ([]Outcome, error) {
	ca := &lazyAuthority{connect: func() (authority, error) { return m.connect(ctx, cfg) }}
	targets := plan(cfg)
	outcomes := make([]Outcome, 0, len(targets))
	keep := map[string]bool{}
	for _, t := range targets {
		action, err := m.ensure(ctx, t, ca)
		outcomes = append(outcomes, Outcome{ID: t.id, Names: t.names, Action: action, Err: err})
		keep[t.id] = true
	}
	if Failures(outcomes) != nil {
		return outcomes, nil
	}
	m.forgetExcept(keep)
	return outcomes, removeCerts(m.dataDir, keep)
}

// Covers meldet, ob ein gültiges Zertifikat dieser CA alle Namen trägt. Es
// darf mehr Namen tragen: Wer Agent oder MCP abschaltet, braucht kein neues
// Zertifikat, das bisherige deckt die übrigen Namen weiter ab. Ob es bald zu
// erneuern ist, sagt es nicht (das tut der tägliche Lauf).
func (m *Manager) Covers(directoryURL string, names []string) bool {
	ids, err := certIDs(m.dataDir)
	if err != nil {
		return false
	}
	for _, id := range ids {
		if m.coversWith(id, directoryURL, names) {
			return true
		}
	}
	return false
}

// coversWith prüft ein abgelegtes Zertifikat. Die Kennung entsteht aus CA und
// Namen (CertID); passt sie zu den Namen im Zertifikat und dieser CA, stammt
// es von dieser CA.
func (m *Manager) coversWith(id, directoryURL string, names []string) bool {
	_, leaf, err := readBundle(m.dataDir, id)
	if err != nil || !m.now().Before(leaf.NotAfter) {
		return false
	}
	have := tlsx.CertNames(leaf)
	if CertID(directoryURL, have) != id {
		return false
	}
	for _, name := range names {
		if !slices.Contains(have, name) {
			return false
		}
	}
	return true
}

// RemoveAll löscht alle ausgestellten Zertifikate (ACME wurde abgeschaltet).
func (m *Manager) RemoveAll() error {
	m.forgetExcept(nil)
	return removeCerts(m.dataDir, nil)
}

// ensure stellt ein Zertifikat aus oder erneuert es, wenn es fällig ist.
func (m *Manager) ensure(ctx context.Context, t target, ca *lazyAuthority) (Action, error) {
	old, leaf, err := readBundle(m.dataDir, t.id)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return m.obtain(ctx, t, ca)
	case err != nil:
		slog.Warn("acme certificate not readable - issuing a new one", "id", t.id, "error", err)
		return m.obtain(ctx, t, ca)
	case !m.now().Before(leaf.NotAfter):
		return m.obtain(ctx, t, ca)
	}
	at := m.renewalAt(ctx, leaf, ca)
	m.setRenewAt(t.id, at)
	if m.now().Before(at) {
		return ActionKept, nil
	}
	auth, err := ca.get()
	if err != nil {
		return ActionRenewed, err
	}
	fresh, err := auth.Renew(ctx, t.names, old)
	return m.store(t, fresh, err, ActionRenewed)
}

func (m *Manager) obtain(ctx context.Context, t target, ca *lazyAuthority) (Action, error) {
	auth, err := ca.get()
	if err != nil {
		return ActionIssued, err
	}
	fresh, err := auth.Obtain(ctx, t.names)
	return m.store(t, fresh, err, ActionIssued)
}

// store prüft das Ergebnis und legt es ab. Was der Pool nicht laden würde,
// kommt nicht auf die Platte.
func (m *Manager) store(t target, fresh bundle, err error, action Action) (Action, error) {
	if err != nil {
		return action, err
	}
	pair, err := tlsx.ValidatePair(fresh.certPEM, fresh.keyPEM, m.now())
	if err != nil {
		return action, fmt.Errorf("die ca lieferte ein unbrauchbares zertifikat: %w", err)
	}
	if err := writeBundle(m.dataDir, t.id, fresh); err != nil {
		return action, err
	}
	m.setRenewAt(t.id, renewalTime(pair.Leaf, nil))
	return action, nil
}

// renewalAt fragt die CA nach ihrem Fenster. Gibt sie keines, gilt die Regel
// ohne ARI; auch eine CA, die nicht antwortet, hält ein gültiges Zertifikat
// nicht auf.
func (m *Manager) renewalAt(ctx context.Context, leaf *x509.Certificate, ca *lazyAuthority) time.Time {
	auth, err := ca.get()
	if err != nil {
		slog.Warn("acme ca not reachable - renewal falls back to the lifetime rule", "error", err)
		return renewalTime(leaf, nil)
	}
	win, err := auth.RenewalWindow(ctx, leaf)
	if err != nil {
		slog.Warn("acme renewal info not available - renewal falls back to the lifetime rule", "error", err)
		return renewalTime(leaf, nil)
	}
	return renewalTime(leaf, win)
}

func (m *Manager) setRenewAt(id string, at time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.renewAt[id] = at
}

// forgetExcept vergisst die Erneuerungszeitpunkte aller anderen Zertifikate.
func (m *Manager) forgetExcept(keep map[string]bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id := range m.renewAt {
		if !keep[id] {
			delete(m.renewAt, id)
		}
	}
}

// Certificates beschreibt die ausgestellten Zertifikate.
func (m *Manager) Certificates() ([]CertInfo, error) {
	ids, err := certIDs(m.dataDir)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	certs := make([]CertInfo, 0, len(ids))
	for _, id := range ids {
		_, leaf, err := readBundle(m.dataDir, id)
		if err != nil {
			slog.Warn("acme certificate not readable", "id", id, "error", err)
			continue
		}
		renewAt, known := m.renewAt[id]
		if !known {
			renewAt = renewalTime(leaf, nil)
		}
		certs = append(certs, certInfo(id, leaf, renewAt))
	}
	slices.SortFunc(certs, func(a, b CertInfo) int { return strings.Compare(a.ID, b.ID) })
	return certs, nil
}

// lazyAuthority verbindet sich erst, wenn ein Zertifikat die CA braucht: Ist
// nichts fällig und das Zertifikat noch gültig, reicht eine Frage nach ARI,
// und auch die darf scheitern.
type lazyAuthority struct {
	connect func() (authority, error)
	done    bool
	auth    authority
	err     error
}

func (l *lazyAuthority) get() (authority, error) {
	if !l.done {
		l.auth, l.err = l.connect()
		l.done = true
	}
	return l.auth, l.err
}
