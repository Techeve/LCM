package services

import (
	"sort"
	"sync"
	"time"
)

// OpenConsole ist eine gerade offene Konsolen-Sitzung - damit jeder sieht,
// dass auf einem Server jemand von Hand arbeitet.
type OpenConsole struct {
	ServerID uint      `json:"server_id"`
	Actor    string    `json:"actor"`
	Since    time.Time `json:"since"`
}

// consoleRegistry führt die offenen Konsolen. Nur im Speicher: Nach einem
// Neustart von LCM ist ohnehin keine mehr offen.
type consoleRegistry struct {
	mu     sync.Mutex
	nextID uint64
	open   map[uint64]OpenConsole
}

func newConsoleRegistry() *consoleRegistry {
	return &consoleRegistry{open: map[uint64]OpenConsole{}}
}

// add trägt eine Sitzung ein und liefert die Funktion, die sie wieder
// austrägt (idempotent).
func (r *consoleRegistry) add(serverID uint, actor string) func() {
	r.mu.Lock()
	r.nextID++
	id := r.nextID
	r.open[id] = OpenConsole{ServerID: serverID, Actor: actor, Since: time.Now()}
	r.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			r.mu.Lock()
			delete(r.open, id)
			r.mu.Unlock()
		})
	}
}

// list liefert die offenen Sitzungen, die älteste zuerst.
func (r *consoleRegistry) list() []OpenConsole {
	r.mu.Lock()
	out := make([]OpenConsole, 0, len(r.open))
	for _, c := range r.open {
		out = append(out, c)
	}
	r.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Since.Before(out[j].Since) })
	return out
}
