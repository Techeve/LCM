package cron

import "time"

// Schedule liefert zu einer Zeit den nächsten Termin danach. Die Nullzeit
// heißt: innerhalb von fünf Jahren gibt es keinen.
type Schedule interface {
	Next(time.Time) time.Time
}

// specSchedule ist ein Ausdruck aus fünf Feldern als Bitmengen. Sekunden
// gibt es nicht; jeder Termin fällt auf Sekunde 0.
type specSchedule struct {
	minute, hour, dom, month, dow uint64
	// location ist die Zone aus dem Präfix; time.Local heißt: die Zone der
	// übergebenen Zeit.
	location *time.Location
}

// search ist der Zustand der Terminsuche in specSchedule.Next.
type search struct {
	t   time.Time
	loc *time.Location
	// added: Ein Feld wurde schon weitergezählt. Ab dann beginnen die
	// feineren Felder bei null statt bei der Startzeit.
	added bool
}

// restart setzt t einmalig auf from, sobald zum ersten Mal ein Feld
// weiterzählt.
func (s *search) restart(from time.Time) {
	if !s.added {
		s.added = true
		s.t = from
	}
}

// Next sucht wie die frühere Bibliothek: Monat, Tag, Stunde, Minute und Sekunde werden
// nacheinander weitergezählt, bis sie passen. Läuft ein Feld dabei über,
// beginnt die Prüfung beim Monat von vorn. Die Eigenheiten bei
// Zeitumstellungen sind bewusst übernommen: Ein Termin in einer
// übersprungenen Stunde entfällt an diesem Tag, eine doppelte Stunde läuft
// je nach Startzeit ein- oder zweimal.
func (s *specSchedule) Next(t time.Time) time.Time {
	origLocation := t.Location()
	loc := s.location
	if loc == time.Local {
		loc = t.Location()
	}
	if s.location != time.Local {
		t = t.In(s.location)
	}

	// Frühestens zur nächsten vollen Sekunde.
	t = t.Add(time.Second - time.Duration(t.Nanosecond()))
	sr := &search{t: t, loc: loc}
	yearLimit := t.Year() + 5
	for sr.t.Year() <= yearLimit {
		if sr.month(s.month) && sr.day(s) && sr.hour(s.hour) && sr.minute(s.minute) && sr.second() {
			return sr.t.In(origLocation)
		}
	}
	return time.Time{}
}

// month, day, hour, minute und second zählen ihr Feld weiter, bis es passt.
// Sie melden false, wenn das Feld dabei übergelaufen ist und die Suche von
// vorn beginnen muss.

func (s *search) month(bits uint64) bool {
	for 1<<uint(s.t.Month())&bits == 0 {
		s.restart(time.Date(s.t.Year(), s.t.Month(), 1, 0, 0, 0, 0, s.loc))
		s.t = s.t.AddDate(0, 1, 0)
		if s.t.Month() == time.January {
			return false
		}
	}
	return true
}

func (s *search) day(sched *specSchedule) bool {
	for !dayMatches(sched, s.t) {
		s.restart(time.Date(s.t.Year(), s.t.Month(), s.t.Day(), 0, 0, 0, 0, s.loc))
		s.t = s.t.AddDate(0, 0, 1)
		// Wo Mitternacht durch eine Zeitumstellung fehlt oder doppelt ist,
		// landet AddDate eine Stunde daneben; zurück auf Stunde 0.
		if h := s.t.Hour(); h > 12 {
			s.t = s.t.Add(time.Duration(24-h) * time.Hour)
		} else if h != 0 {
			s.t = s.t.Add(time.Duration(-h) * time.Hour)
		}
		if s.t.Day() == 1 {
			return false
		}
	}
	return true
}

func (s *search) hour(bits uint64) bool {
	for 1<<uint(s.t.Hour())&bits == 0 {
		s.restart(time.Date(s.t.Year(), s.t.Month(), s.t.Day(), s.t.Hour(), 0, 0, 0, s.loc))
		s.t = s.t.Add(time.Hour)
		if s.t.Hour() == 0 {
			return false
		}
	}
	return true
}

func (s *search) minute(bits uint64) bool {
	for 1<<uint(s.t.Minute())&bits == 0 {
		s.restart(s.t.Truncate(time.Minute))
		s.t = s.t.Add(time.Minute)
		if s.t.Minute() == 0 {
			return false
		}
	}
	return true
}

func (s *search) second() bool {
	for s.t.Second() != 0 {
		s.restart(s.t.Truncate(time.Second))
		s.t = s.t.Add(time.Second)
		if s.t.Second() == 0 {
			return false
		}
	}
	return true
}

// dayMatches verknüpft Tag des Monats und Wochentag wie bisher: mit UND,
// sobald eins der Felder mit * oder ? beginnt, sonst mit ODER („0 0 13 * 5"
// läuft am 13. und an jedem Freitag).
func dayMatches(s *specSchedule, t time.Time) bool {
	domMatch := 1<<uint(t.Day())&s.dom > 0
	dowMatch := 1<<uint(t.Weekday())&s.dow > 0
	if s.dom&starBit > 0 || s.dow&starBit > 0 {
		return domMatch && dowMatch
	}
	return domMatch || dowMatch
}

// every ist ein Deskriptor „@every <Dauer>": fester Abstand ab der
// übergebenen Zeit, auf volle Sekunden.
type every struct {
	delay time.Duration
}

// newEvery rundet wie bisher: unter einer Sekunde wird eine Sekunde daraus,
// Bruchteile einer Sekunde fallen weg.
func newEvery(d time.Duration) every {
	if d < time.Second {
		d = time.Second
	}
	return every{delay: d - d%time.Second}
}

func (e every) Next(t time.Time) time.Time {
	return t.Add(e.delay - time.Duration(t.Nanosecond()))
}
