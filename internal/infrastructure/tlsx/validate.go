package tlsx

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// ValidatedPair ist ein geprüftes Zertifikat samt passendem Schlüssel.
type ValidatedPair struct {
	Certificate tls.Certificate // Leaf ist gesetzt
	Leaf        *x509.Certificate
	// Warnings sind Hinweise, die das Laden nicht verhindern.
	Warnings []string
}

// ValidatePair prüft, ob ein Zertifikat als Server-Zertifikat taugt: Der
// Schlüssel passt, es gilt zum Zeitpunkt now, darf für serverAuth dienen
// und nennt mindestens einen DNS- oder IP-Namen. Fehlt in der PEM-Datei die
// Kette bis zum Aussteller, ist das nur eine Warnung.
func ValidatePair(certPEM, keyPEM []byte, now time.Time) (*ValidatedPair, error) {
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("zertifikat laden: %s", explainKeyPairError(err))
	}
	leaf := pair.Leaf
	if err := checkValidity(leaf, now); err != nil {
		return nil, err
	}
	if err := checkServerUse(leaf); err != nil {
		return nil, err
	}
	v := &ValidatedPair{Certificate: pair, Leaf: leaf}
	if !chainComplete(pair) {
		v.Warnings = append(v.Warnings, "kette unvollständig: das zertifikat des ausstellers fehlt in der datei")
	}
	return v, nil
}

// explainKeyPairError übersetzt die Meldungen von tls.X509KeyPair, die ein
// Admin beim Hochladen am ehesten sieht, in einen Satz, der sagt, was zu tun ist.
func explainKeyPairError(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "may have been switched"):
		return "Zertifikat und Schlüssel sind vertauscht"
	case strings.Contains(msg, "failed to find any PEM data in certificate input"):
		return "in der Zertifikatsdatei steht kein Zertifikat im PEM-Format"
	case strings.Contains(msg, "failed to find any PEM data in key input"):
		return "in der Schlüsseldatei steht kein Schlüssel im PEM-Format"
	case strings.Contains(msg, "does not match public key"):
		return "der Schlüssel gehört nicht zu diesem Zertifikat"
	case strings.Contains(msg, "private key"):
		return "der Schlüssel lässt sich nicht lesen; er muss unverschlüsselt sein (ohne Passwort)"
	}
	return msg
}

func checkValidity(leaf *x509.Certificate, now time.Time) error {
	if now.Before(leaf.NotBefore) {
		return fmt.Errorf("zertifikat noch nicht gültig (ab %s)", leaf.NotBefore.Format(time.DateOnly))
	}
	if !now.Before(leaf.NotAfter) {
		return fmt.Errorf("zertifikat abgelaufen (seit %s)", leaf.NotAfter.Format(time.DateOnly))
	}
	return nil
}

func checkServerUse(leaf *x509.Certificate) error {
	if len(leaf.DNSNames) == 0 && len(leaf.IPAddresses) == 0 {
		return errors.New("zertifikat nennt weder einen dns- noch einen ip-namen (subject alternative name)")
	}
	usages := leaf.ExtKeyUsage
	if len(usages) > 0 && !slices.Contains(usages, x509.ExtKeyUsageServerAuth) && !slices.Contains(usages, x509.ExtKeyUsageAny) {
		return errors.New("zertifikat ist nicht für server-authentifizierung (serverAuth) zugelassen")
	}
	return nil
}

// chainComplete: Das Zertifikat ist selbst ausgestellt, oder die Datei
// enthält ein weiteres, dessen Inhaber der Aussteller des Zertifikats ist.
func chainComplete(pair tls.Certificate) bool {
	leaf := pair.Leaf
	if bytes.Equal(leaf.RawIssuer, leaf.RawSubject) {
		return true
	}
	for _, der := range pair.Certificate[1:] {
		if cert, err := x509.ParseCertificate(der); err == nil && bytes.Equal(cert.RawSubject, leaf.RawIssuer) {
			return true
		}
	}
	return false
}
