package services

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
	"LCM/internal/version"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func newReleaseNotes(t *testing.T, status int, body string) (*ReleaseNotesService, *int) {
	t.Helper()
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	previousURL, previousVersion := ReleaseNotesBaseURL, version.Version
	ReleaseNotesBaseURL, version.Version = server.URL+"/", "2.0.0"
	t.Cleanup(func() { ReleaseNotesBaseURL, version.Version = previousURL, previousVersion })
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.GlobalSettings{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&domain.GlobalSettings{}).Error; err != nil {
		t.Fatal(err)
	}
	return NewReleaseNotesService(repositories.NewSettingsRepository(db), repositories.NewUserRepository(db)), &calls
}

func TestReleaseNotesFollowTheUpdate(t *testing.T) {
	s, _ := newReleaseNotes(t, 200, "### Features\n\n- neu <b>fett</b>\n")
	user := &domain.User{ID: 1}

	if notes, err := s.Pending(user); err != nil || notes != nil {
		t.Fatalf("ohne Update gibt es nichts zu zeigen: %v, %v", notes, err)
	}
	if err := s.NoteUpdate("", "2.0.0"); err != nil {
		t.Fatal(err)
	}
	if notes, _ := s.Pending(user); notes != nil {
		t.Fatal("eine Erstinstallation hat keine Hinweise")
	}
	if err := s.NoteUpdate("1.9.0", "2.0.0"); err != nil {
		t.Fatal(err)
	}
	notes, err := s.Pending(user)
	if err != nil || notes == nil {
		t.Fatalf("Hinweise erwartet: %v, %v", notes, err)
	}
	if notes.Version != "2.0.0" {
		t.Errorf("Version = %q", notes.Version)
	}
	user.ReleaseNotesSeen = "2.0.0"
	if notes, _ := s.Pending(user); notes != nil {
		t.Error("wer sie gesehen hat, bekommt sie nicht noch einmal")
	}
	if notes, _ := s.Pending(&domain.User{ID: 2}); notes == nil {
		t.Error("jeder Administrator bekommt sie einmal")
	}
}

func TestReleaseNotesEscapeMarkup(t *testing.T) {
	s, _ := newReleaseNotes(t, 200, "<script>alert(1)</script>\n")
	if err := s.NoteUpdate("1.9.0", "2.0.0"); err != nil {
		t.Fatal(err)
	}
	notes, _ := s.Pending(&domain.User{ID: 1})
	if notes == nil || strings.Contains(notes.HTML, "<script") {
		t.Fatalf("Quelle muss maskiert sein: %+v", notes)
	}
}

func TestReleaseNotesMissingFileStaysOpenAndBacksOff(t *testing.T) {
	s, calls := newReleaseNotes(t, 404, "fehlt")
	if err := s.NoteUpdate("1.9.0", "2.0.0"); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if notes, err := s.Pending(&domain.User{ID: 1}); err != nil || notes != nil {
			t.Fatalf("ohne Datei kein Fenster: %v, %v", notes, err)
		}
	}
	if *calls != 1 {
		t.Errorf("Doku-Server %d-mal gefragt, erwartet einmal bis zur Wartezeit", *calls)
	}
}
