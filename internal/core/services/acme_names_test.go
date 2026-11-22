package services_test

import (
	"slices"
	"strings"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
)

// acmeNamesOf liest die Namen, die der Dienst für das Zertifikat ableitet.
func acmeNamesOf(t *testing.T, svc *services.SettingsService) []string {
	t.Helper()
	st, err := svc.Get()
	if err != nil {
		t.Fatal(err)
	}
	return svc.ACMENames(st)
}

func wantNames(t *testing.T, what string, got []string, want ...string) {
	t.Helper()
	if !slices.Equal(got, append([]string{}, want...)) {
		t.Errorf("%s: Namen %v, erwartet %v", what, got, want)
	}
}

// Modus A: die verschiedenen Namen von Oberfläche, Agent und MCP, in dieser
// Reihenfolge; MCP nur, solange die Schnittstelle an ist.
func TestACMENamesModusA(t *testing.T) {
	svc, repo := newSettingsService(t)
	svc.WithAddressConfig(services.AddressConfig{Scheme: "https", FallbackHost: "localhost"})

	wantNames(t, "ohne Adressen", acmeNamesOf(t, svc))

	mustUpdate(t, svc, services.GlobalSettingsInput{
		PublicBaseURL: strPtr("https://LCM.example.org:8443"), PublicAgentURL: strPtr("https://agent.example.org"),
		PublicMCPURL: strPtr("https://mcp.example.org"),
	})
	wantNames(t, "MCP aus", acmeNamesOf(t, svc), "lcm.example.org", "agent.example.org")

	if err := repo.UpdateFields(map[string]any{"mcp_enabled": true}); err != nil {
		t.Fatal(err)
	}
	wantNames(t, "MCP an", acmeNamesOf(t, svc), "lcm.example.org", "agent.example.org", "mcp.example.org")

	mustUpdate(t, svc, services.GlobalSettingsInput{AgentEnabled: boolPtr(false)})
	wantNames(t, "Agent aus", acmeNamesOf(t, svc), "lcm.example.org", "mcp.example.org")
}

// Ein Name, der in mehreren Adressen steht, zählt einmal; Agent und MCP ohne
// eigene Adresse tragen den Namen der Oberfläche.
func TestACMENamesDoppelteUndAbgeleitete(t *testing.T) {
	svc, repo := newSettingsService(t)
	svc.WithAddressConfig(services.AddressConfig{Scheme: "https", FallbackHost: "localhost"})
	if err := repo.UpdateFields(map[string]any{"mcp_enabled": true}); err != nil {
		t.Fatal(err)
	}
	mustUpdate(t, svc, services.GlobalSettingsInput{PublicBaseURL: strPtr("https://lcm.example.org")})
	wantNames(t, "nur die Oberfläche hat eine Adresse", acmeNamesOf(t, svc), "lcm.example.org")

	mustUpdate(t, svc, services.GlobalSettingsInput{PublicAgentURL: strPtr("https://lcm.example.org:9320")})
	wantNames(t, "Agent mit demselben Namen", acmeNamesOf(t, svc), "lcm.example.org")
}

// IP-Adressen und Namen ohne Punkt bekommen von keiner öffentlichen CA ein
// Zertifikat und fallen heraus.
func TestACMENamesSchliessenIPUndLocalhostAus(t *testing.T) {
	svc, _ := newSettingsService(t)
	svc.WithAddressConfig(services.AddressConfig{Scheme: "https", FallbackHost: "localhost"})
	mustUpdate(t, svc, services.GlobalSettingsInput{
		PublicBaseURL: strPtr("https://10.0.0.5:9310"), PublicAgentURL: strPtr("https://lcm.example.org"),
	})
	wantNames(t, "IPv4 und Name", acmeNamesOf(t, svc), "lcm.example.org")

	mustUpdate(t, svc, services.GlobalSettingsInput{PublicBaseURL: strPtr("https://[2001:db8::1]:9310"), PublicAgentURL: strPtr("https://intern:9320")})
	wantNames(t, "IPv6 und Name ohne Punkt", acmeNamesOf(t, svc))
}

// Modus B: der eine Name.
func TestACMENamesModusB(t *testing.T) {
	svc, repo := newSettingsService(t)
	svc.WithAddressConfig(services.AddressConfig{Scheme: "https", FallbackHost: "localhost"})
	if err := repo.UpdateFields(map[string]any{"mcp_enabled": true}); err != nil {
		t.Fatal(err)
	}
	mustUpdate(t, svc, services.GlobalSettingsInput{AddressMode: strPtr("one_name"), PublicHost: strPtr("LCM.Example.org.")})
	wantNames(t, "ein Name", acmeNamesOf(t, svc), "lcm.example.org")

	mustUpdate(t, svc, services.GlobalSettingsInput{PublicHost: strPtr("192.0.2.10")})
	wantNames(t, "eine IP-Adresse", acmeNamesOf(t, svc))
}

// Modus C: drei verschiedene Namen; Ports in den Adressen zählen nicht.
func TestACMENamesModusC(t *testing.T) {
	svc, repo := newSettingsService(t)
	svc.WithAddressConfig(services.AddressConfig{Scheme: "https", FallbackHost: "localhost"})
	mustUpdate(t, svc, services.GlobalSettingsInput{
		AddressMode: strPtr("shared_port"), PublicBaseURL: strPtr("https://lcm.example.org:9310"),
		PublicAgentURL: strPtr("https://agent.example.org"), PublicMCPURL: strPtr("https://mcp.example.org"),
	})
	wantNames(t, "MCP aus", acmeNamesOf(t, svc), "lcm.example.org", "agent.example.org")
	if err := repo.UpdateFields(map[string]any{"mcp_enabled": true}); err != nil {
		t.Fatal(err)
	}
	wantNames(t, "MCP an", acmeNamesOf(t, svc), "lcm.example.org", "agent.example.org", "mcp.example.org")
}

// Hinweis zum Umstieg --------------------------------------------------------

// legacy legt den Stand einer älteren Version an: ACME an, eigene Namen, Modus.
func legacy(t *testing.T, f *acmeFixture, enabled bool, names, mode string) {
	t.Helper()
	err := f.settings.UpdateFields(map[string]any{"acme_enabled": enabled, "acme_names": names, "acme_mode": mode})
	if err != nil {
		t.Fatal(err)
	}
}

func noticeOf(t *testing.T, f *acmeFixture) *services.ACMENamesNotice {
	t.Helper()
	v, err := f.svc.View()
	if err != nil {
		t.Fatal(err)
	}
	return v.NamesChangedNotice
}

func TestACMENamesNoticeWhenOldNamesDoNotCoverTheAddresses(t *testing.T) {
	f := newACMEFixture(t)
	legacy(t, f, true, "alt.example.com", "single")

	n := noticeOf(t, f)
	if n == nil {
		t.Fatal("kein Hinweis, obwohl die alten Namen die Adressen nicht abdecken")
	}
	if strings.Join(n.Old, ",") != "alt.example.com" || strings.Join(n.New, ",") != "lcm.example.com,agent.example.com" || n.WasPerName {
		t.Errorf("Hinweis: %+v", n)
	}
}

func TestACMENamesNoticeWhenPerNameWasUsed(t *testing.T) {
	f := newACMEFixture(t)
	legacy(t, f, true, "lcm.example.com,agent.example.com", "per_name")

	n := noticeOf(t, f)
	if n == nil || !n.WasPerName || len(n.Old) != 2 {
		t.Fatalf("Hinweis: %+v, erwartet per_name mit zwei alten Namen", n)
	}
}

// Kein Hinweis, wo sich nichts ändert: gleiche Namen, ein Zertifikat; ein
// einzelner Name je Name ist dasselbe wie eines für alle; ACME aus; ein neuer
// Stand ohne alte Werte.
func TestACMENamesNoticeStaysQuietWithoutChange(t *testing.T) {
	f := newACMEFixture(t)
	for name, c := range map[string]struct {
		enabled     bool
		names, mode string
	}{
		"gleiche Namen":             {true, "lcm.example.com,agent.example.com", "single"},
		"alte Namen kleinere Menge": {true, "lcm.example.com", "single"},
		"ACME aus":                  {false, "alt.example.com", "per_name"},
		"kein alter Stand":          {true, "", "single"},
		"per_name ohne alte Namen":  {true, "", "per_name"},
	} {
		legacy(t, f, c.enabled, c.names, c.mode)
		if n := noticeOf(t, f); n != nil {
			t.Errorf("%s: Hinweis %+v", name, n)
		}
	}

	f.names = []string{"lcm.example.com"}
	legacy(t, f, true, "lcm.example.com", "per_name")
	if n := noticeOf(t, f); n != nil {
		t.Errorf("per_name mit einem Namen: Hinweis %+v", n)
	}
}

// Ein alter Name, der in den Adressen fehlt, geht nicht still verloren: Der
// Hinweis kommt - auch wenn die alten Namen mehr abdecken als die Adressen.
func TestACMENamesNoticeWhenOldHasNamesTheAddressesLack(t *testing.T) {
	f := newACMEFixture(t)
	legacy(t, f, true, "lcm.example.com,agent.example.com,extra.example.com", "single")
	n := noticeOf(t, f)
	if n == nil || strings.Join(n.Old, ",") != "lcm.example.com,agent.example.com,extra.example.com" {
		t.Fatalf("Hinweis: %+v", n)
	}
}

// Eine Bestandsinstallation ohne öffentliche Adresse: aus den Adressen folgt
// kein Name, der alte steht noch im Feld. Der Hinweis kommt, der Start
// vergisst den alten Namen nicht, und der Lauf nutzt ihn weiter.
func TestACMENamesLegacyNameSurvivesWithoutDerivedNames(t *testing.T) {
	f := newACMEFixture(t)
	f.names = nil
	legacy(t, f, true, "lcm.example.com", "single")

	n := noticeOf(t, f)
	if n == nil || len(n.New) != 0 || strings.Join(n.Old, ",") != "lcm.example.com" {
		t.Fatalf("Hinweis: %+v, erwartet alt lcm.example.com und keine neuen Namen", n)
	}
	f.svc.MigrateNames()
	if st, _ := f.settings.Get(); st.ACMENames != "lcm.example.com" {
		t.Fatalf("MigrateNames vergaß den einzigen Namen: %q", st.ACMENames)
	}
	err := f.svc.Run("system")
	if err != nil && strings.Contains(err.Error(), "DNS-Name") {
		t.Errorf("der Lauf verlor den alten Namen: %v", err)
	}
}

// Für den Lauf gilt: die Namen aus den Adressen zuerst, dazu die alten, ohne Doppelte.
func TestACMERunNamesUnionOfDerivedAndOld(t *testing.T) {
	f := newACMEFixture(t)
	legacy(t, f, true, "alt.example.com,lcm.example.com", "single")

	got := f.svc.RunNamesForTest(mustSettings(t, f))
	if strings.Join(got, ",") != "lcm.example.com,agent.example.com,alt.example.com" {
		t.Errorf("Namen für den Lauf: %v", got)
	}

	// Nach der Bestätigung gelten nur noch die Namen aus den Adressen.
	if _, err := f.svc.AckNamesNotice("admin"); err != nil {
		t.Fatal(err)
	}
	got = f.svc.RunNamesForTest(mustSettings(t, f))
	if strings.Join(got, ",") != "lcm.example.com,agent.example.com" {
		t.Errorf("Namen nach der Bestätigung: %v", got)
	}
}

func mustSettings(t *testing.T, f *acmeFixture) *domain.GlobalSettings {
	t.Helper()
	st, err := f.settings.Get()
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestACMENamesNoticeIsOneTime(t *testing.T) {
	f := newACMEFixture(t)
	legacy(t, f, true, "alt.example.com", "per_name")

	v, err := f.svc.AckNamesNotice("admin")
	if err != nil {
		t.Fatal(err)
	}
	if v.NamesChangedNotice != nil || noticeOf(t, f) != nil {
		t.Errorf("Hinweis nach der Bestätigung: %+v", v.NamesChangedNotice)
	}
	st, _ := f.settings.Get()
	if st.ACMENames != "" || st.ACMEMode != services.ACMEModeSingle {
		t.Errorf("alter Stand bleibt liegen: namen %q, modus %q", st.ACMENames, st.ACMEMode)
	}
	if auditCount(t, f.env, "settings.acme.notice_ack") != 1 {
		t.Error("kein Audit-Eintrag settings.acme.notice_ack")
	}
	// Ändern sich die Adressen später, kommt kein Umstiegshinweis wieder.
	f.names = []string{"neu.example.com"}
	if n := noticeOf(t, f); n != nil {
		t.Errorf("Hinweis nach späterer Adressänderung: %+v", n)
	}
}

// Wer die Einstellungen speichert, hat den Stand gesehen: Der Hinweis erlischt.
func TestACMENamesNoticeEndsWithSaving(t *testing.T) {
	f := newACMEFixture(t)
	legacy(t, f, true, "alt.example.com", "per_name")

	v, err := f.svc.Update(validUpdate(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	if v.NamesChangedNotice != nil {
		t.Errorf("Hinweis nach dem Speichern: %+v", v.NamesChangedNotice)
	}
	if st, _ := f.settings.Get(); st.ACMENames != "" || st.ACMEMode != services.ACMEModeSingle {
		t.Errorf("alter Stand bleibt liegen: %q %q", st.ACMENames, st.ACMEMode)
	}
}

// Beim Start vergisst LCM den alten Stand, wo es nichts zu melden gibt, und
// lässt ihn liegen, wo der Hinweis noch kommt.
func TestACMEMigrateNames(t *testing.T) {
	f := newACMEFixture(t)

	legacy(t, f, true, "lcm.example.com,agent.example.com", "single")
	f.svc.MigrateNames()
	if st, _ := f.settings.Get(); st.ACMENames != "" {
		t.Errorf("abgedeckter Stand blieb liegen: %q", st.ACMENames)
	}

	// Ein alter Name, den die Adressen nicht tragen, bleibt liegen.
	legacy(t, f, true, "lcm.example.com,extra.example.com", "single")
	f.svc.MigrateNames()
	if st, _ := f.settings.Get(); st.ACMENames != "lcm.example.com,extra.example.com" {
		t.Errorf("Name ohne Adresse wurde verworfen: %q", st.ACMENames)
	}

	legacy(t, f, false, "alt.example.com", "per_name")
	f.svc.MigrateNames()
	if st, _ := f.settings.Get(); st.ACMENames != "" || st.ACMEMode != services.ACMEModeSingle {
		t.Errorf("Stand bei ausgeschaltetem ACME blieb liegen: %q %q", st.ACMENames, st.ACMEMode)
	}

	legacy(t, f, true, "alt.example.com", "per_name")
	f.svc.MigrateNames()
	if st, _ := f.settings.Get(); st.ACMENames != "alt.example.com" || st.ACMEMode != "per_name" {
		t.Errorf("Stand mit Hinweis wurde verworfen: %q %q", st.ACMENames, st.ACMEMode)
	}
	if noticeOf(t, f) == nil {
		t.Error("der Hinweis ging verloren")
	}
}
