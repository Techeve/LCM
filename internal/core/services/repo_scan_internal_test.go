package services

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestParseAptReposDeb822 prüft, dass sowohl klassische sources.list-Zeilen als
// auch deb822-.sources-Stanzas (Ubuntu 24.04) erkannt werden.
func TestParseAptReposDeb822(t *testing.T) {
	// Klassischer Teil, Marker, dann eine deb822-Stanza (wie vom Scan geliefert).
	out := strings.Join([]string{
		"deb http://archive.ubuntu.com/ubuntu noble main restricted",
		"@@@DEB822@@@",
		"",
		"Types: deb",
		"URIs: https://repo.techeve.de",
		"Suites: stable",
		"Components: main",
		"Signed-By: /usr/share/keyrings/techeve.gpg",
		"",
		"Types: deb",
		"URIs: http://old.example/debian",
		"Suites: bookworm",
		"Components: main",
		"Enabled: no",
	}, "\n")

	repos := parseAptRepos(out)
	lines := make([]string, len(repos))
	byLine := map[string]bool{}
	for i, r := range repos {
		lines[i] = r.Line
		byLine[r.Line] = r.Insecure
	}
	joined := strings.Join(lines, "\n")

	// Klassische Zeile bleibt erhalten (und ist unsicher wegen http).
	if !strings.Contains(joined, "deb http://archive.ubuntu.com/ubuntu noble main restricted") {
		t.Errorf("klassische Zeile fehlt:\n%s", joined)
	}
	// deb822-Eintrag wird als klassische Zeile aufgenommen - DAS ist der Bugfix.
	want := "deb https://repo.techeve.de stable main"
	if !byLineExists(byLine, want) {
		t.Errorf("deb822-Repo nicht aufgelistet, erwartet %q:\n%s", want, joined)
	}
	if byLine[want] {
		t.Errorf("https-Repo darf nicht als unsicher gelten: %q", want)
	}
	// Deaktivierte Stanza (Enabled: no) wird übersprungen.
	if strings.Contains(joined, "old.example") {
		t.Errorf("deaktivierte Stanza sollte fehlen:\n%s", joined)
	}
}

func byLineExists(m map[string]bool, key string) bool {
	_, ok := m[key]
	return ok
}

// TestAptRepoScanCmdLiestNurAktiveQuellen führt den echten Lesebefehl gegen ein
// nachgebautes /etc/apt aus. Gezeigt werden darf nur, was apt auch liest:
// *.list und *.sources. Stillgelegte Dateien (…​.list.disabled, .bak) und
// deb822-Stanzas mit „Enabled: no" gehören NICHT in die Übersicht - sonst zeigt
// LCM einen Paketkanal an, aus dem längst nichts mehr kommt.
func TestAptRepoScanCmdLiestNurAktiveQuellen(t *testing.T) {
	etc := t.TempDir()
	mustWrite := func(name, content string) {
		t.Helper()
		if strings.Contains(name, "/") {
			if err := os.MkdirAll(filepath.Join(etc, filepath.Dir(name)), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(etc, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Aktiv: klassische Enterprise-Zeile …
	mustWrite("sources.list.d/lcm-enterprise.list",
		"deb [signed-by=/etc/apt/keyrings/techeve-repo.gpg] https://repo.example/enterprise stable main\n")
	// … stillgelegt: der Vorgänger derselben Quelle …
	mustWrite("sources.list.d/lcm-alt.list.disabled", "deb https://alt.example/debian stable main\n")
	// … und eine Sicherungskopie, wie sie beim Basteln entsteht.
	mustWrite("sources.list.d/backup.bak", "deb https://sicherung.example/debian stable main\n")
	// deb822: die Community-Quelle, per „Enabled: no" abgeschaltet.
	mustWrite("sources.list.d/techeve.sources", strings.Join([]string{
		"Types: deb",
		"Enabled: no",
		"URIs: https://repo.example",
		"Suites: stable",
		"Components: main",
		"",
	}, "\n"))

	out, err := exec.Command("sh", "-c", aptRepoScanCmdIn(etc)).Output()
	if err != nil {
		t.Fatalf("Scan-Befehl: %v", err)
	}

	var lines []string
	for _, r := range parseAptRepos(string(out)) {
		lines = append(lines, r.Line)
	}
	joined := strings.Join(lines, "\n")

	if !strings.Contains(joined, "https://repo.example/enterprise stable main") {
		t.Errorf("die aktive Enterprise-Quelle fehlt:\n%s", joined)
	}
	for _, tot := range []string{"alt.example", "sicherung.example"} {
		if strings.Contains(joined, tot) {
			t.Errorf("stillgelegte Datei wird als aktive Quelle gezeigt (%s):\n%s", tot, joined)
		}
	}
	if strings.Contains(joined, "repo.example stable") {
		t.Errorf("abgeschaltete deb822-Quelle (Enabled: no) wird als aktiv gezeigt:\n%s", joined)
	}
}

// TestClampSessionTTL prüft die Begrenzung der Session-Dauer.
func TestClampSessionTTL(t *testing.T) {
	cases := []struct{ in, want int }{
		{0, 0},          // 0 = config-Vorgabe
		{-5, 0},         // negativ -> 0
		{1, 5},          // unter Minimum -> 5
		{60, 60},        // normal
		{999999, 43200}, // über Maximum (30 Tage) -> gekappt
		{43200, 43200},  // exakt Maximum
	}
	for _, c := range cases {
		if got := clampSessionTTL(c.in); got != c.want {
			t.Errorf("clampSessionTTL(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}

// TestRepoScanFindetRedHatQuellen: Red Hat schreibt "baseurl = …" mit
// Leerzeichen. Bis das Muster das zuließ, meldete LCM für einen RHEL-Server
// null Paketquellen - obwohl drei aktiv waren.
func TestRepoScanFindetRedHatQuellen(t *testing.T) {
	script := repoIniScan("/etc/yum.repos.d")
	for _, part := range []string{
		"[[:space:]]*(baseurl|mirrorlist|metalink)[[:space:]]*=", // Leerzeichen erlaubt
		"[[:space:]]*enabled[[:space:]]*=",                       // abgeschaltete Sektionen
		"/etc/yum.repos.d/*.repo",
	} {
		if !strings.Contains(script, part) {
			t.Errorf("im Skript fehlt %q:\n%s", part, script)
		}
	}
	// Die Ausgabe bleibt "baseurl=<url>", damit die Auswertung unverändert
	// bleibt - sonst fiele jede URL ohne Gleichheitszeichen still weg.
	if !strings.Contains(script, `printf "baseurl=%s\n", url`) {
		t.Errorf("die Ausgabeform hat sich geändert:\n%s", script)
	}
}

// TestRepoZeilenMitLeerzeichenWerdenZerlegt: Das Gegenstück in Go - was das
// awk liefert, muss hier ankommen.
func TestRepoZeilenMitLeerzeichenWerdenZerlegt(t *testing.T) {
	repos := parseRepoURIs("baseurl=https://cdn-ubi.redhat.com/content/public/ubi/dist/ubi10/10/x86_64/baseos/os\n" +
		"baseurl = http://alt.example/rhel\n")
	if len(repos) != 2 {
		t.Fatalf("erwartet zwei Quellen, war %+v", repos)
	}
	if repos[0].Insecure {
		t.Error("https-Quelle wurde als unsicher gewertet")
	}
	if !repos[1].Insecure {
		t.Error("http-Quelle wurde nicht als unsicher gewertet")
	}
}
