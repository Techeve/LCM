package services_test

import (
	"fmt"
	"io"
	"strings"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/storage/repositories"
)

// konsolenConn liefert ein Terminal, das einen festen Ausgabestrom abspielt.
type konsolenConn struct {
	sshx.Conn
	ausgabe string
}

func (c *konsolenConn) Terminal(string, int, int) (sshx.Terminal, error) {
	return &abspielTerminal{Reader: strings.NewReader(c.ausgabe)}, nil
}

type abspielTerminal struct{ *strings.Reader }

func (*abspielTerminal) Write(p []byte) (int, error) { return len(p), nil }
func (*abspielTerminal) Close() error                { return nil }
func (*abspielTerminal) Resize(int, int) error       { return nil }

// konsoleMitschneiden spielt eine Sitzung ab und liefert den gespeicherten
// Mitschnitt.
func konsoleMitschneiden(t *testing.T, ausgabe string) domain.SSHCommand {
	t.Helper()
	logs := repositories.NewSSHLogRepository(newTestEnv(t).DB())
	jobID := "job-konsole"
	conn := services.NewSSHRecorder(logs).Record(&konsolenConn{ausgabe: ausgabe}, services.SessionContext{JobID: &jobID, Purpose: "terminal"})
	term, err := conn.(sshx.TerminalConn).Terminal("xterm", 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	// Kleine Happen wie bei einer echten Sitzung, nicht ein einziger Block.
	buf := make([]byte, 4096)
	for {
		if _, err := term.Read(buf); err == io.EOF {
			break
		}
	}
	if err := term.Close(); err != nil {
		t.Fatal(err)
	}
	_, kommando := erstesKommando(t, logs, jobID)
	return kommando
}

// TestKonsolenMitschnittBehaeltAnfangUndEnde: Eine Sitzung, die größer ist
// als der Puffer, behält ihren Anfang UND ihr Ende - dort steht, was zuletzt
// geschah. Die Lücke ist markiert, samt Zahl der ausgelassenen Bytes.
func TestKonsolenMitschnittBehaeltAnfangUndEnde(t *testing.T) {
	var b strings.Builder
	b.WriteString("ANFANG der Sitzung\n")
	for i := 0; b.Len() < 300_000; i++ {
		fmt.Fprintf(&b, "zeile %06d\n", i)
	}
	b.WriteString("rm -rf /srv/daten\nENDE der Sitzung\n")
	ausgabe := b.String()

	out := konsoleMitschneiden(t, ausgabe).Output
	vorn, hinten, gefunden := strings.Cut(out, "\n\n… [Mitschnitt gekürzt: ")
	if !gefunden {
		t.Fatalf("Vermerk über die Lücke fehlt (Länge %d)", len(out))
	}
	vermerk, hinten, _ := strings.Cut(hinten, "] …\n\n")
	if !strings.HasPrefix(ausgabe, vorn) || !strings.HasPrefix(vorn, "ANFANG der Sitzung") {
		t.Errorf("der Anfang der Sitzung fehlt: %.40q", vorn)
	}
	if !strings.HasSuffix(ausgabe, hinten) || !strings.HasSuffix(hinten, "rm -rf /srv/daten\nENDE der Sitzung\n") {
		t.Errorf("das Ende der Sitzung fehlt: …%q", hinten[max(0, len(hinten)-40):])
	}
	ausgelassen := len(ausgabe) - len(vorn) - len(hinten)
	if want := fmt.Sprintf("%d Bytes ausgelassen", ausgelassen); vermerk != want {
		t.Errorf("Vermerk %q, erwartet %q", vermerk, want)
	}
}

// TestKonsolenMitschnittKurzBleibtVollstaendig: Was in den Puffer passt,
// steht ungekürzt und ohne Vermerk im Protokoll.
func TestKonsolenMitschnittKurzBleibtVollstaendig(t *testing.T) {
	ausgabe := strings.Repeat("kurze zeile\n", 1000)
	if out := konsoleMitschneiden(t, ausgabe).Output; out != ausgabe {
		t.Errorf("kurze Sitzung verändert: %d statt %d Bytes", len(out), len(ausgabe))
	}
}
