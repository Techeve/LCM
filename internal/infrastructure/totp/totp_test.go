package totp

import (
	"testing"
	"time"
)

func TestCodeIsSixDigits(t *testing.T) {
	secret, err := GenerateSecret()
	if err != nil {
		t.Fatal(err)
	}
	code, err := Code(secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(code) != 6 {
		t.Errorf("code hat %d stellen, erwartet 6: %q", len(code), code)
	}
}

func TestValidateAcceptsCurrentCode(t *testing.T) {
	secret, _ := GenerateSecret()
	code, _ := Code(secret, time.Now())
	if !Validate(secret, code) {
		t.Error("aktueller code wurde nicht akzeptiert")
	}
}

func TestValidateRejectsWrongCode(t *testing.T) {
	secret, _ := GenerateSecret()
	if Validate(secret, "000000") && Validate(secret, "123456") {
		t.Error("beliebige codes wurden akzeptiert")
	}
}

// TestRFC6238Vector prüft gegen einen bekannten Testvektor aus RFC 6238
// (Secret "12345678901234567890" als base32, SHA1, T=59 => 94287082,
// die letzten 6 Stellen: 287082).
func TestRFC6238Vector(t *testing.T) {
	// "12345678901234567890" base32-kodiert.
	secret := "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	code, err := Code(secret, time.Unix(59, 0))
	if err != nil {
		t.Fatal(err)
	}
	if code != "287082" {
		t.Errorf("RFC-6238-vektor: bekam %q, erwartet 287082", code)
	}
}

func TestProvisioningURI(t *testing.T) {
	uri := ProvisioningURI("ABC", "admin", "LCM")
	if uri == "" || uri[:10] != "otpauth://" {
		t.Errorf("ungültige provisioning-uri: %q", uri)
	}
}

func TestQRCodeDataURI(t *testing.T) {
	uri := ProvisioningURI("JBSWY3DPEHPK3PXP", "admin", "LCM")
	qr, err := QRCodeDataURI(uri)
	if err != nil {
		t.Fatal(err)
	}
	// Muss eine base64-kodierte PNG-data-URI sein.
	const prefix = "data:image/png;base64,"
	if len(qr) < len(prefix) || qr[:len(prefix)] != prefix {
		t.Errorf("kein PNG-data-URI: %q", qr[:min(40, len(qr))])
	}
	if len(qr) < 200 {
		t.Errorf("qr-code verdächtig klein: %d bytes", len(qr))
	}
}
