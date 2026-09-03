package domain

import "testing"

// TestRaspberryFromRevision prüft die Übersetzung echter Revisions-Codes -
// die Rückfallebene für Pi-Systeme ohne lesbaren Device-Tree.
func TestRaspberryFromRevision(t *testing.T) {
	tests := []struct {
		rev, model, soc string
	}{
		{"a03111", "Raspberry Pi 4 Model B", "BCM2711"}, // Pi 4, 1 GB
		{"c03114", "Raspberry Pi 4 Model B", "BCM2711"}, // Pi 4, 4 GB
		{"d04170", "Raspberry Pi 5 Model B", "BCM2712"}, // Pi 5, 8 GB
		{"9000c1", "Raspberry Pi Zero W", "BCM2835"},    // Zero W
		{"a02082", "Raspberry Pi 3 Model B", "BCM2837"}, // Pi 3 B
		{"902120", "Raspberry Pi Zero 2 W", "BCM2837"},  // Zero 2 W
		{"0013", "Raspberry Pi Model B+", "BCM2835"},    // altes Format
		{"1000003", "Raspberry Pi Model B", "BCM2835"},  // altes Format, Warranty-Bit
		{"", "", ""},        // nichts gelesen
		{"keinhex", "", ""}, // Unsinn
		{"c0ff11", "", ""},  // unbekannter Typ 0xff
	}
	for _, tt := range tests {
		model, soc := RaspberryFromRevision(tt.rev)
		if model != tt.model || soc != tt.soc {
			t.Errorf("RaspberryFromRevision(%q) = (%q, %q), want (%q, %q)", tt.rev, model, soc, tt.model, tt.soc)
		}
	}
}

// TestIsSlowHardware: Der Pi zählt unabhängig von seinen Eckdaten als schwach
// (Flaschenhals SD-Karte), ein normaler Server erst bei wenig Kernen UND
// wenig RAM - und Unbekanntes nie.
func TestIsSlowHardware(t *testing.T) {
	tests := []struct {
		name  string
		model string
		cores int
		mem   int64
		want  bool
	}{
		{"Pi 4 mit 8 GB", "Raspberry Pi 4 Model B Rev 1.4", 4, 8192, true},
		{"kleiner Server", "Dell Inc. PowerEdge R640", 2, 2048, true},
		{"wenig RAM, viele Kerne", "", 8, 1024, false},
		{"wenig Kerne, viel RAM", "", 2, 16384, false},
		{"noch nicht erfasst", "", 0, 0, false},
	}
	for _, tt := range tests {
		s := &Server{HardwareModel: tt.model, CPUCores: tt.cores, MemTotalMB: tt.mem}
		if got := s.IsSlowHardware(); got != tt.want {
			t.Errorf("%s: IsSlowHardware() = %v, want %v", tt.name, got, tt.want)
		}
	}
}
