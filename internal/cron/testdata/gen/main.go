//go:build ignore

// Erzeugt testdata/golden.jsonl: das Verhalten von robfig/cron v3.0.1 für
// alle Ausdrucksformen, die LCM annimmt. Das Paket cron muss dieselben
// Ausdrücke annehmen, dieselben ablehnen (mit demselben Fehlertext) und
// dieselben Termine liefern - golden_test.go prüft das.
//
// Eigenes Modul (go.mod daneben), damit robfig/cron nicht in den
// Abhängigkeitsgraphen von LCM gerät. Neu erzeugen:
//
//	cd internal/cron/testdata/gen && go run main.go > ../golden.jsonl
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/robfig/cron/v3"
)

// Derselbe Parser wie bisher in LCM (services.cronParser und cron.New).
var parser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

// chain ist die Zahl der Folgetermine je Startzeit.
const chain = 5

var specs = []string{
	// Feste Takte von LCM (scheduler.go, backupCron, updateCheckSpec, Vorgaben).
	"30 3 * * *", "*/30 * * * *", "*/15 * * * *", "*/10 * * * *", "10 * * * *",
	"45 4 * * *", "15 4 * * *", "15 5 * * *", "30 2 * * *",
	"37 1-23/3 * * *", "0 0-23/3 * * *", "59 2-23/3 * * *",
	"0 2,8,14,20 * * *", "30 0,6,12,18 * * *", "0 3 * * *", "0 0,12 * * *",
	"0 */2 * * *", "*/5 * * * *", "0 * * * *", "* * * * *",

	// Schritte und Bereiche.
	"*/7 * * * *", "5-59/15 * * * *", "5/15 * * * *", "*/1 * * * *", "0/1 * * * *",
	"59/1 * * * *", "0 */5 * * *", "0 0 */3 * *", "0 0 1 */2 *", "0 0 * * */2",
	"1-10/3 2-20/4 * * *", "*/60 * * * *", "*/100 * * * *", "0-59/59 * * * *",
	"1-1 * * * *", "?/5 * * * *", "*-5 * * * *", "15 10-12 * * *",

	// Listen.
	"0,15,30,45 * * * *", "1,2,3-5,10-20/5 * * * *", "0 9,17 * * 1-5",
	"0 0 1,,15 * *", "0 0 , * *",

	// Namen (Groß-/Kleinschreibung egal).
	"0 0 1 JAN *", "0 0 * * MON", "0 0 * * mon-fri", "0 12 * jan-mar,oct *",
	"0 0 * * SUN,SAT", "0 0 1 Dec *", "0 0 * * Sun", "0 8 * FEB-APR/2 TUE",

	// Fragezeichen.
	"0 0 ? * MON", "0 0 1 * ?", "? ? ? ? ?",

	// Tag des Monats und Wochentag: ODER, sobald keins der Felder mit * beginnt.
	"0 0 13 * 5", "0 0 1 * 1", "0 0 1-7 * 1", "0 0 * * 1", "0 0 1 * *",
	"0 0 */2 * 1", "0 0 */1 * 1", "0 0 1-31 * 1", "0 0 ? * 1", "0 0 1 * */1",
	"0 0 1 * */2", "0 0 15 * 0,6",

	// Seltene und unmögliche Tage.
	"0 0 31 2 *", "0 0 30 2 *", "0 0 29 2 *", "0 0 31 4 *", "0 0 31 * *",
	"0 0 31 4,6 *", "0 0 29 2 1", "0 0 29 2 */2",

	// Rund um Zeitumstellungen.
	"0 2 * * *", "59 1 * * *", "0 1 * * *", "30 1 * * *", "*/30 2 * * *",
	"0 0 * * *", "30 0 * * *", "45 2 * * *", "0 23 * * *",

	// Zahlenformen, die strconv.Atoi annimmt.
	"+5 * * * *", "05 * * * *", "-0 * * * *", "0 0 * * 0", "0 0 * * 6",

	// Leerraum.
	" 0 0 * * * ", "0\t0 * * *", "0  0 * * *", "\n0 0 * * *",

	// Deskriptoren.
	"@yearly", "@annually", "@monthly", "@weekly", "@daily", "@midnight", "@hourly",
	"@every 1h", "@every 30m", "@every 3h", "@every 36h", "@every 1h30m", "@every 90s",
	"@every 1.5s", "@every 500ms", "@every 0s", "@every -5m", "@every 1h0m0.5s",
	"@every 1us", "@every 24h", "@every 168h",

	// Zeitzonen-Präfix.
	"CRON_TZ=Europe/Berlin 30 2 * * *", "TZ=America/New_York 0 1 * * *",
	"CRON_TZ=UTC @daily", "TZ=Asia/Tokyo @every 1h", "CRON_TZ=Local 0 0 * * *",
	"TZ= 0 0 * * *", "CRON_TZ=Europe/Berlin  0 0 * * *", "TZ=UTC 0 */6 * * *",
	"CRON_TZ=Australia/Lord_Howe 15 2 * * *", "TZ=Europe/Berlin @weekly",

	// Abgelehnt.
	"", " ", "* * * *", "* * * * * *", "60 * * * *", "* 24 * * *", "* * 0 * *",
	"* * 32 * *", "* * * 13 *", "* * * 0 *", "* * * * 7", "5-1 * * * *",
	"1-2-3 * * * *", "*/0 * * * *", "1/2/3 * * * *", "a * * * *", "-1 * * * *",
	"* * * * MONDAY", "* * * JANUARY *", "MON * * * *", "@daily ", "@Daily",
	"@every", "@every ", "@every 1x", "@every 1h ", "@every  1h", "@reboot", "@",
	"TZ=Mars/Phobos 0 0 * * *", "CRON_TZ=Europe/Berlin\t0 0 * * *", "0 0 * * 1-",
	"0 0 * * -1", "*/ * * * *", "1/ * * * *", "0x10 * * * *", "1.5 * * * *",
	"99999999999999999999 * * * *", "TZ=UTC ", "0 0 * * mon-", "0 0 * * fri-mon",
	"0 0 L * *", "0 0 1W * *", "0 0 * * 1#2", "0 0 * * 5L", "0 0 0 1 *",

	// robfig gerät hier in einen Panic (Präfix ohne Leerzeichen).
	"TZ=Europe/Berlin", "CRON_TZ=UTC", "TZ=UTC@daily",
}

// starts deckt Monats- und Jahresenden, Schaltjahre, die Fünfjahresgrenze
// der Suche und die Zeitumstellungen mehrerer Zonen ab. Mehrdeutige
// Ortszeiten sind über den Versatz eindeutig angegeben.
var starts = []struct{ loc, at string }{
	{"UTC", "2026-11-09T10:17:23.5Z"},
	{"UTC", "2026-12-31T23:59:59Z"},
	{"UTC", "2027-02-28T23:59:30Z"},
	{"UTC", "2028-02-28T12:00:00Z"},
	{"UTC", "2028-02-29T00:00:00Z"},
	{"UTC", "2097-03-01T00:00:00Z"},
	{"UTC", "2026-01-31T23:59:59.999999999Z"},
	{"UTC", "2030-06-15T08:00:00Z"},
	{"Europe/Berlin", "2026-03-29T01:30:00+01:00"},
	{"Europe/Berlin", "2026-03-29T01:59:59+01:00"},
	{"Europe/Berlin", "2026-03-28T23:00:00+01:00"},
	{"Europe/Berlin", "2026-10-25T01:59:00+02:00"},
	{"Europe/Berlin", "2026-10-25T02:30:00+02:00"},
	{"Europe/Berlin", "2026-10-25T02:30:00+01:00"},
	{"Europe/Berlin", "2026-10-24T22:00:00+02:00"},
	{"Europe/Berlin", "2026-11-08T00:21:00+01:00"},
	{"Europe/Berlin", "2026-07-01T12:00:00+02:00"},
	{"Europe/Berlin", "2027-03-28T00:00:00+01:00"},
	{"America/New_York", "2026-03-08T01:30:00-05:00"},
	{"America/New_York", "2026-03-07T23:00:00-05:00"},
	{"America/New_York", "2026-11-01T00:30:00-04:00"},
	{"America/New_York", "2026-11-01T01:30:00-04:00"},
	{"America/New_York", "2026-11-01T01:30:00-05:00"},
	{"America/New_York", "2026-12-31T23:30:00-05:00"},
	{"America/Sao_Paulo", "2018-11-03T23:00:00-03:00"},
	{"America/Sao_Paulo", "2018-11-03T12:00:00-03:00"},
	{"America/Sao_Paulo", "2019-02-16T22:00:00-02:00"},
	{"Australia/Lord_Howe", "2026-10-04T01:30:00+10:30"},
	{"Australia/Lord_Howe", "2026-04-05T01:00:00+11:00"},
	{"Australia/Sydney", "2026-04-05T02:30:00+11:00"},
	{"Asia/Kolkata", "2026-05-31T23:45:00+05:30"},
}

// accepted ist eine Zeile für einen angenommenen Ausdruck und eine Startzeit.
type accepted struct {
	Spec  string   `json:"spec"`
	Loc   string   `json:"loc"`
	Start string   `json:"start"`
	Next  []string `json:"next"`
}

// rejected ist eine Zeile für einen abgelehnten Ausdruck; Panic ist gesetzt,
// wenn robfig statt eines Fehlers in einen Panic lief.
type rejected struct {
	Spec  string `json:"spec"`
	Error string `json:"error"`
	Panic bool   `json:"panic,omitempty"`
}

func main() {
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	for _, spec := range specs {
		sched, panicked, err := parse(spec)
		if err != nil {
			must(enc.Encode(rejected{Spec: spec, Error: err.Error(), Panic: panicked}))
			continue
		}
		for _, s := range starts {
			must(enc.Encode(run(sched, spec, s.loc, s.at)))
		}
	}
}

func parse(spec string) (sched cron.Schedule, panicked bool, err error) {
	defer func() {
		if r := recover(); r != nil {
			panicked, err = true, fmt.Errorf("panic: %v", r)
		}
	}()
	sched, err = parser.Parse(spec)
	return sched, false, err
}

func run(sched cron.Schedule, spec, locName, at string) accepted {
	loc, err := time.LoadLocation(locName)
	must(err)
	start, err := time.Parse(time.RFC3339Nano, at)
	must(err)
	out := accepted{Spec: spec, Loc: locName, Start: at}
	t := start.In(loc)
	for range chain {
		t = sched.Next(t)
		if t.IsZero() {
			out.Next = append(out.Next, "")
			break
		}
		if t.Location().String() != locName {
			must(fmt.Errorf("%q: Ergebnis in %s statt %s", spec, t.Location(), locName))
		}
		out.Next = append(out.Next, t.Format(time.RFC3339Nano))
	}
	return out
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
