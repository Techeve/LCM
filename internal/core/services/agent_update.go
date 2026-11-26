package services

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"LCM/internal/core/agentcaps"
	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
	"LCM/internal/version"
)

// Agent aktualisieren.
//
// Der lcm-agent läuft als Paket (lcm-agent) auf dem verwalteten Server und ist
// dort vom normalen Systemupdate ausgenommen (apt, dnf, zypper, apk, pacman: je
// auf eigene Weise, siehe agent_pkg.go): Er wechselt die Version nur gezielt,
// durch diesen Auftrag. LCM bestimmt dabei die Zielversion (siehe
// agentcaps.PickTarget): die höchste Version, die die Paketquelle des Servers
// führt und die zur Feature-Reihe von LCM passt (Bugfixes darin eingeschlossen,
// nie eine höhere Reihe).
//
// Der Auftrag läuft als gewöhnlicher Job über den Agent: Der Agent führt
// Kommandos in einem eigenen Scope aus, ein Update des eigenen Pakets reißt
// sie also nicht mit, und er wechselt nach dem Auftrag von selbst auf das neue
// Binary (Selbstwechsel per exec, sobald er nichts mehr zu tun hat).
//
// Ablauf des Jobs:
//
//  1. Paketlisten erneuern und die Versionen lesen, die die Quellen des Servers
//     führen (unter apt: apt-cache madison lcm-agent).
//  2. Zielversion wählen. Steht der Agent schon darauf oder gibt es nichts
//     Neueres: Job endet ohne Änderung.
//  3. Genau diese Version installieren, unter apt `apt-get install
//     lcm-agent=<Version>` unter dem Sperren-Vorspann (lcm_apt).
//     Die Paketquelle des Agents ist auf jedem Server die Community-Quelle (die
//     Einrichtung am Server legt sie an, der Enterprise-Kanal führt kein
//     lcm-agent), auch bei einem Enterprise-LCM.
//  4. Auf die Rückmeldung warten: Der Agent meldet sich mit der neuen Version
//     (Inventar) oder, wenn sie sich nicht verbinden konnte, mit einem Rollback
//     auf die vorige. Erst danach gilt der Job als erledigt, und erst dann gibt
//     LCM den Server wieder frei.
//  5. Ein Kommando über den neuen Agent prüfen: Verbunden ist nicht
//     antwortbereit.

// agentPackage ist der Paketname des Agents.
const agentPackage = "lcm-agent"

// Marken in der Ausgabe der Skripte.
const (
	agentMarkInstalled   = "LCM_AGENT_INSTALLED="
	agentMarkVersions    = "LCM_AGENT_VERSIONS"
	agentMarkNotPackaged = "LCM_AGENT_NOT_PACKAGED"
	agentMarkNoApt       = "LCM_AGENT_NO_APT"
	agentMarkRefreshFail = "LCM_AGENT_REFRESH_FAILED="
	agentMarkEntPin      = "LCM_AGENT_ENTERPRISE_PIN"
)

// Zeiten des Wartens auf die Rückmeldung. Variablen, damit Tests sie senken.
var (
	// AgentUpdateMaxWait ist das Zeitfenster, in dem der Agent nach der
	// Installation mit der neuen Version (oder einem Rollback) zurück sein muss.
	// Der Agent wechselt frühestens nach 30 Sekunden und bis zu 30 Minuten
	// später, wenn eine Web-Konsole offen ist; ein Rückfall zeigt sich nach etwa
	// 30 Sekunden (Absturzschleife) oder sechs bis sieben Minuten (nicht
	// bereit). Agenten bis 2.3.0 können keinen Rollback melden: Für sie gilt
	// allein diese Frist.
	AgentUpdateMaxWait = 45 * time.Minute
	// AgentUpdatePoll ist der Abstand der Nachfragen.
	AgentUpdatePoll = 2 * time.Second
)

// AgentUpdatePlan sagt, was ein Agent-Update auf diesem Server täte.
type AgentUpdatePlan struct {
	// Action ist update, current, newer, none oder unsupported.
	Action string `json:"action"`
	// Reason nennt bei unsupported und none den Grund als Kennung: no_manager,
	// not_packaged, no_version, unknown_lcm_version.
	Reason        string   `json:"reason,omitempty"`
	Installed     string   `json:"installed"`
	Target        string   `json:"target,omitempty"`
	ServerVersion string   `json:"server_version"`
	Available     []string `json:"available"`
	// Warnings sind Hinweise, die den Lauf nicht verhindern (Kennungen).
	Warnings []string `json:"warnings,omitempty"`
}

// Aktionen und Gründe des Plans.
const (
	agentActionUnsupported = "unsupported"

	agentReasonNoManager      = "no_manager"
	agentReasonNotPackaged    = "not_packaged"
	agentReasonNoVersion      = "no_version"
	agentReasonUnknownVersion = "unknown_lcm_version"
)

// agentVersionsScript liest, was auf dem Server installiert und zu haben ist.
// Mit refresh erneuert es vorher die Paketlisten - über lcm_apt_update, das auf
// die apt-Sperren wartet und die Dauer begrenzt. Eine Quelle, die sich nicht
// aktualisieren lässt, hindert nichts: Es gilt, was die Listen führen; die
// Marke merkt es an.
//
// Bewusst die Listen ALLER Quellen und nicht nur die der Techeve-Quelle: Wie die
// Quelle heißt (.sources oder .list, eigener Server), ist je Server verschieden,
// und apt kann eine einzelne deb822-Datei nicht zuverlässig allein
// aktualisieren.
func agentVersionsScript(refresh bool) string {
	lines := []string{
		`command -v apt-get >/dev/null 2>&1 && command -v dpkg-query >/dev/null 2>&1 || { echo ` + agentMarkNoApt + `; exit 0; }`,
		// ii: installiert, hi: installiert und angehalten (der Agent sperrt sich
		// selbst gegen apt upgrade).
		`case "$(dpkg-query -W -f='${db:Status-Abbrev}' ` + agentPackage + ` 2>/dev/null)" in ii*|hi*) ;; *) echo ` + agentMarkNotPackaged + `; exit 0 ;; esac`,
	}
	if refresh {
		lines = append(lines,
			`lcm_apt_update`,
			`rc=$?`,
			`[ $rc -eq `+strconv.Itoa(aptLockBusyExit)+` ] && exit $rc`,
			`[ $rc -eq 0 ] || echo `+agentMarkRefreshFail+`$rc`,
		)
	}
	lines = append(lines,
		`echo `+agentMarkInstalled+`$(dpkg-query -W -f='${Version}' `+agentPackage+` 2>/dev/null)`,
		// Die ältere Fassung von setup-enterprise.sh sperrte lcm-agent neben lcm
		// auf dem LCM-Host; ein Agent auf diesem Host bekäme dort kein Update.
		`grep -qs `+agentPackage+` /etc/apt/preferences.d/lcm-enterprise.pref && echo `+agentMarkEntPin,
		`echo `+agentMarkVersions,
		`LC_ALL=C apt-cache madison `+agentPackage+` 2>/dev/null`,
		`exit 0`,
	)
	return aptScript(lines...)
}

// agentInstallScript installiert genau diese Version. Der Agent sperrt sich im
// Paket gegen apt (Hold oder Pin); eine ausdrückliche Version mit
// --allow-change-held-packages durchbricht die Sperre nur für diesen Aufruf.
// --allow-downgrades gilt, falls eine Quelle die Version nur als „älter“ führt
// (Epoche, Beta nach Final); gewählt wird sie nie unter der installierten.
func agentInstallScript(ver string) string {
	return aptScript(
		`lcm_apt install -y --allow-downgrades --allow-change-held-packages `+agentPackage+`=`+ver,
		`rc=$?`,
		`echo `+agentMarkInstalled+`$(dpkg-query -W -f='${Version}' `+agentPackage+` 2>/dev/null)`,
		`exit $rc`,
	)
}

// agentVersions ist die ausgewertete Ausgabe von agentVersionsScript.
type agentVersions struct {
	Installed      string
	Available      []string
	Sources        map[string]string // Version → „Quelle Suite/Komponente“ (nur für das Protokoll)
	NoManager      bool              // apt-get/dpkg (oder dnf, zypper, apk, pacman) fehlt auf dem Server
	NotPackaged    bool
	RefreshFailed  bool
	EnterprisePin  bool
	refreshExitRaw string
}

// parseAgentVersions liest die Marken und die Liste der Quellen: unter apt die
// madison-Zeilen, sonst die rohe Ausgabe der Paketverwaltung (parseAgentList).
// Versionen stehen danach in der Schreibweise von apt (agentCanonVersion).
func parseAgentVersions(mgr, out string) agentVersions {
	family := pkgFamily(mgr)
	v := agentVersions{Sources: map[string]string{}}
	inList := false
	var listed []string
	seen := map[string]bool{}
	add := func(ver, source string) {
		if ver == "" || seen[ver] || !rePackageVersion.MatchString(ver) {
			return
		}
		seen[ver] = true
		v.Available = append(v.Available, ver)
		v.Sources[ver] = source
	}
	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case line == agentMarkNoApt || line == agentMarkNoManager:
			v.NoManager = true
		case line == agentMarkNotPackaged:
			v.NotPackaged = true
		case line == agentMarkEntPin:
			v.EnterprisePin = true
		case strings.HasPrefix(line, agentMarkRefreshFail):
			v.RefreshFailed = true
			v.refreshExitRaw = strings.TrimPrefix(line, agentMarkRefreshFail)
		case strings.HasPrefix(line, agentMarkInstalled):
			v.Installed = agentCanonVersion(family, strings.TrimPrefix(line, agentMarkInstalled))
		case line == agentMarkVersions:
			inList = true
		case inList && family == pkgApt:
			parts := strings.Split(line, "|")
			if len(parts) >= 3 && strings.TrimSpace(parts[0]) == agentPackage {
				add(strings.TrimSpace(parts[1]), strings.TrimSpace(parts[2]))
			}
		case inList:
			listed = append(listed, raw)
		}
	}
	for _, l := range parseAgentList(family, strings.Join(listed, "\n")) {
		add(agentCanonVersion(family, l.Native), l.Source)
	}
	return v
}

// planAgentUpdate ordnet die Lage des Servers ein. limit ist eine feste
// Obergrenze zusätzlich zur Feature-Reihe dieses LCM (leer: keine).
func planAgentUpdate(v agentVersions, limit string) AgentUpdatePlan {
	plan := AgentUpdatePlan{
		ServerVersion: version.Version, Installed: v.Installed, Available: v.Available,
	}
	if plan.Available == nil {
		plan.Available = []string{}
	}
	if v.EnterprisePin {
		plan.Warnings = append(plan.Warnings, "enterprise_pin")
	}
	switch {
	case v.NoManager:
		plan.Action, plan.Reason = agentActionUnsupported, agentReasonNoManager
		return plan
	case v.NotPackaged:
		plan.Action, plan.Reason = agentActionUnsupported, agentReasonNotPackaged
		return plan
	}
	sel := agentcaps.PickTarget(v.Installed, v.Available, version.Version, limit)
	plan.Action, plan.Target = string(sel.Action), sel.Target
	switch sel.Action {
	case agentcaps.ActionNone:
		if !versionIsKnown(version.Version) {
			plan.Reason = agentReasonUnknownVersion
		} else {
			plan.Reason = agentReasonNoVersion
		}
	}
	return plan
}

// versionIsKnown meldet, ob v eine echte Versionsnummer ist (kein Entwicklungsstand).
func versionIsKnown(v string) bool { return version.Compare(v, "0.0.0") > 0 }

// agentUpdateOptions steuern einen Lauf.
type agentUpdateOptions struct {
	// Limit begrenzt die Zielversion zusätzlich zur Feature-Reihe von LCM
	// (leer: keine). Gestaffelte Läufe setzen hier die Version, die der erste
	// Server erreicht hat.
	Limit string
}

// AgentUpdatePlan liest live vom Server, was ein Update täte (nur lesend, ohne
// die Paketlisten zu erneuern - der Job selbst erneuert sie und kann deshalb
// eine neuere Version finden).
func (s *ServerService) AgentUpdatePlan(scope repositories.AccessScope, id uint) (*AgentUpdatePlan, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return nil, err
	}
	if !server.IsAgent() {
		return nil, ErrNotAgentServer
	}
	if server.IsDemo {
		return &AgentUpdatePlan{Action: string(agentcaps.ActionCurrent), ServerVersion: version.Version,
			Installed: server.AgentVersion, Available: []string{}}, nil
	}
	conn, err := s.connectRecRead(server, "agent-update-plan", "system")
	if err != nil {
		return nil, fmt.Errorf("verbindung: %w", err)
	}
	defer conn.Close()
	out, code, err := conn.Run(privRun(server, agentVersionsScriptFor(server.PackageManager, false)))
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, fmt.Errorf("versionsabfrage endete mit exit-code %d", code)
	}
	plan := planAgentUpdate(parseAgentVersions(server.PackageManager, out), "")
	return &plan, nil
}

// StartAgentUpdate startet das Update des Agents als Job.
func (s *ServerService) StartAgentUpdate(scope repositories.AccessScope, id uint, actor string) (*domain.Job, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return nil, err
	}
	return s.startAgentUpdate(server, actor, agentUpdateOptions{})
}

// StartAgentUpdateTo ist StartAgentUpdate mit einer festen Obergrenze (die
// Staffelung der Gruppen-Regel).
func (s *ServerService) StartAgentUpdateTo(scope repositories.AccessScope, id uint, actor, limit string) (*domain.Job, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return nil, err
	}
	return s.startAgentUpdate(server, actor, agentUpdateOptions{Limit: limit})
}

func (s *ServerService) startAgentUpdate(server *domain.Server, actor string, opts agentUpdateOptions) (*domain.Job, error) {
	if !server.IsAgent() {
		return nil, ErrNotAgentServer
	}
	job, err := s.jobs.Start(&server.ID, nil, domain.JobTypeAgentUpdate, "Agent aktualisieren @ "+server.Name, actor)
	if err != nil {
		return nil, err // u.a. ErrServerBusy
	}
	s.audit.Log(actor, "server.agent-update", "server", server.ID, server.Name+" ("+server.AgentVersion+")")
	return startJob(s.jobs, "job:agent-update", job, func() {
		s.runAgentUpdateJob(job, server, actor, opts)
	}), nil
}

// runAgentUpdateJob führt die Schritte aus (siehe Kopf der Datei).
func (s *ServerService) runAgentUpdateJob(job *domain.Job, server *domain.Server, actor string, opts agentUpdateOptions) {
	if server.IsDemo {
		s.jobs.CompleteSkipped(job, "Demo-Server: Der Agent wird nicht aktualisiert.")
		return
	}
	var log strings.Builder
	say := func(format string, args ...any) { fmt.Fprintf(&log, format+"\n", args...) }
	finish := func(code int, err error) { s.jobs.Complete(job, log.String(), ptrInt(code), err) }

	conn, err := s.connect(server)
	if err != nil {
		recordUnreachable(s.servers, server, err)
		finish(-1, fmt.Errorf("verbindung: %w", err))
		return
	}
	conn = s.recorder.Record(conn, SessionContext{
		ServerID: server.ID, JobID: &job.ID, Actor: actor,
		Purpose: "agent-update", Host: server.Host, User: server.ServiceUser,
	})

	out, code, runErr := conn.Run(privRun(server, agentVersionsScriptFor(server.PackageManager, true)))
	if runErr != nil || code != 0 {
		_ = conn.Close()
		log.WriteString(out)
		finish(code, orExit(runErr, code, "versionsabfrage"))
		return
	}
	found := parseAgentVersions(server.PackageManager, out)
	plan := planAgentUpdate(found, opts.Limit)
	describeAgentPlan(&log, found, plan, opts.Limit)

	switch agentcaps.Action(plan.Action) {
	case agentcaps.ActionCurrent:
		_ = conn.Close()
		say("Nichts zu tun: Der Agent steht auf %s, der höchsten Version, die zu diesem LCM (%s) passt.", plan.Installed, version.Version)
		finish(0, nil)
		return
	case agentcaps.ActionNewer:
		_ = conn.Close()
		say("Nichts zu tun: Der Agent (%s) ist neuer als dieses LCM (%s). Ein Agent-Rückbau ist nicht vorgesehen - aktualisiere LCM.", plan.Installed, version.Version)
		finish(0, nil)
		return
	case agentcaps.ActionNone:
		_ = conn.Close()
		finish(1, errors.New(noVersionMessage(plan)))
		return
	}
	if plan.Action == agentActionUnsupported {
		_ = conn.Close()
		finish(1, errors.New(unsupportedMessage(plan)))
		return
	}

	installScript, err := agentInstallScriptFor(server.PackageManager, plan.Target)
	if err != nil {
		_ = conn.Close()
		finish(1, err)
		return
	}
	say("Installiere lcm-agent %s (bisher %s) ...", plan.Target, plan.Installed)
	// Ein neuer Versuch: Ein alter Rollback-Befund gilt ab hier nicht mehr, sonst
	// läse das Warten unten ihn als Scheitern dieses Versuchs.
	if err := s.servers.UpdateFields(server.ID, map[string]any{"agent_rollback": "", "agent_rollback_reason": ""}); err != nil {
		finish(1, fmt.Errorf("rollback-befund nicht zurückgesetzt: %w", err))
		return
	}
	started := time.Now()
	installOut, installCode, installErr := conn.Run(privRun(server, installScript))
	// Vor dem Warten schließen: Der Agent wechselt jetzt sein Binary, die
	// Verbindung bricht dabei kurz ab, und der Ausführungs-Platz des Servers soll
	// für die Prüfung danach frei sein.
	_ = conn.Close()
	log.WriteString(installOut)
	if !strings.HasSuffix(installOut, "\n") {
		log.WriteString("\n")
	}
	if installErr != nil || installCode != 0 {
		finish(installCode, orExit(installErr, installCode, "installation"))
		return
	}
	if err := checkInstalledAgent(server.PackageManager, installOut, plan.Target); err != nil {
		finish(1, err)
		return
	}
	say("Installiert. Der Agent wechselt auf das neue Programm, sobald er nichts mehr zu tun hat.")

	if err := s.awaitAgentVersion(job, server, plan.Target, started, &log); err != nil {
		var rolled *agentRolledBackError
		if errors.As(err, &rolled) {
			s.realignAgentPackage(server, rolled.running, found.Available, &log)
		}
		finish(1, err)
		return
	}
	if err := s.probeAgent(server); err != nil {
		say("Der Agent meldet sich mit %s, antwortet aber nicht auf Kommandos: %v", plan.Target, err)
		finish(1, fmt.Errorf("agent antwortet nicht auf kommandos: %w", err))
		return
	}
	say("Der Agent läuft mit %s und antwortet auf Kommandos.", plan.Target)
	finish(0, nil)
}

// orExit macht aus Fehler oder Exit-Code den Fehler des Jobs.
func orExit(err error, code int, step string) error {
	if err != nil {
		return err
	}
	return fmt.Errorf("%s endete mit exit-code %d", step, code)
}

// checkInstalledAgent prüft die Rückmeldung der Installation: Meldet das Skript
// eine andere Version als das Ziel, hat die Paketverwaltung nichts getauscht
// (etwa ein Schloss, das sie ohne Fehler überging). Ohne Meldung gilt es nicht.
func checkInstalledAgent(mgr, installOut, target string) error {
	var installed string
	for _, line := range strings.Split(installOut, "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), agentMarkInstalled); ok {
			installed = agentCanonVersion(pkgFamily(mgr), rest)
		}
	}
	if installed == "" || agentcaps.SameVersion(installed, target) {
		return nil
	}
	return fmt.Errorf("die paketverwaltung meldet nach der installation lcm-agent %s statt %s", installed, target)
}

// allowedAgentVersions beschreibt, welche Agent-Versionen LCM der Version lcm
// zulässt (siehe agentcaps.PickTarget).
func allowedAgentVersions(lcm string) string {
	if agentcaps.IsPrerelease(lcm) {
		return "bis " + lcm
	}
	parts := strings.SplitN(lcm, ".", 3)
	if len(parts) < 2 {
		return "bis " + lcm
	}
	return "bis zur Reihe " + parts[0] + "." + parts[1] + " samt aller Bugfixes"
}

// describeAgentPlan schreibt ins Protokoll, was LCM gesehen und gewählt hat.
func describeAgentPlan(log *strings.Builder, found agentVersions, plan AgentUpdatePlan, limit string) {
	fmt.Fprintf(log, "LCM %s, zulässige Agent-Versionen: %s\n", version.Version, allowedAgentVersions(version.Version))
	if limit != "" {
		fmt.Fprintf(log, "Gestaffelt: höchstens %s (Stand des ersten Servers)\n", limit)
	}
	fmt.Fprintf(log, "Installiert: %s\n", orDash(found.Installed))
	if found.RefreshFailed {
		fmt.Fprintf(log, "Hinweis: Die Paketlisten ließen sich nicht ganz erneuern (Exit-Code %s) - es gilt der Stand der Listen.\n", found.refreshExitRaw)
	}
	if found.EnterprisePin {
		log.WriteString("Hinweis: Auf diesem Server sperrt die Enterprise-Vorrang-Regel auch lcm-agent (ältere Einrichtung) - das Update kann scheitern.\n")
	}
	if len(found.Available) == 0 {
		log.WriteString("Verfügbar: nichts - keine Paketquelle des Servers führt lcm-agent.\n")
	}
	for _, v := range found.Available {
		fmt.Fprintf(log, "Verfügbar: %s (%s)\n", v, found.Sources[v])
	}
	if plan.Target != "" {
		fmt.Fprintf(log, "Zielversion: %s\n", plan.Target)
	}
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func noVersionMessage(plan AgentUpdatePlan) string {
	if plan.Reason == agentReasonUnknownVersion {
		return "diese LCM-Version ist ein Entwicklungsstand - LCM kann keine passende Agent-Version bestimmen"
	}
	return "die Paketquellen des Servers führen keine lcm-agent-Version, die zu diesem LCM (" + version.Version + ") passt"
}

func unsupportedMessage(plan AgentUpdatePlan) string {
	if plan.Reason == agentReasonNoManager {
		return "LCM findet auf diesem Server keine Paketverwaltung, mit der es den Agent aktualisieren kann (apt, dnf, zypper, apk oder pacman fehlen oder passen nicht zum erkannten System)"
	}
	return "der Agent ist auf diesem Server nicht als Paket installiert (z. B. von Hand kopiert) - er lässt sich nicht über die Paketverwaltung aktualisieren"
}

// awaitAgentVersion wartet, bis sich der Agent mit der Zielversion zurückmeldet.
// Meldet er stattdessen einen Rollback, scheitert das Update mit genau dieser
// Aussage; kommt nichts, mit der Zeitüberschreitung.
func (s *ServerService) awaitAgentVersion(job *domain.Job, server *domain.Server, target string, started time.Time, log *strings.Builder) error {
	deadline := time.Now().Add(AgentUpdateMaxWait)
	for {
		s.jobs.MarkActivity(job.ID)
		fresh, err := s.servers.FindByID(repositories.ScopeAll(), server.ID)
		if err == nil && fresh.AgentReportedAt != nil && fresh.AgentReportedAt.After(started) {
			switch {
			case agentcaps.SameVersion(fresh.AgentVersion, target):
				fmt.Fprintf(log, "Der Agent hat sich mit %s zurückgemeldet.\n", fresh.AgentVersion)
				return nil
			case fresh.AgentRollback != "":
				why := rollbackReasonText(fresh.AgentRollbackReason)
				fmt.Fprintf(log, "Der Agent hat sich mit %s zurückgemeldet und meldet einen Rollback: Version %s %s, er läuft wieder auf dem vorigen Programm.\n",
					fresh.AgentVersion, fresh.AgentRollback, why)
				return &agentRolledBackError{failed: fresh.AgentRollback, why: why, running: fresh.AgentVersion}
			}
		}
		if !time.Now().Before(deadline) {
			fmt.Fprintf(log, "Keine Rückmeldung mit Version %s innerhalb von %s.\n", target, AgentUpdateMaxWait)
			return fmt.Errorf("der agent hat sich nach %s nicht mit version %s zurückgemeldet - offene konsolen halten den wechsel bis zu 30 minuten auf, ältere agenten (bis 2.3.0) können einen rollback nicht melden, oder der agent ist nicht mehr erreichbar", AgentUpdateMaxWait, target)
		}
		time.Sleep(AgentUpdatePoll)
	}
}

// agentRolledBackError: Der Agent ist nach dem Update auf das vorige Programm
// zurückgefallen.
type agentRolledBackError struct {
	failed, why, running string
}

func (e *agentRolledBackError) Error() string {
	return fmt.Sprintf("update zurückgerollt: lcm-agent %s %s, der Agent läuft wieder auf %s", e.failed, e.why, e.running)
}

// realignAgentPackage bringt nach einem Rollback das Paket auf die Version, die
// wieder läuft. Sonst führt die Paketverwaltung die gescheiterte Version samt
// Sperre auf sie, und ein neuer Versuch hielte den Agent für aktuell. Steht die Version nicht
// mehr in der Quelle, bleibt der Paketstand, wie er ist (der Agent läuft
// trotzdem, nur ein neuer Versuch braucht dann eine neuere Version).
func (s *ServerService) realignAgentPackage(server *domain.Server, running string, available []string, log *strings.Builder) {
	target := ""
	for _, v := range available {
		if agentcaps.SameVersion(v, running) {
			target = v
		}
	}
	if target == "" && pkgFamily(server.PackageManager) == pkgPacman {
		// Die Datenbank des Arch-Repos führt nur die neueste Version; die ältere
		// Datei liegt im Verzeichnis des Repos, pacman -U holt sie von dort. Die
		// Schreibweise prüft agentInstallScriptFor.
		target = strings.Replace(version.Core(running), "-", "~", 1)
	}
	if target == "" {
		fmt.Fprintf(log, "Die laufende Version %s steht nicht in der Paketquelle - der Paketstand bleibt auf der gescheiterten Version.\n", version.Core(running))
		return
	}
	// Die laufende Version meldet der Agent selbst: Das Skript nimmt nur eine
	// gültige Paketversion an.
	script, err := agentInstallScriptFor(server.PackageManager, target)
	if err != nil {
		fmt.Fprintf(log, "Nicht möglich: %v - der Paketstand bleibt, wie er ist.\n", err)
		return
	}
	fmt.Fprintf(log, "Gleiche das Paket an das laufende Programm an: lcm-agent %s ...\n", target)
	conn, err := s.connect(server)
	if err != nil {
		fmt.Fprintf(log, "Nicht möglich, keine Verbindung: %v\n", err)
		return
	}
	defer conn.Close()
	out, code, err := conn.Run(privRun(server, script))
	log.WriteString(out)
	if err != nil || code != 0 {
		fmt.Fprintf(log, "Angleichen gescheitert (%v) - der Agent läuft weiter auf %s.\n", orExit(err, code, "installation"), version.Core(running))
		return
	}
	fmt.Fprintf(log, "Paket wieder auf %s. „Erneut versuchen“ installiert die neue Version noch einmal.\n", target)
}

// rollbackReasonText sagt, warum der Agent zurückgerollt ist.
func rollbackReasonText(reason string) string {
	switch reason {
	case "crash-loop":
		return "stürzte wiederholt ab"
	case "not-ready":
		return "wurde nicht rechtzeitig bereit"
	}
	return "ließ sich nicht verbinden"
}

// probeAgent schickt ein Kommando über den (neuen) Agent: Er ist verbunden und
// hat sein Abonnement - jetzt muss er auch antworten.
func (s *ServerService) probeAgent(server *domain.Server) error {
	conn, err := s.connect(server)
	if err != nil {
		return err
	}
	defer conn.Close()
	out, code, err := conn.Run("echo lcm-agent-ok")
	if err != nil {
		return err
	}
	if code != 0 || !strings.Contains(out, "lcm-agent-ok") {
		return fmt.Errorf("unerwartete antwort (exit-code %d)", code)
	}
	return nil
}
