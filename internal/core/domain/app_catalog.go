package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Anwendungskatalog: Software, die nicht aus der Paketverwaltung kommt.
//
// AdGuard Home liegt unter /opt, Nextcloud im Webroot, mailcow ist ein
// git-Checkout - für die Paketverwaltung existiert nichts davon. Genau diese
// Anwendungen tragen aber Sicherheitslücken und wollen aktualisiert werden.
// Ohne Katalog fallen sie durch jedes Raster.
//
// Warum das nicht automatisch geht: Dass ein Dienst „AdGuardHome" heißt, sagt
// nichts darüber, wie man seine Version erfährt. Das ist bei jeder Anwendung
// anders - ein Kommando hier, eine VERSION-Datei dort, ein git-Tag beim
// dritten. Dieses Wissen muss irgendwo stehen, und weil es nicht vollständig
// vorhersehbar ist, steht es in einem PFLEGBAREN Katalog: LCM liefert Einträge
// mit, jeder kann eigene ergänzen.
//
// Die zweite Hälfte ist der generische Fund (siehe UnknownApp): laufende
// Dienste, deren Unit keinem Paket gehört. Der findet auch, was im Katalog
// fehlt - und liefert die Vorlage für einen neuen Eintrag.

// Merkmal-Arten: woran LCM erkennt, dass eine Anwendung installiert ist.
const (
	// MarkerPath: Datei oder Verzeichnis existiert. Der Fundort wird zum
	// Installationspfad und ist im Versionskommando als {path} verfügbar.
	MarkerPath = "path"
	// MarkerUnit: es gibt eine systemd-Unit dieses Namens.
	MarkerUnit = "unit"
	// MarkerBin: das Programm liegt im PATH.
	MarkerBin = "bin"
	// MarkerProc: ein Prozess dieses Namens läuft. Bewusst das schwächste
	// Merkmal und deshalb das letzte: Intrexx läuft als `java`, Nextcloud als
	// `php-fpm` - als alleiniges Kennzeichen wäre das wertlos.
	MarkerProc = "proc"
)

// Vergleichsarten für „installiert" gegen „neueste".
const (
	// CompareSemver: 1.10.0 ist neuer als 1.9.0, ein führendes v stört nicht.
	CompareSemver = "semver"
	// CompareExact: verschieden heißt veraltet. Für alles, was sich nicht in
	// Zahlen zerlegen lässt - Datumsstände, Kanalnamen, Build-Kennungen.
	CompareExact = "exact"
	// CompareNone: nur anzeigen, nie bewerten. Der ehrliche Ausweg, wenn die
	// Versionsangabe nicht vergleichbar ist. Ein Reiter, der zu Unrecht
	// „veraltet" meldet, ist nach dem zweiten Fehlalarm verbrannt.
	CompareNone = "none"
)

// MaxAppSlugLen begrenzt den technischen Namen eines Katalogeintrags.
const MaxAppSlugLen = 40

var (
	// ErrAppSlug: der technische Name passt nicht auf die Konvention.
	ErrAppSlug = errors.New("ungültiger technischer name (klein, ziffern, bindestrich)")
	// ErrAppName: Anzeigename fehlt.
	ErrAppName = errors.New("name ist erforderlich")
	// ErrAppMarker: eine Merkmal-Zeile ist unbrauchbar.
	ErrAppMarker = errors.New("ungültiges merkmal")
	// ErrAppNoMarker: ohne Merkmal kann nichts erkannt werden.
	ErrAppNoMarker = errors.New("mindestens ein merkmal ist erforderlich")
	// ErrAppCompare: unbekannte Vergleichsart.
	ErrAppCompare = errors.New("unbekannte vergleichsart")
	// ErrAppPattern: das Versions-Muster ist kein gültiger regulärer Ausdruck.
	ErrAppPattern = errors.New("ungültiges versions-muster")
	// ErrAppSource: die Quelle für die neueste Version ist unbrauchbar.
	ErrAppSource = errors.New("ungültige quelle für die neueste version")
)

var reAppSlug = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// AppCatalogEntry ist der Steckbrief einer Anwendung: woran man sie erkennt,
// wie man ihre Version erfährt, wo die neueste steht und womit sie sich
// sichern und aktualisieren lässt.
type AppCatalogEntry struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	Slug        string `gorm:"uniqueIndex;not null" json:"slug"`
	Name        string `gorm:"not null" json:"name"`
	Description string `json:"description"`
	// NameEN/DescriptionEN sind die englischen Fassungen; leer heißt Rückfall
	// auf die deutsche (siehe ProfileBlock).
	NameEN        string `gorm:"column:name_en" json:"name_en"`
	DescriptionEN string `gorm:"column:description_en" json:"description_en"`
	// Builtin: von LCM mitgeliefert. Wird beim Seeden gepflegt; selbst
	// angelegte Einträge bleiben unangetastet.
	Builtin bool `gorm:"default:false" json:"builtin"`
	// Enabled: abschaltbar, ohne den Eintrag zu verlieren.
	Enabled bool `gorm:"default:true" json:"enabled"`

	// Markers: eine Merkmal-Zeile je Zeile, "art wert" - erster Treffer
	// gewinnt. Beispiel: "path /opt/AdGuardHome/AdGuardHome".
	Markers string `gorm:"not null" json:"markers"`

	// VersionCommand ermittelt die installierte Version. {path} wird durch den
	// Fundort ersetzt. Leer heißt: Version unbekannt, nur die Anwesenheit
	// wird gemeldet.
	VersionCommand string `json:"version_command"`
	// VersionPattern schneidet die Version aus der Ausgabe (erste Gruppe,
	// sonst der ganze Treffer). Leer: die erste nichtleere Zeile.
	VersionPattern string `json:"version_pattern"`
	// Compare: semver|exact|none.
	Compare string `gorm:"default:'semver'" json:"compare"`

	// LatestSource ist die Quelle der neuesten Version: "github:owner/repo"
	// für den Regelfall, sonst "url:https://…" zusammen mit LatestPattern.
	LatestSource  string `json:"latest_source"`
	LatestPattern string `json:"latest_pattern"`
	// LatestVersion/LatestCheckedAt/LatestError halten das Ergebnis des
	// letzten Abgleichs. Die Abfrage gehört zur Anwendung, nicht zum Server -
	// bei 40 Servern mit derselben Anwendung wäre alles andere 40-mal
	// dieselbe Anfrage.
	LatestVersion   string     `json:"latest_version"`
	LatestCheckedAt *time.Time `json:"latest_checked_at"`
	LatestError     string     `json:"latest_error"`

	// BackupActionID/UpdateActionID verweisen auf Eigene Aktionen. Bewusst ein
	// Verweis und keine rohe Kommandozeile: So erben Sicherung und Update die
	// Rechteprüfung, das Job-Protokoll und die Nachvollziehbarkeit, statt
	// daneben einen zweiten, ungeschützten Ausführungsweg aufzumachen.
	BackupActionID *uint `json:"backup_action_id"`
	UpdateActionID *uint `json:"update_action_id"`
}

// AppMarker ist ein einzelnes Erkennungsmerkmal.
type AppMarker struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

// ParseAppMarkers zerlegt die Merkmal-Zeilen eines Eintrags.
func ParseAppMarkers(s string) ([]AppMarker, error) {
	var out []AppMarker
	for _, raw := range strings.Split(s, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		kind, value, ok := strings.Cut(line, " ")
		value = strings.TrimSpace(value)
		if !ok || value == "" {
			return nil, fmt.Errorf("%w: %q - erwartet: art wert", ErrAppMarker, line)
		}
		switch kind {
		case MarkerPath, MarkerUnit, MarkerBin, MarkerProc:
		default:
			return nil, fmt.Errorf("%w: unbekannte art %q (path|unit|bin|proc)", ErrAppMarker, kind)
		}
		if !SafeShellValue(value) {
			return nil, fmt.Errorf("%w: %q enthält Sonderzeichen", ErrAppMarker, value)
		}
		out = append(out, AppMarker{Kind: kind, Value: value})
	}
	if len(out) == 0 {
		return nil, ErrAppNoMarker
	}
	return out, nil
}

// unsafeShellChars sind die Zeichen, die einen Wert für eine Kommandozeile
// unbrauchbar machen. Merkmale werden in ein Skript eingesetzt, das als root
// läuft - Pfade und Dienstnamen brauchen keines dieser Zeichen.
const unsafeShellChars = "&|$`'\"\\;<>(){}*?[]!\n\t "

// SafeShellValue sagt, ob der Wert gefahrlos in ein Kommando eingesetzt
// werden kann.
func SafeShellValue(v string) bool {
	return v != "" && !strings.ContainsAny(v, unsafeShellChars)
}

// ValidateAppEntry prüft einen Katalogeintrag, bevor er gespeichert wird.
func ValidateAppEntry(e *AppCatalogEntry) error {
	e.Slug = strings.TrimSpace(strings.ToLower(e.Slug))
	e.Name = strings.TrimSpace(e.Name)
	if e.Slug == "" || len(e.Slug) > MaxAppSlugLen || !reAppSlug.MatchString(e.Slug) {
		return ErrAppSlug
	}
	if e.Name == "" {
		return ErrAppName
	}
	if _, err := ParseAppMarkers(e.Markers); err != nil {
		return err
	}
	if e.Compare == "" {
		e.Compare = CompareSemver
	}
	switch e.Compare {
	case CompareSemver, CompareExact, CompareNone:
	default:
		return fmt.Errorf("%w: %q", ErrAppCompare, e.Compare)
	}
	if e.VersionPattern != "" {
		if _, err := regexp.Compile(e.VersionPattern); err != nil {
			return fmt.Errorf("%w: %v", ErrAppPattern, err)
		}
	}
	if e.LatestPattern != "" {
		if _, err := regexp.Compile(e.LatestPattern); err != nil {
			return fmt.Errorf("%w: %v", ErrAppPattern, err)
		}
	}
	return validateLatestSource(e.LatestSource)
}

var reGitHubRepo = regexp.MustCompile(`^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$`)

func validateLatestSource(src string) error {
	src = strings.TrimSpace(src)
	if src == "" {
		return nil
	}
	kind, value, ok := strings.Cut(src, ":")
	if !ok || value == "" {
		return fmt.Errorf("%w: erwartet github:owner/repo oder url:https://…", ErrAppSource)
	}
	switch kind {
	case "github":
		if !reGitHubRepo.MatchString(value) {
			return fmt.Errorf("%w: %q ist kein owner/repo", ErrAppSource, value)
		}
	case "url":
		if !strings.HasPrefix(value, "https://") {
			return fmt.Errorf("%w: nur https-Adressen", ErrAppSource)
		}
	default:
		return fmt.Errorf("%w: unbekannte art %q", ErrAppSource, kind)
	}
	return nil
}

// ExtractVersion schneidet die Version aus der Ausgabe des
// Versionskommandos. Ohne Muster: die erste nichtleere Zeile.
func ExtractVersion(out, pattern string) string {
	out = strings.TrimSpace(out)
	if out == "" {
		return ""
	}
	if pattern == "" {
		for _, line := range strings.Split(out, "\n") {
			if line = strings.TrimSpace(line); line != "" {
				return line
			}
		}
		return ""
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return ""
	}
	m := re.FindStringSubmatch(out)
	switch {
	case m == nil:
		return ""
	case len(m) > 1:
		return strings.TrimSpace(m[1])
	default:
		return strings.TrimSpace(m[0])
	}
}

// AppUpdateAvailable sagt, ob die installierte Version hinter der neuesten
// liegt. Bei CompareNone, fehlenden Angaben oder einer Version, die sich der
// gewählten Vergleichsart entzieht, lautet die Antwort false - LCM behauptet
// dann lieber nichts, als einen Fehlalarm zu erzeugen.
func AppUpdateAvailable(installed, latest, compare string) bool {
	installed, latest = strings.TrimSpace(installed), strings.TrimSpace(latest)
	if installed == "" || latest == "" || compare == CompareNone {
		return false
	}
	if compare == CompareExact {
		return installed != latest
	}
	a, aok := parseVersionNumbers(installed)
	b, bok := parseVersionNumbers(latest)
	if !aok || !bok {
		return false
	}
	for i := 0; i < len(a) || i < len(b); i++ {
		x, y := at(a, i), at(b, i)
		if x != y {
			return y > x
		}
	}
	return false
}

func at(v []int, i int) int {
	if i < len(v) {
		return v[i]
	}
	return 0
}

// reVersionNumbers zieht die Zahlenfolge aus einer Versionsangabe: aus
// "v0.107.52", "24.0.7.1" oder "nginx/1.24.0" wird dieselbe Liste. Was gar
// keine Zahlen trägt, ist für den semver-Vergleich nicht brauchbar.
var reVersionNumbers = regexp.MustCompile(`[0-9]+(?:\.[0-9]+)*`)

func parseVersionNumbers(v string) ([]int, bool) {
	m := reVersionNumbers.FindString(v)
	if m == "" {
		return nil, false
	}
	parts := strings.Split(m, ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, false
		}
		out = append(out, n)
	}
	return out, true
}

// LocalizedName liefert den Namen in der gewünschten Sprache.
func (e AppCatalogEntry) LocalizedName(lang string) string {
	return pickLanguage(lang, e.Name, e.NameEN)
}

// LocalizedDescription liefert die Beschreibung in der gewünschten Sprache.
func (e AppCatalogEntry) LocalizedDescription(lang string) string {
	return pickLanguage(lang, e.Description, e.DescriptionEN)
}
