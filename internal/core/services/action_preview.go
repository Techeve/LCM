package services

import (
	"errors"
	"strings"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

// Befehlsvorschau der Server-Aktionen (GET /servers/:id/actions/:action/preview).
//
// Die Vorschau ruft dieselben planX-Schritte auf wie der Lauf (siehe
// action_plan.go) und liest den Paketstand aus der Datenbank - es gibt keine
// SSH-Verbindung und keinen Job. Was sie zeigt, ist das Skript, das der Lauf
// anschließend unverändert an privRun übergibt, nur ohne die Funktions-
// definitionen der Sperrenlogik und mit maskierten Geheimnissen.

// Namen der Aktionen in der Vorschau-Route. Sie folgen den Pfaden der
// eigentlichen Aktions-Endpunkte (packages/upgrade-all, snaps/refresh-all, …).
const (
	PreviewRefresh         = "refresh"          // POST packages/refresh
	PreviewUpgradeAll      = "upgrade-all"      // POST packages/upgrade-all
	PreviewUpgradeSecurity = "upgrade-security" // POST packages/upgrade-security
	PreviewUpdate          = "update"           // POST packages/update
	PreviewRemove          = "remove"           // POST packages/remove
	PreviewAutoremove      = "autoremove"       // POST packages/autoremove
	PreviewRepair          = "repair"           // POST packages/repair
	PreviewKernelsCleanup  = "kernels-cleanup"  // POST kernels/cleanup
	PreviewReboot          = "reboot"           // POST reboot
	PreviewRetry           = "retry"            // POST capabilities/:category/retry
	PreviewSnapsRefreshAll = "snaps-refresh-all"
	PreviewSnapsRefresh    = "snaps-refresh"
	PreviewSnapsRemove     = "snaps-remove"
	PreviewCustom          = "custom" // Custom-Aktion (läuft über Gruppen-Regeln)
)

// Neustart-Einschätzung der Vorschau.
const (
	RebootNo    = "no"
	RebootMaybe = "maybe"
	RebootYes   = "yes"
)

// Wer das Kommando ausführt.
const (
	previewAsRoot  = "root"         // über privRun: root bzw. sudo
	previewAsLogin = "service_user" // direkt als Service-User, ohne Rechte-Erhöhung
)

// maxPreviewSeconds deckelt die Dauer-Schätzung: Alles darüber ist Ratespiel.
const maxPreviewSeconds = 3600

var (
	// ErrUnknownPreviewAction: Zu diesem Namen gibt es keine Vorschau.
	ErrUnknownPreviewAction = errors.New("unbekannte aktion für die befehlsvorschau")
	// ErrPreviewUnavailable: Die Befehle stehen vorher nicht fest.
	ErrPreviewUnavailable = errors.New("die befehle dieser aktion stehen vorher nicht fest")
	// ErrCustomActionNotFound: Die Custom-Aktion gibt es nicht.
	ErrCustomActionNotFound = errors.New("custom-aktion nicht gefunden")
)

// PreviewArgs sind die Argumente der Aktion (Query-Parameter der Route).
type PreviewArgs struct {
	Packages []string // Paket- bzw. Snap-Namen
	Version  string   // exakte Version (nur update)
	Kind     string   // Reparatur: RepairDpkgConfigure | RepairFixDeps
	Category string   // retry: Kategorie der Betriebsfähigkeit
	CustomID uint     // custom: ID der Custom-Aktion

	// Konfigurations-Aktionen (action_preview_server.go).
	Port          int               // ssh-port: neuer Port
	FirewallRules string            // firewall-apply: Regeln als JSON; leer = gespeicherter Stand
	SSHSources    string            // firewall-apply: Quell-Einschränkung der SSH-Freigabe als JSON
	SecurityTool  SecurityToolInput // security-tool(-manage): Werkzeug und Optionen
	ToolAction    string            // security-tool-manage: start, stop, uninstall, …
	UnbanIP       string            // security-tool-manage unban
	URIs          []string          // repos-revert-https: Auswahl; leer = alle Kandidaten
	RepoKey       string            // repos-add: Schlüssel der Katalog-Quelle
	CacheID       uint              // apt-cache-connect: Cache; 0 = der einzige
	Servers       []string          // dns, ntp: Name- bzw. Zeitserver
	Timezone      string            // timezone
	Paths         []HardenTarget    // path-harden
	PathID        uint              // path-restore: ID der Härtung
	Bouncer       bool              // lcm-host-crowdsec-lapi: Firewall-Bouncer mitinstallieren
	Project       string            // docker-compose-update: Compose-Projekt
	Service       string            // docker-compose-update: einzelner Service (optional)
	Image         string            // docker-pull, docker-image-delete: Image-Referenz
	Username      string            // user-lock, user-unlock, user-remove
}

// PreviewCommand ist ein Kommando, das die Aktion absetzt.
type PreviewCommand struct {
	Run string `json:"run"` // Geheimnisse maskiert
	As  string `json:"as"`  // root | service_user
	// When ist leer, wenn der Schritt immer läuft; sonst die Bedingung als
	// Code: if_needed (nur wenn der Lauf es auf dem Server für nötig befindet)
	// oder on_failure (nur als Rollback, wenn ein vorheriger Schritt scheitert).
	When string `json:"when,omitempty"`
}

// PreviewPackage ist ein vom Lauf betroffenes Paket.
type PreviewPackage struct {
	Name string `json:"name"`
	From string `json:"from,omitempty"` // installierte Version
	To   string `json:"to,omitempty"`   // Zielversion; leer beim Entfernen
}

// PreviewDuration ist eine grobe Schätzung, siehe roughDuration.
type PreviewDuration struct {
	MinSeconds int `json:"min_seconds"`
	MaxSeconds int `json:"max_seconds"`
}

// PreviewNote ist ein Hinweis als Code, den die Oberfläche übersetzt.
type PreviewNote struct {
	Code   string         `json:"code"`
	Params map[string]any `json:"params"`
}

// ActionPreview beschreibt, was eine Aktion auf dem Server ausführen würde.
type ActionPreview struct {
	Action     string           `json:"action"`
	Commands   []PreviewCommand `json:"commands"`
	Packages   []PreviewPackage `json:"packages"`
	Services   []string         `json:"services,omitempty"` // LCM kennt vor dem Lauf keine Dienste; bleibt leer
	Reboot     string           `json:"reboot"`
	Reversible bool             `json:"reversible"`
	Duration   PreviewDuration  `json:"duration"`
	Notes      []PreviewNote    `json:"notes"`

	steps []previewStep // die Skripte unmaskiert, für den Abgleich mit dem Lauf
}

// previewStep ist ein Skript so, wie es dem Lauf übergeben wird.
type previewStep struct {
	script string
	root   bool
}

// PreviewAction liefert die Befehlsvorschau einer Aktion. Der Zugriff wird wie
// bei jedem Server-Lesezugriff über den Scope geprüft.
func (s *ServerService) PreviewAction(scope repositories.AccessScope, id uint, action string, args PreviewArgs) (*ActionPreview, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return nil, err
	}
	switch action {
	case PreviewRefresh:
		return s.previewRefresh(server)
	case PreviewUpgradeAll:
		return s.previewUpgrade(server, action, s.planUpgradeAll, false)
	case PreviewUpgradeSecurity:
		return s.previewUpgrade(server, action, s.planUpgradeSecurity, true)
	case PreviewUpdate:
		return s.previewUpdate(server, args)
	case PreviewRemove:
		return s.previewRemove(server, args)
	case PreviewAutoremove:
		return s.previewAutoremove(server)
	case PreviewRepair:
		return s.previewRepair(server, args.Kind)
	case PreviewKernelsCleanup:
		return s.previewKernelsCleanup(server)
	case PreviewReboot:
		return previewReboot(server)
	case PreviewRetry:
		return s.previewRetry(server, args.Category)
	case PreviewSnapsRefreshAll, PreviewSnapsRefresh, PreviewSnapsRemove:
		return s.previewSnaps(server, action, args.Packages)
	case PreviewCustom:
		return s.previewCustom(server, args.CustomID)
	case PreviewFirewallApply, PreviewFirewallDisable:
		return s.previewFirewall(server, action == PreviewFirewallApply, args)
	case PreviewSSHPort:
		return s.previewSSHPort(server, args.Port)
	case PreviewRootLoginLock, PreviewRootLoginUnlock:
		return previewRootLogin(server, action == PreviewRootLoginLock)
	case PreviewHardenSSH, PreviewUnhardenSSH:
		return previewHardenSSH(server, action == PreviewHardenSSH)
	case PreviewSecurityTool:
		return s.previewSecurityTool(server, args.SecurityTool)
	case PreviewSecurityManage:
		return s.previewManageSecurityTool(server, SecurityToolManageInput{
			Tool: args.SecurityTool.Tool, Action: args.ToolAction, UnbanIP: args.UnbanIP,
			AllowlistIPs: args.SecurityTool.AllowlistIPs, AllowlistIDs: args.SecurityTool.AllowlistIDs,
		})
	case PreviewSSH2FAEnable, PreviewSSH2FADisable:
		return previewSSH2FA(server, action == PreviewSSH2FAEnable)
	case PreviewReposSecure:
		return previewSecureRepos(server)
	case PreviewReposRevert:
		return previewRevertRepos(server, args.URIs)
	case PreviewReposAdd:
		return s.previewAddRepo(server, args.RepoKey)
	case PreviewAptCacheOn, PreviewAptCacheOff:
		return s.previewAptProxy(server, action == PreviewAptCacheOn, args.CacheID)
	case PreviewDNS:
		return s.previewDNS(server, args.Servers)
	case PreviewTimezone:
		return previewTimezone(server, args.Timezone)
	case PreviewNTP:
		return previewNTP(server, args.Servers)
	case PreviewPinsApply:
		return s.previewApplyPins(server)
	case PreviewDeepScanTools:
		return s.previewPackageInstall(server, action, s.planDeepScanTools, "needrestart", "lynis")
	case PreviewACLInstall:
		return s.previewPackageInstall(server, action, s.planACLSupport, "acl")
	case PreviewPathHarden:
		return previewHardenPaths(server, args.Paths)
	case PreviewPathRestore:
		return s.previewRestorePath(server, args.PathID)
	case PreviewHostTrivy, PreviewHostSandbox, PreviewHostAptCacher, PreviewHostCrowdSec:
		return s.previewLcmHost(server, action, args.Bouncer)
	case PreviewDockerCompose, PreviewDockerPull, PreviewDockerPullAll, PreviewDockerRemove, PreviewDockerPrune:
		return s.previewDocker(server, action, args)
	case PreviewUserLock, PreviewUserUnlock, PreviewUserRemove:
		return s.previewServerUser(server, action, args.Username)
	}
	return nil, ErrUnknownPreviewAction
}

// commandsFor liefert die Kommandos so, wie der Lauf sie an die SSH-Verbindung
// übergibt (nach privRun, unmaskiert). Nur für Tests, die Vorschau und Lauf
// vergleichen.
func (p *ActionPreview) commandsFor(server *domain.Server) []string {
	out := make([]string, 0, len(p.steps))
	for _, st := range p.steps {
		if st.root {
			out = append(out, privRun(server, st.script))
		} else {
			out = append(out, st.script)
		}
	}
	return out
}

// newActionPreview legt eine Vorschau mit den Hinweisen an, die für jeden
// Lauf auf diesem Server gelten.
func newActionPreview(action string, server *domain.Server) *ActionPreview {
	p := &ActionPreview{
		Action:   action,
		Commands: []PreviewCommand{},
		Packages: []PreviewPackage{},
		Notes:    []PreviewNote{},
		Reboot:   RebootNo,
	}
	if server.IsDemo {
		p.note("preview.demo_server")
	}
	if server.RestrictedSudo {
		p.note("preview.restricted_sudo")
	}
	return p
}

// note hängt einen Hinweis an; params sind Schlüssel-Wert-Paare.
func (p *ActionPreview) note(code string, params ...any) {
	n := PreviewNote{Code: code, Params: map[string]any{}}
	for i := 0; i+1 < len(params); i += 2 {
		n.Params[params[i].(string)] = params[i+1]
	}
	for _, have := range p.Notes {
		if have.Code == n.Code && len(n.Params) == 0 {
			return
		}
	}
	p.Notes = append(p.Notes, n)
}

// addRoot nimmt ein Skript auf, das der Lauf über privRun ausführt.
func (p *ActionPreview) addRoot(script string) { p.add(script, true) }

// addLogin nimmt ein Kommando auf, das der Lauf ohne privRun ausführt.
func (p *ActionPreview) addLogin(cmd string) { p.add(cmd, false) }

func (p *ActionPreview) add(script string, root bool) {
	shown, wrapped := displayScript(script)
	masked := maskPreviewSecrets(shown)
	as := previewAsLogin
	if root {
		as = previewAsRoot
	}
	p.steps = append(p.steps, previewStep{script: script, root: root})
	p.Commands = append(p.Commands, PreviewCommand{Run: masked, As: as})
	if wrapped {
		p.note("preview.apt_wrapper")
	}
	if masked != shown {
		p.note("preview.secrets_masked")
	}
}

// previewHidden sind die Funktionsdefinitionen, die jedes apt-Skript
// vorangestellt bekommt. Sie sind Mechanik (Sperren abwarten), keine Aktion.
var previewHidden = []string{aptPrelude, aptHeldBackFunc + aptHeldBackReport}

// displayScript entfernt die Funktionsdefinitionen aus dem Skript. Der zweite
// Rückgabewert sagt, ob der apt-Vorspann dabei war - dann stehen im Rest die
// Aufrufe lcm_apt/lcm_apt_update, die die Oberfläche erklären muss.
func displayScript(script string) (string, bool) {
	wrapped := strings.Contains(script, aptPrelude)
	for _, h := range previewHidden {
		script = strings.Replace(script, h, "", 1)
	}
	return strings.TrimSpace(script), wrapped
}

// roughDuration schätzt die Dauer aus einer Grundzeit und einer Zeit je Paket
// (jeweils Minimum und Maximum). Die Zahlen sind Erfahrungswerte, keine
// Messung: ein Server mit schnellem Netz und SSD liegt nahe am Minimum, ein
// Raspberry Pi hinter einem langsamen Cache am Maximum.
func roughDuration(baseMin, perMin, baseMax, perMax, n int) PreviewDuration {
	return PreviewDuration{
		MinSeconds: min(baseMin+perMin*n, maxPreviewSeconds),
		MaxSeconds: min(baseMax+perMax*n, maxPreviewSeconds),
	}
}

// pendingPackages liefert die Updates, die der Server gemeldet hat und die ein
// Upgrade tatsächlich mitnimmt (ohne zurückgehaltene).
func (s *ServerService) pendingPackages(server *domain.Server) ([]domain.Package, error) {
	outdated, err := s.servers.FindOutdatedPackages(server.ID)
	if err != nil {
		return nil, err
	}
	pending := outdated[:0]
	for _, pkg := range outdated {
		if pkg.HeldReason == "" {
			pending = append(pending, pkg)
		}
	}
	return pending, nil
}

// upgradePreviewPackages übersetzt Updates in die Anzeige (installiert → neu).
func upgradePreviewPackages(pkgs []domain.Package) []PreviewPackage {
	out := make([]PreviewPackage, 0, len(pkgs))
	for _, pkg := range pkgs {
		out = append(out, PreviewPackage{Name: pkg.Name, From: pkg.Version, To: pkg.CandidateVersion})
	}
	return out
}

// rebootForPackages schätzt, ob die Updates einen Neustart brauchen: Kernel und
// Microcode wirken erst nach einem Neustart („yes"), Bibliotheken und
// Systemdienste teils („maybe"). Alles andere braucht keinen.
func rebootForPackages(pkgs []PreviewPackage) string {
	result := RebootNo
	for _, pkg := range pkgs {
		switch {
		case hasAnyPrefix(pkg.Name, "linux-image", "linux-generic", "linux-lts", "linux-virtual",
			"kernel", "intel-microcode", "amd64-microcode", "linux-firmware"):
			return RebootYes
		case hasAnyPrefix(pkg.Name, "libc6", "glibc", "systemd", "dbus"):
			result = RebootMaybe
		}
	}
	return result
}

func hasAnyPrefix(s string, prefixes ...string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

func (s *ServerService) previewRefresh(server *domain.Server) (*ActionPreview, error) {
	plan, err := s.planRefresh(server)
	if err != nil {
		return nil, err
	}
	p := newActionPreview(PreviewRefresh, server)
	p.addRoot(plan.script)
	p.Reversible = true // ändert nur die Paketlisten, kein installiertes Paket
	p.Duration = roughDuration(10, 0, 120, 0, 0)
	return p, nil
}

// previewUpgrade deckt „alle Updates" und „nur Security" ab.
func (s *ServerService) previewUpgrade(server *domain.Server, action string, plan func(*domain.Server) (packagePlan, error), securityOnly bool) (*ActionPreview, error) {
	pl, err := plan(server)
	if err != nil {
		return nil, err
	}
	pending, err := s.pendingPackages(server)
	if err != nil {
		return nil, err
	}
	p := newActionPreview(action, server)
	p.addRoot(pl.script)
	if securityOnly && !hasSecurityChannel(server.PackageManager) {
		// pacman und apk kennen keinen Security-Kanal: Der Lauf ist ein Voll-Update.
		p.note("preview.no_security_channel")
	} else if securityOnly {
		pending = onlySecurity(pending)
	}
	p.Packages = upgradePreviewPackages(pending)
	p.Reboot = rebootForPackages(p.Packages)
	p.Duration = roughDuration(20, 2, 120, 30, len(pending))
	p.note("preview.restart_services")
	if len(pending) == 0 {
		p.note("preview.nothing_pending")
	}
	return p, nil
}

// hasSecurityChannel meldet, ob die Paketverwaltung Security-Updates getrennt
// einspielen kann (siehe pkgSecurityUpgradeScript).
func hasSecurityChannel(mgr string) bool {
	family := pkgFamily(mgr)
	return family != pkgPacman && family != pkgApk
}

func onlySecurity(pkgs []domain.Package) []domain.Package {
	out := make([]domain.Package, 0, len(pkgs))
	for _, pkg := range pkgs {
		if pkg.Security {
			out = append(out, pkg)
		}
	}
	return out
}

// installedPackages liefert die genannten Pakete aus dem gespeicherten Bestand
// und die Namen, die der Server nicht kennt.
func (s *ServerService) installedPackages(server *domain.Server, names []string) (found []domain.Package, missing []string, err error) {
	all, err := s.servers.FindPackages(server.ID)
	if err != nil {
		return nil, nil, err
	}
	byName := make(map[string]domain.Package, len(all))
	for _, pkg := range all {
		byName[pkg.Name] = pkg
	}
	for _, name := range names {
		if pkg, ok := byName[name]; ok {
			found = append(found, pkg)
		} else {
			missing = append(missing, name)
		}
	}
	return found, missing, nil
}

func (s *ServerService) previewUpdate(server *domain.Server, args PreviewArgs) (*ActionPreview, error) {
	plan, err := s.planUpdate(server, args.Packages, args.Version)
	if err != nil {
		return nil, err
	}
	names, _ := parsePackageNames(strings.Join(args.Packages, " "))
	found, missing, err := s.installedPackages(server, names)
	if err != nil {
		return nil, err
	}
	p := newActionPreview(PreviewUpdate, server)
	p.addRoot(plan.script)
	for _, pkg := range found {
		to := pkg.CandidateVersion
		if args.Version != "" {
			to = args.Version
		}
		p.Packages = append(p.Packages, PreviewPackage{Name: pkg.Name, From: pkg.Version, To: to})
	}
	for _, name := range missing {
		p.note("preview.package_not_installed", "name", name)
	}
	if args.Version != "" {
		p.note("preview.exact_version", "version", args.Version)
	}
	p.Reboot = rebootForPackages(p.Packages)
	p.Duration = roughDuration(10, 2, 60, 30, len(names))
	p.note("preview.restart_services")
	return p, nil
}

func (s *ServerService) previewRemove(server *domain.Server, args PreviewArgs) (*ActionPreview, error) {
	plan, err := s.planRemove(server, args.Packages)
	if err != nil {
		return nil, err
	}
	names, _ := parsePackageNames(strings.Join(args.Packages, " "))
	found, missing, err := s.installedPackages(server, names)
	if err != nil {
		return nil, err
	}
	p := newActionPreview(PreviewRemove, server)
	p.addRoot(plan.script)
	for _, pkg := range found {
		p.Packages = append(p.Packages, PreviewPackage{Name: pkg.Name, From: pkg.Version})
	}
	for _, name := range missing {
		p.note("preview.package_not_installed", "name", name)
	}
	p.Duration = roughDuration(10, 2, 60, 15, len(names))
	p.note("preview.config_kept")
	return p, nil
}

func (s *ServerService) previewAutoremove(server *domain.Server) (*ActionPreview, error) {
	plan, err := s.planAutoremove(server)
	if err != nil {
		return nil, err
	}
	p := newActionPreview(PreviewAutoremove, server)
	p.addRoot(plan.script)
	// Welche Pakete verwaist sind, entscheidet die Paketverwaltung erst im Lauf.
	p.note("preview.autoremove_set_unknown")
	if pins := s.effectivePins(server); len(pins) > 0 {
		p.note("preview.pins_enforced", "count", len(pins))
	}
	p.Duration = roughDuration(10, 0, 300, 0, 0)
	return p, nil
}

func (s *ServerService) previewRepair(server *domain.Server, kind string) (*ActionPreview, error) {
	plan, err := s.planRepair(server, kind)
	if err != nil {
		return nil, err
	}
	p := newActionPreview(PreviewRepair, server)
	p.addRoot(plan.script)
	p.Duration = roughDuration(10, 0, 600, 0, 0)
	return p, nil
}

func (s *ServerService) previewKernelsCleanup(server *domain.Server) (*ActionPreview, error) {
	plan, releases, err := s.planKernelCleanup(server)
	if err != nil {
		return nil, err
	}
	p := newActionPreview(PreviewKernelsCleanup, server)
	p.addRoot(plan.script)
	p.note("preview.kernel_releases", "releases", strings.Join(releases, ", "))
	p.Duration = roughDuration(30, 10, 120, 60, len(releases))
	return p, nil
}

func previewReboot(server *domain.Server) (*ActionPreview, error) {
	if err := ensureRebootable(server); err != nil {
		return nil, err
	}
	p := newActionPreview(PreviewReboot, server)
	p.addRoot(rebootScript())
	p.Reboot = RebootYes
	maxWait := rebootMaxWait(server)
	p.Duration = PreviewDuration{MinSeconds: 20, MaxSeconds: int(maxWait.Seconds())}
	p.note("preview.reboot_downtime", "max_minutes", int(maxWait.Minutes()))
	return p, nil
}

func (s *ServerService) previewSnaps(server *domain.Server, action string, names []string) (*ActionPreview, error) {
	var plan packagePlan
	var err error
	switch action {
	case PreviewSnapsRefreshAll:
		plan, err = s.planSnapRefreshAll(server)
	case PreviewSnapsRefresh:
		plan, err = s.planSnapRefresh(server, names)
	default:
		plan, err = s.planSnapRemove(server, names)
	}
	if err != nil {
		return nil, err
	}
	snaps, err := s.servers.FindSnapPackages(server.ID)
	if err != nil {
		return nil, err
	}
	p := newActionPreview(action, server)
	p.addRoot(plan.script)
	p.Packages = snapPreviewPackages(snaps, action, names)
	p.Duration = roughDuration(10, 5, 60, 120, len(p.Packages))
	if action == PreviewSnapsRemove {
		// snapd legt vor dem Entfernen eine Momentaufnahme an (kein --purge).
		p.Reversible = true
		p.note("preview.snap_snapshot")
	}
	return p, nil
}

// snapPreviewPackages wählt die betroffenen Snaps: beim Aktualisieren aller
// die mit Update, sonst die genannten.
func snapPreviewPackages(snaps []domain.SnapPackage, action string, names []string) []PreviewPackage {
	wanted := map[string]bool{}
	for _, n := range names {
		wanted[strings.ToLower(strings.TrimSpace(n))] = true
	}
	out := []PreviewPackage{}
	for _, sn := range snaps {
		switch {
		case action == PreviewSnapsRefreshAll && sn.CandidateVersion != "" && sn.CandidateVersion != sn.Version:
			out = append(out, PreviewPackage{Name: sn.Name, From: sn.Version, To: sn.CandidateVersion})
		case action == PreviewSnapsRefresh && wanted[sn.Name]:
			out = append(out, PreviewPackage{Name: sn.Name, From: sn.Version, To: sn.CandidateVersion})
		case action == PreviewSnapsRemove && wanted[sn.Name]:
			out = append(out, PreviewPackage{Name: sn.Name, From: sn.Version})
		}
	}
	return out
}

// previewCustom zeigt die Command-Liste einer Custom-Aktion. Sie läuft über
// eine Gruppen-Regel auf dem Server; der Executor führt jedes Kommando einzeln
// über privRun aus (runActionCommands).
func (s *ServerService) previewCustom(server *domain.Server, id uint) (*ActionPreview, error) {
	if s.customActions == nil {
		return nil, ErrPreviewUnavailable
	}
	// Dieselben Sperren wie im Lauf: kein Gerät ohne Shell, kein
	// eingeschränkter Service-User.
	if err := ensureNotRouterOS(server); err != nil {
		return nil, err
	}
	if server.RestrictedSudo && !restrictedAllowsRule(domain.RuleTypeCustom) {
		return nil, ErrRestrictedSudo
	}
	action, err := s.customActions.FindByID(id)
	if errors.Is(err, repositories.ErrNotFound) {
		return nil, ErrCustomActionNotFound
	}
	if err != nil {
		return nil, err
	}
	p := newActionPreview(PreviewCustom, server)
	commands := parseActionCommands(action.Commands)
	for _, cmd := range commands {
		p.addRoot(cmd)
	}
	p.Duration = roughDuration(len(commands), 0, 300*len(commands), 0, 0)
	return p, nil
}

// previewRetry zeigt, was „Erneut versuchen" auslöst: dieselbe Regel nur für
// diesen Server oder die entsprechende Ad-hoc-Aktion (siehe startRetry).
func (s *ServerService) previewRetry(server *domain.Server, category string) (*ActionPreview, error) {
	c, err := s.retryTarget(server.ID, category)
	if err != nil {
		return nil, err
	}
	plan, err := s.retryPlanFor(c)
	if err != nil {
		return nil, err
	}
	// API-Geräte (RouterOS, DSM) laufen über ihre Web-API, nicht über eine Shell.
	if server.IsAPIDevice() {
		return nil, ErrPreviewUnavailable
	}
	p, err := s.previewRetryPlan(server, plan)
	if err != nil {
		return nil, err
	}
	p.Action = PreviewRetry
	return p, nil
}

func (s *ServerService) previewRetryPlan(server *domain.Server, plan retryPlan) (*ActionPreview, error) {
	if plan.rule != nil {
		return s.previewRule(server, plan.rule)
	}
	switch plan.jobType {
	case domain.RuleTypeUpdate:
		return s.previewUpgrade(server, PreviewUpgradeAll, s.planUpgradeAll, false)
	case domain.RuleTypeSecurity:
		return s.previewUpgrade(server, PreviewUpgradeSecurity, s.planUpgradeSecurity, true)
	case domain.RuleTypePackageScan:
		return s.previewRefresh(server)
	case domain.RuleTypeAutoremove:
		return s.previewAutoremove(server)
	case domain.RuleTypeHealth:
		return previewHealthCheck(server), nil
	}
	// Der CVE-Scan läuft auf dem LCM-Host, nicht auf dem Server.
	return nil, ErrPreviewUnavailable
}

// previewHealthCheck: „Erneut versuchen" der Verbindung ist der Health-Ping.
func previewHealthCheck(server *domain.Server) *ActionPreview {
	p := newActionPreview(PreviewRetry, server)
	p.addLogin(healthProbeCmd)
	p.Reversible = true
	p.Duration = roughDuration(1, 0, 30, 0, 0)
	p.note("preview.retry_connection_check")
	return p
}

// previewRule zeigt den Lauf einer Regel auf diesem Server, soweit ihr Kommando
// vorher feststeht: Paket-Regeln, Skripte und Custom-Aktionen.
func (s *ServerService) previewRule(server *domain.Server, rule *domain.Rule) (*ActionPreview, error) {
	if server.RestrictedSudo && !restrictedAllowsRule(rule.Type) {
		return nil, ErrRestrictedSudo
	}
	if script, ok := ruleScript(s.settings, s.servers, server, rule); ok {
		p := newActionPreview(PreviewRetry, server)
		p.addRoot(script)
		p.note("preview.restart_services")
		p.Duration = roughDuration(10, 0, 600, 0, 0)
		return p, nil
	}
	switch rule.Type {
	case domain.RuleTypeCustom:
		id, err := parseCustomActionID(rule.Command)
		if err != nil {
			return nil, ErrPreviewUnavailable
		}
		return s.previewCustom(server, id)
	case domain.RuleTypeScript:
		p := newActionPreview(PreviewRetry, server)
		p.addLogin(rule.Command)
		p.Duration = roughDuration(1, 0, 300, 0, 0)
		return p, nil
	case domain.RuleTypeHealth:
		return previewHealthCheck(server), nil
	}
	return nil, ErrPreviewUnavailable
}
