package services_test

import (
	"errors"
	"strings"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/storage/repositories"
)

// TestAnwendungenWerdenErkanntUndBewertet ist der Reiter in einem Zug: Der
// Scan findet die Anwendung, der Katalog kennt die neueste Version, und die
// Bewertung fällt an genau einer Stelle - nicht je Server kopiert.
func TestAnwendungenWerdenErkanntUndBewertet(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	repo := repositories.NewAppRepository(env.DB())

	// Der Scan meldet AdGuard Home in Version 0.107.50 und einen Dienst,
	// der zu keinem Paket gehört.
	// Schlüssel ist ein Stück des Erkennungsskripts - die Anführungszeichen
	// darin werden beim sudo-Einpacken maskiert und taugen nicht zum Treffen.
	env.Dialer.Responses["/opt/AdGuardHome/AdGuardHome"] = sshx.FakeResponse{
		Output: "APP\tadguard-home\tpath\t/opt/AdGuardHome/AdGuardHome\t" +
			"QWRHdWFyZCBIb21lLCB2ZXJzaW9uIHYwLjEwNy41MAo=\n" +
			"UNKNOWN\tfoobar.service\t/etc/systemd/system/foobar.service\t/opt/foobar/foobar\n",
	}
	if _, err := env.Servers.RefreshAll(repositories.ScopeAll(), id, "admin"); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	waitFor(t, func() bool {
		apps, _ := repo.DetectedFor(id)
		return len(apps) > 0
	})

	// Neueste Version am KATALOG hinterlegen, nicht am Fund.
	entry, err := repo.FindBySlug("adguard-home")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateLatest(entry.ID, map[string]any{"latest_version": "v0.107.52"}); err != nil {
		t.Fatal(err)
	}

	view, err := env.Apps.ForServer(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Detected) != 1 {
		t.Fatalf("erwartet einen Fund, war %+v", view.Detected)
	}
	fund := view.Detected[0]
	if fund.Version != "0.107.50" {
		t.Errorf("installierte Version = %q", fund.Version)
	}
	if !fund.UpdateAvailable {
		t.Errorf("0.107.50 gegen 0.107.52 müsste ein Update ergeben: %+v", fund)
	}
	if fund.CanUpdate {
		t.Error("ohne hinterlegte Aktion darf kein Update angeboten werden")
	}
	if len(view.Unknown) != 1 || view.Unknown[0].Unit != "foobar.service" {
		t.Errorf("generischer Fund fehlt: %+v", view.Unknown)
	}
}

// TestKatalogeintragBrauchtGueltigeAktion: Ein Verweis auf eine Aktion, die es
// nicht gibt, ergäbe eine Schaltfläche, die ins Leere führt.
func TestKatalogeintragBrauchtGueltigeAktion(t *testing.T) {
	env := newTestEnv(t)
	fremd := uint(4711)
	entry := &domain.AppCatalogEntry{
		Slug: "eigenbau", Name: "Eigenbau", Markers: "path /opt/eigenbau",
		UpdateActionID: &fremd,
	}
	if err := env.Apps.Create(entry, "admin"); !errors.Is(err, services.ErrAppActionMissing) {
		t.Errorf("erwartet ErrAppActionMissing, war %v", err)
	}

	// Mit einer echten Aktion geht es.
	action, err := env.CustomActions.Create("Eigenbau aktualisieren", "", "systemctl restart eigenbau", "admin")
	if err != nil {
		t.Fatal(err)
	}
	entry.UpdateActionID = &action.ID
	if err := env.Apps.Create(entry, "admin"); err != nil {
		t.Fatalf("mit gültiger Aktion: %v", err)
	}
}

// TestMitgelieferteAnwendungLaesstSichNichtLoeschen: Löschen brächte nichts -
// das nächste Seeding legte den Eintrag wieder an. Abschalten ist der Weg.
func TestMitgelieferteAnwendungLaesstSichNichtLoeschen(t *testing.T) {
	env := newTestEnv(t)
	entry, err := repositories.NewAppRepository(env.DB()).FindBySlug("adguard-home")
	if err != nil {
		t.Fatal(err)
	}
	if err := env.Apps.Delete(entry.ID, "admin"); !errors.Is(err, services.ErrAppBuiltinDelete) {
		t.Errorf("erwartet ErrAppBuiltinDelete, war %v", err)
	}
}

// TestUpdateNurFuerErkannteAnwendungen: Ein Update auf einem Server
// anzustoßen, auf dem die Anwendung gar nicht liegt, wäre bestenfalls
// wirkungslos.
func TestUpdateNurFuerErkannteAnwendungen(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	server, err := env.Servers.Get(repositories.ScopeAll(), id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.Executor.RunAppAction(server, "adguard-home", services.AppActionUpdate, true, "admin"); !errors.Is(err, services.ErrAppNotDetected) {
		t.Errorf("erwartet ErrAppNotDetected, war %v", err)
	}
}

// TestSicherungLaeuftVorDemUpdateUndStopptEs ist die Zusage aus der
// Oberfläche: erst sichern, dann aktualisieren - und wenn die Sicherung
// scheitert, gar nicht erst aktualisieren.
func TestSicherungLaeuftVorDemUpdateUndStopptEs(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	repo := repositories.NewAppRepository(env.DB())

	sicherung, err := env.CustomActions.Create("Eigenbau sichern", "", "eigenbau-backup", "admin")
	if err != nil {
		t.Fatal(err)
	}
	update, err := env.CustomActions.Create("Eigenbau aktualisieren", "", "eigenbau-update", "admin")
	if err != nil {
		t.Fatal(err)
	}
	entry := &domain.AppCatalogEntry{
		Slug: "eigenbau", Name: "Eigenbau", Markers: "path /opt/eigenbau",
		BackupActionID: &sicherung.ID, UpdateActionID: &update.ID,
	}
	if err := env.Apps.Create(entry, "admin"); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceDetected(id, []domain.DetectedApp{
		{Slug: "eigenbau", Name: "Eigenbau", Path: "/opt/eigenbau", Marker: "path", Version: "1.0"},
	}); err != nil {
		t.Fatal(err)
	}

	// Die Sicherung scheitert.
	env.Dialer.Responses["eigenbau-backup"] = sshx.FakeResponse{Output: "kein platz mehr\n", ExitCode: 1}
	server, err := env.Servers.Get(repositories.ScopeAll(), id)
	if err != nil {
		t.Fatal(err)
	}
	job, err := env.Executor.RunAppAction(server, "eigenbau", services.AppActionUpdate, true, "admin")
	if err != nil {
		t.Fatalf("Aktion nicht gestartet: %v", err)
	}
	waitFor(t, func() bool {
		j, err := env.Jobs.Status(job.ID)
		return err == nil && j.Status != "running"
	})

	fertig, err := env.Jobs.Status(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fertig.Status != "failed" {
		t.Errorf("Job-Status = %q, erwartet failed", fertig.Status)
	}
	for _, cmd := range env.Dialer.Commands {
		if strings.Contains(cmd, "eigenbau-update") {
			t.Fatal("das Update lief trotz gescheiterter Sicherung")
		}
	}
}
