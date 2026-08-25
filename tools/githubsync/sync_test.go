package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestMarkierungFindetIhrIssueWieder: Die Zuordnung hängt an der Zeile in der
// Beschreibung - wer sie umbaut, trennt bestehende Zwillinge von ihren
// GitHub-Issues, und der Sync legt alles doppelt an.
func TestMarkierungFindetIhrIssueWieder(t *testing.T) {
	gh := githubIssue{Number: 7, Title: "Absturz beim Start", HTMLURL: "https://github.com/Techeve/LCM/issues/7"}
	gh.User.Login = "melderin"

	desc := gitlabDescription(gh)
	if got := markerURL(desc); got != gh.HTMLURL {
		t.Fatalf("markerURL(gitlabDescription(...)) = %q, erwartet %q", got, gh.HTMLURL)
	}
	// Auch wenn jemand die Beschreibung unten ergänzt, hält die Zuordnung.
	if got := markerURL(desc + "\n\nInterner Nachtrag."); got != gh.HTMLURL {
		t.Fatalf("nachtrag zerstört die Zuordnung: %q", got)
	}
	if markerURL("Ein von Hand angelegtes Issue ohne Markierung") != "" {
		t.Fatal("ein Issue ohne Markierung darf keinem GitHub-Issue zugeordnet werden")
	}
}

// TestLeererBodyBleibtLesbar: GitHub-Issues ohne Text sind häufig - die
// Beschreibung darf dann nicht mit einem verwaisten Trenner enden.
func TestLeererBodyBleibtLesbar(t *testing.T) {
	gh := githubIssue{HTMLURL: "https://github.com/Techeve/LCM/issues/9"}
	gh.User.Login = "melder"
	if desc := gitlabDescription(gh); strings.Contains(desc, "---") {
		t.Errorf("leerer Body erzeugt verwaisten Trenner:\n%s", desc)
	}
}

// TestAbgleichEndeZuEnde spielt den Abgleich gegen gefälschte GitHub- und
// GitLab-Server durch: ein neues Issue (anlegen), ein auf GitHub
// geschlossenes (schließen), ein unverändertes, ein Pull Request (ignorieren).
func TestAbgleichEndeZuEnde(t *testing.T) {
	twinDesc := gitlabDescription(githubIssue{HTMLURL: "https://github.com/Techeve/LCM/issues/1"})
	openTwinDesc := gitlabDescription(githubIssue{HTMLURL: "https://github.com/Techeve/LCM/issues/2"})

	var created []string
	var stateEvents []string

	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/Techeve/LCM/issues", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") != "1" {
			w.Write([]byte("[]"))
			return
		}
		json.NewEncoder(w).Encode([]map[string]any{
			{"number": 1, "title": "geschlossen auf GitHub", "state": "closed", "html_url": "https://github.com/Techeve/LCM/issues/1"},
			{"number": 2, "title": "unverändert", "state": "open", "html_url": "https://github.com/Techeve/LCM/issues/2"},
			{"number": 3, "title": "neu", "state": "open", "html_url": "https://github.com/Techeve/LCM/issues/3"},
			{"number": 4, "title": "ein PR", "state": "open", "html_url": "https://github.com/Techeve/LCM/pull/4", "pull_request": map[string]any{}},
		})
	})
	mux.HandleFunc("GET /api/v4/projects/techeve%2Flcm-ce/issues", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") != "1" {
			w.Write([]byte("[]"))
			return
		}
		json.NewEncoder(w).Encode([]map[string]any{
			{"iid": 11, "state": "opened", "description": twinDesc},
			{"iid": 12, "state": "opened", "description": openTwinDesc},
		})
	})
	mux.HandleFunc("POST /api/v4/projects/techeve%2Flcm-ce/issues", func(w http.ResponseWriter, r *http.Request) {
		var p map[string]any
		json.NewDecoder(r.Body).Decode(&p)
		created = append(created, p["title"].(string))
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"iid": 99}`))
	})
	mux.HandleFunc("PUT /api/v4/projects/techeve%2Flcm-ce/issues/", func(w http.ResponseWriter, r *http.Request) {
		stateEvents = append(stateEvents, r.URL.Path+"?"+r.URL.RawQuery)
		w.Write([]byte(`{}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Beide Gegenstellen auf den Testserver biegen: GitHub über einen
	// austauschbaren Basis-Pfad wäre mehr Umbau als die Sache wert - der
	// Client folgt schlicht der URL, also reicht ein Rewrite im Transport.
	orig := httpClient.Transport
	httpClient.Transport = rewriteHost(srv)
	defer func() { httpClient.Transport = orig }()

	sum, err := sync(config{
		GitHubRepo: "Techeve/LCM", GitLabAPI: srv.URL + "/api/v4",
		GitLabProject: "techeve/lcm-ce", Token: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := summary{Seen: 3, Created: 1, Closed: 1, Unchanged: 1}
	if sum != want {
		t.Errorf("summary = %+v, erwartet %+v", sum, want)
	}
	if len(created) != 1 || created[0] != "neu" {
		t.Errorf("angelegt: %v, erwartet nur \"neu\"", created)
	}
	if len(stateEvents) != 1 || !strings.Contains(stateEvents[0], "/11?state_event=close") {
		t.Errorf("zustandswechsel: %v, erwartet close auf iid 11", stateEvents)
	}
}

// rewriteHost lenkt alle Anfragen (github.com wie GitLab) auf den Testserver.
func rewriteHost(srv *httptest.Server) http.RoundTripper {
	return roundTripFunc(func(req *http.Request) (*http.Response, error) {
		req.URL.Scheme = "http"
		req.URL.Host = strings.TrimPrefix(srv.URL, "http://")
		return http.DefaultTransport.RoundTrip(req)
	})
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
