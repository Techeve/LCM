package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Erreichbarkeits-Check der zentralen CrowdSec-LAPI. Aufbau bewusst analog zum
// APT-Cache-Check (apt_cache.go): eine reine HTTP-Probe vom LCM-Host aus, die
// sowohl die On-Demand-Statusabfrage der CrowdSec-Seite als auch den
// crowdsec_lapi_down-Alarm speist.

// CrowdSecLapiStatus ist das Ergebnis des LAPI-Erreichbarkeits-Checks.
type CrowdSecLapiStatus struct {
	Configured bool   `json:"configured"`
	Reachable  bool   `json:"reachable"`
	Running    bool   `json:"running"` // Login akzeptiert → LAPI voll funktionsfähig
	HTTPStatus int    `json:"http_status,omitempty"`
	Message    string `json:"message"`
}

// crowdsecLapiHTTPTimeout: die LAPI steht im lokalen Netz (meist auf dem
// LCM-Host selbst) - antwortet sie nicht binnen weniger Sekunden, ist sie für
// die verwalteten Server faktisch nicht nutzbar.
const crowdsecLapiHTTPTimeout = 5 * time.Second

// probeCrowdSecLapi prüft die LAPI über ihren eigenen Login-Endpunkt
// (POST /v1/watchers/login) mit den hinterlegten Maschinen-Zugangsdaten.
// Das validiert nicht nur die TCP-Erreichbarkeit, sondern den kompletten
// Anmeldepfad, den auch die verwalteten Server im Remote-Modus nutzen.
func probeCrowdSecLapi(baseURL, login, password string) *CrowdSecLapiStatus {
	status := &CrowdSecLapiStatus{Configured: true}
	body, err := json.Marshal(map[string]any{
		"machine_id": login, "password": password, "scenarios": []string{},
	})
	if err != nil {
		status.Message = fmt.Sprintf("interner fehler: %v", err)
		return status
	}
	client := &http.Client{Timeout: crowdsecLapiHTTPTimeout}
	resp, err := client.Post(strings.TrimSuffix(baseURL, "/")+"/v1/watchers/login",
		"application/json", bytes.NewReader(body))
	if err != nil {
		status.Message = fmt.Sprintf("nicht erreichbar: %v", err)
		return status
	}
	defer resp.Body.Close()
	status.Reachable = true
	status.HTTPStatus = resp.StatusCode
	switch {
	case resp.StatusCode == http.StatusOK:
		status.Running = true
		status.Message = "CrowdSec-LAPI erreichbar, Login OK"
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		// Erreichbar, aber die Zugangsdaten stimmen nicht (mehr) - für die
		// verwalteten Server genauso ein Ausfall wie eine tote LAPI.
		status.Message = fmt.Sprintf("LAPI erreichbar, aber Login abgelehnt (HTTP %d) - Zugangsdaten prüfen", resp.StatusCode)
	default:
		status.Message = fmt.Sprintf("erreichbar, aber unerwartete Antwort (HTTP %d) - läuft dort die CrowdSec-LAPI?", resp.StatusCode)
	}
	return status
}

// CheckCrowdSecLapi prüft vom LCM-Host aus, ob die konfigurierte CrowdSec-LAPI
// erreichbar ist und die hinterlegten Maschinen-Zugangsdaten akzeptiert.
func (s *SettingsService) CheckCrowdSecLapi() (*CrowdSecLapiStatus, error) {
	settings, err := s.settings.Get()
	if err != nil {
		return nil, err
	}
	if !settings.CrowdSecLapiConfigured() {
		return &CrowdSecLapiStatus{Message: "keine CrowdSec-LAPI konfiguriert"}, nil
	}
	password, err := s.cipher.DecryptString(settings.CrowdSecLapiPasswordEnc)
	if err != nil {
		return nil, err
	}
	return probeCrowdSecLapi(settings.CrowdSecLapiURL, settings.CrowdSecLapiLogin, password), nil
}
