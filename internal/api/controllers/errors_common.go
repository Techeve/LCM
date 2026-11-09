package controllers

import (
	"errors"
	"strings"

	"LCM/internal/api/apierr"
	"LCM/internal/core/services"
)

// Fehler der Eingabeprüfung und allgemeine Anfragefehler, die zu keinem
// Fachbereich gehören.
var (
	errTooLong       = apierr.Define(422, "validation.too_long", "value is too long")
	errControlChars  = apierr.Define(422, "validation.control_characters", "value contains control characters")
	errInvalidEmail  = apierr.Define(422, "validation.invalid_email", "not a valid email address")
	errInvalidHost   = apierr.Define(422, "validation.invalid_host", "invalid character in host or IP address")
	errInvalidPort   = apierr.Define(422, "validation.invalid_port", "port out of range 1-65535")
	errTooMany       = apierr.Define(422, "validation.too_many", "too many entries")
	errOutOfRange    = apierr.OutOfRange
	errNotANumber    = apierr.Define(422, "validation.not_a_number", "value must be a number")
	errWrongType     = apierr.Define(422, "validation.wrong_type", "value has the wrong type")
	errIPLetter      = apierr.Define(422, "validation.ip_letter", "letters in an IPv4 address")
	errIPOctet       = apierr.Define(422, "validation.ip_octet", "IPv4 octet above 255")
	errIPLeadingZero = apierr.Define(422, "validation.ip_leading_zero", "IPv4 octet with a leading zero")
	errIPFormat      = apierr.Define(422, "validation.ip_format", "not an IPv4 address")
	errIPv6          = apierr.Define(422, "validation.ipv6", "not a valid IPv6 address")
	errIPPrefix      = apierr.Define(422, "validation.ip_prefix", "invalid prefix length")
	errIPHostBits    = apierr.Define(422, "validation.ip_host_bits", "address has host bits set for this prefix")
	errMissingFields = apierr.Define(400, "request.missing_fields", "required fields are missing")
	errInvalidFilter = apierr.Define(400, "request.invalid_filter", "invalid filter value")
)

// sentinelCode ordnet einem Fehler des Service- oder Domain-Pakets den
// API-Fehler zu.
type sentinelCode struct {
	target error
	api    *apierr.Error
}

// matchSentinel liefert den API-Fehler zum ersten passenden Eintrag der
// Tabelle, sonst nil. Die Reihenfolge der Tabelle entscheidet bei Fehlern,
// die mehrere Sentinels umschließen.
func matchSentinel(err error, table []sentinelCode) *apierr.Error {
	for _, entry := range table {
		if errors.Is(err, entry.target) {
			return withDetails(err, entry.api)
		}
	}
	return nil
}

// withDetails hängt dem API-Fehler an, was der Service-Fehler über den Grund
// weiß: bei einem schwachen Passwort die Codes der verletzten Regeln, damit die
// Oberfläche sie nennen kann.
func withDetails(err error, api *apierr.Error) *apierr.Error {
	var weak *services.PasswordPolicyError
	if api == errWeakPassword && errors.As(err, &weak) {
		return api.With("problems", weak.Codes())
	}
	return api
}

// mapSentinel übersetzt err über die Tabelle; unbekannte Fehler bleiben
// unverändert und werden vom zentralen Handler zu "internal".
func mapSentinel(err error, table []sentinelCode) error {
	if mapped := matchSentinel(err, table); mapped != nil {
		return mapped
	}
	return err
}

// missingFields meldet fehlende Pflichtfelder (Namen wie im JSON).
func missingFields(fields ...string) error {
	return errMissingFields.With("fields", strings.Join(fields, ", "))
}

// concat hängt Tabellen hintereinander.
func concat(tables ...[]sentinelCode) []sentinelCode {
	var out []sentinelCode
	for _, table := range tables {
		out = append(out, table...)
	}
	return out
}
