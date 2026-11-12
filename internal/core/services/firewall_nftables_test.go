package services

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestNftIncludeNurEchteZeilen läuft gegen echte Konfigurationsdateien: Ein
// auskommentiertes Include oder eines für eine andere Datei unter
// /etc/nftables.d galt als vorhanden - nach einem Neustart lud dann niemand
// die Regeln von LCM.
func TestNftIncludeNurEchteZeilen(t *testing.T) {
	const appended = "\ninclude \"/etc/nftables.d/*.nft\"\n"
	for name, tc := range map[string]struct {
		conf       string
		wantAppend bool
	}{
		"kommentar":    {"#!/usr/sbin/nft -f\nflush ruleset\n# include \"/etc/nftables.d/*.nft\"\n", true},
		"fremde datei": {"flush ruleset\ninclude \"/etc/nftables.d/docker.nft\"\n", true},
		"ohne include": {"#!/usr/sbin/nft -f\nflush ruleset\ntable inet filter {\n}\n", true},
		// Alpine liefert /etc/nftables.nft mit diesem Include aus.
		"alpine":     {"flush ruleset\ninclude \"/var/lib/nftables/*.nft\"\ninclude \"/etc/nftables.d/*.nft\"\n", false},
		"eingerückt": {"flush ruleset\n\tinclude \"/etc/nftables.d/*.nft\"\n", false},
		"lcm direkt": {"flush ruleset\ninclude \"/etc/nftables.d/lcm.nft\"\n", false},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := filepath.Join(t.TempDir(), "nftables.conf")
			if err := os.WriteFile(cfg, []byte(tc.conf), 0o644); err != nil {
				t.Fatal(err)
			}
			if out, err := exec.Command("sh", "-c", nftEnsureIncludeScript(cfg, cfg+".fehlt")).CombinedOutput(); err != nil {
				t.Fatalf("skript scheitert: %v\n%s", err, out)
			}
			got, _ := os.ReadFile(cfg)
			want := tc.conf
			if tc.wantAppend {
				want += appended
			}
			if string(got) != want {
				t.Errorf("ergebnis:\n%s\nerwartet:\n%s", got, want)
			}
		})
	}
	if !strings.Contains(nftablesApplyBody(), nftEnsureIncludeScript("/etc/nftables.conf", "/etc/nftables.nft")) {
		t.Error("das anwenden prüft das include nicht mehr")
	}
}
