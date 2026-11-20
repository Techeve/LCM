package middlewares

import (
	"bytes"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"

	"LCM/internal/api/apierr"
)

func guardApp() *fiber.App {
	app := fiber.New(fiber.Config{ErrorHandler: func(c fiber.Ctx, err error) error {
		var coded *apierr.Error
		if !errors.As(err, &coded) {
			return err
		}
		return c.Status(coded.Status).SendString(coded.Code)
	}})
	app.Use(InputGuard())
	app.All("/*", func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusNoContent) })
	return app
}

func guarded(t *testing.T, method, target, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := guardApp().Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	text, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(text)
}

func TestInputGuardRejectsSQLEverywhere(t *testing.T) {
	tests := []struct{ name, target, body string }{
		{"Feld", "/x", `{"name":"' OR '1'='1"}`},
		{"verschachtelt", "/x", `{"rules":[{"comment":"x'; DROP TABLE users; --"}]}`},
		{"Liste", "/x", `{"names":["ok","1 UNION SELECT 1"]}`},
		{"Abfrage", "/x?q=%27%20OR%201%3D1--", ""},
		{"Pfad", "/x/1%20UNION%20SELECT%201", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, text := guarded(t, http.MethodPost, tt.target, tt.body)
			if status == fiber.StatusNoContent {
				t.Fatalf("durchgelassen: %s", text)
			}
		})
	}
}

func TestInputGuardLeavesFreeFieldsAlone(t *testing.T) {
	for _, body := range []string{
		`{"password":"' OR '1'='1"}`,
		`{"command":"mysql -e 'DROP TABLE x; --'"}`,
		`{"sudo_rules":[{"command":"/usr/bin/psql -c 'select 1; drop table x'"}]}`,
		`{"name":"O'Brien","description":"select the servers from the list"}`,
		`{"port":22,"priority":-1,"enabled":true,"list":[1,2,3]}`,
		`kein json`,
	} {
		if status, text := guarded(t, http.MethodPost, "/x", body); status != fiber.StatusNoContent {
			t.Errorf("%s abgelehnt: %d %s", body, status, text)
		}
	}
}

func TestInputGuardChecksNumbers(t *testing.T) {
	for _, target := range []string{"/x?limit=abc", "/x?limit=-1", "/x?limit=99999", "/x?page=0", "/x?page_size=1e3", "/x?offset=1.5"} {
		if status, _ := guarded(t, http.MethodGet, target, ""); status != fiber.StatusUnprocessableEntity {
			t.Errorf("%s: %d", target, status)
		}
	}
	if status, _ := guarded(t, http.MethodGet, "/x?limit=50&offset=0&page=1", ""); status != fiber.StatusNoContent {
		t.Errorf("gültige Zahlen abgelehnt: %d", status)
	}
	if status, _ := guarded(t, http.MethodPost, "/x", `{"server_id":99999999999999999999}`); status != fiber.StatusUnprocessableEntity {
		t.Errorf("riesige ID durchgelassen: %d", status)
	}
}

// TestInputGuardChecksEveryOccurrence: Ein Parameter, der mehrfach vorkommt,
// wird in JEDEM Vorkommen geprüft - ein harmloser erster Wert verdeckt keinen
// späteren (HTTP Parameter Pollution).
func TestInputGuardChecksEveryOccurrence(t *testing.T) {
	for _, target := range []string{
		"/x?q=harmlos&q=%27%20OR%201%3D1--",
		"/x?q=%27%20OR%201%3D1--&q=harmlos",
	} {
		if status, _ := guarded(t, http.MethodGet, target, ""); status != fiber.StatusUnprocessableEntity {
			t.Errorf("%s: %d, erwartet 422", target, status)
		}
	}
	if status, _ := guarded(t, http.MethodGet, "/x?limit=10&limit=abc", ""); status != fiber.StatusUnprocessableEntity {
		t.Errorf("zweite Zahl ungeprüft: %d", status)
	}
}

// TestInputGuardChecksFormBody: Ein Formular-Rumpf landet über c.Bind().Body
// in denselben Feldern wie JSON und wird genauso geprüft.
func TestInputGuardChecksFormBody(t *testing.T) {
	send := func(body string) int {
		req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err := guardApp().Test(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode
	}
	if status := send("name=web01&name=%27%20OR%20%271%27%3D%271"); status != fiber.StatusUnprocessableEntity {
		t.Errorf("SQL im Formular durchgelassen: %d", status)
	}
	if status := send("name=web01&password=%27%20OR%20%271%27%3D%271"); status != fiber.StatusNoContent {
		t.Errorf("freies Feld im Formular abgelehnt: %d", status)
	}
}

// TestInputGuardLiestDenMedientypWieFiber: Fiber bindet „Application/JSON",
// Herstellertypen mit +json und Multipart wie JSON - die Prüfung muss sie
// genauso lesen. Formate, die sie nicht liest, lehnt sie ab.
func TestInputGuardLiestDenMedientypWieFiber(t *testing.T) {
	sqlValue := `' OR '1'='1`
	send := func(contentType, body string) int {
		req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(body))
		req.Header.Set("Content-Type", contentType)
		resp, err := guardApp().Test(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode
	}
	jsonBody := `{"name":"` + sqlValue + `"}`
	for _, contentType := range []string{"Application/JSON", "application/vnd.api+json", "application/json; charset=utf-8"} {
		if status := send(contentType, jsonBody); status != fiber.StatusUnprocessableEntity {
			t.Errorf("%s: SQL durchgelassen (%d)", contentType, status)
		}
	}

	var multipartBody bytes.Buffer
	form := multipart.NewWriter(&multipartBody)
	_ = form.WriteField("name", sqlValue)
	_ = form.Close()
	if status := send(form.FormDataContentType(), multipartBody.String()); status != fiber.StatusUnprocessableEntity {
		t.Errorf("multipart: SQL durchgelassen (%d)", status)
	}

	for _, contentType := range []string{"application/xml", "text/xml", "application/vnd.msgpack", "application/cbor"} {
		if status := send(contentType, "<x/>"); status != fiber.StatusUnsupportedMediaType {
			t.Errorf("%s: nicht abgelehnt (%d)", contentType, status)
		}
	}
}
