package services

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

// apt-Kanal-Umstellung des LCM-Hosts zwischen den drei Paketkanälen:
// Community (offen, jedes Release sofort), Beta (offen, Vorabversionen zum
// Testen) und Enterprise (zugangsgeschützt, abgehangene Releases). Läuft als
// normaler Job über den SSH-Weg auf dem selbst-registrierten LCM-Host - mit
// Protokoll, Job-Verknüpfung und Sperre wie jede andere Server-Aktion.
//
// Das Vorgehen entspricht packaging/repo-server/setup-enterprise.sh
// (Zugangsdaten in apt auth.conf.d, eigene Quelle, Community-Quelle
// stilllegen, Prüfung NUR gegen die neue Quelle, Rückbau bei Fehlschlag)
// - nur dass die Zugangsdaten hier der instanzgebundene Zugangsschlüssel
// sind, nicht der Subscription-Key.
//
// Der Beta-Kanal ist der einfache Fall: offen, kein Zugang nötig, und er
// liegt als eigene Suite an derselben Wurzel wie der Community-Kanal. Er
// kommt deshalb NEBEN die Community-Quelle statt an ihre Stelle - apt nimmt
// dann von beiden die neuere Version. Genau das will ein Beta-Tester: die
// Vorabversion, solange sie vorne liegt, und das fertige Release, sobald es
// erscheint (Sicherheitsupdates eingeschlossen).

const (
	subAptAuthFile = "/etc/apt/auth.conf.d/lcm-enterprise.conf"
	subAptEntList  = "/etc/apt/sources.list.d/lcm-enterprise.list"
	subAptBetaList = "/etc/apt/sources.list.d/lcm-beta.list"
	// Community-Quelle in beiden Schreibweisen: setup.sh legt sie heute als
	// deb822-Datei (.sources) an, ältere Installationen haben die klassische
	// Einzeilen-Datei (.list). Wer nur eine der beiden kennt, legt die andere
	// nicht stillt - und dann laufen beide Kanäle nebeneinander.
	subAptComList    = "/etc/apt/sources.list.d/techeve.list"
	subAptComSources = "/etc/apt/sources.list.d/techeve.sources"
	// Vorrang-Regel: hält die LCM-Pakete auf dem Enterprise-Kanal, ohne die
	// Community-Quelle abzuschalten - die liefert auch andere Pakete.
	subAptPrefFile  = "/etc/apt/preferences.d/lcm-enterprise.pref"
	subAptSuite     = "stable"
	subAptSuiteBeta = "beta"
	subAptJobEnable = "Enterprise-Paketkanal einrichten"
	subAptJobBeta   = "Beta-Paketkanal einrichten"
	subAptJobRevert = "Zurück zum Community-Paketkanal"

	// Kennzeichnung der Kanäle im Release-File (aptly: Label je Publish-Punkt,
	// gesetzt mit packaging/repo-server/set-channel-metadata.sh). Erst damit
	// kann apt die Kanäle auseinanderhalten: gleicher Host, gleiche Suite.
	aptLabelCommunity = "techeve-community"
	aptLabelBeta      = "techeve-beta"
	// Die Pakete, die aus dem Enterprise-Kanal kommen müssen.
	aptPinnedPackages = "lcm lcm-agent"
)

// aptChannelPaths bündelt die beteiligten apt-Dateien. Produktiv sind das die
// Konstanten oben; die Tests schieben eine Sandbox davor und führen die
// Skripte dort wirklich aus.
type aptChannelPaths struct {
	Auth         string // Zugangsdaten (auth.conf.d)
	Ent          string // Enterprise-Quelle (klassisch)
	Beta         string // Beta-Quelle (klassisch)
	ComList      string // Community-Quelle, klassisch
	ComSources   string // Community-Quelle, deb822
	Pref         string // Vorrang-Regel (preferences.d)
	KeyringGlobs string // Suchmuster für den Signaturschlüssel (Rückfall)
}

func defaultAptChannelPaths() aptChannelPaths {
	return aptChannelPaths{
		Auth:         subAptAuthFile,
		Ent:          subAptEntList,
		Beta:         subAptBetaList,
		ComList:      subAptComList,
		ComSources:   subAptComSources,
		Pref:         subAptPrefFile,
		KeyringGlobs: "/etc/apt/keyrings/techeve*.gpg /etc/apt/keyrings/techeve*.asc /usr/share/keyrings/techeve*.gpg",
	}
}

// aptPathVars setzt die Pfad-Variablen an den Anfang eines Skripts.
func (p aptChannelPaths) vars() string {
	return "AUTH=" + p.Auth + "\n" +
		"ENT=" + p.Ent + "\n" +
		"BETA=" + p.Beta + "\n" +
		"COM=" + p.ComList + "\n" +
		"COMS=" + p.ComSources + "\n" +
		"PREF=" + p.Pref + "\n"
}

// keyringSnippet sucht den Signaturschlüssel des TechEve-Repositorys. Alle
// drei Kanäle sind mit demselben Schlüssel signiert; er steht je nach Format
// der Community-Quelle in der signed-by-Option oder im Feld „Signed-By:".
// Findet sich keiner, ist das Repository nicht eingerichtet - dann bricht das
// Skript ab, statt eine unsignierte Quelle zu schreiben.
func keyringSnippet(globs string) string {
	return "KEYRING=$(sed -n 's/.*signed-by=\\([^]]*\\)\\].*/\\1/p' \"$COM\" \"$COM.disabled\" 2>/dev/null | head -1)\n" +
		"[ -n \"$KEYRING\" ] || KEYRING=$(sed -n 's/^[Ss]igned-[Bb]y:[[:space:]]*//p' \"$COMS\" 2>/dev/null | head -1)\n" +
		"[ -n \"$KEYRING\" ] || KEYRING=$(ls " + globs + " 2>/dev/null | head -1)\n" +
		"[ -n \"$KEYRING\" ] || { echo 'Signatur-Keyring nicht gefunden - ist das TechEve-Repository eingerichtet?'; exit 1; }\n"
}

// baseURLSnippet ermittelt die Wurzel des Paket-Servers aus der
// Community-Quelle des Hosts - der Beta-Kanal liegt auf demselben Server,
// nur in einer anderen Suite. Damit sind eigene Repository-Server
// mitgedacht; erst wenn sich dort nichts findet, greift die vom Dienst
// bekannte Adresse (fallback).
func baseURLSnippet(fallback string) string {
	return "BASE=\n" +
		// Klassische Quelle: „deb [optionen] <URL> <suite> <komponenten>" -
		// die URL ist das erste Feld mit Schema. Jede Datei einzeln, ein
		// awk über eine fehlende Datei bricht sonst das Skript ab.
		"for f in \"$COM\" \"$COM.disabled\"; do\n" +
		"  if [ -f \"$f\" ]; then BASE=$(awk '$1==\"deb\"{for(i=2;i<=NF;i++) if($i ~ /:\\/\\//){print $i; exit}}' \"$f\" | head -1); fi\n" +
		"  if [ -n \"$BASE\" ]; then break; fi\n" +
		"done\n" +
		"[ -n \"$BASE\" ] || BASE=$(sed -n 's/^[Uu][Rr][Ii][Ss]:[[:space:]]*//p' \"$COMS\" 2>/dev/null | awk '{print $1}' | head -1)\n" +
		"[ -n \"$BASE\" ] || BASE='" + fallback + "'\n" +
		"[ -n \"$BASE\" ] || { echo 'Adresse des Paket-Servers nicht ermittelbar - ist das TechEve-Repository eingerichtet?'; exit 1; }\n"
}

// comDisableSnippet legt die Community-Quelle still - in beiden Formaten.
// Klassisch per Umbenennen (apt liest nur *.list), deb822 per „Enabled: no"
// (den Schalter kennt apt selbst; die Datei behält Name und Inhalt, der
// Rückweg ist damit sauber). Findet sich gar keine Community-Quelle, sagt das
// Skript es laut: dann zeigt womöglich eine anders benannte Datei weiter auf
// den freien Kanal, und genau das soll niemand übersehen.
const comDisableSnippet = "DIS=0\n" +
	"if [ -e \"$COM\" ]; then mv \"$COM\" \"$COM.disabled\"; DIS=1; fi\n" +
	"if [ -e \"$COM.disabled\" ]; then DIS=1; fi\n" +
	"if [ -e \"$COMS\" ]; then\n" +
	"  awk '{k=tolower($1)} k==\"enabled:\"{next} {print} k==\"types:\"{print \"Enabled: no\"}' \"$COMS\" > \"$COMS.lcmtmp\"\n" +
	// Nur zurückschreiben, wenn wirklich etwas herauskam: ein gescheitertes
	// awk darf niemals die einzige Paketquelle der Maschine leeren.
	"  if [ -s \"$COMS.lcmtmp\" ]; then cat \"$COMS.lcmtmp\" > \"$COMS\"; fi\n" +
	"  rm -f \"$COMS.lcmtmp\"\n" +
	"  DIS=1\n" +
	"fi\n" +
	"if [ \"$DIS\" = 0 ]; then echo \"Hinweis: keine Community-Paketquelle gefunden (weder $COM noch $COMS) - bitte prüfen, ob eine anders benannte Quelle weiterhin auf den freien Kanal zeigt.\"; fi\n"

// comEnableSnippet macht comDisableSnippet rückgängig: klassische Datei
// zurückbenennen, im deb822-Format die von uns gesetzte Zeile „Enabled: no"
// wieder entfernen. Ein vom Betreiber selbst gesetztes „Enabled: yes" bleibt
// unangetastet.
const comEnableSnippet = "if [ -e \"$COM.disabled\" ]; then mv \"$COM.disabled\" \"$COM\"; fi\n" +
	"if [ -e \"$COMS\" ]; then\n" +
	"  awk '{k=tolower($1)} k==\"enabled:\" && tolower($2)==\"no\"{next} {print}' \"$COMS\" > \"$COMS.lcmtmp\"\n" +
	"  if [ -s \"$COMS.lcmtmp\" ]; then cat \"$COMS.lcmtmp\" > \"$COMS\"; fi\n" +
	"  rm -f \"$COMS.lcmtmp\"\n" +
	"fi\n"

// channelSeparationSnippet trennt die Kanäle - auf dem schonenden Weg, wenn
// der Repository-Server ihn hergibt.
//
// Bevorzugt per Vorrang-Regel: apt bekommt gesagt, dass die LCM-Pakete aus dem
// Community-Kanal nie in Frage kommen (Priorität -1). Die Community-Quelle
// bleibt dabei aktiv und liefert weiter ihre anderen Pakete - beim Stilllegen
// der ganzen Quelle wären die mit weg.
//
// Das setzt voraus, dass die Kanäle im Release-File unterscheidbar sind
// (Label). Alte Publish-Punkte tragen keins; dann bleibt nur der grobe Weg,
// und das Skript sagt auch, warum. Die Prüfung läuft gegen die WIEDER
// eingeschaltete Community-Quelle - sonst könnte eine frühere Umstellung den
// feineren Weg für immer verstellen.
const channelSeparationSnippet = comEnableSnippet +
	"apt-get update -qq || true\n" +
	"if LC_ALL=C apt-cache policy 2>/dev/null | grep -q 'l=" + aptLabelCommunity + "'; then\n" +
	"  mkdir -p \"$(dirname \"$PREF\")\"\n" +
	"  printf '%s\\n' '# Von LCM verwaltet: die LCM-Pakete kommen nur aus dem Enterprise-Kanal.' " +
	"'# Die Community-Quelle bleibt fuer alle anderen Pakete nutzbar.' " +
	"'Package: " + aptPinnedPackages + "' 'Pin: release l=" + aptLabelCommunity + "' 'Pin-Priority: -1' '' " +
	"'Package: " + aptPinnedPackages + "' 'Pin: release l=" + aptLabelBeta + "' 'Pin-Priority: -1' > \"$PREF\"\n" +
	"  chmod 0644 \"$PREF\"\n" +
	"  echo 'Kanaltrennung per Vorrang-Regel: die Community-Quelle bleibt aktiv, liefert aber kein LCM mehr.'\n" +
	"else\n" +
	"  rm -f \"$PREF\"\n" +
	comDisableSnippet +
	"  echo 'Hinweis: der Repository-Server kennzeichnet seine Kanäle noch nicht (kein Label im Release-File) - deshalb wurde die Community-Quelle stillgelegt. Andere Pakete von dort sind damit vorerst nicht verfügbar.'\n" +
	"fi\n" +
	"apt-get update -qq || true\n"

// candidateSnippet ermittelt, welche lcm-Version apt jetzt nähme und aus
// welcher Quelle: CAND (Version), SRC (URL) und SUITE (Suite/Komponente).
// Leeres SRC = kein Kandidat, dann gibt es nichts zu melden.
//
// LC_ALL=C ist Pflicht: apt-cache übersetzt seine Ausgabe, und auf einem
// deutschen System heißt es „Kandidat:" und „Versionstabelle:". Gesucht wird
// die Quellzeile DIREKT unter der Kandidaten-Version in der Versionstabelle -
// die Zeile „Candidate: …" selbst steht davor und darf den Treffer nicht
// auslösen.
const candidateSnippet = "CAND=$(LC_ALL=C apt-cache policy lcm 2>/dev/null | sed -n 's/^ *Candidate: *//p' | head -1)\n" +
	"LINE=$(LC_ALL=C apt-cache policy lcm 2>/dev/null | awk -v v=\"$CAND\" '" +
	"/Version table:/ {t=1; next} " +
	"t && !f && $0 ~ (\"(^| )\" v \"( |$)\") {f=1; next} " +
	"f {if ($0 ~ /:\\/\\//) print $2, $3; exit}')\n" +
	"SRC=${LINE%% *}\n" +
	"SUITE=${LINE#* }\n"

// channelProofSnippet ist die Gegenprobe für den Enterprise-Kanal: aus
// welcher Quelle käme lcm jetzt? Das ist die Frage, um die es geht - schärfer
// als „ist die Community-Quelle weg", und sie stimmt für beide Wege der
// Kanaltrennung.
func channelProofSnippet(repoURL string) string {
	return candidateSnippet +
		"if [ -n \"$SRC\" ]; then\n" +
		"  case \"$SRC\" in\n" +
		"    '" + repoURL + "'*) echo \"Gegenprobe: lcm $CAND kommt aus $SRC.\" ;;\n" +
		"    *) echo \"WARNUNG: lcm $CAND käme weiterhin aus $SRC statt aus dem Enterprise-Kanal - bitte die apt-Quellen des LCM-Hosts prüfen.\" ;;\n" +
		"  esac\n" +
		"fi\n"
}

// betaProofSnippet ist die Gegenprobe für den Beta-Kanal. Sie warnt bewusst
// NICHT, wenn der Kandidat aus der Community-Quelle kommt: beide Quellen sind
// aktiv, und solange im Beta-Kanal nichts Neueres liegt, ist das fertige
// Release die richtige Antwort. Gesagt werden muss es trotzdem - sonst sucht
// jemand nach einer Vorabversion, die es gerade nicht gibt.
const betaProofSnippet = candidateSnippet +
	"if [ -n \"$SRC\" ]; then\n" +
	"  case \"$SUITE\" in\n" +
	"    " + subAptSuiteBeta + "/*) echo \"Gegenprobe: lcm $CAND kommt aus dem Beta-Kanal ($SRC $SUITE).\" ;;\n" +
	"    *) echo \"Hinweis: die Beta-Quelle ist eingerichtet, die neueste Version kommt derzeit aber aus $SRC $SUITE (lcm $CAND) - im Beta-Kanal liegt gerade keine neuere Vorabversion.\" ;;\n" +
	"  esac\n" +
	"fi\n"

// enterpriseChannelScript baut das Umstell-Skript. login/secret sind
// Instanz-ID (UUID) und Zugangsschlüssel (hex) - beide shell-harmlos,
// werden aber trotzdem single-quoted. repoURL kommt vom Dienst
// (z. B. https://repo.techeve.de/enterprise).
func enterpriseChannelScript(repoURL, login, secret string) string {
	return enterpriseChannelScriptIn(defaultAptChannelPaths(), repoURL, login, secret)
}

func enterpriseChannelScriptIn(p aptChannelPaths, repoURL, login, secret string) string {
	return "set -e\n" +
		p.vars() +
		// Signatur-Keyring aus der Community-Quelle übernehmen - alle Kanäle
		// sind mit demselben Schlüssel signiert.
		keyringSnippet(p.KeyringGlobs) +
		"umask 077\n" +
		"mkdir -p \"$(dirname \"$AUTH\")\"\n" +
		"printf 'machine %s\\nlogin %s\\npassword %s\\n' '" + repoURL + "' '" + login + "' '" + secret + "' > \"$AUTH\"\n" +
		"chmod 0600 \"$AUTH\"\n" +
		"printf 'deb [signed-by=%s] %s " + subAptSuite + " main\\n' \"$KEYRING\" '" + repoURL + "' > \"$ENT\"\n" +
		// Prüfung NUR gegen die neue Quelle - ein falscher Schlüssel muss
		// laut scheitern und rückgebaut werden, nicht im Rauschen eines
		// vollen apt-get update untergehen.
		"if ! apt-get update -qq -o Dir::Etc::sourcelist=\"sources.list.d/$(basename \"$ENT\")\" -o Dir::Etc::sourceparts=\"-\" -o APT::Get::List-Cleanup=\"0\"; then\n" +
		"  rm -f \"$AUTH\" \"$ENT\" \"$PREF\"\n" +
		comEnableSnippet +
		"  echo 'Enterprise-Kanal NICHT eingerichtet - Zugang abgelehnt; Community-Quelle wiederhergestellt.'\n" +
		"  exit 1\n" +
		"fi\n" +
		// Eine Beta-Quelle von früher muss weg: sie ist offen und läge
		// versionsmäßig vor dem abgehangenen Kanal - der Enterprise-Wechsel
		// wäre damit hinfällig. Erst nach der bestandenen Prüfung, damit der
		// Rückbau oben nichts anfassen muss.
		"rm -f \"$BETA\"\n" +
		// Erst jetzt trennen: beide Kanäle zugleich wären sinnlos, apt nähme
		// immer die höhere Version - und die kommt früher oder später aus
		// dem freien Kanal.
		channelSeparationSnippet +
		channelProofSnippet(repoURL) +
		"echo 'Enterprise-Paketkanal aktiv - Updates kommen ab jetzt aus dem abgehangenen Kanal.'\n"
}

// betaChannelScript baut die Umstellung auf den Beta-Kanal. Der ist offen -
// es gibt hier weder Zugangsdaten noch eine Vorrang-Regel: die Beta-Quelle
// kommt NEBEN die Community-Quelle, apt nimmt von beiden die neuere Version.
// fallbackBase ist die Adresse des Paket-Servers für den Fall, dass sich aus
// den Quellen des Hosts keine ermitteln lässt.
func betaChannelScript(fallbackBase string) string {
	return betaChannelScriptIn(defaultAptChannelPaths(), fallbackBase)
}

func betaChannelScriptIn(p aptChannelPaths, fallbackBase string) string {
	return "set -e\n" +
		p.vars() +
		// Die Community-Quelle gehört im Beta-Kanal wieder eingeschaltet:
		// sie kann von einer früheren Enterprise-Umstellung stillgelegt sein,
		// und ohne sie fehlten dem Host das fertige Release und alle anderen
		// Pakete des Servers.
		comEnableSnippet +
		"if [ ! -e \"$COM\" ] && [ ! -e \"$COMS\" ]; then echo 'Hinweis: auf diesem Host ist keine Community-Paketquelle eingerichtet - dann liefert allein die Beta-Suite LCM-Pakete, fertige Releases kämen erst nach dem Rückweg auf den Community-Kanal.'; fi\n" +
		keyringSnippet(p.KeyringGlobs) +
		baseURLSnippet(fallbackBase) +
		"umask 022\n" +
		"printf 'deb [signed-by=%s] %s " + subAptSuiteBeta + " main\\n' \"$KEYRING\" \"$BASE\" > \"$BETA\"\n" +
		"chmod 0644 \"$BETA\"\n" +
		// Prüfung NUR gegen die neue Quelle: kennt der Server die Beta-Suite
		// nicht, muss das laut scheitern und zurückgebaut werden, statt im
		// Rauschen eines vollen apt-get update unterzugehen.
		"if ! apt-get update -qq -o Dir::Etc::sourcelist=\"sources.list.d/$(basename \"$BETA\")\" -o Dir::Etc::sourceparts=\"-\" -o APT::Get::List-Cleanup=\"0\"; then\n" +
		"  rm -f \"$BETA\"\n" +
		"  echo \"Beta-Kanal NICHT eingerichtet - die Beta-Quelle ($BASE " + subAptSuiteBeta + ") ist nicht erreichbar; es bleibt beim bisherigen Kanal.\"\n" +
		"  exit 1\n" +
		"fi\n" +
		// Enterprise-Reste weg: Zugangsdaten, Quelle und vor allem die
		// Vorrang-Regel - die hält lcm sonst von Community UND Beta fern.
		"rm -f \"$AUTH\" \"$ENT\" \"$PREF\"\n" +
		"apt-get update -qq || true\n" +
		betaProofSnippet +
		"echo 'Beta-Paketkanal aktiv - ab jetzt kommen auch Vorabversionen von LCM.'\n"
}

// communityRevertScript baut den Rückweg. Er lässt die Maschine nie ohne
// Paketquelle zurück: fehlt die Community-Quelle in jeder Schreibweise,
// bleiben die Quellen der anderen Kanäle bestehen und der Job scheitert mit
// Klartext.
func communityRevertScript() string {
	return communityRevertScriptIn(defaultAptChannelPaths())
}

func communityRevertScriptIn(p aptChannelPaths) string {
	return "set -e\n" +
		p.vars() +
		"if [ ! -e \"$COM\" ] && [ ! -e \"$COM.disabled\" ] && [ ! -e \"$COMS\" ]; then\n" +
		"  echo 'Community-Quelle nicht gefunden - Umstellung abgebrochen, die bisherige Quelle bleibt aktiv (sonst stünde der Server ohne LCM-Paketquelle da).'\n" +
		"  exit 1\n" +
		"fi\n" +
		comEnableSnippet +
		// Die Vorrang-Regel muss mit weg: sie hielte lcm sonst weiterhin vom
		// Community-Kanal fern - der Server bekäme nie wieder ein Update.
		// Die Beta-Quelle ebenso, sonst lieferte sie weiter Vorabversionen.
		"rm -f \"$AUTH\" \"$ENT\" \"$PREF\" \"$BETA\"\n" +
		"apt-get update -qq || true\n" +
		"echo 'Community-Paketkanal wieder aktiv.'\n"
}

// lcmHostAptServer sucht den selbst-registrierten LCM-Host mit apt -
// das Ziel der Kanal-Umstellung. nil = nicht vorhanden/kein apt.
func (s *SubscriptionService) lcmHostAptServer() *domain.Server {
	servers, err := s.servers.FindAll(repositories.ScopeAll())
	if err != nil {
		return nil
	}
	for i := range servers {
		if servers[i].IsLcmHost() && servers[i].PackageManager == "apt" {
			return &servers[i]
		}
	}
	return nil
}

// ErrAptSwitchUnavailable: keine apt-Umstellung möglich (kein LCM-Host mit
// apt in der Verwaltung oder Runner nicht verdrahtet).
var ErrAptSwitchUnavailable = errors.New("apt-Umstellung nicht verfügbar: der LCM-Host ist nicht als apt-Server in der Verwaltung (Selbst-Registrierung prüfen)")

// ErrUnknownAptChannel: unbekannter Kanalname.
var ErrUnknownAptChannel = errors.New("unbekannter Paketkanal (community, beta oder enterprise)")

// SetAptChannel stellt den LCM-Host auf einen der drei Paketkanäle um. Nur
// der Enterprise-Kanal verlangt eine aktive Subscription - Community und Beta
// sind offen und stehen jeder Installation offen. Läuft als Job; der
// gespeicherte Kanal wird erst nach ERFOLGREICHEM Job nachgezogen, nicht
// beim Start.
func (s *SubscriptionService) SetAptChannel(channel, actor string) (*domain.Job, error) {
	if !domain.ValidAptChannel(channel) {
		return nil, ErrUnknownAptChannel
	}
	if s.runLcmHostScript == nil || s.lcmHostAptServer() == nil {
		return nil, ErrAptSwitchUnavailable
	}
	settings, err := s.settings.Get()
	if err != nil {
		return nil, err
	}

	var name, script string
	switch channel {
	case domain.AptChannelEnterprise:
		if !settings.SubscriptionConfigured() {
			return nil, ErrNoSubscription
		}
		if settings.SubscriptionStatus != "active" {
			return nil, fmt.Errorf("die Subscription ist nicht aktiv (%s) - erst prüfen bzw. verlängern", settings.SubscriptionStatus)
		}
		accessKey, err := s.cipher.DecryptString(settings.SubscriptionAccessKeyEnc)
		if err != nil {
			return nil, fmt.Errorf("zugangsschlüssel entschlüsseln: %w", err)
		}
		repoURL := strings.TrimSpace(settings.SubscriptionRepoURL)
		if repoURL == "" {
			return nil, errors.New("keine Repository-Adresse gespeichert - Subscription neu aktivieren")
		}
		name, script = subAptJobEnable, enterpriseChannelScript(repoURL, settings.InstanceID, accessKey)
	case domain.AptChannelBeta:
		name, script = subAptJobBeta, betaChannelScript(settings.SubscriptionServiceBase())
	default:
		name, script = subAptJobRevert, communityRevertScript()
	}

	job, err := s.runLcmHostScript(name, script, actor, func(ok bool) {
		if !ok {
			return // Fehlschlag ändert den Kanal nicht - der Stand bleibt ehrlich
		}
		if err := s.settings.UpdateFields(map[string]any{"subscription_apt_channel": channel}); err != nil {
			slog.Warn("subscription apt channel could not be saved", "error", err)
		}
	})
	if err != nil {
		return nil, err
	}
	s.audit.Log(actor, "subscription.apt-channel", "settings", 1, "channel="+channel)
	return job, nil
}
