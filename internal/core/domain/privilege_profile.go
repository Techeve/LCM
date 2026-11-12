package domain

import (
	"errors"
	"fmt"
	"path"
	"slices"
	"sort"
	"strings"
	"time"
)

// Berechtigungsprofile: benannte Rechtebündel, die ein von LCM verteilter
// Linux-Benutzer auf den Zielsystemen bekommt.
//
// Bisher kannte ein LinuxUser genau zwei Zustände - sudo oder kein sudo. Wer
// auch nur einen Dienst neu starten können musste, bekam volles NOPASSWD:ALL.
// Ein Profil beschreibt stattdessen genau, WAS jemand darf: einzelne
// Kommandos als root, einzelne Dateien zum Bearbeiten, einzelne Verzeichnisse
// mit Lese- oder Schreibrecht.
//
// Diese Datei enthält das Modell und die Eingabeprüfungen; angewendet werden
// die Profile in services/profile_apply.go. Die Verzeichnisrechte (ACLs)
// folgen in einer eigenen Etappe - bis dahin werden sie gespeichert, aber
// nicht auf die Server gebracht.

// Slugs der mitgelieferten Profile. Sie bilden den heutigen Zustand ab, damit
// bestehende Benutzer sich eindeutig einem Profil zuordnen lassen.
const (
	// ProfileSlugFullAdmin gewährt uneingeschränkte Root-Rechte - das
	// entspricht dem bisherigen Häkchen „sudo".
	ProfileSlugFullAdmin = "full-admin"
	// ProfileSlugStandard gewährt keine Root-Rechte - das bisherige
	// Verhalten ohne „sudo".
	ProfileSlugStandard = "standard"
)

// Kontotypen eines Profils.
const (
	// AccountTypeShell ist der Normalfall: Anmeldung mit Shell.
	AccountTypeShell = "shell"
	// AccountTypeSFTP gibt Dateizugriff OHNE Shell. Damit ist die Frage
	// „welche Programme darf jemand starten" endgültig beantwortet: keine.
	// Sichtbar bleibt, was die Verzeichnisrechte hergeben.
	AccountTypeSFTP = "sftp"
)

// ValidAccountType prüft den Kontotyp.
func ValidAccountType(t string) bool {
	return t == AccountTypeShell || t == AccountTypeSFTP
}

// MaxProfileSlugLen begrenzt den Slug: Aus ihm entsteht auf dem Zielsystem
// der Gruppenname `lcm-prof-<slug>`, und Linux-Gruppennamen sind auf 32
// Zeichen begrenzt.
const MaxProfileSlugLen = 20

// PrivilegeProfile ist ein benanntes Rechtebündel für Linux-Benutzer.
type PrivilegeProfile struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	Name string `gorm:"uniqueIndex;not null" json:"name"`
	// Slug ist der technische Name. Aus ihm entstehen später Gruppe und
	// sudoers-Datei auf dem Zielsystem; er ist deshalb unveränderlich.
	Slug        string `gorm:"uniqueIndex;not null" json:"slug"`
	Description string `json:"description"`

	// Builtin markiert die mitgelieferten Profile: nicht löschbar, Name und
	// Regeln nicht änderbar. Sie bilden den Zustand ab, den es vor den
	// Profilen schon gab.
	Builtin bool `gorm:"default:false" json:"builtin"`
	// AccountType entscheidet, ob sich Mitglieder mit einer Shell anmelden
	// oder nur Dateien übertragen dürfen.
	AccountType string `gorm:"not null;default:shell" json:"account_type"`
	// GrantsFullRoot steht ausschließlich am eingebauten Voll-Administrator.
	// Ein selbst angelegtes Profil kann das nicht setzen - sonst wäre die
	// ganze Feinsteuerung mit einem Häkchen ausgehebelt.
	GrantsFullRoot bool `gorm:"default:false" json:"grants_full_root"`

	SudoRules []ProfileSudoRule `gorm:"foreignKey:ProfileID;constraint:OnDelete:CASCADE" json:"sudo_rules,omitempty"`
	EditRules []ProfileEditRule `gorm:"foreignKey:ProfileID;constraint:OnDelete:CASCADE" json:"edit_rules,omitempty"`
	PathRules []ProfilePathRule `gorm:"foreignKey:ProfileID;constraint:OnDelete:CASCADE" json:"path_rules,omitempty"`
	// BlockUses sind die angehängten Regelbausteine. Sie werden REFERENZIERT,
	// nicht kopiert: Wird einem Baustein später ein fehlendes `--no-pager`
	// nachgetragen, bekommen alle Profile die Korrektur.
	BlockUses []ProfileBlockUse `gorm:"foreignKey:ProfileID;constraint:OnDelete:CASCADE" json:"block_uses,omitempty"`
}

// ProfileSudoRule ist ein Kommando, das Mitglieder des Profils als root
// ausführen dürfen - als VOLLSTÄNDIGE Kommandozeile inklusive der festen
// Argumente. sudo vergleicht die ganze Zeile: `/usr/bin/systemctl restart
// nginx` erlaubt genau das und nichts sonst, `/usr/bin/systemctl` dagegen
// jede Unteraktion auf jeder Unit - und damit faktisch alles.
type ProfileSudoRule struct {
	ID        uint `gorm:"primarykey" json:"id"`
	ProfileID uint `gorm:"not null;index" json:"profile_id"`

	Command string `gorm:"not null" json:"command"`
	// RunAs ist der Zielbenutzer (Vorgabe root).
	RunAs string `gorm:"default:root" json:"run_as"`
	// RequirePassword lässt sudo das Passwort des Benutzers abfragen. Nur
	// sinnvoll, wenn der Account eines gesetzt hat.
	RequirePassword bool `json:"require_password"`
	// AllowRootEquivalent ist die ausdrückliche Bestätigung, dass dieses
	// Kommando faktisch Root-Rechte gewährt (Shell-Ausbruch möglich). Ohne
	// sie werden solche Kommandos abgewiesen.
	AllowRootEquivalent bool `json:"allow_root_equivalent"`
}

// ProfileEditRule ist eine Datei, die per `sudoedit` bearbeitet werden darf.
//
// Warum nicht als sudo-Kommando: `sudo nano /etc/nginx/nginx.conf` startet
// einen Editor ALS ROOT - aus dem heraus lässt sich mit `!sh` jedes Kommando
// als root ausführen. `sudoedit` kopiert die Datei stattdessen in eine
// temporäre, startet den Editor als der Benutzer und schreibt sie danach als
// root zurück. Es läuft nie ein Editor mit Root-Rechten.
type ProfileEditRule struct {
	ID        uint   `gorm:"primarykey" json:"id"`
	ProfileID uint   `gorm:"not null;index" json:"profile_id"`
	Path      string `gorm:"not null" json:"path"`
}

// Modi einer Pfadregel.
const (
	PathModeRead      = "read"      // lesen (und in Verzeichnisse wechseln)
	PathModeReadWrite = "readwrite" // lesen und schreiben
	PathModeDeny      = "deny"      // ausdrücklich verweigern
)

// ProfilePathRule ist ein Verzeichnisrecht, das später über eine POSIX-ACL
// gesetzt wird.
type ProfilePathRule struct {
	ID        uint   `gorm:"primarykey" json:"id"`
	ProfileID uint   `gorm:"not null;index" json:"profile_id"`
	Path      string `gorm:"not null" json:"path"`
	Mode      string `gorm:"not null;default:read" json:"mode"`
}

// ---- Eingabeprüfungen --------------------------------------------------------

var (
	ErrProfileSlugInvalid  = fmt.Errorf("ungültiger slug - erlaubt sind Kleinbuchstaben, Ziffern und Bindestrich, höchstens %d Zeichen (daraus entsteht der Gruppenname auf dem Zielsystem)", MaxProfileSlugLen)
	ErrProfileNameEmpty    = errors.New("das profil braucht einen namen")
	ErrSudoCommandEmpty    = errors.New("die regel braucht ein kommando")
	ErrSudoCommandRelative = errors.New("das kommando muss mit einem absoluten pfad beginnen - sonst entscheidet der Suchpfad des Benutzers, welches Programm als root läuft")
	ErrSudoCommandWildcard = errors.New("platzhalter sind nicht erlaubt: sudo vergleicht die ganze Kommandozeile, und ein Stern öffnet sie für alles, was darauf passt (z.B. erlaubt „apt-get install *\" die Installation eines beliebigen Pakets - und Paketskripte laufen als root)")
	ErrSudoCommandMeta     = errors.New("im kommando sind nur buchstaben ohne umlaute, ziffern, leerzeichen und ._+@%:=~^/- erlaubt")
	ErrSudoCommandNoArgs   = errors.New("dieses kommando braucht feste argumente - ohne sie erlaubt es jede Unteraktion auf jedem Ziel und ist damit gleichbedeutend mit vollen Root-Rechten")
	ErrPathRelative        = errors.New("der pfad muss absolut sein")
	ErrPathMeta            = errors.New("im pfad sind nur buchstaben ohne umlaute, ziffern und ._+@%:=~^/- erlaubt")
	ErrPathProtected       = errors.New("dieser pfad ist gesperrt: an ihm hängt der Betrieb des Systems oder der Zugang von LCM selbst")
	ErrPathModeInvalid     = errors.New("ungültiger modus - erlaubt: read, readwrite, deny")
	ErrPathParentRef       = errors.New("der pfad darf kein „..\" enthalten")
	ErrPathRootEquivalent  = errors.New("dieser pfad bedeutet root-rechte: was dort liegt, führt das System als root aus (cron, systemd, PAM, Linker, Login-Skripte, sudo), oder es ist der Zugang von LCM selbst")
)

// ErrRootEquivalent meldet ein Kommando, das faktisch Root-Rechte gewährt.
// Es ist nicht verboten, aber es muss ausdrücklich bestätigt werden.
type ErrRootEquivalent struct {
	Binary string
	Reason string
}

func (e *ErrRootEquivalent) Error() string {
	return fmt.Sprintf("%q gewährt faktisch volle root-rechte (%s) - nur mit ausdrücklicher Bestätigung aufnehmen", e.Binary, e.Reason)
}

// rootEquivalentBinaries sind Programme, aus denen sich UNABHÄNGIG von den
// Argumenten ein beliebiges Kommando als root starten lässt. Ein Eintrag hier
// ist kein Verbot, sondern eine Bestätigungspflicht.
//
// Bewusst NICHT enthalten sind Paketverwaltungen, Docker & Co.: Mit festen
// Argumenten sind sie harmlos, gefährlich werden sie erst über Platzhalter -
// und die sind ohnehin verboten.
var rootEquivalentBinaries = map[string]string{
	"sh": "Shell", "bash": "Shell", "dash": "Shell", "zsh": "Shell",
	"ksh": "Shell", "csh": "Shell", "tcsh": "Shell", "fish": "Shell",
	"mksh": "Shell", "ash": "Shell", "rbash": "Shell", "busybox": "enthält eine Shell",

	"perl": "Interpreter", "python": "Interpreter", "python2": "Interpreter",
	"python3": "Interpreter", "ruby": "Interpreter", "php": "Interpreter",
	"node": "Interpreter", "nodejs": "Interpreter", "lua": "Interpreter", "tclsh": "Interpreter",
	"expect": "Interpreter", "awk": "Interpreter", "gawk": "Interpreter",
	"mawk": "Interpreter", "nawk": "Interpreter",

	"vi": "Editor mit Shell-Ausbruch", "vim": "Editor mit Shell-Ausbruch",
	"nvim": "Editor mit Shell-Ausbruch", "view": "Editor mit Shell-Ausbruch",
	"rvim": "Editor mit Shell-Ausbruch", "rview": "Editor mit Shell-Ausbruch",
	"vimdiff": "Editor mit Shell-Ausbruch", "ex": "Editor mit Shell-Ausbruch",
	"editor": "ruft einen Editor auf", "sensible-editor": "ruft einen Editor auf",
	"pager": "ruft einen Pager auf", "sensible-pager": "ruft einen Pager auf",
	"nano": "Editor", "emacs": "Editor mit Shell-Ausbruch",
	"ed": "Editor", "pico": "Editor",
	"less": "Pager mit Shell-Ausbruch", "more": "Pager mit Shell-Ausbruch",
	"man": "ruft einen Pager auf",

	"su": "Benutzerwechsel", "sudo": "Rechteerhöhung", "doas": "Rechteerhöhung",
	"sudoedit": "Rechteerhöhung", "pkexec": "Rechteerhöhung", "run0": "Rechteerhöhung",
	"setpriv": "startet Programme mit anderen Rechten", "capsh": "startet eine Shell",
	"env": "startet beliebige Programme", "nsenter": "betritt fremde Namespaces",
	"unshare": "startet Programme in neuen Namespaces", "chroot": "startet Programme in fremder Wurzel",
	"script": "startet eine Shell", "socat": "startet Programme",
	"nc": "startet Programme", "ncat": "startet Programme", "netcat": "startet Programme",
	"xargs": "startet beliebige Programme",

	"dd": "schreibt beliebige Dateien und Geräte", "tee": "schreibt beliebige Dateien",
	"cp": "überschreibt beliebige Dateien", "mv": "überschreibt beliebige Dateien",
	"ln": "verbiegt beliebige Pfade", "install": "schreibt beliebige Dateien",
	"truncate": "schreibt beliebige Dateien",
	"chmod":    "ändert Rechte beliebiger Dateien", "chown": "ändert Eigentümer beliebiger Dateien",
	"chgrp": "ändert Gruppen beliebiger Dateien",

	"mount": "bindet Dateisysteme ein", "umount": "hängt Dateisysteme aus",
	"insmod": "lädt Kernelmodule", "modprobe": "lädt Kernelmodule",
	"gdb": "führt beliebigen Code aus", "strace": "führt beliebige Programme aus",
	"ltrace":  "führt beliebige Programme aus",
	"crontab": "hinterlegt beliebige Kommandos", "at": "hinterlegt beliebige Kommandos",
}

// escapeArguments sind Argumente, mit denen auch ein sonst harmloses Programm
// ein beliebiges Kommando startet. Die Liste ist NICHT vollständig - sie
// fängt die verbreiteten Fälle ab, die Verantwortung für die Zusammenstellung
// eines Profils bleibt beim Betreiber.
var escapeArguments = map[string]string{
	"-exec": "führt ein beliebiges Kommando aus", "-execdir": "führt ein beliebiges Kommando aus",
	"-ok": "führt ein beliebiges Kommando aus", "--to-command": "übergibt Daten an ein Kommando",
	"--privileged": "hebt die Container-Isolierung auf", "-c": "führt übergebenen Code aus",
}

// rootEquivalentBinary sucht ein Programm in rootEquivalentBinaries, auch
// unter seinen Nebennamen: mit Versionsnummer (python3.11, perl5.36.0,
// lua5.4) und mit Variante hinter einem Punkt (vim.basic, vim.tiny). Der
// Helper rechnet in profile_rule_ok genauso.
func rootEquivalentBinary(name string) (string, string, bool) {
	unversioned := strings.TrimRight(name, "0123456789.")
	for _, candidate := range []string{name, unversioned, beforeDot(name), beforeDot(unversioned)} {
		if reason, blocked := rootEquivalentBinaries[candidate]; blocked {
			return candidate, reason, true
		}
	}
	return "", "", false
}

// beforeDot liefert den Namen bis zum ersten Punkt (vim.basic: vim).
func beforeDot(name string) string {
	stem, _, _ := strings.Cut(name, ".")
	return stem
}

// RootEquivalentBinaries liefert die Programme aus rootEquivalentBinaries
// sortiert. Der LCM-Helper wiederholt damit im eingeschränkten Modus dieselbe
// Prüfung auf dem Zielsystem - eine zweite, von Hand gepflegte Liste liefe
// auseinander.
func RootEquivalentBinaries() []string { return sortedKeys(rootEquivalentBinaries) }

// EscapeArguments liefert die Argumente aus escapeArguments sortiert (für den
// Helper wie RootEquivalentBinaries).
func EscapeArguments() []string { return sortedKeys(escapeArguments) }

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// pagerCommands brauchen `--no-pager`: Ohne das schicken sie ihre Ausgabe
// durch einen Pager, der dann ALS ROOT läuft - und in `less` genügt `!sh` für
// eine Root-Shell. Ein vermeintlich lesendes „status"-Kommando wäre damit ein
// vollwertiger Rechteaufstieg.
var pagerCommands = map[string]bool{"systemctl": true, "journalctl": true}

// needsArguments sind Programme, deren nackter Aufruf jede Unteraktion auf
// jedem Ziel erlaubt. Genau daran scheitern sudo-Regelwerke in der Praxis:
// Jemand trägt „/usr/bin/systemctl" ein, weil der Kollege „Dienste verwalten"
// soll - und hat volle Root-Rechte vergeben (`systemctl edit` etwa öffnet
// einen Editor als root).
var needsArguments = map[string]bool{
	"systemctl": true, "journalctl": true, "service": true,
	"apt": true, "apt-get": true, "dnf": true, "yum": true,
	"zypper": true, "pacman": true, "apk": true,
	"docker": true, "podman": true, "ufw": true, "firewall-cmd": true,
	"nft": true, "iptables": true, "ip": true,
}

// ValidProfileSlug prüft den technischen Namen eines Profils.
func ValidProfileSlug(slug string) bool {
	if len(slug) == 0 || len(slug) > MaxProfileSlugLen {
		return false
	}
	for i, c := range slug {
		switch {
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9', c == '-':
			if i == 0 {
				return false // nicht mit Ziffer oder Bindestrich beginnen
			}
		default:
			return false
		}
	}
	return !strings.HasSuffix(slug, "-")
}

// hasShellMeta meldet Zeichen, die in einer sudoers-Zeile oder auf dem Weg
// dorthin etwas anderes bedeuten, als sie aussehen. Das Komma ist dabei kein
// Schönheitsfehler: In sudoers TRENNT es Kommandos - ein Komma im Kommando
// schmuggelte ein zweites in dieselbe Regel.
func hasShellMeta(s string) bool {
	return strings.ContainsAny(s, ";|&$`\n\r\\<>()!{}'\"#,")
}

// helperSafe meldet, ob s nur aus den Zeichen besteht, die auch der Helper im
// eingeschränkten Modus annimmt (clean_abs_path, profile_rule_ok). Was hier
// durchginge und dort nicht, ließe auf solchen Servern jeden Abgleich
// scheitern; im Voll-Modus brächen Leerzeichen das Skript.
func helperSafe(s string) bool {
	for _, r := range s {
		ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._+@%:=~^/-", r)
		if !ok {
			return false
		}
	}
	return true
}

// hasWildcard meldet Platzhalter, die sudo als solche auswertet.
func hasWildcard(s string) bool { return strings.ContainsAny(s, "*?[]") }

// NormalizeSudoCommand räumt Leerzeichen auf und ergänzt `--no-pager`, wo es
// fehlt. Ergänzt statt abgelehnt, weil die Regel sonst am häufigsten Fehler
// wäre, den niemand erwartet - und weil das Ergebnis gespeichert und in der
// Oberfläche sichtbar ist.
func NormalizeSudoCommand(cmd string) string {
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return ""
	}
	if !pagerCommands[path.Base(fields[0])] {
		return strings.Join(fields, " ")
	}
	for _, f := range fields[1:] {
		if f == "--no-pager" {
			return strings.Join(fields, " ")
		}
	}
	out := append([]string{fields[0], "--no-pager"}, fields[1:]...)
	return strings.Join(out, " ")
}

// hasRealArguments meldet, ob nach den von LCM ergänzten Schaltern noch ein
// echtes Argument übrig bleibt - also eine Unteraktion oder ein Ziel.
func hasRealArguments(args []string) bool {
	for _, arg := range args {
		if arg != "--no-pager" {
			return true
		}
	}
	return false
}

// ValidateSudoCommand prüft eine Kommandozeile für die sudoers-Whitelist.
// allowRootEquivalent ist die ausdrückliche Bestätigung des Betreibers für
// Kommandos, die faktisch Root-Rechte gewähren.
func ValidateSudoCommand(cmd string, allowRootEquivalent bool) error {
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return ErrSudoCommandEmpty
	}
	if !strings.HasPrefix(fields[0], "/") {
		return ErrSudoCommandRelative
	}
	if hasWildcard(cmd) {
		return ErrSudoCommandWildcard
	}
	if hasShellMeta(cmd) {
		return ErrSudoCommandMeta
	}
	for _, field := range fields {
		if !helperSafe(field) {
			return ErrSudoCommandMeta
		}
	}
	binary := path.Base(fields[0])
	// `--no-pager` zählt hier NICHT als Argument: Es wird von LCM selbst
	// ergänzt (NormalizeSudoCommand) und machte die Prüfung sonst wirkungslos -
	// `/usr/bin/systemctl --no-pager` erlaubt weiterhin jede Unteraktion auf
	// jeder Unit und ist damit vollwertiger Root-Zugriff.
	if needsArguments[binary] && !hasRealArguments(fields[1:]) {
		return ErrSudoCommandNoArgs
	}
	if allowRootEquivalent {
		return nil
	}
	if listed, reason, blocked := rootEquivalentBinary(binary); blocked {
		return &ErrRootEquivalent{Binary: listed, Reason: reason}
	}
	for _, arg := range fields[1:] {
		if reason, blocked := escapeArguments[arg]; blocked {
			return &ErrRootEquivalent{Binary: arg, Reason: reason}
		}
	}
	return nil
}

// protectedPaths sind Pfade, auf die weder eine Pfadregel noch eine
// sudoedit-Regel zeigen darf: An ihnen hängt der Betrieb des Systems oder der
// Zugang von LCM selbst. Ein Schreibrecht auf /etc/sudoers.d etwa wäre ein
// Selbstbedienungsladen für Root-Rechte.
var protectedPaths = []string{
	"/", "/bin", "/boot", "/dev", "/lib", "/lib64", "/proc", "/sbin", "/sys",
	"/usr", "/etc/sudoers", "/etc/sudoers.d", "/etc/shadow", "/etc/passwd",
	"/etc/group", "/etc/ssh", "/root", "/var/lib/lcm",
}

// isProtectedPath meldet, ob der Pfad selbst geschützt ist oder unter einem
// geschützten Verzeichnis liegt.
func isProtectedPath(p string) bool {
	clean := path.Clean(p)
	for _, protected := range protectedPaths {
		if clean == protected {
			return true
		}
		if protected != "/" && strings.HasPrefix(clean, protected+"/") {
			return true
		}
	}
	// „/etc" selbst ist gesperrt, einzelne Konfigurationsverzeichnisse
	// darunter (z.B. /etc/nginx) sind ausdrücklich erlaubt - sie sind der
	// häufigste sinnvolle Fall.
	return clean == "/etc"
}

// validRulePath ist die gemeinsame Prüfung für Pfad- und sudoedit-Regeln.
func validRulePath(p string) error {
	if !strings.HasPrefix(p, "/") {
		return ErrPathRelative
	}
	if hasShellMeta(p) || hasWildcard(p) || !helperSafe(p) {
		return ErrPathMeta
	}
	if isProtectedPath(p) {
		return ErrPathProtected
	}
	return nil
}

// rootEquivalentPaths sind Pfade, deren Freigabe Root bedeutet: Was dort liegt,
// führt das System als root aus (cron, systemd, PAM, der dynamische Linker,
// Login-Skripte, sudo selbst, Paket-Hooks und -quellen, sysctl mit
// core_pattern, Netzwerk-Hooks, Docker), oder es ist das Home des
// Management-Benutzers, dessen authorized_keys der Zugang von LCM sind.
// Die Homes anderer Benutzer stehen nicht darin: Wer ein fremdes Home
// freigibt, entscheidet das bewusst für dieses Konto.
//
// Ein Stern am Ende heißt „alles, was so beginnt" (/etc/cron* trifft
// /etc/crontab und /etc/cron.d). Sonst gilt der Pfad samt allem darunter:
// /etc/systemd trifft /etc/systemd/system, aber nicht /etc/systemdx.
var rootEquivalentPaths = []string{
	"/etc/cron*", "/etc/anacrontab", "/var/spool/cron",
	"/etc/systemd", "/run/systemd",
	"/etc/pam.d", "/etc/ld.so.*",
	"/etc/profile*", "/etc/environment", "/etc/bash.bashrc",
	"/etc/sudo*", "/etc/gshadow",
	"/etc/init.d", "/etc/rc.local", "/etc/update-motd.d", "/etc/logrotate*",
	"/etc/modprobe.d", "/etc/modules-load.d", "/etc/udev", "/etc/apt",
	"/etc/sysctl*", "/etc/rc*", "/etc/network", "/etc/NetworkManager", "/etc/dhcp",
	"/etc/security", "/etc/xinetd*", "/etc/polkit-1", "/etc/dbus-1", "/etc/nsswitch.conf",
	"/etc/docker", "/var/lib/dpkg", "/var/lib/docker",
	"/home/" + DefaultServiceUser,
}

// BlockedRulePaths liefert die gesperrten Pfade für den LCM-Helper, der
// dieselbe Prüfung auf dem Zielsystem wiederholt: "=" vorne heißt nur genau
// dieser Pfad, "*" hinten alles, was so beginnt, sonst der Pfad samt allem
// darunter - dieselbe Bedeutung wie in isProtectedPath und rootEquivalentPaths.
func BlockedRulePaths() []string {
	out := []string{"=/", "=/etc"}
	for _, p := range protectedPaths {
		if p != "/" {
			out = append(out, p)
		}
	}
	return append(out, rootEquivalentPaths...)
}

// pathHits meldet, ob eine Regel auf p den gesperrten Eintrag blocked trifft:
// p liegt darin oder - mit recursive - darüber. Eine rekursive Freigabe von
// /var gäbe mit setfacl -R auch /var/spool/cron frei.
func pathHits(p, blocked string, recursive bool) bool {
	prefix, glob := strings.CutSuffix(blocked, "*")
	if glob && strings.HasPrefix(p, prefix) {
		return true
	}
	if !glob && (p == blocked || strings.HasPrefix(p, blocked+"/")) {
		return true
	}
	return recursive && strings.HasPrefix(prefix, p+"/")
}

// GrantReaches meldet, ob eine Pfadregel mit diesem Modus den Pfad target
// erfasst - der Weg, das Home eines abweichend benannten Service-Users je
// Server auszuschließen.
func GrantReaches(p, mode, target string) bool {
	return pathHits(path.Clean(p), target, mode != PathModeDeny)
}

// hasParentRef meldet ein „..": Ein Pfad soll so dastehen, wie er wirkt.
func hasParentRef(p string) bool {
	return slices.Contains(strings.Split(p, "/"), "..")
}

// validGrantPath prüft einen Pfad, auf dem eine Regel Rechte vergibt
// (Verzeichnisrecht, sudoedit). Zusätzlich zu validRulePath: kein „..", kein
// Pfad, der Root bedeutet, und mit recursive auch keiner darüber.
func validGrantPath(p string, recursive bool) error {
	if err := validRulePath(p); err != nil {
		return err
	}
	if hasParentRef(p) {
		return ErrPathParentRef
	}
	clean := path.Clean(p)
	for _, blocked := range rootEquivalentPaths {
		if pathHits(clean, blocked, recursive) {
			return ErrPathRootEquivalent
		}
	}
	for _, protected := range protectedPaths {
		if recursive && protected != "/" && pathHits(clean, protected, true) {
			return ErrPathProtected
		}
	}
	return nil
}

// ValidateEditPath prüft die Datei einer sudoedit-Regel.
func ValidateEditPath(p string) error { return validGrantPath(p, false) }

// ValidatePathRule prüft Pfad und Modus eines Verzeichnisrechts. Lesen und
// Schreiben wirken rekursiv (setfacl -R), eine Verweigerung gibt nichts frei.
func ValidatePathRule(p, mode string) error {
	switch mode {
	case PathModeRead, PathModeReadWrite, PathModeDeny:
	default:
		return ErrPathModeInvalid
	}
	return validGrantPath(p, mode != PathModeDeny)
}

// ---- Zuweisung ---------------------------------------------------------------

// ServerLinuxUser und ServerGroupLinuxUser sind die Zuordnungstabellen
// zwischen Linux-Benutzern und Servern bzw. Servergruppen. Sie existieren
// bereits als many2many-Verknüpfung; die Modelle hier hängen ihnen die Spalte
// `profile_id` an und machen sie beschreibbar.
//
// Bewusst OHNE GORM-Join-Table-Umbau (SetupJoinTable): Die bestehende
// Verknüpfung bleibt unangetastet, AutoMigrate ergänzt nur die Spalte, und der
// Profilwert wird gezielt gesetzt und gelesen. Ein Umbau der Assoziation hätte
// jede vorhandene Abfrage auf diesen Tabellen berührt, ohne dass es dafür
// einen fachlichen Grund gäbe.
//
// ProfileID ist optional: NULL bedeutet „Standardprofil des Benutzers".
type ServerLinuxUser struct {
	ServerID    uint  `gorm:"primaryKey" json:"server_id"`
	LinuxUserID uint  `gorm:"primaryKey" json:"linux_user_id"`
	ProfileID   *uint `gorm:"index" json:"profile_id"`
}

// TableName bindet das Modell an die bestehende Verknüpfungstabelle.
func (ServerLinuxUser) TableName() string { return "server_linux_users" }

type ServerGroupLinuxUser struct {
	ServerGroupID uint  `gorm:"primaryKey" json:"server_group_id"`
	LinuxUserID   uint  `gorm:"primaryKey" json:"linux_user_id"`
	ProfileID     *uint `gorm:"index" json:"profile_id"`
}

// TableName bindet das Modell an die bestehende Verknüpfungstabelle.
func (ServerGroupLinuxUser) TableName() string { return "server_group_linux_users" }

// ProfileGroupName ist der Name der Linux-Gruppe, über die ein Profil auf dem
// Zielsystem wirkt. Das Präfix trennt sie von den per-Benutzer-Grants
// (`lcm-<username>`) - ein Profil-Slug, der zufällig einem Benutzernamen
// gleicht, würde sonst mit dessen Datei kollidieren.
func ProfileGroupName(slug string) string { return "lcm-prof-" + slug }

// ProfileSudoersPath ist die von LCM verwaltete sudoers-Datei eines Profils.
func ProfileSudoersPath(slug string) string { return "/etc/sudoers.d/" + ProfileGroupName(slug) }

// AppliedProfilePath hält fest, auf welchen Pfaden LCM für ein Profil ACLs
// gesetzt hat.
//
// Warum eine Tabelle, wo doch die sudoers-Dateien am Server selbst aufgezählt
// werden: ACL-Einträge tragen weder Kommentar noch Namenskonvention. Sie
// lassen sich nicht daran erkennen, dass LCM sie gesetzt hat - ohne diese
// Aufzeichnung bliebe ein aus dem Profil entfernter Pfad für immer freigegeben.
//
// Slug hält fest, für welche Gruppe der Eintrag gesetzt wurde. Ohne ihn ließ
// sich ein Eintrag nach dem Löschen oder Abziehen des Profils nicht mehr
// zurücknehmen: setfacl kennt die Gruppe nur beim Namen, und eine später
// angelegte Gruppe mit derselben GID erbte die Rechte. Leer bei Einträgen aus
// der Zeit davor - dann hilft nur noch das Profil selbst, solange es besteht.
type AppliedProfilePath struct {
	ID        uint   `gorm:"primarykey" json:"id"`
	ServerID  uint   `gorm:"not null;index" json:"server_id"`
	ProfileID uint   `gorm:"not null;index" json:"profile_id"`
	Slug      string `gorm:"not null;default:''" json:"slug"`
	Path      string `gorm:"not null" json:"path"`
	Mode      string `gorm:"not null" json:"mode"`
}

// HardenedPath ist ein Verzeichnis, dessen Welt-Zugriff LCM entfernt hat.
//
// Der Vorzustand wird mitgeschrieben: Ohne ihn gäbe es keine Rücknahme - und
// eine Härtung, die man nicht zurücknehmen kann, traut sich niemand
// anzuwenden.
type HardenedPath struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	CreatedAt time.Time `json:"created_at"`

	ServerID uint   `gorm:"not null;index" json:"server_id"`
	Path     string `gorm:"not null" json:"path"`

	// Vorzustand (Rechte und Gruppe) vor dem Eingriff.
	PrevMode  string `json:"prev_mode"`
	PrevGroup string `json:"prev_group"`
	// Zustand danach.
	Mode  string `json:"mode"`
	Group string `json:"group"`
	// Unit ist die systemd-Unit, gegen die nach dem Eingriff geprüft wurde
	// (leer = keine Probe).
	Unit string `json:"unit"`
}

// ValidateHardenTarget prüft Pfad, Gruppe und Unit einer Härtung.
func ValidateHardenTarget(path, group, unit string) error {
	if err := validRulePath(path); err != nil {
		return err
	}
	if group != "" && !ValidLinuxUsername(group) {
		return fmt.Errorf("ungültiger gruppenname %q", group)
	}
	if unit != "" && (hasShellMeta(unit) || hasWildcard(unit) || strings.ContainsAny(unit, " \t/")) {
		return fmt.Errorf("ungültiger dienstname %q", unit)
	}
	return nil
}
