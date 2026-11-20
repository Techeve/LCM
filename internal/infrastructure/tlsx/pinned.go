package tlsx

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"path/filepath"
)

// IsNotTLS erkennt den einen Fall, der einen zweiten Versuch rechtfertigt:
// Die Gegenstelle spricht kein TLS (Dienst laeuft mit --dev).
//
// Zwei Wege, weil die Antwort an zwei Stellen auffliegt: Erkennt der
// HTTP-Client die Klartext-Antwort selbst, meldet er ErrSchemeMismatch;
// scheitert schon der Handshake, kommt ein tls.RecordHeaderError.
func IsNotTLS(err error) bool {
	var re tls.RecordHeaderError
	return errors.Is(err, http.ErrSchemeMismatch) || errors.As(err, &re)
}

// PinnedTo bindet eine Verbindung zum eigenen Dienst (Healthcheck,
// lcm-probe) an genau das Zertifikat, das der Dienst
// ausliefert (Datei im Datenverzeichnis).
//
// Warum gepinnt statt ueber die Namen im Zertifikat geprueft: Das
// mitgelieferte Self-Signed-Zertifikat deckt 127.0.0.1 ab, ein vom Betreiber
// hinterlegtes eigenes Zertifikat aber typischerweise nur seine Domain. Eine
// Namenspruefung wuerde dann fehlschlagen, obwohl der Dienst laeuft - der
// Container startete in einer Schleife neu. Gleichzeitig ist „Pruefung aus"
// keine Antwort: Wir vergleichen, ob die Gegenstelle exakt das Zertifikat
// vorzeigt, das hier auf der Platte liegt. Findet sich keins, bleibt nur die
// Erreichbarkeit - das ist ehrlicher als ein erfundener Vertrauensanker.
//
// Verglichen werden die Bytes, keine Kette: Eine Kettenpruefung gegen die
// Datei nahm jedes Zertifikat an, das die mitgelieferte CA ausgestellt hat,
// und lehnte das richtige ab, sobald es abgelaufen war. Ob der Dienst lebt,
// haengt aber nicht an der Laufzeit seines Zertifikats.
func PinnedTo(dataDir string) *tls.Config {
	want, err := firstCertificate(filepath.Join(dataDir, CertFileName))
	if err != nil {
		return &tls.Config{InsecureSkipVerify: true} //nolint:gosec // siehe Kommentar: nur Loopback, kein Anker vorhanden
	}
	return &tls.Config{
		// Die Pruefung uebernimmt VerifyPeerCertificate; ohne das scheitert
		// schon der Handshake an der Adresse 127.0.0.1.
		InsecureSkipVerify: true, //nolint:gosec // ersetzt durch die Pinnung unten
		VerifyPeerCertificate: func(raw [][]byte, _ [][]*x509.Certificate) error {
			if len(raw) == 0 {
				return errors.New("die Gegenstelle zeigt kein Zertifikat vor")
			}
			if !bytes.Equal(raw[0], want) {
				return errors.New("das vorgezeigte Zertifikat ist nicht das aus dem Datenverzeichnis")
			}
			return nil
		},
	}
}
