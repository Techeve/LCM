package middlewares

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func etagApp() *fiber.App {
	app := fiber.New()
	app.Use(ETag())
	app.Get("/json", func(c fiber.Ctx) error { return c.JSON(fiber.Map{"a": 1}) })
	app.Get("/api/v1/events", func(c fiber.Ctx) error { return c.JSON(fiber.Map{"a": 1}) })
	app.Get("/text", func(c fiber.Ctx) error { return c.SendString("hallo") })
	app.Get("/fail", func(c fiber.Ctx) error { return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"a": 1}) })
	app.Post("/json", func(c fiber.Ctx) error { return c.JSON(fiber.Map{"a": 1}) })
	return app
}

func etagGet(t *testing.T, app *fiber.App, method, path, ifNoneMatch string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if ifNoneMatch != "" {
		req.Header.Set("If-None-Match", ifNoneMatch)
	}
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestETagSetztStarkesEtikettUndAntwortet304(t *testing.T) {
	app := etagApp()
	first := etagGet(t, app, "GET", "/json", "")
	tag := first.Header.Get("ETag")
	if first.StatusCode != 200 || len(tag) < 10 || tag[0] != '"' || tag[1] == 'W' {
		t.Fatalf("status %d, ETag %q: erwartet 200 und ein starkes Etikett", first.StatusCode, tag)
	}
	if again := etagGet(t, app, "GET", "/json", "").Header.Get("ETag"); again != tag {
		t.Errorf("gleicher Rumpf, anderes Etikett: %q und %q", tag, again)
	}

	for _, header := range []string{tag, "W/" + tag, `"anderes", ` + tag, "*"} {
		resp := etagGet(t, app, "GET", "/json", header)
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 304 || len(body) != 0 {
			t.Errorf("If-None-Match %q: status %d, %d Byte Rumpf, erwartet 304 ohne Rumpf", header, resp.StatusCode, len(body))
		}
		if resp.Header.Get("ETag") != tag {
			t.Errorf("If-None-Match %q: 304 ohne Etikett", header)
		}
	}

	resp := etagGet(t, app, "GET", "/json", `"veraltet"`)
	if body, _ := io.ReadAll(resp.Body); resp.StatusCode != 200 || len(body) == 0 {
		t.Errorf("fremdes Etikett: status %d, erwartet 200 mit Rumpf", resp.StatusCode)
	}
}

func TestETagLaesstAndereAntwortenUnberuehrt(t *testing.T) {
	app := etagApp()
	for _, tc := range []struct{ method, path string }{
		{"GET", "/text"}, {"GET", "/fail"}, {"GET", "/api/v1/events"}, {"POST", "/json"},
	} {
		resp := etagGet(t, app, tc.method, tc.path, "*")
		if resp.Header.Get("ETag") != "" || resp.StatusCode == 304 {
			t.Errorf("%s %s: ETag %q, status %d - hier darf es keins geben", tc.method, tc.path, resp.Header.Get("ETag"), resp.StatusCode)
		}
	}
}

func TestETagUeberspringtWebSocketAnfragen(t *testing.T) {
	req := httptest.NewRequest("GET", "/json", nil)
	req.Header.Set("Upgrade", "websocket")
	resp, err := etagApp().Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Header.Get("ETag") != "" {
		t.Errorf("ETag %q an einer Upgrade-Anfrage", resp.Header.Get("ETag"))
	}
}
