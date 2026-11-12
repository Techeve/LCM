package services

import (
	"errors"
	"fmt"
	"strings"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/storage/repositories"
)

var ErrUnknownRepo = errors.New("unbekannte paketquelle")

// ErrRepoPackageManagerMismatch: die gewählte Katalog-Quelle gilt für eine
// andere Paketverwaltung als die des Zielservers (z. B. eine apt-Quelle auf
// einem dnf-System). LCM richtet sie dann bewusst NICHT ein, statt ein
// unpassendes Repository auf dem Server zu hinterlassen.
var ErrRepoPackageManagerMismatch = errors.New("paketquelle passt nicht zur paketverwaltung des servers")

// KnownRepoCatalog liefert den Katalog der bekannten Paketquellen: aus der
// Datenbank (pflegbar unter Einstellungen → Repositories), ohne verdrahtete
// DB-Sicht den mitgelieferten Default-Katalog.
func (s *ServerService) KnownRepoCatalog() ([]domain.KnownRepo, error) {
	if s.knownRepos == nil {
		return domain.DefaultKnownRepos(), nil
	}
	return s.knownRepos.List()
}

func (s *ServerService) knownRepoByKey(key string) (domain.KnownRepo, bool) {
	if s.knownRepos != nil {
		repo, err := s.knownRepos.FindByKey(key)
		if err != nil {
			return domain.KnownRepo{}, false
		}
		return *repo, true
	}
	for _, r := range domain.DefaultKnownRepos() {
		if r.Key == key {
			return r, true
		}
	}
	return domain.KnownRepo{}, false
}

// ErrRepoNotSecurable: die angeforderte Quelle ist keine unverschlüsselte
// Quelle des Servers. Was nicht im Bestand steht, kommt nicht in die
// Kommandozeile.
var ErrRepoNotSecurable = errors.New("diese paketquelle ist keine unverschlüsselte quelle des servers")

// SecureRepositories stellt unverschlüsselte (http://) apt-Quellen des
// Servers auf https um: die genannten uris, ohne Auswahl alle. Die Auswahl
// kommt aus der Prüfung vorher (CheckRepositoriesHTTPS), denn nicht jeder
// Spiegel spricht HTTPS. Für die TLS-Verbindungen wird ca-certificates
// sichergestellt. Sicherheitsnetz: Vor der Änderung wird jede Datei gesichert
// - schlägt `apt-get update` danach fehl, wird alles zurückgerollt und die
// Aktion als Fehler gemeldet.
func (s *ServerService) SecureRepositories(scope repositories.AccessScope, id uint, uris []string, actor string) (string, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return "", err
	}
	script, targets, err := s.planSecureRepos(server, uris)
	if err != nil {
		return "", err
	}
	conn, err := s.connectRec(server, "secure-repos", actor)
	if err != nil {
		return "", fmt.Errorf("verbindung: %w", err)
	}
	defer conn.Close()

	output, code, runErr := conn.Run(privRun(server, script))
	if runErr != nil {
		return output, runErr
	}
	if code != 0 {
		return output, repoSwitchError("https-umstellung", output, code)
	}
	// Die http→https-Umstellung ist apt-spezifisch (sources.list).
	s.rescanRepositories(conn, server, pkgApt)
	details := server.Name
	if len(targets) > 0 {
		details += ": " + strings.Join(targets, ", ")
	}
	s.audit.Log(actor, "server.secure-repos", "server", id, details)
	return output, nil
}

// repoSwitchError nennt die Quellen, an denen die Umstellung gescheitert ist.
// Die Oberfläche zeigt im Fehlerfall nur diesen Text, nicht die Ausgabe.
func repoSwitchError(action, output string, code int) error {
	failed := domain.ClassifyAptUpdate(output).FailedSources
	if len(failed) == 0 {
		return fmt.Errorf("%s fehlgeschlagen (exit %d)", action, code)
	}
	return fmt.Errorf("%s zurückgerollt, nicht erreichbar: %s", action, strings.Join(failed, ", "))
}

// planSecureRepos prüft Server und Auswahl und baut das Skript der
// https-Umstellung - für den Lauf und die Befehlsvorschau gleich. Der zweite
// Rückgabewert sind die gewählten URLs; leer heißt „alle".
func (s *ServerService) planSecureRepos(server *domain.Server, uris []string) (string, []string, error) {
	if err := secureReposSupported(server); err != nil {
		return "", nil, err
	}
	targets, err := s.secureTargets(server, uris)
	if err != nil {
		return "", nil, err
	}
	// Eingeschränkter Modus: dieselbe Logik steckt validiert im LCM-Helper.
	// Ein älterer Helper überginge die Auswahl und stellte alle Quellen um -
	// mit Auswahl muss er deshalb aktuell sein.
	if server.RestrictedSudo && len(targets) == 0 {
		return helperCmd("repos-https"), nil, nil
	}
	if server.RestrictedSudo {
		return helperCurrentStep(true) + "\n" + helperCmd("repos-https", targets...), targets, nil
	}
	if len(targets) == 0 {
		return secureHTTPSScript(), nil, nil
	}
	return secureHTTPSURLsScript(targets), targets, nil
}

// secureReposSupported sind die Guards der https-Umstellung und ihrer Prüfung.
// Gleich wie bei AddKnownRepository: RouterOS hat keine apt-Quellen, Proxmox
// pflegt seine Paketquellen selbst, und die Umstellung ist apt-spezifisch -
// auf anderen Paketverwaltungen wäre „nichts zu tun" irreführend.
func secureReposSupported(server *domain.Server) error {
	if server.IsRouterOS() {
		return ErrRouterOSUnsupported
	}
	if server.IsProxmox() {
		return ErrProxmoxRestricted
	}
	if pkgFamily(server.PackageManager) != pkgApt {
		return fmt.Errorf("%w: die https-Umstellung betrifft apt-Quellen, dieser Server nutzt %s",
			ErrRepoPackageManagerMismatch, PackageManagerLabel(server.PackageManager))
	}
	return nil
}

// secureTargets prüft die Auswahl gegen die unverschlüsselten Quellen im
// Bestand des Servers. Ohne Auswahl bleibt es bei der Umstellung aller
// Quellen (ältere Oberflächen schicken keine).
func (s *ServerService) secureTargets(server *domain.Server, uris []string) ([]string, error) {
	var wanted []string
	for _, uri := range uris {
		if uri = strings.TrimSpace(uri); uri != "" {
			wanted = append(wanted, uri)
		}
	}
	if len(wanted) == 0 {
		return nil, nil
	}
	repos, err := s.servers.FindRepositories(server.ID)
	if err != nil {
		return nil, err
	}
	return chooseRepoURIs(domain.InsecureRepoURIs(repos), wanted)
}

// chooseRepoURIs lässt nur zu, was in allowed steht - jede URL einmal.
func chooseRepoURIs(allowed, wanted []string) ([]string, error) {
	known := map[string]bool{}
	for _, uri := range allowed {
		known[uri] = true
	}
	seen := map[string]bool{}
	var chosen []string
	for _, uri := range wanted {
		if !known[uri] {
			return nil, fmt.Errorf("%w: %s", ErrRepoNotSecurable, uri)
		}
		if !seen[uri] {
			seen[uri] = true
			chosen = append(chosen, uri)
		}
	}
	return chosen, nil
}

// AddKnownRepository richtet eine bekannte Paketquelle aus dem Katalog auf dem
// Zielserver ein - in der Syntax und mit den Werkzeugen der jeweiligen
// Paketverwaltung (apt/dnf/zypper/pacman/apk, siehe repoAddScript). Die Quelle
// MUSS zur Paketverwaltung des Servers passen; andernfalls wird sie bewusst
// nicht eingerichtet (ErrRepoPackageManagerMismatch).
func (s *ServerService) AddKnownRepository(scope repositories.AccessScope, id uint, repoKey, actor string) (string, error) {
	repo, ok := s.knownRepoByKey(repoKey)
	if !ok {
		return "", ErrUnknownRepo
	}
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return "", err
	}
	script, err := planAddRepo(server, repo)
	if err != nil {
		return "", err
	}
	conn, err := s.connectRec(server, "add-repo:"+repo.Key, actor)
	if err != nil {
		return "", fmt.Errorf("verbindung: %w", err)
	}
	defer conn.Close()

	output, code, runErr := conn.Run(privRun(server, script))
	if runErr != nil {
		return output, runErr
	}
	if code != 0 {
		return output, fmt.Errorf("repository %s einrichten fehlgeschlagen (exit %d)", repo.Name, code)
	}
	s.rescanRepositories(conn, server, server.PackageManager)
	s.audit.Log(actor, "server.add-repo", "server", id, server.Name+": "+repo.Name)
	return output, nil
}

// planAddRepo prüft Server und Quelle und baut das Einrichtungsskript - für
// den Lauf und die Befehlsvorschau gleich.
func planAddRepo(server *domain.Server, repo domain.KnownRepo) (string, error) {
	if server.IsRouterOS() {
		return "", ErrRouterOSUnsupported
	}
	// Proxmox pflegt seine Paketquellen selbst (enterprise/no-subscription)
	// - fremde Repositories sind dort bewusst gesperrt (stärkere Regel als
	// die Paketverwaltungs-Prüfung).
	if server.IsProxmox() {
		return "", ErrProxmoxRestricted
	}
	// Die Quelle muss für die Paketverwaltung des Servers gedacht sein.
	if repo.RepoPackageManager() != server.PackageManager {
		return "", fmt.Errorf("%w: %q ist für %s, dieser Server nutzt %s",
			ErrRepoPackageManagerMismatch, repo.Name,
			PackageManagerLabel(repo.RepoPackageManager()), PackageManagerLabel(server.PackageManager))
	}
	// Eingeschränkter Modus gibt es nur auf apt-Systemen (sudoers + LCM-Helper):
	// Key-Download, Quellen-Zeile und apt-Update erledigt dort der validierte
	// Helper.
	if server.RestrictedSudo && server.PackageManager == pkgApt {
		return helperCmd("repo-add", repo.Key, helperB64(repo.KeyURL), helperB64(repo.Line)), nil
	}
	return repoAddScript(server.PackageManager, repo), nil
}

// repoAddScript baut das Einrichtungsskript einer Katalog-Quelle passend zur
// Paketverwaltung. Alle eingebetteten Werte (Key, KeyURL, Line) sind bereits
// streng validiert (SaveKnownRepo: keine Quotes, Backslashes, Steuerzeichen
// oder Subshells), daher ist die wörtliche Einbettung in doppelte
// Anführungszeichen sicher.
func repoAddScript(mgr string, repo domain.KnownRepo) string {
	switch pkgFamily(mgr) {
	case pkgDnf:
		// Line = URL einer .repo-Datei → nach /etc/yum.repos.d/ laden.
		repofile := "/etc/yum.repos.d/lcm-" + repo.Key + ".repo"
		lines := []string{
			"set -e",
			"install -d -m 755 /etc/yum.repos.d",
			fmt.Sprintf(`if command -v curl >/dev/null 2>&1; then curl -fsSL "%s" -o %s; else wget -qO %s "%s"; fi`, repo.Line, repofile, repofile, repo.Line),
			fmt.Sprintf("chmod 644 %s", repofile),
		}
		if repo.KeyURL != "" {
			lines = append(lines, fmt.Sprintf(`rpm --import "%s" || true`, repo.KeyURL))
		}
		lines = append(lines, fmt.Sprintf("cat %s", repofile), dnfBin(mgr)+" -y makecache")
		return strings.Join(lines, "\n")
	case pkgZypper:
		// Line = Repository-URL → zypper addrepo (Alias lcm-<key>).
		alias := "lcm-" + repo.Key
		return strings.Join([]string{
			"set -e",
			fmt.Sprintf("zypper --non-interactive removerepo %s >/dev/null 2>&1 || true", alias),
			fmt.Sprintf(`zypper --non-interactive addrepo --refresh "%s" %s`, repo.Line, alias),
			fmt.Sprintf("zypper --non-interactive --gpg-auto-import-keys refresh %s", alias),
		}, "\n")
	case pkgPacman:
		// Line = Server-URL → Abschnitt [<key>] in /etc/pacman.conf; Key wird
		// importiert und lokal signiert (sonst lehnt pacman die Pakete ab).
		lines := []string{"set -e"}
		if repo.KeyURL != "" {
			keyfile := "/tmp/lcm-" + repo.Key + ".gpg"
			lines = append(lines,
				fmt.Sprintf(`if command -v curl >/dev/null 2>&1; then curl -fsSL "%s" -o %s; else wget -qO %s "%s"; fi`, repo.KeyURL, keyfile, keyfile, repo.KeyURL),
				fmt.Sprintf("pacman-key --add %s", keyfile),
				fmt.Sprintf(`FP="$(gpg --with-colons --show-keys %s 2>/dev/null | awk -F: '/^fpr:/{print $10; exit}')"`, keyfile),
				`[ -n "$FP" ] && pacman-key --lsign-key "$FP" || true`,
			)
		}
		lines = append(lines,
			fmt.Sprintf(`grep -q '^\[%s\]' /etc/pacman.conf || printf '\n[%%s]\nServer = %%s\n' "%s" "%s" >> /etc/pacman.conf`, repo.Key, repo.Key, repo.Line),
			"pacman -Sy",
		)
		return strings.Join(lines, "\n")
	case pkgApk:
		// Line = Repository-URL → Zeile in /etc/apk/repositories; Key nach
		// /etc/apk/keys.
		keyfile := "/etc/apk/keys/lcm-" + repo.Key + ".rsa.pub"
		lines := []string{"set -e"}
		if repo.KeyURL != "" {
			lines = append(lines,
				"install -d -m 755 /etc/apk/keys",
				fmt.Sprintf(`if command -v curl >/dev/null 2>&1; then curl -fsSL "%s" -o %s; else wget -qO %s "%s"; fi`, repo.KeyURL, keyfile, keyfile, repo.KeyURL),
			)
		}
		lines = append(lines,
			fmt.Sprintf(`grep -qxF "%s" /etc/apk/repositories || echo "%s" >> /etc/apk/repositories`, repo.Line, repo.Line),
			"apk update",
		)
		return strings.Join(lines, "\n")
	default:
		// apt: GPG-Key (armored) nach /etc/apt/keyrings, signed-by-Zeile nach
		// /etc/apt/sources.list.d, danach apt-Update. $ID/$CODENAME/$ARCH
		// werden auf dem Zielsystem aufgelöst.
		keyring := "/etc/apt/keyrings/" + repo.Key + ".asc"
		list := "/etc/apt/sources.list.d/lcm-" + repo.Key + ".list"
		return strings.Join([]string{
			"set -e",
			". /etc/os-release",
			`CODENAME="${VERSION_CODENAME:-}"`,
			`ARCH="$(dpkg --print-architecture)"`,
			fmt.Sprintf(`KEY_URL="%s"`, repo.KeyURL),
			"install -d -m 755 /etc/apt/keyrings",
			fmt.Sprintf(`if command -v curl >/dev/null 2>&1; then curl -fsSL "$KEY_URL" -o %s; else wget -qO %s "$KEY_URL"; fi`, keyring, keyring),
			fmt.Sprintf("chmod 644 %s", keyring),
			fmt.Sprintf(`printf '%%s\n' "%s" > %s`, repo.Line, list),
			fmt.Sprintf("cat %s", list),
			"apt-get update",
		}, "\n")
	}
}

// rescanRepositories liest die Paketquellen (passend zur Paketverwaltung) neu
// ein und aktualisiert den Bestand in der Datenbank (best effort -
// Anzeigedaten).
func (s *ServerService) rescanRepositories(conn sshx.Conn, server *domain.Server, mgr string) {
	run := func(_, cmd string) string {
		out, code, err := conn.Run(cmd)
		if err != nil || code != 0 {
			return ""
		}
		return out
	}
	repos := scanReposFor(mgr, run)
	_ = s.servers.ReplaceRepositories(server.ID, repos)
	s.updateHTTPSRevertURLs(conn, server, repos)
	s.pruneHTTPSUnavailableURLs(server, repos)
}

// aptUpdateProbe lässt `apt-get update` laufen und reicht die Ausgabe durch.
// Ausgewertet wird sie mit aptUpdateOK, denn der Exit-Code allein genügt nicht:
// `apt-get update` endet auch dann mit 0, wenn einzelne Quellen mit „Err:"
// scheitern. So blieb ein Spiegel ohne HTTPS als gemeldeter Erfolg stehen.
// LC_ALL=C, weil apt auf einem deutschen System „Fehl:" schreibt.
const aptUpdateProbe = `LCM_OUT=$(LC_ALL=C apt-get update 2>&1); LCM_RC=$?
printf '%s\n' "$LCM_OUT"`

// aptUpdateOK ist die Bedingung zu aptUpdateProbe. Sie gilt für alle Quellen:
// War eine fremde Quelle schon vorher kaputt, bleibt der Server lieber beim
// bekannten Stand, und die Ausgabe nennt die Quelle.
const aptUpdateOK = `[ "$LCM_RC" -eq 0 ] && ! printf '%s\n' "$LCM_OUT" | grep -qE '^(Err|E):'`

// rollbackSourcesScript stellt die gesicherten Quellen wieder her und lädt die
// Paketlisten neu: apt verwirft beim Update die Listen der alten Adressen.
const rollbackSourcesScript = `  for f in $(find /etc/apt -name '*.lcm-bak'); do mv "$f" "${f%.lcm-bak}"; done
  apt-get update >/dev/null 2>&1 || true`

// aptSourceFiles sind die Dateien, in denen apt seine Quellen liest.
const aptSourceFiles = "/etc/apt/sources.list /etc/apt/sources.list.d/*.list /etc/apt/sources.list.d/*.sources"

// secureHTTPSScript stellt alle http-Quellen auf https um.
func secureHTTPSScript() string {
	return secureWithRollback([]string{
		"for f in " + aptSourceFiles + "; do",
		`  [ -f "$f" ] || continue`,
		`  grep -q 'http://' "$f" || continue`,
		`  cp "$f" "$f.lcm-bak"`,
		`  sed -i 's|http://|https://|g' "$f"`,
		"  changed=1",
		"done",
		`if [ "$changed" -eq 0 ]; then echo 'LCM: keine http-quellen gefunden - nichts zu tun'; exit 0; fi`,
	}, "LCM: alle paketquellen auf https umgestellt")
}

// secureHTTPSURLsScript stellt genau die genannten http-URLs auf https um,
// URL-weise wie revertHTTPSScript. Eine URL zählt nur als Ganzes - gefolgt von
// Leerraum oder dem Zeilenende -, sonst stellte .../debian auch
// .../debian-security mit um, obwohl der Anwender es abgewählt hat. Ein
// Schrägstrich am Ende zählt nicht: deb822-Dateien schreiben „.../ubuntu/",
// einzeilige oft „.../ubuntu". Geändert gilt nur, was sich wirklich geändert
// hat, sonst meldete die Umstellung Erfolg, ohne etwas zu tun.
func secureHTTPSURLsScript(uris []string) string {
	return secureWithRollback([]string{
		"URLS='" + strings.Join(uris, " ") + "'",
		"for f in " + aptSourceFiles + "; do",
		`  [ -f "$f" ] || continue`,
		`  grep -q 'http://' "$f" || continue`,
		`  cp "$f" "$f.lcm-bak"`,
		`  for u in $URLS; do ` + httpsURLSed + `; done`,
		`  if cmp -s "$f" "$f.lcm-bak"; then rm -f "$f.lcm-bak"; else changed=1; fi`,
		"done",
		`if [ "$changed" -eq 0 ]; then echo 'LCM: keine der genannten quellen gefunden - nichts zu tun'; exit 0; fi`,
	}, "LCM: ausgewaehlte paketquellen auf https umgestellt")
}

// httpsURLSed stellt die URL $u in der Datei $f auf https um, mit oder ohne
// Schrägstrich am Ende. Derselbe Befehl steht im LCM-Helper (https_switch_file).
const httpsURLSed = `b="${u%/}"; h="https://${b#http://}"; sed -i -e "s|$b\(/\{0,1\}[[:space:]]\)|$h\1|g" -e "s|$b\(/\{0,1\}\)\$|$h\1|" "$f"`

// secureWithRollback rahmt die Änderung an den Quellen ein: vorher
// ca-certificates, danach die Probe mit `apt-get update`. Meldet sie eine
// fehlerhafte Quelle, wird alles zurückgerollt und die Aktion als Fehler
// gemeldet; sonst wandern die Sicherungen ins Protokoll.
func secureWithRollback(change []string, okMessage string) string {
	lines := []string{
		// Root-Zertifikate für TLS sicherstellen (best effort - auf minimalen
		// Systemen fehlen sie; apt >= 1.5 spricht https nativ).
		"apt-get install -y --no-install-recommends ca-certificates >/dev/null 2>&1 || true",
		"changed=0",
	}
	lines = append(lines, change...)
	return strings.Join(append(lines,
		aptUpdateProbe,
		"if "+aptUpdateOK+"; then",
		keepBackupsScript,
		"  echo '"+okMessage+"'",
		"else",
		rollbackSourcesScript,
		"  echo 'LCM: apt-update nach der umstellung fehlgeschlagen - alle aenderungen zurueckgerollt'",
		"  exit 1",
		"fi",
	), "\n")
}

// httpsBackupDir ist das Protokoll der https-Umstellung: die Sicherungskopien
// der Quellen-Dateien, wie sie VOR der Umstellung aussahen - unter dem
// Original-Pfad, nur mit diesem Präfix davor.
//
// Sie bleiben nach der Umstellung liegen, und genau darin liegt ihr Zweck:
// Ohne sie ließe sich später nicht mehr sagen, welche Quelle vorher http war
// und welche von sich aus schon https sprach. Erst damit kann die
// Rückstellung die Fremdquellen in Ruhe lassen.
const httpsBackupDir = "/var/backups/lcm-apt-https"

// keepBackupsScript hebt die Sicherungen des laufenden Umstellungs-Vorgangs
// ins Protokoll. Eine bereits vorhandene Sicherung bleibt unangetastet: Die
// älteste ist der echte Vorzustand - eine zweite Umstellung würde sonst einen
// Stand festschreiben, in dem die erste schon gewirkt hat.
const keepBackupsScript = `  for f in $(find /etc/apt -name '*.lcm-bak'); do
    orig="${f%.lcm-bak}"; dest="` + httpsBackupDir + `$orig"
    if [ -f "$dest" ]; then rm -f "$f"; else mkdir -p "$(dirname "$dest")"; mv "$f" "$dest"; fi
  done`

// httpsRecordScript liest die von LCM umgestellten URLs aus dem Protokoll.
const httpsRecordScript = "grep -rhoE 'http://[^ ]+' " + httpsBackupDir + " 2>/dev/null | sort -u | head -200 || true"

// ErrNoRevertCandidates: für diesen Server ist keine Quelle bekannt, die sich
// zurückstellen ließe.
var ErrNoRevertCandidates = errors.New("keine Paketquelle zum Zurückstellen")

// ErrNotRevertible: die angeforderte Quelle steht nicht auf der Kandidatenliste
// des Servers. Alles andere wäre ein Downgrade auf Verdacht.
var ErrNotRevertible = errors.New("diese Paketquelle ist nicht zum Zurückstellen vorgemerkt")

// RevertRepositoriesHTTPS stellt Paketquellen von https auf http zurück -
// aber nur die, die vor der LCM-Umstellung http waren (siehe httpsBackupDir
// und domain.HTTPSRevertCandidates). Ohne uris werden alle Kandidaten des
// Servers zurückgestellt.
//
// Sicherheitsnetz wie bei der Umstellung: Vor der Änderung wird jede Datei
// gesichert - meldet `apt-get update` danach eine fehlerhafte Quelle, wird
// alles zurückgerollt.
func (s *ServerService) RevertRepositoriesHTTPS(scope repositories.AccessScope, id uint, uris []string, actor string) (string, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return "", err
	}
	script, _, err := planRevertRepos(server, uris)
	if err != nil {
		return "", err
	}

	conn, err := s.connectRec(server, "revert-repos-https", actor)
	if err != nil {
		return "", fmt.Errorf("verbindung: %w", err)
	}
	defer conn.Close()
	output, code, runErr := conn.Run(privRun(server, script))
	if runErr != nil {
		return output, runErr
	}
	if code != 0 {
		return output, repoSwitchError("https-rückstellung", output, code)
	}
	s.rescanRepositories(conn, server, pkgApt)
	s.audit.Log(actor, "server.revert-repos-https", "server", id, server.Name)
	return output, nil
}

// planRevertRepos prüft Server und Auswahl und baut das Skript der
// Rückstellung - für den Lauf und die Befehlsvorschau gleich. Der zweite
// Rückgabewert sind die URLs, die zurückgestellt werden.
func planRevertRepos(server *domain.Server, uris []string) (string, []string, error) {
	if server.IsRouterOS() {
		return "", nil, ErrRouterOSUnsupported
	}
	if server.IsProxmox() {
		return "", nil, ErrProxmoxRestricted
	}
	if pkgFamily(server.PackageManager) != pkgApt {
		return "", nil, fmt.Errorf("%w: die https-Rückstellung betrifft apt-Quellen, dieser Server nutzt %s",
			ErrRepoPackageManagerMismatch, PackageManagerLabel(server.PackageManager))
	}
	targets, err := revertTargets(server.HTTPSRevertURLs, uris)
	if err != nil {
		return "", nil, err
	}
	if server.RestrictedSudo {
		return helperCmd("repos-http", targets...), targets, nil
	}
	return revertHTTPSScript(targets), targets, nil
}

// revertTargets prüft die Auswahl gegen die beim Scan ermittelten Kandidaten
// des Servers. Eine leere Auswahl heißt „alle Kandidaten".
//
// Die Kandidatenliste ist hier die eigentliche Absicherung: Was nicht darauf
// steht, kommt auch nicht in die Kommandozeile - weder eine Fremdquelle, die
// von Haus aus https spricht, noch eine URL mit Sonderzeichen (die filtert
// domain.HTTPSRevertCandidates schon beim Ermitteln aus).
func revertTargets(candidates string, uris []string) ([]string, error) {
	allowed := map[string]bool{}
	var all []string
	for _, uri := range strings.Split(candidates, ",") {
		if uri = strings.TrimSpace(uri); uri != "" {
			allowed[uri] = true
			all = append(all, uri)
		}
	}
	if len(all) == 0 {
		return nil, ErrNoRevertCandidates
	}
	var chosen []string
	for _, uri := range uris {
		if uri = strings.TrimSpace(uri); uri == "" {
			continue
		}
		if !allowed[uri] {
			return nil, fmt.Errorf("%w: %s", ErrNotRevertible, uri)
		}
		chosen = append(chosen, uri)
	}
	if len(chosen) == 0 {
		return all, nil
	}
	return chosen, nil
}

// revertHTTPSScript dreht genau die genannten https-URLs auf http zurück.
//
// Gearbeitet wird URL-weise statt dateiweise: Die alte Sicherung
// zurückzukopieren würde auch alles überschreiben, was seit der Umstellung an
// der Datei geändert wurde - neue Quellen, andere Komponenten, Kommentare.
func revertHTTPSScript(uris []string) string {
	return strings.Join([]string{
		"URLS='" + strings.Join(uris, " ") + "'",
		"changed=0",
		"for f in /etc/apt/sources.list /etc/apt/sources.list.d/*.list /etc/apt/sources.list.d/*.sources; do",
		`  [ -f "$f" ] || continue`,
		"  hit=0",
		`  for u in $URLS; do grep -qF "$u" "$f" && hit=1; done`,
		`  [ "$hit" -eq 1 ] || continue`,
		`  cp "$f" "$f.lcm-bak"`,
		`  for u in $URLS; do ` + httpURLSed + `; done`,
		`  if cmp -s "$f" "$f.lcm-bak"; then rm -f "$f.lcm-bak"; else changed=1; fi`,
		"done",
		`if [ "$changed" -eq 0 ]; then echo 'LCM: keine der genannten quellen gefunden - nichts zu tun'; exit 0; fi`,
		aptUpdateProbe,
		"if " + aptUpdateOK + "; then",
		"  find /etc/apt -name '*.lcm-bak' -delete",
		"  echo 'LCM: paketquellen auf http zurueckgestellt'",
		"else",
		rollbackSourcesScript,
		"  echo 'LCM: apt-update nach der rueckstellung fehlgeschlagen - alle aenderungen zurueckgerollt'",
		"  exit 1",
		"fi",
	}, "\n")
}

// httpURLSed stellt die URL $u in der Datei $f auf http zurück - nur als
// Ganzes, gefolgt von Leerraum, einem Schrägstrich oder dem Zeilenende. Ohne
// diese Grenze drehte .../debian auch .../debian-security mit zurück, und
// https://deb.example.org auch https://deb.example.org.evil. Das Gegenstück
// zu httpsURLSed; derselbe Befehl steht im LCM-Helper (repos_http).
const httpURLSed = `b="${u%/}"; h="http://${b#https://}"; sed -i -e "s|$b\(/\{0,1\}[[:space:]]\)|$h\1|g" -e "s|$b\(/\{0,1\}\)\$|$h\1|" "$f"`

// updateHTTPSRevertURLs schreibt fort, welche Quellen des Servers sich auf
// http zurückstellen ließen. Aufgerufen dort, wo sich der Bestand ändern kann:
// beim vollständigen Scan und nach jeder Repo-Aktion.
func (s *ServerService) updateHTTPSRevertURLs(conn sshx.Conn, server *domain.Server, repos []domain.AptRepository) {
	if pkgFamily(server.PackageManager) != pkgApt {
		return
	}
	var recorded []string
	if out, code, err := conn.Run(privRun(server, httpsRecordScript)); err == nil && code == 0 {
		recorded = strings.Fields(out)
	}
	urls := strings.Join(domain.HTTPSRevertCandidates(recorded, repos), ",")
	if urls == server.HTTPSRevertURLs {
		return
	}
	_ = s.servers.UpdateFields(server.ID, map[string]any{"https_revert_urls": urls})
}
