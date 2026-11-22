package controllers

import (
	"errors"
	"strconv"

	"github.com/gofiber/fiber/v3"

	"LCM/internal/api/apierr"
	"LCM/internal/core/services"
)

// Zertifikate von einer ACME-CA.
var (
	errACMEInvalid    = apierr.Define(422, "acme.invalid", "invalid ACME setting")
	errACMETerms      = apierr.Define(422, "acme.terms_required", "the terms of service of the CA must be accepted (accept_terms)")
	errACMERunning    = apierr.Define(409, "acme.running", "an ACME run is already in progress")
	errACMENotEnabled = apierr.Define(409, "acme.not_enabled", "ACME is not enabled")
	errACMEMissing    = apierr.Define(422, "acme.field_missing", "a required ACME setting is missing")
	errACMEDNSTest    = apierr.Define(422, "acme.dns_test_failed", "the DNS provider test failed")
)

// ACMEController bedient die ACME-Einstellungen. Das Recht dazu
// (system:endpoints) hat allein admin: Ein Zertifikat bestimmt, wem Browser
// und Agenten LCM abnehmen.
type ACMEController struct {
	acme *services.ACMEService
}

func NewACMEController(acme *services.ACMEService) *ACMEController {
	return &ACMEController{acme: acme}
}

// Get - GET /api/v1/system/acme (system:endpoints)
// Einstellungen, Zustand der Läufe und die ausgestellten Zertifikate. `names`
// sind die Namen aus den öffentlichen Adressen; `names_changed_notice` ist beim
// Umstieg von einem eigenen Namensfeld der einmalige Hinweis (sonst null).
func (ctrl *ACMEController) Get(c fiber.Ctx) error {
	view, err := ctrl.acme.View()
	if err != nil {
		return err
	}
	return c.JSON(view)
}

// Update - PUT /api/v1/system/acme (system:endpoints)
// Setzt alle Einstellungen. Beim Einschalten nimmt accept_terms die
// Nutzungsbedingungen der CA an; LCM verlangt es, bis es für diese CA einmal
// geschehen ist. Die Namen kommen aus den öffentlichen Adressen, und es gibt
// immer ein Zertifikat für alle: Die Felder `names` und `mode` älterer
// Anfragen werden überlesen, ohne Fehler.
func (ctrl *ACMEController) Update(c fiber.Ctx) error {
	var in struct {
		Enabled      *bool  `json:"enabled"`
		DirectoryURL string `json:"directory_url"`
		Email        string `json:"email"`
		Challenge    string `json:"challenge"`
		AcceptTerms  bool   `json:"accept_terms"`
		dnsBody
	}
	if err := bindBody(c, &in); err != nil {
		return err
	}
	// Ein fehlendes Feld darf nicht wie „aus" wirken: Das entfernte die
	// ausgestellten Zertifikate.
	if in.Enabled == nil {
		return missingFields("enabled")
	}
	dnsValues, err := in.values()
	if err != nil {
		return mapACMEError(err)
	}
	view, err := ctrl.acme.Update(services.ACMEUpdate{
		Enabled:        *in.Enabled,
		DirectoryURL:   in.DirectoryURL,
		Email:          in.Email,
		Challenge:      in.Challenge,
		AcceptTerms:    in.AcceptTerms,
		DNSProvider:    in.DNSProvider,
		DNSValues:      dnsValues,
		DNSNameservers: in.DNSNameservers,
	}, actor(c))
	if err != nil {
		return mapACMEError(err)
	}
	return c.JSON(view)
}

// AckNamesNotice - POST /api/v1/system/acme/names-notice/ack (system:endpoints)
// Bestätigt den Hinweis zum Umstieg auf die Namen aus den Adressen; er
// erscheint danach nicht mehr. Antwort: der neue Stand wie bei GET.
func (ctrl *ACMEController) AckNamesNotice(c fiber.Ctx) error {
	view, err := ctrl.acme.AckNamesNotice(actor(c))
	if err != nil {
		return err
	}
	return c.JSON(view)
}

// Issue - POST /api/v1/system/acme/issue (system:endpoints)
// Stößt einen Lauf im Hintergrund an: stellt fehlende Zertifikate aus und
// erneuert fällige. Das Ergebnis steht danach in GET /system/acme.
func (ctrl *ACMEController) Issue(c fiber.Ctx) error {
	if err := ctrl.acme.IssueNow(actor(c)); err != nil {
		return mapACMEError(err)
	}
	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{"status": "started"})
}

// DNSTest - POST /api/v1/system/acme/dns-test (system:endpoints)
// Setzt einen TXT-Eintrag _acme-challenge.<erster Name> über den DNS-Anbieter,
// wartet auf seine Sichtbarkeit und räumt ihn wieder auf. Fehlende Angaben
// kommen aus den gespeicherten Einstellungen. Es läuft im Aufruf und kann bis
// zu einer Minute dauern.
func (ctrl *ACMEController) DNSTest(c fiber.Ctx) error {
	var in struct {
		Names []string `json:"names"`
		dnsBody
	}
	if len(c.Body()) > 0 {
		if err := bindBody(c, &in); err != nil {
			return err
		}
	}
	values, err := in.values()
	if err != nil {
		return mapACMEError(err)
	}
	err = ctrl.acme.DNSTest(services.ACMEDNSTest{
		Names:       in.Names,
		Provider:    in.DNSProvider,
		Values:      values,
		Nameservers: in.DNSNameservers,
	}, actor(c))
	if err != nil {
		return mapACMEError(err)
	}
	return c.JSON(fiber.Map{"ok": true})
}

// dnsBody sind die Angaben zu DNS-01 in der Anfrage. Bei dns_values sind
// Zahlen und Wahrheitswerte erlaubt; ein Geheimfeld, das fehlt, bleibt, ein
// leeres wird gelöscht.
type dnsBody struct {
	DNSProvider    string         `json:"dns_provider"`
	DNSValues      map[string]any `json:"dns_values"`
	DNSNameservers []string       `json:"dns_check_nameservers"`
}

func (b dnsBody) values() (map[string]string, error) {
	if b.DNSValues == nil {
		return nil, nil
	}
	out := make(map[string]string, len(b.DNSValues))
	for name, v := range b.DNSValues {
		switch v := v.(type) {
		case nil:
			out[name] = ""
		case string:
			out[name] = v
		case bool:
			out[name] = strconv.FormatBool(v)
		case float64:
			out[name] = strconv.FormatFloat(v, 'f', -1, 64)
		default:
			return nil, &services.ACMEInvalidError{Field: "dns_values." + name, Detail: "erlaubt sind text, zahl und wahrheitswert"}
		}
	}
	return out, nil
}

func mapACMEError(err error) error {
	var invalid *services.ACMEInvalidError
	var dnsTest *services.ACMEDNSTestError
	switch {
	case errors.As(err, &invalid) && invalid.Missing:
		return errACMEMissing.With("field", invalid.Field)
	case errors.As(err, &invalid):
		return errACMEInvalid.With("field", invalid.Field).With("detail", invalid.Detail)
	case errors.As(err, &dnsTest):
		return errACMEDNSTest.With("detail", dnsTest.Detail)
	case errors.Is(err, services.ErrACMETermsRequired):
		return errACMETerms
	case errors.Is(err, services.ErrACMERunning):
		return errACMERunning
	case errors.Is(err, services.ErrACMENotActive):
		return errACMENotEnabled
	}
	return err
}
