package services

import (
	"crypto/tls"
	"crypto/x509"
	"net"
	"strings"

	"LCM/internal/infrastructure/tlsx"
)

// AgentTrust sind die Zertifikatswerte, mit denen ein Agent LCM unter einem
// Namen erkennt: fürs Enrollment-Token und für den Download-Befehl.
type AgentTrust struct {
	// CertFingerprint ist der Fingerprint des ausgelieferten Zertifikats
	// (Token f).
	CertFingerprint string
	// CustomCAFingerprint ist die CA eines eigenen Zertifikats, soweit die
	// ausgelieferte Kette sie zeigt (Token h). Leer beim Zertifikat der LCM-CA.
	CustomCAFingerprint string
	// CAFingerprint ist die LCM-CA (Token c). Er steht auch neben einem
	// eigenen Zertifikat im Token: Entfernt der Betreiber es wieder, trägt
	// die LCM-CA den Rückfall.
	CAFingerprint string
	// KeyPin ist der Schlüssel-Pin des Zertifikats für curl --pinnedpubkey.
	KeyPin string
}

// AgentTrustFor liefert die Werte für das Zertifikat, das der Pool einem
// Client ausliefert, der LCM unter host anspricht - dasselbe, das der Agent
// beim Handshake sieht. lcmCA ist der Fingerprint der LCM-CA, leer, wenn LCM
// ein Zertifikat des Betreibers im Datenverzeichnis ausliefert, das die CA
// nicht ausstellt (CertRenewal.TokenCAFingerprint).
func AgentTrustFor(pool *tlsx.Pool, lcmCA, host string) AgentTrust {
	served, fallback := pool.Lookup(sniName(host))
	leaf, issuers := parseServed(served)
	if leaf == nil {
		return AgentTrust{}
	}
	trust := AgentTrust{
		CertFingerprint: tlsx.CertFingerprint(leaf),
		CAFingerprint:   lcmCA,
		KeyPin:          tlsx.CurlKeyPin(leaf),
	}
	if !fallback {
		trust.CustomCAFingerprint = issuerFingerprint(leaf, issuers)
	}
	return trust
}

// sniName ist der Name, den ein Client beim Handshake als SNI schickt: der
// Host ohne Klammern und Schlusspunkt, klein geschrieben. Eine IP-Adresse
// ist kein SNI-Name - der Client schickt dann keinen, und der Pool liefert
// den Rückfall.
func sniName(host string) string {
	host = strings.ToLower(strings.TrimSuffix(strings.Trim(host, "[]"), "."))
	if net.ParseIP(host) != nil {
		return ""
	}
	return host
}

// parseServed zerlegt die Kette des ausgelieferten Zertifikats: das
// Zertifikat des Servers und die Zertifikate dahinter.
func parseServed(served *tls.Certificate) (leaf *x509.Certificate, issuers []*x509.Certificate) {
	for i, der := range served.Certificate {
		cert, err := x509.ParseCertificate(der)
		if err != nil {
			if i == 0 {
				return nil, nil
			}
			continue
		}
		if i == 0 {
			leaf = cert
			continue
		}
		issuers = append(issuers, cert)
	}
	return leaf, issuers
}

// issuerFingerprint ist der Fingerprint der CA aus der Kette, die das
// Zertifikat ausgestellt hat - leer, wenn die Kette sie nicht enthält
// (selbstsigniert oder unvollständig).
func issuerFingerprint(leaf *x509.Certificate, issuers []*x509.Certificate) string {
	for _, ca := range issuers {
		if leaf.CheckSignatureFrom(ca) == nil {
			return tlsx.CertFingerprint(ca)
		}
	}
	return ""
}

// agentHost ist der Name, unter dem der Agent LCM erreicht: der Host der
// öffentlichen Agent-Adresse, sonst der Name, unter dem der Anwender LCM im
// Browser aufruft - der Join-Assistent setzt die Adresse des Agents genauso.
func (s *ServerService) agentHost(browserHost string) string {
	if s.agentURL == nil {
		return browserHost
	}
	if host := sniHost(s.agentURL()); host != "" {
		return host
	}
	return browserHost
}
