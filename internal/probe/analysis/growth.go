package analysis

import (
	"cmp"
	"slices"
	"time"
)

// growthTables begrenzt die Tabellen im Bericht auf die mit dem größten
// Zuwachs.
const growthTables = 12

// Growth ist das Wachstum je Tag: Datei und Tabellen.
type Growth struct {
	Days   []string
	File   []FileDay
	Tables []TableGrowth
}

// FileDay ist der Stand der Datei am Ende eines Tages.
type FileDay struct {
	Day       string
	DBBytes   int64
	WALBytes  int64
	FreeBytes int64 // freie Seiten in der Datei
}

// TableGrowth ist der Zuwachs einer Tabelle je Tag. PerDay[0] zählt ab der
// ersten Zählung im Zeitraum.
type TableGrowth struct {
	Table       string
	First, Last int64
	PerDay      []int64
}

func growth(d *Data) Growth {
	var g Growth
	lastOfDay := map[string]DBPoint{}
	countsOfDay := map[string]tableRows{}
	var firstCount tableRows
	for _, p := range d.DBs {
		day := p.Time().Format(time.DateOnly)
		if _, seen := lastOfDay[day]; !seen {
			g.Days = append(g.Days, day)
		}
		lastOfDay[day] = p
		tr := p.Snap.TableRows
		if tr.CountedAt.IsZero() {
			continue
		}
		if firstCount.CountedAt.IsZero() {
			firstCount = tr
		}
		countsOfDay[day] = tr
	}
	for _, day := range g.Days {
		f := lastOfDay[day].Snap.File
		g.File = append(g.File, FileDay{Day: day, DBBytes: f.DBBytes, WALBytes: f.WALBytes, FreeBytes: f.FreelistCount * f.PageSize})
	}
	g.Tables = tableGrowth(g.Days, firstCount, countsOfDay)
	return g
}

func tableGrowth(days []string, first tableRows, byDay map[string]tableRows) []TableGrowth {
	var out []TableGrowth
	for table, start := range first.Tables {
		tg := TableGrowth{Table: table, First: start, Last: start}
		prev := start
		for _, day := range days {
			n, ok := byDay[day].Tables[table]
			if !ok {
				n = prev // an diesem Tag nicht gezählt
			}
			tg.PerDay = append(tg.PerDay, n-prev)
			prev, tg.Last = n, n
		}
		out = append(out, tg)
	}
	slices.SortFunc(out, func(a, b TableGrowth) int {
		return cmp.Or(cmp.Compare(b.Last-b.First, a.Last-a.First), cmp.Compare(b.Last, a.Last), cmp.Compare(a.Table, b.Table))
	})
	if len(out) > growthTables {
		out = out[:growthTables]
	}
	return out
}
