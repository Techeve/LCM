package controllers

import (
	"strconv"
	"strings"
	"sync"
	"time"
)

// retryAfterSeconds formatiert eine Restdauer für den Retry-After-Header
// (mindestens 1 Sekunde).
func retryAfterSeconds(d time.Duration) string {
	secs := int(d.Seconds())
	if secs < 1 {
		secs = 1
	}
	return strconv.Itoa(secs)
}

// loginGuard sperrt einen Anmelde-Schlüssel (Benutzername bzw. User-ID)
// nach zu vielen Fehlversuchen temporär - Schutz gegen Passwort- und
// TOTP-Brute-Force, das über viele IPs verteilt wird (und daher an der
// IP-Rate-Limit-Middleware vorbeiliefe). Rein in-memory; bei Neustart leer.
type loginGuard struct {
	mu    sync.Mutex
	state map[string]*attemptState
	now   func() time.Time // in Tests überschreibbar
}

type attemptState struct {
	fails int
	until time.Time // Sperre gilt bis zu diesem Zeitpunkt
	last  time.Time // Zeitpunkt des letzten Fehlversuchs (für den Zerfall)
}

const (
	// Bis maxLoginFails aufeinanderfolgende Fehlversuche sind frei; danach
	// greift eine wachsende Sperre.
	maxLoginFails    = 5
	loginLockoutBase = time.Minute
	loginLockoutMax  = 15 * time.Minute
	// loginDecayWindow: liegt der letzte Fehlversuch länger zurück, beginnt
	// die Zählung von vorn - vereinzelte Vertipper akkumulieren nicht zu einer
	// Sperre.
	loginDecayWindow = 15 * time.Minute
	// maxAccountFails ist die Schwelle der KONTObezogenen Sperre. Bewusst
	// höher als die IP-Schwelle: Sie soll Password-Spraying über viele IPs
	// stoppen, ohne dass ein Angreifer ein fremdes Konto zu billig aussperren
	// kann (Account-Lockout-DoS). Die Sperre ist zudem auf loginLockoutMax
	// gedeckelt und zerfällt.
	maxAccountFails = 15
)

func newLoginGuard() *loginGuard {
	return &loginGuard{state: map[string]*attemptState{}, now: time.Now}
}

func guardKey(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// totpGuardKey ist der kontobezogene Sperrschlüssel für TOTP-Codeprüfungen.
// Bewusst EIN gemeinsamer Schlüssel für alle Stellen, die einen Code prüfen
// (Login-2. Faktor, 2FA-Deaktivierung, Passwortwechsel) - sonst könnte ein
// Angreifer die Sperre umgehen, indem er zwischen den Endpunkten wechselt.
func totpGuardKey(userID uint) string {
	return "totp:" + strconv.FormatUint(uint64(userID), 10)
}

// blocked meldet, ob der Schlüssel aktuell gesperrt ist, und die Restdauer.
func (g *loginGuard) blocked(key string) (bool, time.Duration) {
	key = guardKey(key)
	g.mu.Lock()
	defer g.mu.Unlock()
	st := g.state[key]
	if st == nil {
		return false, 0
	}
	if rem := st.until.Sub(g.now()); rem > 0 {
		return true, rem
	}
	return false, 0
}

// beginAttempt prüft die Sperre UND verbucht den Versuch in EINER Operation
// unter demselben Lock. Nötig gegen ein Zeitfenster-Race: zwischen einem
// getrennten blocked() und fail() liegt der argon2id-Vergleich (~50-100 ms).
// Hunderte parallele Requests kamen dadurch alle an der Prüfung vorbei, bevor
// der erste Fehlversuch verbucht war - die Schwelle ließ sich um Größenordnungen
// überschreiten. Der Aufrufer ruft bei ERFOLG reset(key) auf; bei Misserfolg
// ist der Versuch bereits gezählt.
func (g *loginGuard) beginAttempt(key string, threshold int) (bool, time.Duration) {
	key = guardKey(key)
	g.mu.Lock()
	defer g.mu.Unlock()
	if st := g.state[key]; st != nil {
		if rem := st.until.Sub(g.now()); rem > 0 {
			return true, rem
		}
	}
	g.recordFailLocked(key, threshold)
	return false, 0
}

// fail verbucht einen Fehlversuch und verhängt ab maxLoginFails eine
// exponentiell wachsende Sperre (base * 2^n, gedeckelt auf loginLockoutMax).
func (g *loginGuard) fail(key string) { g.failWith(key, maxLoginFails) }

// failWith verbucht einen Fehlversuch mit eigener Schwelle (siehe
// maxAccountFails für die kontobezogene Sperre).
func (g *loginGuard) failWith(key string, threshold int) {
	key = guardKey(key)
	g.mu.Lock()
	defer g.mu.Unlock()
	g.recordFailLocked(key, threshold)
}

// recordFailLocked ist der gemeinsame Kern; der Aufrufer hält g.mu.
func (g *loginGuard) recordFailLocked(key string, threshold int) {
	st := g.state[key]
	if st == nil {
		st = &attemptState{}
		g.state[key] = st
	}
	// Zerfall: lag der letzte Fehlversuch lange zurück, von vorn beginnen.
	if !st.last.IsZero() && g.now().Sub(st.last) > loginDecayWindow {
		st.fails = 0
	}
	st.last = g.now()
	st.fails++
	if st.fails >= threshold {
		lockout := loginLockoutBase << (st.fails - threshold)
		if lockout > loginLockoutMax || lockout <= 0 {
			lockout = loginLockoutMax
		}
		st.until = g.now().Add(lockout)
	}
	// Gelegentliche Bereinigung, damit die Map nicht unbegrenzt wächst.
	if len(g.state) > 10_000 {
		for k, v := range g.state {
			if v.until.Before(g.now()) && v.fails < threshold {
				delete(g.state, k)
			}
		}
	}
}

// reset löscht den Zähler nach einer erfolgreichen Anmeldung.
func (g *loginGuard) reset(key string) {
	key = guardKey(key)
	g.mu.Lock()
	delete(g.state, key)
	g.mu.Unlock()
}
