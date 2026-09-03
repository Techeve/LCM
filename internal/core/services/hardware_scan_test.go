package services

import "testing"

// fakeHardwareRun liefert die Ausgabe des Hardware-Kommandos.
func fakeHardwareRun(out string) func(label, cmd string) string {
	return func(_, _ string) string { return out }
}

// TestScanHardwareRaspberryPi: Auf einem Pi meldet /etc/os-release nur Debian -
// Modell und CPU müssen aus Device-Tree, SoC und lscpu kommen, weil
// /proc/cpuinfo dort keine `model name`-Zeile hat.
func TestScanHardwareRaspberryPi(t *testing.T) {
	got := scanHardware("", fakeHardwareRun(
		"model=Raspberry Pi 4 Model B Rev 1.4\nrevision=c03114\nsoc=BCM2711\ncpu=Cortex-A72\n"))
	if got.Model != "Raspberry Pi 4 Model B Rev 1.4" {
		t.Errorf("modell aus dem device-tree fehlt: %q", got.Model)
	}
	if got.CPU != "BCM2711 (Cortex-A72)" {
		t.Errorf("cpu-bezeichnung unerwartet: %q", got.CPU)
	}
}

// TestScanHardwareOhneDeviceTree: Alte Pi-Kernel stellen keinen lesbaren
// Device-Tree bereit - dann trägt allein der Revisions-Code das Modell.
func TestScanHardwareOhneDeviceTree(t *testing.T) {
	got := scanHardware("", fakeHardwareRun("revision=0013\n"))
	if got.Model != "Raspberry Pi Model B+" {
		t.Errorf("rückfall auf den revisions-code fehlt: %q", got.Model)
	}
	if got.CPU != "BCM2835" {
		t.Errorf("soc aus dem revisions-code fehlt: %q", got.CPU)
	}
}

// TestScanHardwareDMI: Auf x86 kommt das Modell aus der DMI-Tabelle, und die
// bereits gelesene `model name`-Zeile bleibt die CPU-Bezeichnung.
func TestScanHardwareDMI(t *testing.T) {
	tests := []struct {
		name, vendor, product, want string
	}{
		{"Hersteller + Produkt", "Dell Inc.", "PowerEdge R640", "Dell Inc. PowerEdge R640"},
		{"Hersteller doppelt", "ASUSTeK COMPUTER INC.", "ASUSTeK COMPUTER INC. PRIME", "ASUSTeK COMPUTER INC. PRIME"},
		{"nur Werksplatzhalter", "System manufacturer", "To Be Filled By O.E.M.", ""},
		{"Platzhalter beim Hersteller", "Default string", "NUC7i5BNH", "NUC7i5BNH"},
	}
	for _, tt := range tests {
		got := scanHardware("Intel(R) Xeon(R) Silver 4210R", fakeHardwareRun(
			"sys_vendor="+tt.vendor+"\nproduct_name="+tt.product+"\n"))
		if got.Model != tt.want {
			t.Errorf("%s: modell = %q, want %q", tt.name, got.Model, tt.want)
		}
		if got.CPU != "Intel(R) Xeon(R) Silver 4210R" {
			t.Errorf("%s: gelesene cpu überschrieben: %q", tt.name, got.CPU)
		}
	}
}

// TestScanHardwareStumm: Liefert kein Kommando etwas (Container ohne DMI und
// ohne Device-Tree), bleiben beide Felder leer statt halb gefüllt.
func TestScanHardwareStumm(t *testing.T) {
	got := scanHardware("", fakeHardwareRun(""))
	if got.Model != "" || got.CPU != "" {
		t.Errorf("ohne Quellen darf nichts entstehen: %+v", got)
	}
}
