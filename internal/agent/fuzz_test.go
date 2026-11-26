package agent

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"log/slog"
	"math/big"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"testing"
	"time"
	"unicode/utf8"

	"LCM/internal/remote/wire"
)

// Fuzz-Ziele für alles, was der Agent als root von außen oder von der Platte
// liest: Rahmen der Web-Konsole (kommen von LCM), die Dateien der Job-Ablage
// (schreibt der Agent selbst, ein Absturz oder ein voller Datenträger lässt
// halbe Dateien zurück) und das Zertifikat, das ihm der Server vorlegt. Kein
// Fall darf in einen Panic laufen, und nichts darf aus der Ablage
// herausführen.
//
// Die Kommandos selbst (wire.Command) führt kein Ziel aus: Sie sind eine
// Shell-Zeile, die Prüfung der Decodierung steht in wire/fuzz_test.go.
//
// Ohne -fuzz laufen nur die Startkorpora. Lauf:
//
//	go test ./internal/agent -run '^$' -fuzz FuzzTerminalFrames -fuzztime 60s

// FuzzTerminalFrames: Zwei Rahmen von LCM an die Konsolen des Agents (mit einer
// Schein-Shell). Es gibt höchstens maxTerminals Sitzungen, jeder Rahmen an LCM
// trägt die Nummer seiner Sitzung, und nach closeAll bleibt nichts offen.
func FuzzTerminalFrames(f *testing.F) {
	for _, s := range []string{
		`{"s":"s1","n":1,"op":"open","t":"xterm-256color","c":120,"r":40}`,
		`{"s":"s1","n":2,"op":"data","d":"bHMK"}`,
		`{"s":"s1","n":3,"op":"resize","c":-5,"r":99999999}`,
		`{"s":"s2","n":4,"op":"close"}`,
		`{"s":"s3","n":1,"op":"open"}`,
		`{"s":"","n":1,"op":"open"}`,
		`{"s":"s1","n":18446744073709551615,"op":"data","d":"eA=="}`,
	} {
		f.Add([]byte(s), []byte(s))
	}
	f.Fuzz(func(t *testing.T, first, second []byte) {
		var mu sync.Mutex
		var sent []wire.TTYFrame
		ts := newTerminals(func(fr wire.TTYFrame, _ bool) {
			mu.Lock()
			sent = append(sent, fr)
			mu.Unlock()
		}, func() bool { return false }, slog.New(slog.DiscardHandler))

		old := startShell
		startShell = func(_ string, cols, rows int) (shellProcess, error) { return newFakeShell(cols, rows), nil }
		defer func() { startShell = old }()

		for _, payload := range [][]byte{first, second} {
			var fr wire.TTYFrame
			if json.Unmarshal(payload, &fr) != nil {
				continue
			}
			ts.handle(fr)
			if n := ts.count(); n > maxTerminals {
				t.Fatalf("%d Sitzungen offen, erlaubt %d", n, maxTerminals)
			}
		}
		ts.closeAll(nil)
		if n := ts.count(); n != 0 {
			t.Fatalf("nach closeAll noch %d Sitzungen", n)
		}
		mu.Lock()
		defer mu.Unlock()
		last := map[string]uint64{}
		for _, fr := range sent {
			if fr.Seq != last[fr.Session]+1 {
				t.Fatalf("Sitzung %q: Nummer %d nach %d", fr.Session, fr.Seq, last[fr.Session])
			}
			last[fr.Session] = fr.Seq
		}
	})
}

// FuzzJobStoreFiles: Die Dateien eines Jobs in der Ablage in beliebigem Zustand
// (meta.json, pid, exit, result.json, die Liste der bestätigten IDs). Das Lesen
// darf nicht scheitern, und was sich lesen lässt, schreibt sich wieder
// lesbar zurück.
func FuzzJobStoreFiles(f *testing.F) {
	meta := `{"id":"0b39a4f2-1111-2222-3333-444455556666","state":"running","started":"2026-11-24T10:00:00Z",` +
		`"finished":"0001-01-01T00:00:00Z","scope":"lcm-job-0b39a4f2.scope","pid_start":123456,"boot_id":"abc","aborted":true}`
	result := `{"id":"0b39a4f2-1111-2222-3333-444455556666","output":"ok","exit_code":0}`
	f.Add([]byte(meta), []byte("4242\n"), []byte("0\n"), []byte(result), []byte("a\nb\n"))
	f.Add([]byte(`{"id":"x","state":"finished"}`), []byte("-1"), []byte("abc"), []byte(`{`), []byte(""))
	f.Add([]byte(`{"id":"../../etc","state":"running"}`), []byte("0"), []byte("99999999999999999999"), []byte(`null`), []byte("\x00"))
	f.Add([]byte(``), []byte(``), []byte(``), []byte(``), []byte(``))
	f.Fuzz(func(t *testing.T, metaRaw, pidRaw, exitRaw, resultRaw, ackedRaw []byte) {
		root := t.TempDir()
		store := &jobStore{dir: root}
		dir := filepath.Join(root, "job")
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		for name, raw := range map[string][]byte{metaFile: metaRaw, pidFile: pidRaw, exitFile: exitRaw, resultFile: resultRaw} {
			if err := os.WriteFile(filepath.Join(dir, name), raw, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(root, ackedFile), ackedRaw, 0o600); err != nil {
			t.Fatal(err)
		}

		store.ackedIDs()
		store.all()
		store.withState(jobRunning)
		store.withState(jobFinished)
		store.prune(time.Now())
		r := NewRunner().WithJobsDir(root)
		r.Known()
		r.Pending()
		r.Result("job")
		r.Result(string(metaRaw))

		j, err := store.load("job")
		if err != nil {
			return
		}
		j.pid()
		j.readExit()
		j.outputSize()
		j.lastOutput()
		_, _ = j.result()
		if err := j.save(); err != nil {
			t.Fatalf("lesbares meta.json lässt sich nicht schreiben: %v", err)
		}
		again, err := store.load("job")
		if err != nil {
			t.Fatalf("eigenes meta.json nicht lesbar: %v", err)
		}
		if !again.meta.Started.Equal(j.meta.Started) || again.meta.ID != j.meta.ID || again.meta.State != j.meta.State ||
			again.meta.Scope != j.meta.Scope || again.meta.PIDStart != j.meta.PIDStart || again.meta.Aborted != j.meta.Aborted {
			t.Fatalf("meta.json nicht rundlaufend: %+v -> %+v", j.meta, again.meta)
		}
	})
}

// FuzzSafeID: Aus jeder Befehls-ID, die LCM schickt, wird ein Verzeichnisname
// in der Ablage. Er darf nie aus ihr herausführen, und create legt das
// Verzeichnis direkt darunter an.
func FuzzSafeID(f *testing.F) {
	for _, s := range []string{
		"0b39a4f2-1111-2222-3333-444455556666", "../../etc/passwd", "..", ".", "/", "a/b", "", "-rf", "x\x00y",
		"ä", string(make([]byte, 5000)), "meta.json", "acked",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, id string) {
		name := safeID(id)
		if !safeIDPattern.MatchString(name) || filepath.Base(name) != name || name == ackedFile {
			t.Fatalf("safeID(%q) = %q führt aus der Ablage heraus oder trifft eine Dateiname der Ablage", id, name)
		}
		if safeID(name) != name {
			t.Fatalf("safeID nicht stabil: %q -> %q -> %q", id, name, safeID(name))
		}
		root := t.TempDir()
		store := &jobStore{dir: root}
		j, err := store.create(id, "true")
		if err != nil {
			return // etwa eine doppelte ID
		}
		if filepath.Dir(j.dir) != root {
			t.Fatalf("Job-Verzeichnis %q liegt nicht direkt in %q", j.dir, root)
		}
		store.ack(id)
		if _, err := os.Stat(j.dir); err == nil {
			t.Fatalf("ack(%q) hat das Verzeichnis nicht entfernt", id)
		}
	})
}

// FuzzFitOutput: Die Ausgabe eines Kommandos, auch voller Steuerzeichen und
// ungültigem UTF-8, wird so gekürzt, dass das ganze Ergebnis ins Paketlimit
// passt (sonst verwirft der Broker es und die Verbindung bricht ab).
func FuzzFitOutput(f *testing.F) {
	f.Add([]byte("Reading package lists...\n"), []byte("Done\n"), 0, 4096)
	f.Add(make([]byte, 5000), make([]byte, 5000), 123456, 4096)
	f.Add([]byte("\x1b[31m\xff\xfe"), []byte("\xe2\x82"), 7, 1024)
	f.Fuzz(func(t *testing.T, head, tail []byte, omitted, limit int) {
		limit = 512 + max(limit, 0)%(64*1024)
		omitted = max(omitted, 0)
		// Die echte Ausgabe ist höchstens 2 MiB je Hälfte; der Fuzzer wüchse
		// sonst auf zig MiB, und jede Prüfung kodiert das Ergebnis mehrfach.
		head, tail = head[:min(len(head), 256<<10)], tail[:min(len(tail), 256<<10)]
		res := wire.Result{ID: "0b39a4f2-1111-2222-3333-444455556666", ExitCode: 100, Error: "exit"}
		fitOutput(&res, head, tail, omitted, limit)
		if size := resultSize(res); size > limit {
			t.Fatalf("Ergebnis %d Bytes, Grenze %d (Eingabe %d+%d)", size, limit, len(head), len(tail))
		}
		if !utf8.ValidString(res.Output) && utf8.Valid(head) && utf8.Valid(tail) {
			t.Fatalf("aus gültigem UTF-8 wurde ungültiges")
		}
	})
}

// FuzzCheckPeer: Die Zertifikatskette, die ein Server dem Agent vorlegt, ist
// beliebiger Netz-Input, solange das Zertifikat nicht geprüft ist. Ohne Pins
// und ohne Zertifikat einer vertrauten Wurzel darf nichts angenommen werden,
// und gelernt wird nur aus einer bestätigten Verbindung.
func FuzzCheckPeer(f *testing.F) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "LCM"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(1, 0, 0)}
	der, _ := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	expired := *tmpl
	expired.NotBefore, expired.NotAfter = time.Now().AddDate(-2, 0, 0), time.Now().AddDate(-1, 0, 0)
	derExpired, _ := x509.CreateCertificate(rand.Reader, &expired, &expired, &key.PublicKey, key)

	f.Add(der, []byte(nil), "", "", "", "lcm.example")
	f.Add(der, der, fingerprintOf(der), "", "", "lcm.example")
	f.Add(derExpired, []byte(nil), "", hex32(der), "", "")
	f.Add([]byte("kein zertifikat"), der, "AA", "bb", fingerprintOf(der), "")
	f.Fuzz(func(t *testing.T, leaf, next []byte, pinCert, pinKey, pinCA, hostname string) {
		// Unter macOS stürzt crypto/x509 (Go 1.27) mit SIGILL ab, sobald es einen
		// Namen mit ungültigem UTF-8 oder Leerzeichen an die Systemprüfung gibt
		// (SecPolicyCreateSSL). Der Agent läuft nur unter Linux, wo es die eigene
		// Prüfung nimmt; dort deckt der Lauf auch diese Namen ab (docker, siehe doc).
		if runtime.GOOS == "darwin" && !validHostChars(hostname) {
			t.Skip("macOS-Systemprüfung stürzt bei diesem Namen ab")
		}
		p := pins{cert: pinCert, key: pinKey, ca: pinCA}
		learned, err := checkPeer([][]byte{leaf, next}, p, hostname)
		if err != nil && learned.any() {
			t.Fatalf("Fehler %v, aber gelernt: %+v", err, learned)
		}
		if !p.any() && err == nil {
			t.Fatalf("ohne Pins angenommen: %q / %q", leaf, next)
		}
		if _, err := checkPeer(nil, p, hostname); err == nil {
			t.Fatal("ohne Zertifikat angenommen")
		}
	})
}

func hex32(der []byte) string {
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return ""
	}
	return keyPinOf(cert)
}

// Eine Befehls-ID "acked" darf nicht den Namen der Liste der bestätigten IDs
// treffen: Das Verzeichnis ließe sich nicht anlegen, und ack löschte die Liste.
func TestSafeIDMeidetDieListeDerBestaetigtenIDs(t *testing.T) {
	root := t.TempDir()
	store := &jobStore{dir: root}
	store.rememberAcked("alt-1")
	store.rememberAcked("alt-2")
	j, err := store.create(ackedFile, "true")
	if err != nil {
		t.Fatalf("Job mit der ID %q: %v", ackedFile, err)
	}
	if filepath.Base(j.dir) == ackedFile {
		t.Fatalf("Verzeichnis heißt wie die Liste: %s", j.dir)
	}
	store.ack(ackedFile)
	got := store.ackedIDs()
	if len(got) != 3 || got[0] != "alt-1" || got[1] != "alt-2" || got[2] != ackedFile {
		t.Errorf("bestätigte IDs = %v", got)
	}
}

// Die Datei pid stammt von der Platte. Steht darin 1 (halb geschrieben, beschädigt),
// darf kein Signal an -1 gehen: kill(-1) träfe jeden Prozess des Systems.
func TestSignalSendetNieAnPID1(t *testing.T) {
	var got []int
	old := killProcess
	killProcess = func(pid int, _ syscall.Signal) error { got = append(got, pid); return nil }
	defer func() { killProcess = old }()

	r := NewRunner()
	for _, pid := range []int{-5, 0, 1} {
		r.signal(jobTarget{pid: pid}, "SIGTERM", syscall.SIGTERM)
	}
	if len(got) != 0 {
		t.Errorf("Signale an %v", got)
	}
	r.signal(jobTarget{pid: 4242}, "SIGTERM", syscall.SIGTERM)
	if len(got) != 1 || got[0] != -4242 {
		t.Errorf("Signal an die Gruppe 4242 = %v, erwartet [-4242]", got)
	}
}

// validHostChars: nur Buchstaben, Ziffern, Punkt und Bindestrich.
func validHostChars(host string) bool {
	for _, r := range host {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-') {
			return false
		}
	}
	return true
}

// Das Feld truncated, das fitOutput beim Kürzen setzt, gehört in die gemessene
// Größe (vorher lag das Ergebnis um 17 Bytes über dem Limit). Der Fuzz-Fund
// steht in testdata/fuzz/FuzzFitOutput.
func TestFitOutputZaehltTruncatedMit(t *testing.T) {
	res := wire.Result{ID: "0b39a4f2-1111-2222-3333-444455556666", ExitCode: 100, Error: "exit"}
	head := append([]byte("\xf3\xb0"), make([]byte, 2154)...)
	fitOutput(&res, head, []byte("\xf3\xb0zzz"), 0, 1443)
	if size := resultSize(res); size > 1443 {
		t.Errorf("Ergebnis %d Bytes, Grenze 1443", size)
	}
	if !res.Truncated {
		t.Error("gekürzt, aber nicht als gekürzt gekennzeichnet")
	}
}
