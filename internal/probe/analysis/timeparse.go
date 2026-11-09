package analysis

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

var timeLayouts = []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02 15:04:05", "2006-01-02 15:04", time.DateOnly}

// ParseTime liest eine Zeitangabe: Datum mit oder ohne Uhrzeit, nur Uhrzeit
// (am Tag von anchor) oder relativ wie 90m, 6h, 7d (vor anchor). anchor ist
// die jüngste Zeile der Daten, damit kopierte ältere Tage genauso gehen.
func ParseTime(s string, anchor time.Time) (time.Time, error) {
	s = strings.TrimSpace(s)
	if d, ok := parseRelative(s); ok {
		return anchor.Add(-d), nil
	}
	for _, layout := range timeLayouts {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t, nil
		}
	}
	for _, layout := range []string{"15:04:05", "15:04"} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			a := anchor.Local()
			return time.Date(a.Year(), a.Month(), a.Day(), t.Hour(), t.Minute(), t.Second(), 0, time.Local), nil
		}
	}
	return time.Time{}, fmt.Errorf("zeitangabe %q nicht lesbar (Beispiele: 2026-11-08T04:00, 04:00, 6h, 7d)", s)
}

func parseRelative(s string) (time.Duration, bool) {
	if days, ok := strings.CutSuffix(s, "d"); ok {
		n, err := strconv.Atoi(days)
		return time.Duration(n) * 24 * time.Hour, err == nil && n >= 0
	}
	d, err := time.ParseDuration(s)
	return d, err == nil && d >= 0
}
