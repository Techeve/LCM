package controllers

import (
	"fmt"
	"strings"
	"testing"
)

// Die Prüfungen der Rümpfe melden Feld und Grenze; hier der Querschnitt über
// die Bereiche, die früher gar keine Längen- oder Zeichenprüfung hatten.

func wantCode(t *testing.T, name string, err error, code string) {
	t.Helper()
	if code == "" {
		if err != nil {
			t.Errorf("%s: unerwarteter Fehler %v", name, err)
		}
		return
	}
	if err == nil {
		t.Errorf("%s: Fehler %q erwartet, keiner gemeldet", name, code)
		return
	}
	if got := asAPIError(t, err); got.Code != code {
		t.Errorf("%s: Code %q, erwartet %q", name, got.Code, code)
	}
}

func TestCheckGlobalSettings(t *testing.T) {
	str := func(s string) *string { return &s }
	num := func(n int) *int { return &n }
	cases := []struct {
		name string
		req  globalSettingsRequest
		code string
	}{
		{"leer", globalSettingsRequest{}, ""},
		{"gültig", globalSettingsRequest{MailHost: str("smtp.example.com"), MailPort: num(587), MailFrom: str("lcm@example.com"), MailAdminRecipients: str("a@example.com, b@example.com\nc@example.com")}, ""},
		{"ssh-benutzer zu lang", globalSettingsRequest{DefaultSSHUser: str(strings.Repeat("a", maxLoginUserLen+1))}, "validation.too_long"},
		{"passwort zu lang", globalSettingsRequest{MailPassword: str(strings.Repeat("a", maxPasswordLen+1))}, "validation.too_long"},
		{"cron mit zeilenumbruch", globalSettingsRequest{CVEScanCron: str("0 3 * * *\n1")}, "validation.control_characters"},
		{"mail-host mit leerzeichen", globalSettingsRequest{MailHost: str("smtp example.com")}, "validation.invalid_host"},
		{"mail-port zu groß", globalSettingsRequest{MailPort: num(70000)}, "validation.invalid_port"},
		{"ssh-port zu groß", globalSettingsRequest{DefaultSSHPort: num(65536)}, "validation.invalid_port"},
		{"absender mit anzeigename", globalSettingsRequest{MailFrom: str("LCM <lcm@example.com>")}, ""},
		{"absender mit komma im namen", globalSettingsRequest{MailFrom: str(`"LCM, Betrieb" <lcm@example.com>`)}, ""},
		{"absender mit umlauten", globalSettingsRequest{MailFrom: str("LCM Überwachung <lcm@example.com>")}, ""},
		{"absender name zu lang", globalSettingsRequest{MailFrom: str(strings.Repeat("a", maxPersonNameLen+1) + " <lcm@example.com>")}, "validation.too_long"},
		{"absender adresse zu lang", globalSettingsRequest{MailFrom: str(strings.Repeat("a", maxEmailLen) + "@example.com")}, "validation.too_long"},
		{"absender keine adresse", globalSettingsRequest{MailFrom: str("LCM")}, "validation.invalid_email"},
		{"absender zwei adressen", globalSettingsRequest{MailFrom: str("a@example.com, b@example.com")}, "validation.invalid_email"},
		{"absender mit zeilenumbruch", globalSettingsRequest{MailFrom: str("LCM <lcm@example.com>\r\nBcc: x@y.z")}, "validation.control_characters"},
		{"empfänger keine adresse", globalSettingsRequest{MailAdminRecipients: str("a@example.com, kaputt")}, "validation.invalid_email"},
		{"zu viele empfänger", globalSettingsRequest{MailAdminRecipients: str(strings.Repeat("a@example.com,", maxRecipients+1))}, "validation.too_many"},
		{"liste zu lang", globalSettingsRequest{DNSTestDomains: str(strings.Repeat("a.example\n", maxListLen))}, "validation.too_long"},
		{"liste mit zeilenumbruch erlaubt", globalSettingsRequest{DNSServerPresets: str("Google = 8.8.8.8\nCloudflare = 1.1.1.1")}, ""},
	}
	for _, tc := range cases {
		wantCode(t, tc.name, checkGlobalSettings(tc.req), tc.code)
	}
}

func TestCheckAlertRequest(t *testing.T) {
	base := alertRuleRequest{Name: "Platte", Type: "disk_capacity", ThresholdPercent: 90, CooldownMinutes: 360}
	wantCode(t, "gültig", checkAlertRequest(base), "")
	for name, mutate := range map[string]func(*alertRuleRequest){
		"schwelle über 100": func(r *alertRuleRequest) { r.ThresholdPercent = 101 },
		"schwelle negativ":  func(r *alertRuleRequest) { r.ThresholdPercent = -1 },
		"prognose negativ":  func(r *alertRuleRequest) { r.ForecastDays = -5 },
		"stunden zu groß":   func(r *alertRuleRequest) { r.HeartbeatHours = 9000 },
		"sperre negativ":    func(r *alertRuleRequest) { r.CooldownMinutes = -1 },
		"veraltet negativ":  func(r *alertRuleRequest) { r.MaxOutdated = -1 },
	} {
		req := base
		mutate(&req)
		wantCode(t, name, checkAlertRequest(req), "validation.out_of_range")
	}
	req := base
	req.GroupIDs = make([]uint, maxItems+1)
	wantCode(t, "zu viele gruppen", checkAlertRequest(req), "validation.too_many")
	req = base
	req.Name = strings.Repeat("n", maxNameLen+1)
	wantCode(t, "name zu lang", checkAlertRequest(req), "validation.too_long")
}

func TestCheckBlockRequest(t *testing.T) {
	variant := func(commands string) []struct {
		Family       string `json:"family"`
		SudoCommands string `json:"sudo_commands"`
		RunAs        string `json:"run_as"`
		EditPaths    string `json:"edit_paths"`
		PathRules    string `json:"path_rules"`
	} {
		return []struct {
			Family       string `json:"family"`
			SudoCommands string `json:"sudo_commands"`
			RunAs        string `json:"run_as"`
			EditPaths    string `json:"edit_paths"`
			PathRules    string `json:"path_rules"`
		}{{Family: "all", SudoCommands: commands}}
	}
	ok := blockRequest{Name: "Dienste", Slug: "dienste", Description: "x\ny", Variants: variant("/usr/bin/systemctl restart nginx\n/usr/bin/systemctl status nginx")}
	wantCode(t, "gültig", checkBlockRequest(ok), "")
	req := ok
	req.Name = strings.Repeat("n", maxNameLen+1)
	wantCode(t, "name zu lang", checkBlockRequest(req), "validation.too_long")
	req = ok
	req.Description = strings.Repeat("d", maxDescriptionLen+1)
	wantCode(t, "beschreibung zu lang", checkBlockRequest(req), "validation.too_long")
	req = ok
	req.Params = strings.Repeat("p", maxCommandLen+1)
	wantCode(t, "parameter zu lang", checkBlockRequest(req), "validation.too_long")
	req = ok
	req.Variants = variant(strings.Repeat("x", maxScriptLen+1))
	wantCode(t, "variante zu lang", checkBlockRequest(req), "validation.too_long")
	req = ok
	req.Variants = variant("ls\x00")
	wantCode(t, "steuerzeichen", checkBlockRequest(req), "validation.control_characters")
	req = ok
	req.Variants = append(variant("ls"), make([]struct {
		Family       string `json:"family"`
		SudoCommands string `json:"sudo_commands"`
		RunAs        string `json:"run_as"`
		EditPaths    string `json:"edit_paths"`
		PathRules    string `json:"path_rules"`
	}, maxItems)...)
	wantCode(t, "zu viele varianten", checkBlockRequest(req), "validation.too_many")
}

func TestCheckAppRequest(t *testing.T) {
	ok := appRequest{Slug: "adguard", Name: "AdGuard Home", Markers: "path /opt/AdGuardHome/AdGuardHome", VersionCommand: "{path} --version"}
	wantCode(t, "gültig", checkAppRequest(ok), "")
	req := ok
	req.VersionCommand = "{path} --version\nid"
	wantCode(t, "kommando mehrzeilig", checkAppRequest(req), "validation.control_characters")
	req = ok
	req.VersionCommand = strings.Repeat("x", maxCommandLen+1)
	wantCode(t, "kommando zu lang", checkAppRequest(req), "validation.too_long")
	req = ok
	req.Markers = strings.Repeat("path /x\n", maxListLen)
	wantCode(t, "marker zu lang", checkAppRequest(req), "validation.too_long")
}

func TestCheckRoleNamesAndNames(t *testing.T) {
	wantCode(t, "rollen", checkRoleNames([]string{"admin", "viewer"}), "")
	wantCode(t, "rolle mit zeilenumbruch", checkRoleNames([]string{"ad\nmin"}), "validation.control_characters")
	wantCode(t, "zu viele rollen", checkRoleNames(make([]string, maxItems+1)), "validation.too_many")
	wantCode(t, "pakete", checkNames("names", []string{"htop", "git"}), "")
	wantCode(t, "paketname zu lang", checkNames("names", []string{strings.Repeat("a", maxNameLen+1)}), "validation.too_long")
	var many []string
	for i := 0; i <= maxItems; i++ {
		many = append(many, fmt.Sprintf("pkg%d", i))
	}
	wantCode(t, "zu viele pakete", checkNames("names", many), "validation.too_many")
}

func TestCheckRangeAndCount(t *testing.T) {
	got := asAPIError(t, checkRange("x", 11, 0, 10))
	if got.Params["field"] != "x" || got.Params["min"] != 0 || got.Params["max"] != 10 {
		t.Errorf("Parameter: %v", got.Params)
	}
	got = asAPIError(t, checkCount("x", 5, 4))
	if got.Params["limit"] != 4 {
		t.Errorf("Parameter: %v", got.Params)
	}
}
