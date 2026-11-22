package acmex

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// fakeACMEDNS spielt einen acme-dns-Server: Er legt Konten an und nimmt TXT-Werte entgegen.
type fakeACMEDNS struct {
	*httptest.Server
	mu      sync.Mutex
	updates []string // Werte, die über /update kamen
}

func startFakeACMEDNS(t *testing.T) *fakeACMEDNS {
	t.Helper()
	f := &fakeACMEDNS{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/register":
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"username":"u1","password":"p1","fulldomain":"abc.auth.example.org","subdomain":"abc","allowfrom":[]}`)
		case "/update":
			var body struct{ Txt string }
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.mu.Lock()
			f.updates = append(f.updates, body.Txt+"|"+r.Header.Get("X-Api-User")+"|"+r.Header.Get("X-Api-Key"))
			f.mu.Unlock()
			_, _ = io.WriteString(w, `{"txt":"`+body.Txt+`"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

// Beim ersten Mal legt lego das Konto an und nennt den CNAME, den der Admin
// setzen muss; das Konto liegt danach in der Datei zu diesem Server, und der
// nächste Aufruf setzt den Wert.
func TestACMEDNSRegistersAccountOnceThenUpdates(t *testing.T) {
	server := startFakeACMEDNS(t)
	env := DNSEnv{DataDir: t.TempDir()}
	values := map[string]string{"api_base": server.URL}

	p, err := NewDNSProvider("acmedns", values, env)
	if err != nil {
		t.Fatal(err)
	}
	err = p.Present(context.Background(), "lcm.example.com", "token", "keyauth")
	if err == nil || !strings.Contains(err.Error(), "abc.auth.example.org") {
		t.Fatalf("erster Aufruf: %v, erwartet den CNAME-Hinweis", err)
	}
	path := acmeDNSStoragePath(env.DataDir, server.URL)
	if info, statErr := os.Stat(path); statErr != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("Kontodatei: %v, %v", info, statErr)
	}

	again, err := NewDNSProvider("acmedns", values, env)
	if err != nil {
		t.Fatal(err)
	}
	if err := again.Present(context.Background(), "lcm.example.com", "token", "keyauth"); err != nil {
		t.Fatalf("zweiter Aufruf: %v", err)
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if len(server.updates) != 1 || !strings.HasSuffix(server.updates[0], "|u1|p1") {
		t.Errorf("Updates: %v", server.updates)
	}
}

// Die Konten gehören zur Sicherung und kommen dort wieder hin; andere
// Namen im Archiv sind keine ACME-Dateien.
func TestACMEDNSAccountsAreBackedUp(t *testing.T) {
	dataDir := t.TempDir()
	path := acmeDNSStoragePath(dataDir, "https://auth.example.org")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	files, err := BackupFiles(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	name := backupACMEDNS + shortID("https://auth.example.org") + ".json"
	if files[name] != path {
		t.Fatalf("Sicherung: %v, erwartet %s", files, name)
	}
	if target, ok := RestoreTarget(dataDir, name); !ok || target != path {
		t.Errorf("Ziel: %q, %v", target, ok)
	}
	for _, bad := range []string{backupACMEDNS + "../x.json", backupACMEDNS + "zz.json", backupACMEDNS + shortID("a")} {
		if _, ok := RestoreTarget(dataDir, bad); ok {
			t.Errorf("%q wird zurückgespielt", bad)
		}
	}
}
