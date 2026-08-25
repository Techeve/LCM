package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Regelbausteine: wiederverwendbare Rechte-Vorlagen, aus denen sich ein
// Berechtigungsprofil zusammenklicken lässt.
//
// Der Grund ist Erfahrung, keine Bequemlichkeit: sudo-Regeln von Hand zu
// schreiben ist mühsam und fehleranfällig - also trägt jemand
// „/usr/bin/systemctl" ein, weil der Kollege „Dienste verwalten" soll, und hat
// volle Root-Rechte vergeben. Ein Baustein „Apache betreiben" zum Anhaken
// beseitigt genau diesen Weg.
//
// Zwei Eigenschaften machen Bausteine über Distributionen hinweg brauchbar:
//
//   - Sie tragen je Distributionsfamilie eine eigene Variante. Die Unit heißt
//     auf Debian/Ubuntu `apache2`, auf RHEL und SUSE `httpd`; Binärpfade
//     weichen ebenso ab.
//   - Sie haben Parameter. Ein Baustein „Systemd-Dienst betreiben" deckt damit
//     nginx, postgresql und eigene Units ab, statt für jede Anwendung einen
//     neuen zu erzwingen.

// BlockFamilyAll gilt, wenn für die Familie eines Servers keine eigene
// Variante hinterlegt ist - der Normalfall identischer Zeilen.
const BlockFamilyAll = "all"

// MaxBlockSlugLen begrenzt den technischen Namen eines Bausteins.
const MaxBlockSlugLen = 40

// ProfileBlock ist eine benannte Rechte-Vorlage.
type ProfileBlock struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	Name        string `gorm:"uniqueIndex;not null" json:"name"`
	Slug        string `gorm:"uniqueIndex;not null" json:"slug"`
	Description string `json:"description"`
	// NameEN/DescriptionEN sind die englischen Fassungen. Leer heißt: Es gibt
	// keine - dann zeigt die Oberfläche die deutsche. Bewusst zwei Spalten
	// statt einer Übersetzungstabelle: Ein Katalogeintrag hat genau zwei
	// Textfelder, und eine eigene Tabelle dafür wäre mehr Maschinerie als
	// Nutzen. Für selbst angelegte Einträge bleibt die englische Fassung
	// optional - wer nur deutsch arbeitet, füllt sie nicht aus.
	NameEN        string `gorm:"column:name_en" json:"name_en"`
	DescriptionEN string `gorm:"column:description_en" json:"description_en"`
	// Params sind die Platzhalter des Bausteins, kommagetrennt (z.B.
	// "service,path"). In den Regeln stehen sie als {service}.
	Params string `json:"params"`
	// Builtin markiert die mitgelieferten Bausteine: nicht änderbar, aber
	// klonbar. Sie werden mit LCM aktualisiert.
	Builtin bool `gorm:"default:false" json:"builtin"`

	Variants []ProfileBlockVariant `gorm:"foreignKey:BlockID;constraint:OnDelete:CASCADE" json:"variants,omitempty"`
}

// ProfileBlockVariant sind die Regeln eines Bausteins für eine
// Distributionsfamilie.
type ProfileBlockVariant struct {
	ID      uint `gorm:"primarykey" json:"id"`
	BlockID uint `gorm:"not null;index" json:"block_id"`

	// Family ist die Paketverwaltungs-Familie (apt, dnf, zypper, pacman, apk)
	// oder BlockFamilyAll.
	Family string `gorm:"not null" json:"family"`
	// SudoCommands: ein Kommando je Zeile, mit Platzhaltern.
	SudoCommands string `json:"sudo_commands"`
	// RunAs ist der Zielbenutzer aller Kommandos dieser Variante - leer
	// bedeutet root.
	//
	// Er gehört an die VARIANTE, weil er sich je Distribution unterscheidet:
	// Der Web-Benutzer heißt auf Debian www-data, auf RHEL apache, auf SUSE
	// wwwrun. Und er ist nicht Kosmetik: Nextclouds `occ` als root
	// auszuführen verstellt die Dateirechte der ganzen Installation - die
	// Anwendung warnt selbst davor. Ein Baustein, der nur root kennt, wäre
	// für solche Anwendungen unbrauchbar.
	RunAs string `json:"run_as"`
	// EditPaths: eine Datei je Zeile (sudoedit).
	EditPaths string `json:"edit_paths"`
	// PathRules: ein Verzeichnisrecht je Zeile, „modus pfad" (z.B.
	// „readwrite /etc/nginx"). Erst damit lässt sich ein Baustein bauen, der
	// nicht nur den Dienst bedient, sondern die Anwendung VERWALTET: Der
	// Unterschied zwischen „nginx neu starten dürfen" und „nginx betreuen"
	// ist der Zugriff auf sein Konfigurations- und Datenverzeichnis.
	//
	// Umgesetzt werden sie über POSIX-ACLs auf dem Zielsystem - fehlt dort
	// das Paket „acl", bleiben sie wirkungslos und der Abgleich meldet das.
	PathRules string `json:"path_rules"`
}

// ParseBlockPathRule zerlegt eine Zeile „modus pfad". ok=false bei leerer
// Zeile oder fehlendem Pfad; der Modus wird NICHT hier geprüft, das macht
// ValidatePathRule mit derselben Liste wie bei den Profilen.
func ParseBlockPathRule(line string) (mode, path string, ok bool) {
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) != 2 {
		return "", "", false
	}
	return fields[0], fields[1], true
}

// ProfileBlockUse hängt einen Baustein an ein Profil und füllt dessen
// Parameter.
type ProfileBlockUse struct {
	ID        uint `gorm:"primarykey" json:"id"`
	ProfileID uint `gorm:"not null;index" json:"profile_id"`
	BlockID   uint `gorm:"not null;index" json:"block_id"`
	// Values sind die Parameterwerte, je Zeile „name=wert".
	Values string `json:"values"`

	Block *ProfileBlock `gorm:"foreignKey:BlockID" json:"block,omitempty"`
}

var (
	ErrBlockSlugInvalid = fmt.Errorf("ungültiger slug - erlaubt sind Kleinbuchstaben, Ziffern und Bindestrich, höchstens %d Zeichen", MaxBlockSlugLen)
	ErrBlockNameEmpty   = errors.New("der baustein braucht einen namen")
	ErrBlockNoVariants  = errors.New("der baustein braucht mindestens eine variante")
	ErrBlockNoRules     = errors.New("jede variante braucht mindestens ein kommando, eine datei oder ein verzeichnisrecht")
	ErrBlockPathLine    = errors.New(`ungültige zeile für ein verzeichnisrecht - erwartet wird "modus pfad", z. B. "read /var/log/nginx"`)
	ErrBlockFamily      = errors.New("ungültige distributions-familie")
	ErrBlockRunAs       = errors.New("ungültiger zielbenutzer - erwartet wird ein linux-benutzername (leer = root)")
	ErrBlockParamValue  = errors.New("ungültiger parameterwert - keine leerzeichen und keine sonderzeichen")
	ErrBlockParamName   = errors.New("ungültiger parametername - erlaubt sind kleinbuchstaben, ziffern und unterstrich")
)

// ValidBlockFamily prüft die Familien-Kennung. Die Liste entspricht den
// Paketverwaltungen, die LCM bedient.
func ValidBlockFamily(family string) bool {
	switch family {
	case BlockFamilyAll, "apt", "dnf", "zypper", "pacman", "apk":
		return true
	}
	return false
}

// ValidBlockSlug prüft den technischen Namen eines Bausteins.
func ValidBlockSlug(slug string) bool {
	if len(slug) == 0 || len(slug) > MaxBlockSlugLen {
		return false
	}
	for i, c := range slug {
		switch {
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9', c == '-':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return !strings.HasSuffix(slug, "-")
}

// ValidBlockParamName prüft einen Platzhalternamen.
func ValidBlockParamName(name string) bool {
	if name == "" {
		return false
	}
	for _, c := range name {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '_':
		default:
			return false
		}
	}
	return true
}

// ValidBlockParamValue prüft einen eingesetzten Wert. Er landet in einer
// sudoers-Zeile - Leerzeichen würden ein zusätzliches Argument erzeugen, und
// Sonderzeichen sind dort ohnehin verboten.
func ValidBlockParamValue(value string) bool {
	if value == "" || strings.ContainsAny(value, " \t") {
		return false
	}
	return !hasShellMeta(value) && !hasWildcard(value)
}

// BlockParamNames zerlegt die Parameterliste eines Bausteins.
func BlockParamNames(params string) []string {
	var out []string
	for _, name := range strings.Split(params, ",") {
		if name = strings.TrimSpace(name); name != "" {
			out = append(out, name)
		}
	}
	return out
}

// ParseBlockValues liest die Parameterwerte einer Verwendung („name=wert" je
// Zeile).
func ParseBlockValues(values string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(values, "\n") {
		name, value, found := strings.Cut(strings.TrimSpace(line), "=")
		if !found {
			continue
		}
		out[strings.TrimSpace(name)] = strings.TrimSpace(value)
	}
	return out
}

// BlockLines zerlegt einen Regelblock in einzelne Zeilen (leere und
// Kommentarzeilen fallen weg) - dasselbe Format wie bei den Custom-Aktionen.
func BlockLines(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out
}

// SubstituteBlockParams ersetzt {name}-Platzhalter durch die Werte. Ein nicht
// gefüllter Platzhalter bleibt stehen - die anschließende Prüfung der
// Kommandozeile weist ihn dann ab, statt eine halbfertige Regel auszurollen.
func SubstituteBlockParams(text string, values map[string]string) string {
	for name, value := range values {
		text = strings.ReplaceAll(text, "{"+name+"}", value)
	}
	return text
}

// VariantForFamily wählt die Variante eines Bausteins für eine
// Distributionsfamilie: die passende, sonst die für alle. Fehlt beides,
// liefert sie nil - der Baustein gilt auf diesem Server dann NICHT, und das
// muss gemeldet werden. Eine fehlende Regel heißt fehlende Rechte; still
// übersprungen sucht jemand stundenlang, warum es nur auf den Debian-Servern
// geht.
func (b *ProfileBlock) VariantForFamily(family string) *ProfileBlockVariant {
	var fallback *ProfileBlockVariant
	for i := range b.Variants {
		switch b.Variants[i].Family {
		case family:
			return &b.Variants[i]
		case BlockFamilyAll:
			fallback = &b.Variants[i]
		}
	}
	return fallback
}

// ValidateBlockVariant prüft eine Variante vollständig - mit PROBEWEISE
// eingesetzten Parametern. Anders ginge es nicht: Eine Vorlage mit
// Platzhaltern ist keine gültige Kommandozeile, und ohne Prüfung fiele ein
// „/usr/bin/systemctl“ ohne Unteraktion erst auf, wenn es auf den Servern
// steht.
//
// Sie liegt hier und nicht im Dienst, weil zwei Aufrufer sie brauchen: der
// Dienst beim Speichern und der Test, der den mitgelieferten Katalog abnimmt.
// Zwei Umsetzungen derselben Prüfung wären genau die Art Abweichung, die man
// erst im Betrieb bemerkt.
func ValidateBlockVariant(v ProfileBlockVariant, params string) error {
	if !ValidBlockFamily(v.Family) {
		return ErrBlockFamily
	}
	if v.RunAs != "" && !ValidLinuxUsername(v.RunAs) {
		return ErrBlockRunAs
	}
	// Zwei Probewerte, weil die Prüfung zwei Formen verlangt: In einer
	// Kommandozeile ist „probe" ein gewöhnliches Argument, in einer Pfadregel
	// muss dasselbe Feld absolut sein. Mit nur einem Wert wäre entweder
	// „/usr/bin/getfacl -R {path}" oder „read {path}" unmöglich - und genau
	// diese Kombination braucht ein Baustein, dem man das Verzeichnis erst
	// beim Einhängen nennt.
	probe, pathProbe := map[string]string{}, map[string]string{}
	for _, name := range BlockParamNames(params) {
		probe[name], pathProbe[name] = "probe", "/probe"
	}
	commands, paths, dirs := BlockLines(v.SudoCommands), BlockLines(v.EditPaths), BlockLines(v.PathRules)
	if len(commands) == 0 && len(paths) == 0 && len(dirs) == 0 {
		return ErrBlockNoRules
	}
	for _, cmd := range commands {
		rendered := NormalizeSudoCommand(SubstituteBlockParams(cmd, probe))
		if err := ValidateSudoCommand(rendered, false); err != nil {
			return fmt.Errorf("variante %s, %q: %w", v.Family, cmd, err)
		}
	}
	for _, p := range paths {
		if err := ValidateEditPath(SubstituteBlockParams(p, pathProbe)); err != nil {
			return fmt.Errorf("variante %s, %q: %w", v.Family, p, err)
		}
	}
	for _, line := range dirs {
		mode, dir, ok := ParseBlockPathRule(SubstituteBlockParams(line, pathProbe))
		if !ok {
			return fmt.Errorf("variante %s, %q: %w", v.Family, line, ErrBlockPathLine)
		}
		if err := ValidatePathRule(dir, mode); err != nil {
			return fmt.Errorf("variante %s, %q: %w", v.Family, line, err)
		}
	}
	return nil
}

// LocalizedName liefert den Namen in der gewünschten Sprache - mit Rückfall
// auf die deutsche Fassung, wenn keine englische hinterlegt ist. Der Rückfall
// ist wichtiger als die Vollständigkeit: Ein leeres Feld in der Oberfläche
// wäre schlechter als ein deutscher Name.
func (b ProfileBlock) LocalizedName(lang string) string {
	return pickLanguage(lang, b.Name, b.NameEN)
}

// LocalizedDescription liefert die Beschreibung in der gewünschten Sprache.
func (b ProfileBlock) LocalizedDescription(lang string) string {
	return pickLanguage(lang, b.Description, b.DescriptionEN)
}

// pickLanguage wählt zwischen deutscher und englischer Fassung.
func pickLanguage(lang, german, english string) string {
	if lang == "en" && strings.TrimSpace(english) != "" {
		return english
	}
	return german
}
