package services

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Die Umstell-Skripte werden hier wirklich ausgeführt - in einer Sandbox mit
// nachgebauten apt-Dateien und apt-Attrappen im PATH. Nur so fällt auf, was
// der reine Textvergleich nie zeigt: dass eine Datei unter einem anderen Namen
// liegt und das Stilllegen deshalb ins Leere greift.

// aptSandbox baut ein Mini-/etc/apt und liefert die Pfade dazu.
type aptSandbox struct {
	dir   string
	paths aptChannelPaths
	bin   string   // PATH-Verzeichnis mit den Attrappen
	env   []string // zusätzliche Umgebung für die Attrappen
}

func newAptSandbox(t *testing.T) *aptSandbox {
	t.Helper()
	dir := t.TempDir()
	for _, sub := range []string{"sources.list.d", "auth.conf.d", "preferences.d", "keyrings", "bin"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "keyrings", "techeve-repo.gpg"), []byte("key"), 0o644); err != nil {
		t.Fatal(err)
	}
	return &aptSandbox{
		dir: dir,
		bin: filepath.Join(dir, "bin"),
		paths: aptChannelPaths{
			Auth:         filepath.Join(dir, "auth.conf.d", "lcm-enterprise.conf"),
			Ent:          filepath.Join(dir, "sources.list.d", "lcm-enterprise.list"),
			Beta:         filepath.Join(dir, "sources.list.d", "lcm-beta.list"),
			ComList:      filepath.Join(dir, "sources.list.d", "techeve.list"),
			ComSources:   filepath.Join(dir, "sources.list.d", "techeve.sources"),
			Pref:         filepath.Join(dir, "preferences.d", "lcm-enterprise.pref"),
			KeyringGlobs: filepath.Join(dir, "keyrings", "techeve*.gpg"),
		},
	}
}

// stubApt legt die Attrappen an: apt-get („update" liefert rc) und apt-cache
// („policy" bzw. „policy lcm" liefern die vorgegebene Ausgabe) - also genau
// die Aufrufe der Skripte.
func (s *aptSandbox) stubApt(t *testing.T, updateRC int) {
	t.Helper()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(s.bin, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write("apt-get", "#!/bin/sh\ncase \"$1\" in\n  update) exit "+itoa(updateRC)+" ;;\nesac\nexit 0\n")
	write("apt-cache", "#!/bin/sh\ncase \"${2:-}\" in\n  lcm) printf '%s\\n' \"$STUB_POLICY_LCM\" ;;\n  *) printf '%s\\n' \"$STUB_POLICY\" ;;\nesac\n")
}

// labelled/unlabelled: die Ausgabe von „apt-cache policy" mit bzw. ohne
// Kennzeichnung der Kanäle im Release-File.
func (s *aptSandbox) policyLabelled() {
	s.setEnv("STUB_POLICY", " 500 https://repo.example stable/main amd64 Packages\n"+
		"     release o=TechEve,a=stable,n=stable,l="+aptLabelCommunity+",c=main,b=amd64\n"+
		"     origin repo.example")
}

func (s *aptSandbox) policyUnlabelled() {
	s.setEnv("STUB_POLICY", " 500 https://repo.example stable/main amd64 Packages\n"+
		"     release o=.,a=stable,n=stable,c=main,b=amd64\n"+
		"     origin repo.example")
}

// policyLcm gibt vor, woher lcm käme - die Grundlage der Gegenprobe.
func (s *aptSandbox) policyLcm(candidate, source string) {
	s.policyLcmSuite(candidate, source, "stable")
}

// policyLcmSuite ist dasselbe mit wählbarer Suite - im Beta-Kanal steht dort
// „beta/main", und genau daran erkennt die Gegenprobe den Kanal.
func (s *aptSandbox) policyLcmSuite(candidate, source, suite string) {
	out := "lcm:\n  Installed: 1.12.6\n  Candidate: " + candidate + "\n  Version table:\n" +
		"     " + candidate + " 500\n        500 " + source + " " + suite + "/main amd64 Packages\n" +
		" *** 1.12.6 100\n        100 /var/lib/dpkg/status"
	s.setEnv("STUB_POLICY_LCM", out)
}

func (s *aptSandbox) setEnv(k, v string) {
	s.env = append(s.env, k+"="+v)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	return string(rune('0' + i))
}

func (s *aptSandbox) run(t *testing.T, script string) (string, error) {
	t.Helper()
	cmd := exec.Command("sh", "-c", script)
	cmd.Env = append(append(os.Environ(), "PATH="+s.bin+":"+os.Getenv("PATH")), s.env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (s *aptSandbox) write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (s *aptSandbox) read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s nicht lesbar: %v", filepath.Base(path), err)
	}
	return string(b)
}

func (s *aptSandbox) exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// deb822Community ist die Community-Quelle, wie setup.sh sie heute anlegt.
const deb822Community = `# TechEve package repository
Types: deb
URIs: https://repo.example
Suites: stable
Components: main
Signed-By: /etc/apt/keyrings/techeve-repo.gpg
`

const entURL = "https://repo.example/enterprise"

// betaFallback ist die Adresse, die der Dienst kennt - sie greift erst, wenn
// sich aus den Quellen des Hosts keine ermitteln lässt.
const betaFallback = "https://repo.techeve.example"

// enterprise baut das Umstell-Skript für diese Sandbox.
func (s *aptSandbox) enterprise() string {
	return enterpriseChannelScriptIn(s.paths, entURL, "instanz", "geheim")
}

// beta baut die Umstellung auf den Beta-Kanal für diese Sandbox.
func (s *aptSandbox) beta() string {
	return betaChannelScriptIn(s.paths, betaFallback)
}

// TestKanaltrennungPerVorrangRegel: kennzeichnet der Repository-Server seine
// Kanäle (Label im Release-File), bleibt die Community-Quelle aktiv - sie
// liefert schließlich auch andere Pakete - und nur die LCM-Pakete werden per
// Vorrang-Regel auf den Enterprise-Kanal festgelegt.
func TestKanaltrennungPerVorrangRegel(t *testing.T) {
	sb := newAptSandbox(t)
	sb.write(t, sb.paths.ComSources, deb822Community)
	sb.stubApt(t, 0)
	sb.policyLabelled()
	sb.policyLcm("1.12.7", entURL)

	out, err := sb.run(t, sb.enterprise())
	if err != nil {
		t.Fatalf("Umstellung fehlgeschlagen: %v\n%s", err, out)
	}

	pref := sb.read(t, sb.paths.Pref)
	for _, must := range []string{
		"Package: " + aptPinnedPackages,
		"Pin: release l=" + aptLabelCommunity,
		"Pin: release l=" + aptLabelBeta,
		"Pin-Priority: -1",
	} {
		if !strings.Contains(pref, must) {
			t.Errorf("Vorrang-Regel ohne %q:\n%s", must, pref)
		}
	}
	if com := sb.read(t, sb.paths.ComSources); strings.Contains(com, "Enabled: no") {
		t.Errorf("die Community-Quelle wurde stillgelegt, obwohl die Vorrang-Regel greift:\n%s", com)
	}
	if !strings.Contains(out, "Gegenprobe: lcm 1.12.7 kommt aus "+entURL) {
		t.Errorf("die Gegenprobe fehlt oder nennt die falsche Quelle:\n%s", out)
	}
	if strings.Contains(out, "WARNUNG") {
		t.Errorf("unerwartete Warnung:\n%s", out)
	}
}

// TestKanaltrennungOhneKennzeichnungLegtStill: trägt der Repository-Server
// noch kein Label, ist die Vorrang-Regel wirkungslos - dann bleibt nur der
// grobe Weg. Das muss im Protokoll stehen, samt Folge für andere Pakete.
func TestKanaltrennungOhneKennzeichnungLegtStill(t *testing.T) {
	sb := newAptSandbox(t)
	sb.write(t, sb.paths.ComSources, deb822Community)
	sb.stubApt(t, 0)
	sb.policyUnlabelled()
	sb.policyLcm("1.12.7", entURL)

	out, err := sb.run(t, sb.enterprise())
	if err != nil {
		t.Fatalf("Umstellung fehlgeschlagen: %v\n%s", err, out)
	}

	com := sb.read(t, sb.paths.ComSources)
	if !strings.Contains(com, "Enabled: no") {
		t.Errorf("die Community-Quelle ist weiterhin aktiv - beide Kanäle nebeneinander:\n%s", com)
	}
	// Die Datei bleibt sonst unverändert: der Rückweg braucht sie vollständig.
	for _, must := range []string{"URIs: https://repo.example", "Signed-By: /etc/apt/keyrings/techeve-repo.gpg"} {
		if !strings.Contains(com, must) {
			t.Errorf("Stilllegen hat %q verloren:\n%s", must, com)
		}
	}
	if sb.exists(sb.paths.Pref) {
		t.Error("eine Vorrang-Regel ohne Kennzeichnung wäre wirkungslos und darf nicht liegen bleiben")
	}
	if !strings.Contains(out, "kennzeichnet seine Kanäle noch nicht") {
		t.Errorf("der grobe Weg wurde nicht begründet:\n%s", out)
	}
}

// TestKanaltrennungHeiltStillgelegteQuelle: ein Host, der vor der
// Kennzeichnung umgestellt wurde, hat die Community-Quelle stillgelegt. Wird
// die Umstellung danach erneut angewandt, muss die Quelle zurückkommen und die
// Vorrang-Regel ihre Stelle einnehmen - sonst verstellt der erste Lauf den
// besseren Weg für immer.
func TestKanaltrennungHeiltStillgelegteQuelle(t *testing.T) {
	sb := newAptSandbox(t)
	sb.write(t, sb.paths.ComSources, deb822Community)
	sb.stubApt(t, 0)
	sb.policyLcm("1.12.7", entURL)

	sb.policyUnlabelled()
	if out, err := sb.run(t, sb.enterprise()); err != nil {
		t.Fatalf("erster Lauf: %v\n%s", err, out)
	}
	if com := sb.read(t, sb.paths.ComSources); !strings.Contains(com, "Enabled: no") {
		t.Fatalf("Vorbedingung: die Quelle sollte stillgelegt sein:\n%s", com)
	}

	// Repository-Server kennzeichnet jetzt - zweiter Lauf.
	sb.env = nil
	sb.policyLabelled()
	sb.policyLcm("1.12.7", entURL)
	out, err := sb.run(t, sb.enterprise())
	if err != nil {
		t.Fatalf("zweiter Lauf: %v\n%s", err, out)
	}
	if com := sb.read(t, sb.paths.ComSources); strings.Contains(com, "Enabled: no") {
		t.Errorf("die Community-Quelle blieb stillgelegt:\n%s", com)
	}
	if !sb.exists(sb.paths.Pref) {
		t.Error("die Vorrang-Regel wurde nicht geschrieben")
	}
}

// TestGegenprobeWarntBeiFalschemKanal: die Frage, um die es geht, ist nicht
// „ist die Community-Quelle weg", sondern „woher käme lcm jetzt". Kommt es aus
// dem freien Kanal, muss das im Job-Protokoll stehen.
func TestGegenprobeWarntBeiFalschemKanal(t *testing.T) {
	sb := newAptSandbox(t)
	sb.write(t, sb.paths.ComSources, deb822Community)
	sb.stubApt(t, 0)
	sb.policyLabelled()
	sb.policyLcm("1.12.8", "https://repo.example") // freier Kanal ist voraus

	out, err := sb.run(t, sb.enterprise())
	if err != nil {
		t.Fatalf("Umstellung fehlgeschlagen: %v\n%s", err, out)
	}
	if !strings.Contains(out, "WARNUNG") || !strings.Contains(out, "https://repo.example") {
		t.Errorf("der falsche Kanal wurde nicht gemeldet:\n%s", out)
	}
}

// TestGegenprobeSchweigtOhneKandidat: ist lcm in keiner Quelle (nur installiert),
// gibt es nichts zu vergleichen - dann darf auch nicht gewarnt werden.
func TestGegenprobeSchweigtOhneKandidat(t *testing.T) {
	sb := newAptSandbox(t)
	sb.write(t, sb.paths.ComSources, deb822Community)
	sb.stubApt(t, 0)
	sb.policyLabelled()
	sb.setEnv("STUB_POLICY_LCM", "lcm:\n  Installed: 1.12.6\n  Candidate: 1.12.6\n  Version table:\n"+
		" *** 1.12.6 100\n        100 /var/lib/dpkg/status")

	out, err := sb.run(t, sb.enterprise())
	if err != nil {
		t.Fatalf("Umstellung fehlgeschlagen: %v\n%s", err, out)
	}
	if strings.Contains(out, "WARNUNG") || strings.Contains(out, "Gegenprobe") {
		t.Errorf("ohne Kandidat gibt es nichts zu melden:\n%s", out)
	}
}

// TestEnterpriseWechselOhneCommunityQuelleSagtBescheid: findet sich gar keine
// Community-Quelle, darf das nicht stillschweigend durchgehen.
func TestEnterpriseWechselOhneCommunityQuelleSagtBescheid(t *testing.T) {
	sb := newAptSandbox(t)
	sb.stubApt(t, 0)
	sb.policyUnlabelled()
	sb.policyLcm("1.12.7", entURL)

	out, err := sb.run(t, sb.enterprise())
	if err != nil {
		t.Fatalf("Umstellung fehlgeschlagen: %v\n%s", err, out)
	}
	if !strings.Contains(out, "keine Community-Paketquelle gefunden") {
		t.Errorf("fehlende Community-Quelle wurde verschwiegen:\n%s", out)
	}
}

// TestEnterpriseWechselRollbackStelltDeb822Wieder: wird der Schlüssel abgelehnt,
// muss die Maschine exakt so dastehen wie vorher - auch im deb822-Format.
func TestEnterpriseWechselRollbackStelltDeb822Wieder(t *testing.T) {
	sb := newAptSandbox(t)
	sb.write(t, sb.paths.ComSources, deb822Community)
	sb.stubApt(t, 1) // apt-get update scheitert = Zugang abgelehnt
	sb.policyLabelled()

	out, err := sb.run(t, sb.enterprise())
	if err == nil {
		t.Fatalf("abgelehnter Zugang muss den Job scheitern lassen:\n%s", out)
	}
	if com := sb.read(t, sb.paths.ComSources); strings.Contains(com, "Enabled: no") {
		t.Errorf("nach dem Rückbau ist die Community-Quelle noch stillgelegt:\n%s", com)
	}
	for _, p := range []string{sb.paths.Auth, sb.paths.Ent, sb.paths.Pref} {
		if sb.exists(p) {
			t.Errorf("%s hätte zurückgebaut werden müssen", filepath.Base(p))
		}
	}
}

// TestCommunityRueckweg: der Rückweg muss die deb822-Quelle wieder einschalten,
// die Vorrang-Regel entfernen (sie hielte lcm sonst weiterhin vom freien Kanal
// fern) und darf nicht abbrechen, bloß weil die klassische .list-Datei fehlt.
func TestCommunityRueckweg(t *testing.T) {
	for _, tc := range []struct {
		name      string
		labelled  bool
		wantPref  bool
		wantStill bool
	}{
		{name: "mit Vorrang-Regel", labelled: true, wantPref: true},
		{name: "mit stillgelegter Quelle", labelled: false, wantStill: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sb := newAptSandbox(t)
			sb.write(t, sb.paths.ComSources, deb822Community)
			sb.stubApt(t, 0)
			if tc.labelled {
				sb.policyLabelled()
			} else {
				sb.policyUnlabelled()
			}
			sb.policyLcm("1.12.7", entURL)

			if out, err := sb.run(t, sb.enterprise()); err != nil {
				t.Fatalf("Vorbedingung: %v\n%s", err, out)
			}
			if sb.exists(sb.paths.Pref) != tc.wantPref {
				t.Fatalf("Vorbedingung: Vorrang-Regel vorhanden=%v, erwartet %v", !tc.wantPref, tc.wantPref)
			}
			if strings.Contains(sb.read(t, sb.paths.ComSources), "Enabled: no") != tc.wantStill {
				t.Fatalf("Vorbedingung: Quelle stillgelegt=%v, erwartet %v", !tc.wantStill, tc.wantStill)
			}

			out, err := sb.run(t, communityRevertScriptIn(sb.paths))
			if err != nil {
				t.Fatalf("Rückweg fehlgeschlagen: %v\n%s", err, out)
			}
			com := sb.read(t, sb.paths.ComSources)
			if strings.Contains(com, "Enabled: no") {
				t.Errorf("die Community-Quelle ist nach dem Rückweg noch abgeschaltet:\n%s", com)
			}
			if !strings.Contains(com, "URIs: https://repo.example") {
				t.Errorf("die Community-Quelle wurde beschädigt:\n%s", com)
			}
			for _, p := range []string{sb.paths.Auth, sb.paths.Ent, sb.paths.Pref} {
				if sb.exists(p) {
					t.Errorf("%s besteht nach dem Rückweg weiter", filepath.Base(p))
				}
			}
		})
	}
}

// TestAltinstallationKlassischeListe: Installationen von früher haben die
// klassische techeve.list - die muss weiterhin umbenannt und zurückbenannt
// werden (ohne Kennzeichnung greift nur dieser Weg).
func TestAltinstallationKlassischeListe(t *testing.T) {
	sb := newAptSandbox(t)
	sb.write(t, sb.paths.ComList, "deb [signed-by=/etc/apt/keyrings/techeve.asc] https://repo.example stable main\n")
	sb.stubApt(t, 0)
	sb.policyUnlabelled()
	sb.policyLcm("1.12.7", entURL)

	if out, err := sb.run(t, sb.enterprise()); err != nil {
		t.Fatalf("Umstellung fehlgeschlagen: %v\n%s", err, out)
	}
	if sb.exists(sb.paths.ComList) {
		t.Error("die klassische Community-Quelle ist weiterhin aktiv")
	}
	if !sb.exists(sb.paths.ComList + ".disabled") {
		t.Error("die klassische Community-Quelle wurde nicht stillgelegt, sondern verloren")
	}
	// Keyring aus der signed-by-Option der alten Zeile.
	if ent := sb.read(t, sb.paths.Ent); !strings.Contains(ent, "signed-by=/etc/apt/keyrings/techeve.asc") {
		t.Errorf("Keyring der Altinstallation nicht übernommen: %q", ent)
	}

	if out, err := sb.run(t, communityRevertScriptIn(sb.paths)); err != nil {
		t.Fatalf("Rückweg fehlgeschlagen: %v\n%s", err, out)
	}
	if !sb.exists(sb.paths.ComList) {
		t.Error("der Rückweg hat die klassische Community-Quelle nicht wiederhergestellt")
	}
}

// TestEnterpriseWechselIstWiederholbar: die Umstellung wird auf bereits
// umgestellten Hosts erneut angewandt (genau der Weg, auf dem eine unvollständig
// gebliebene Umstellung geradegezogen wird). Beim zweiten Lauf darf sie weder
// scheitern noch die stillgelegte Quelle als „nicht gefunden" melden.
func TestEnterpriseWechselIstWiederholbar(t *testing.T) {
	for _, tc := range []struct{ name, file string }{
		{"deb822", "ComSources"},
		{"klassisch", "ComList"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sb := newAptSandbox(t)
			if tc.file == "ComSources" {
				sb.write(t, sb.paths.ComSources, deb822Community)
			} else {
				sb.write(t, sb.paths.ComList, "deb https://repo.example stable main\n")
			}
			sb.stubApt(t, 0)
			sb.policyUnlabelled()
			sb.policyLcm("1.12.7", entURL)

			if out, err := sb.run(t, sb.enterprise()); err != nil {
				t.Fatalf("erster Lauf: %v\n%s", err, out)
			}
			out, err := sb.run(t, sb.enterprise())
			if err != nil {
				t.Fatalf("zweiter Lauf: %v\n%s", err, out)
			}
			if strings.Contains(out, "keine Community-Paketquelle gefunden") {
				t.Errorf("die bereits stillgelegte Quelle wird als fehlend gemeldet:\n%s", out)
			}
			if strings.Contains(out, "WARNUNG") {
				t.Errorf("unerwartete Warnung im zweiten Lauf:\n%s", out)
			}
		})
	}
}

// TestBetaKanalNebenCommunity: der Beta-Kanal ist offen und liegt als eigene
// Suite an derselben Wurzel. Er kommt deshalb NEBEN die Community-Quelle -
// ohne Zugangsdaten, ohne Vorrang-Regel und ohne die freie Quelle anzurühren.
// Nur so bekommt ein Beta-Tester weiterhin das fertige Release, sobald es die
// Vorabversion überholt.
func TestBetaKanalNebenCommunity(t *testing.T) {
	sb := newAptSandbox(t)
	sb.write(t, sb.paths.ComSources, deb822Community)
	sb.stubApt(t, 0)
	sb.policyLabelled()
	sb.policyLcmSuite("1.13.0~beta.1", "https://repo.example", "beta")

	out, err := sb.run(t, sb.beta())
	if err != nil {
		t.Fatalf("Umstellung fehlgeschlagen: %v\n%s", err, out)
	}

	beta := sb.read(t, sb.paths.Beta)
	if !strings.Contains(beta, "signed-by=/etc/apt/keyrings/techeve-repo.gpg") ||
		!strings.Contains(beta, "https://repo.example beta main") {
		t.Errorf("Beta-Quelle falsch geschrieben: %q", beta)
	}
	if com := sb.read(t, sb.paths.ComSources); strings.Contains(com, "Enabled: no") {
		t.Errorf("die Community-Quelle wurde stillgelegt - im Beta-Kanal muss sie bleiben:\n%s", com)
	}
	if sb.exists(sb.paths.Pref) {
		t.Error("der Beta-Kanal braucht keine Vorrang-Regel - sie hielte das fertige Release fern")
	}
	if sb.exists(sb.paths.Auth) {
		t.Error("der Beta-Kanal ist offen und darf keine Zugangsdaten hinterlassen")
	}
	if !strings.Contains(out, "kommt aus dem Beta-Kanal") {
		t.Errorf("die Gegenprobe fehlt oder erkennt den Kanal nicht:\n%s", out)
	}
}

// TestBetaKanalOhneVorabversionSagtEsAn: liegt im Beta-Kanal gerade nichts
// Neueres, kommt die neueste Version weiter aus dem freien Kanal. Das ist
// richtig so - aber es muss dastehen, sonst sucht jemand nach einer
// Vorabversion, die es nicht gibt.
func TestBetaKanalOhneVorabversionSagtEsAn(t *testing.T) {
	sb := newAptSandbox(t)
	sb.write(t, sb.paths.ComSources, deb822Community)
	sb.stubApt(t, 0)
	sb.policyLabelled()
	sb.policyLcmSuite("1.13.0", "https://repo.example", "stable")

	out, err := sb.run(t, sb.beta())
	if err != nil {
		t.Fatalf("Umstellung fehlgeschlagen: %v\n%s", err, out)
	}
	if !strings.Contains(out, "keine neuere Vorabversion") {
		t.Errorf("der Stand wurde nicht erklärt:\n%s", out)
	}
	if strings.Contains(out, "WARNUNG") {
		t.Errorf("das ist kein Fehler und darf nicht als Warnung erscheinen:\n%s", out)
	}
}

// TestBetaKanalAdresseAusAltinstallation: die Adresse des Paket-Servers kommt
// aus der Quelle des Hosts, nicht aus einer festen Zeichenkette - sonst zöge
// ein eigener Repository-Server plötzlich Pakete von woanders.
func TestBetaKanalAdresseAusAltinstallation(t *testing.T) {
	sb := newAptSandbox(t)
	sb.write(t, sb.paths.ComList, "deb [signed-by=/etc/apt/keyrings/techeve.asc] https://repo.intern.example stable main\n")
	sb.stubApt(t, 0)
	sb.policyLabelled()
	sb.policyLcmSuite("1.13.0~beta.1", "https://repo.intern.example", "beta")

	out, err := sb.run(t, sb.beta())
	if err != nil {
		t.Fatalf("Umstellung fehlgeschlagen: %v\n%s", err, out)
	}
	if beta := sb.read(t, sb.paths.Beta); !strings.Contains(beta, "https://repo.intern.example beta main") {
		t.Errorf("die Adresse des eigenen Servers wurde nicht übernommen: %q", beta)
	}
	if strings.Contains(out, betaFallback) {
		t.Errorf("die Vorgabe-Adresse hat die des Hosts verdrängt:\n%s", out)
	}
}

// TestBetaKanalOhneQuelleNimmtVorgabe: hat der Host gar keine Community-Quelle
// mehr (z. B. lange auf Enterprise gelaufen), bleibt nur die vom Dienst
// bekannte Adresse.
func TestBetaKanalOhneQuelleNimmtVorgabe(t *testing.T) {
	sb := newAptSandbox(t)
	sb.stubApt(t, 0)
	sb.policyLabelled()
	sb.policyLcmSuite("1.13.0~beta.1", betaFallback, "beta")

	out, err := sb.run(t, sb.beta())
	if err != nil {
		t.Fatalf("Umstellung fehlgeschlagen: %v\n%s", err, out)
	}
	if beta := sb.read(t, sb.paths.Beta); !strings.Contains(beta, betaFallback+" beta main") {
		t.Errorf("Vorgabe-Adresse nicht verwendet: %q", beta)
	}
}

// TestBetaKanalUnerreichbarBautZurueck: kennt der Server die Beta-Suite nicht,
// muss der Job scheitern und darf keine tote Quelle hinterlassen - sonst
// scheitert ab da jedes „apt update" auf dem Host.
func TestBetaKanalUnerreichbarBautZurueck(t *testing.T) {
	sb := newAptSandbox(t)
	sb.write(t, sb.paths.ComSources, deb822Community)
	sb.stubApt(t, 1) // apt-get update scheitert = Suite nicht vorhanden
	sb.policyLabelled()

	out, err := sb.run(t, sb.beta())
	if err == nil {
		t.Fatalf("eine unerreichbare Beta-Quelle muss den Job scheitern lassen:\n%s", out)
	}
	if sb.exists(sb.paths.Beta) {
		t.Error("die Beta-Quelle blieb liegen, obwohl sie nicht erreichbar ist")
	}
	if com := sb.read(t, sb.paths.ComSources); strings.Contains(com, "Enabled: no") {
		t.Errorf("die Community-Quelle wurde in Mitleidenschaft gezogen:\n%s", com)
	}
}

// TestBetaWechselRaeumtEnterpriseAb: vom Enterprise- auf den Beta-Kanal. Bleibt
// die Vorrang-Regel liegen, hält sie lcm von BEIDEN offenen Kanälen fern - der
// Host bekäme nie wieder ein Update.
func TestBetaWechselRaeumtEnterpriseAb(t *testing.T) {
	sb := newAptSandbox(t)
	sb.write(t, sb.paths.ComSources, deb822Community)
	sb.stubApt(t, 0)
	sb.policyLabelled()
	sb.policyLcm("1.12.7", entURL)
	if out, err := sb.run(t, sb.enterprise()); err != nil {
		t.Fatalf("Vorbedingung: %v\n%s", err, out)
	}

	sb.env = nil
	sb.policyLabelled()
	sb.policyLcmSuite("1.13.0~beta.1", "https://repo.example", "beta")
	out, err := sb.run(t, sb.beta())
	if err != nil {
		t.Fatalf("Wechsel auf Beta fehlgeschlagen: %v\n%s", err, out)
	}
	for _, p := range []string{sb.paths.Auth, sb.paths.Ent, sb.paths.Pref} {
		if sb.exists(p) {
			t.Errorf("%s besteht nach dem Wechsel auf Beta weiter", filepath.Base(p))
		}
	}
	if com := sb.read(t, sb.paths.ComSources); strings.Contains(com, "Enabled: no") {
		t.Errorf("die Community-Quelle blieb stillgelegt:\n%s", com)
	}
}

// TestEnterpriseWechselEntferntBetaQuelle: der offene Beta-Kanal liegt
// versionsmäßig vor dem abgehangenen - bleibt er stehen, wäre der Wechsel
// auf Enterprise wirkungslos.
func TestEnterpriseWechselEntferntBetaQuelle(t *testing.T) {
	sb := newAptSandbox(t)
	sb.write(t, sb.paths.ComSources, deb822Community)
	sb.stubApt(t, 0)
	sb.policyLabelled()
	sb.policyLcmSuite("1.13.0~beta.1", "https://repo.example", "beta")
	if out, err := sb.run(t, sb.beta()); err != nil {
		t.Fatalf("Vorbedingung: %v\n%s", err, out)
	}

	sb.env = nil
	sb.policyLabelled()
	sb.policyLcm("1.12.7", entURL)
	if out, err := sb.run(t, sb.enterprise()); err != nil {
		t.Fatalf("Wechsel auf Enterprise fehlgeschlagen: %v\n%s", err, out)
	}
	if sb.exists(sb.paths.Beta) {
		t.Error("die Beta-Quelle liefert weiter Vorabversionen, obwohl der Host auf Enterprise steht")
	}
}

// TestCommunityRueckwegEntferntBetaQuelle: der Rückweg endet auf dem
// Community-Kanal - mit Beta-Quelle daneben wäre er keiner.
func TestCommunityRueckwegEntferntBetaQuelle(t *testing.T) {
	sb := newAptSandbox(t)
	sb.write(t, sb.paths.ComSources, deb822Community)
	sb.stubApt(t, 0)
	sb.policyLabelled()
	sb.policyLcmSuite("1.13.0~beta.1", "https://repo.example", "beta")
	if out, err := sb.run(t, sb.beta()); err != nil {
		t.Fatalf("Vorbedingung: %v\n%s", err, out)
	}

	out, err := sb.run(t, communityRevertScriptIn(sb.paths))
	if err != nil {
		t.Fatalf("Rückweg fehlgeschlagen: %v\n%s", err, out)
	}
	if sb.exists(sb.paths.Beta) {
		t.Error("die Beta-Quelle besteht nach dem Rückweg weiter")
	}
	if com := sb.read(t, sb.paths.ComSources); !strings.Contains(com, "URIs: https://repo.example") {
		t.Errorf("die Community-Quelle wurde beschädigt:\n%s", com)
	}
}

// TestAuthDateiNurFuerRoot: der Zugangsschlüssel darf niemandem sonst offenstehen.
func TestAuthDateiNurFuerRoot(t *testing.T) {
	sb := newAptSandbox(t)
	sb.write(t, sb.paths.ComSources, deb822Community)
	sb.stubApt(t, 0)
	sb.policyLabelled()
	sb.policyLcm("1.12.7", entURL)

	if out, err := sb.run(t, sb.enterprise()); err != nil {
		t.Fatalf("Umstellung fehlgeschlagen: %v\n%s", err, out)
	}
	info, err := os.Stat(sb.paths.Auth)
	if err != nil {
		t.Fatalf("Zugangsdaten fehlen: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("Zugangsdaten mit Rechten %v - erwartet 0600", info.Mode().Perm())
	}
	// Die Vorrang-Regel dagegen ist keine Geheimnis-Datei und muss trotz
	// umask 077 im Skript für alle lesbar bleiben (apt-Konvention).
	if pref, err := os.Stat(sb.paths.Pref); err != nil {
		t.Fatalf("Vorrang-Regel fehlt: %v", err)
	} else if pref.Mode().Perm() != 0o644 {
		t.Errorf("Vorrang-Regel mit Rechten %v - erwartet 0644", pref.Mode().Perm())
	}
	// Der Keyring kommt aus dem deb822-Feld Signed-By.
	ent := sb.read(t, sb.paths.Ent)
	if !strings.Contains(ent, "signed-by=/etc/apt/keyrings/techeve-repo.gpg") || !strings.Contains(ent, entURL+" stable main") {
		t.Errorf("Enterprise-Quelle falsch geschrieben: %q", ent)
	}
}
