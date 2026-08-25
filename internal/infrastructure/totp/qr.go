package totp

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"slices"
)

// QR-Code-Erzeugung nach ISO/IEC 18004 (Byte-Modus, Fehlerkorrekturstufe M).
//
// Selbst gebaut statt als Abhängigkeit: keine Kryptographie, keine fremde
// Eingabe (die otpauth-URI bauen wir selbst) und ein harmloses Fehlerbild -
// ein defekter Code heißt „Secret manuell eintippen", kein Sicherheitsproblem.
// Begründung und Regel: docs/reference/dependencies.md.

// qrQuietZone ist der helle Rand in Modulen, den die Norm vorschreibt.
const qrQuietZone = 4

// qrFormatBits sind die fertigen 15-Bit-BCH-Formatinformationen für
// Fehlerkorrekturstufe M, indiziert nach Maskennummer (Tabelle der Norm).
var qrFormatBits = [8]uint{0x5412, 0x5125, 0x5E7C, 0x5B4B, 0x45F9, 0x40CE, 0x4F97, 0x4AA0}

// qrVersionBits sind die 18-Bit-Versionsinformationen; erst ab Version 7 trägt
// das Symbol sie überhaupt.
var qrVersionBits = map[int]uint{7: 0x07C94, 8: 0x085BC, 9: 0x09A99, 10: 0x0A4D3}

// qrAlignmentCenters listet die Mittelpunkte der Ausrichtungsmuster je Version
// (Index = Version-1); Version 1 hat keine.
var qrAlignmentCenters = [][]int{
	nil, {6, 18}, {6, 22}, {6, 26}, {6, 30}, {6, 34},
	{6, 22, 38}, {6, 24, 42}, {6, 26, 46}, {6, 28, 50},
}

// qrEncode kodiert content als PNG-QR-Code mit einer Kantenlänge von etwa
// targetSize Pixeln.
func qrEncode(content string, targetSize int) ([]byte, error) {
	version, err := qrVersionFor(len(content))
	if err != nil {
		return nil, err
	}
	m := newQRMatrix(version)
	m.placeData(qrCodewords(content, version))
	return qrPNG(m.bestMasked(), targetSize)
}

// qrMatrix hält die Modulmatrix im Aufbau. reserved markiert Funktionsmuster
// und Formatbereiche: dort landen keine Daten, und die Maske greift nicht.
type qrMatrix struct {
	size     int
	version  int
	modules  [][]bool
	reserved [][]bool
}

func newQRMatrix(version int) *qrMatrix {
	size := version*4 + 17
	m := &qrMatrix{size: size, version: version}
	m.modules = make([][]bool, size)
	m.reserved = make([][]bool, size)
	for i := range m.modules {
		m.modules[i] = make([]bool, size)
		m.reserved[i] = make([]bool, size)
	}
	m.placeFinders()
	m.placeTiming()
	m.placeAlignment()
	m.placeVersion()
	m.reserveFormat()
	return m
}

func (m *qrMatrix) set(row, col int, dark bool) {
	m.modules[row][col] = dark
	m.reserved[row][col] = true
}

func (m *qrMatrix) placeFinders() {
	for _, corner := range [][2]int{{0, 0}, {0, m.size - 7}, {m.size - 7, 0}} {
		m.placeFinderAt(corner[0], corner[1])
	}
}

// placeFinderAt zeichnet ein Suchmuster samt hellem Trennstreifen; der Bereich
// von -1 bis 7 deckt den Streifen mit ab.
func (m *qrMatrix) placeFinderAt(row, col int) {
	for dr := -1; dr <= 7; dr++ {
		for dc := -1; dc <= 7; dc++ {
			r, c := row+dr, col+dc
			if r < 0 || r >= m.size || c < 0 || c >= m.size {
				continue
			}
			onPattern := dr >= 0 && dr <= 6 && dc >= 0 && dc <= 6
			ring := dr == 0 || dr == 6 || dc == 0 || dc == 6
			core := dr >= 2 && dr <= 4 && dc >= 2 && dc <= 4
			m.set(r, c, onPattern && (ring || core))
		}
	}
}

func (m *qrMatrix) placeTiming() {
	for i := 8; i < m.size-8; i++ {
		m.set(6, i, i%2 == 0)
		m.set(i, 6, i%2 == 0)
	}
}

// placeAlignment setzt die Ausrichtungsmuster an alle Kombinationen der
// Mittelpunkte - außer an den drei Ecken, wo die Suchmuster stehen.
func (m *qrMatrix) placeAlignment() {
	centers := qrAlignmentCenters[m.version-1]
	last := len(centers) - 1
	for i, row := range centers {
		for j, col := range centers {
			atFinder := (i == 0 && j == 0) || (i == 0 && j == last) || (i == last && j == 0)
			if atFinder {
				continue
			}
			m.placeAlignmentAt(row, col)
		}
	}
}

func (m *qrMatrix) placeAlignmentAt(row, col int) {
	for dr := -2; dr <= 2; dr++ {
		for dc := -2; dc <= 2; dc++ {
			edge := dr == -2 || dr == 2 || dc == -2 || dc == 2
			m.set(row+dr, col+dc, edge || (dr == 0 && dc == 0))
		}
	}
}

// placeVersion schreibt die Versionsinformation in die beiden 3x6-Blöcke neben
// den Suchmustern unten links und oben rechts.
func (m *qrMatrix) placeVersion() {
	bits, ok := qrVersionBits[m.version]
	if !ok {
		return
	}
	for i := 0; i < 18; i++ {
		dark := bits>>uint(i)&1 == 1
		row, col := m.size-11+i%3, i/3
		m.set(row, col, dark)
		m.set(col, row, dark)
	}
}

// reserveFormat hält die Felder der Formatinformation frei; gefüllt werden sie
// erst, wenn die Maske feststeht.
func (m *qrMatrix) reserveFormat() {
	for i := 0; i <= 8; i++ {
		m.reserved[8][i] = true
		m.reserved[i][8] = true
	}
	for i := 0; i < 8; i++ {
		m.reserved[m.size-1-i][8] = true
		m.reserved[8][m.size-1-i] = true
	}
}

// placeData verteilt die Codewörter im Zickzack: spaltenweise von rechts nach
// links, je zwei Spalten breit, abwechselnd auf- und absteigend. Die
// senkrechte Taktspalte 6 wird übersprungen.
func (m *qrMatrix) placeData(data []byte) {
	bit := 0
	upward := true
	for right := m.size - 1; right >= 1; right -= 2 {
		if right == 6 {
			right = 5
		}
		for i := 0; i < m.size; i++ {
			row := i
			if upward {
				row = m.size - 1 - i
			}
			for _, col := range [2]int{right, right - 1} {
				if m.reserved[row][col] {
					continue
				}
				if bit < len(data)*8 {
					m.modules[row][col] = data[bit/8]>>uint(7-bit%8)&1 == 1
				}
				bit++
			}
		}
		upward = !upward
	}
}

// bestMasked probiert alle acht Masken durch und liefert die Variante mit der
// kleinsten Strafpunktzahl - so schreibt es die Norm vor.
func (m *qrMatrix) bestMasked() [][]bool {
	var best [][]bool
	bestScore := -1
	for mask := 0; mask < 8; mask++ {
		candidate := m.masked(mask)
		if score := qrPenalty(candidate); bestScore < 0 || score < bestScore {
			best, bestScore = candidate, score
		}
	}
	return best
}

// masked liefert eine fertige Kopie der Matrix: Maske auf die Datenmodule
// angewandt und die dazu passende Formatinformation eingetragen.
func (m *qrMatrix) masked(mask int) [][]bool {
	out := make([][]bool, m.size)
	for r := range out {
		out[r] = make([]bool, m.size)
		copy(out[r], m.modules[r])
		for c := range out[r] {
			if !m.reserved[r][c] && qrMaskApplies(mask, r, c) {
				out[r][c] = !out[r][c]
			}
		}
	}
	qrPlaceFormat(out, mask)
	return out
}

func qrMaskApplies(mask, row, col int) bool {
	switch mask {
	case 0:
		return (row+col)%2 == 0
	case 1:
		return row%2 == 0
	case 2:
		return col%3 == 0
	case 3:
		return (row+col)%3 == 0
	case 4:
		return (row/2+col/3)%2 == 0
	case 5:
		return row*col%2+row*col%3 == 0
	case 6:
		return (row*col%2+row*col%3)%2 == 0
	default:
		return ((row+col)%2+row*col%3)%2 == 0
	}
}

// qrPlaceFormat trägt die 15 Bit Formatinformation an beiden vorgeschriebenen
// Stellen ein und setzt zuletzt das immer dunkle Modul. Die erste Kopie läuft
// senkrecht neben dem oberen linken Suchmuster und knickt in Zeile 8 nach
// links ab, die zweite von rechts in Zeile 8 und senkrecht nach unten.
func qrPlaceFormat(m [][]bool, mask int) {
	size := len(m)
	bit := func(i int) bool { return qrFormatBits[mask]>>uint(i)&1 == 1 }
	for i := 0; i <= 5; i++ {
		m[i][8] = bit(i)
	}
	m[7][8] = bit(6)
	m[8][8] = bit(7)
	m[8][7] = bit(8)
	for i := 9; i < 15; i++ {
		m[8][14-i] = bit(i)
	}
	for i := 0; i < 8; i++ {
		m[8][size-1-i] = bit(i)
	}
	for i := 8; i < 15; i++ {
		m[size-15+i][8] = bit(i)
	}
	m[size-8][8] = true
}

// qrPenalty bewertet ein fertig maskiertes Symbol nach den vier Regeln der
// Norm; je kleiner, desto besser lesbar.
func qrPenalty(m [][]bool) int {
	score := qrPenaltyBlocks(m) + qrPenaltyBalance(m)
	for i := range m {
		for _, line := range [2][]bool{m[i], qrColumn(m, i)} {
			score += qrRunScore(line) + qrFinderLikeScore(line)
		}
	}
	return score
}

func qrColumn(m [][]bool, col int) []bool {
	out := make([]bool, len(m))
	for r := range m {
		out[r] = m[r][col]
	}
	return out
}

// qrRunScore: Regel 1 - jede Kette ab fünf gleichfarbigen Modulen kostet
// 3 Punkte, jedes weitere Modul einen zusätzlichen.
func qrRunScore(line []bool) int {
	score, run := 0, 1
	for i := 1; i <= len(line); i++ {
		if i < len(line) && line[i] == line[i-1] {
			run++
			continue
		}
		if run >= 5 {
			score += run - 2
		}
		run = 1
	}
	return score
}

// qrPenaltyBlocks: Regel 2 - jeder gleichfarbige 2x2-Block kostet 3 Punkte.
func qrPenaltyBlocks(m [][]bool) int {
	score := 0
	for r := 0; r+1 < len(m); r++ {
		for c := 0; c+1 < len(m); c++ {
			v := m[r][c]
			if m[r][c+1] == v && m[r+1][c] == v && m[r+1][c+1] == v {
				score += 3
			}
		}
	}
	return score
}

// qrFinderLike sind die beiden Folgen, die einem Suchmuster ähneln und einen
// Scanner in die Irre führen können (Regel 3).
var qrFinderLike = [2][11]bool{
	{true, false, true, true, true, false, true, false, false, false, false},
	{false, false, false, false, true, false, true, true, true, false, true},
}

func qrFinderLikeScore(line []bool) int {
	score := 0
	for _, pattern := range qrFinderLike {
		for i := 0; i+len(pattern) <= len(line); i++ {
			if slices.Equal(line[i:i+len(pattern)], pattern[:]) {
				score += 40
			}
		}
	}
	return score
}

// qrPenaltyBalance: Regel 4 - je weiter der Dunkelanteil von 50 % abweicht,
// desto mehr Punkte, in Stufen von 5 Prozentpunkten.
func qrPenaltyBalance(m [][]bool) int {
	dark := 0
	for _, row := range m {
		for _, v := range row {
			if v {
				dark++
			}
		}
	}
	deviation := dark*100/(len(m)*len(m)) - 50
	if deviation < 0 {
		deviation = -deviation
	}
	return deviation / 5 * 10
}

// qrPNG rendert die Modulmatrix als PNG samt hellem Rand. Die Kantenlänge wird
// auf ein ganzzahliges Vielfaches der Modulgröße abgerundet, damit alle Module
// gleich breit bleiben.
func qrPNG(modules [][]bool, targetSize int) ([]byte, error) {
	span := len(modules) + 2*qrQuietZone
	scale := targetSize / span
	if scale < 1 {
		scale = 1
	}
	img := image.NewPaletted(
		image.Rect(0, 0, span*scale, span*scale),
		color.Palette{color.White, color.Black},
	)
	for r, row := range modules {
		for c, dark := range row {
			if !dark {
				continue
			}
			for y := 0; y < scale; y++ {
				for x := 0; x < scale; x++ {
					img.SetColorIndex((c+qrQuietZone)*scale+x, (r+qrQuietZone)*scale+y, 1)
				}
			}
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
