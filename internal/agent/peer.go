package agent

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"LCM/internal/remote/wire"
)

// pins sind die Vertrauensanker des Agents für das Zertifikat von LCM. Als
// Rückgabe von checkPeer: die Pins, die der Agent aus einer vertrauten
// Verbindung lernt (leer = nichts zu lernen).
type pins struct {
	cert string // Fingerprint des Zertifikats aus dem Enrollment-Token
	key  string // Pin des öffentlichen Schlüssels
	ca   string // Fingerprint der LCM-CA
}

func (p pins) any() bool { return p.cert != "" || p.key != "" || p.ca != "" }

// checkPeer akzeptiert das Server-Zertifikat, wenn sein Fingerprint dem Pin
// aus dem Token entspricht, wenn es den gemerkten Schlüssel trägt, wenn die
// gepinnte LCM-CA es ausgestellt hat ODER wenn die reguläre x509-Prüfung
// gegen die Systemroots (inkl. Hostname) gelingt.
//
// Gelernt wird nur aus einer Verbindung, die ein Pin bestätigt hat: der
// Schlüssel des Zertifikats und die CA aus der Kette, die es ausgestellt hat.
// So übernimmt ein Agent mit Schlüssel-Pin beim Umstieg die CA (das erste
// Zertifikat der CA behält den alten Schlüssel), und ein Agent mit CA-Pin
// folgt den monatlich neuen Schlüsseln.
func checkPeer(rawCerts [][]byte, p pins, hostname string) (learned pins, err error) {
	if len(rawCerts) == 0 {
		return pins{}, errors.New("server hat kein zertifikat präsentiert")
	}
	leaf, chain, err := parseChain(rawCerts)
	if err != nil {
		return pins{}, err
	}
	if pinned(leaf, chain, p) {
		return pins{key: keyPinOf(leaf), ca: issuingCA(leaf, chain)}, nil
	}
	// Fallback: reguläres Zertifikat (z.B. Let's Encrypt) - dann muss die
	// Kette samt Hostname stimmen.
	inter := x509.NewCertPool()
	for _, c := range chain {
		inter.AddCert(c)
	}
	_, err = leaf.Verify(x509.VerifyOptions{DNSName: hostname, Intermediates: inter})
	if err != nil && p.any() {
		return pins{}, fmt.Errorf("zertifikat entspricht weder den pins (zertifikat, schlüssel, ca) noch den systemroots: %w", err)
	}
	return pins{}, err
}

// parseChain zerlegt die Kette; Zertifikate hinter dem ersten, die sich nicht
// lesen lassen, fallen weg.
func parseChain(rawCerts [][]byte) (*x509.Certificate, []*x509.Certificate, error) {
	leaf, err := x509.ParseCertificate(rawCerts[0])
	if err != nil {
		return nil, nil, fmt.Errorf("zertifikat parsen: %w", err)
	}
	var chain []*x509.Certificate
	for _, raw := range rawCerts[1:] {
		if c, err := x509.ParseCertificate(raw); err == nil {
			chain = append(chain, c)
		}
	}
	return leaf, chain, nil
}

// pinned meldet, ob einer der Pins das Zertifikat bestätigt.
func pinned(leaf *x509.Certificate, chain []*x509.Certificate, p pins) bool {
	if p.cert != "" && fingerprintOf(leaf.Raw) == strings.ToLower(p.cert) {
		return true
	}
	if p.key != "" && keyPinOf(leaf) == p.key {
		return true
	}
	if p.ca == "" {
		return false
	}
	for _, ca := range chain {
		if fingerprintOf(ca.Raw) == strings.ToLower(p.ca) && verifyIssuedBy(leaf, ca) == nil {
			return true
		}
	}
	return false
}

// issuingCA liefert den Fingerprint der CA aus der Kette, die das Zertifikat
// ausgestellt hat - leer beim alten selbstsignierten Zertifikat.
func issuingCA(leaf *x509.Certificate, chain []*x509.Certificate) string {
	for _, ca := range chain {
		if verifyIssuedBy(leaf, ca) == nil {
			return fingerprintOf(ca.Raw)
		}
	}
	return ""
}

// verifyIssuedBy prüft das Zertifikat gegen genau diese CA als Anker:
// Signatur, Laufzeit beider, Zweck Serverauthentifizierung und die Kennung
// wire.ServerCertMarker.
//
// Ohne Namensprüfung, und das mit Absicht: Agenten erreichen LCM über einen
// Namen oder eine IP, die LCM nicht kennt (NAT, Portweiterleitung), und das
// Zertifikat nennt nur localhost. Die Bindung an LCM leistet die gepinnte CA
// zusammen mit der Kennung - ein anderes Zertifikat derselben CA (etwa einer
// Betreiber-CA für weitere Dienste) trägt sie nicht.
func verifyIssuedBy(leaf, ca *x509.Certificate) error {
	if !ca.IsCA {
		return errors.New("das zertifikat dahinter ist keine ca")
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	opts := x509.VerifyOptions{Roots: roots, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	if _, err := leaf.Verify(opts); err != nil {
		return err
	}
	for _, uri := range leaf.URIs {
		if uri.String() == wire.ServerCertMarker {
			return nil
		}
	}
	return errors.New("zertifikat ist nicht für lcm ausgestellt")
}

// keyPinOf ist der Schlüssel-Pin eines Zertifikats: SHA-256 über die
// SubjectPublicKeyInfo, hex (wie tlsx.KeyPinOf auf der Seite von LCM; das
// Agent-Binary bleibt bewusst ohne LCM-Pakete außer wire).
func keyPinOf(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	return hex.EncodeToString(sum[:])
}

// fingerprintOf ist der SHA-256 eines Zertifikats (DER), hex.
func fingerprintOf(der []byte) string {
	sum := sha256.Sum256(der)
	return hex.EncodeToString(sum[:])
}
