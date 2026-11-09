package router

import (
	"io"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/gofiber/fiber/v3"
)

func TestAcceptsEncoding(t *testing.T) {
	tests := []struct {
		header, token string
		want          bool
	}{
		{"gzip, deflate, br", "br", true},
		{"gzip, br;q=1.0", "br", true},
		{"gzip, br;q=0", "br", false},
		{"gzip", "br", false},
		{"", "gzip", false},
	}
	for _, tt := range tests {
		if got := acceptsEncoding(tt.header, tt.token); got != tt.want {
			t.Errorf("acceptsEncoding(%q, %q) = %v", tt.header, tt.token, got)
		}
	}
}

func TestPrecompressedAssets(t *testing.T) {
	files := fstest.MapFS{
		"assets/a.js":    {Data: []byte("raw")},
		"assets/a.js.br": {Data: []byte("brotli")},
		"assets/a.js.gz": {Data: []byte("gzip")},
	}
	app := fiber.New()
	app.Get("/*", precompressedAssets(files, func(c fiber.Ctx) error { return c.SendString("raw") }))

	tests := []struct{ accept, wantBody, wantEncoding string }{
		{"gzip, br", "brotli", "br"},
		{"gzip", "gzip", "gzip"},
		{"", "raw", ""},
	}
	for _, tt := range tests {
		req := httptest.NewRequest("GET", "/assets/a.js", nil)
		req.Header.Set("Accept-Encoding", tt.accept)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if string(body) != tt.wantBody || resp.Header.Get("Content-Encoding") != tt.wantEncoding {
			t.Errorf("Accept-Encoding %q: %q / %q", tt.accept, body, resp.Header.Get("Content-Encoding"))
		}
		if tt.wantEncoding != "" && resp.Header.Get("Cache-Control") != assetMaxAge {
			t.Errorf("Cache-Control fehlt: %q", resp.Header.Get("Cache-Control"))
		}
		if resp.Header.Get("Vary") != "Accept-Encoding" {
			t.Errorf("Vary fehlt: %q", resp.Header.Get("Vary"))
		}
	}
}
