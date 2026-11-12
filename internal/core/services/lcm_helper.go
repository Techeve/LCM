package services

import (
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"strconv"
	"strings"

	"LCM/internal/core/domain"
)

// LCM-Helper: das Gegenstück der sudoers-Whitelist für Aktionen, die mehr
// brauchen als ein einzelnes Whitelist-Binary - Root-Dateischreibzugriffe an
// FEST UMRISSENEN Stellen (apt-Quellen, sshd-Drop-ins, Benutzer-Sync).
//
// Der Helper ist ein root-eigenes Shell-Skript unter /usr/local/sbin/lcm-helper,
// das beim Einschränken (restrictedProvisionScript) installiert und in die
// sudoers-Whitelist aufgenommen wird. Jedes Unterkommando validiert seine
// Parameter streng (Slugs, Ports, Benutzernamen, URLs) und schreibt nur die
// von LCM verwalteten Pfade - der eingeschränkte Management-Benutzer bekommt
// damit alles, was LCM selbst verwaltet (Repositories, Paketkanal, apt-Cache,
// SSH-Konfiguration, SSH-Port, Benutzer-Sync, firewalld und nftables, fail2ban
// und CrowdSec, Paket-Pins, Neustart, LCM-Host-Aktionen), zurück, ohne je eine Root-Shell
// oder freien Dateisystemzugriff zu erhalten.
//
// Die Regeln für jedes Unterkommando - sie sind die Sicherheitsgrenze:
//   - Kein Unterkommando nimmt ein Skript oder einen frei gewählten Pfad an.
//   - Jeder Parameter ist streng geprüft (valid_*): Aufzählungen, IPs, Ports,
//     URLs, Paketnamen; Freitext nur base64-kodiert.
//   - Geschrieben werden nur Pfade, die LCM verwaltet (apt-, fail2ban-,
//     CrowdSec-, Firewall-Konfiguration).
//   - Der Rumpf ist derselbe wie im Voll-Modus (Platzhalter @@…@@, siehe
//     helperEmbeds): Dort setzt LCM die Variablen, hier der Helper aus den
//     geprüften Parametern.
const lcmHelperPath = "/usr/local/sbin/lcm-helper"

// lcmHelperScript ist das Helper-Skript selbst (POSIX sh, keine Bashismen).
// Freitext-Parameter (apt-Zeile, SSH-Keys, Passwörter) kommen base64-kodiert -
// so gibt es keine Quoting-Fallen über die verschachtelten sudo/sh-Ebenen.
//
// Die Prüfskripte des Deep Scans stehen NICHT doppelt hier drin, sondern
// werden aus deepscan.go eingesetzt: zwei Kopien derselben Prüfungen würden
// auseinanderlaufen, und dann meldete der eingeschränkte Modus etwas anderes
// als der Voll-Modus.
var lcmHelperScript = strings.NewReplacer(
	append(helperEmbeds(), "@@HELPER_VERSION@@", lcmHelperVersion)...).Replace(lcmHelperTemplate)

// helperEmbeds sind die Platzhalter des Helpers und die Skripte, die an ihre
// Stelle kommen (Paare Platzhalter, Skript). Eine Liste für beides: der
// Versionsstand (helperVersionInput) hasht genau das, was eingesetzt wird -
// ein vergessener Eintrag dort ließe eine Änderung am Skript unbemerkt.
func helperEmbeds() []string {
	apt := defaultAptChannelPaths()
	return []string{
		"@@DEEPSCAN_TOOLS@@", deepScanToolsScript(),
		"@@DEEPSCAN_NEEDRESTART@@", needrestartBatchCmd,
		"@@DEEPSCAN_LYNIS@@", lynisRunCmd,
		"@@DEEPSCAN_CURATED@@", curatedChecksScript(),
		"@@USERS_SCAN@@", usersScanScript(),
		"@@NTP_CHRONY_DROPINS@@", chronyDropinStrip,
		"@@DNS_FUNCS@@", dnsShellFuncs,
		"@@HOST_TRIVY@@", trivyInstallScript,
		"@@ACCOUNT_UNLOCK@@", accountUnlockFragment(`"$name"`),
		"@@ACCOUNT_UNEXPIRE@@", accountUnexpireFragment(`"$name"`),
		"@@HOST_SANDBOX@@", sandboxInstallScript,
		"@@HOST_APTCACHER@@", aptCacherInstallScript,
		"@@HOST_APTCACHER_RESTART@@", aptCacherRestartScript,
		"@@HOST_APTCACHER_CACHE_ON@@", aptCacherPermanentCacheScript(true),
		"@@HOST_APTCACHER_CACHE_OFF@@", aptCacherPermanentCacheScript(false),
		"@@HOST_CROWDSEC_BOUNCER@@", crowdsecLapiInstallScript(true),
		"@@HOST_CROWDSEC_PLAIN@@", crowdsecLapiInstallScript(false),
		"@@HOST_SELF_UPDATE@@", selfUpdateScript(false),
		"@@HOST_APT_COMMUNITY@@", communityRevertScriptIn(apt),
		"@@HOST_APT_BETA@@", apt.vars() + betaChannelBody(apt),
		"@@HOST_APT_ENTERPRISE@@", apt.vars() + enterpriseChannelBody(apt),
		"@@PINS_APT@@", pinAptBody,
		"@@PINS_DNF@@", pinDnfBody,
		"@@PINS_ZYPPER@@", pinZypperBody,
		"@@PINS_APK@@", pinApkBody,
		"@@PINS_PACMAN@@", pinPacmanBody,
		"@@FW_NFT_APPLY@@", nftablesApplyBody(),
		"@@FW_NFT_DISABLE@@", nftablesDisableScript(),
		"@@FW_NFT_STATUS@@", nftablesStatusCmd,
		"@@FW_NFT_PORT_ALLOW@@", nftablesAllowPortBody,
		"@@FW_NFT_PORT_REMOVE@@", nftablesRemovePortCmd(0),
		"@@FW_FIREWALLD_APPLY@@", firewalldApplyBody(),
		"@@FW_FIREWALLD_DISABLE@@", firewalldDisableScript(),
		"@@FW_FIREWALLD_STATUS@@", firewalldStatusCmd,
		"@@FW_FIREWALLD_PORT_ALLOW@@", firewalldAllowPortBody,
		"@@FW_FIREWALLD_PORT_REMOVE@@", firewalldRemovePortBody,
		"@@SEC_FAIL2BAN_CONFIG@@", fail2banConfigBody,
		"@@SEC_CROWDSEC_REPO@@", crowdsecRepoBody,
		"@@SEC_CROWDSEC_COLLECTIONS@@", crowdsecCollectionsBody,
		"@@SEC_CROWDSEC_CONFIGURE@@", crowdsecConfigureBody,
		"@@SEC_CROWDSEC_LAPI_URL@@", crowdsecLapiURLBody,
		"@@SEC_SVC_START@@", securityServiceBody(SecurityToolActionStart),
		"@@SEC_SVC_STOP@@", securityServiceBody(SecurityToolActionStop),
		"@@SEC_SVC_RESTART@@", securityServiceBody(SecurityToolActionRestart),
		"@@SEC_SVC_ENABLE@@", securityServiceBody(SecurityToolActionEnable),
		"@@SEC_SVC_DISABLE@@", securityServiceBody(SecurityToolActionDisable),
		"@@SEC_PURGE_FAIL2BAN@@", purgeFail2banBody,
		"@@SEC_PURGE_CROWDSEC@@", purgeCrowdSecBody,
		"@@SEC_UNBAN_FAIL2BAN@@", unbanFail2banBody,
		"@@SEC_UNBAN_CROWDSEC@@", unbanCrowdSecBody,
		"@@SEC_BANS_FAIL2BAN@@", banListFail2banBody,
		"@@SEC_BANS_CROWDSEC@@", banListCrowdSecBody,
		"@@REBOOT@@", rebootScript(),
		"@@HTTPS_BACKUP_DIR@@", httpsBackupDir,
		"@@ROOT_EQUIVALENT_BINARIES@@", strings.Join(domain.RootEquivalentBinaries(), " "),
		"@@ESCAPE_ARGUMENTS@@", strings.Join(domain.EscapeArguments(), " "),
		"@@BLOCKED_RULE_PATHS@@", strings.Join(domain.BlockedRulePaths(), " "),
	}
}

// lcmHelperVersion kennzeichnet den Stand des Helper-Skripts. Bewusst KEINE
// von Hand gepflegte Nummer: sie wird aus dem Skript selbst abgeleitet und
// ändert sich damit bei jeder inhaltlichen Änderung automatisch. Eine
// vergessene Erhöhung wäre schlimmer als keine Version - dann hielte LCM einen
// veralteten Helper für aktuell.
//
// Gehasht wird die Vorlage MIT dem Platzhalter (der Wert steht ja erst danach
// fest) und alle eingesetzten Skripte (helperEmbeds); so bewegt auch eine
// Änderung an den Prüfungen oder an den Skripten der Aktionen die Version.
var lcmHelperVersion = helperVersion()

func helperVersion() string { return shortHash(helperVersionInput()) }

func helperVersionInput() string {
	embeds := helperEmbeds()
	parts := []string{lcmHelperTemplate}
	for i := 1; i < len(embeds); i += 2 {
		parts = append(parts, embeds[i])
	}
	return strings.Join(parts, "\x00")
}

// lcmHelperTemplate ist die Vorlage des Helpers mit den Platzhaltern. Sie
// liegt als eigene Datei neben dem Code, damit shellcheck und Editoren sie
// als Shell-Skript lesen.
//
//go:embed lcm-helper.sh
var lcmHelperTemplate string

// helperCmd baut den Aufruf eines Helper-Unterkommandos für Restricted-Skripte.
// Der Aufruf geht über den PATH-Shim (`lcm-helper` → `sudo -n lcm-helper`);
// sudo löst das Binary über secure_path (/usr/local/sbin) auf. Das erste
// Element ist das feste Unterkommando (unquoted - lesbare SSH-Protokolle),
// alle weiteren Parameter werden shell-quotiert.
func helperCmd(sub string, args ...string) string {
	parts := make([]string, 0, len(args)+2)
	parts = append(parts, "lcm-helper", sub)
	for _, a := range args {
		parts = append(parts, shellQuote(a))
	}
	return strings.Join(parts, " ")
}

// helperCurrentStep steht vor einer Einrichtung, die erst Pakete installiert und
// dann den Helper braucht: Ein veralteter Helper kennt die Unterkommandos nicht
// und ließe den Server halb eingerichtet zurück. Gefragt wird der Helper selbst,
// der im Scan gemerkte Stand hinkt nach einem Update hinterher.
func helperCurrentStep(restricted bool) string {
	if !restricted {
		return ":"
	}
	return helperCmd("version") + " 2>/dev/null | grep -qF " + shellQuote(lcmHelperVersion) +
		" || { echo 'LCM-FEHLER: Der LCM-Helper auf diesem Server ist veraltet, es wurde nichts eingerichtet. Bitte den Server neu verbinden.'; exit 1; }"
}

// parseHelperVersion liest den Stand aus der Ausgabe von `lcm-helper version`.
// Alles andere (leere Ausgabe, „unbekanntes kommando" eines alten Helpers,
// sudo-Fehler) ergibt "" - also „veraltet oder nicht ermittelbar".
func parseHelperVersion(out string) string {
	for _, line := range strings.Split(out, "\n") {
		_, v, ok := strings.Cut(strings.TrimSpace(line), "LCM-HELPER-VERSION:")
		if !ok {
			continue
		}
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}

// Der Domäne den ausgelieferten Helper-Stand bekanntgeben: sie vergleicht ihn
// für die Ampel, kennt das Skript selbst aber nicht (Muster wie ServerRef).
func init() { domain.CurrentHelperVersion = lcmHelperVersion }

// helperB64 kodiert einen Freitext-Parameter für die Helper-Übergabe.
func helperB64(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

// helperUserEnsureCmd baut den Restricted-Ersatz für provisionScript: gleiche
// Wirkung (Account, LCM-Key-Block, sudo-Grant, optional Name/Passwort), aber
// über den validierenden Helper statt einer Root-Shell.
func helperUserEnsureCmd(u *domain.LinuxUser, password string, effect profileEffect) string {
	shell := u.Shell
	if shell == "" {
		shell = "/bin/bash"
	}
	// Kontotyp „nur Dateizugriff": Der Helper löst nologin je Distribution auf.
	if effect.NoShell {
		shell = "nologin"
	}
	var block strings.Builder
	block.WriteString(managedBegin + "\n")
	for _, k := range u.SSHKeys {
		block.WriteString(strings.TrimSpace(k.PublicKey) + "\n")
	}
	block.WriteString(managedEnd + "\n")

	// Der per-Benutzer-Grant kennt nur „volle Rechte oder keine". Eigene
	// Profile wirken über die Profilgruppe (helperProfileMemberCmd).
	sudoFlag := "nosudo"
	if effect.FullRoot {
		sudoFlag = "sudo"
	}
	fullname := "-"
	if u.FullName != "" {
		fullname = helperB64(u.FullName)
	}
	pw := "-"
	if password != "" {
		pw = helperB64(password)
	}
	return helperCmd("user-ensure", u.Username, shell, helperB64(block.String()), sudoFlag, fullname, pw)
}

// sshOptionsApplyCmd liefert das Kommando, das das per-Server-SSH-Drop-in auf
// den Soll-Zustand bringt - im Voll-Modus als Inline-Skript, im
// eingeschränkten Modus über den Helper.
func sshOptionsApplyCmd(server *domain.Server, disableRoot bool, ports []int) string {
	if server.RestrictedSudo {
		var parts []string
		for _, p := range ports {
			if p > 0 {
				parts = append(parts, strconv.Itoa(p))
			}
		}
		csv := "-"
		if len(parts) > 0 {
			csv = strings.Join(parts, ",")
		}
		root := "yes"
		if disableRoot {
			root = "no"
		}
		return helperCmd("ssh-options", csv, root)
	}
	// Auf socket-aktiviertem sshd bestimmt die Socket-Unit den Port, nicht
	// sshd_config - beides muss zusammen gestellt werden, sonst bleibt ein
	// Portwechsel dort wirkungslos.
	return applySSHOptionsScript(sshOptionsBody(disableRoot, ports)) + " && " + applySSHSocketPortsScript(ports)
}

// shortHash ist die Ableitung hinter helperVersion - ausgelagert, damit der
// Test belegen kann, dass eine Inhaltsänderung den Stand bewegt.
func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:12]
}

// helperProfileApplyCmds baut den Restricted-Ersatz für profileApplyScript:
// dieselbe Wirkung (Gruppe, geprüfte sudoers-Datei, Aufräumen), aber über den
// validierenden Helper statt einer Root-Shell.
//
// Der Helper übernimmt den Inhalt NICHT blind - er prüft, dass jede Zeile auf
// die eigene Profilgruppe ausgestellt ist, kein `ALL` in irgendeiner Form und
// kein Root-gleichwertiges Kommando trägt (profile_rule_ok).
// Ohne diese Prüfung könnte ein kompromittiertes LCM dem eingeschränkten
// Service-User über eine Profil-Datei volle Rechte zurückgeben; genau das soll
// der eingeschränkte Modus verhindern.
func helperProfileApplyCmds(wanted []*domain.PrivilegeProfile) []string {
	cmds := make([]string, 0, len(wanted)+1)
	slugs := make([]string, 0, len(wanted))
	for _, profile := range wanted {
		slugs = append(slugs, profile.Slug)
		body := ""
		if profileHasSudoers(profile) {
			body = profileSudoersContent(profile)
		}
		cmds = append(cmds, helperCmd("profile-apply", profile.Slug, helperB64(body)))
	}
	keep := "-"
	if len(slugs) > 0 {
		keep = strings.Join(slugs, ",")
	}
	return append(cmds, helperCmd("profile-prune", keep), helperProfileSFTPCmd(wanted))
}

// helperProfileSFTPCmd bringt das sshd-Drop-in der Profile mit Kontotyp „nur
// Dateizugriff" auf den Soll-Stand (ForceCommand internal-sftp). Ohne solches
// Profil läuft es nur, wenn die Datei liegt: Ein Helper von vor diesem
// Kommando kennt es nicht, und der Abgleich soll auf Servern ohne SFTP-Profil
// nicht daran scheitern.
func helperProfileSFTPCmd(profiles []*domain.PrivilegeProfile) string {
	var slugs []string
	for _, profile := range profiles {
		if profile.AccountType == domain.AccountTypeSFTP {
			slugs = append(slugs, profile.Slug)
		}
	}
	if len(slugs) == 0 {
		return "[ ! -e " + profileSSHDPath + " ] || " + helperCmd("profile-sftp", "-")
	}
	return helperCmd("profile-sftp", strings.Join(slugs, ","))
}

// helperProfileMemberCmd setzt die Profil-Mitgliedschaft eines Kontos über den
// Helper (leerer Slug = in keiner Profilgruppe).
func helperProfileMemberCmd(username, slug string) string {
	if slug == "" {
		slug = "-"
	}
	return helperCmd("profile-member", username, slug)
}
