package services

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/registry"
)

// RunDockerCheck ist der zentrale Docker-Lauf (serverloser System-Job,
// analog zu Backup/CVE-Scan): Er prüft für jedes erfasste Image, ob die
// Registry hinter dem Tag einen neueren Digest führt, und scannt die Images
// aus imagesToScan mit Trivy auf bekannte Sicherheitslücken. Beides läuft
// auf dem LCM-Host - die verwalteten Server werden dafür NICHT
// kontaktiert. Dedupliziert: nginx:1.25 auf 12 Servern = 1 Registry-Call
// und 1 Trivy-Scan; die Funde werden auf alle betroffenen Server verteilt
// (gleiche Schwachstellen-Tabelle wie der Paket-Scan → Ampel,
// Security-Seite und CVE-Alarme greifen automatisch).
func (e *Executor) RunDockerCheck(triggeredBy string) {
	e.runSystemJob(domain.RuleTypeDockerCheck, triggeredBy, "Docker-Check", func() (string, error) {
		return e.runDockerCheck(context.Background())
	})
}

func (e *Executor) runDockerCheck(ctx context.Context) (string, error) {
	if e.registry == nil {
		return "Registry-Client nicht verdrahtet - Docker-Check übersprungen.", nil
	}
	images, err := e.servers.FindAllDockerImagesUnscoped()
	if err != nil {
		return "", err
	}
	demo, err := e.demoServerIDs()
	if err != nil {
		return "", err
	}

	// Prüfbare Images (Registry-Digest + Tag vorhanden) pro Referenz
	// gruppieren - jede Referenz wird genau einmal gegen die Registry
	// geprüft, das Ergebnis auf alle Server-Zeilen verteilt.
	byRef := map[string][]*domain.DockerImage{}
	var local int
	for i := range images {
		img := &images[i]
		if demo[img.ServerID] {
			continue
		}
		if img.RepoDigest == "" || img.Tag == "" {
			local++
			continue
		}
		byRef[img.Ref()] = append(byRef[img.Ref()], img)
	}

	now := time.Now()
	var updates, unverifiable, errored int
	for ref, rows := range byRef {
		res := e.registry.CheckDigest(ctx, ref)
		switch res.Status {
		case domain.DockerCheckUnauthorized:
			unverifiable++
		case domain.DockerCheckNotFound, domain.DockerCheckError:
			errored++
		}
		refHasUpdate := false
		for _, img := range rows {
			// Digest-Vergleich pro Zeile: Server können unterschiedliche
			// Stände desselben Tags haben.
			available := res.Status == domain.DockerCheckOK && res.Digest != img.RepoDigest
			if available {
				refHasUpdate = true
			}
			_ = e.servers.UpdateDockerImageCheck(img.ID, map[string]any{
				"candidate_digest": res.Digest,
				"update_available": available,
				"check_status":     res.Status,
				"check_error":      res.Err,
				"last_checked_at":  &now,
			})
		}
		if refHasUpdate {
			updates++
		}
	}

	summary := dockerCheckSummary(len(byRef), updates, unverifiable, errored, local)
	summary += "\n" + e.scanDockerImages(ctx, images, demo)
	return summary, nil
}

// imagesToScan wählt die Images für den CVE-Scan. Was ein Container nutzt, wird
// immer geprüft, auch eine überholte Fassung: Genau sie läuft ja noch. Von den
// ungenutzten zählt je Repository nur das neueste, denn das könnte als Nächstes
// in Betrieb gehen. Ältere Stände, die nichts mehr nutzt, wären nur Rauschen.
// Ohne Registry-Digest (lokal gebaut) kann Trivy nichts laden.
func imagesToScan(images []domain.DockerImage) []*domain.DockerImage {
	type repo struct {
		server uint
		name   string
	}
	newest := map[repo]time.Time{}
	for i := range images {
		img := &images[i]
		if img.Superseded || img.BuiltAt == nil {
			continue
		}
		key := repo{img.ServerID, img.Repository}
		if img.BuiltAt.After(newest[key]) {
			newest[key] = *img.BuiltAt
		}
	}
	var out []*domain.DockerImage
	for i := range images {
		img := &images[i]
		if img.ScanRef() == "" {
			continue
		}
		latest := !img.Superseded && (img.BuiltAt == nil || !img.BuiltAt.Before(newest[repo{img.ServerID, img.Repository}]))
		if img.InUse || (latest && img.Tag != "") {
			out = append(out, img)
		}
	}
	return out
}

// scanDockerImages prüft die Images aus imagesToScan mit Trivy auf CVEs -
// dedupliziert pro (Referenz, Digest) - und ersetzt die Docker-CVE-Funde
// der betroffenen Server. Server, bei denen ein Image-Scan fehlschlug,
// behalten ihren bisherigen Bestand (kein Flackern durch transiente
// Registry-Fehler). Liefert die Zeile fürs Job-Protokoll.
func (e *Executor) scanDockerImages(ctx context.Context, images []domain.DockerImage, demo map[uint]bool) string {
	if e.scanner == nil || !e.scanner.Available() {
		return "Image-CVE-Scan übersprungen: Trivy nicht verfügbar."
	}

	// Gescannt wird per "repo@digest" - also EXAKT der Stand, der auf den
	// Servern liegt. Ein Scan per "repo:tag" würde den AKTUELLEN Registry-Stand
	// des Tags prüfen und bei veralteten lokalen Images die falschen (nämlich
	// gar keine) Lücken melden - im Live-Test mit einem zurückgetaggten
	// alpine:3 aufgefallen. Nebeneffekt: mehrfach getaggte Images
	// (alpine:3 + alpine:3.19 auf demselben Digest) werden nur einmal
	// gescannt.
	var live []domain.DockerImage
	for i := range images {
		if !demo[images[i].ServerID] {
			live = append(live, images[i])
		}
	}
	byKey := map[string][]*domain.DockerImage{}
	for _, img := range imagesToScan(live) {
		byKey[img.ScanRef()] = append(byKey[img.ScanRef()], img)
	}

	// Jede (Repository, Digest)-Kombination genau einmal scannen.
	vulnsByKey := map[string][]domain.Vulnerability{}
	failedKeys := map[string]bool{}
	var scanned, failed int
	for k := range byKey {
		vulns, err := e.scanner.ScanImage(ctx, k)
		if err != nil {
			failed++
			failedKeys[k] = true
			slog.Warn("image cve scan failed", "image", k, "error", err)
			continue
		}
		scanned++
		for i := range vulns {
			vulns[i].Source = domain.VulnSourceDocker
		}
		vulnsByKey[k] = vulns
	}

	// Fan-out: pro Server die Funde seiner Images sammeln (mit der
	// menschenlesbaren Referenz der jeweiligen Zeile) und die Docker-Quelle
	// atomar ersetzen (auch leer - räumt verschwundene Images ab). Server
	// mit fehlgeschlagenem Image-Scan überspringen.
	perServer := map[uint][]domain.Vulnerability{}
	skipServer := map[uint]bool{}
	for k, rows := range byKey {
		for _, img := range rows {
			if failedKeys[k] {
				skipServer[img.ServerID] = true
				continue
			}
			for _, v := range vulnsByKey[k] {
				v.ImageRef = img.VulnRef()
				perServer[img.ServerID] = append(perServer[img.ServerID], v)
			}
		}
	}
	var crit, high int
	for serverID := range e.dockerServerIDs(images, demo) {
		if skipServer[serverID] {
			continue
		}
		vulns := perServer[serverID]
		if err := e.servers.ReplaceVulnerabilities(serverID, domain.VulnSourceDocker, vulns); err != nil {
			slog.Warn("docker cve findings could not be stored", "server", serverID, "error", err)
			continue
		}
		for _, v := range vulns {
			switch v.Severity {
			case domain.SeverityCritical:
				crit++
			case domain.SeverityHigh:
				high++
			}
		}
	}
	return fmt.Sprintf("Image-CVE-Scan: %d Images gescannt (%d fehlgeschlagen), %d kritische, %d hohe Sicherheitslücken",
		scanned, failed, crit, high)
}

// dockerServerIDs liefert die IDs aller Nicht-Demo-Server mit
// Docker-Inventar (die Bezugsmenge des CVE-Fan-outs).
func (e *Executor) dockerServerIDs(images []domain.DockerImage, demo map[uint]bool) map[uint]bool {
	ids := map[uint]bool{}
	for i := range images {
		if !demo[images[i].ServerID] {
			ids[images[i].ServerID] = true
		}
	}
	return ids
}

// demoServerIDs liefert die IDs aller Demo-Server (deren Inventar wird
// nie gegen echte Registries geprüft).
func (e *Executor) demoServerIDs() (map[uint]bool, error) {
	servers, err := e.servers.FindAllUnscoped()
	if err != nil {
		return nil, err
	}
	demo := map[uint]bool{}
	for i := range servers {
		if servers[i].IsDemo {
			demo[servers[i].ID] = true
		}
	}
	return demo, nil
}

// dockerCheckSummary formatiert die Job-Zusammenfassung des Update-Checks.
func dockerCheckSummary(checked, updates, unverifiable, errored, local int) string {
	s := fmt.Sprintf("Docker-Check: %d Image-Tags geprüft, %d mit verfügbarem Update", checked, updates)
	if unverifiable > 0 {
		s += fmt.Sprintf(", %d nicht prüfbar (privat)", unverifiable)
	}
	if errored > 0 {
		s += fmt.Sprintf(", %d mit Fehler", errored)
	}
	if local > 0 {
		s += fmt.Sprintf("; %d lokale Images übersprungen", local)
	}
	return s
}

// WithRegistry verdrahtet den Registry-Client für den Docker-Update-Check.
// Optional - fehlt er, wird der Docker-Check sauber übersprungen.
func (e *Executor) WithRegistry(r registry.Checker) *Executor {
	e.registry = r
	return e
}
