package domain

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Fehlerklassifizierung der Paketverwaltung.
//
// Die Werkzeuge (apt, dnf, zypper) melden Fehler als Text. Der Klassifizierer
// macht daraus eine der ErrClass*-Klassen, damit die Oberfläche eine
// Handlungsanweisung geben kann („Prozess 1234 beenden", „Platz schaffen")
// statt den Rohtext zu zeigen. Er ist eine reine Funktion ohne Zustand.

const (
	// classifyTailBytes: Fehler stehen am Ende der Ausgabe. Ein Upgrade mit
	// tausenden Zeilen Fortschritt würde sonst bei jedem Fehlschlag vollständig
	// durchsucht - bei jedem Server, jedem Lauf.
	classifyTailBytes = 16 * 1024
	// lockBusyExit ist der Exit-Code „apt ist belegt" der LCM-Skripte (siehe
	// aptLockBusyExit in services/aptlock.go).
	lockBusyExit = 75
	// timeoutExit ist der Exit-Code von timeout(1).
	timeoutExit = 124
	// maxBrokenPackages begrenzt die Paketliste der Klasse broken_deps.
	maxBrokenPackages = 10
	// maxProcessName begrenzt den Prozessnamen des Sperrenhalters.
	maxProcessName = 64
)

// ScannerUnavailableMarker steht in der Fehlermeldung, wenn der CVE-Scan wegen
// des fehlenden Scanners scheitert. Die Meldung wird im Service erzeugt, die
// Klasse hier erkannt - die Konstante hält beide Seiten zusammen.
const ScannerUnavailableMarker = "Trivy nicht verfügbar"

// FailureInput ist, was der Klassifizierer vom Fehlschlag weiß.
type FailureInput struct {
	ExitCode   *int
	Output     string
	ByWatchdog bool // der Job-Watchdog hat den Lauf wegen Stillstand beendet
	// PackageJob: Der Job gehört zur Paketverwaltung. Nur dort bedeutet Exit 75
	// „apt ist belegt" - ein beliebiges Skript darf 75 aus eigenem Grund liefern.
	PackageJob bool
}

// Failure ist das Ergebnis: Klasse, kurzer Beleg und Hinweis-Parameter.
type Failure struct {
	Class   string
	Message string
	Params  map[string]string
}

// ClassifyFailure ordnet einen Fehlschlag einer Fehlerklasse zu. Die
// Reihenfolge der Prüfungen ist Absicht: Die konkretere Ursache gewinnt gegen
// die allgemeinere (eine nicht auflösbare Quelle ist ein Netzproblem, kein
// Quellenfehler).
func ClassifyFailure(in FailureInput) Failure {
	lines := tailLines(in.Output)
	switch {
	case containsLine(lines, ScannerUnavailableMarker) >= 0:
		return failureFromLine(ErrClassScannerUnavailable, lines, ScannerUnavailableMarker)
	case isLockBusy(in, lines):
		return classifyLockBusy(lines)
	case containsLine(lines, "dpkg was interrupted") >= 0:
		return failureFromLine(ErrClassDpkgInterrupted, lines, "dpkg was interrupted")
	case containsLine(lines, diskFullMarkers...) >= 0:
		return classifyDiskFull(lines)
	case containsLine(lines, brokenDepsMarkers...) >= 0:
		return classifyBrokenDeps(lines)
	case containsLine(lines, networkMarkers...) >= 0:
		return failureFromLine(ErrClassNetwork, lines, networkMarkers...)
	case repoErrorLine(lines) >= 0:
		return classifyRepoError(lines)
	case isTimeout(in, lines):
		return classifyTimeout(lines)
	}
	return Failure{Class: ErrClassUnknown, Message: lastLines(lines, 3)}
}

var (
	diskFullMarkers = []string{
		"No space left on device",
		"You don't have enough free space in",
		"more space on the", // dnf: „needs 120MB more space on the / filesystem"
	}
	brokenDepsMarkers = []string{
		"unmet dependencies", "held broken packages", "nothing provides", "conflicting requests",
	}
	networkMarkers = []string{
		"Temporary failure resolving", "Could not resolve", "Couldn't resolve host",
		"Connection timed out", "Network is unreachable", "Could not connect to", "Failed to connect",
	}
	repoErrorMarkers = []string{
		"404  Not Found", "NO_PUBKEY", "is not signed", "does not have a Release file",
		"is not valid yet",
		"Failed to download metadata for repo", "Cannot download repomd.xml",
		"Valid metadata not found",
	}
)

// --- Zeilen ---------------------------------------------------------------------

// tailLines liefert die Zeilen vom Ende der Ausgabe (höchstens classifyTailBytes),
// ohne Steuerzeichen. Der Schnitt liegt auf einer Zeilengrenze: Eine
// angeschnittene erste Zeile ergäbe Treffer auf halbe Meldungen.
func tailLines(output string) []string {
	if len(output) > classifyTailBytes {
		output = output[len(output)-classifyTailBytes:]
		if i := strings.IndexByte(output, '\n'); i >= 0 {
			output = output[i+1:]
		} else {
			output = strings.ToValidUTF8(output, "")
		}
	}
	raw := strings.Split(output, "\n")
	lines := make([]string, 0, len(raw))
	for _, l := range raw {
		lines = append(lines, cleanLine(l))
	}
	return lines
}

// cleanLine entfernt Steuerzeichen (Farbcodes, Wagenrücklauf der
// Fortschrittsanzeigen): Der Text landet in der Oberfläche.
func cleanLine(l string) string {
	return strings.TrimRightFunc(strings.Map(func(r rune) rune {
		if r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, l), unicode.IsSpace)
}

// containsLine liefert den Index der ersten Zeile, die einen der Marker
// enthält, oder -1. Groß-/Kleinschreibung zählt: Die Werkzeuge schreiben ihre
// Meldungen fest.
func containsLine(lines []string, markers ...string) int {
	for i, l := range lines {
		for _, m := range markers {
			if strings.Contains(l, m) {
				return i
			}
		}
	}
	return -1
}

// failureFromLine macht aus der ersten passenden Zeile (samt eingerückter
// Folgezeile) die Meldung der Klasse.
func failureFromLine(class string, lines []string, markers ...string) Failure {
	return Failure{Class: class, Message: lineWithContinuation(lines, containsLine(lines, markers...))}
}

// lineWithContinuation gibt die Zeile i und - falls eingerückt - die nächste
// zurück. apt und dnf setzen die Ursache oft in die Zeile unter der Meldung
// („Err:3 …" / „  404  Not Found").
func lineWithContinuation(lines []string, i int) string {
	if i < 0 {
		return ""
	}
	text := strings.TrimSpace(lines[i])
	if i+1 < len(lines) && strings.HasPrefix(lines[i+1], " ") && strings.TrimSpace(lines[i+1]) != "" {
		text += "\n" + strings.TrimSpace(lines[i+1])
	}
	return truncateRunes(text, capabilityTextMax)
}

// lastLines gibt die letzten n nichtleeren Zeilen zurück - bei unbekannten
// Fehlern steht die Ursache fast immer am Ende.
func lastLines(lines []string, n int) string {
	var picked []string
	for i := len(lines) - 1; i >= 0 && len(picked) < n; i-- {
		if t := strings.TrimSpace(lines[i]); t != "" {
			picked = append([]string{t}, picked...)
		}
	}
	text := strings.Join(picked, "\n")
	if utf8.RuneCountInString(text) > capabilityTextMax {
		// Vom Ende her kürzen: Dort steht die Ursache.
		r := []rune(text)
		text = string(r[len(r)-capabilityTextMax:])
	}
	return text
}

// --- lock_busy ---------------------------------------------------------------------

// Halter einer Sperre. Gesucht wird nur PID und Prozessname: Die Kommandozeile
// des Halters (LCMs eigene Zeile hängt sie an) kann Argumente mit Zugangsdaten
// enthalten und gehört nicht in Meldung oder Parameter.
var lockHolderPatterns = []*regexp.Regexp{
	regexp.MustCompile(`held by process (\d+)(?: \(([^)]*)\))?`),                    // apt
	regexp.MustCompile(`locked by the application with pid (\d+)(?: \(([^)]*)\))?`), // zypper
	regexp.MustCompile(`Waiting for process with pid (\d+)`),                        // dnf
	regexp.MustCompile(`another copy is running as pid (\d+)`),                      // yum
	regexp.MustCompile(`LCM: apt ist .*?PID (\d+)(?: \(([^)]*)\))?`),                // LCM
}

var lockMarkers = []string{
	"Could not get lock",
	"Unable to acquire the dpkg frontend lock",
	"Waiting for process with pid",
	"System management is locked by the application",
	"another copy is running as pid",
	"LCM: apt ist seit",
}

func isLockBusy(in FailureInput, lines []string) bool {
	if in.PackageJob && in.ExitCode != nil && *in.ExitCode == lockBusyExit {
		return true
	}
	return containsLine(lines, lockMarkers...) >= 0
}

// classifyLockBusy liest den Halter aus. Die Meldung ist nur „PID n (name)":
// siehe lockHolderPatterns.
func classifyLockBusy(lines []string) Failure {
	f := Failure{Class: ErrClassLockBusy}
	// Von hinten: Der Halter kann wechseln, die jüngste Meldung gilt.
	for i := len(lines) - 1; i >= 0; i-- {
		for _, re := range lockHolderPatterns {
			m := re.FindStringSubmatch(lines[i])
			if m == nil {
				continue
			}
			f.Params = map[string]string{"pid": m[1]}
			f.Message = "PID " + m[1]
			if len(m) > 2 && strings.TrimSpace(m[2]) != "" {
				name := truncateRunes(strings.TrimSpace(m[2]), maxProcessName)
				f.Params["process"] = name
				f.Message += " (" + name + ")"
			}
			return f
		}
	}
	return f
}

// --- disk_full ---------------------------------------------------------------------

var (
	freeSpacePath = regexp.MustCompile(`free space in (/\S*?)\.?$`)
	filesystemRef = regexp.MustCompile(`more space on the (/\S*) filesystem`)
	quotedPath    = regexp.MustCompile(`'(/[^']+)'`)
)

func classifyDiskFull(lines []string) Failure {
	i := containsLine(lines, diskFullMarkers...)
	f := Failure{Class: ErrClassDiskFull, Message: lineWithContinuation(lines, i)}
	for _, re := range []*regexp.Regexp{freeSpacePath, filesystemRef, quotedPath} {
		if m := re.FindStringSubmatch(lines[i]); m != nil {
			f.Params = map[string]string{"path": m[1]}
			break
		}
	}
	return f
}

// --- broken_deps -------------------------------------------------------------------

var (
	// apt: " libfoo : Depends: libbar (>= 2) but it is not going to be installed"
	aptDependsLine = regexp.MustCompile(`^\s+(\S+)\s*:\s*(?:Depends|PreDepends|Pre-Depends|Conflicts|Breaks):`)
	// dnf/zypper: "nothing provides libbar needed by foo-1.0-1.x86_64"
	neededByLine = regexp.MustCompile(`needed by (?:the to be installed )?(\S+)`)
)

func classifyBrokenDeps(lines []string) Failure {
	f := Failure{Class: ErrClassBrokenDeps, Message: lineWithContinuation(lines, containsLine(lines, brokenDepsMarkers...))}
	var pkgs []string
	seen := map[string]bool{}
	for _, l := range lines {
		for _, re := range []*regexp.Regexp{aptDependsLine, neededByLine} {
			m := re.FindStringSubmatch(l)
			if m != nil && !seen[m[1]] && len(pkgs) < maxBrokenPackages {
				seen[m[1]] = true
				pkgs = append(pkgs, m[1])
			}
		}
	}
	if len(pkgs) > 0 {
		f.Params = map[string]string{"packages": strings.Join(pkgs, ",")}
	}
	return f
}

// --- repo_error --------------------------------------------------------------------

var (
	aptErrLine = regexp.MustCompile(`^Err:\d+\s`)
	// Zu allgemein für einen Marker: „is expired" und „is invalid" kommen
	// auch bei Zertifikaten und Optionen vor.
	repoErrorPatterns = []*regexp.Regexp{
		regexp.MustCompile(`Release file for .* is expired`),
		regexp.MustCompile(`Repository '[^']*' is invalid`),
	}
	// Quellen, wie die Werkzeuge sie benennen. Die Reihenfolge hat keine
	// Bedeutung; die erste Zeile mit Treffer bestimmt die Quelle.
	repoNamePatterns = []*regexp.Regexp{
		regexp.MustCompile(`^Err:\d+\s+(\S+)`),
		regexp.MustCompile(`GPG error: (\S+)`),
		regexp.MustCompile(`The repository '(\S+)`),
		regexp.MustCompile(`Release file for (\S+)`),
		regexp.MustCompile(`Failed to fetch (\S+)`),
		regexp.MustCompile(`[Rr]epo(?:sitory)? ['"]([^'"]+)['"]`),
	}
)

func repoErrorLine(lines []string) int {
	for i, l := range lines {
		if aptErrLine.MatchString(l) {
			return i
		}
		for _, re := range repoErrorPatterns {
			if re.MatchString(l) {
				return i
			}
		}
		for _, m := range repoErrorMarkers {
			if strings.Contains(l, m) {
				return i
			}
		}
	}
	return -1
}

func classifyRepoError(lines []string) Failure {
	f := Failure{Class: ErrClassRepoError, Message: lineWithContinuation(lines, repoErrorLine(lines))}
	params := map[string]string{}
	if repo := firstRepoName(lines); repo != "" {
		params["repo"] = repo
	}
	if containsLine(lines, "is not valid yet") >= 0 {
		params["not_valid_yet"] = "true"
	}
	if len(params) > 0 {
		f.Params = params
	}
	return f
}

func firstRepoName(lines []string) string {
	for _, l := range lines {
		for _, re := range repoNamePatterns {
			if m := re.FindStringSubmatch(strings.TrimSpace(l)); m != nil {
				return truncateRunes(m[1], capabilityTextMax)
			}
		}
	}
	return ""
}

// --- timeout -----------------------------------------------------------------------

func isTimeout(in FailureInput, lines []string) bool {
	if in.ByWatchdog || (in.ExitCode != nil && *in.ExitCode == timeoutExit) {
		return true
	}
	i := containsLine(lines, "apt-get update nach")
	return i >= 0 && strings.Contains(lines[i], "abgebrochen")
}

// classifyTimeout nimmt LCMs eigene Zeile als Meldung, sonst die letzten
// Zeilen: Sie zeigen, woran der Lauf hing.
func classifyTimeout(lines []string) Failure {
	if i := containsLine(lines, "apt-get update nach"); i >= 0 {
		return Failure{Class: ErrClassTimeout, Message: lineWithContinuation(lines, i)}
	}
	return Failure{Class: ErrClassTimeout, Message: lastLines(lines, 3)}
}

// --- apt-get update ----------------------------------------------------------------

// RefreshResult wertet die Ausgabe eines Paketlisten-Laufs aus
// (`apt-get update` oder `zypper refresh`).
type RefreshResult struct {
	Sources       int      // verschiedene Paketquellen (apt: URL + Suite, zypper: Name)
	FailedSources []string // gescheiterte Quellen (apt: Err:-Zeile als „URL Suite", zypper: Name)
	FatalError    bool     // allgemeiner Fehler ohne Bezug auf eine einzelne Quelle
	Warnings      bool     // mindestens eine W:-Zeile (nur apt)
}

// Failed: der Lauf ist ein Fehlschlag (rot). Das gilt bei einem allgemeinen E:
// - Sperre, kaputter dpkg-Zustand - und wenn ALLE Quellen scheitern: Dann gibt
// es keine brauchbaren Paketlisten, auch wenn apt selbst nur Quellenfehler
// meldet.
func (r RefreshResult) Failed() bool {
	return r.FatalError || (r.Sources > 0 && len(r.FailedSources) == r.Sources)
}

// PartiallyFailed: einzelne Quellen scheitern, der Lauf gilt aber als Erfolg
// mit gelbem Hinweis (repo_error). Eine kaputte Fremdquelle darf die
// Updates aus den übrigen nicht blockieren - und soll trotzdem nicht
// unbemerkt bleiben. Nur W:-Zeilen sind bloße Information.
func (r RefreshResult) PartiallyFailed() bool {
	return !r.Failed() && len(r.FailedSources) > 0
}

var (
	aptSourceLine = regexp.MustCompile(`^(?:Hit|Get|Ign|Err):\d+\s+(\S+)(?:\s+(\S+))?`)
	// E:-Zeilen, die eine einzelne Quelle betreffen - sie sagen nichts über
	// den Zustand von apt selbst.
	repoSpecificError = []string{
		"The repository ", "Release file for ", "Failed to fetch ", "GPG error:",
	}
)

// ClassifyRefresh wertet die Ausgabe eines Paketlisten-Laufs aus. Den
// Paketmanager erkennt es an der Ausgabe: Der Job trägt ihn nicht, und
// zypper-Zeilen („Retrieving repository", „Repository '…' is up to date")
// kommen in der apt-Ausgabe nicht vor.
func ClassifyRefresh(output string) RefreshResult {
	if zypperRepoLine.MatchString(output) {
		return ClassifyZypperRefresh(output)
	}
	return ClassifyAptUpdate(output)
}

// ClassifyAptUpdate wertet die Ausgabe von `apt-get update` aus.
func ClassifyAptUpdate(output string) RefreshResult {
	var res RefreshResult
	sources := map[string]bool{}
	failed := map[string]bool{}
	for _, line := range strings.Split(output, "\n") {
		line = cleanLine(line)
		switch {
		case strings.HasPrefix(line, "E: "):
			res.FatalError = res.FatalError || !isRepoSpecificError(line)
		case strings.HasPrefix(line, "W: "):
			res.Warnings = true
		}
		m := aptSourceLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		key := sourceKey(m[1], m[2])
		sources[key] = true
		if strings.HasPrefix(line, "Err:") && !failed[key] {
			failed[key] = true
			res.FailedSources = append(res.FailedSources, key)
		}
	}
	res.Sources = len(sources)
	return res
}

func isRepoSpecificError(line string) bool {
	for _, m := range repoSpecificError {
		if strings.Contains(line, m) {
			return true
		}
	}
	return false
}

// sourceKey benennt eine Quelle „URL Suite". Die Zeilen der Indexdateien
// nennen die Suite mit Komponente („jammy-updates/main"); sie gehören zur
// selben Quelle wie ihre InRelease-Zeile.
func sourceKey(url, suite string) string {
	if i := strings.Index(suite, "/"); i > 0 {
		suite = suite[:i]
	}
	return strings.TrimSpace(url + " " + suite)
}

// --- zypper refresh ----------------------------------------------------------------

var (
	// Jede Quelle nennt zypper mit ihrem Namen in einer dieser Zeilen; die
	// Namen können Leerzeichen und Klammern enthalten („repo-oss (16.0)").
	zypperRepoLine    = regexp.MustCompile(`(?m)^\s*(?:Retrieving repository '.+' metadata|Repository '.+' is up to date|Skipping repository '.+' because of the above error)`)
	zypperRepoSeen    = regexp.MustCompile(`^(?:Retrieving repository|Repository) '(.+)' (?:metadata|is up to date)`)
	zypperRepoSkipped = regexp.MustCompile(`^Skipping repository '(.+)' because of the above error`)
)

// zypperAllFailed steht am Ende, wenn keine Quelle aktualisiert werden konnte.
// Bei einem Teilausfall steht dort „Some of the repositories have not been
// refreshed because of an error." - der Exit-Code ist in beiden Fällen 4.
const zypperAllFailed = "Could not refresh the repositories because of errors"

// ClassifyZypperRefresh wertet die Ausgabe von `zypper refresh` aus. Eine
// übersprungene Quelle („Skipping repository …") gilt als gescheitert; scheitern
// alle, meldet zypper das zusätzlich selbst und der Lauf ist ein Fehlschlag.
func ClassifyZypperRefresh(output string) RefreshResult {
	var res RefreshResult
	sources := map[string]bool{}
	failed := map[string]bool{}
	for _, line := range strings.Split(output, "\n") {
		line = cleanLine(line)
		if m := zypperRepoSeen.FindStringSubmatch(line); m != nil {
			sources[m[1]] = true
		}
		if m := zypperRepoSkipped.FindStringSubmatch(line); m != nil {
			sources[m[1]] = true
			if !failed[m[1]] {
				failed[m[1]] = true
				res.FailedSources = append(res.FailedSources, m[1])
			}
		}
		if strings.Contains(line, zypperAllFailed) {
			res.FatalError = true
		}
	}
	res.Sources = len(sources)
	return res
}
