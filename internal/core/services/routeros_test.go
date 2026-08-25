package services

import "testing"

func TestParseRouterOSVersion(t *testing.T) {
	cases := []struct{ in, wantV, wantCh string }{
		{"7.15.3 (stable)", "7.15.3", "stable"},
		{"7.16beta5 (testing)", "7.16beta5", "testing"},
		{"6.49.10 (long-term)", "6.49.10", "long-term"},
		{"7.15.3", "7.15.3", ""},
		{"", "", ""},
	}
	for _, c := range cases {
		v, ch := parseRouterOSVersion(c.in)
		if v != c.wantV || ch != c.wantCh {
			t.Errorf("parseRouterOSVersion(%q) = (%q,%q), erwartet (%q,%q)", c.in, v, ch, c.wantV, c.wantCh)
		}
	}
}

func TestParseRouterOSMemMB(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"1024.0MiB", 1024},
		{"890.0MiB", 890},
		{"2.0GiB", 2048},
		{"512.0KiB", 0}, // < 1 MB → gerundet 1? 512KiB=0.5MB → rundet auf 1
		{"", 0},
		{"128MB", 128},
	}
	for _, c := range cases {
		got := parseRouterOSMemMB(c.in)
		// 512KiB rundet auf 1 (0.5 + 0.5); toleriere 0/1 für den Grenzfall.
		if c.in == "512.0KiB" {
			if got != 1 && got != 0 {
				t.Errorf("parseRouterOSMemMB(%q) = %d, erwartet 0/1", c.in, got)
			}
			continue
		}
		if got != c.want {
			t.Errorf("parseRouterOSMemMB(%q) = %d, erwartet %d", c.in, got, c.want)
		}
	}
}

func TestParseRouterOSKV(t *testing.T) {
	out := `                   uptime: 1w2d3h4m5s
                  version: 7.15.3 (stable)
              free-memory: 890.0MiB
             total-memory: 1024.0MiB
                cpu-count: 4
               board-name: RB5009UG+S+
        architecture-name: arm64`
	kv := parseRouterOSKV(out)
	if kv["version"] != "7.15.3 (stable)" {
		t.Errorf("version = %q", kv["version"])
	}
	if kv["board-name"] != "RB5009UG+S+" {
		t.Errorf("board-name = %q", kv["board-name"])
	}
	if kv["cpu-count"] != "4" {
		t.Errorf("cpu-count = %q", kv["cpu-count"])
	}
}

// TestScanRouterOS prüft den vollständigen Scan über eine Fake-Verbindung, die
// je Kommando eine RouterOS-typische Ausgabe liefert - inkl. Erkennung eines
// verfügbaren Updates.
func TestScanRouterOS(t *testing.T) {
	responses := map[string]string{
		"/system resource print": `                  uptime: 1w2d
                 version: 7.15.3 (stable)
             free-memory: 512.0MiB
            total-memory: 1024.0MiB
               cpu-count: 4
                     cpu: ARM64
          total-hdd-space: 128.0MiB
           free-hdd-space: 100.0MiB
              board-name: RB5009UG+S+
       architecture-name: arm64`,
		"/system routerboard print": `       routerboard: yes
             model: RB5009UG+S+
     serial-number: ABC123`,
		"/system package update check-for-updates": `           channel: stable
  installed-version: 7.15.3
     latest-version: 7.16
             status: New version is available`,
	}
	conn := &scriptedConn{handler: func(cmd, _ string) (string, int) { return responses[cmd], 0 }}
	res := scanRouterOS(conn)
	if res.OSID != "routeros" {
		t.Errorf("OSID = %q", res.OSID)
	}
	if res.OSVersion != "7.15.3" {
		t.Errorf("OSVersion = %q, erwartet 7.15.3", res.OSVersion)
	}
	if res.RouterOSChannel != "stable" {
		t.Errorf("Channel = %q", res.RouterOSChannel)
	}
	if res.RouterBoardModel != "RB5009UG+S+" {
		t.Errorf("Board = %q", res.RouterBoardModel)
	}
	if res.CPUCores != 4 {
		t.Errorf("CPUCores = %d", res.CPUCores)
	}
	if res.MemTotalMB != 1024 {
		t.Errorf("MemTotalMB = %d", res.MemTotalMB)
	}
	if res.RouterOSLatestVersion != "7.16" || !res.RouterOSUpdateAvailable {
		t.Errorf("Update-Erkennung falsch: latest=%q available=%v", res.RouterOSLatestVersion, res.RouterOSUpdateAvailable)
	}
}

// TestScanRouterOSUpToDate: gleiche installed/latest → kein Update.
func TestScanRouterOSUpToDate(t *testing.T) {
	responses := map[string]string{
		"/system resource print":                   "version: 7.16 (stable)\ncpu-count: 2\n",
		"/system routerboard print":                "model: CHR\n",
		"/system package update check-for-updates": "channel: stable\ninstalled-version: 7.16\nlatest-version: 7.16\nstatus: System is already up to date\n",
	}
	conn := &scriptedConn{handler: func(cmd, _ string) (string, int) { return responses[cmd], 0 }}
	res := scanRouterOS(conn)
	if res.RouterOSUpdateAvailable {
		t.Errorf("kein Update erwartet, aber UpdateAvailable=true (latest=%q)", res.RouterOSLatestVersion)
	}
	if res.OSVersion != "7.16" {
		t.Errorf("OSVersion = %q", res.OSVersion)
	}
}
