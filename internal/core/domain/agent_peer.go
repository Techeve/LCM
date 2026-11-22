package domain

import "net/netip"

// AgentPeer ist ein verbundener Agent mit der Adresse, von der er kommt -
// hinter einem vertrauten Proxy die weitergeleitete Client-Adresse.
type AgentPeer struct {
	AgentID string     `json:"id"`
	Name    string     `json:"name"`
	Addr    netip.Addr `json:"address"` // ungültig, wenn die Adresse nicht lesbar war
}
