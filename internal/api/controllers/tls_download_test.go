package controllers

import (
	"strings"
	"testing"
)

// Der Dateiname steht in einer Kopfzeile: Anführungszeichen, Zeilenumbrüche,
// Pfadtrenner und Leerraum dürfen nicht durch.
func TestChainFileNameIsSafeInAHeader(t *testing.T) {
	cases := map[string]string{
		"acme-0a1b2c3d":      "lcm-acme-0a1b2c3d-chain.pem",
		"upload-00ff00ff":    "lcm-upload-00ff00ff-chain.pem",
		"config-0":           "lcm-config-0-chain.pem",
		"x\"\r\nSet-Cookie:": "lcm-x_Set-Cookie_-chain.pem",
		"../../etc/passwd":   "lcm-.._.._etc_passwd-chain.pem",
		"ä ö":                "lcm-_-chain.pem",
	}
	for id, want := range cases {
		got := chainFileName(id)
		if got != want {
			t.Errorf("chainFileName(%q) = %q, erwartet %q", id, got, want)
		}
		if strings.ContainsAny(got, "\"\r\n/\\ ") {
			t.Errorf("chainFileName(%q) = %q enthält ein unsicheres Zeichen", id, got)
		}
	}
}
