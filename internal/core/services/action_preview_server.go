package services

import (
	"strconv"
	"strings"

	"LCM/internal/core/domain"
)

// Befehlsvorschau der Konfigurations-Aktionen eines Servers (Firewall, SSH,
// Sicherheitswerkzeuge, Paketquellen, …). Wie bei den Paket-Aktionen ruft
// die Vorschau denselben planX-Schritt auf wie der Lauf. Viele dieser Läufe
// sind mehrstufig: Sie prüfen nach jedem Schritt und entscheiden erst auf dem
// Server, ob ein Schritt nötig ist (Werkzeug fehlt, Sperre greift nicht) oder
// ob zurückgerollt wird. Solche Schritte trägt die Vorschau mit `when` ein.

// Namen der Konfigurations-Aktionen in der Vorschau-Route.
const (
	PreviewFirewallApply   = "firewall-apply"   // POST firewall {enable:true}
	PreviewFirewallDisable = "firewall-disable" // POST firewall {enable:false}
	PreviewSSHPort         = "ssh-port"         // POST ssh-port
	PreviewRootLoginLock   = "ssh-root-login-lock"
	PreviewRootLoginUnlock = "ssh-root-login-unlock"
	PreviewHardenSSH       = "harden-ssh"           // POST harden-ssh
	PreviewUnhardenSSH     = "unharden-ssh"         // POST unharden-ssh
	PreviewSecurityTool    = "security-tool"        // POST security-tool (Einrichtung)
	PreviewSecurityManage  = "security-tool-manage" // POST security-tool/manage
	PreviewSSH2FAEnable    = "ssh-2fa-enable"       // POST ssh-2fa {enable:true}
	PreviewSSH2FADisable   = "ssh-2fa-disable"      // POST ssh-2fa {enable:false}
	PreviewReposSecure     = "repos-secure"         // POST repositories/secure
	PreviewReposRevert     = "repos-revert-https"   // POST repositories/revert-https
	PreviewReposAdd        = "repos-add"            // POST repositories/add
	PreviewAptCacheOn      = "apt-cache-connect"    // POST apt-proxy {enable:true}
	PreviewAptCacheOff     = "apt-cache-disconnect" // POST apt-proxy {enable:false}
	PreviewDNS             = "dns"                  // POST dns
	PreviewTimezone        = "timezone"             // POST timezone
	PreviewNTP             = "ntp"                  // POST ntp
	PreviewPinsApply       = "pins-apply"           // POST packages/pins/apply
	PreviewDeepScanTools   = "deep-scan-tools"      // POST deep-scan/install-tools
	PreviewACLInstall      = "acl-install"          // POST acl/install
	PreviewPathHarden      = "path-harden"          // POST hardened-paths(/bulk)
	PreviewPathRestore     = "path-restore"         // DELETE hardened-paths/:pathId
	PreviewHostTrivy       = "lcm-host-trivy"       // POST lcm-host/install-trivy
	PreviewHostSandbox     = "lcm-host-sandbox"     // POST lcm-host/install-sandbox
	PreviewHostAptCacher   = "lcm-host-apt-cacher"  // POST lcm-host/install-apt-cacher
	PreviewHostCrowdSec    = "lcm-host-crowdsec-lapi"
	PreviewDockerCompose   = "docker-compose-update" // POST docker/compose-update
	PreviewDockerPull      = "docker-pull"           // POST docker/pull
	PreviewDockerPullAll   = "docker-pull-all"       // POST docker/pull-all
	PreviewDockerRemove    = "docker-image-delete"   // POST docker/remove-image
	PreviewDockerPrune     = "docker-prune"          // POST docker/prune
	PreviewUserLock        = "user-lock"             // POST users/disable
	PreviewUserUnlock      = "user-unlock"           // POST users/enable
	PreviewUserRemove      = "user-remove"           // POST users/remove
)

// Bedingungen eines Schritts (PreviewCommand.When).
const (
	whenAlways    = ""
	whenIfNeeded  = "if_needed"  // nur, wenn der Lauf auf dem Server feststellt, dass es nötig ist
	whenOnFailure = "on_failure" // nur, wenn ein vorheriger Schritt scheitert (Rollback)
)

// addRootWhen nimmt ein Skript auf, das der Lauf nur unter einer Bedingung
// über privRun ausführt.
func (p *ActionPreview) addRootWhen(script, when string) {
	p.addRoot(script)
	p.Commands[len(p.Commands)-1].When = when
}

// firewallPreviewRules liefert die Regeln der Vorschau: die übergebenen oder,
// ohne Regeln in der Anfrage, den gespeicherten Stand („erneut anwenden").
func firewallPreviewRules(server *domain.Server, args PreviewArgs) (string, domain.FirewallSSHSources) {
	if args.FirewallRules == "" {
		return firewallRulesJSON(firewallRulesFromServer(server)), parseSSHSources(server.FirewallSSHSources)
	}
	return args.FirewallRules, parseSSHSources(args.SSHSources)
}

// previewFirewall zeigt den Lauf von runFirewallJob. Welches Werkzeug
// verwendet wird, erkennt erst der Lauf (firewallDetectCmd); die Vorschau
// nimmt das zuletzt erkannte bzw. das für die Distribution vorgesehene.
func (s *ServerService) previewFirewall(server *domain.Server, enable bool, args PreviewArgs) (*ActionPreview, error) {
	spec, sources := firewallPreviewRules(server, args)
	rules, err := s.planFirewall(server, enable, spec, sources)
	if err != nil {
		return nil, err
	}
	action := PreviewFirewallDisable
	if enable {
		action = PreviewFirewallApply
	}
	p := newActionPreview(action, server)
	p.addRoot(firewallDetectCmd)
	backend := firewallBackendFor(server)
	be := firewallBackendOf(server, backend)
	p.Reversible = true
	p.note("preview.firewall_backend", "backend", backend)
	if !enable {
		p.addRoot(be.disableScript())
		p.note("preview.firewall_disable_open")
		p.Duration = roughDuration(2, 0, 20, 0, 0)
		return p, nil
	}
	if server.FirewallTool == "" {
		_, install := resolveFirewallBackend(server, "")
		p.addRootWhen(install, whenIfNeeded)
		p.note("preview.firewall_install", "backend", backend)
	}
	ssh, applied, warnings, err := expandSSHAndRules(serverSSHRule(server), rules, s.ipAllowlistExpand)
	if err != nil {
		return nil, err
	}
	p.addRoot(be.enableScript(ssh, applied))
	p.addRoot(be.statusCmd)
	ports := strings.TrimSuffix(fmtPort(ssh.Port)+","+firewallRulesPortsCSV(applied), ",")
	p.note("preview.firewall_default_deny", "ports", ports)
	for _, w := range warnings {
		p.note("preview.firewall_warning", "detail", w)
	}
	p.Duration = roughDuration(5, 0, 180, 0, 0)
	return p, nil
}

func fmtPort(port int) string {
	if port == 0 {
		port = 22
	}
	return strconv.Itoa(port)
}

// previewSSHPort zeigt die Schritte von ChangeSSHPort in ihrer Reihenfolge.
func (s *ServerService) previewSSHPort(server *domain.Server, port int) (*ActionPreview, error) {
	plan, err := planSSHPort(server, port)
	if err != nil {
		return nil, err
	}
	p := newActionPreview(PreviewSSHPort, server)
	p.Reversible = true
	if plan.unchanged() {
		p.note("preview.ssh_port_unchanged", "port", port)
		return p, nil
	}
	if plan.firewallActive {
		p.addRoot(plan.firewallOpen)
	}
	p.addRoot(plan.transition)
	p.note("preview.ssh_port_verify", "port", plan.newPort)
	p.addRootWhen(plan.rollback, whenOnFailure)
	if plan.firewallActive {
		p.addRootWhen(plan.firewallClose, whenOnFailure)
	}
	p.addRoot(plan.final)
	if plan.firewallActive {
		script, _ := plan.firewallNarrow(server, s.ipAllowlistExpand)
		p.addRoot(script)
		p.note("preview.ssh_port_firewall", "old", plan.oldPort, "new", plan.newPort)
	}
	p.note("preview.ssh_port_reconnect", "old", plan.oldPort, "new", plan.newPort)
	p.Duration = roughDuration(5, 0, 60, 0, 0)
	return p, nil
}

// previewRootLogin zeigt die Schritte von SetSSHRootLogin.
func previewRootLogin(server *domain.Server, disabled bool) (*ActionPreview, error) {
	plan, err := planSSHRootLogin(server, disabled)
	if err != nil {
		return nil, err
	}
	action := PreviewRootLoginUnlock
	if disabled {
		action = PreviewRootLoginLock
	}
	p := newActionPreview(action, server)
	if plan.restore != "" {
		p.addRoot(plan.restore)
	}
	p.addRoot(plan.apply)
	p.addRoot(plan.verify)
	if plan.neutralize != "" {
		p.addRootWhen(plan.neutralize, whenIfNeeded)
		p.note("preview.root_login_neutralize")
	}
	p.Reversible = true
	if disabled {
		p.note("preview.root_login_locked")
		p.note("preview.root_login_lcm_unaffected")
	} else {
		p.note("preview.root_login_allowed")
	}
	p.note("preview.sessions_kept")
	p.Duration = roughDuration(2, 0, 20, 0, 0)
	return p, nil
}

// previewHardenSSH zeigt die SSH-Härtung bzw. ihre Aufhebung.
func previewHardenSSH(server *domain.Server, harden bool) (*ActionPreview, error) {
	plan, action := planUnhardenSSH, PreviewUnhardenSSH
	if harden {
		plan, action = planHardenSSH, PreviewHardenSSH
	}
	script, err := plan(server)
	if err != nil {
		return nil, err
	}
	p := newActionPreview(action, server)
	p.addRoot(script)
	p.Reversible = true
	p.note("preview.sessions_kept")
	if harden {
		// Im eingeschränkten Modus belegt der LCM-Helper die Wirkung selbst.
		if !server.RestrictedSudo {
			p.addRoot(passwordAuthProbe)
		}
		p.note("preview.ssh_keys_only")
	} else {
		p.note("preview.ssh_passwords_back")
	}
	p.Duration = roughDuration(2, 0, 20, 0, 0)
	return p, nil
}

// previewSecurityTool zeigt die Einrichtung von fail2ban bzw. CrowdSec. Die
// LCM-Quell-IP liest der Lauf erst auf dem Server; die Vorschau nimmt die
// zuletzt gelesene.
func (s *ServerService) previewSecurityTool(server *domain.Server, in SecurityToolInput) (*ActionPreview, error) {
	if err := s.planSecurityTool(server, &in); err != nil {
		return nil, err
	}
	script, warn, err := s.securityToolScript(server, in, server.LCMSourceIP)
	if err != nil {
		return nil, err
	}
	p := newActionPreview(PreviewSecurityTool, server)
	p.addLogin(lcmSourceIPCmd)
	p.addRoot(script)
	p.Reversible = true
	p.note("preview.security_tool_allowlist", "ip", server.LCMSourceIP)
	if warn != "" {
		p.note("preview.allowlist_unresolved")
	}
	if in.Tool == SecurityToolCrowdSec && in.Bouncer {
		p.note("preview.crowdsec_bouncer")
	}
	p.Duration = roughDuration(20, 0, 300, 0, 0)
	return p, nil
}

// previewManageSecurityTool zeigt eine Verwaltungs-Aktion. Der Lauf prüft
// vorher, ob das Werkzeug installiert ist, und bricht sonst ab.
func (s *ServerService) previewManageSecurityTool(server *domain.Server, in SecurityToolManageInput) (*ActionPreview, error) {
	if err := planManageSecurityTool(server, &in); err != nil {
		return nil, err
	}
	script, err := s.manageScript(server, in, server.LCMSourceIP)
	if err != nil {
		return nil, err
	}
	p := newActionPreview(PreviewSecurityManage, server)
	if in.Action == SecurityToolActionAllowlist {
		p.addLogin(lcmSourceIPCmd)
	}
	p.addRoot(script)
	p.note("preview.security_tool_installed_check", "tool", in.Tool)
	switch in.Action {
	case SecurityToolActionUninstall:
		p.note("preview.security_tool_uninstall", "tool", in.Tool)
		p.Duration = roughDuration(10, 0, 120, 0, 0)
	case SecurityToolActionStop, SecurityToolActionDisable:
		p.Reversible = true
		p.note("preview.security_tool_unprotected", "tool", in.Tool)
		p.Duration = roughDuration(1, 0, 20, 0, 0)
	default:
		p.Reversible = true
		p.Duration = roughDuration(1, 0, 20, 0, 0)
	}
	return p, nil
}

// previewSSH2FA zeigt Einrichtung bzw. Rückbau der SSH-Zwei-Faktor-Anmeldung.
func previewSSH2FA(server *domain.Server, enable bool) (*ActionPreview, error) {
	plan, err := planSSH2FA(server, enable)
	if err != nil {
		return nil, err
	}
	action := PreviewSSH2FADisable
	if enable {
		action = PreviewSSH2FAEnable
	}
	p := newActionPreview(action, server)
	p.addRoot(plan.script)
	p.Reversible = true
	p.note("preview.sessions_kept")
	if enable {
		p.note("preview.ssh_2fa_probe")
		p.addRootWhen(plan.rollback, whenOnFailure)
		p.note("preview.ssh_2fa_users")
	} else {
		p.note("preview.ssh_2fa_secrets_kept")
	}
	p.Duration = roughDuration(10, 0, 180, 0, 0)
	return p, nil
}

func (s *ServerService) previewSecureRepos(server *domain.Server, uris []string) (*ActionPreview, error) {
	script, targets, err := s.planSecureRepos(server, uris)
	if err != nil {
		return nil, err
	}
	p := newActionPreview(PreviewReposSecure, server)
	p.addRoot(script)
	p.Reversible = true
	if len(targets) > 0 {
		p.note("preview.repos_secure_targets", "urls", strings.Join(targets, ", "))
	}
	p.note("preview.repos_update_rollback")
	p.Duration = roughDuration(10, 0, 180, 0, 0)
	return p, nil
}

func previewRevertRepos(server *domain.Server, uris []string) (*ActionPreview, error) {
	script, targets, err := planRevertRepos(server, uris)
	if err != nil {
		return nil, err
	}
	p := newActionPreview(PreviewReposRevert, server)
	p.addRoot(script)
	p.Reversible = true
	p.note("preview.repos_revert_targets", "urls", strings.Join(targets, ", "))
	p.note("preview.repos_unencrypted")
	p.note("preview.repos_update_rollback")
	p.Duration = roughDuration(10, 0, 180, 0, 0)
	return p, nil
}

func (s *ServerService) previewAddRepo(server *domain.Server, key string) (*ActionPreview, error) {
	repo, ok := s.knownRepoByKey(key)
	if !ok {
		return nil, ErrUnknownRepo
	}
	script, err := planAddRepo(server, repo)
	if err != nil {
		return nil, err
	}
	p := newActionPreview(PreviewReposAdd, server)
	p.addRoot(script)
	p.note("preview.repo_add", "name", repo.Name)
	p.Duration = roughDuration(10, 0, 180, 0, 0)
	return p, nil
}

func (s *ServerService) previewAptProxy(server *domain.Server, enable bool, cacheID uint) (*ActionPreview, error) {
	script, cacheURL, err := s.planAptProxy(server, enable, cacheID)
	if err != nil {
		return nil, err
	}
	action := PreviewAptCacheOff
	if enable {
		action = PreviewAptCacheOn
	}
	p := newActionPreview(action, server)
	p.addRoot(script)
	p.Reversible = true
	if enable {
		p.note("preview.apt_cache_probe", "url", maskPreviewSecrets(cacheURL))
		p.Duration = roughDuration(10, 0, 180, 0, 0)
		return p, nil
	}
	p.note("preview.apt_cache_direct")
	if s.aptProxyEnforceHint(server.ID) != "" {
		p.note("preview.apt_cache_enforced")
	}
	p.Duration = roughDuration(1, 0, 10, 0, 0)
	return p, nil
}

func (s *ServerService) previewDNS(server *domain.Server, servers []string) (*ActionPreview, error) {
	script, clean, testDomain, err := s.planDNS(server, servers)
	if err != nil {
		return nil, err
	}
	p := newActionPreview(PreviewDNS, server)
	p.addRoot(script)
	p.addLogin(dnsCurrentScript())
	p.Reversible = true
	if len(clean) == 0 {
		p.note("preview.dns_unmanage")
	} else {
		p.note("preview.dns_servers", "servers", strings.Join(clean, ", "))
		p.note("preview.dns_guard")
	}
	p.note("preview.dns_test_rollback", "domain", testDomain)
	p.Duration = roughDuration(2, 0, 30, 0, 0)
	return p, nil
}

func previewTimezone(server *domain.Server, tz string) (*ActionPreview, error) {
	script, clean, err := planTimezone(server, tz)
	if err != nil {
		return nil, err
	}
	p := newActionPreview(PreviewTimezone, server)
	p.addRoot(script)
	p.Reversible = true
	p.note("preview.timezone", "timezone", clean)
	p.Duration = roughDuration(1, 0, 10, 0, 0)
	return p, nil
}

func previewNTP(server *domain.Server, servers []string) (*ActionPreview, error) {
	script, clean, err := planNTP(server, servers)
	if err != nil {
		return nil, err
	}
	p := newActionPreview(PreviewNTP, server)
	p.addRoot(script)
	p.Reversible = true
	p.note("preview.ntp_servers", "servers", strings.Join(clean, ", "))
	p.note("preview.ntp_sync_check")
	p.Duration = roughDuration(5, 0, 90, 0, 0)
	return p, nil
}

// previewPlanned zeigt eine Aktion, die als Paket-Job läuft (runPlanned).
func previewPlanned(server *domain.Server, action string, plan func(*domain.Server) (packagePlan, error)) (*ActionPreview, error) {
	pl, err := plan(server)
	if err != nil {
		return nil, err
	}
	p := newActionPreview(action, server)
	p.addRoot(pl.script)
	return p, nil
}

func (s *ServerService) previewApplyPins(server *domain.Server) (*ActionPreview, error) {
	p, err := previewPlanned(server, PreviewPinsApply, s.planApplyPins)
	if err != nil {
		return nil, err
	}
	p.Reversible = true
	p.note("preview.pins_written", "count", len(s.effectivePins(server)))
	p.Duration = roughDuration(2, 0, 30, 0, 0)
	return p, nil
}

func (s *ServerService) previewPackageInstall(server *domain.Server, action string, plan func(*domain.Server) (packagePlan, error), pkgs ...string) (*ActionPreview, error) {
	p, err := previewPlanned(server, action, plan)
	if err != nil {
		return nil, err
	}
	for _, name := range pkgs {
		p.Packages = append(p.Packages, PreviewPackage{Name: name})
	}
	p.Reversible = true
	p.Duration = roughDuration(10, 5, 120, 60, len(pkgs))
	return p, nil
}

func previewHardenPaths(server *domain.Server, targets []HardenTarget) (*ActionPreview, error) {
	scripts, err := planHardenPaths(server, targets)
	if err != nil {
		return nil, err
	}
	p := newActionPreview(PreviewPathHarden, server)
	for _, script := range scripts {
		p.addRoot(script)
	}
	p.Reversible = true
	p.note("preview.path_harden")
	for _, target := range targets {
		if target.Unit != "" {
			p.note("preview.path_harden_unit", "unit", target.Unit)
		}
	}
	p.Duration = roughDuration(len(targets), 0, 10*len(targets), 0, 0)
	return p, nil
}

func (s *ServerService) previewRestorePath(server *domain.Server, hardenedID uint) (*ActionPreview, error) {
	hardened, err := s.servers.FindHardenedPath(server.ID, hardenedID)
	if err != nil {
		return nil, err
	}
	p := newActionPreview(PreviewPathRestore, server)
	p.addRoot(restoreScript(hardened))
	p.Reversible = true
	p.note("preview.path_restore", "path", hardened.Path, "mode", hardened.PrevMode, "group", hardened.PrevGroup)
	p.Duration = roughDuration(1, 0, 10, 0, 0)
	return p, nil
}

// previewLcmHost zeigt eine Einrichtungs-Aktion des LCM-Hosts.
func (s *ServerService) previewLcmHost(server *domain.Server, action string, bouncer bool) (*ActionPreview, error) {
	if err := s.checkLcmHostApt(server); err != nil {
		return nil, err
	}
	p := newActionPreview(action, server)
	switch action {
	case PreviewHostTrivy:
		p.addRoot(trivyHostScript(server))
		p.note("preview.host_trivy")
	case PreviewHostSandbox:
		p.addRoot(sandboxHostScript(server))
	case PreviewHostAptCacher:
		p.addRoot(aptCacherHostScript(server))
		p.note("preview.host_apt_cacher", "url", lcmCacheURL(server))
	default:
		p.addRoot(crowdsecLapiHostScript(server, bouncer))
		p.note("preview.host_crowdsec_lapi", "port", crowdsecLapiListenPort)
	}
	p.Duration = roughDuration(30, 0, 600, 0, 0)
	return p, nil
}

// previewDocker zeigt eine Docker-Aktion. Nach dem Skript liest der Lauf das
// Docker-Inventar neu ein; das ist ein reiner Lesezugriff und fehlt hier.
func (s *ServerService) previewDocker(server *domain.Server, action string, args PreviewArgs) (*ActionPreview, error) {
	var plan dockerPlan
	var refs []string
	var err error
	switch action {
	case PreviewDockerCompose:
		plan, err = s.planComposeUpdate(server, args.Project, args.Service)
	case PreviewDockerPull:
		plan, err = s.planPullImage(server, args.Image)
		refs = []string{args.Image}
	case PreviewDockerPullAll:
		plan, refs, err = s.planPullAllImages(server)
	case PreviewDockerRemove:
		plan, err = s.planDeleteImage(server, args.Image)
		refs = []string{args.Image}
	default:
		plan, err = planPruneImages(server)
	}
	if err != nil {
		return nil, err
	}
	p := newActionPreview(action, server)
	p.addRoot(plan.script)
	for _, ref := range refs {
		p.Packages = append(p.Packages, PreviewPackage{Name: ref})
	}
	switch action {
	case PreviewDockerCompose:
		p.note("preview.docker_compose_recreate", "project", args.Project)
		p.Duration = roughDuration(10, 0, 600, 0, 0)
	case PreviewDockerPull, PreviewDockerPullAll:
		p.note("preview.docker_pull_no_recreate")
		p.Duration = roughDuration(5, 5, 60, 120, len(refs))
	case PreviewDockerRemove:
		p.note("preview.docker_image_unused")
		p.Duration = roughDuration(1, 0, 30, 0, 0)
	default:
		p.note("preview.docker_prune_unused")
		p.Duration = roughDuration(2, 0, 300, 0, 0)
	}
	if plan.rescanCVEs {
		p.note("preview.docker_cve_rescan")
	}
	return p, nil
}

// previewServerUser zeigt Sperren, Freigeben oder Entfernen eines gescannten
// Kontos. Die Skripte baut die Provisionierung.
func (s *ServerService) previewServerUser(server *domain.Server, action, username string) (*ActionPreview, error) {
	if s.userPlans == nil {
		return nil, ErrPreviewUnavailable
	}
	var script string
	var err error
	if action == PreviewUserRemove {
		script, err = s.userPlans.planRemoveServerUser(server, username)
	} else {
		script, err = s.userPlans.planServerUserDisabled(server, username, action == PreviewUserLock)
	}
	if err != nil {
		return nil, err
	}
	p := newActionPreview(action, server)
	p.addRoot(script)
	p.Duration = roughDuration(1, 0, 15, 0, 0)
	switch action {
	case PreviewUserLock:
		p.Reversible = true
		p.note("preview.user_lock", "user", username)
	case PreviewUserUnlock:
		p.Reversible = true
		p.note("preview.user_unlock", "user", username)
	default:
		p.note("preview.user_remove_home", "user", username)
	}
	return p, nil
}
