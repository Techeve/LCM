package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/trivy"
	"LCM/internal/storage/repositories"
)

// VulnScanner ist die im Service verwendete Scanner-Abstraktion (Trivy).
// Alias auf die Infrastruktur-Schnittstelle, damit Tests einen Fake einsetzen
// und der graceful degrade greift, wenn Trivy fehlt.
type VulnScanner = trivy.Scanner

// ErrCVEScanUnreachable: der Server ist nicht erreichbar - der Scan würde nur
// den zuletzt gespeicherten Paketbestand bewerten und dabei eine frische
// Scan-Zeit ausweisen (R2-017). Aufrufer unterscheiden damit „übersprungen"
// von einem echten Scanner-Fehler.
var ErrCVEScanUnreachable = errors.New("Server nicht erreichbar")

// scanServerCVEs prüft den Paketbestand eines Servers gegen Trivy, ersetzt den
// CVE-Bestand in der DB und aktualisiert die Scan-Metadaten des Servers. Es
// liefert die Anzahl kritischer/hoher Funde für die Job-Zusammenfassung.
//
// Snaps werden nicht geprüft (Trivy hat keine Snap-Advisory-DB); demo-Server
// werden übersprungen. Fehler des Scanners werden am Server als cve_scan_error
// vermerkt und weitergereicht.
func scanServerCVEs(ctx context.Context, scanner VulnScanner, servers *repositories.ServerRepository, server *domain.Server) (crit, high int, err error) {
	if server.IsDemo {
		return 0, 0, nil
	}
	// Reine API-Geräte (RouterOS, Synology DSM) haben keinen Linux-
	// Paketbestand, den Trivy bewerten könnte - der CVE-Scan ist dort nicht
	// anwendbar und wird stillschweigend übersprungen. Auf DSM wäre er sogar
	// irreführend: der Synology-Kernel und die synopkg-Pakete tragen eigene
	// Versionsschemata, jede Trivy-Aussage darüber wäre geraten.
	if server.IsAPIDevice() {
		return 0, 0, nil
	}
	// Nicht erreichbare Server werden NICHT gescannt: die gespeicherte
	// Paketliste ist so alt wie der letzte Kontakt, ein Scan darüber schriebe
	// aber last_cve_scan_at auf „jetzt" - ein Sicherheitswerkzeug, das für
	// einen Host, den es nicht erreicht, eine tagesaktuelle Prüfzeit ausweist
	// (R2-017). Der alte Bestand samt alter Scan-Zeit bleibt stehen und sagt
	// damit die Wahrheit über sein Alter.
	if !server.Reachable {
		detail := ""
		if server.LastSeenAt != nil {
			detail = " (zuletzt erreichbar: " + server.LastSeenAt.Format("02.01.2006 15:04") + ")"
		}
		return 0, 0, fmt.Errorf("%w%s - der Scan würde nur den damals gespeicherten Paketbestand bewerten und wird übersprungen, bis der Server wieder erreichbar ist", ErrCVEScanUnreachable, detail)
	}
	pkgs, err := servers.FindPackages(server.ID)
	if err != nil {
		return 0, 0, err
	}
	vulns, scanErr := scanner.Scan(ctx, trivy.Target{
		OSID:           server.OSID,
		OSVersionID:    server.OSVersionID,
		PackageManager: server.PackageManager,
		Packages:       pkgs,
	})
	if scanErr != nil {
		_ = servers.UpdateFields(server.ID, map[string]any{"cve_scan_error": scanErr.Error()})
		return 0, 0, scanErr
	}
	if err := servers.ReplaceVulnerabilities(server.ID, domain.VulnSourceOS, vulns); err != nil {
		return 0, 0, err
	}
	for _, v := range vulns {
		switch v.Severity {
		case domain.SeverityCritical:
			crit++
		case domain.SeverityHigh:
			high++
		}
	}
	now := time.Now()
	_ = servers.UpdateFields(server.ID, map[string]any{"last_cve_scan_at": now, "cve_scan_error": ""})
	return crit, high, nil
}

// rescanCVEsAfterPackageUpdate frischt nach einem erfolgreichen Paket-Update die
// CVE-Bewertung eines Servers auf: Trivy prüft den (unmittelbar zuvor neu
// eingelesenen) Paketbestand erneut und ersetzt den CVE-Bestand. Ohne diesen
// Schritt blieben nach einem Update veraltete Sicherheits-Labels an bereits
// aktualisierten Paketen hängen.
//
// Läuft nur, wenn der CVE-Scan aktiviert (enabled) UND Trivy verfügbar ist;
// demo-Server werden übersprungen. Ein Scanner-Fehler bricht den ohnehin
// erfolgreichen Update-Job NICHT ab - er wird geloggt und als Notiz gemeldet.
// Rückgabe ist eine kurze Ergänzung fürs Job-Protokoll ("" = nichts getan).
func rescanCVEsAfterPackageUpdate(scanner VulnScanner, servers *repositories.ServerRepository, enabled bool, server *domain.Server) string {
	if server.IsDemo || !enabled || scanner == nil || !scanner.Available() {
		return ""
	}
	crit, high, err := scanServerCVEs(context.Background(), scanner, servers, server)
	if err != nil {
		slog.Warn("cve re-evaluation after package update failed", "server", server.Name, "error", err)
		return "\n\nHinweis: CVE-Neubewertung nach dem Update fehlgeschlagen: " + err.Error()
	}
	return "\n\nCVE-Bewertung nach dem Update aktualisiert: " + remainingVulnsSentence(servers, server, crit, high)
}

// remainingVulnsSentence beziffert die verbleibenden Lücken UND nennt ihren
// Bezugsrahmen.
//
// Ein Paket-Update betrifft nur das Betriebssystem - die gescannten Zahlen
// stammen entsprechend aus dem Paketbestand. "0 kritische verbleibend" las
// sich aber wie eine Aussage über den ganzen Server und verschwieg damit
// kritische Funde in Container-Images (im Test 23 kritische bei einem Host,
// der als sauber gemeldet wurde). Die Gewichtung selbst ist richtig - Docker
// zählt für Ampel und Alarme bewusst nicht mit -, nur der Satz nannte seinen
// Geltungsbereich nicht (BUG-022).
func remainingVulnsSentence(servers *repositories.ServerRepository, server *domain.Server, crit, high int) string {
	sentence := fmt.Sprintf("%d kritische, %d hohe Betriebssystem-Lücken verbleibend.", crit, high)

	facts, err := servers.VulnerabilityFacts(server.ID)
	if err != nil {
		return sentence
	}
	var dockerCrit, dockerHigh int
	for _, f := range facts {
		if f.Source != domain.VulnSourceDocker {
			continue
		}
		switch domain.NormalizeSeverity(f.Severity) {
		case domain.SeverityCritical:
			dockerCrit++
		case domain.SeverityHigh:
			dockerHigh++
		}
	}
	if dockerCrit == 0 && dockerHigh == 0 {
		return sentence
	}
	return sentence + fmt.Sprintf(" Zusätzlich in Container-Images: %d kritische, %d hohe (siehe Docker-Tab) -"+
		" diese zählen für Ampel und Alarme nicht mit.", dockerCrit, dockerHigh)
}

// cveScanSummary formatiert die Zusammenfassung eines Scan-Laufs für das
// Job-Protokoll.
// Die Zahlen benennen ausdrücklich ihren Gegenstand: sie stammen aus dem
// Paketbestand des Betriebssystems. Ohne diesen Zusatz las sich „0 kritische,
// 0 hohe" wie eine Entwarnung für den ganzen Server - auch dort, wo
// Container-Images hunderte Funde trugen (R2-086).
func cveScanSummary(scanned, failed, crit, high int) string {
	return fmt.Sprintf("CVE-Scan abgeschlossen: %d Server gescannt (%d fehlgeschlagen), %d kritische, %d hohe Sicherheitslücken in Betriebssystempaketen",
		scanned, failed, crit, high)
}
