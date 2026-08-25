package totp

import "fmt"

// Codewort-Aufbau und Reed-Solomon-Fehlerkorrektur für die QR-Kodierung nach
// ISO/IEC 18004. Bewusst nur der Teil, den LCM braucht: Byte-Modus,
// Fehlerkorrekturstufe M, Versionen 1-10 (bis 213 Zeichen). Die otpauth-URI
// ist rund 120 Zeichen lang, größere Inhalte gibt es hier nicht.

// qrVersionSpec beschreibt den Blockaufbau einer Version bei Stufe M. Blöcke
// der zweiten Gruppe tragen immer genau ein Nutz-Codewort mehr als die der
// ersten - das gilt in der gesamten Norm.
type qrVersionSpec struct {
	dataCodewords int
	ecPerBlock    int
	group1Blocks  int
	group1Data    int
	group2Blocks  int
}

var qrVersionSpecs = []qrVersionSpec{
	{16, 10, 1, 16, 0},
	{28, 16, 1, 28, 0},
	{44, 26, 1, 44, 0},
	{64, 18, 2, 32, 0},
	{86, 24, 2, 43, 0},
	{108, 16, 4, 27, 0},
	{124, 18, 4, 31, 0},
	{154, 22, 2, 38, 2},
	{182, 22, 3, 36, 2},
	{216, 26, 4, 43, 1},
}

// qrCharCountBits: der Byte-Modus nutzt 8 Bit für die Längenangabe bis
// Version 9, darüber 16.
func qrCharCountBits(version int) int {
	if version >= 10 {
		return 16
	}
	return 8
}

// qrVersionFor wählt die kleinste Version, in die n Bytes passen.
func qrVersionFor(n int) (int, error) {
	for version := 1; version <= len(qrVersionSpecs); version++ {
		free := qrVersionSpecs[version-1].dataCodewords*8 - 4 - qrCharCountBits(version)
		if n*8 <= free {
			return version, nil
		}
	}
	return 0, fmt.Errorf("inhalt zu lang für einen qr-code: %d zeichen", n)
}

// qrBits sammelt einen Bitstrom byteweise, höchstwertiges Bit zuerst.
type qrBits struct {
	bytes []byte
	n     int
}

func (b *qrBits) add(value uint, bits int) {
	for i := bits - 1; i >= 0; i-- {
		if b.n%8 == 0 {
			b.bytes = append(b.bytes, 0)
		}
		if value>>uint(i)&1 == 1 {
			b.bytes[b.n/8] |= 1 << uint(7-b.n%8)
		}
		b.n++
	}
}

// qrDataCodewords baut den Nutzdatenblock: Modusindikator, Längenangabe,
// Inhalt, Terminator und Füllbytes bis zur Kapazität der Version.
func qrDataCodewords(content string, version int) []byte {
	spec := qrVersionSpecs[version-1]
	var b qrBits
	b.add(0b0100, 4) // Byte-Modus
	b.add(uint(len(content)), qrCharCountBits(version))
	for _, c := range []byte(content) {
		b.add(uint(c), 8)
	}
	for i := 0; i < 4 && b.n < spec.dataCodewords*8; i++ {
		b.add(0, 1) // Terminator
	}
	for b.n%8 != 0 {
		b.add(0, 1)
	}
	pad := []byte{0xEC, 0x11}
	for i := 0; len(b.bytes) < spec.dataCodewords; i++ {
		b.bytes = append(b.bytes, pad[i%2])
	}
	return b.bytes
}

// qrCodewords verschränkt Nutz- und Fehlerkorrektur-Codewörter aller Blöcke in
// die Reihenfolge, in der sie in die Matrix wandern.
func qrCodewords(content string, version int) []byte {
	spec := qrVersionSpecs[version-1]
	data := qrDataCodewords(content, version)

	blocks := make([][]byte, 0, spec.group1Blocks+spec.group2Blocks)
	for i, pos := 0, 0; i < spec.group1Blocks+spec.group2Blocks; i++ {
		size := spec.group1Data
		if i >= spec.group1Blocks {
			size++
		}
		blocks = append(blocks, data[pos:pos+size])
		pos += size
	}

	ecBlocks := make([][]byte, len(blocks))
	for i, block := range blocks {
		ecBlocks[i] = qrErrorCorrection(block, spec.ecPerBlock)
	}

	out := make([]byte, 0, len(data)+len(blocks)*spec.ecPerBlock)
	return qrInterleave(qrInterleave(out, blocks), ecBlocks)
}

// qrInterleave hängt die Blöcke spaltenweise an: erst das jeweils erste
// Codewort jedes Blocks, dann das zweite und so fort.
func qrInterleave(out []byte, blocks [][]byte) []byte {
	longest := 0
	for _, b := range blocks {
		if len(b) > longest {
			longest = len(b)
		}
	}
	for i := 0; i < longest; i++ {
		for _, b := range blocks {
			if i < len(b) {
				out = append(out, b[i])
			}
		}
	}
	return out
}

// Galois-Feld GF(256) mit dem primitiven Polynom x^8+x^4+x^3+x^2+1 (0x11d)
// und Generator 2 - so schreibt es die Norm vor.
var (
	gfExp [512]byte
	gfLog [256]byte
)

func init() {
	x := 1
	for i := 0; i < 255; i++ {
		gfExp[i] = byte(x)
		gfLog[x] = byte(i)
		if x <<= 1; x&0x100 != 0 {
			x ^= 0x11d
		}
	}
	for i := 255; i < len(gfExp); i++ {
		gfExp[i] = gfExp[i-255]
	}
}

func gfMul(a, b byte) byte {
	if a == 0 || b == 0 {
		return 0
	}
	return gfExp[int(gfLog[a])+int(gfLog[b])]
}

// qrGenerator liefert das Reed-Solomon-Generatorpolynom vom Grad n,
// Koeffizienten nach absteigender Potenz.
func qrGenerator(n int) []byte {
	g := []byte{1}
	for i := 0; i < n; i++ {
		next := make([]byte, len(g)+1)
		for j, c := range g {
			next[j] ^= c
			next[j+1] ^= gfMul(c, gfExp[i])
		}
		g = next
	}
	return g
}

// qrErrorCorrection berechnet die n Fehlerkorrektur-Codewörter zu data als
// Rest der Polynomdivision durch das Generatorpolynom.
func qrErrorCorrection(data []byte, n int) []byte {
	gen := qrGenerator(n)
	rem := make([]byte, n)
	for _, d := range data {
		factor := d ^ rem[0]
		copy(rem, rem[1:])
		rem[n-1] = 0
		for i, g := range gen[1:] {
			rem[i] ^= gfMul(g, factor)
		}
	}
	return rem
}
