package controllers

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"LCM/internal/api/apierr"
	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/storage/repositories"
)

// asAPIError holt den API-Fehler aus err oder bricht den Test ab.
func asAPIError(t *testing.T, err error) *apierr.Error {
	t.Helper()
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("kein apierr-Fehler: %v", err)
	}
	return apiErr
}

func TestMapServerErrorUsesCodes(t *testing.T) {
	tests := []struct {
		err    error
		code   string
		status int
	}{
		{repositories.ErrNotFound, "server.not_found", 404},
		{fmt.Errorf("umschlossen: %w", services.ErrServerBusy), "server.busy", 409},
		{services.ErrServerNameTaken, "server.name_taken", 409},
		{services.ErrInvalidPackage, "package.invalid_name", 422},
		{services.ErrTooManyDNSServers, "dns.too_many_servers", 400},
	}
	for _, tt := range tests {
		got := asAPIError(t, mapServerError(tt.err))
		if got.Code != tt.code || got.Status != tt.status {
			t.Errorf("%v: code %q status %d, erwartet %q %d", tt.err, got.Code, got.Status, tt.code, tt.status)
		}
	}
}

func TestMapServerErrorKeepsUnknownErrors(t *testing.T) {
	unknown := errors.New("irgendein interner Fehler")
	if got := mapServerError(unknown); got != unknown { //nolint:errorlint - Identität ist hier der Punkt
		t.Errorf("unbekannter Fehler wurde verändert: %v", got)
	}
}

func TestSentinelParamsCarryLimits(t *testing.T) {
	got := asAPIError(t, mapServerError(services.ErrTooManyNTPServers))
	if got.Params["limit"] != domain.MaxNTPServers {
		t.Errorf("limit = %v, erwartet %d", got.Params["limit"], domain.MaxNTPServers)
	}
}

func TestBlockVariantErrorKeepsLocationAsDetail(t *testing.T) {
	err := fmt.Errorf("variante apt, regel 2: %w", domain.ErrSudoCommandRelative)
	got := asAPIError(t, mapBlockError(err))
	if got.Code != "command.relative" || got.Params["detail"] != err.Error() {
		t.Errorf("code %q params %v", got.Code, got.Params)
	}
}

func TestRootEquivalentCarriesBinary(t *testing.T) {
	got := asAPIError(t, mapProfileError(&domain.ErrRootEquivalent{Binary: "bash", Reason: "Shell"}))
	if got.Code != "profile.root_equivalent" || got.Params["binary"] != "bash" {
		t.Errorf("code %q params %v", got.Code, got.Params)
	}
}

func TestCheckLineReportsFieldAndLimit(t *testing.T) {
	got := asAPIError(t, checkLine("name", strings.Repeat("a", 5), 3))
	if got.Code != "validation.too_long" || got.Params["field"] != "name" || got.Params["limit"] != 3 {
		t.Errorf("code %q params %v", got.Code, got.Params)
	}
	got = asAPIError(t, checkLine("name", "a\x00b", 10))
	if got.Code != "validation.control_characters" {
		t.Errorf("code %q", got.Code)
	}
}

// Diese Sentinels fielen früher auf 500 "internal" durch.
func TestFrueherUnzugeordneteSentinels(t *testing.T) {
	tests := []struct {
		err    error
		mapped func(error) error
		code   string
		status int
	}{
		{services.ErrSecurityToolNotInstalled, mapServerError, "security_tool.not_installed", 409},
		{services.ErrUnbanIPMissing, mapServerError, "security_tool.invalid_unban_ip", 400},
		{domain.ErrBlockRunAs, mapBlockError, "block.invalid_run_as", 422},
		{fmt.Errorf("variante apt: %w", domain.ErrBlockPathLine), mapBlockError, "block.invalid_path_line", 422},
		{services.ErrMailerDisabled, mapNotificationError, "settings.mailer_disabled", 409},
		{services.ErrMailerNoRecipients, mapNotificationError, "settings.mailer_no_recipients", 409},
	}
	for _, tt := range tests {
		got := asAPIError(t, tt.mapped(tt.err))
		if got.Code != tt.code || got.Status != tt.status {
			t.Errorf("%v: code %q status %d, erwartet %q %d", tt.err, got.Code, got.Status, tt.code, tt.status)
		}
	}
}

func TestWeakPasswordCarriesProblemCodes(t *testing.T) {
	err := services.EnforcePasswordPolicy("Passwort123!", services.PasswordIdentity{})
	got := asAPIError(t, mapSentinel(err, userErrors))
	if got.Code != "password.too_weak" || got.Status != 422 {
		t.Fatalf("code %q status %d, erwartet password.too_weak 422", got.Code, got.Status)
	}
	problems, _ := got.Params["problems"].([]string)
	if !slices.Contains(problems, services.PwProblemCommon) {
		t.Errorf("problems = %v, erwartet %q darunter", problems, services.PwProblemCommon)
	}
	if len(errWeakPassword.Params) != 0 {
		t.Error("die definierte Variable darf keine Parameter behalten")
	}
}

func TestWeakPasswordWithoutPolicyDetailsHasNoParams(t *testing.T) {
	got := asAPIError(t, mapSentinel(services.ErrWeakPassword, userErrors))
	if len(got.Params) != 0 {
		t.Errorf("params = %v, erwartet keine", got.Params)
	}
}
