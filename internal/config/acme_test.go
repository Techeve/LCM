package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"LCM/internal/config"
)

// acme_http_listen: Standard :80, sonst host:port oder :port.
func TestACMEHTTPListen(t *testing.T) {
	cases := map[string]bool{
		`{}`:                                  true,
		`{"acme_http_listen":":8080"}`:        true,
		`{"acme_http_listen":"0.0.0.0:80"}`:   true,
		`{"acme_http_listen":"[::]:80"}`:      true,
		`{"acme_http_listen":""}`:             false,
		`{"acme_http_listen":"80"}`:           false,
		`{"acme_http_listen":":0"}`:           false,
		`{"acme_http_listen":":70000"}`:       false,
		`{"acme_http_listen":":http"}`:        false,
		`{"acme_http_listen":"lcm.example:"}`: false,
	}
	for body, ok := range cases {
		path := filepath.Join(t.TempDir(), "config.json")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg, err := config.LoadFrom(path)
		if (err == nil) != ok {
			t.Errorf("%s: err=%v, erwartet ok=%v", body, err, ok)
			continue
		}
		if err != nil && !strings.Contains(err.Error(), "acme_http_listen") {
			t.Errorf("%s: Fehler nennt den Schlüssel nicht: %v", body, err)
		}
		if body == `{}` && cfg.ACMEHTTPListen != ":80" {
			t.Errorf("Standard = %q, erwartet :80", cfg.ACMEHTTPListen)
		}
	}
}

// acme_dns_exec: leer (Standard) oder ein absoluter Pfad. Ob die Datei da
// ist, prüft LCM erst beim Auswählen des Anbieters.
func TestACMEDNSExec(t *testing.T) {
	cases := map[string]bool{
		`{}`: true,
		`{"acme_dns_exec":"/usr/local/bin/lcm-dns-hook"}`: true,
		`{"acme_dns_exec":"/gibt/es/nicht"}`:              true,
		`{"acme_dns_exec":"hook.sh"}`:                     false,
		`{"acme_dns_exec":"./hook.sh"}`:                   false,
	}
	for body, ok := range cases {
		path := filepath.Join(t.TempDir(), "config.json")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := config.LoadFrom(path)
		if (err == nil) != ok {
			t.Errorf("%s: err=%v, erwartet ok=%v", body, err, ok)
		}
		if err != nil && !strings.Contains(err.Error(), "acme_dns_exec") {
			t.Errorf("%s: Fehler nennt den Schlüssel nicht: %v", body, err)
		}
	}
}
