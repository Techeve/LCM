package wire_test

import (
	"encoding/base64"
	"strings"
	"testing"

	"LCM/internal/remote/wire"
)

func TestTokenRoundtrip(t *testing.T) {
	tests := []struct {
		name  string
		token wire.Token
	}{
		{"mit fingerprint und ca", wire.Token{AgentID: "0b39a4f2-1111-2222-3333-444455556666", Secret: "s3cr3t-abcdef",
			CertFingerprint: "aa11bb22", CAFingerprint: "cc33dd44"}},
		{"ohne ca (älteres LCM)", wire.Token{AgentID: "agent-2", Secret: "geheim", CertFingerprint: "aa11"}},
		{"ohne fingerprint (dev)", wire.Token{AgentID: "agent-1", Secret: "geheim"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token := wire.EncodeToken(tt.token)
			if !strings.HasPrefix(token, wire.TokenPrefix) {
				t.Fatalf("token ohne präfix: %q", token)
			}
			got, err := wire.ParseToken(token)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if got != tt.token {
				t.Fatalf("roundtrip: got %+v", got)
			}
		})
	}
}

// TestParseTokenAltesFormat: Ein Token aus LCM vor der CA (nur u, k, f)
// bleibt lesbar - der CA-Pin fehlt dann einfach.
func TestParseTokenAltesFormat(t *testing.T) {
	old := wire.TokenPrefix + base64.RawURLEncoding.EncodeToString([]byte(`{"u":"id","k":"geheim","f":"ff00"}`))
	got, err := wire.ParseToken(old)
	if err != nil {
		t.Fatal(err)
	}
	if got.CertFingerprint != "ff00" || got.CAFingerprint != "" {
		t.Errorf("altes Token gelesen als %+v", got)
	}
}

func TestParseTokenErrors(t *testing.T) {
	tests := []struct {
		name  string
		token string
	}{
		{"leer", ""},
		{"falsches präfix", "lcmb1.abc"},
		{"kaputtes base64", wire.TokenPrefix + "%%%%"},
		{"kein json", wire.TokenPrefix + "bm90LWpzb24"},
		{"unvollständig (nur agent-id)", wire.EncodeToken(wire.Token{AgentID: "id"})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := wire.ParseToken(tt.token); err == nil {
				t.Fatalf("erwartete fehler für %q", tt.token)
			}
		})
	}
}

func TestAgentIDFromTopic(t *testing.T) {
	tests := []struct {
		topic string
		want  string
	}{
		{"lcm/a/abc-123/cmd", "abc-123"},
		{"lcm/a/abc-123/res", "abc-123"},
		{"lcm/a/x/status", "x"},
		{"lcm/a/", ""},
		{"lcm/a/ohne-suffix", ""},
		{"anders/a/abc/cmd", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := wire.AgentIDFromTopic(tt.topic); got != tt.want {
			t.Errorf("AgentIDFromTopic(%q) = %q, want %q", tt.topic, got, tt.want)
		}
	}
}

func TestHashSecretStable(t *testing.T) {
	// SHA-256("test") - bekannte Referenz, schützt vor stillem Algorithmus-
	// Wechsel (Broker-Auth und Enrollment müssen denselben Hash bilden).
	const want = "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"
	if got := wire.HashSecret("test"); got != want {
		t.Fatalf("HashSecret geändert: %s", got)
	}
}

func TestTopics(t *testing.T) {
	if wire.TopicCmd("x") != "lcm/a/x/cmd" || wire.TopicRes("x") != "lcm/a/x/res" ||
		wire.TopicInv("x") != "lcm/a/x/inv" || wire.TopicStatus("x") != "lcm/a/x/status" {
		t.Fatal("topic-schema geändert")
	}
}
