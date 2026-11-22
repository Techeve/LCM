package main

import (
	"log/slog"
	"sync"
	"time"

	"github.com/gofiber/fiber/v3"
)

// shutdownTimeout ist die Frist, die laufende Anfragen beim Beenden bekommen.
// Länger darf ein Stopp nicht dauern: Der Dienst wartet sonst bis zum
// TimeoutStopSec von systemd und wird dann hart beendet.
const shutdownTimeout = 10 * time.Second

// portCloser sammelt, was beim Beenden zuerst schließt, damit die Ports frei
// werden. Die Listener entstehen erst nach dem Signalhandler, darum trägt der
// Start sie nach.
type portCloser struct {
	mu      sync.Mutex
	closers []func()
}

func (p *portCloser) Add(closeFn func()) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closers = append(p.closers, closeFn)
}

func (p *portCloser) CloseAll() {
	p.mu.Lock()
	closers := p.closers
	p.mu.Unlock()
	for _, closeFn := range closers {
		closeFn()
	}
}

// shutdownApps fährt die Apps gleichzeitig herunter, jede mit der Frist timeout.
// Eine nil-App (den Dienst gibt es nicht) wird übersprungen.
func shutdownApps(timeout time.Duration, apps ...*fiber.App) {
	var wg sync.WaitGroup
	for _, app := range apps {
		if app == nil {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := app.ShutdownWithTimeout(timeout); err != nil {
				slog.Warn("shutdown did not finish in time - open connections are dropped", "error", err)
			}
		}()
	}
	wg.Wait()
}
