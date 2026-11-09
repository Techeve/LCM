package middlewares

import (
	"errors"
	"io"
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
