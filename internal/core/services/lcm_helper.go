package services

import (
	"crypto/sha256"
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
// damit die Kernfunktionen (Repositories, apt-Cache, SSH-Konfiguration,
// SSH-Port, Benutzer-Sync) zurück, ohne je eine Root-Shell oder freien
// Dateisystemzugriff zu erhalten.
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
	"@@DEEPSCAN_TOOLS@@", deepScanToolsScript(),
	"@@DEEPSCAN_NEEDRESTART@@", needrestartBatchCmd,
	"@@DEEPSCAN_LYNIS@@", lynisRunCmd,
	"@@DEEPSCAN_CURATED@@", curatedChecksScript(),
	"@@USERS_SCAN@@", usersScanScript(),
	"@@HTTPS_BACKUP_DIR@@", httpsBackupDir,
	"@@HELPER_VERSION@@", lcmHelperVersion,
).Replace(lcmHelperTemplate)

// lcmHelperVersion kennzeichnet den Stand des Helper-Skripts. Bewusst KEINE
// von Hand gepflegte Nummer: sie wird aus dem Skript selbst abgeleitet und
// ändert sich damit bei jeder inhaltlichen Änderung automatisch. Eine
// vergessene Erhöhung wäre schlimmer als keine Version - dann hielte LCM einen
// veralteten Helper für aktuell.
//
// Gehasht wird die Vorlage MIT dem Platzhalter (der Wert steht ja erst danach
// fest) und OHNE die eingesetzten Deep-Scan-Skripte; letztere fließen über
// helperVersionInput trotzdem ein, damit auch eine Änderung an den Prüfungen
// die Version bewegt.
var lcmHelperVersion = helperVersion()

func helperVersion() string { return shortHash(helperVersionInput()) }

func helperVersionInput() string {
	return lcmHelperTemplate + "\x00" + deepScanToolsScript() + "\x00" +
		needrestartBatchCmd + "\x00" + lynisRunCmd + "\x00" + curatedChecksScript() +
		"\x00" + usersScanScript()
}

const lcmHelperTemplate = `#!/bin/sh
# LCM-Helper - von LCM verwaltet; nicht von Hand bearbeiten.
# Fest umrissene privilegierte Verwaltungsaktionen fuer den eingeschraenkten
# LCM-Management-Benutzer. Jeder Parameter wird streng validiert - keine
# Root-Shell, kein freier Dateisystemzugriff.
set -u

die() { echo "lcm-helper: $*" >&2; exit 1; }

b64dec() { printf '%s' "$1" | base64 -d 2>/dev/null || die "ungueltige base64-daten"; }

valid_slug()  { printf '%s' "$1" | grep -Eq '^[a-z0-9][a-z0-9-]{0,63}$'; }
valid_user()  { printf '%s' "$1" | grep -Eq '^[a-z_][a-z0-9_-]{0,31}$'; }
valid_port()  { printf '%s' "$1" | grep -Eq '^[0-9]{1,5}$' && [ "$1" -ge 1 ] && [ "$1" -le 65535 ]; }
valid_url()   { printf '%s' "$1" | grep -Eq '^https?://[A-Za-z0-9._~:/?#@!$&()*+,;=%-]+$'; }
valid_repo_url(){ printf '%s' "$1" | grep -Eq '^https://[A-Za-z0-9._~:/@%+,=-]+$'; }
valid_shell() { printf '%s' "$1" | grep -Eq '^/[A-Za-z0-9/._-]+$'; }
valid_ip()    { printf '%s' "$1" | grep -Eq '^[0-9A-Fa-f:.]{2,45}$'; }
valid_domain(){ printf '%s' "$1" | grep -Eq '^[A-Za-z0-9._-]{1,253}$'; }

# Der Management-Benutzer selbst und root sind fuer den Benutzer-Sync tabu -
# sonst koennte sich der eingeschraenkte Benutzer via sudo-Grant selbst
# volle Rechte zurueckgeben.
deny_self_and_root() {
    [ "$1" = "root" ] && die "root ist nicht verwaltbar"
    [ -n "${SUDO_USER:-}" ] && [ "$1" = "$SUDO_USER" ] && die "der management-benutzer selbst ist nicht verwaltbar"
    return 0
}

# Neustart ohne festen Init-Dienst: systemd deckt die Mehrheit ab, OpenRC
# (Alpine) und das klassische service-Skript die uebrigen.
# Socket-aktivierter sshd: NICHT immer "pro Verbindung". Debian 13 betreibt
# ssh.socket mit Accept=no - systemd reicht den Socket an EINEN dauerhaften
# ssh.service weiter. Ein Skip liess dort jede Konfigurationsaenderung
# wirkungslos im alten Daemon liegen; ein Reload scheitert am systemd-eigenen
# Socket (zweiter sshd, failed, /run/sshd weg). Deshalb: laeuft neben dem
# Socket ein aktiver Dienst, DIESEN neu starten (Sitzungen ueberleben dank
# KillMode=process). Nur ohne aktiven Dienst (Accept=yes, sshd je Verbindung)
# ist nichts zu tun - jede neue Verbindung liest die Konfiguration frisch.
sshd_socket_active() { systemctl is-active --quiet ssh.socket 2>/dev/null || systemctl is-active --quiet sshd.socket 2>/dev/null; }
sshd_reload() { sshd -t && { if sshd_socket_active; then if systemctl is-active --quiet ssh.service 2>/dev/null; then systemctl restart ssh; elif systemctl is-active --quiet sshd.service 2>/dev/null; then systemctl restart sshd; fi; else systemctl reload sshd 2>/dev/null || systemctl reload ssh 2>/dev/null || rc-service sshd reload 2>/dev/null || rc-service sshd restart 2>/dev/null || service ssh reload 2>/dev/null || service sshd reload; fi; }; }

# sshd_ensure_include stellt sicher, dass sshd_config das Drop-in-Verzeichnis
# ueberhaupt einliest - openSUSE liefert keine Include-Zeile mit, dort blieb
# jedes Drop-in wirkungslos. Die Zeile muss GANZ OBEN stehen: OpenSSH nimmt
# je Option den ZUERST gefundenen Wert.
# Die Hauptdatei wird nicht unter /etc/ssh vorausgesetzt: openSUSE Leap 16
# hat ein stateless /etc und liefert sie unter /usr/etc/ssh/sshd_config
# (R2-015). Eine Ergaenzung entsteht immer unter /etc - so ueberschreibt sie
# die Vorgabe, statt sie zu veraendern.
sshd_conf_path() {
    [ -f /etc/ssh/sshd_config ] && { echo /etc/ssh/sshd_config; return 0; }
    [ -f /usr/etc/ssh/sshd_config ] && { echo /usr/etc/ssh/sshd_config; return 0; }
    return 1
}

sshd_ensure_include() {
    conf=$(sshd_conf_path) || die "sshd_config weder unter /etc/ssh noch unter /usr/etc/ssh gefunden"
    grep -qiE '^[[:space:]]*Include[[:space:]]+/etc/ssh/sshd_config\.d/' "$conf" && return 0
    cp -f "$conf" /etc/ssh/sshd_config.lcm-backup || return 1
    { echo '# von LCM ergaenzt: Drop-ins einlesen (muss oben stehen)'
      echo 'Include /etc/ssh/sshd_config.d/*.conf'
      cat /etc/ssh/sshd_config.lcm-backup; } > /etc/ssh/sshd_config
}

# sshd_password_auth_off prueft die EFFEKTIVE Konfiguration (sshd -T wertet
# Includes und Match-Bloecke aus). Eine geschriebene Datei ist kein Beleg
# dafuer, dass sie auch gelesen wird.
# mkdir -p /run/sshd: bei socket-aktiviertem sshd (Debian 13) existiert das
# Verzeichnis nur waehrend einer Verbindung, sonst bricht 'sshd -T' mit
# "Missing privilege separation directory" ab und die Pruefung liefe leer
# ins Nichts (R2-014). Ein leeres Ergebnis gilt jetzt als NICHT belegt.
# Rueckgabe: 0 = nachweislich aus, 1 = nachweislich an, 2 = nicht belegbar.
sshd_password_auth_off() {
    mkdir -p /run/sshd 2>/dev/null
    val=$(sshd -T 2>/dev/null | grep -i '^passwordauthentication' | head -n1)
    [ -z "$val" ] && return 2   # unbelegt zaehlt nicht als Erfolg
    case "$val" in *" no") return 0 ;; *) return 1 ;; esac
}

# repo-add <key> <keyurl_b64> <line_b64>
# GPG-Key nach /etc/apt/keyrings/<key>.asc, Zeile nach
# /etc/apt/sources.list.d/lcm-<key>.list, danach apt-Update.
repo_add() {
    key="$1"
    valid_slug "$key" || die "ungueltiger repo-key"
    key_url=$(b64dec "$2")
    line=$(b64dec "$3" | head -n1)
    valid_url "$key_url" || die "ungueltige key-url"
    case "$line" in
        deb\ *) ;;
        *) die "ungueltige apt-zeile (muss mit 'deb ' beginnen)" ;;
    esac
    . /etc/os-release
    CODENAME="${VERSION_CODENAME:-}"
    ARCH=$(dpkg --print-architecture)
    key_url=$(printf '%s' "$key_url" | sed -e "s|\$ID|$ID|g" -e "s|\$CODENAME|$CODENAME|g" -e "s|\$ARCH|$ARCH|g")
    line=$(printf '%s' "$line" | sed -e "s|\$ID|$ID|g" -e "s|\$CODENAME|$CODENAME|g" -e "s|\$ARCH|$ARCH|g")
    keyring="/etc/apt/keyrings/$key.asc"
    list="/etc/apt/sources.list.d/lcm-$key.list"
    install -d -m 755 /etc/apt/keyrings
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL "$key_url" -o "$keyring" || die "gpg-key-download fehlgeschlagen"
    else
        wget -qO "$keyring" "$key_url" || die "gpg-key-download fehlgeschlagen"
    fi
    chmod 644 "$keyring"
    printf '%s\n' "$line" > "$list"
    cat "$list"
    apt-get update
}

# repos-https: alle http://-apt-Quellen auf https umstellen (mit Sicherung
# und Rollback, falls apt-update danach fehlschlaegt).
repos_https() {
    apt-get install -y --no-install-recommends ca-certificates >/dev/null 2>&1 || true
    changed=0
    for f in /etc/apt/sources.list /etc/apt/sources.list.d/*.list /etc/apt/sources.list.d/*.sources; do
        [ -f "$f" ] || continue
        grep -q 'http://' "$f" || continue
        cp "$f" "$f.lcm-bak"
        sed -i 's|http://|https://|g' "$f"
        changed=1
    done
    if [ "$changed" -eq 0 ]; then echo 'LCM: keine http-quellen gefunden - nichts zu tun'; return 0; fi
    if apt-get update; then
        for f in $(find /etc/apt -name '*.lcm-bak'); do
            orig="${f%.lcm-bak}"; dest="@@HTTPS_BACKUP_DIR@@$orig"
            if [ -f "$dest" ]; then rm -f "$f"; else mkdir -p "$(dirname "$dest")"; mv "$f" "$dest"; fi
        done
        echo 'LCM: alle paketquellen auf https umgestellt'
    else
        for f in $(find /etc/apt -name '*.lcm-bak'); do mv "$f" "${f%.lcm-bak}"; done
        echo 'LCM: apt-update nach der umstellung fehlgeschlagen - alle aenderungen zurueckgerollt'
        exit 1
    fi
}

# repos-http <url>...: die genannten Paketquellen von https auf http
# zurueckstellen (mit Sicherung und Rollback, falls apt-update fehlschlaegt).
# Zurueck darf nur, was LCM selbst umgestellt hat - welche URLs das sind,
# entscheidet der LCM-Server anhand seines Protokolls; hier wird geprueft,
# dass es ueberhaupt unbedenkliche URLs sind.
repos_http() {
    for u in "$@"; do valid_repo_url "$u" || die "ungueltige paketquellen-url: $u"; done
    URLS="$*"
    changed=0
    for f in /etc/apt/sources.list /etc/apt/sources.list.d/*.list /etc/apt/sources.list.d/*.sources; do
        [ -f "$f" ] || continue
        hit=0
        for u in $URLS; do grep -qF "$u" "$f" && hit=1; done
        [ "$hit" -eq 1 ] || continue
        cp "$f" "$f.lcm-bak"
        for u in $URLS; do sed -i "s|$u|http://${u#https://}|g" "$f"; done
        changed=1
    done
    if [ "$changed" -eq 0 ]; then echo 'LCM: keine der genannten quellen gefunden - nichts zu tun'; return 0; fi
    if apt-get update; then
        find /etc/apt -name '*.lcm-bak' -delete
        echo 'LCM: paketquellen auf http zurueckgestellt'
    else
        for f in $(find /etc/apt -name '*.lcm-bak'); do mv "$f" "${f%.lcm-bak}"; done
        echo 'LCM: apt-update nach der rueckstellung fehlgeschlagen - alle aenderungen zurueckgerollt'
        exit 1
    fi
}

# apt-proxy <url>|off: das von LCM verwaltete Proxy-Drop-in schreiben (mit
# apt-update-Probe und Rollback) oder entfernen.
apt_proxy() {
    dropin=/etc/apt/apt.conf.d/02lcm-apt-cache
    if [ "$1" = "off" ]; then
        rm -f "$dropin"
        echo 'LCM: apt-cache-anbindung entfernt'
        return 0
    fi
    valid_url "$1" || die "ungueltige cache-url"
    printf 'Acquire::http::Proxy "%s";\nAcquire::https::Proxy "%s";\n' "$1" "$1" > "$dropin"
    out=$(apt-get update 2>&1)
    mode='HTTP und HTTPS'
    # apt-cacher-ng tunnelt HTTPS nur mit gesetztem PassThroughPattern; sonst
    # antwortet er mit 403 und JEDE HTTPS-Paketquelle faellt aus (R2-038).
    # Dann HTTPS am Proxy vorbei - lieber ungecacht als unerreichbar.
    if printf '%s' "$out" | grep -qiE 'CONNECT denied|Invalid response from proxy'; then
        printf 'Acquire::http::Proxy "%s";\nAcquire::https::Proxy "DIRECT";\n' "$1" > "$dropin"
        out=$(apt-get update 2>&1)
        mode='nur HTTP - der Cache erlaubt keine HTTPS-Tunnel, HTTPS-Quellen laufen ungecacht direkt'
    fi
    # apt-get update endet auch bei kaputten Quellen mit 0 - deshalb zaehlt
    # die Ausgabe, nicht der Exit-Code.
    if printf '%s' "$out" | grep -qE '^(Err|E):'; then
        rm -f "$dropin"
        apt-get update >/dev/null 2>&1 || true
        printf '%s\n' "$out" | grep -E '^(Err|E|W): ' | head -n 10
        echo 'LCM: apt-update ueber den cache meldete fehlerhafte paketquellen - drop-in wieder entfernt'
        exit 1
    fi
    echo "LCM: apt-anfragen laufen jetzt ueber den cache $1 ($mode)"
}

# timezone <zone>: die Zeitzone setzen und ZURUECKLESEN. Eine geschriebene
# Datei ist kein Beleg: ohne systemd greift timedatectl nicht, und ein blosses
# Ueberschreiben von /etc/timezone bleibt ohne passenden localtime-Symlink
# wirkungslos.
valid_tz() { printf '%s' "$1" | grep -Eq '^[A-Za-z][A-Za-z0-9+_-]*(/[A-Za-z0-9+._-]+){0,2}$'; }

set_timezone() {
    valid_tz "$1" || die "ungueltige zeitzone"
    if command -v timedatectl >/dev/null 2>&1 && timedatectl set-timezone "$1" 2>/dev/null; then
        :
    else
        [ -f "/usr/share/zoneinfo/$1" ] || die "zeitzone '$1' nicht vorhanden (paket tzdata fehlt?)"
        ln -sf "/usr/share/zoneinfo/$1" /etc/localtime
        printf '%s\n' "$1" > /etc/timezone 2>/dev/null || true
    fi
    now=$(timedatectl show -p Timezone --value 2>/dev/null)
    [ -n "$now" ] || now=$(readlink -f /etc/localtime 2>/dev/null | sed 's#.*/zoneinfo/##')
    [ "$now" = "$1" ] || die "zeitzone geschrieben, das system meldet aber weiterhin '$now'"
    echo "LCM: Zeitzone gesetzt und geprueft: $now ($(date))"
}

# ntp <servers-csv>: Zeitserver eintragen, Dienst starten und die
# Synchronisierung BELEGEN. "Dienst gestartet" heisst nicht "Uhr geht richtig".
set_ntp() {
    srv=$(printf '%s' "$1" | tr ',' ' ')
    [ -n "$srv" ] || die "keine zeitserver angegeben"
    for s in $srv; do
        printf '%s' "$s" | grep -Eq '^[A-Za-z0-9]([A-Za-z0-9._:-]{0,251}[A-Za-z0-9])?$' || die "ungueltiger zeitserver: $s"
    done
    synced=no; svc=""
    if command -v chronyc >/dev/null 2>&1; then
        svc=chrony
        conf=/etc/chrony/chrony.conf; [ -f "$conf" ] || conf=/etc/chrony.conf
        [ -f "$conf" ] && { [ -f "$conf.lcm-bak" ] || cp -f "$conf" "$conf.lcm-bak"; }
        { grep -vE '^[ \t]*(server|pool)[ \t]' "$conf" 2>/dev/null
          for s in $srv; do echo "server $s iburst"; done; } > "$conf.lcm-new" && mv "$conf.lcm-new" "$conf"
        systemctl restart chronyd 2>/dev/null || systemctl restart chrony 2>/dev/null || rc-service chronyd restart 2>/dev/null || true
        chronyc -n makestep >/dev/null 2>&1 || true
        i=0; while [ $i -lt 10 ]; do
            chronyc -n tracking 2>/dev/null | grep -qiE '^Leap status +: +Normal' && { synced=yes; break; }
            sleep 2; i=$((i+1))
        done
    elif command -v timedatectl >/dev/null 2>&1 && [ -d /etc/systemd ]; then
        svc=systemd-timesyncd
        install -d -m 755 /etc/systemd/timesyncd.conf.d
        printf '[Time]\nNTP=%s\n' "$srv" > /etc/systemd/timesyncd.conf.d/lcm-ntp.conf
        timedatectl set-ntp true 2>/dev/null || true
        systemctl restart systemd-timesyncd 2>/dev/null || true
        i=0; while [ $i -lt 10 ]; do
            [ "$(timedatectl show -p NTPSynchronized --value 2>/dev/null)" = "yes" ] && { synced=yes; break; }
            sleep 2; i=$((i+1))
        done
    elif [ -f /etc/ntp.conf ]; then
        svc=ntpd
        [ -f /etc/ntp.conf.lcm-bak ] || cp -f /etc/ntp.conf /etc/ntp.conf.lcm-bak
        { grep -vE '^[ \t]*(server|pool)[ \t]' /etc/ntp.conf
          for s in $srv; do echo "server $s iburst"; done; } > /etc/ntp.conf.lcm-new && mv /etc/ntp.conf.lcm-new /etc/ntp.conf
        systemctl restart ntpd 2>/dev/null || rc-service ntpd restart 2>/dev/null || true
        i=0; while [ $i -lt 5 ]; do
            ntpq -pn 2>/dev/null | grep -q '^\*' && { synced=yes; break; }
            sleep 2; i=$((i+1))
        done
    else
        die "kein unterstuetzter zeitdienst gefunden (chrony, systemd-timesyncd oder ntpd installieren)"
    fi
    echo "LCM: Zeitserver gesetzt ($svc): $srv"
    if [ "$synced" = yes ]; then
        echo "LCM: Uhr ist synchronisiert - $(date -u '+%Y-%m-%d %H:%M:%S UTC')"
    else
        echo "LCM: Zeitserver eingetragen, eine Synchronisierung war im Zeitfenster aber nicht nachweisbar. Die Konfiguration bleibt bestehen; Erreichbarkeit der Zeitserver pruefen." >&2
        exit 2
    fi
}

# dns <ips-csv|off> <testdomain>: bis zu drei Nameserver setzen
# (systemd-resolved-Drop-in wenn aktiv, sonst /etc/resolv.conf) mit
# Aufloesungstest + Rollback, oder die LCM-DNS-Verwaltung wieder entfernen.
dns() {
    ips="$1"; testdom="$2"
    dropin=/etc/systemd/resolved.conf.d/lcm-dns.conf
    resolv=/etc/resolv.conf
    valid_domain "$testdom" || die "ungueltige test-domain"
    dns_resolve_ok() { getent hosts "$1" >/dev/null 2>&1 || nslookup "$1" >/dev/null 2>&1; }
    if [ "$ips" = "off" ]; then
        if command -v resolvectl >/dev/null 2>&1 && systemctl is-active --quiet systemd-resolved 2>/dev/null; then
            rm -f "$dropin" "$dropin.lcm-bak"; systemctl restart systemd-resolved 2>/dev/null || true
            echo 'LCM: DNS-Verwaltung entfernt (systemd-resolved)'
        else
            [ -e "$resolv.lcm-bak" ] && mv -f "$resolv.lcm-bak" "$resolv"
            echo 'LCM: DNS-Verwaltung entfernt (/etc/resolv.conf)'
        fi
        return 0
    fi
    space=""; nslines=""
    old_ifs="$IFS"; IFS=','
    for ip in $ips; do
        valid_ip "$ip" || die "ungueltige dns-ip: $ip"
        space="${space}${ip} "
        nslines="${nslines}nameserver ${ip}
"
    done
    IFS="$old_ifs"
    if command -v resolvectl >/dev/null 2>&1 && systemctl is-active --quiet systemd-resolved 2>/dev/null; then
        install -d -m 755 /etc/systemd/resolved.conf.d
        [ -f "$dropin" ] && cp -f "$dropin" "$dropin.lcm-bak"
        printf '[Resolve]\nDNS=%s\n' "$space" > "$dropin"
        systemctl restart systemd-resolved 2>/dev/null || true
        if dns_resolve_ok "$testdom"; then echo "LCM: DNS gesetzt (systemd-resolved): $space"
        else rm -f "$dropin"; [ -f "$dropin.lcm-bak" ] && mv "$dropin.lcm-bak" "$dropin"; systemctl restart systemd-resolved 2>/dev/null || true; die "DNS-Test nach dem Setzen fehlgeschlagen - zurueckgerollt"; fi
    else
        [ -e "$resolv.lcm-bak" ] || cp -f "$resolv" "$resolv.lcm-bak" 2>/dev/null || true
        [ -L "$resolv" ] && rm -f "$resolv"
        printf '%s' "$nslines" > "$resolv"
        if dns_resolve_ok "$testdom"; then echo "LCM: DNS gesetzt (/etc/resolv.conf): $space"
        else [ -e "$resolv.lcm-bak" ] && cp -f "$resolv.lcm-bak" "$resolv"; die "DNS-Test nach dem Setzen fehlgeschlagen - zurueckgerollt"; fi
    fi
}

# ssh-harden on|off: das LCM-Haertungs-Drop-in schreiben/entfernen und sshd
# neu laden (bei Fehlschlag zurueckrollen).
ssh_harden() {
    dropin=/etc/ssh/sshd_config.d/60-lcm-hardening.conf
    case "$1" in
        on)
            sshd_ensure_include || die "sshd_config konnte nicht um die Include-Zeile ergaenzt werden"
            install -d -m 755 /etc/ssh/sshd_config.d
            printf 'PasswordAuthentication no\nChallengeResponseAuthentication no\nPubkeyAuthentication yes\nPermitRootLogin prohibit-password\n' > "$dropin"
            if ! sshd_reload; then
                rm -f "$dropin"; sshd_reload
                die "sshd-haertung fehlgeschlagen - zurueckgerollt"
            fi
            # Nachweis statt Annahme: wirkt die Haertung nicht, ist eine
            # Erfolgsmeldung schaedlicher als ein ehrlicher Fehlschlag.
            sshd_password_auth_off; rc=$?
            if [ "$rc" -eq 1 ]; then
                rm -f "$dropin"; sshd_reload
                die "sshd meldet weiterhin passwordauthentication yes - haertung wirkungslos, zurueckgerollt"
            fi
            # Unbelegt ist kein Erfolg (R2-014). Das Drop-in bleibt liegen -
            # ein womoeglich wirksamer Schutz wird nicht wegen einer
            # fehlgeschlagenen Messung abgeraeumt -, aber gemeldet wird der
            # Zweifel, nicht ein Erfolg.
            if [ "$rc" -eq 2 ]; then
                die "haertung geschrieben, aber nicht belegbar: sshd -T liefert keinen wert fuer passwordauthentication"
            fi
            ;;
        off)
            rm -f "$dropin"
            sshd_reload || die "sshd-reload fehlgeschlagen"
            ;;
        *) die "ssh-harden on|off" ;;
    esac
}

# ssh-options <ports-csv|-> <root:yes|no>: das per-Server-Drop-in
# (10-lcm-ssh.conf) aus validierten Werten neu aufbauen - Ports != 22 und
# optional PermitRootLogin no. Leerer Soll-Zustand entfernt das Drop-in.
# Mit Sicherung + Rollback, falls sshd die neue Konfiguration ablehnt.
ssh_options() {
    ports="$1"; root="$2"
    path=/etc/ssh/sshd_config.d/10-lcm-ssh.conf
    bak="$path.lcmbak"
    body=""
    if [ -n "$ports" ] && [ "$ports" != "-" ]; then
        old_ifs="${IFS}"; IFS=','
        for p in $ports; do
            valid_port "$p" || die "ungueltiger port: $p"
            if [ "$p" -ne 22 ]; then body="${body}Port $p
"; fi
        done
        IFS="${old_ifs}"
    fi
    case "$root" in
        no) body="${body}PermitRootLogin no
" ;;
        yes) ;;
        *) die "ssh-options: root muss yes|no sein" ;;
    esac
    cp -a "$path" "$bak" 2>/dev/null || true
    if [ -z "$body" ]; then
        rm -f "$path"
    else
        install -d -m 755 /etc/ssh/sshd_config.d
        {
            printf '%s\n' '# LCM - per-Server SSH-Optionen (von LCM verwaltet, nicht von Hand aendern).'
            printf '%s' "$body"
        } > "$path"
    fi
    if sshd_reload; then
        rm -f "$bak"
    else
        mv "$bak" "$path" 2>/dev/null || rm -f "$path"
        sshd_reload
        die "sshd-konfiguration fehlgeschlagen - zurueckgerollt"
    fi
    ssh_socket_ports "$ports"
}

# ssh_socket_ports <ports-csv|->: Bei socket-aktiviertem sshd (Debian 13,
# neuere Ubuntu) lauscht ssh.socket, nicht der sshd - "Port" in sshd_config
# wird dort nie gelesen. Der Portwechsel geht deshalb ueber ein Drop-in der
# Socket-Unit. Nur Port 22 = Vorgabe der Distribution, dann faellt es weg.
# Der Neustart der Socket-Unit trennt bestehende Sitzungen nicht.
ssh_socket_ports() {
    ports="$1"
    u=""
    for c in ssh.socket sshd.socket; do
        systemctl is-active --quiet "$c" 2>/dev/null && { u="$c"; break; }
    done
    [ -n "$u" ] || return 0
    want=""
    if [ -n "$ports" ] && [ "$ports" != "-" ] && [ "$ports" != "22" ]; then
        want="# LCM - SSH-Port der Socket-Unit (von LCM verwaltet).
[Socket]
ListenStream=
"
        old_ifs="${IFS}"; IFS=','
        for p in $ports; do
            valid_port "$p" || die "ungueltiger port: $p"
            want="${want}ListenStream=$p
"
        done
        IFS="${old_ifs}"
    fi
    d=/etc/systemd/system/$u.d
    f=$d/10-lcm-port.conf
    if [ -z "$want" ]; then
        [ -e "$f" ] || return 0
        rm -f "$f"
    else
        [ "$(cat "$f" 2>/dev/null)" = "$want" ] && return 0
        install -d -m 755 "$d"
        printf '%s' "$want" > "$f"
    fi
    systemctl daemon-reload && systemctl restart "$u" || die "socket-unit $u konnte nicht neu gestartet werden"
}

# user-ensure <name> <shell> <keys_b64> <sudo|nosudo> [fullname_b64|-] [password_b64|-]
# Idempotent: Benutzer anlegen/aktualisieren, LCM-Key-Block in die
# authorized_keys schreiben, sudo-Grant setzen/entfernen.
user_ensure() {
    name="$1"; shell="$2"; keys_b64="$3"; sudo_flag="$4"
    fullname_b64="${5:--}"; password_b64="${6:--}"
    valid_user "$name" || die "ungueltiger benutzername"
    deny_self_and_root "$name"
    valid_shell "$shell" || die "ungueltige shell"
    keys=$(b64dec "$keys_b64")
    # Werkzeug nach Verfuegbarkeit (useradd fehlt auf BusyBox) und die
    # Passwortsperre aufheben - bei "UsePAM no" lehnt OpenSSH sonst auch den
    # Key-Login ab, und das provisionierte Konto waere unbrauchbar.
    if ! id -u "$name" >/dev/null 2>&1; then
        if command -v useradd >/dev/null 2>&1; then useradd -m -s "$shell" "$name"
        elif command -v adduser >/dev/null 2>&1; then adduser -D -h "/home/$name" -s "$shell" "$name"
        else die "weder useradd noch adduser vorhanden"
        fi
    fi
    if command -v usermod >/dev/null 2>&1; then usermod -p '*' "$name" >/dev/null 2>&1 || true
    elif command -v passwd >/dev/null 2>&1; then passwd -u "$name" >/dev/null 2>&1 || true
    fi
    usermod -s "$shell" "$name" 2>/dev/null || true
    home=$(getent passwd "$name" | cut -d: -f6)
    [ -n "$home" ] || die "kein home-verzeichnis fuer $name"
    install -d -m 700 -o "$name" -g "$name" "$home/.ssh"
    touch "$home/.ssh/authorized_keys"
    sed -i '/# >>> LCM managed keys >>>/,/# <<< LCM managed keys <<</d' "$home/.ssh/authorized_keys"
    printf '%s' "$keys" >> "$home/.ssh/authorized_keys"
    chown -R "$name:$name" "$home/.ssh"
    chmod 600 "$home/.ssh/authorized_keys"
    if [ "$fullname_b64" != "-" ]; then
        chfn -f "$(b64dec "$fullname_b64")" "$name" 2>/dev/null || true
    fi
    if [ "$password_b64" != "-" ]; then
        printf '%s:%s' "$name" "$(b64dec "$password_b64")" | chpasswd
        usermod -U "$name" 2>/dev/null || true
    fi
    if [ "$sudo_flag" = "sudo" ]; then
        printf '%s ALL=(ALL) NOPASSWD:ALL\n' "$name" > "/etc/sudoers.d/lcm-$name"
        chmod 440 "/etc/sudoers.d/lcm-$name"
    else
        rm -f "/etc/sudoers.d/lcm-$name"
    fi
    echo "lcm-helper: benutzer $name provisioniert"
}

# profile-apply <slug> <sudoers_b64>: Berechtigungsprofil einrichten - Gruppe
# lcm-prof-<slug> anlegen und die sudoers-Datei schreiben.
#
# Der Inhalt kommt base64-kodiert von LCM, wird hier aber NICHT blind
# uebernommen: Jede Zeile muss auf die eigene Gruppe ausgestellt sein und darf
# kein ALL als Kommando tragen. Ohne diese Pruefung koennte ein
# kompromittiertes LCM dem eingeschraenkten Service-User ueber eine
# Profil-Datei volle Rechte zurueckgeben - genau das, was der eingeschraenkte
# Modus verhindern soll.
profile_apply() {
    slug="$1"; body_b64="$2"
    valid_slug "$slug" || die "ungueltiger profil-slug"
    group="lcm-prof-$slug"
    path="/etc/sudoers.d/$group"
    body=$(b64dec "$body_b64")
    printf '%s\n' "$body" | grep -v '^#' | grep -v '^[[:space:]]*$' | while IFS= read -r line; do
        printf '%s' "$line" | grep -q "^%$group " || { echo "fremde gruppe in profil-regel: $line" >&2; exit 1; }
        printf '%s' "$line" | grep -Eq '(NOPASSWD|PASSWD):[[:space:]]*ALL[[:space:]]*$' && { echo "uneingeschraenkte regel abgelehnt: $line" >&2; exit 1; }
    done || die "profil-regeln abgelehnt"
    getent group "$group" >/dev/null 2>&1 || {
        if command -v groupadd >/dev/null 2>&1; then groupadd "$group"
        elif command -v addgroup >/dev/null 2>&1; then addgroup "$group"
        else die "weder groupadd noch addgroup vorhanden"
        fi
    }
    if [ -z "$body" ]; then
        rm -f "$path"
    else
        printf '%s\n' "$body" > "$path.tmp"
        chmod 440 "$path.tmp"
        visudo -cf "$path.tmp" >/dev/null || { rm -f "$path.tmp"; die "sudoers-pruefung fehlgeschlagen"; }
        mv "$path.tmp" "$path"
    fi
    echo "lcm-helper: profil $slug eingerichtet"
}

# profile-member <name> <slug|->: Konto in GENAU EINE Profilgruppe setzen.
profile_member() {
    name="$1"; slug="$2"
    valid_user "$name" || die "ungueltiger benutzername"
    deny_self_and_root "$name"
    want=""
    if [ "$slug" != "-" ]; then
        valid_slug "$slug" || die "ungueltiger profil-slug"
        want="lcm-prof-$slug"
    fi
    for g in $(id -nG "$name" 2>/dev/null); do
        case "$g" in
            lcm-prof-*)
                [ "$g" = "$want" ] || gpasswd -d "$name" "$g" >/dev/null 2>&1 || deluser "$name" "$g" >/dev/null 2>&1 || true
                ;;
        esac
    done
    [ -n "$want" ] && { usermod -aG "$want" "$name" 2>/dev/null || addgroup "$name" "$want" 2>/dev/null || true; }
    echo "lcm-helper: profil-mitgliedschaft von $name gesetzt"
}

# profile-prune <slugs-csv|->: sudoers-Dateien und Gruppen nicht mehr
# gebrauchter Profile entfernen. Eine Gruppe mit Mitgliedern bleibt stehen -
# fremde Zuordnungen fasst LCM nicht an.
profile_prune() {
    keep=" "
    if [ "${1:--}" != "-" ]; then
        old_ifs="${IFS}"; IFS=','
        for s in $1; do valid_slug "$s" || die "ungueltiger profil-slug"; keep="${keep}lcm-prof-$s "; done
        IFS="${old_ifs}"
    fi
    for f in /etc/sudoers.d/lcm-prof-*; do
        [ -e "$f" ] || continue
        n=$(basename "$f")
        case "$keep" in *" $n "*) continue;; esac
        rm -f "$f"
    done
    for g in $(getent group | cut -d: -f1 | grep '^lcm-prof-' || true); do
        case "$keep" in *" $g "*) continue;; esac
        [ -z "$(getent group "$g" | cut -d: -f4)" ] || continue
        groupdel "$g" >/dev/null 2>&1 || delgroup "$g" >/dev/null 2>&1 || true
    done
    echo "lcm-helper: profile aufgeraeumt"
}

# user-remove <name>: Benutzer samt Home und sudo-Grant entfernen (idempotent).
# Distributionsbewusst (BusyBox hat kein userdel, nur deluser) und mit
# Nachweis: besteht das Konto danach weiter, ist das ein FEHLER - kein
# stilles "entfernt" bei bestehendem Zugang (R2-040).
user_remove() {
    name="$1"
    valid_user "$name" || die "ungueltiger benutzername"
    deny_self_and_root "$name"
    if id -u "$name" >/dev/null 2>&1; then
        if command -v userdel >/dev/null 2>&1; then userdel -r "$name"
        elif command -v deluser >/dev/null 2>&1; then deluser --remove-home "$name"
        else die "weder userdel noch deluser vorhanden"
        fi
    fi
    id -u "$name" >/dev/null 2>&1 && die "konto $name besteht weiterhin - entfernen fehlgeschlagen"
    rm -f "/etc/sudoers.d/lcm-$name"
    echo "lcm-helper: benutzer $name entfernt"
}

# user-lock <name>: deaktivierten Benutzer sperren - Passwort gesperrt,
# LCM-Schluesselblock raus, sudo-Grant weg. Konto und Home bleiben
# (Deaktivierung ist umkehrbar; endgueltig ist user-remove). R2-039.
user_lock() {
    name="$1"
    valid_user "$name" || die "ungueltiger benutzername"
    deny_self_and_root "$name"
    if id -u "$name" >/dev/null 2>&1; then
        usermod -L "$name" 2>/dev/null || passwd -l "$name" 2>/dev/null || true
        ak="/home/$name/.ssh/authorized_keys"
        if [ -f "$ak" ]; then
            sed -i '/# >>> LCM managed keys >>>/,/# <<< LCM managed keys <<</d' "$ak"
        fi
        rm -f "/etc/sudoers.d/lcm-$name"
    fi
    echo "lcm-helper: benutzer $name gesperrt"
}

# user-disable <name>: beliebiges Konto VOLL sperren - Passwort gesperrt UND
# Ablaufdatum gesetzt. Das Ablaufdatum ist der entscheidende Teil: usermod -L
# sperrt nur das Passwort, SSH-Key-Logins gehen weiter durch; erst ein
# abgelaufenes Konto blockiert jeden Login. (Gegenstueck zur Benutzer-
# Uebersicht; fuer LCM-verwaltete Konten bleibt user-lock der Weg.)
user_disable() {
    name="$1"
    valid_user "$name" || die "ungueltiger benutzername"
    deny_self_and_root "$name"
    id -u "$name" >/dev/null 2>&1 || die "unbekanntes konto: $name"
    usermod -L "$name" 2>/dev/null || passwd -l "$name" 2>/dev/null || true
    usermod -e 1970-01-02 "$name" 2>/dev/null || chage -E 1 "$name" 2>/dev/null || true
    echo "lcm-helper: benutzer $name deaktiviert"
}

# user-enable <name>: Gegenstueck zu user-disable - Passwortsperre und
# Ablaufdatum aufheben.
user_enable() {
    name="$1"
    valid_user "$name" || die "ungueltiger benutzername"
    deny_self_and_root "$name"
    id -u "$name" >/dev/null 2>&1 || die "unbekanntes konto: $name"
    usermod -U "$name" 2>/dev/null || passwd -u "$name" 2>/dev/null || true
    usermod -e '' "$name" 2>/dev/null || chage -E -1 "$name" 2>/dev/null || true
    echo "lcm-helper: benutzer $name aktiviert"
}

# users-scan: die anmeldefaehigen Linux-Konten erfassen (Name, UID, Shell,
# Passwort-Status, Keys, 2FA, Ablauf, letzter Login). Rein lesend, keine
# Parameter - das Skript kommt aus users_scan.go (eine Quelle fuer beide Modi).
users_scan() {
@@USERS_SCAN@@
}

# deep-scan <teil>: die vier Leseschritte des Deep Scans als Root. Rein
# lesend - kein Parameter fliesst in ein Kommando ein, der Teil ist eine feste
# Auswahl. Ohne diesen Weg lief der Deep Scan im eingeschraenkten Modus als
# unprivilegierter Dienstbenutzer ins Leere und meldete "nichts gefunden".
deep_scan() {
    case "$1" in
        tools)       @@DEEPSCAN_TOOLS@@ ;;
        needrestart) @@DEEPSCAN_NEEDRESTART@@ ;;
        lynis)       @@DEEPSCAN_LYNIS@@ ;;
        curated)     @@DEEPSCAN_CURATED@@ ;;
        *) die "deep-scan tools|needrestart|lynis|curated" ;;
    esac
}

cmd="${1:-}"
[ $# -ge 1 ] && shift
case "$cmd" in
    repo-add)    [ $# -eq 3 ] || die "repo-add <key> <keyurl_b64> <line_b64>"; repo_add "$@" ;;
    repos-https) repos_https ;;
    repos-http)  [ $# -ge 1 ] || die "repos-http <https-url>..."; repos_http "$@" ;;
    apt-proxy)   [ $# -eq 1 ] || die "apt-proxy <url>|off"; apt_proxy "$@" ;;
    dns)         [ $# -eq 2 ] || die "dns <ips-csv|off> <testdomain>"; dns "$@" ;;
    ssh-harden)  [ $# -eq 1 ] || die "ssh-harden on|off"; ssh_harden "$@" ;;
    ssh-options) [ $# -eq 2 ] || die "ssh-options <ports-csv|-> <root:yes|no>"; ssh_options "$@" ;;
    user-ensure) [ $# -ge 4 ] && [ $# -le 6 ] || die "user-ensure <name> <shell> <keys_b64> <sudo|nosudo> [fullname_b64|-] [password_b64|-]"; user_ensure "$@" ;;
    profile-apply)  [ $# -eq 2 ] || die "profile-apply <slug> <sudoers_b64>"; profile_apply "$@" ;;
    profile-member) [ $# -eq 2 ] || die "profile-member <name> <slug|->"; profile_member "$@" ;;
    profile-prune)  [ $# -le 1 ] || die "profile-prune [slugs-csv|-]"; profile_prune "${1:--}" ;;
    user-remove) [ $# -eq 1 ] || die "user-remove <name>"; user_remove "$@" ;;
    user-lock)   [ $# -eq 1 ] || die "user-lock <name>"; user_lock "$@" ;;
    timezone)    [ $# -eq 1 ] || die "timezone <zone>"; set_timezone "$@" ;;
    ntp)         [ $# -eq 1 ] || die "ntp <servers-csv>"; set_ntp "$@" ;;
    user-disable) [ $# -eq 1 ] || die "user-disable <name>"; user_disable "$@" ;;
    user-enable) [ $# -eq 1 ] || die "user-enable <name>"; user_enable "$@" ;;
    users-scan)  [ $# -eq 0 ] || die "users-scan"; users_scan ;;
    deep-scan)   [ $# -eq 1 ] || die "deep-scan tools|needrestart|lynis|curated"; deep_scan "$@" ;;
    # version meldet den Stand des Skripts. LCM vergleicht ihn im Scan mit dem
    # eingebauten Stand und weist einen veralteten Helper aus - sonst laufen
    # Korrekturen an den privilegierten Aktionen ins Leere, ohne dass es
    # jemandem auffaellt.
    version)     [ $# -eq 0 ] || die "version"; echo "LCM-HELPER-VERSION: @@HELPER_VERSION@@" ;;
    # selftest belegt, dass der eingeschraenkte Benutzer den Helper ueber
    # sudo tatsaechlich erreicht - ohne etwas zu veraendern. Grundlage der
    # Wirkungsprobe beim Einschraenken (R2-019/R2-020).
    selftest)    [ $# -eq 0 ] || die "selftest"; [ "$(id -u)" = "0" ] || die "laeuft nicht als root"; echo "lcm-helper ok" ;;
    *) die "unbekanntes kommando: ${cmd:-<leer>}" ;;
esac
`

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
	var block strings.Builder
	block.WriteString(managedBegin + "\n")
	for _, k := range u.SSHKeys {
		block.WriteString(strings.TrimSpace(k.PublicKey) + "\n")
	}
	block.WriteString(managedEnd + "\n")

	// Der Helper kennt nur „volle Rechte oder keine". Eigene Profile werden im
	// eingeschränkten Modus ohnehin übersprungen (siehe applyProfiles), sodass
	// hier nur die mitgelieferten Zustände ankommen.
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
// die eigene Profilgruppe ausgestellt ist und kein `ALL` als Kommando trägt.
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
	return append(cmds, helperCmd("profile-prune", keep))
}

// helperProfileMemberCmd setzt die Profil-Mitgliedschaft eines Kontos über den
// Helper (leerer Slug = in keiner Profilgruppe).
func helperProfileMemberCmd(username, slug string) string {
	if slug == "" {
		slug = "-"
	}
	return helperCmd("profile-member", username, slug)
}
