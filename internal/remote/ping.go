package remote

import (
	"encoding/json"
	"time"

	"LCM/internal/core/agentcaps"
	"LCM/internal/remote/wire"
)

// Lebendprüfung der Agenten (siehe Kopf von wire): LCM pingt jeden Agent, der
// wire.CapabilityPing gemeldet hat, im Takt von pingInterval auf dem
// Kommando-Topic. Der Agent antwortet mit einem Pong und baut seine Verbindung
// selbst neu auf, wenn die Pings ausbleiben. Ältere Agenten bekommen nie ein
// Ping, sie kennen die Nachricht nicht.

// startPinging merkt einen Agent als pingbar vor, wenn sein Inventar es
// anzeigt, und schickt gleich das erste Ping - so erfährt er sofort, dass LCM
// pingt, und das erste Pong kommt ohne Wartezeit.
func (h *Hub) startPinging(agentID string, agent agentcaps.Agent) {
	if !agent.Supports(agentcaps.FeatureLiveness) {
		return
	}
	h.mu.Lock()
	_, online := h.online[agentID]
	if online {
		h.pingable[agentID] = struct{}{}
	}
	h.mu.Unlock()
	if online {
		h.ping(agentID)
	}
}

// pingLoop pingt alle pingbaren Agenten, bis der Hub schließt.
func (h *Hub) pingLoop() {
	ticker := time.NewTicker(h.pingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-h.done:
			return
		case <-ticker.C:
			h.pingAll()
		}
	}
}

func (h *Hub) pingAll() {
	h.mu.Lock()
	ids := make([]string, 0, len(h.pingable))
	for id := range h.pingable {
		ids = append(ids, id)
	}
	h.mu.Unlock()
	for _, id := range ids {
		h.ping(id)
	}
}

// ping schickt ein Ping. QoS 0: Ein verspätetes Ping wäre wertlos und füllte
// nur die Warteschlange einer toten Verbindung.
func (h *Hub) ping(agentID string) {
	payload, err := json.Marshal(wire.Command{Ping: true})
	if err != nil {
		return
	}
	if err := h.broker.Publish(wire.TopicCmd(agentID), payload, false, 0); err != nil {
		h.log.Debug("ping could not be sent", "agent_id", agentID, "error", err)
	}
}

func (h *Hub) recordPong(agentID string) {
	h.mu.Lock()
	h.pongs[agentID] = time.Now()
	h.mu.Unlock()
}

// LastPong liefert, wann der Agent zuletzt auf ein Ping geantwortet hat. false,
// wenn noch keine Antwort kam (älterer Agent oder noch nicht lange genug da).
func (h *Hub) LastPong(agentID string) (time.Time, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	at, ok := h.pongs[agentID]
	return at, ok
}
