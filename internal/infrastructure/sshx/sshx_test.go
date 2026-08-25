package sshx

import (
	"strings"
	"sync"
	"testing"
)

// TestSyncBufferConcurrentWrites bildet den Bug ab, der die SSH-Outputs
// sporadisch leerte: x/crypto/ssh kopiert Stdout und Stderr in zwei
// parallelen Goroutinen. Mit einem ungeschützten bytes.Buffer gingen
// Schreibvorgänge durch den Data Race verloren; der syncBuffer verhindert
// das. Mit `go test -race` schlägt eine ungeschützte Variante hier fehl.
func TestSyncBufferConcurrentWrites(t *testing.T) {
	var b syncBuffer
	const goroutines = 8
	const writes = 200
	const chunk = "0123456789"

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < writes; i++ {
				if _, err := b.Write([]byte(chunk)); err != nil {
					t.Errorf("write: %v", err)
				}
			}
		}()
	}
	wg.Wait()

	got := b.String()
	want := goroutines * writes * len(chunk)
	if len(got) != want {
		t.Fatalf("verlorene daten durch race: %d bytes, erwartet %d", len(got), want)
	}
	// Die Ziffern müssen unversehrt bleiben (keine korrupten Schreibvorgänge).
	if strings.Count(got, "0123456789") != goroutines*writes {
		t.Error("puffer-inhalt korrumpiert")
	}
}
