package services_test

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	"gorm.io/gorm"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/storage/repositories"
)

// previewOf holt die Vorschau einer Aktion für einen Server.
func previewOf(t *testing.T, env *testEnv, id uint, action string, args services.PreviewArgs) *services.ActionPreview {
	t.Helper()
	p, err := env.Servers.PreviewAction(repositories.ScopeAll(), id, action, args)
	if err != nil {
		t.Fatalf("Vorschau %s: %v", action, err)
	}
	return p
}

func firstRun(t *testing.T, p *services.ActionPreview) string {
	t.Helper()
	if len(p.Commands) == 0 {
		t.Fatalf("Vorschau %s ohne Kommando", p.Action)
	}
	return p.Commands[0].Run
}

// assertRunMatchesPreview ist der Kern: Die Kommandos, die der Lauf an die
// SSH-Verbindung übergibt, sind genau die der Vorschau.
func assertRunMatchesPreview(t *testing.T, env *testEnv, id uint, action string, args services.PreviewArgs, run func() (*domain.Job, error)) {
	t.Helper()
	server, err := env.Servers.Get(repositories.ScopeAll(), id)
	if err != nil {
		t.Fatal(err)
	}
	want := previewOf(t, env, id, action, args).ExpectedCommandsForTest(server)
	if len(want) == 0 {
		t.Fatalf("%s: Vorschau ohne Kommandos", action)
	}

	env.Dialer.Commands = nil
	job, err := run()
	if err != nil {
		t.Fatalf("%s: Lauf: %v", action, err)
	}
	waitForJob(t, env, job.ID)

	for _, cmd := range want {
		found := false
		for _, got := range env.Dialer.Commands {
			if got == cmd {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s: der Lauf hat dieses Kommando der Vorschau nicht abgesetzt:\n%s\nabgesetzt:\n%s",
				action, cmd, strings.Join(env.Dialer.Commands, "\n---\n"))
		}
	}
}

// seedKernels legt ein Kernel-Inventar an, aus dem alte Kernel entfernbar sind.
func seedKernels(t *testing.T, env *testEnv, id uint) {
	t.Helper()
	kernels := []domain.KernelPackage{
		{Name: "linux-image-6.1.0-13-amd64", Release: "6.1.0-13-amd64", Running: true},
		{Name: "linux-image-6.1.0-12-amd64", Release: "6.1.0-12-amd64"},
		{Name: "linux-image-6.1.0-11-amd64", Release: "6.1.0-11-amd64"},
		{Name: "linux-image-6.1.0-10-amd64", Release: "6.1.0-10-amd64"},
	}
	err := env.DB().Model(&domain.Server{}).Where("id = ?", id).Updates(map[string]any{
		"kernel_version":    "6.1.0-13-amd64",
		"installed_kernels": domain.MarshalKernelPackages(kernels),
	}).Error
	if err != nil {
		t.Fatal(err)
	}
}

func setHasSnap(t *testing.T, env *testEnv, id uint) {
	t.Helper()
	if err := env.DB().Model(&domain.Server{}).Where("id = ?", id).Update("has_snap", true).Error; err != nil {
		t.Fatal(err)
	}
}

// TestPreviewMatchesRunApt vergleicht je Aktion Vorschau und Lauf auf apt.
func TestPreviewMatchesRunApt(t *testing.T) {
	scope := repositories.ScopeAll()
	cases := []struct {
		action string
		args   services.PreviewArgs
		setup  func(*testing.T, *testEnv, uint)
		run    func(*testEnv, uint) (*domain.Job, error)
	}{
		{services.PreviewRefresh, services.PreviewArgs{}, nil,
			func(e *testEnv, id uint) (*domain.Job, error) { return e.Servers.RefreshPackages(scope, id, "admin") }},
		{services.PreviewUpgradeAll, services.PreviewArgs{}, nil,
			func(e *testEnv, id uint) (*domain.Job, error) {
				return e.Servers.UpgradeAllPackages(scope, id, "admin")
			}},
		{services.PreviewUpgradeSecurity, services.PreviewArgs{}, nil,
			func(e *testEnv, id uint) (*domain.Job, error) {
				return e.Servers.UpgradeSecurityPackages(scope, id, "admin")
			}},
		{services.PreviewUpdate, services.PreviewArgs{Packages: []string{"openssl,nginx"}}, nil,
			func(e *testEnv, id uint) (*domain.Job, error) {
				return e.Servers.UpdatePackages(scope, id, []string{"openssl", "nginx"}, "", "admin")
			}},
		{services.PreviewUpdate, services.PreviewArgs{Packages: []string{"openssl"}, Version: "3.0.14-1"}, nil,
			func(e *testEnv, id uint) (*domain.Job, error) {
				return e.Servers.UpdatePackages(scope, id, []string{"openssl"}, "3.0.14-1", "admin")
			}},
		{services.PreviewRemove, services.PreviewArgs{Packages: []string{"nginx"}}, nil,
			func(e *testEnv, id uint) (*domain.Job, error) {
				return e.Servers.RemovePackages(scope, id, []string{"nginx"}, "admin")
			}},
		{services.PreviewAutoremove, services.PreviewArgs{}, nil,
			func(e *testEnv, id uint) (*domain.Job, error) {
				return e.Servers.AutoremovePackages(scope, id, "admin")
			}},
		{services.PreviewRepair, services.PreviewArgs{Kind: services.RepairDpkgConfigure}, nil,
			func(e *testEnv, id uint) (*domain.Job, error) {
				return e.Servers.RepairPackages(scope, id, services.RepairDpkgConfigure, "admin")
			}},
		{services.PreviewRepair, services.PreviewArgs{Kind: services.RepairFixDeps}, nil,
			func(e *testEnv, id uint) (*domain.Job, error) {
				return e.Servers.RepairPackages(scope, id, services.RepairFixDeps, "admin")
			}},
		{services.PreviewKernelsCleanup, services.PreviewArgs{}, seedKernels,
			func(e *testEnv, id uint) (*domain.Job, error) { return e.Servers.RemoveOldKernels(scope, id, "admin") }},
		{services.PreviewReboot, services.PreviewArgs{}, nil,
			func(e *testEnv, id uint) (*domain.Job, error) { return e.Servers.Reboot(scope, id, "admin") }},
		{services.PreviewSnapsRefreshAll, services.PreviewArgs{}, func(t *testing.T, e *testEnv, id uint) { setHasSnap(t, e, id) },
			func(e *testEnv, id uint) (*domain.Job, error) { return e.Servers.RefreshAllSnaps(scope, id, "admin") }},
		{services.PreviewSnapsRefresh, services.PreviewArgs{Packages: []string{"firefox"}}, func(t *testing.T, e *testEnv, id uint) { setHasSnap(t, e, id) },
			func(e *testEnv, id uint) (*domain.Job, error) {
				return e.Servers.RefreshSnaps(scope, id, []string{"firefox"}, "admin")
			}},
		{services.PreviewSnapsRemove, services.PreviewArgs{Packages: []string{"firefox"}}, func(t *testing.T, e *testEnv, id uint) { setHasSnap(t, e, id) },
			func(e *testEnv, id uint) (*domain.Job, error) {
				return e.Servers.RemoveSnaps(scope, id, []string{"firefox"}, "admin")
			}},
	}
	for _, tc := range cases {
		t.Run(tc.action+"/"+tc.args.Kind+tc.args.Version, func(t *testing.T) {
			env := newTestEnv(t)
			id := joinTestServer(t, env, "web01")
			if tc.setup != nil {
				tc.setup(t, env, id)
			}
			assertRunMatchesPreview(t, env, id, tc.action, tc.args, func() (*domain.Job, error) { return tc.run(env, id) })
		})
	}
}

// TestPreviewMatchesRunOtherManagers: derselbe Abgleich auf dnf und zypper.
func TestPreviewMatchesRunOtherManagers(t *testing.T) {
	scope := repositories.ScopeAll()
	for _, mgr := range []string{"dnf", "zypper"} {
		t.Run(mgr, func(t *testing.T) {
			env := newTestEnv(t)
			id := joinTestServer(t, env, "web01")
			setPackageManager(t, env, id, mgr)
			assertRunMatchesPreview(t, env, id, services.PreviewUpgradeAll, services.PreviewArgs{},
				func() (*domain.Job, error) { return env.Servers.UpgradeAllPackages(scope, id, "admin") })
			assertRunMatchesPreview(t, env, id, services.PreviewUpgradeSecurity, services.PreviewArgs{},
				func() (*domain.Job, error) { return env.Servers.UpgradeSecurityPackages(scope, id, "admin") })
			assertRunMatchesPreview(t, env, id, services.PreviewRefresh, services.PreviewArgs{},
				func() (*domain.Job, error) { return env.Servers.RefreshPackages(scope, id, "admin") })
			assertRunMatchesPreview(t, env, id, services.PreviewRemove, services.PreviewArgs{Packages: []string{"nginx"}},
				func() (*domain.Job, error) {
					return env.Servers.RemovePackages(scope, id, []string{"nginx"}, "admin")
				})
		})
	}
}

// TestPreviewCommandsPerPackageManager prüft die gezeigten Befehle je
// Paketverwaltung.
func TestPreviewCommandsPerPackageManager(t *testing.T) {
	cases := []struct {
		mgr, action string
		args        services.PreviewArgs
		want        string
	}{
		{"apt", services.PreviewUpgradeAll, services.PreviewArgs{}, "lcm_apt -y --with-new-pkgs upgrade"},
		{"apt", services.PreviewRefresh, services.PreviewArgs{}, "lcm_apt_update"},
		{"apt", services.PreviewUpgradeSecurity, services.PreviewArgs{}, "apt list --upgradable"},
		{"dnf", services.PreviewUpgradeAll, services.PreviewArgs{}, "dnf -y --refresh upgrade"},
		{"dnf", services.PreviewRefresh, services.PreviewArgs{}, "dnf -y makecache"},
		{"dnf", services.PreviewUpgradeSecurity, services.PreviewArgs{}, "dnf -y --refresh --security upgrade"},
		{"dnf", services.PreviewUpdate, services.PreviewArgs{Packages: []string{"openssl"}}, "dnf -y --refresh upgrade openssl"},
		{"dnf", services.PreviewRemove, services.PreviewArgs{Packages: []string{"nginx"}}, "dnf -y remove nginx"},
		{"dnf", services.PreviewAutoremove, services.PreviewArgs{}, "dnf -y autoremove"},
		{"zypper", services.PreviewUpgradeAll, services.PreviewArgs{}, "zypper --non-interactive update"},
		{"zypper", services.PreviewRefresh, services.PreviewArgs{}, "zypper --non-interactive refresh"},
		{"zypper", services.PreviewUpgradeSecurity, services.PreviewArgs{}, "patch --category security"},
		{"zypper", services.PreviewUpdate, services.PreviewArgs{Packages: []string{"openssl"}}, "zypper --non-interactive update openssl"},
		{"zypper", services.PreviewRemove, services.PreviewArgs{Packages: []string{"nginx"}}, "zypper --non-interactive remove nginx"},
		{"zypper", services.PreviewRepair, services.PreviewArgs{Kind: services.RepairFixDeps}, "zypper --non-interactive verify"},
		{"pacman", services.PreviewUpgradeAll, services.PreviewArgs{}, "pacman -Syu --noconfirm"},
		{"apk", services.PreviewRemove, services.PreviewArgs{Packages: []string{"nginx"}}, "apk del nginx"},
	}
	for _, tc := range cases {
		t.Run(tc.mgr+"/"+tc.action, func(t *testing.T) {
			env := newTestEnv(t)
			id := joinTestServer(t, env, "web01")
			setPackageManager(t, env, id, tc.mgr)
			p := previewOf(t, env, id, tc.action, tc.args)
			if run := firstRun(t, p); !strings.Contains(run, tc.want) {
				t.Errorf("Vorschau ohne %q:\n%s", tc.want, run)
			}
			if p.Commands[0].As != "root" {
				t.Errorf("as = %q, erwartet root", p.Commands[0].As)
			}
		})
	}
}

// TestPreviewHidesAptMechanics: Die Funktionsdefinitionen der Sperrenlogik
// stehen nicht in der Anzeige, der Hinweis darauf schon.
func TestPreviewHidesAptMechanics(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	p := previewOf(t, env, id, services.PreviewUpgradeAll, services.PreviewArgs{})
	run := firstRun(t, p)
	if strings.Contains(run, "lcm_apt_holder()") || strings.Contains(run, "LCM_APT_LOCKS=") {
		t.Errorf("Vorspann der Sperrenlogik in der Anzeige:\n%s", run)
	}
	if !hasNote(p, "preview.apt_wrapper") {
		t.Errorf("Hinweis preview.apt_wrapper fehlt: %+v", p.Notes)
	}
}

func hasNote(p *services.ActionPreview, code string) bool {
	for _, n := range p.Notes {
		if n.Code == code {
			return true
		}
	}
	return false
}

func TestPreviewUnsupportedAndRejected(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	scope := repositories.ScopeAll()
	preview := func(action string, args services.PreviewArgs) error {
		_, err := env.Servers.PreviewAction(scope, id, action, args)
		return err
	}

	if err := preview("format-c", services.PreviewArgs{}); !errors.Is(err, services.ErrUnknownPreviewAction) {
		t.Errorf("unbekannte Aktion: %v", err)
	}
	if err := preview(services.PreviewRemove, services.PreviewArgs{Packages: []string{"openssh-server"}}); !errors.Is(err, services.ErrProtectedPackage) {
		t.Errorf("geschütztes Paket: %v", err)
	}
	if err := preview(services.PreviewUpdate, services.PreviewArgs{Packages: []string{"bad$(rm)"}}); !errors.Is(err, services.ErrInvalidPackage) {
		t.Errorf("ungültiger Name: %v", err)
	}
	if err := preview(services.PreviewSnapsRefreshAll, services.PreviewArgs{}); !errors.Is(err, services.ErrNoSnapd) {
		t.Errorf("Snap ohne snapd: %v", err)
	}
	if err := preview(services.PreviewKernelsCleanup, services.PreviewArgs{}); !errors.Is(err, services.ErrNoOldKernels) {
		t.Errorf("Kernel ohne Altbestand: %v", err)
	}
	if err := preview(services.PreviewRepair, services.PreviewArgs{Kind: "format"}); !errors.Is(err, services.ErrUnknownRepairAction) {
		t.Errorf("unbekannte Reparatur: %v", err)
	}
	setPackageManager(t, env, id, "dnf")
	if err := preview(services.PreviewRepair, services.PreviewArgs{Kind: services.RepairFixDeps}); !errors.Is(err, services.ErrRepairUnsupported) {
		t.Errorf("dnf-Reparatur: %v", err)
	}
	if err := env.DB().Model(&domain.Server{}).Where("id = ?", id).Update("restricted_sudo", true).Error; err != nil {
		t.Fatal(err)
	}
	if err := preview(services.PreviewReboot, services.PreviewArgs{}); !errors.Is(err, services.ErrRestrictedSudo) {
		t.Errorf("Neustart im eingeschränkten Modus: %v", err)
	}
	// Eine Vorschau ist auch im eingeschränkten Modus möglich - für Aktionen, die dort laufen.
	p := previewOf(t, env, id, services.PreviewUpgradeAll, services.PreviewArgs{})
	if !hasNote(p, "preview.restricted_sudo") {
		t.Errorf("Hinweis preview.restricted_sudo fehlt: %+v", p.Notes)
	}
}

func TestPreviewPackagesAndReboot(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")

	// Aus dem Scan des Test-Servers: openssl 3.0.11 → 3.0.14 (Security).
	p := previewOf(t, env, id, services.PreviewUpgradeAll, services.PreviewArgs{})
	if len(p.Packages) != 1 || p.Packages[0] != (services.PreviewPackage{Name: "openssl", From: "3.0.11", To: "3.0.14"}) {
		t.Fatalf("Pakete: %+v", p.Packages)
	}
	if p.Reboot != services.RebootNo || p.Reversible {
		t.Errorf("Neustart %q, umkehrbar %v", p.Reboot, p.Reversible)
	}
	if !hasNote(p, "preview.restart_services") {
		t.Errorf("Notizen: %+v", p.Notes)
	}
	if p.Duration.MinSeconds <= 0 || p.Duration.MaxSeconds <= p.Duration.MinSeconds {
		t.Errorf("Dauer: %+v", p.Duration)
	}

	// Ein Kernel-Update braucht einen Neustart.
	err := env.DB().Create(&domain.Package{
		ServerRef: repositories.ServerRef(id), Name: "linux-image-6.1.0-13-amd64",
		Version: "6.1.0-13", CandidateVersion: "6.1.0-14",
	}).Error
	if err != nil {
		t.Fatal(err)
	}
	p = previewOf(t, env, id, services.PreviewUpgradeAll, services.PreviewArgs{})
	if p.Reboot != services.RebootYes || len(p.Packages) != 2 {
		t.Errorf("mit Kernel: Neustart %q, %d Pakete", p.Reboot, len(p.Packages))
	}

	// Security-Vorschau: nur Pakete aus Security-Quellen.
	sec := previewOf(t, env, id, services.PreviewUpgradeSecurity, services.PreviewArgs{})
	if len(sec.Packages) != 1 || sec.Packages[0].Name != "openssl" {
		t.Errorf("Security-Pakete: %+v", sec.Packages)
	}

	// Einzelnes Paket: Version aus dem Bestand; unbekanntes Paket als Hinweis.
	one := previewOf(t, env, id, services.PreviewUpdate, services.PreviewArgs{Packages: []string{"openssl,htop"}})
	if len(one.Packages) != 1 || one.Packages[0].To != "3.0.14" || !hasNote(one, "preview.package_not_installed") {
		t.Errorf("update: %+v %+v", one.Packages, one.Notes)
	}
	rm := previewOf(t, env, id, services.PreviewRemove, services.PreviewArgs{Packages: []string{"nginx"}})
	if len(rm.Packages) != 1 || rm.Packages[0] != (services.PreviewPackage{Name: "nginx", From: "1.22.1"}) {
		t.Errorf("remove: %+v", rm.Packages)
	}

	// Neustart: immer „yes", nicht umkehrbar; Aktualisieren der Listen ist umkehrbar.
	if rb := previewOf(t, env, id, services.PreviewReboot, services.PreviewArgs{}); rb.Reboot != services.RebootYes || rb.Reversible {
		t.Errorf("reboot: %+v", rb)
	}
	if rf := previewOf(t, env, id, services.PreviewRefresh, services.PreviewArgs{}); !rf.Reversible {
		t.Errorf("refresh nicht umkehrbar: %+v", rf)
	}
}

// TestPreviewIsCheap: keine SSH-Verbindung, kein Job, und für eine Aktion ohne
// Paketbezug dieselben Lesezugriffe wie das Laden des Servers.
func TestPreviewIsCheap(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	scope := repositories.ScopeAll()

	queries := countQueries(env.DB())
	before := queries()
	if _, err := env.Servers.Get(scope, id); err != nil {
		t.Fatal(err)
	}
	baseline := queries() - before

	env.Dialer.Commands = nil
	dials := len(env.Dialer.DialedPorts)
	var jobsBefore int64
	env.DB().Model(&domain.Job{}).Count(&jobsBefore)

	before = queries()
	previewOf(t, env, id, services.PreviewRefresh, services.PreviewArgs{})
	if used := queries() - before; used != baseline {
		t.Errorf("Vorschau refresh: %d Abfragen, Server laden braucht %d", used, baseline)
	}
	for _, a := range []string{services.PreviewUpgradeAll, services.PreviewUpgradeSecurity, services.PreviewReboot, services.PreviewAutoremove} {
		previewOf(t, env, id, a, services.PreviewArgs{})
	}

	var jobsAfter int64
	env.DB().Model(&domain.Job{}).Count(&jobsAfter)
	if jobsAfter != jobsBefore {
		t.Errorf("die Vorschau hat %d Job(s) angelegt", jobsAfter-jobsBefore)
	}
	if len(env.Dialer.Commands) != 0 || len(env.Dialer.DialedPorts) != dials {
		t.Errorf("die Vorschau hat eine SSH-Verbindung genutzt: %v", env.Dialer.Commands)
	}
}

// countQueries zählt die Lesezugriffe der Datenbank ab jetzt.
func countQueries(db *gorm.DB) func() int {
	n := 0
	_ = db.Callback().Query().Before("gorm:query").Register("preview_test:count", func(*gorm.DB) { n++ })
	return func() int { return n }
}

// customRule legt eine Custom-Aktion samt Regel an, die auf dem Server läuft.
func customRule(t *testing.T, env *testEnv, serverID uint, commands string) (*domain.CustomAction, *domain.Rule) {
	t.Helper()
	act, err := env.CustomActions.Create("Wartung", "", commands, "admin")
	if err != nil {
		t.Fatal(err)
	}
	group, _ := env.Groups.Create("Ops", "", nil, "admin")
	if err := env.Groups.AssignServer(repositories.ScopeAll(), group.ID, serverID, "admin"); err != nil {
		t.Fatal(err)
	}
	sched, _ := env.Groups.DefineSchedule(repositories.ScopeAll(), group.ID, "Nacht", "0 3 * * *", 0, "admin")
	rule, err := env.Groups.DefineRule(repositories.ScopeAll(), group.ID, "wartung", domain.RuleTypeCustom,
		strconv.FormatUint(uint64(act.ID), 10), &sched.ID, false, "admin")
	if err != nil {
		t.Fatal(err)
	}
	return act, rule
}

func TestPreviewCustomMatchesRunAndMasks(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	act, rule := customRule(t, env, id,
		"systemctl restart nginx\n# Kommentar\nDB_PASSWORD=hunter2 mysqldump app\ncurl -H 'Authorization: Bearer abc123token' https://x.test/hook")

	server, err := env.Servers.Get(repositories.ScopeAll(), id)
	if err != nil {
		t.Fatal(err)
	}
	p := previewOf(t, env, id, services.PreviewCustom, services.PreviewArgs{CustomID: act.ID})
	if len(p.Commands) != 3 {
		t.Fatalf("%d Kommandos, erwartet 3 (Kommentar entfällt): %+v", len(p.Commands), p.Commands)
	}
	if p.Commands[0].Run != "systemctl restart nginx" || p.Commands[0].As != "root" {
		t.Errorf("erstes Kommando: %+v", p.Commands[0])
	}
	shown := p.Commands[1].Run + p.Commands[2].Run
	if strings.Contains(shown, "hunter2") || strings.Contains(shown, "abc123token") {
		t.Errorf("Geheimnis in der Vorschau: %s", shown)
	}
	if !strings.Contains(p.Commands[1].Run, "DB_PASSWORD=••••") || !hasNote(p, "preview.secrets_masked") {
		t.Errorf("Maskierung fehlt: %+v %+v", p.Commands[1], p.Notes)
	}

	// Der Lauf setzt die unmaskierten Kommandos ab, in derselben Reihenfolge.
	env.Dialer.Commands = nil
	env.Executor.RunRule(rule, "admin")
	want := p.ExpectedCommandsForTest(server)
	if len(want) != 3 || !strings.Contains(want[1], "hunter2") {
		t.Fatalf("erwartete Kommandos: %v", want)
	}
	next := 0
	for _, got := range env.Dialer.Commands {
		if next < len(want) && got == want[next] {
			next++
		}
	}
	if next != len(want) {
		t.Errorf("Lauf und Vorschau weichen ab; abgesetzt:\n%s", strings.Join(env.Dialer.Commands, "\n---\n"))
	}
}

func TestPreviewCustomErrors(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	scope := repositories.ScopeAll()
	if _, err := env.Servers.PreviewAction(scope, id, services.PreviewCustom, services.PreviewArgs{CustomID: 4711}); !errors.Is(err, services.ErrCustomActionNotFound) {
		t.Errorf("unbekannte Custom-Aktion: %v", err)
	}
	act, _ := customRule(t, env, id, "uptime")
	if err := env.DB().Model(&domain.Server{}).Where("id = ?", id).Update("restricted_sudo", true).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := env.Servers.PreviewAction(scope, id, services.PreviewCustom, services.PreviewArgs{CustomID: act.ID}); !errors.Is(err, services.ErrRestrictedSudo) {
		t.Errorf("Custom-Aktion im eingeschränkten Modus: %v", err)
	}
}

// TestPreviewRetryMatchesRun: „Erneut versuchen" zeigt, was es auslöst - hier
// die Ad-hoc-Aktion „alle Updates" nach einem gescheiterten Update.
func TestPreviewRetryMatchesRun(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	repo := retryEnv(t, env)
	failUpdate(t, env, repo, id)

	args := services.PreviewArgs{Category: domain.CapabilityPackages}
	p := previewOf(t, env, id, services.PreviewRetry, args)
	if p.Action != services.PreviewRetry || !strings.Contains(firstRun(t, p), "--with-new-pkgs upgrade") {
		t.Errorf("Vorschau: %+v", p)
	}
	assertRunMatchesPreview(t, env, id, services.PreviewRetry, args, func() (*domain.Job, error) {
		return env.Servers.RetryCapability(repositories.ScopeAll(), id, domain.CapabilityPackages, "admin")
	})
}

func TestPreviewRetryErrors(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	retryEnv(t, env)
	scope := repositories.ScopeAll()

	if _, err := env.Servers.PreviewAction(scope, id, services.PreviewRetry, services.PreviewArgs{Category: "kaffee"}); !errors.Is(err, services.ErrUnknownCapability) {
		t.Errorf("unbekannte Kategorie: %v", err)
	}
	if _, err := env.Servers.PreviewAction(scope, id, services.PreviewRetry, services.PreviewArgs{Category: domain.CapabilityPackages}); !errors.Is(err, services.ErrCapabilityNotFailed) {
		t.Errorf("nicht fehlgeschlagen: %v", err)
	}
}

// TestPreviewScope: Ein Server außerhalb des Zugriffsbereichs ist nicht da.
func TestPreviewScope(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	_, err := env.Servers.PreviewAction(repositories.ScopeManager(4711), id, services.PreviewRefresh, services.PreviewArgs{})
	if !errors.Is(err, repositories.ErrNotFound) {
		t.Errorf("fremder Server: %v", err)
	}
}
