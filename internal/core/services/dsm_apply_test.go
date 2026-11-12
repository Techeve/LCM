package services_test

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/infrastructure/synology"
	"LCM/internal/storage/repositories"
)

// fakeDSM ist ein DSM, das set-Aufrufe übernimmt: Das Formular wird zur
// neuen get-Antwort derselben API, wie ein Gerät, das die Werte speichert.
// failing nennt APIs, deren set scheitert.
type fakeDSM struct {
	mu      sync.Mutex
	state   map[string]string // "<api>.<method>" → data
	failing map[string]bool
}

func (f *fakeDSM) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	api, method := r.FormValue("api"), r.FormValue("method")
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case api == "SYNO.API.Auth":
		_, _ = w.Write([]byte(`{"success":true,"data":{"sid":"SID"}}`))
	case method == "set" && f.failing[api]:
		_, _ = w.Write([]byte(`{"success":false,"error":{"code":105}}`))
	case method == "set":
		f.state[api+".get"] = formObject(r)
		_, _ = w.Write([]byte(`{"success":true}`))
	default:
		data, ok := f.state[api+"."+method]
		if !ok {
			_, _ = w.Write([]byte(`{"success":false,"error":{"code":102}}`))
			return
		}
		_, _ = w.Write([]byte(`{"success":true,"data":` + data + `}`))
	}
}

// formObject baut aus den JSON-kodierten Formularwerten wieder ein Objekt.
func formObject(r *http.Request) string {
	var fields []string
	for k := range r.PostForm {
		switch k {
		case "api", "version", "method", "_sid":
			continue
		}
		fields = append(fields, `"`+k+`":`+r.PostForm.Get(k))
	}
	sort.Strings(fields)
	return "{" + strings.Join(fields, ",") + "}"
}

// addFakeDSM nimmt ein nachgebautes DSM über den echten Onboarding-Weg auf.
func addFakeDSM(t *testing.T, env *testEnv, dev *fakeDSM) *domain.Server {
	t.Helper()
	srv := httptest.NewTLSServer(dev)
	t.Cleanup(srv.Close)
	addr := srv.Listener.Addr().(*net.TCPAddr)
	server, err := env.Servers.CreateDSMServer(services.DSMRequest{
		Name: "nas01", Host: addr.IP.String(), Port: addr.Port, Account: "lcm", Password: "geheim", Actor: "admin",
	})
	if err != nil {
		t.Fatalf("DSM aufnehmen: %v", err)
	}
	return server
}

// TestDSMSollwerteSetzenAlsJob: Jeder Schlüssel steht mit Stand vorher und
// Ergebnis im Protokoll. Scheitert einer, läuft der nächste trotzdem, und
// der Job gilt als fehlgeschlagen. Danach ist das Gerät neu erfasst.
func TestDSMSollwerteSetzenAlsJob(t *testing.T) {
	env := newTestEnv(t)
	dev := &fakeDSM{
		state: map[string]string{
			"SYNO.Core.System.info":            `{"model":"DS923+","firmware_ver":"DSM 7.3.2-86009"}`,
			"SYNO.Core.Security.DSM.get":       `{"skip_ip_checking":true,"enable_csrf_protection":true,"timeout":15}`,
			"SYNO.Core.Security.AutoBlock.get": `{"enable":false,"attempts":10,"within_mins":5}`,
		},
		failing: map[string]bool{"SYNO.Core.Security.AutoBlock": true},
	}
	server := addFakeDSM(t, env, dev)

	job, err := env.Servers.ApplyDSMTargets(repositories.ScopeAll(), server.ID, []string{"autoBlock", "skipIpCheck", "skipIpCheck"}, "admin")
	if err != nil {
		t.Fatalf("starten: %v", err)
	}
	if job.Name != "DSM-Sollwerte setzen @ nas01" || job.Type != services.JobTypeDSMApply {
		t.Errorf("Job %q vom Typ %q", job.Name, job.Type)
	}
	done := waitForJob(t, env, job.ID)
	if done.Status != domain.JobStatusFailed {
		t.Errorf("ein Schlüssel scheiterte, Job ist %s", done.Status)
	}
	for _, want := range []string{
		"skipIpCheck: vorher an - weicht vom Soll ab",
		"skip_ip_checking true → false;",
		"autoBlock: vorher aus - weicht vom Soll ab",
		"fehlgeschlagen: SYNO.Core.Security.AutoBlock.set",
		"skipIpCheck  aus - Soll erfüllt",
		"1 von 2 Einstellungen nicht gesetzt",
	} {
		if !strings.Contains(done.Output, want) {
			t.Errorf("Protokoll ohne %q:\n%s", want, done.Output)
		}
	}
	if strings.Index(done.Output, "skipIpCheck: vorher") > strings.Index(done.Output, "autoBlock: vorher") {
		t.Error("Reihenfolge nicht wie in der DSM-Systemsteuerung")
	}

	var fresh domain.Server
	if err := env.DB().First(&fresh, server.ID).Error; err != nil {
		t.Fatal(err)
	}
	for _, c := range fresh.DSMChecks {
		if c.Key == domain.DSMCheckSkipIPCheck && c.State != domain.DSMCheckOK {
			t.Errorf("Prüfung nicht neu erfasst: %+v", c)
		}
	}
	var audits int64
	env.DB().Model(&domain.AuditLog{}).Where("action = ? AND details LIKE ?", "server.dsm-apply", "%autoBlock%").Count(&audits)
	if audits != 1 {
		t.Errorf("Audit-Einträge: %d", audits)
	}
}

// TestDSMSollwerteNurErlaubteSchluessel: Firewall, Unbekanntes und eine
// leere Liste werden abgewiesen, ein Linux-Server auch. Es entsteht kein Job.
func TestDSMSollwerteNurErlaubteSchluessel(t *testing.T) {
	env := newTestEnv(t)
	linux := joinTestServer(t, env, "web01")
	scope := repositories.ScopeAll()
	tests := []struct {
		keys []string
		want error
	}{
		{[]string{"firewall"}, services.ErrDSMNotSettable},
		{[]string{"csp", "erfunden"}, services.ErrDSMNotSettable},
		{nil, services.ErrDSMNoTargets},
		{[]string{"csp"}, services.ErrNotDSM},
	}
	for _, tt := range tests {
		if _, err := env.Servers.ApplyDSMTargets(scope, linux, tt.keys, "admin"); !errors.Is(err, tt.want) {
			t.Errorf("%v: erwartet %v, bekam %v", tt.keys, tt.want, err)
		}
	}
	var jobs int64
	env.DB().Model(&domain.Job{}).Where("type = ?", services.JobTypeDSMApply).Count(&jobs)
	if jobs != 0 {
		t.Errorf("%d Jobs angelegt", jobs)
	}
}

// TestDSMSetzbarGleichClient: Was als setzbar gilt, kann der DSM-Client
// setzen, die Firewall nicht.
func TestDSMSetzbarGleichClient(t *testing.T) {
	for _, key := range domain.DSMSettable {
		if !synology.HasTarget(key) {
			t.Errorf("%s: kein Sollwert im Client", key)
		}
	}
	if domain.IsDSMSettable(domain.DSMCheckFirewall) || synology.HasTarget(domain.DSMCheckFirewall) {
		t.Error("die Firewall darf nicht setzbar sein")
	}
}

// TestDSMSollwerteDemoGeraet: Ein Demo-Gerät wird nie kontaktiert.
func TestDSMSollwerteDemoGeraet(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "nas-demo")
	if err := env.DB().Model(&domain.Server{}).Where("id = ?", id).
		Updates(map[string]any{"os_id": domain.OSIDSynologyDSM, "is_demo": true}).Error; err != nil {
		t.Fatal(err)
	}
	job, err := env.Servers.ApplyDSMTargets(repositories.ScopeAll(), id, []string{"csp"}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	done := waitForJob(t, env, job.ID)
	if done.Status != domain.JobStatusSuccess || !strings.Contains(done.Output, "Demo-Gerät") {
		t.Errorf("Demo: %s %q", done.Status, done.Output)
	}
}
