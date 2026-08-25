package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/storage/repositories"
)

// Deep Scan: tiefergehende Sicherheitsprüfung, die AUF dem Ziel läuft und den
// zentralen (offline) Trivy-CVE-Scan ergänzt. Drei Teile:
//  1. Kernel-/Restart-Lücke via needrestart (laufender vs. installierter Kernel,
//     Dienste mit alten Bibliotheken) - needrestart sieht die Laufzeit, Trivy nur
//     die installierten Paketversionen.
//  2. Kernel-CVEs: kein neuer Scanner - der Deep Scan frischt den zentralen
//     Trivy-Scan auf; der Report hebt die kernel-bezogenen Funde hervor.
//  3. Fehlkonfiguration via Lynis (Härtungs-Index + Warnungen/Empfehlungen);
//     ist Lynis nicht da, greifen die kuratierten LCM-Eigenprüfungen.
// needrestart und lynis werden NICHT automatisch installiert - fehlt eines, wird
// der Teil ehrlich als „nicht geprüft" gemeldet; nachrüsten über InstallDeepScanTools.

// ---- Tool-Erkennung ---------------------------------------------------------

func deepScanToolsScript() string {
	return `for t in needrestart lynis; do if command -v "$t" >/dev/null 2>&1; then echo "HAVE $t"; else echo "MISS $t"; fi; done`
}

// measured meldet, ob die Werkzeug-Erhebung überhaupt ein Ergebnis geliefert
// hat: das Skript gibt für JEDES Werkzeug eine Zeile aus (HAVE oder MISS).
// Kommt gar nichts zurück, ist die Messung fehlgeschlagen - das darf nicht als
// „beide Werkzeuge fehlen" durchgehen und erst recht nicht als „nichts
// gefunden" (B16).
func parseDeepScanTools(out string) (haveNeedrestart, haveLynis, measured bool) {
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(strings.TrimSpace(line))
		if len(f) != 2 || (f[0] != "HAVE" && f[0] != "MISS") {
			continue
		}
		if f[1] == "needrestart" || f[1] == "lynis" {
			measured = true
		}
		if f[0] != "HAVE" {
			continue
		}
		switch f[1] {
		case "needrestart":
			haveNeedrestart = true
		case "lynis":
			haveLynis = true
		}
	}
	return
}

// ---- needrestart ------------------------------------------------------------

// needrestartBatchCmd liefert den maschinenlesbaren Batch-Report (apt-dater-
// Protokoll). Braucht root (liest die Maps anderer Prozesse).
const needrestartBatchCmd = "needrestart -b 2>/dev/null"

type needrestartResult struct {
	KSTA     int // 0 unbekannt, 1 kein Upgrade, 2 ABI-kompatibel ausstehend, 3 Versions-Upgrade ausstehend
	KCur     string
	KExp     string
	Services []string
}

// parseNeedrestart wertet die NEEDRESTART-*-Zeilen aus (Schlüssel:Wert).
func parseNeedrestart(out string) needrestartResult {
	res := needrestartResult{}
	seen := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		key, val, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		val = strings.TrimSpace(val)
		switch strings.TrimSpace(key) {
		case "NEEDRESTART-KSTA":
			res.KSTA, _ = strconv.Atoi(val)
		case "NEEDRESTART-KCUR":
			res.KCur = val
		case "NEEDRESTART-KEXP":
			res.KExp = val
		case "NEEDRESTART-SVC":
			if val != "" && !seen[val] {
				seen[val] = true
				res.Services = append(res.Services, val)
			}
		}
	}
	return res
}

func (r needrestartResult) kernelRebootPending() bool { return r.KSTA == 2 || r.KSTA == 3 }

// ---- Lynis ------------------------------------------------------------------

// lynisRunCmd führt das Audit aus (stdout unterdrückt) und gibt danach die
// maschinenlesbare Report-Datei aus. Braucht root für die volle Abdeckung.
const lynisRunCmd = "lynis audit system --quiet --no-colors >/dev/null 2>&1; cat /var/log/lynis-report.dat 2>/dev/null"

type lynisResult struct {
	HardeningIndex *int
	Warnings       []string
	Suggestions    []string
}

// parseLynisReport liest hardening_index sowie warning[]/suggestion[] aus der
// report.dat (key=value; Werte pipe-getrennt: TEST-ID|Text|…).
func parseLynisReport(out string) lynisResult {
	res := lynisResult{}
	for _, line := range strings.Split(out, "\n") {
		key, val, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "hardening_index":
			if n, err := strconv.Atoi(strings.TrimSpace(val)); err == nil {
				res.HardeningIndex = &n
			}
		case "warning[]":
			if t := lynisText(val); t != "" {
				res.Warnings = append(res.Warnings, t)
			}
		case "suggestion[]":
			if t := lynisText(val); t != "" {
				res.Suggestions = append(res.Suggestions, t)
			}
		}
	}
	return res
}

// lynisText holt aus „TEST-ID|Beschreibung|…" die Beschreibung (2. Feld),
// ersatzweise den Rohwert.
func lynisText(raw string) string {
	parts := strings.Split(raw, "|")
	if len(parts) >= 2 && strings.TrimSpace(parts[1]) != "" {
		return strings.TrimSpace(parts[1])
	}
	return strings.TrimSpace(raw)
}

// ---- Kuratierte LCM-Eigenprüfungen (Fallback ohne Lynis) --------------------

// curatedChecksScript ist ein rein lesendes Härtungs-Skript; jede Zeile
// „LCMFIND|<severity>|<titel>" wird zu einem Befund. Braucht root (sshd -T,
// /etc/shadow). Bewusst schlank - Lynis bleibt die vollständige Prüfung.
func curatedChecksScript() string {
	return `
awk -F: '($2==""){print "LCMFIND|critical|Konto ohne Passwort: "$1}' /etc/shadow 2>/dev/null
if sshd -T 2>/dev/null | grep -qi '^permitrootlogin[[:space:]]\+yes'; then echo "LCMFIND|warning|SSH: Root-Login per SSH erlaubt (PermitRootLogin yes)"; fi
if sshd -T 2>/dev/null | grep -qi '^passwordauthentication[[:space:]]\+yes'; then echo "LCMFIND|info|SSH: Passwort-Login erlaubt (PasswordAuthentication yes)"; fi
va=$(sysctl -n kernel.randomize_va_space 2>/dev/null); if [ -n "$va" ] && [ "$va" != "2" ]; then echo "LCMFIND|warning|Kernel: ASLR nicht voll aktiv (kernel.randomize_va_space=$va)"; fi
pt=$(sysctl -n kernel.kptr_restrict 2>/dev/null); if [ "$pt" = "0" ]; then echo "LCMFIND|info|Kernel: Kernel-Pointer werden nicht verschleiert (kernel.kptr_restrict=0)"; fi
if command -v apt-get >/dev/null 2>&1; then dpkg -s unattended-upgrades >/dev/null 2>&1 || echo "LCMFIND|info|Keine automatischen Sicherheitsupdates (unattended-upgrades nicht installiert)"; fi
`
}

func parseCuratedChecks(out string) []domain.DeepScanFinding {
	var findings []domain.DeepScanFinding
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "LCMFIND|") {
			continue
		}
		parts := strings.SplitN(line, "|", 3)
		if len(parts) != 3 {
			continue
		}
		sev := parts[1]
		switch sev {
		case domain.DeepScanCritical, domain.DeepScanWarning, domain.DeepScanInfo:
		default:
			sev = domain.DeepScanInfo
		}
		findings = append(findings, domain.DeepScanFinding{
			Category: domain.DeepScanMisconfig, Severity: sev, Tool: "lcm", Title: parts[2],
		})
	}
	return findings
}

// ---- Kernel-Paket-Erkennung (für die kernel-fokussierte CVE-Sicht) ----------

// isKernelPackage erkennt Kernel-Pakete über die üblichen Distributions-
// Namensschemata. Bewusst präzise gehalten: ein breites Contains("kernel")
// bzw. HasPrefix("linux-") würde auch linux-firmware, linux-libc-dev oder
// kernel-doc erfassen und die Kernel-CVE-Sicht verwässern.
func isKernelPackage(name string) bool {
	n := strings.ToLower(name)
	switch n {
	case "linux", "kernel", "linux-lts", "linux-hardened", "linux-zen", "linux-virt", "kernel-core", "kernel-default":
		return true
	}
	for _, p := range []string{"linux-image", "kernel-default-", "kernel-core-", "linux-lts-", "kernel-modules"} {
		if strings.HasPrefix(n, p) {
			return true
		}
	}
	return false
}

// ---- Orchestrierung auf dem Ziel (Executor) --------------------------------

// performDeepScan führt den Deep Scan über die bereits aufgebaute (aufgezeichnete)
// Verbindung aus, speichert Befunde + Kennzahlen und liefert eine Job-Zusammenfassung.
func (e *Executor) performDeepScan(server *domain.Server, conn sshx.Conn) (string, error) {
	// Im eingeschränkten Modus laufen die vier Leseschritte über den
	// validierenden Helper. Vorher gingen sie als unprivilegierter
	// Dienstbenutzer durch - `sshd -T`, /etc/shadow, needrestart und lynis
	// brauchen aber root. Das Ergebnis war leer, und leer wurde als „nichts
	// gefunden" verbucht: der Bericht meldete volle Werkzeug-Abdeckung und
	// null Befunde, ohne dass irgendetwas geprüft worden wäre (B16).
	run := func(part, cmd string) string {
		if server.RestrictedSudo {
			cmd = helperCmd("deep-scan", part)
		}
		out, _, err := conn.Run(privRun(server, cmd))
		if err != nil {
			slog.Warn("deep scan step failed", "server", server.Name, "step", part, "error", err)
			return ""
		}
		return out
	}

	haveNr, haveLynis, measured := parseDeepScanTools(run("tools", deepScanToolsScript()))
	// Ohne belastbare Messung wird kein Bericht geschrieben: ein Sicherheits-
	// Audit, das „sauber" meldet, obwohl es nicht laufen konnte, ist
	// schädlicher als ein ehrlicher Fehlschlag (dieselbe Regel wie bei der
	// SSH-Härtung, R2-014).
	if !measured {
		msg := "Deep Scan nicht durchführbar: die Werkzeug-Erhebung auf dem Server lieferte kein Ergebnis"
		if server.RestrictedSudo {
			msg += " - im eingeschränkten Modus läuft der Deep Scan über den LCM-Helper. " +
				"Ist er veraltet, kennt er das Unterkommando `deep-scan` noch nicht; " +
				"über „Neu verbinden“ (Admin-Login) wird er erneuert"
		}
		_ = e.servers.UpdateFields(server.ID, map[string]any{"deep_scan_error": msg})
		return "", errors.New(msg)
	}
	// Welche Werkzeuge diesen Lauf getragen haben, gehört in den Bericht:
	// ein Lauf ohne Lynis deckt deutlich weniger ab, und beim Vergleich zweier
	// Läufe erklärt das sonst unerklärliche Sprünge in der Befundzahl.
	var usedTools []string
	if haveNr {
		usedTools = append(usedTools, "needrestart")
	}
	if haveLynis {
		usedTools = append(usedTools, "lynis")
	}
	if len(usedTools) == 0 {
		usedTools = append(usedTools, "lcm")
	}
	var findings []domain.DeepScanFinding
	kernelRebootPending := false

	// 1. Kernel-/Restart-Lücke.
	if haveNr {
		nr := parseNeedrestart(run("needrestart", needrestartBatchCmd))
		kernelRebootPending = nr.kernelRebootPending()
		if kernelRebootPending {
			findings = append(findings, domain.DeepScanFinding{
				Category: domain.DeepScanKernel, Severity: domain.DeepScanWarning, Tool: "needrestart",
				Title:  fmt.Sprintf("Laufender Kernel %s älter als installierter %s - Neustart nötig", nr.KCur, nr.KExp),
				Detail: "Kernel-Sicherheitsfixes wirken erst nach einem Neustart.",
			})
		}
		for _, svc := range nr.Services {
			findings = append(findings, domain.DeepScanFinding{
				Category: domain.DeepScanRestart, Severity: domain.DeepScanWarning, Tool: "needrestart",
				Title:  "Dienst nutzt veraltete Bibliotheken: " + svc,
				Detail: "Neu starten, damit aktualisierte (ggf. sicherheitsrelevante) Bibliotheken geladen werden.",
			})
		}
	} else {
		findings = append(findings, domain.DeepScanFinding{
			Category: domain.DeepScanRestart, Severity: domain.DeepScanInfo, Tool: "lcm",
			Title:  "needrestart nicht installiert - Kernel-/Dienst-Neustartlücke nicht geprüft",
			Detail: "Über den Knopf Tools installieren nachrüsten für den vollständigen Deep Scan.",
		})
	}

	// 2. Fehlkonfiguration.
	var hardening *int
	if haveLynis {
		ly := parseLynisReport(run("lynis", lynisRunCmd))
		hardening = ly.HardeningIndex
		for _, w := range ly.Warnings {
			findings = append(findings, domain.DeepScanFinding{
				Category: domain.DeepScanMisconfig, Severity: domain.DeepScanWarning, Tool: "lynis", Title: w,
			})
		}
		for _, s := range ly.Suggestions {
			findings = append(findings, domain.DeepScanFinding{
				Category: domain.DeepScanMisconfig, Severity: domain.DeepScanInfo, Tool: "lynis", Title: s,
			})
		}
	} else {
		findings = append(findings, domain.DeepScanFinding{
			Category: domain.DeepScanMisconfig, Severity: domain.DeepScanInfo, Tool: "lcm",
			Title:  "Lynis nicht installiert - kuratierte LCM-Prüfungen verwendet (geringere Abdeckung)",
			Detail: "Über den Knopf Tools installieren Lynis nachrüsten für das vollständige Härtungs-Audit.",
		})
		findings = append(findings, parseCuratedChecks(run("curated", curatedChecksScript()))...)
	}

	// 3. Kernel-CVEs frisch halten (zentraler Trivy-Lauf; berührt das Ziel nicht).
	if e.scanner != nil && e.scanner.Available() {
		if _, _, err := scanServerCVEs(context.Background(), e.scanner, e.servers, server); err != nil {
			slog.Warn("deep scan: trivy refresh failed", "server", server.Name, "error", err)
		}
	}

	// 4. Verdichten + als datierten Lauf speichern.
	warnings := 0
	for _, f := range findings {
		if domain.DeepScanSeverityAtLeast(f.Severity, domain.DeepScanWarning) {
			warnings++
		}
	}
	report := &domain.DeepScanReport{
		ServerID:            server.ID,
		HardeningIndex:      hardening,
		KernelRebootPending: kernelRebootPending,
		Tools:               strings.Join(usedTools, ","),
	}
	// SaveDeepScanReport bestimmt dabei neu/behoben gegenüber dem Vorlauf und
	// setzt die Zählungen - der Aufrufer muss dafür nichts wissen.
	if err := e.servers.SaveDeepScanReport(report, findings); err != nil {
		return "", err
	}
	fields := map[string]any{
		"deep_scan_at":          time.Now(),
		"deep_scan_warnings":    warnings,
		"kernel_reboot_pending": kernelRebootPending,
		"deep_scan_error":       "",
		"hardening_index":       hardening, // nil ⇒ NULL (Lynis nicht gelaufen)
	}
	if kernelRebootPending {
		fields["reboot_required"] = true // in die Ampel/den Reboot-Alarm einspeisen
	}
	_ = e.servers.UpdateFields(server.ID, fields)

	return deepScanSummary(warnings, findings, hardening), nil
}

func deepScanSummary(warnings int, findings []domain.DeepScanFinding, hardening *int) string {
	s := fmt.Sprintf("Deep Scan abgeschlossen: %d Warnung(en), %d Befund(e) gesamt", warnings, len(findings))
	if hardening != nil {
		s += fmt.Sprintf(", Härtungs-Index %d/100", *hardening)
	}
	return s
}

// RunDeepScanServer führt den Deep Scan für einen einzelnen Server aus (manueller
// Trigger). Asynchron; Ergebnis erscheint als Job in der Historie.
func (e *Executor) RunDeepScanServer(id uint, triggeredBy string) {
	server, err := e.servers.FindByIDUnscoped(id)
	if err != nil {
		slog.Error("deep scan: server not loadable", "server", id, "error", err)
		return
	}
	job, err := e.jobs.Start(&server.ID, nil, domain.RuleTypeDeepScan, "Deep Scan @ "+server.Name, triggeredBy)
	if err != nil {
		if !errors.Is(err, ErrServerBusy) {
			slog.Error("deep scan job start failed", "server", server.Name, "error", err)
		}
		return
	}
	if server.IsDemo {
		e.jobs.Complete(job, "Demo-Server - Deep Scan übersprungen.", ptrInt(0), nil)
		return
	}
	conn, err := e.connect(server)
	if err != nil {
		e.markUnreachable(server, err)
		e.jobs.Complete(job, "", nil, fmt.Errorf("verbindung: %w", err))
		return
	}
	conn = e.recorder.Record(conn, SessionContext{
		ServerID: server.ID, JobID: &job.ID, Actor: triggeredBy,
		Purpose: "deep-scan", Host: server.Host, User: server.ServiceUser,
	})
	summary, derr := e.performDeepScan(server, conn)
	conn.Close()
	e.finishWithHealth(job, server, summary, derr)
}

// runDeepScanRule führt den Deep Scan als Gruppen-Regel auf einem Server aus.
func (e *Executor) runDeepScanRule(job *domain.Job, server *domain.Server, rule *domain.Rule, triggeredBy string) {
	if server.IsDemo {
		e.jobs.Complete(job, "Demo-Server - Deep Scan übersprungen.", ptrInt(0), nil)
		return
	}
	conn, err := e.connect(server)
	if err != nil {
		e.markUnreachable(server, err)
		e.jobs.Complete(job, "", nil, fmt.Errorf("verbindung: %w", err))
		return
	}
	conn = e.recorder.Record(conn, SessionContext{
		ServerID: server.ID, JobID: &job.ID, Actor: triggeredBy,
		Purpose: "rule:" + rule.Name, Host: server.Host, User: server.ServiceUser,
	})
	summary, derr := e.performDeepScan(server, conn)
	conn.Close()
	e.finishWithHealth(job, server, summary, derr)
}

// ---- Lesesicht + Tool-Installation (ServerService) -------------------------

// DeepScanReport bündelt die gespeicherten Deep-Scan-Ergebnisse eines Servers
// für die Detailansicht.
type DeepScanReport struct {
	// Findings ist der AKTUELLE Stand (Befunde des jüngsten Laufs) - die
	// Grundlage für Ampel, Insights und Alarme.
	Findings    []domain.DeepScanFinding `json:"findings"`
	KernelVulns []domain.Vulnerability   `json:"kernel_vulns"`
	// Reports ist die Lauf-Historie, neueste zuerst (ohne Befunde; die kommen
	// beim Aufklappen eines Laufs über DeepScanReportDetail).
	Reports             []domain.DeepScanReport `json:"reports"`
	HardeningIndex      *int                    `json:"hardening_index"`
	DeepScanAt          *time.Time              `json:"deep_scan_at"`
	DeepScanError       string                  `json:"deep_scan_error"`
	KernelRebootPending bool                    `json:"kernel_reboot_pending"`
	ScannerAvailable    bool                    `json:"scanner_available"`
}

// deepScanReportListLimit begrenzt die mitgelieferte Historie. 30 Läufe sind
// bei täglichem Scan ein Monat - genug, um einen Fortschritt zu belegen, ohne
// die Detailansicht mit Altlasten zu fluten.
const deepScanReportListLimit = 30

// deepScanReportsKept ist die Aufbewahrung je Server: die jüngsten N Läufe.
// Etwas mehr als die Anzeige (deepScanReportListLimit), damit die Liste nicht
// gerade an der Kante der Bereinigung endet.
const deepScanReportsKept = 40

// DeepScanReport liefert die Lesesicht: gespeicherte Befunde + die
// kernel-bezogenen CVEs aus dem letzten Trivy-Scan.
func (s *ServerService) DeepScanReport(scope repositories.AccessScope, id uint) (*DeepScanReport, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return nil, err
	}
	findings, err := s.servers.FindDeepScanFindings(id)
	if err != nil {
		return nil, err
	}
	rep := &DeepScanReport{
		Findings:            findings,
		HardeningIndex:      server.HardeningIndex,
		DeepScanAt:          server.DeepScanAt,
		DeepScanError:       server.DeepScanError,
		KernelRebootPending: server.KernelRebootPending,
		ScannerAvailable:    s.ScannerAvailable(),
	}
	// Die Lauf-Historie kommt mit: ohne sie zeigt die Oberfläche wieder nur
	// den letzten Stand, und die Frage „ist das neu oder stand das vorher
	// schon da?" bleibt unbeantwortet.
	if reports, err := s.servers.FindDeepScanReports(id, deepScanReportListLimit); err == nil {
		rep.Reports = reports
	}
	if vulns, err := s.servers.FindVulnerabilities(id); err == nil {
		for _, v := range vulns {
			if v.Source == domain.VulnSourceOS && isKernelPackage(v.PackageName) {
				rep.KernelVulns = append(rep.KernelVulns, v)
			}
		}
	}
	return rep, nil
}

// DeepScanReportDetail liefert EINEN Lauf samt seiner Befunde - das, was beim
// Aufklappen eines Eintrags der Historie sichtbar wird. Die Scope-Prüfung
// läuft über den Server, die Report-ID wird gegen ihn verifiziert; eine
// fremde ID liefert damit nie fremde Befunde.
func (s *ServerService) DeepScanReportDetail(scope repositories.AccessScope, id uint, reportID string) (*domain.DeepScanReport, error) {
	if _, err := s.servers.FindByID(scope, id); err != nil {
		return nil, err
	}
	return s.servers.FindDeepScanReport(id, reportID)
}

// InstallDeepScanTools installiert needrestart + lynis auf dem Server (angeboten,
// nicht automatisch). Multi-Distro, best-effort je Paket.
func (s *ServerService) InstallDeepScanTools(scope repositories.AccessScope, id uint, actor string) (*domain.Job, error) {
	return s.startPackageJob(scope, id, domain.RuleTypeScript,
		"Deep-Scan-Tools installieren (needrestart, lynis)",
		func(mgr string) string { return pkgInstallScript(mgr, []string{"needrestart", "lynis"}) }, actor)
}
