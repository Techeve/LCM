// Package cron liest Cron-Ausdrücke und führt Aufgaben nach ihnen aus. Es
// ersetzt die frühere, seit 2020 ungepflegte Cron-Bibliothek (siehe
// docs/reference/dependencies.md) und nimmt genau die Sprache an, die LCM mit
// ihr angenommen hat: fünf Felder (Minute, Stunde, Tag, Monat, Wochentag),
// Deskriptoren wie @daily und @every 1h und ein optionales Präfix CRON_TZ=
// bzw. TZ=.
//
// Administratoren haben ihre Ausdrücke unter der alten Bibliothek
// gespeichert. Deshalb gleichen Annahme, Fehlertexte und Termine ihr bis in
// die Eigenheiten hinein; testdata/golden.jsonl hält ihr Verhalten fest
// (Generator in testdata/gen), und golden_test.go prüft dagegen. Einzige
// Abweichung: Ein Zeitzonen-Präfix ohne folgendes Leerzeichen („TZ=UTC") ließ
// die alte Bibliothek in einen Panic laufen, hier ist es ein Fehler.
package cron

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// bounds beschreibt den Wertebereich eines Feldes samt erlaubter Namen.
type bounds struct {
	min, max uint
	names    map[string]uint
}

var (
	minutes = bounds{0, 59, nil}
	hours   = bounds{0, 23, nil}
	dom     = bounds{1, 31, nil}
	months  = bounds{1, 12, map[string]uint{
		"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6,
		"jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12,
	}}
	dow = bounds{0, 6, map[string]uint{
		"sun": 0, "mon": 1, "tue": 2, "wed": 3, "thu": 4, "fri": 5, "sat": 6,
	}}
)

// starBit markiert ein Feld, das mit * oder ? beginnt (ohne Schritt über 1).
// Davon hängt ab, ob Tag des Monats und Wochentag mit UND oder ODER
// verknüpft werden (siehe dayMatches).
const starBit = 1 << 63

// Parse liest einen Cron-Ausdruck. Ohne Zeitzonen-Präfix gilt die Zeitzone
// der Zeit, die Next übergeben wird.
func Parse(spec string) (Schedule, error) {
	if spec == "" {
		return nil, errors.New("empty spec string")
	}
	loc := time.Local
	if strings.HasPrefix(spec, "TZ=") || strings.HasPrefix(spec, "CRON_TZ=") {
		var err error
		if loc, spec, err = splitLocation(spec); err != nil {
			return nil, err
		}
	}
	if strings.HasPrefix(spec, "@") {
		return parseDescriptor(spec, loc)
	}
	fields := strings.Fields(spec)
	if len(fields) != 5 {
		return nil, fmt.Errorf("expected exactly 5 fields, found %d: %s", len(fields), fields)
	}
	s := &specSchedule{location: loc}
	targets := []*uint64{&s.minute, &s.hour, &s.dom, &s.month, &s.dow}
	for i, r := range []bounds{minutes, hours, dom, months, dow} {
		bits, err := parseField(fields[i], r)
		if err != nil {
			return nil, err
		}
		*targets[i] = bits
	}
	return s, nil
}

// splitLocation trennt das Zeitzonen-Präfix vom Rest. Wie bisher endet
// der Zonenname am ersten Leerzeichen, nicht an beliebigem Leerraum.
func splitLocation(spec string) (*time.Location, string, error) {
	eq := strings.Index(spec, "=")
	space := strings.Index(spec, " ")
	if space < 0 {
		return nil, "", fmt.Errorf("provided bad location %s: no schedule after time zone", spec[eq+1:])
	}
	name := spec[eq+1 : space]
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, "", fmt.Errorf("provided bad location %s: %v", name, err)
	}
	return loc, strings.TrimSpace(spec[space:]), nil
}

// parseField liest eine kommagetrennte Liste von Bereichen. Leere Einträge
// („1,,15" oder nur „,") überspringt es; ein Feld ohne jeden Eintrag trifft
// nie.
func parseField(field string, r bounds) (uint64, error) {
	var bits uint64
	for expr := range strings.SplitSeq(field, ",") {
		if expr == "" {
			continue
		}
		bit, err := parseRange(expr, r)
		if err != nil {
			return 0, err
		}
		bits |= bit
	}
	return bits, nil
}

// parseRange liest einen Bereich: Zahl oder Name, * oder ?, optional mit
// „-Ende" und „/Schritt". „N/Schritt" läuft bis zum Feldende. Nach * oder ?
// wird ein „-Ende" überlesen, wie es bisher war („*-5" heißt „*").
func parseRange(expr string, r bounds) (uint64, error) {
	rangeAndStep := strings.Split(expr, "/")
	lowAndHigh := strings.Split(rangeAndStep[0], "-")
	start, end, extra, err := parseBounds(expr, lowAndHigh, r)
	if err != nil {
		return 0, err
	}

	var step uint = 1
	switch len(rangeAndStep) {
	case 1:
	case 2:
		if step, err = parseInt(rangeAndStep[1]); err != nil {
			return 0, err
		}
		if len(lowAndHigh) == 1 {
			end = r.max
		}
		if step > 1 {
			extra = 0
		}
	default:
		return 0, fmt.Errorf("too many slashes: %s", expr)
	}

	switch {
	case start < r.min:
		return 0, fmt.Errorf("beginning of range (%d) below minimum (%d): %s", start, r.min, expr)
	case end > r.max:
		return 0, fmt.Errorf("end of range (%d) above maximum (%d): %s", end, r.max, expr)
	case start > end:
		return 0, fmt.Errorf("beginning of range (%d) beyond end of range (%d): %s", start, end, expr)
	case step == 0:
		return 0, fmt.Errorf("step of range should be a positive number: %s", expr)
	}
	return bitRange(start, end, step) | extra, nil
}

// parseBounds liest Anfang und Ende eines Bereichs; extra ist starBit bei *
// und ?.
func parseBounds(expr string, lowAndHigh []string, r bounds) (start, end uint, extra uint64, err error) {
	if lowAndHigh[0] == "*" || lowAndHigh[0] == "?" {
		return r.min, r.max, starBit, nil
	}
	if start, err = parseIntOrName(lowAndHigh[0], r.names); err != nil {
		return 0, 0, 0, err
	}
	switch len(lowAndHigh) {
	case 1:
		return start, start, 0, nil
	case 2:
		end, err = parseIntOrName(lowAndHigh[1], r.names)
		return start, end, 0, err
	default:
		return 0, 0, 0, fmt.Errorf("too many hyphens: %s", expr)
	}
}

func parseIntOrName(expr string, names map[string]uint) (uint, error) {
	if n, ok := names[strings.ToLower(expr)]; ok {
		return n, nil
	}
	return parseInt(expr)
}

func parseInt(expr string) (uint, error) {
	num, err := strconv.Atoi(expr)
	if err != nil {
		return 0, fmt.Errorf("failed to parse int from %s: %s", expr, err)
	}
	if num < 0 {
		return 0, fmt.Errorf("negative number (%d) not allowed: %s", num, expr)
	}
	return uint(num), nil
}

// bitRange setzt die Bits von lo bis hi im Abstand step.
func bitRange(lo, hi, step uint) uint64 {
	if step == 1 {
		return ^(math.MaxUint64 << (hi + 1)) & (math.MaxUint64 << lo)
	}
	var bits uint64
	for i := lo; i <= hi; i += step {
		bits |= 1 << i
	}
	return bits
}

// all setzt alle Bits eines Feldes samt starBit.
func all(r bounds) uint64 {
	return bitRange(r.min, r.max, 1) | starBit
}

// parseDescriptor liest @yearly, @monthly, … und @every <Dauer>. Die Namen
// müssen klein geschrieben sein und ohne Leerraum dahinter stehen.
func parseDescriptor(descriptor string, loc *time.Location) (Schedule, error) {
	midnight := specSchedule{
		minute: 1 << minutes.min, hour: 1 << hours.min,
		dom: all(dom), month: all(months), dow: all(dow), location: loc,
	}
	switch descriptor {
	case "@yearly", "@annually":
		midnight.dom, midnight.month = 1<<dom.min, 1<<months.min
		return &midnight, nil
	case "@monthly":
		midnight.dom = 1 << dom.min
		return &midnight, nil
	case "@weekly":
		midnight.dow = 1 << dow.min
		return &midnight, nil
	case "@daily", "@midnight":
		return &midnight, nil
	case "@hourly":
		midnight.hour = all(hours)
		return &midnight, nil
	}

	const every = "@every "
	if !strings.HasPrefix(descriptor, every) {
		return nil, fmt.Errorf("unrecognized descriptor: %s", descriptor)
	}
	d, err := time.ParseDuration(descriptor[len(every):])
	if err != nil {
		return nil, fmt.Errorf("failed to parse duration %s: %s", descriptor, err)
	}
	return newEvery(d), nil
}
