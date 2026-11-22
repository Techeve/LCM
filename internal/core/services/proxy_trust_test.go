package services_test

import (
	"net/netip"
	"strings"
	"testing"

	"LCM/internal/core/services"
	"LCM/internal/netfilter"
)

func proxiesInput(list ...string) services.GlobalSettingsInput {
	return services.GlobalSettingsInput{TrustedProxies: &list}
}

func trustHolder(t *testing.T) (*services.SettingsService, *netfilter.ProxyTrustHolder) {
	t.Helper()
	svc, _ := newSettingsService(t)
	holder := netfilter.NewProxyTrustHolder(netfilter.ProxyTrust{})
	if _, err := svc.WithProxyTrust(holder); err != nil {
		t.Fatal(err)
	}
	return svc, holder
}

// Gültige Einträge werden normalisiert gespeichert und wirken sofort in der
// Regel, die die Middleware liest.
func TestTrustedProxiesGespeichertUndWirkenSofort(t *testing.T) {
	svc, holder := trustHolder(t)
	on := true
	list := []string{"10.0.0.5", "192.168.0.0/16", "private"}
	mustUpdate(t, svc, services.GlobalSettingsInput{TrustProxyHeader: &on, TrustedProxies: &list})

	st, err := svc.Get()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(st.TrustedProxies, ","); got != "10.0.0.5/32,192.168.0.0/16,private" || !st.TrustProxyHeader {
		t.Errorf("gespeichert: %q, trust=%v", got, st.TrustProxyHeader)
	}
	trust := holder.Load()
	if !trust.Trusts(netip.MustParseAddr("10.0.0.5")) || !trust.Trusts(netip.MustParseAddr("192.168.4.4")) {
		t.Error("gelistete Proxys werden nicht vertraut")
	}
	if trust.Trusts(netip.MustParseAddr("203.0.113.9")) {
		t.Error("ein fremder Peer wird vertraut")
	}

	off := false
	mustUpdate(t, svc, services.GlobalSettingsInput{TrustProxyHeader: &off})
	if holder.Load().Trusts(netip.MustParseAddr("10.0.0.5")) {
		t.Error("nach dem Ausschalten wird noch vertraut")
	}
}

// Ungültige Einträge und Bereiche über alle Adressen lehnt der Dienst ab; die
// Regel bleibt dabei, wie sie war.
func TestTrustedProxiesAblehnen(t *testing.T) {
	svc, holder := trustHolder(t)
	for _, bad := range []string{
		"nicht-ip", "10.0.0.0/33", "0.0.0.0/0", "::/0", "::ffff:0.0.0.0/96", "0.0.0.0/0 ",
	} {
		_, err := svc.UpdateGlobal(proxiesInput("10.0.0.5", bad), "admin")
		wantSettingInvalid(t, err, "trusted_proxies "+bad)
	}
	if holder.Load().Trusts(netip.MustParseAddr("10.0.0.5")) {
		t.Error("eine abgelehnte Liste wirkt trotzdem")
	}
}

// Eingeschaltet ohne Eintrag bewirkte nichts (LCM glaubte keinem Proxy): abgelehnt,
// ebenso das Leeren der Liste bei eingeschaltetem Schalter.
func TestTrustProxyHeaderBrauchtEintrag(t *testing.T) {
	svc, _ := trustHolder(t)
	on := true
	_, err := svc.UpdateGlobal(services.GlobalSettingsInput{TrustProxyHeader: &on}, "admin")
	wantSettingInvalid(t, err, "trust_proxy_header ohne Liste")

	list := []string{"127.0.0.1"}
	mustUpdate(t, svc, services.GlobalSettingsInput{TrustProxyHeader: &on, TrustedProxies: &list})
	_, err = svc.UpdateGlobal(proxiesInput(), "admin")
	wantSettingInvalid(t, err, "Liste leeren bei eingeschaltetem Schalter")

	off := false
	empty := []string{}
	mustUpdate(t, svc, services.GlobalSettingsInput{TrustProxyHeader: &off, TrustedProxies: &empty})
}

// Ein älterer Stand (Schalter an, Liste leer) blockiert andere Einstellungen nicht.
func TestTrustProxyHeaderAlterStandBlockiertNichts(t *testing.T) {
	svc, repo := newSettingsService(t)
	st, err := repo.Get()
	if err != nil {
		t.Fatal(err)
	}
	st.TrustProxyHeader = true
	if err := repo.Save(st); err != nil {
		t.Fatal(err)
	}
	user := "ops"
	mustUpdate(t, svc, services.GlobalSettingsInput{DefaultSSHUser: &user})
}

// Der Dienst schreibt die config.json nach jedem Speichern und meldet einen
// Fehler dabei in der Antwort, ohne das Speichern zurückzunehmen.
func TestSpeichernSyncMitConfigJson(t *testing.T) {
	svc, _ := newSettingsService(t)
	calls := 0
	svc.WithConfigSync(func() error { calls++; return nil })
	days := 30
	st, err := svc.UpdateGlobal(services.GlobalSettingsInput{LogRetentionDays: &days}, "admin")
	if err != nil || st.ConfigSyncError != "" || calls != 1 {
		t.Errorf("Aufrufe = %d, Fehler = %q, %v", calls, st.ConfigSyncError, err)
	}

	svc.WithConfigSync(func() error { return errTest })
	days = 31
	st, err = svc.UpdateGlobal(services.GlobalSettingsInput{LogRetentionDays: &days}, "admin")
	if err != nil || st.ConfigSyncError != errTest.Error() {
		t.Errorf("config_sync_error = %q", st.ConfigSyncError)
	}
	if got, _ := svc.Get(); got.LogRetentionDays != 31 {
		t.Errorf("die Änderung wurde zurückgenommen: %d", got.LogRetentionDays)
	}
}

// Teil-Updates einzelner Spalten (UpdateFields, etwa beim Schalten von MCP)
// lassen die Liste der Proxys stehen.
func TestTrustedProxiesUeberlebenTeilUpdates(t *testing.T) {
	svc, repo := newSettingsService(t)
	on, list := true, []string{"10.0.0.5"}
	mustUpdate(t, svc, services.GlobalSettingsInput{TrustProxyHeader: &on, TrustedProxies: &list})

	if err := repo.UpdateFields(map[string]any{"mcp_enabled": true, "allowed_nets_ui": "10.0.0.0/8"}); err != nil {
		t.Fatal(err)
	}
	st, err := svc.Get()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(st.TrustedProxies, ",") != "10.0.0.5/32" || !st.MCPEnabled {
		t.Errorf("trusted_proxies = %v, mcp = %v", st.TrustedProxies, st.MCPEnabled)
	}
}
