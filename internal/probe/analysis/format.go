package analysis

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// Zahlen und Zeiten im Bericht stehen deutsch: Komma, Tag.Monat.

var weekdays = [...]string{"So", "Mo", "Di", "Mi", "Do", "Fr", "Sa"}

// clock ist ein Zeitpunkt mit Wochentag, Datum und Uhrzeit.
func clock(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	t = t.Local()
	return weekdays[t.Weekday()] + " " + t.Format("02.01. 15:04:05")
}

// duration schreibt eine Dauer kurz: 850 ms, 96 s, 2 min 18 s, 3 h 5 min.
func duration(d time.Duration) string {
	switch {
	case d < time.Second:
		return num(float64(d.Microseconds())/1000, 0) + " ms"
	case d < 100*time.Second:
		return num(d.Seconds(), 1) + " s"
	case d < time.Hour:
		return fmt.Sprintf("%d min %d s", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%d h %d min", int(d.Hours()), int(d.Minutes())%60)
}

// num schreibt eine Zahl mit prec Nachkommastellen und Komma.
func num(v float64, prec int) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return "-"
	}
	return strings.Replace(strconv.FormatFloat(v, 'f', prec, 64), ".", ",", 1)
}

// count schreibt eine ganze Zahl mit Punkten: 12.345.
func count[T int | int64 | uint64](v T) string {
	s := strconv.FormatInt(int64(v), 10)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(r)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

// bytesText schreibt Bytes in der passenden Einheit.
func bytesText[T int64 | uint64](v T) string {
	f := float64(v)
	for _, unit := range []string{"B", "KB", "MB", "GB"} {
		if math.Abs(f) < 1000 {
			return num(f, precFor(unit)) + " " + unit
		}
		f /= 1000
	}
	return num(f, 2) + " TB"
}

func precFor(unit string) int {
	if unit == "B" {
		return 0
	}
	return 1
}

// waitText schreibt eine Stufe der Wartezeit: „≤ 5 ms" oder „> 10 s".
func waitText(bound float64, above bool) string {
	if above {
		return "> " + msText(bound)
	}
	return "≤ " + msText(bound)
}

func msText(ms float64) string {
	if ms >= 1000 {
		return num(ms/1000, 1) + " s"
	}
	prec := 0
	if ms < 1 {
		prec = 1
	}
	return num(ms, prec) + " ms"
}
