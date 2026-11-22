package acmex

import (
	"context"
	"sync"
)

// ChallengeStore hält die Antworten auf offene HTTP-01-Prüfungen. Es ist der
// Anbieter, den lego zum Auslegen und Aufräumen aufruft; der Listener auf
// Port 80 liest daraus (KeyAuthorization).
type ChallengeStore struct {
	mu     sync.RWMutex
	tokens map[string]string // Token -> Schlüsselautorisierung
}

func NewChallengeStore() *ChallengeStore {
	return &ChallengeStore{tokens: map[string]string{}}
}

// Present legt die Antwort für ein Token aus.
func (s *ChallengeStore) Present(_ context.Context, _, token, keyAuth string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens[token] = keyAuth
	return nil
}

// CleanUp nimmt sie wieder weg.
func (s *ChallengeStore) CleanUp(_ context.Context, _, token, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tokens, token)
	return nil
}

// KeyAuthorization liefert die Antwort für ein Token; ok ist false, wenn
// keine Prüfung dieses Tokens offen ist.
func (s *ChallengeStore) KeyAuthorization(token string) (keyAuth string, ok bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	keyAuth, ok = s.tokens[token]
	return keyAuth, ok
}
