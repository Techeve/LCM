package middlewares

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"sync"
	"time"

	"github.com/gofiber/fiber/v3"
)

// APIKeyRateLimit begrenzt Requests pro API-Key auf maxPerMinute
// (Fixed-Window pro Minute). Konfiguriert über config.json
// (api_key_rate_limit_per_minute); 0 deaktiviert das Limit.
//
// Die Middleware greift VOR der Key-Validierung und zählt auch ungültige
// Keys - das drosselt gleichzeitig Brute-Force-Versuche. Browser-Sessions
// (JWT) sind nicht betroffen. Bei Überschreitung: 429 + Retry-After.
//
// Die Zähler liegen in-memory; bei einem Neustart beginnen sie bei null.
// Für Multi-Instanz-Deployments hinter einem Load-Balancer gehört das
// Limit stattdessen in den Proxy oder einen geteilten Store (z.B. Redis).
func APIKeyRateLimit(maxPerMinute int) fiber.Handler {
	if maxPerMinute <= 0 {
		return func(c fiber.Ctx) error { return c.Next() }
	}

	type window struct {
		start time.Time
		count int
	}
	var (
		mu      sync.Mutex
		buckets = map[string]*window{}
	)

	return func(c fiber.Ctx) error {
		apiKey := c.Get("X-API-Key")
		if apiKey == "" {
			return c.Next()
		}

		// Nicht den Klartext-Key als Map-Schlüssel im Speicher halten.
		sum := sha256.Sum256([]byte(apiKey))
		id := hex.EncodeToString(sum[:8])

		now := time.Now()
		mu.Lock()
		w := buckets[id]
		if w == nil || now.Sub(w.start) >= time.Minute {
			// Gelegentlich abgelaufene Fenster aufräumen, damit die Map
			// bei vielen unterschiedlichen Keys nicht unbegrenzt wächst.
			if len(buckets) > 10_000 {
				for k, v := range buckets {
					if now.Sub(v.start) >= time.Minute {
						delete(buckets, k)
					}
				}
			}
			buckets[id] = &window{start: now, count: 1}
			mu.Unlock()
			return c.Next()
		}
		w.count++
		count := w.count
		retryAfter := time.Minute - now.Sub(w.start)
		mu.Unlock()

		if count > maxPerMinute {
			c.Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())+1))
			return fiber.NewError(fiber.StatusTooManyRequests,
				"Rate-Limit überschritten - bitte später erneut versuchen")
		}
		return c.Next()
	}
}
