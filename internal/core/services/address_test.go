package services_test

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
)

func addressService(t *testing.T, cfg services.AddressConfig) *services.SettingsService {
	t.Helper()
	svc, _ := newSettingsService(t)
	return svc.WithAddressConfig(cfg)
}

func mustAddresses(t *testing.T, svc *services.SettingsService) services.Addresses {
	t.Helper()
	a, err := svc.Addresses()
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func mustUpdate(t *testing.T, svc *services.SettingsService, in services.GlobalSettingsInput) {
	t.Helper()
	if _, err := svc.UpdateGlobal(in, "admin"); err != nil {
		t.Fatalf("UpdateGlobal: %v", err)
	}
}

func wantSettingInvalid(t *testing.T, err error, what string) {
	t.Helper()
	if !errors.Is(err, services.ErrSettingInvalid) {
		t.Errorf("%s: Fehler = %v, erwartet ErrSettingInvalid", what, err)
	}
}

// Ohne jede Einstellung gilt der Stand vor dem Adressmodus: Modus A, die
// Vorgabe-Ports, nichts gesperrt.
func TestAddressesVorgaben(t *testing.T) {
	svc := addressService(t, services.AddressConfig{Scheme: "https", FallbackHost: "localhost"})
	a := mustAddresses(t, svc)

	if a.Mode != domain.AddressModeSeparate {
		t.Errorf("Modus = %q", a.Mode)
	}
	if a.UIPort != 9310 || a.AgentPort != 9320 || a.MCPPort != 9330 || a.SharedTLSPort != 443 {
		t.Errorf("Ports = %+v", a)
	}
	if a.Locks != (services.AddressLocks{}) {
		t.Errorf("Sperren = %+v, erwartet keine", a.Locks)
	}
}

// Modus A: die eingestellten Adressen, sonst Host der Oberfläche mit dem Port des Dienstes.
func TestAddressesModusA(t *testing.T) {
	svc := addressService(t, services.AddressConfig{Scheme: "https", FallbackHost: "localhost"})

	if got := mustAddresses(t, svc).URLs; got.UI != "https://localhost:9310" ||
		got.Agent != "https://localhost:9320" || got.MCP != "https://localhost:9330" {
		t.Errorf("ohne Adressen: %+v", got)
	}

	mustUpdate(t, svc, services.GlobalSettingsInput{
		PublicBaseURL: strPtr("https://lcm.example.org"), PublicMCPURL: strPtr("https://mcp.example.org"),
	})
	got := mustAddresses(t, svc).URLs
	if got.UI != "https://lcm.example.org" || got.MCP != "https://mcp.example.org" {
		t.Errorf("eingestellte Adressen: %+v", got)
	}
	// Der Agent hat keine eigene Adresse: Host der Oberfläche, Agent-Port.
	if got.Agent != "https://lcm.example.org:9320" {
		t.Errorf("Agent ohne eigene Adresse = %q", got.Agent)
	}
}

// Modus B: https://<public_host>:<port> für alle drei.
func TestAddressesModusB(t *testing.T) {
	svc := addressService(t, services.AddressConfig{Scheme: "https", FallbackHost: "localhost"})
	mustUpdate(t, svc, services.GlobalSettingsInput{
		AddressMode: strPtr("one_name"), PublicHost: strPtr("LCM.Example.org."),
		UIPort: intPtr(9400), AgentPort: intPtr(9401), MCPPort: intPtr(9402),
	})

	got := mustAddresses(t, svc).URLs
	want := services.EffectiveURLs{
		UI: "https://lcm.example.org:9400", Agent: "https://lcm.example.org:9401", MCP: "https://lcm.example.org:9402", // MCP immer mit TLS
	}
	if got != want {
		t.Errorf("URLs = %+v, erwartet %+v", got, want)
	}
	if base := svc.LinkBaseURL(); base != want.UI {
		t.Errorf("LinkBaseURL = %q: Mail-Links müssen die abgeleitete Adresse tragen", base)
	}
	if agent := svc.PublicAgentURL(); agent != want.Agent {
		t.Errorf("PublicAgentURL = %q", agent)
	}
}

// Modus C: https://<name>, :port nur, wenn der gemeinsame Port nicht 443 ist.
func TestAddressesModusC(t *testing.T) {
	svc := addressService(t, services.AddressConfig{Scheme: "https", FallbackHost: "localhost"})
	mustUpdate(t, svc, services.GlobalSettingsInput{
		AddressMode:   strPtr("shared_port"),
		PublicBaseURL: strPtr("https://lcm.example.org:9310"),
		// Ein alter Port in der Adresse zählt im Modus C nicht.
		PublicAgentURL: strPtr("https://agent.example.org:9320"),
		PublicMCPURL:   strPtr("https://mcp.example.org"),
	})
	got := mustAddresses(t, svc).URLs
	want := services.EffectiveURLs{UI: "https://lcm.example.org", Agent: "https://agent.example.org", MCP: "https://mcp.example.org"}
	if got != want {
		t.Errorf("Port 443: %+v, erwartet %+v", got, want)
	}

	mustUpdate(t, svc, services.GlobalSettingsInput{SharedTLSPort: intPtr(8443)})
	if got := mustAddresses(t, svc).URLs; got.UI != "https://lcm.example.org:8443" || got.MCP != "https://mcp.example.org:8443" {
		t.Errorf("Port 8443: %+v", got)
	}
}

func TestAddressesDevModusHatHttp(t *testing.T) {
	svc := addressService(t, services.AddressConfig{Scheme: "http", FallbackHost: "localhost"})
	if got := mustAddresses(t, svc).URLs.UI; got != "http://localhost:9310" {
		t.Errorf("UI im Entwicklungsmodus = %q", got)
	}
}

func TestAddressesIPv6Fallback(t *testing.T) {
	svc := addressService(t, services.AddressConfig{Scheme: "https", FallbackHost: "::1"})
	if got := mustAddresses(t, svc).URLs.UI; got != "https://[::1]:9310" {
		t.Errorf("UI mit IPv6 = %q", got)
	}
}

// Der Modus B verlangt genau einen Namen, ohne Schema, Port und Pfad.
func TestValidierungModusB(t *testing.T) {
	svc := addressService(t, services.AddressConfig{Scheme: "https", FallbackHost: "localhost"})

	for _, host := range []string{"https://lcm.example.org", "lcm.example.org:9310", "lcm.example.org/x", "a b", "user@lcm.example.org", "-.example.org"} {
		_, err := svc.UpdateGlobal(services.GlobalSettingsInput{AddressMode: strPtr("one_name"), PublicHost: strPtr(host)}, "admin")
		wantSettingInvalid(t, err, "public_host "+host)
	}
	// Ohne Namen kein Modus B.
	_, err := svc.UpdateGlobal(services.GlobalSettingsInput{AddressMode: strPtr("one_name")}, "admin")
	wantSettingInvalid(t, err, "Modus B ohne Namen")
	if mustAddresses(t, svc).Mode != domain.AddressModeSeparate {
		t.Error("ein abgelehnter Modus wurde gespeichert")
	}
	_, err = svc.UpdateGlobal(services.GlobalSettingsInput{AddressMode: strPtr("beides")}, "admin")
	wantSettingInvalid(t, err, "unbekannter Modus")

	mustUpdate(t, svc, services.GlobalSettingsInput{AddressMode: strPtr("one_name"), PublicHost: strPtr("192.0.2.10")})
}

// Der Modus C braucht den Namen der Oberfläche; Agent und MCP dürfen leer
// bleiben, gesetzte Namen sind paarweise verschieden - auch für ein ausgeschaltetes MCP.
func TestValidierungModusC(t *testing.T) {
	svc := addressService(t, services.AddressConfig{Scheme: "https", FallbackHost: "localhost"})
	shared := strPtr("shared_port")

	_, err := svc.UpdateGlobal(services.GlobalSettingsInput{
		AddressMode: shared, PublicAgentURL: strPtr("https://agent.example.org"),
	}, "admin")
	wantSettingInvalid(t, err, "Name der Oberfläche fehlt")

	_, err = svc.UpdateGlobal(services.GlobalSettingsInput{
		AddressMode: shared, PublicBaseURL: strPtr("https://lcm.example.org"),
		PublicAgentURL: strPtr("https://agent.example.org"), PublicMCPURL: strPtr("https://LCM.example.org:9330"),
	}, "admin")
	var conflict *services.PublicHostConflict
	if !errors.As(err, &conflict) || conflict.First != "UI" || conflict.Second != "MCP" {
		t.Errorf("gleicher Name: %v, erwartet Konflikt UI/MCP", err)
	}
	if mustAddresses(t, svc).Mode != domain.AddressModeSeparate {
		t.Error("ein abgelehnter Modus wurde gespeichert")
	}

	// Ohne Namen für MCP geht es: MCP ist dann aus.
	mustUpdate(t, svc, services.GlobalSettingsInput{
		AddressMode: shared, PublicBaseURL: strPtr("https://lcm.example.org"), PublicAgentURL: strPtr("https://agent.example.org"),
	})
	mustUpdate(t, svc, services.GlobalSettingsInput{
		AddressMode: shared, PublicBaseURL: strPtr("https://lcm.example.org"),
		PublicAgentURL: strPtr("https://agent.example.org"), PublicMCPURL: strPtr("https://mcp.example.org"),
	})
}

// Die Ports: gültiger Bereich, paarweise verschieden, nicht auf dem Port der
// Let's-Encrypt-Prüfung; der gemeinsame Port zählt nur im Modus C.
func TestValidierungPorts(t *testing.T) {
	svc := addressService(t, services.AddressConfig{Scheme: "https", FallbackHost: "localhost", ACMEHTTPPort: 80})

	for name, in := range map[string]services.GlobalSettingsInput{
		"Port 0":           {UIPort: intPtr(0)},
		"Port 70000":       {AgentPort: intPtr(70000)},
		"UI = Agent":       {UIPort: intPtr(9320)},
		"MCP = UI":         {MCPPort: intPtr(9310)},
		"UI auf Port 80":   {UIPort: intPtr(80)},
		"Agent = MCP":      {AgentPort: intPtr(9330)},
		"negativer Port":   {MCPPort: intPtr(-1)},
		"gleichzeitig dop": {UIPort: intPtr(9500), AgentPort: intPtr(9500)},
	} {
		_, err := svc.UpdateGlobal(in, "admin")
		wantSettingInvalid(t, err, name)
	}
	if a := mustAddresses(t, svc); a.UIPort != 9310 || a.AgentPort != 9320 || a.MCPPort != 9330 {
		t.Errorf("abgelehnte Ports wurden gespeichert: %+v", a)
	}

	// In A und B darf die Oberfläche selbst auf 443 lauschen: Der gemeinsame
	// Port ist dort nicht in Betrieb.
	mustUpdate(t, svc, services.GlobalSettingsInput{UIPort: intPtr(443)})

	// Im Modus C ist 443 der gemeinsame Port und nicht mehr frei.
	_, err := svc.UpdateGlobal(services.GlobalSettingsInput{
		AddressMode: strPtr("shared_port"), PublicBaseURL: strPtr("https://lcm.example.org"),
		PublicAgentURL: strPtr("https://agent.example.org"), PublicMCPURL: strPtr("https://mcp.example.org"),
	}, "admin")
	wantSettingInvalid(t, err, "Modus C mit UI auf dem gemeinsamen Port")
}

// Ein Stand von früher blockiert das Speichern anderer Felder nicht.
func TestValidierungNurBeiAdressaenderung(t *testing.T) {
	svc, repo := newSettingsService(t)
	st, _ := repo.Get()
	st.UIPort, st.AgentPort = 9400, 9400 // doppelt vergeben, von Hand in die DB gekommen
	if err := repo.Save(st); err != nil {
		t.Fatal(err)
	}
	mustUpdate(t, svc, services.GlobalSettingsInput{DefaultTimezone: strPtr("Europe/Berlin")})
}

// Ein Port aus der Umgebung gilt statt dem der Datenbank, ist gesperrt, und
// ein anderer Wert wird abgelehnt - derselbe Wert nicht.
func TestSperreDurchUmgebung(t *testing.T) {
	svc := addressService(t, services.AddressConfig{
		Scheme: "https", FallbackHost: "localhost",
		UI: services.FixedPort{Set: true, Value: 9500},
	})
	a := mustAddresses(t, svc)
	if a.UIPort != 9500 || !a.Locks.UIPort || a.Locks.AgentPort {
		t.Errorf("Stand = %+v", a)
	}
	if a.URLs.UI != "https://localhost:9500" {
		t.Errorf("die Adresse trägt den Port der Umgebung nicht: %q", a.URLs.UI)
	}

	_, err := svc.UpdateGlobal(services.GlobalSettingsInput{UIPort: intPtr(9501)}, "admin")
	wantSettingInvalid(t, err, "gesperrter Port")
	mustUpdate(t, svc, services.GlobalSettingsInput{UIPort: intPtr(9500), AgentPort: intPtr(9321)})
	if got := mustAddresses(t, svc); got.AgentPort != 9321 || got.UIPort != 9500 {
		t.Errorf("Stand danach = %+v", got)
	}
}

// Ein abgeschalteter Agent-Listener (LCM_AGENT_PORT=0) ist gültig und hat keine Adresse.
func TestAgentListenerAusDurchUmgebung(t *testing.T) {
	svc := addressService(t, services.AddressConfig{
		Scheme: "https", FallbackHost: "localhost", Agent: services.FixedPort{Set: true, Value: 0},
	})
	a := mustAddresses(t, svc)
	if a.AgentPort != 0 || a.URLs.Agent != "" {
		t.Errorf("Agent aus: %+v", a)
	}
	mustUpdate(t, svc, services.GlobalSettingsInput{UIPort: intPtr(9400)})
	if svc.PublicAgentURL() != "" {
		t.Error("PublicAgentURL ohne Agent-Listener ist nicht leer")
	}
}

// Der Mail-Link-Rückfall bleibt der lokale Host, solange nichts gesetzt ist -
// und kommt nie aus einer Anfrage (siehe LinkBaseURL).
func TestLinkBaseURLRueckfall(t *testing.T) {
	svc := addressService(t, services.AddressConfig{Scheme: "https", FallbackHost: "localhost"})
	if got := svc.LinkBaseURL(); got != "https://localhost:9310" {
		t.Errorf("LinkBaseURL = %q", got)
	}
	mustUpdate(t, svc, services.GlobalSettingsInput{PublicBaseURL: strPtr("https://lcm.example.org")})
	if got := svc.LinkBaseURL(); got != "https://lcm.example.org" {
		t.Errorf("LinkBaseURL = %q", got)
	}
}

// Ohne abgeleitete Adresse der Oberfläche (Modus C ohne Namen, etwa aus einem
// Stand von früher) bleibt der Mail-Link absolut.
func TestLinkBaseURLOhneAbgeleiteteAdresse(t *testing.T) {
	svc, repo := newSettingsService(t)
	svc.WithAddressConfig(services.AddressConfig{
		Scheme: "https", FallbackHost: "localhost", UI: services.FixedPort{Set: true, Value: 9444},
	})
	st, err := repo.Get()
	if err != nil {
		t.Fatal(err)
	}
	st.AddressMode = domain.AddressModeSharedPort
	if err := repo.Save(st); err != nil {
		t.Fatal(err)
	}
	if got := mustAddresses(t, svc).URLs.UI; got != "" {
		t.Fatalf("Voraussetzung: UI-Adresse = %q, erwartet leer", got)
	}
	if got := svc.LinkBaseURL(); got != "https://localhost:9444" {
		t.Errorf("LinkBaseURL = %q, erwartet der Rückfall mit dem wirksamen UI-Port", got)
	}
}

// Modus B: Schema, MCP mit TLS und ein abgeschalteter Agent wie im Modus A.
func TestAddressesModusBSchemaMCPAgent(t *testing.T) {
	svc := addressService(t, services.AddressConfig{
		Scheme: "https", FallbackHost: "localhost", Agent: services.FixedPort{Set: true, Value: 0},
	})
	mustUpdate(t, svc, services.GlobalSettingsInput{
		AddressMode: strPtr("one_name"), PublicHost: strPtr("lcm.example.org"),
	})
	got := mustAddresses(t, svc).URLs
	want := services.EffectiveURLs{UI: "https://lcm.example.org:9310", MCP: "https://lcm.example.org:9330"}
	if got != want {
		t.Errorf("URLs = %+v, erwartet %+v", got, want)
	}

	dev := addressService(t, services.AddressConfig{Scheme: "http", FallbackHost: "localhost"})
	mustUpdate(t, dev, services.GlobalSettingsInput{AddressMode: strPtr("one_name"), PublicHost: strPtr("lcm.example.org")})
	if got := mustAddresses(t, dev).URLs; got.UI != "http://lcm.example.org:9310" || got.Agent != "http://lcm.example.org:9320" {
		t.Errorf("Entwicklungsmodus: %+v", got)
	}
}

// Der MCP-Port steht in den Adressen und wird gegen die übrigen Ports geprüft.
func TestMCPPortKollision(t *testing.T) {
	svc := addressService(t, services.AddressConfig{Scheme: "https", FallbackHost: "localhost"})
	_, err := svc.UpdateGlobal(services.GlobalSettingsInput{MCPPort: intPtr(9310)}, "admin")
	wantSettingInvalid(t, err, "MCP auf dem Port der Oberfläche")
	mustUpdate(t, svc, services.GlobalSettingsInput{MCPPort: intPtr(9331)})
	if err := svc.SetMCP(true, "admin"); err != nil {
		t.Errorf("MCP einschalten: %v", err)
	}
}

// fakeApplier merkt sich, was der Service vorbereitet, übernimmt und zurückrollt.
type fakeApplier struct {
	plans              []services.AddressPlan
	err                error
	committed, aborted int
}

func (f *fakeApplier) Prepare(plan services.AddressPlan) (services.AddressChange, error) {
	f.plans = append(f.plans, plan)
	if f.err != nil {
		return nil, f.err
	}
	return f, nil
}
func (f *fakeApplier) Commit()   { f.committed++ }
func (f *fakeApplier) Rollback() { f.aborted++ }

// Ein neuer Port wird vor dem Speichern gebunden; erst nach dem Speichern gilt er.
func TestPortWechselBindetVorDemSpeichern(t *testing.T) {
	svc := addressService(t, services.AddressConfig{Scheme: "https", FallbackHost: "localhost"})
	applier := &fakeApplier{}
	svc.WithAddressApplier(applier)

	mustUpdate(t, svc, services.GlobalSettingsInput{UIPort: intPtr(9400), AgentPort: intPtr(9401)})
	if len(applier.plans) != 1 || applier.committed != 1 || applier.aborted != 0 {
		t.Fatalf("Aufrufe: %d Pläne, %d übernommen, %d zurückgerollt", len(applier.plans), applier.committed, applier.aborted)
	}
	if plan := applier.plans[0]; plan.UIPort != 9400 || plan.AgentPort != 9401 || plan.MCPPort != 9330 || plan.SharedTLSPort != 0 {
		t.Errorf("Plan = %+v", plan)
	}

	// Eine Änderung ohne Wirkung auf die Listener fasst sie nicht an.
	mustUpdate(t, svc, services.GlobalSettingsInput{DefaultTimezone: strPtr("Europe/Berlin")})
	mustUpdate(t, svc, services.GlobalSettingsInput{UIPort: intPtr(9400)})
	if len(applier.plans) != 1 {
		t.Errorf("unveränderte Ports lösten %d Pläne aus", len(applier.plans))
	}
}

// Ein Port, den ein anderer Dienst noch hält, wird nicht gebunden: Ein Tausch
// (und eine Kette) scheitert mit einer klaren Meldung statt am Bind.
func TestPortTauschWirdAbgelehnt(t *testing.T) {
	svc := addressService(t, services.AddressConfig{Scheme: "https", FallbackHost: "localhost"})
	applier := &fakeApplier{}
	svc.WithAddressApplier(applier)

	_, err := svc.UpdateGlobal(services.GlobalSettingsInput{UIPort: intPtr(9320), AgentPort: intPtr(9310)}, "admin")
	wantSettingInvalid(t, err, "Tausch von Oberfläche und Agent")
	if err == nil || !strings.Contains(err.Error(), "nicht tauschen") {
		t.Errorf("Meldung = %v, erwartet den Hinweis, nicht zu tauschen", err)
	}
	_, err = svc.UpdateGlobal(services.GlobalSettingsInput{UIPort: intPtr(9320), AgentPort: intPtr(9340)}, "admin")
	wantSettingInvalid(t, err, "Kette: die Oberfläche nimmt den alten Agent-Port")
	if len(applier.plans) != 0 {
		t.Errorf("trotz Ablehnung gebunden: %d Pläne", len(applier.plans))
	}
	// MCP ist aus: Sein Port ist nicht belegt und darf der Oberfläche gehören.
	mustUpdate(t, svc, services.GlobalSettingsInput{UIPort: intPtr(9330), MCPPort: intPtr(9331)})
}

// Lässt sich der neue Port nicht öffnen, bleibt alles beim Alten: Fehler an den
// Aufrufer, nichts gespeichert.
func TestPortWechselScheitertAmBindenSpeichertNichts(t *testing.T) {
	svc := addressService(t, services.AddressConfig{Scheme: "https", FallbackHost: "localhost"})
	applier := &fakeApplier{err: errors.New("address already in use")}
	svc.WithAddressApplier(applier)

	_, err := svc.UpdateGlobal(services.GlobalSettingsInput{UIPort: intPtr(9400)}, "admin")
	if !errors.Is(err, services.ErrPortUnavailable) || !errors.Is(err, services.ErrSettingInvalid) {
		t.Fatalf("Fehler = %v, erwartet ErrPortUnavailable (4xx)", err)
	}
	if a := mustAddresses(t, svc); a.UIPort != 9310 {
		t.Errorf("der Port wurde trotz Fehler gespeichert: %d", a.UIPort)
	}
	if applier.committed != 0 {
		t.Error("die Listener wurden trotz Fehler umgestellt")
	}
}

// Der Wechsel in den Modus C öffnet den gemeinsamen Port, der zurück schließt ihn.
func TestModusWechselOeffnetUndSchliesstGemeinsamenPort(t *testing.T) {
	svc := addressService(t, services.AddressConfig{Scheme: "https", FallbackHost: "localhost"})
	applier := &fakeApplier{}
	svc.WithAddressApplier(applier)

	mustUpdate(t, svc, services.GlobalSettingsInput{
		AddressMode: strPtr("shared_port"), PublicBaseURL: strPtr("https://lcm.example.org"),
		PublicAgentURL: strPtr("https://agent.example.org"), PublicMCPURL: strPtr("https://mcp.example.org"),
	})
	mustUpdate(t, svc, services.GlobalSettingsInput{AddressMode: strPtr("separate")})

	if len(applier.plans) != 2 || applier.plans[0].SharedTLSPort != 443 || applier.plans[1].SharedTLSPort != 0 {
		t.Errorf("Pläne = %+v", applier.plans)
	}
}

// Gleichzeitige PATCHes überschreiben sich nicht: Der Stand, den ein PATCH
// speichert, ist der nach dem vorigen.
func TestUpdateGlobalGleichzeitigVerliertNichts(t *testing.T) {
	svc := addressService(t, services.AddressConfig{Scheme: "https", FallbackHost: "localhost"})
	const rounds = 25
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := range rounds {
			mustUpdate(t, svc, services.GlobalSettingsInput{UIPort: intPtr(9400 + i)})
		}
	}()
	go func() {
		defer wg.Done()
		for i := range rounds {
			zone := "UTC"
			if i == rounds-1 {
				zone = "Europe/Berlin"
			}
			mustUpdate(t, svc, services.GlobalSettingsInput{DefaultTimezone: strPtr(zone)})
		}
	}()
	wg.Wait()

	st, err := svc.Get()
	if err != nil {
		t.Fatal(err)
	}
	if st.UIPort != 9400+rounds-1 || st.DefaultTimezone != "Europe/Berlin" {
		t.Errorf("Stand = Port %d, Zeitzone %q: ein PATCH hat den anderen überschrieben", st.UIPort, st.DefaultTimezone)
	}
}

// Eine Agent-Adresse ohne Port zeigt auf 443. Im Modus A lauscht LCM dort
// nicht - der Enroll-Befehl braucht dann einen Reverse Proxy, und die
// Oberfläche warnt. Mit Port, im Modus C oder ohne eigene Adresse nicht.
func TestAgentURLNeedsProxy(t *testing.T) {
	svc := addressService(t, services.AddressConfig{Scheme: "https", FallbackHost: "localhost"})
	if svc.AgentURLNeedsProxy() {
		t.Error("ohne Agent-Adresse: Warnung, erwartet keine")
	}

	mustUpdate(t, svc, services.GlobalSettingsInput{PublicAgentURL: strPtr("https://lcm.example.org")})
	if !svc.AgentURLNeedsProxy() {
		t.Error("Modus A, Adresse ohne Port: keine Warnung")
	}

	mustUpdate(t, svc, services.GlobalSettingsInput{PublicAgentURL: strPtr("https://lcm.example.org:9320")})
	if svc.AgentURLNeedsProxy() {
		t.Error("Modus A, Adresse mit Port: Warnung, erwartet keine")
	}

	mustUpdate(t, svc, services.GlobalSettingsInput{
		AddressMode:    strPtr(domain.AddressModeSharedPort),
		PublicBaseURL:  strPtr("https://ui.example.org"),
		PublicAgentURL: strPtr("https://agent.example.org"),
		PublicMCPURL:   strPtr("https://mcp.example.org"),
	})
	if svc.AgentURLNeedsProxy() {
		t.Error("Modus C: Warnung, erwartet keine")
	}
}
