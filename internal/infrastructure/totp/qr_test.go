package totp

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image/png"
	"strings"
	"testing"
)

// Die Vektoren in dieser Datei stammen aus dem Differenztest gegen
// github.com/skip2/go-qrcode, mit dem der eigene Encoder abgelöst wurde.
// Zusätzlich wurde jede der zwanzig Grenzlängen (kleinste und größte Nutzlast
// je Version 1-10) mit dem QR-Decoder von macOS eingelesen und der Inhalt
// verglichen. Ändert sich hier ein Hash, hat sich die Kodierung verschoben -
// das ist kein Formalfehler, sondern erzeugt womöglich unscanbare Codes.

// matrixHash serialisiert die Modulmatrix stabil und hasht sie.
func matrixHash(m [][]bool) string {
	h := sha256.New()
	for _, row := range m {
		buf := make([]byte, len(row))
		for i, dark := range row {
			if dark {
				buf[i] = 1
			}
		}
		h.Write(buf)
	}
	return hex.EncodeToString(h.Sum(nil))[:32]
}

func encodeMatrix(t *testing.T, content string) [][]bool {
	t.Helper()
	version, err := qrVersionFor(len(content))
	if err != nil {
		t.Fatalf("versionswahl: %v", err)
	}
	m := newQRMatrix(version)
	m.placeData(qrCodewords(content, version))
	return m.bestMasked()
}

func TestEncodeGoldenVectors(t *testing.T) {
	tests := []struct {
		name    string
		content string
		version int
		size    int
		hash    string
	}{
		{"kurz", "hello", 1, 21, "19cb2fb2c34525521105cbbf11589ad4"},
		{
			"otpauth",
			"otpauth://totp/LCM:admin?algorithm=SHA1&digits=6&issuer=LCM&period=30&secret=JBSWY3DPEHPK3PXP",
			6, 41, "e22b64373ae8bcbabb99e9ea5e87389d",
		},
		{
			"otpauth lang",
			ProvisioningURI("MFRGGZDFMZTWQ2LKNNWG23TPOBYXE43UOV3HO", "administrator", "LCM"),
			7, 45, "4a97f943da50d58744d344b0ac1b8627",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			version, err := qrVersionFor(len(tt.content))
			if err != nil {
				t.Fatal(err)
			}
			if version != tt.version {
				t.Errorf("version = %d, erwartet %d", version, tt.version)
			}
			m := encodeMatrix(t, tt.content)
			if len(m) != tt.size {
				t.Errorf("kantenlänge = %d, erwartet %d", len(m), tt.size)
			}
			if got := matrixHash(m); got != tt.hash {
				t.Errorf("matrix-hash = %s, erwartet %s", got, tt.hash)
			}
		})
	}
}

// TestCodewordsGolden hält Nutz- und Fehlerkorrektur-Codewörter fest. Bricht
// dieser Test, liegt der Fehler in der Kodierung oder im Reed-Solomon-Teil,
// nicht im Matrixaufbau.
func TestCodewordsGolden(t *testing.T) {
	got := qrCodewords("hello", 1)
	want := []byte{
		0x40, 0x56, 0x86, 0x56, 0xc6, 0xc6, 0xf0, 0xec,
		0x11, 0xec, 0x11, 0xec, 0x11, 0xec, 0x11, 0xec,
		0x16, 0x4f, 0xdf, 0xd4, 0x8c, 0x11, 0xd1, 0x5c, 0x2f, 0xb7,
	}
	if !bytes.Equal(got, want) {
		t.Errorf("codewörter\n  = % x\nerwartet\n  = % x", got, want)
	}
}

// TestVersionSelection prüft die Grenzen: jede Version muss ihre größte
// Nutzlast noch fassen und ein Zeichen mehr in die nächste rutschen.
func TestVersionSelection(t *testing.T) {
	for version := 1; version <= len(qrVersionSpecs); version++ {
		spec := qrVersionSpecs[version-1]
		max := (spec.dataCodewords*8 - 4 - qrCharCountBits(version)) / 8
		if got, err := qrVersionFor(max); err != nil || got != version {
			t.Errorf("länge %d: version %d, err %v - erwartet %d", max, got, err, version)
		}
		if version == len(qrVersionSpecs) {
			if _, err := qrVersionFor(max + 1); err == nil {
				t.Error("überlange inhalte müssen einen fehler liefern")
			}
			continue
		}
		if got, _ := qrVersionFor(max + 1); got != version+1 {
			t.Errorf("länge %d: version %d - erwartet %d", max+1, got, version+1)
		}
	}
}

// TestVersionSpecsConsistent prüft die Blocktabelle gegen sich selbst: die
// Summe der Blöcke muss die Zahl der Nutz-Codewörter ergeben.
func TestVersionSpecsConsistent(t *testing.T) {
	for i, spec := range qrVersionSpecs {
		sum := spec.group1Blocks*spec.group1Data + spec.group2Blocks*(spec.group1Data+1)
		if sum != spec.dataCodewords {
			t.Errorf("version %d: blöcke ergeben %d codewörter, tabelle sagt %d", i+1, sum, spec.dataCodewords)
		}
	}
}

// TestFunctionPatterns prüft die Muster, an denen ein Scanner das Symbol
// überhaupt erst findet.
func TestFunctionPatterns(t *testing.T) {
	m := encodeMatrix(t, "otpauth://totp/LCM:admin?secret=JBSWY3DPEHPK3PXP")
	size := len(m)

	corners := map[string][2]int{"oben links": {0, 0}, "oben rechts": {0, size - 7}, "unten links": {size - 7, 0}}
	for name, c := range corners {
		for dr := 0; dr < 7; dr++ {
			for dc := 0; dc < 7; dc++ {
				ring := dr == 0 || dr == 6 || dc == 0 || dc == 6
				core := dr >= 2 && dr <= 4 && dc >= 2 && dc <= 4
				if got := m[c[0]+dr][c[1]+dc]; got != (ring || core) {
					t.Fatalf("suchmuster %s bei (%d,%d) falsch", name, dr, dc)
				}
			}
		}
	}

	for i := 8; i < size-8; i++ {
		if m[6][i] != (i%2 == 0) || m[i][6] != (i%2 == 0) {
			t.Fatalf("taktmuster bei %d falsch", i)
		}
	}

	if !m[size-8][8] {
		t.Error("dunkelmodul fehlt")
	}
}

// TestPNGRendering prüft, dass ein gültiges PNG mit Ruhezone und ganzzahliger
// Modulbreite entsteht.
func TestPNGRendering(t *testing.T) {
	data, err := qrEncode("otpauth://totp/LCM:admin?secret=JBSWY3DPEHPK3PXP", 256)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("kein gültiges png: %v", err)
	}
	span := 33 + 2*qrQuietZone // Version 4 bei dieser Länge
	scale := 256 / span
	if got := img.Bounds().Dx(); got != span*scale {
		t.Errorf("breite = %d, erwartet %d", got, span*scale)
	}
	if img.Bounds().Dx() != img.Bounds().Dy() {
		t.Error("bild ist nicht quadratisch")
	}
	// Die Ecke gehört zur Ruhezone und muss hell sein.
	if r, _, _, _ := img.At(0, 0).RGBA(); r == 0 {
		t.Error("ruhezone fehlt")
	}
}

func TestEncodeTooLong(t *testing.T) {
	if _, err := qrEncode(strings.Repeat("x", 500), 256); err == nil {
		t.Error("überlanger inhalt muss einen fehler liefern")
	}
}
