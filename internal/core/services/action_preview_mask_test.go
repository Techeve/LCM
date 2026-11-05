package services

import (
	"strings"
	"testing"
)

// fakeKeyBlock ist ein Schlüsselblock ohne Inhalt. Die Kopfzeile wird erst zur
// Laufzeit zusammengesetzt, damit Geheimnis-Scanner den Test nicht anmahnen.
func fakeKeyBlock() string {
	kind := "OPENSSH " + "PRIV" + "ATE KEY"
	return "-----BEGIN " + kind + "-----\nAAAA\nBBBB\n-----END " + kind + "-----"
}

func TestMaskPreviewSecrets(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"Umgebungsvariable", "DB_PASSWORD=hunter2 mysqldump app", "DB_PASSWORD=•••• mysqldump app"},
		{"Token in Anführungszeichen", `API_TOKEN="abc def" ./run`, "API_TOKEN=•••• ./run"},
		{"Option mit Leerzeichen", "mycli --password geheim --host db", "mycli --password •••• --host db"},
		{"Option mit Gleichheitszeichen", "mycli --token=abc123", "mycli --token=••••"},
		{"Authorization-Kopf", "curl -H 'Authorization: Bearer abc123' https://x.test", "curl -H 'Authorization: Bearer ••••' https://x.test"},
		{"sudo -S", "echo 'geheim' | sudo -S apt-get update", "echo •••• | sudo -S apt-get update"},
		{"URL mit Zugangsdaten", "git clone https://user:pw123@git.test/r.git", "git clone https://user:••••@git.test/r.git"},
		{"curl -u", "curl -u admin:pw123 https://x.test", "curl -u admin:•••• https://x.test"},
		{"Privater Schlüssel", "cat <<EOF\n" + fakeKeyBlock() + "\nEOF", "cat <<EOF\n••••\nEOF"},
		{"Base64-Passwort", "CSPW=$(printf '%s' 'aHVudGVyMg==' | base64 -d)", "CSPW=$(printf '%s' '••••' | base64 -d)"},
		{"Console-Key", "cscli console enroll abc123XYZ 2>/dev/null", "cscli console enroll •••• 2>/dev/null"},
		{"Harmloses bleibt", "apt-get -y upgrade; sed -i 's/PasswordAuthentication no/x/' f; StrictHostKeyChecking=no", "apt-get -y upgrade; sed -i 's/PasswordAuthentication no/x/' f; StrictHostKeyChecking=no"},
		{"Umgebung des Voll-Updates bleibt", "LCM_APT_PHASED=1\nLCM_APT_FORCE='openssl libc6'\nlcm_apt_update", "LCM_APT_PHASED=1\nLCM_APT_FORCE='openssl libc6'\nlcm_apt_update"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := maskPreviewSecrets(tc.in)
			if got != tc.want {
				t.Errorf("maskPreviewSecrets(%q)\n got: %q\nwant: %q", tc.in, got, tc.want)
			}
			if again := maskPreviewSecrets(got); again != got {
				t.Errorf("nicht idempotent: %q", again)
			}
		})
	}
}

// TestMaskPreviewSecretsKeepsRedactionOfLog: Auch was das SSH-Protokoll schwärzt,
// zeigt die Vorschau nicht (apt auth.conf der Enterprise-Umstellung).
func TestMaskPreviewSecretsKeepsRedactionOfLog(t *testing.T) {
	in := "printf 'machine %s\\nlogin %s\\npassword %s\\n' 'https://repo.test' 'user' 'geheim' > /etc/apt/auth.conf"
	got := maskPreviewSecrets(in)
	if strings.Contains(got, "geheim") {
		t.Errorf("Zugangsschlüssel sichtbar: %s", got)
	}
	if strings.Contains(got, redactPlaceholder) {
		t.Errorf("Platzhalter des Protokolls statt ••••: %s", got)
	}
}
