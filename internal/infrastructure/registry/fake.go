package registry

import (
	"context"
	"sync"

	"LCM/internal/core/domain"
)

// Fake ist eine In-Memory-Checker-Implementierung für Tests: Ergebnisse
// pro Image-Referenz, plus Aufruf-Zähler für Dedup-Assertions.
type Fake struct {
	Results map[string]Result

	mu    sync.Mutex
	calls map[string]int
}

// CheckDigest liefert das programmierte Ergebnis (unbekannte Refs → error).
func (f *Fake) CheckDigest(_ context.Context, ref string) Result {
	f.mu.Lock()
	if f.calls == nil {
		f.calls = map[string]int{}
	}
	f.calls[ref]++
	f.mu.Unlock()
	if r, ok := f.Results[ref]; ok {
		return r
	}
	return Result{Status: domain.DockerCheckError, Err: "fake: keine antwort programmiert für " + ref}
}

// Calls liefert, wie oft eine Referenz geprüft wurde (Dedup-Assertions).
func (f *Fake) Calls(ref string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[ref]
}

var _ Checker = (*Fake)(nil)
