package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"LCM/internal/infrastructure/tlsx"
)

// fetchTimeout: Antwortet LCM nicht binnen zehn Sekunden, gilt es für diese
// Minute als nicht erreichbar - genau das soll in der Zeile stehen.
const fetchTimeout = 10 * time.Second

// maxSnapshot begrenzt die Antwort; eine Momentaufnahme hat einige zehn KB.
const maxSnapshot = 8 << 20

// lcmClient holt die Momentaufnahme von LCM.
type lcmClient struct {
	base    string
	keyFile string
	dataDir string
}

func newLCMClient(cfg runConfig) *lcmClient {
	base := cfg.url
	if base == "" {
		base = fmt.Sprintf("https://127.0.0.1:%d", lcmPort(cfg.lcmConfig))
	}
	return &lcmClient{base: strings.TrimSuffix(base, "/"), keyFile: cfg.keyFile, dataDir: cfg.lcmData}
}

// lcmPort liest den Port aus der Konfiguration von LCM, Vorgabe 9310.
func lcmPort(path string) int {
	var cfg struct {
		Port int `json:"port"`
	}
	if b, err := os.ReadFile(path); err == nil && json.Unmarshal(b, &cfg) == nil && cfg.Port > 0 {
		return cfg.Port
	}
	return 9310
}

type fetchResult struct {
	at   time.Time
	took float64 // Millisekunden
	body []byte
	err  error
}

// pollLCM holt im Takt eine Momentaufnahme und reicht sie weiter.
func pollLCM(ctx context.Context, c *lcmClient, every time.Duration, out chan<- fetchResult) {
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		started := time.Now()
		body, err := c.fetch(ctx)
		res := fetchResult{at: started, took: float64(time.Since(started).Microseconds()) / 1000, body: body, err: err}
		select {
		case out <- res:
		case <-ctx.Done():
			return
		}
		select {
		case <-tick.C:
		case <-ctx.Done():
			return
		}
	}
}

// errNoKey: Ohne Schlüssel fragt lcm-probe nicht. Das ist kein Ausfall von
// LCM und steht deshalb nicht als lcm_down in der Datei.
var errNoKey = errors.New("kein API-Schlüssel")

// fetch fragt GET /api/v1/system/db ab. Wie der Healthcheck von LCM: HTTPS
// gegen das Zertifikat aus dem Datenverzeichnis, HTTP nur, wenn LCM mit --dev
// läuft. Der Schlüssel wird jedes Mal neu gelesen, damit ein nachträglich
// hinterlegter ohne Neustart greift.
//
// Der Schlüssel liest als Administrator alles. Er geht deshalb nur an eine
// Gegenstelle, die das Zertifikat aus dem Datenverzeichnis vorzeigt, oder an
// den eigenen Rechner (Loopback); Klartext nur an den eigenen Rechner.
func (c *lcmClient) fetch(ctx context.Context) ([]byte, error) {
	raw, err := os.ReadFile(c.keyFile)
	key := strings.TrimSpace(string(raw))
	if err != nil || key == "" {
		return nil, errNoKey
	}
	loopback := isLoopback(c.base)
	if !loopback && !c.hasCert() {
		return nil, fmt.Errorf("%s ist nicht der eigene Rechner und %s fehlt: schlüssel wird nicht gesendet", c.base, tlsx.CertFileName)
	}
	body, err := c.get(ctx, c.base, key)
	if err != nil && loopback && tlsx.IsNotTLS(err) && strings.HasPrefix(c.base, "https://") {
		return c.get(ctx, "http://"+strings.TrimPrefix(c.base, "https://"), key)
	}
	return body, err
}

func (c *lcmClient) hasCert() bool {
	_, err := os.Stat(filepath.Join(c.dataDir, tlsx.CertFileName))
	return err == nil
}

// isLoopback sagt, ob die Adresse auf den eigenen Rechner zeigt.
func isLoopback(base string) bool {
	u, err := url.Parse(base)
	if err != nil {
		return false
	}
	if u.Hostname() == "localhost" {
		return true
	}
	ip := net.ParseIP(u.Hostname())
	return ip != nil && ip.IsLoopback()
}

// get holt die Antwort über eine Verbindung, die danach geschlossen wird: Ein
// Abruf je Minute braucht kein Keep-Alive, und das Zertifikat wird so jedes
// Mal frisch gelesen.
func (c *lcmClient) get(ctx context.Context, base, key string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/v1/system/db", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-API-Key", key)
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: tlsx.PinnedTo(c.dataDir), DisableKeepAlives: true}}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSnapshot))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %.200s", resp.StatusCode, body)
	}
	if !json.Valid(body) {
		return nil, errors.New("antwort ist kein JSON")
	}
	return body, nil
}
