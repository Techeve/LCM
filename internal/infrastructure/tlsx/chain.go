package tlsx

import (
	"bytes"
	"encoding/pem"
	"errors"
)

// ErrNoCertificate: die Datei enthält kein Zertifikat im PEM-Format.
var ErrNoCertificate = errors.New("keine zertifikate in der datei")

// CertificateChainPEM zieht aus einer PEM-Datei die Zertifikatskette: allein
// die CERTIFICATE-Blöcke, in der Reihenfolge der Datei. Alles andere
// (privater Schlüssel in derselben Datei, Kopfzeilen, Text dazwischen) bleibt
// draußen: Die Kette geht zum Download, und ein Schlüssel darf dort nie
// mitkommen.
func CertificateChainPEM(data []byte) ([]byte, error) {
	var chain bytes.Buffer
	for {
		var block *pem.Block
		block, data = pem.Decode(data)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		// Neu kodiert ohne die Kopfzeilen des Blocks.
		if err := pem.Encode(&chain, &pem.Block{Type: block.Type, Bytes: block.Bytes}); err != nil {
			return nil, err
		}
	}
	if chain.Len() == 0 {
		return nil, ErrNoCertificate
	}
	return chain.Bytes(), nil
}
