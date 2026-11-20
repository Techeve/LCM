package middlewares

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"sync"
	"time"

	"github.com/gofiber/fiber/v3"

	"LCM/internal/api/apierr"
)

// maxAPIKeyBuckets begrenzt die Zählertabelle hart. Ist sie voll und hat
// kein Fenster ausgedient, teilen sich alle weiteren Schlüssel EINEN Zähler
// (overflowBucket). Zufällige Schlüssel füllen so nicht den Speicher, und
// das Limit lässt sich auch nicht umgehen - der Preis: Neue Schlüssel
// bekommen während einer solchen Flut gemeinsam das Limit eines einzigen.
// Schlüssel mit laufendem Fenster zählen unverändert weiter.
const maxAPIKeyBuckets = 10_000

// overflowBucket ist der gemeinsame Zähler bei voller Tabelle. Kein Hash
// (16 Hex-Zeichen) sieht so aus.
const overflowBucket = "overflow"

// apiKeySweepInterval: so oft durchsucht eine volle Tabelle höchstens ihre
// Einträge nach abgelaufenen Fenstern.
const apiKeySweepInterval = time.Second

// APIKeyRateLimit begrenzt Requests pro API-Key auf maxPerMinute
// (Fixed-Window pro Minute). Konfiguriert über config.json
// (api_key_rate_limit_per_minute); 0 deaktiviert das Limit.
//
// Die Middleware greift VOR der Key-Validierung und zählt auch ungültige
// Keys - das drosselt gleichzeitig Brute-Force-Versuche. Browser-Sitzungen
// sind nicht betroffen. Bei Überschreitung: 429 + Retry-After.
//
// Die Zähler liegen in-memory; bei einem Neustart beginnen sie bei null.
// Für Multi-Instanz-Deployments hinter einem Load-Balancer gehört das
// Limit stattdessen in den Proxy oder einen geteilten Store (z.B. Redis).
func APIKeyRateLimit(maxPerMinute int) fiber.Handler {
	if maxPerMinute <= 0 {
		return func(c fiber.Ctx) error { return c.Next() }
	}
	limiter := newAPIKeyLimiter(maxAPIKeyBuckets)

	return func(c fiber.Ctx) error {
		apiKey := c.Get("X-API-Key")
		if apiKey == "" {
			return c.Next()
		}
		// Nicht den Klartext-Key als Map-Schlüssel im Speicher halten.
		sum := sha256.Sum256([]byte(apiKey))
		count, retryAfter := limiter.hit(hex.EncodeToString(sum[:8]), time.Now())
		if count > maxPerMinute {
			c.Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())+1))
			return apierr.TooManyRequests
		}
		return c.Next()
	}
}

// apiKeyLimiter zählt Anfragen je Schlüssel in Ein-Minuten-Fenstern.
type apiKeyLimiter struct {
	capacity int

	mu        sync.Mutex
	buckets   map[string]*rateWindow
	lastSweep time.Time
}

type rateWindow struct {
	start time.Time
	count int
}

func newAPIKeyLimiter(capacity int) *apiKeyLimiter {
	return &apiKeyLimiter{capacity: capacity, buckets: map[string]*rateWindow{}}
}

// hit zählt eine Anfrage und liefert den Stand im laufenden Fenster samt
// dessen Restdauer.
func (l *apiKeyLimiter) hit(id string, now time.Time) (int, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	w := l.window(id, now)
	w.count++
	return w.count, time.Minute - now.Sub(w.start)
}

// window liefert das laufende Fenster des Schlüssels und legt bei Bedarf ein
// neues an. Ein unbekannter Schlüssel bekommt bei voller Tabelle den
// gemeinsamen Zähler; der Aufrufer hält l.mu.
func (l *apiKeyLimiter) window(id string, now time.Time) *rateWindow {
	if _, known := l.buckets[id]; !known && len(l.buckets) >= l.capacity {
		l.sweep(now)
		if len(l.buckets) >= l.capacity {
			id = overflowBucket
		}
	}
	w := l.buckets[id]
	if w == nil || now.Sub(w.start) >= time.Minute {
		w = &rateWindow{start: now}
		l.buckets[id] = w
	}
	return w
}

// sweep entfernt abgelaufene Fenster, höchstens einmal je
// apiKeySweepInterval - eine volle Tabelle ohne Abgelaufene soll nicht bei
// jeder Anfrage ganz durchsucht werden.
func (l *apiKeyLimiter) sweep(now time.Time) {
	if now.Sub(l.lastSweep) < apiKeySweepInterval {
		return
	}
	l.lastSweep = now
	for id, w := range l.buckets {
		if now.Sub(w.start) >= time.Minute {
			delete(l.buckets, id)
		}
	}
}
