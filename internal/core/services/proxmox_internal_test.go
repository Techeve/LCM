package services

import "testing"

// fakeRun simuliert die dpkg-query-Ausgabe der Proxmox-Erkennung.
func fakeRun(out string) func(label, cmd string) string {
	return func(_, _ string) string { return out }
}

func TestDetectProxmox(t *testing.T) {
	cases := []struct {
		name        string
		out         string
		wantType    string
		wantVersion string
	}{
		{"kein proxmox (leer)", "", "", ""},
		{"pve", "pve-manager 8.2.4-1\n", "pve", "8.2.4"},
		{"pbs", "proxmox-backup-server 3.2.7-1\n", "pbs", "3.2.7"},
		{"pmg", "pmg-api 8.1.2\n", "pmg", "8.1.2"},
		{"epoch wird entfernt", "pve-manager 1:8.2.4-1\n", "pve", "8.2.4"},
		{"pve gewinnt bei mehrfach-treffern", "pve-manager 8.2.4-1\nproxmox-backup-server 3.2.7-1\n", "pve", "8.2.4"},
		{"fremde zeilen ignoriert", "irgendwas anderes\n", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			typ, version := detectProxmox(fakeRun(tc.out))
			if typ != tc.wantType || version != tc.wantVersion {
				t.Fatalf("erwartet (%q, %q), bekam (%q, %q)", tc.wantType, tc.wantVersion, typ, version)
			}
		})
	}
}

func TestProxmoxDisplayVersion(t *testing.T) {
	for in, want := range map[string]string{
		"8.2.4-1":   "8.2.4",
		"1:8.2.4-1": "8.2.4",
		"8.2.4":     "8.2.4",
		" 3.2.7-2 ": "3.2.7",
	} {
		if got := proxmoxDisplayVersion(in); got != want {
			t.Errorf("%q: erwartet %q, bekam %q", in, want, got)
		}
	}
}
