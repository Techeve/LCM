package services

import (
	"testing"
	"time"
)

// TestSpreadOffset: Die Server verteilen sich gleichmäßig, der erste beginnt
// sofort, keiner erst am Fensterende.
func TestSpreadOffset(t *testing.T) {
	window := 60 * time.Minute
	want := []time.Duration{0, 15 * time.Minute, 30 * time.Minute, 45 * time.Minute}
	for i, w := range want {
		if got := spreadOffset(i, len(want), window); got != w {
			t.Errorf("server %d: versatz %v, erwartet %v", i, got, w)
		}
	}
}
