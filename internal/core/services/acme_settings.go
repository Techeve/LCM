package services

import (
	"errors"
	"fmt"
	"net/mail"
	"net/netip"
	"net/url"
	"slices"
	"strings"

	"LCM/internal/inputcheck"
)

// normalizeACMEUpdate setzt die Vorgaben und prüft die Angaben.
func normalizeACMEUpdate(in ACMEUpdate) (ACMEUpdate, error) {
	in.DirectoryURL = strings.TrimSpace(in.DirectoryURL)
	if in.DirectoryURL == "" {
		in.DirectoryURL = DefaultACMEDirectory
	}
	if in.Challenge == "" {
		in.Challenge = ACMEChallengeHTTP01
	}
	in.Email = strings.TrimSpace(in.Email)
	if err := checkDirectoryURL(in.DirectoryURL); err != nil {
		return in, &ACMEInvalidError{Field: "directory_url", Detail: err.Error()}
	}
	if err := checkACMEEmail(in.Email); err != nil {
		return in, &ACMEInvalidError{Field: "email", Detail: err.Error()}
	}
	if in.Challenge != ACMEChallengeHTTP01 && in.Challenge != ACMEChallengeDNS01 {
		return in, &ACMEInvalidError{Field: "challenge", Detail: "erlaubt sind http-01 und dns-01"}
	}
	nameservers, err := normalizeNameservers(in.DNSNameservers)
	if err != nil {
		return in, &ACMEInvalidError{Field: "dns_check_nameservers", Detail: err.Error()}
	}
	in.DNSNameservers = nameservers
	return in, nil
}

func checkDirectoryURL(raw string) error {
	u, err := url.Parse(raw)
	switch {
	case err != nil || u.Host == "":
		return errors.New("keine adresse")
	case u.Scheme != "https":
		return errors.New("die adresse muss mit https:// beginnen")
	case u.User != nil:
		return errors.New("die adresse darf keine zugangsdaten enthalten")
	case len(raw) > 512:
		return errors.New("die adresse ist zu lang")
	}
	return nil
}

func checkACMEEmail(email string) error {
	if email == "" {
		return nil
	}
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email {
		return errors.New("keine e-mail-adresse")
	}
	return nil
}

// normalizeACMENames bringt die Namen in Kleinschreibung, entfernt
// Doppelte und prüft sie. Mit HTTP-01 sind Platzhalter nicht möglich: Die CA
// prüft sie nur über DNS (dns-01).
func normalizeACMENames(names []string, challenge string) ([]string, error) {
	out := []string{}
	for _, name := range names {
		name = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), "."))
		if name == "" || slices.Contains(out, name) {
			continue
		}
		if err := checkACMEName(name, challenge); err != nil {
			return nil, fmt.Errorf("%q: %w", name, err)
		}
		out = append(out, name)
	}
	if len(out) > maxACMENames {
		return nil, fmt.Errorf("höchstens %d namen", maxACMENames)
	}
	return out, nil
}

func checkACMEName(name, challenge string) error {
	if rest, wildcard := strings.CutPrefix(name, "*."); wildcard {
		if challenge != ACMEChallengeDNS01 {
			return errors.New("platzhalter (wildcard) gibt es nur mit dns-01")
		}
		name = rest
	}
	if strings.Contains(name, "*") {
		return errors.New("der platzhalter gehört nur an den anfang: *.beispiel.de")
	}
	if _, err := netip.ParseAddr(name); err == nil {
		return errors.New("eine ip-adresse bekommt kein zertifikat, nur ein dns-name")
	}
	if inputcheck.HostProblem(name) != "" || !strings.Contains(name, ".") {
		return errors.New("kein gültiger dns-name")
	}
	return nil
}
