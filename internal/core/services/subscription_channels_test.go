package services

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
	"LCM/internal/version"
)

// fakeChannelRepos stellt die Paketquellen nach: je Pfadteil (Suite bzw. das
// Enterprise-Präfix) die Version des lcm-Pakets. Fehlt ein Eintrag, antwortet
// die Quelle mit 404. Liefert die Adresse des Enterprise-Repositorys.
func fakeChannelRepos(t *testing.T, community, beta, enterprise string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v := community
		switch {
		case strings.HasPrefix(r.URL.Path, "/enterprise/"):
			v = enterprise
		case strings.Contains(r.URL.Path, "/dists/"+subAptSuiteBeta+"/"):
			v = beta
		}
		if v == "" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("Package: lcm\nVersion: " + v + "\nArchitecture: amd64\n"))
	}))
	t.Cleanup(srv.Close)
	orig := RepoBaseURL
	RepoBaseURL = srv.URL
	t.Cleanup(func() { RepoBaseURL = orig })
	return srv.URL + "/enterprise"
}

func withInstalledVersion(t *testing.T, v string) {
	t.Helper()
	orig := version.Version
	version.Version = v
	t.Cleanup(func() { version.Version = orig })
}

// newChannelEnv baut den Dienst mit LCM-Host und einem Runner, der die
// gestarteten Skripte nur zählt.
func newChannelEnv(t *testing.T, current string) (*SubscriptionService, *repositories.SettingsRepository, *int) {
	t.Helper()
	svc, settings, db := newSubscriptionEnvDB(t)
	if err := db.Create(&domain.Server{Name: "lcm-host", Host: "localhost", SSHPort: 22, PackageManager: "apt"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := settings.UpdateFields(map[string]any{"subscription_apt_channel": current}); err != nil {
		t.Fatal(err)
	}
	started := 0
	svc.WithLcmHostRunner(func(name, script, actor string, after func(ok bool)) (*domain.Job, error) {
		started++
		return &domain.Job{}, nil
	})
	return svc, settings, &started
}

func lockOf(t *testing.T, view *ChannelsView, channel string) ChannelVersion {
	t.Helper()
	for _, c := range view.Channels {
		if c.Channel == channel {
			return c
		}
	}
	t.Fatalf("kanal %s fehlt", channel)
	return ChannelVersion{}
}

func TestKanalwechselZurueckUeberEineFeatureVersionIstGesperrt(t *testing.T) {
	fakeChannelRepos(t, "1.48.1", "2.0.0~beta.12", "")
	withInstalledVersion(t, "2.0.0-beta.12")
	svc, _, started := newChannelEnv(t, domain.AptChannelBeta)

	view, err := svc.ChannelVersions()
	if err != nil {
		t.Fatal(err)
	}
	if c := lockOf(t, view, domain.AptChannelCommunity); c.Latest != "1.48.1" || c.Lock != ChannelLockBehind {
		t.Errorf("community = %+v, erwartet 1.48.1 gesperrt", c)
	}
	if c := lockOf(t, view, domain.AptChannelBeta); c.Latest != "2.0.0~beta.12" || c.Lock != "" {
		t.Errorf("beta = %+v, erwartet eigener Kanal frei", c)
	}
	if c := lockOf(t, view, domain.AptChannelEnterprise); c.Lock != ChannelLockSubscription {
		t.Errorf("enterprise = %+v, erwartet gesperrt mangels Subscription", c)
	}

	if _, err := svc.SetAptChannel(domain.AptChannelCommunity, "admin"); !errors.Is(err, ErrChannelBehind) {
		t.Fatalf("wechsel nach community: %v, erwartet ErrChannelBehind", err)
	}
	if *started != 0 {
		t.Errorf("trotz Sperre %d Job gestartet", *started)
	}
}

func TestKanalwechselMitPatchRueckstandIstErlaubt(t *testing.T) {
	fakeChannelRepos(t, "2.0.0", "2.0.1~beta.1", "")
	withInstalledVersion(t, "2.0.1-beta.1")
	svc, _, started := newChannelEnv(t, domain.AptChannelBeta)

	if _, err := svc.SetAptChannel(domain.AptChannelCommunity, "admin"); err != nil {
		t.Fatalf("wechsel nach community: %v", err)
	}
	if *started != 1 {
		t.Errorf("gestartete Jobs = %d, erwartet 1", *started)
	}
}

func TestKanalwechselNachVornIstErlaubt(t *testing.T) {
	fakeChannelRepos(t, "1.48.1", "2.0.0~beta.12", "")
	withInstalledVersion(t, "1.48.1")
	svc, _, started := newChannelEnv(t, domain.AptChannelCommunity)

	if _, err := svc.SetAptChannel(domain.AptChannelBeta, "admin"); err != nil {
		t.Fatalf("wechsel nach beta: %v", err)
	}
	if *started != 1 {
		t.Errorf("gestartete Jobs = %d, erwartet 1", *started)
	}
}

func TestEigenerKanalBleibtImmerWaehlbar(t *testing.T) {
	fakeChannelRepos(t, "1.48.1", "", "")
	withInstalledVersion(t, "2.0.0")
	svc, _, started := newChannelEnv(t, domain.AptChannelCommunity)

	if _, err := svc.SetAptChannel(domain.AptChannelCommunity, "admin"); err != nil {
		t.Fatalf("eigenen kanal neu anwenden: %v", err)
	}
	if *started != 1 {
		t.Errorf("gestartete Jobs = %d, erwartet 1", *started)
	}
}

func TestKanalOhneAbrufbarenStandIstGesperrt(t *testing.T) {
	fakeChannelRepos(t, "2.0.0", "", "")
	withInstalledVersion(t, "2.0.0")
	svc, _, started := newChannelEnv(t, domain.AptChannelCommunity)

	view, err := svc.ChannelVersions()
	if err != nil {
		t.Fatal(err)
	}
	if c := lockOf(t, view, domain.AptChannelBeta); c.Lock != ChannelLockUnknown || c.Error == "" {
		t.Errorf("beta = %+v, erwartet gesperrt mit Fehler", c)
	}
	if _, err := svc.SetAptChannel(domain.AptChannelBeta, "admin"); !errors.Is(err, ErrChannelUnknown) {
		t.Fatalf("wechsel auf einen Kanal ohne Stand: %v, erwartet ErrChannelUnknown", err)
	}
	if *started != 0 {
		t.Errorf("trotz Sperre %d Job gestartet", *started)
	}
}

func TestEnterpriseMitSubscriptionZeigtSeinenStand(t *testing.T) {
	enterprise := fakeChannelRepos(t, "1.48.1", "2.0.0~beta.12", "1.48.0")
	withInstalledVersion(t, "1.48.1")
	svc, settings, _ := newChannelEnv(t, domain.AptChannelCommunity)
	key, err := svc.cipher.EncryptString("deadbeef00112233")
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.UpdateFields(map[string]any{
		"subscription_access_key_enc": key, "subscription_status": "active", "subscription_repo_url": enterprise,
	}); err != nil {
		t.Fatal(err)
	}

	view, err := svc.ChannelVersions()
	if err != nil {
		t.Fatal(err)
	}
	if c := lockOf(t, view, domain.AptChannelEnterprise); c.Latest != "1.48.0" || c.Lock != "" {
		t.Errorf("enterprise = %+v, erwartet 1.48.0 frei (nur Patch zurück)", c)
	}
}
