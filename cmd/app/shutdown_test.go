package main

import (
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
)

func TestPortCloserSchliesstAlles(t *testing.T) {
	var closer portCloser
	var closed []string
	closer.Add(func() { closed = append(closed, "ui") })
	closer.Add(func() { closed = append(closed, "gemeinsam") })
	closer.CloseAll()
	if len(closed) != 2 || closed[0] != "ui" {
		t.Errorf("geschlossen: %v", closed)
	}
}

// Eine hängende Anfrage hält das Beenden nicht länger als die Frist auf.
func TestShutdownAppsHaeltNichtAufEineHaengendeAnfrage(t *testing.T) {
	hang := make(chan struct{})
	t.Cleanup(func() { close(hang) })
	app := fiber.New()
	app.Get("/", func(c fiber.Ctx) error {
		<-hang
		return nil
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = app.Listener(ln, fiber.ListenConfig{DisableStartupMessage: true}) }()
	go func() {
		client := http.Client{Timeout: 5 * time.Second}
		if resp, err := client.Get("http://" + ln.Addr().String() + "/"); err == nil {
			_ = resp.Body.Close()
		}
	}()
	time.Sleep(300 * time.Millisecond) // die Anfrage ist im Handler

	start := time.Now()
	shutdownApps(300*time.Millisecond, app, nil)
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("Beenden dauerte %v trotz Frist von 300 ms", elapsed)
	}
}
