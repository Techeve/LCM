package services

import (
	"testing"

	"LCM/internal/infrastructure/sandbox"
)

// TestSandboxRetrofitOffered: Der Knopf „Sandbox nachrüsten“ hängt nur am
// Zustand des Hosts, nie daran, wer Trivy eingerichtet hat. Ein Trivy aus einer
// früheren Installation (CT 332 im Handtest) bekam ihn nicht, solange die Regel
// nur auf „Sandbox aus“ schaute und Landlock die Sperre trug.
func TestSandboxRetrofitOffered(t *testing.T) {
	cases := []struct {
		name   string
		trivy  bool
		status sandbox.Status
		want   bool
	}{
		{"ohne Trivy gibt es nichts nachzurüsten", false, sandbox.Status{}, false},
		{"keine Sperre", true, sandbox.Status{Reason: "kein Backend"}, true},
		{"nur Landlock, bubblewrap fehlt", true, sandbox.Status{Active: true, Backend: "landlock", BubblewrapMissing: true}, true},
		{"Landlock trägt, bubblewrap ist da und startet nur nicht", true, sandbox.Status{Active: true, Backend: "landlock"}, false},
		{"bubblewrap trägt", true, sandbox.Status{Active: true, Backend: "bubblewrap"}, false},
		{"bubblewrap kaputt und kein Landlock", true, sandbox.Status{Reason: "bubblewrap startet nicht"}, true},
	}
	for _, c := range cases {
		if got := sandboxRetrofitOffered(c.trivy, c.status); got != c.want {
			t.Errorf("%s: %v, erwartet %v", c.name, got, c.want)
		}
	}
}
