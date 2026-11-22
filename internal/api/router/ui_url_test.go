package router_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"gorm.io/gorm"

	"LCM/internal/api/router"
	"LCM/internal/core/services"
	"LCM/internal/infrastructure/crypto"
	"LCM/internal/storage/repositories"
)

// Das Ziel der Weiterleitung nach einem Portwechsel kommt aus den Einstellungen,
// wo sie die Adresse festlegen; sonst aus dem geprüften Namen der Anfrage, im
// Zweifel aus dem Rückfall - nie ungeprüft aus dem Host-Header.
func TestUIURLNachPortwechsel(t *testing.T) {
	app, _ := buildTestAppDB(t, 0, nil, func(d *router.Deps, db *gorm.DB) {
		cipher, err := crypto.NewCipher(crypto.GenerateKey())
		if err != nil {
			t.Fatal(err)
		}
		audit := services.NewAuditService(repositories.NewAuditRepository(db))
		d.Settings = services.NewSettingsService(repositories.NewSettingsRepository(db), cipher, audit, nil).
			WithAddressConfig(services.AddressConfig{Scheme: "https", FallbackHost: "localhost"})
	})
	token := loginToken(t, app, "admin", adminPassword)
	patch := func(host, body string) map[string]any {
		req := httptest.NewRequest("PATCH", "http://"+host+"/api/v1/settings/global", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-LCM-Session", token)
		resp, err := app.Test(req, testRequestConfig())
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != 200 {
			t.Fatalf("PATCH %s: %d", body, resp.StatusCode)
		}
		return decodeMap(t, resp)
	}

	// Keine Einstellung: der Name, unter dem der Browser kam, auf dem neuen Port.
	if got := patch("10.0.0.5:9310", `{"ui_port":9400}`)["ui_url"]; got != "http://10.0.0.5:9400" {
		t.Errorf("ui_url mit Namen der Anfrage = %v", got)
	}
	// Ein Name, der keiner ist: der Rückfall.
	if got := patch("-bad.example:9400", `{"ui_port":9401}`)["ui_url"]; got != "https://localhost:9401" {
		t.Errorf("ui_url mit ungültigem Host = %v, erwartet der Rückfall", got)
	}
	// Eine öffentliche Adresse legt das Ziel fest, egal was der Host-Header sagt.
	patch("10.0.0.5:9401", `{"public_base_url":"https://lcm.example.org"}`)
	if got := patch("angreifer.example:9401", `{"ui_port":9402}`)["ui_url"]; got != "https://lcm.example.org" {
		t.Errorf("ui_url mit öffentlicher Adresse = %v", got)
	}
	// Modus B: Name und Port aus den Einstellungen.
	got := patch("angreifer.example:9402", `{"address_mode":"one_name","public_host":"lcm.example.org","ui_port":9403}`)["ui_url"]
	if got != "https://lcm.example.org:9403" {
		t.Errorf("ui_url im Modus B = %v", got)
	}
}
