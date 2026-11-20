package services

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"LCM/internal/core/domain"
)

// TestParseServerUsers: die LCMUSER-Zeilen des Scan-Skripts werden korrekt
// zerlegt - inklusive lastlog-Zeile (deren Spaltenzahl variiert), Kaputtem
// und dem unknown-Fall bei unlesbarem shadow.
func TestParseServerUsers(t *testing.T) {
	out := strings.Join([]string{
		"LCMUSER|root|0|/bin/bash|set|0|no|no|root             pts/3    192.168.201.1    Sat Aug 15 00:40:09 +0200 2026",
		"LCMUSER|lcm-svc|1000|/bin/bash|none|3|no|no|",
		"LCMUSER|tony|1001|/bin/bash|locked|2|yes|no|tony tty1 Mon Aug 17 08:00:00 +0200 2026", // ohne Herkunfts-Spalte
		"LCMUSER|gast|1002|/bin/sh|none|0|no|yes|",
		"LCMUSER|alt|1003|/bin/sh|unknown|1|no|no|alt pts/0 host Sun Aug 16 12:00:00 2026", // lastlog ohne Zeitzonen-Offset
		"irrelevantes echo vom server",
		"LCMUSER|kaputt|abc|/bin/sh|set|0|no|no|", // UID nicht numerisch → übergangen
	}, "\n")

	users := parseServerUsers(out)
	if len(users) != 5 {
		t.Fatalf("erwartet 5 konten, bekam %d: %+v", len(users), users)
	}

	root := users[0]
	if root.Username != "root" || root.UID != 0 || root.PasswordStatus != domain.PasswordStatusSet {
		t.Errorf("root falsch geparst: %+v", root)
	}
	if root.LastLoginAt == nil {
		t.Fatal("root: letzter login fehlt")
	}
	want := time.Date(2026, 8, 15, 0, 40, 9, 0, time.FixedZone("", 2*3600))
	if !root.LastLoginAt.Equal(want) {
		t.Errorf("root: letzter login %v, erwartet %v", root.LastLoginAt, want)
	}

	svc := users[1]
	if svc.SSHKeyCount != 3 || svc.LastLoginAt != nil || svc.PasswordStatus != domain.PasswordStatusNone {
		t.Errorf("lcm-svc falsch geparst: %+v", svc)
	}
	if !svc.KeyOnly() {
		t.Error("lcm-svc sollte als Nur-Key gelten")
	}

	tony := users[2]
	if !tony.TwoFactorEnrolled || tony.PasswordStatus != domain.PasswordStatusLocked || tony.LastLoginAt == nil {
		t.Errorf("tony falsch geparst: %+v", tony)
	}

	if !users[3].Disabled {
		t.Error("gast: disabled-flag nicht übernommen")
	}
	old := users[4]
	if old.PasswordStatus != domain.PasswordStatusUnknown || old.LastLoginAt == nil {
		t.Errorf("alt falsch geparst: %+v", old)
	}
}

// TestParseLastLoginUnverwertbar: ohne Wochentag im Rest gibt es keinen
// Zeitpunkt - lieber leer als geraten.
func TestParseLastLoginUnverwertbar(t *testing.T) {
	for _, rest := range []string{"", "nur text ohne datum", "tony pts/0 host"} {
		if got := parseLastLogin(rest); got != nil {
			t.Errorf("%q: erwartet nil, bekam %v", rest, got)
		}
	}
}

// TestUsersScanScriptImHelper: das Scan-Skript steckt genau EINMAL definiert
// im Helper (users-scan) UND läuft im Voll-Modus - beide Wege nutzen dieselbe
// Quelle, sonst laufen die Erhebungen auseinander.
func TestUsersScanScriptImHelper(t *testing.T) {
	if !strings.Contains(lcmHelperScript, "users-scan") {
		t.Fatal("helper kennt das unterkommando users-scan nicht")
	}
	// Kern des Skripts muss eingesetzt sein (nicht der Platzhalter).
	if strings.Contains(lcmHelperScript, "@@USERS_SCAN@@") {
		t.Fatal("users-scan-platzhalter wurde nicht ersetzt")
	}
	if !strings.Contains(lcmHelperScript, `echo "LCMUSER|$name|$uid|$shell|$pst|$keys|$tfa|$dis|$ll"`) {
		t.Fatal("scan-skript fehlt im helper")
	}
	// user-disable/-enable: Ablaufdatum ist der wirksame Teil (Key-Logins).
	for _, sub := range []string{"user-disable", "user-enable"} {
		if !strings.Contains(lcmHelperScript, sub) {
			t.Fatalf("helper kennt %s nicht", sub)
		}
	}
}

// TestKeyZaehlungMitOptionen läuft gegen eine echte authorized_keys: Keys
// mit Optionen davor zählten nicht mit, ein Konto mit
// from="10.0.0.0/8" ssh-ed25519 ... stand mit 0 Keys da.
func TestKeyZaehlungMitOptionen(t *testing.T) {
	ak := filepath.Join(t.TempDir(), "authorized_keys")
	content := strings.Join([]string{
		"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIA1 tony@laptop",
		`from="10.0.0.0/8" ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIA2 backup`,
		`command="/usr/bin/rrsync -ro /srv",restrict ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQ3 rsync`,
		`no-pty,no-port-forwarding ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNTY4 monitor`,
		`  sk-ssh-ed25519@openssh.com AAAAGnNrLXNzaC1lZDI1NTE5QG9wZW5zc2guY29t5 yubikey`,
		"# ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIA6 auskommentiert",
		`command="ssh-agent-wrapper start" no-pty`,
		"",
	}, "\n")
	if err := os.WriteFile(ak, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("sh", "-c", "ak=\"$1\"; "+sshKeyCountCmd, "sh", ak).Output()
	if err != nil {
		t.Fatalf("zählung scheitert: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != "5" {
		t.Errorf("5 keys erwartet, gezählt %s", got)
	}
}

// TestDisableEnableUserScripts: Voll-Modus-Skripte sperren wirklich JEDEN
// Login (Ablaufdatum!) und heben beides wieder auf.
func TestDisableEnableUserScripts(t *testing.T) {
	dis := disableUserScript("gast")
	for _, want := range []string{"usermod -L gast", "usermod -e 1970-01-02 gast", "chage -E 1 gast"} {
		if !strings.Contains(dis, want) {
			t.Errorf("disable-skript ohne %q: %s", want, dis)
		}
	}
	en := enableUserScript("gast")
	for _, want := range []string{"usermod -U gast", "usermod -e '' gast", "chage -E -1 gast"} {
		if !strings.Contains(en, want) {
			t.Errorf("enable-skript ohne %q: %s", want, en)
		}
	}
}
