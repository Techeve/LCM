package agent

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"LCM/internal/remote/wire"
)

// pins sind die Vertrauensanker des Agents für das Zertifikat von LCM. Als
// Rückgabe von checkPeer: die Pins, die der Agent aus einer vertrauten
// Verbindung lernt (leer = nichts zu lernen).
type pins struct {
	cert string // Fingerprint des Zertifikats aus dem Enrollment-Token
	key  string // Pin des öffentlichen Schlüssels
	ca   string // Fingerprint der LCM-CA (Kennung, keine Namensprüfung)
	// customCAs sind CAs eigener Zertifikate des Betreibers (Heim-CA): Ihre
	// Zertifikate gelten nur mit passendem Namen. Beim Lernen höchstens eine.
	customCAs []string
}

func (p pins) any() bool {
	return p.cert != "" || p.key != "" || p.ca != "" || len(p.customCAs) > 0
}

// checkPeer akzeptiert das Server-Zertifikat, wenn sein Fingerprint dem Pin
// aus dem Token entspricht, wenn es den gemerkten Schlüssel trägt, wenn die
// gepinnte LCM-CA es ausgestellt hat, wenn eine gepinnte CA des Betreibers es
// für den Namen der Server-Adresse ausgestellt hat ODER wenn die reguläre
// x509-Prüfung gegen die Systemroots (inkl. Hostname) gelingt.
//
// Gelernt wird nur aus einer Verbindung, die ein Pin bestätigt hat (siehe
// learnFrom). So übernimmt ein Agent mit Schlüssel-Pin beim Umstieg die CA
// (das erste Zertifikat der CA behält den alten Schlüssel), und ein Agent mit
// CA-Pin folgt den monatlich neuen Schlüsseln.
func checkPeer(rawCerts [][]byte, p pins, hostname string) (learned pins, err error) {
	if len(rawCerts) == 0 {
		return pins{}, errors.New("server hat kein zertifikat präsentiert")
	}
	leaf, chain, err := parseChain(rawCerts)
	if err != nil {
		return pins{}, err
	}
	if pinned(leaf, chain, p, hostname) {
		return learnFrom(leaf, chain), nil
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
func pinned(leaf *x509.Certificate, chain []*x509.Certificate, p pins, hostname string) bool {
	if p.cert != "" && fingerprintOf(leaf.Raw) == strings.ToLower(p.cert) {
		return true
	}
	if p.key != "" && keyPinOf(leaf) == p.key {
		return true
	}
	for _, ca := range chain {
		fp := fingerprintOf(ca.Raw)
		if p.ca != "" && fp == strings.ToLower(p.ca) && verifyIssuedBy(leaf, ca) == nil {
			return true
		}
		if isCustomCA(p, fp) && verifyCustomIssuedBy(leaf, ca, hostname) == nil {
			return true
		}
	}
	return false
}

func isCustomCA(p pins, fingerprint string) bool {
	return slices.ContainsFunc(p.customCAs, func(pin string) bool { return strings.ToLower(pin) == fingerprint })
}

// learnFrom ist, was der Agent aus einer vom Pin bestätigten Verbindung
// lernt: der Schlüssel des Zertifikats und die CA aus der Kette, die es
// ausgestellt hat. Die CA geht je nach Zertifikat in getrennte Anker:
//
//   - Trägt es die Kennung von LCM, ist es die LCM-CA (ca).
//   - Sonst ist es die CA eines eigenen Zertifikats (customCAs).
//
// Die Trennung hält den LCM-Anker unberührt: Mit einem eigenen Zertifikat
// verbunden, überschreibt der Agent die CA von LCM nicht und bleibt auch
// nach dem Rückfall auf das Zertifikat der LCM-CA verbunden (etwa, wenn der
// Betreiber das eigene wieder entfernt). Aus demselben Grund ist der
// Schlüssel-Pin hier unkritisch: Er wird mit jedem Zertifikat überschrieben,
// aber der LCM-CA-Pin trägt den Rückfall unabhängig davon.
func learnFrom(leaf *x509.Certificate, chain []*x509.Certificate) pins {
	learned := pins{key: keyPinOf(leaf)}
	for _, ca := range chain {
		switch {
		case verifyIssuedBy(leaf, ca) == nil:
			learned.ca = fingerprintOf(ca.Raw)
			return learned
		case !carriesMarker(leaf) && verifyChainTo(leaf, ca, "") == nil:
			learned.customCAs = []string{fingerprintOf(ca.Raw)}
			return learned
		}
	}
	return learned
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
	if err := verifyChainTo(leaf, ca, ""); err != nil {
		return err
	}
	if !carriesMarker(leaf) {
		return errors.New("zertifikat ist nicht für lcm ausgestellt")
	}
	return nil
}

// verifyCustomIssuedBy prüft ein eigenes Zertifikat des Betreibers gegen
// seine gepinnte CA, und zwar MIT Namensprüfung gegen die Server-Adresse.
// Ohne sie genügte jedes Zertifikat der Heim-CA, auch das für einen anderen
// Dienst im selben Netz: Die CA des Betreibers trägt keine Kennung von LCM,
// der Name ist die einzige Bindung an diesen Server. Fehlt der Name, gilt
// nichts - x509 überspringt die Namensprüfung bei leerem DNSName.
func verifyCustomIssuedBy(leaf, ca *x509.Certificate, hostname string) error {
	if hostname == "" {
		return errors.New("server-adresse ohne namen")
	}
	return verifyChainTo(leaf, ca, hostname)
}

// verifyChainTo prüft Signatur, Laufzeit und Zweck Serverauthentifizierung
// mit genau dieser CA als Anker; hostname leer = ohne Namensprüfung.
func verifyChainTo(leaf, ca *x509.Certificate, hostname string) error {
	if !ca.IsCA {
		return errors.New("das zertifikat dahinter ist keine ca")
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	opts := x509.VerifyOptions{Roots: roots, DNSName: hostname, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	_, err := leaf.Verify(opts)
	return err
}

// carriesMarker meldet, ob das Zertifikat die Kennung von LCM trägt.
func carriesMarker(leaf *x509.Certificate) bool {
	return slices.ContainsFunc(leaf.URIs, func(uri *url.URL) bool { return uri.String() == wire.ServerCertMarker })
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
