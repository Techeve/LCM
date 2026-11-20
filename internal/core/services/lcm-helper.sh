#!/bin/sh
# LCM-Helper - von LCM verwaltet; nicht von Hand bearbeiten.
# Fest umrissene privilegierte Verwaltungsaktionen fuer den eingeschraenkten
# LCM-Management-Benutzer. Jeder Parameter wird streng validiert - keine
# Root-Shell, kein freier Dateisystemzugriff.
set -u

die() { echo "lcm-helper: $*" >&2; exit 1; }

b64dec() { printf '%s' "$1" | base64 -d 2>/dev/null || die "ungueltige base64-daten"; }

# matches <wert> <grep-argumente>: Der Wert passt als Ganzes. grep prueft
# zeilenweise: Ein Wert mit Zeilenumbruch kaeme sonst mit einer einzigen
# passenden Zeile durch, und der Rest landete in einer Konfigurationsdatei.
NL='
'
matches() {
    _wert="$1"; shift
    case "$_wert" in *"$NL"*) return 1 ;; esac
    printf '%s' "$_wert" | grep -Eq "$@"
}

valid_slug()  { matches "$1" '^[a-z0-9][a-z0-9-]{0,63}$'; }
valid_user()  { matches "$1" '^[a-z_][a-z0-9_-]{0,31}$'; }
valid_port()  { matches "$1" '^[0-9]{1,5}$' && [ "$1" -ge 1 ] && [ "$1" -le 65535 ]; }
valid_url()   { matches "$1" '^https?://[][A-Za-z0-9._~:/?#@!$&()*+,;=%-]+$'; }
valid_repo_url(){ matches "$1" '^https://[A-Za-z0-9._~:/@%+,=-]+$'; }
valid_http_repo_url(){ matches "$1" '^http://[A-Za-z0-9._~:/@%+,=-]+$'; }
valid_pkg_url(){ matches "$1" '^https?://[A-Za-z0-9._~:/@%+,=-]+$'; }
valid_shell() { matches "$1" '^/[A-Za-z0-9/._-]+$'; }
valid_ip()    { matches "$1" '^[0-9A-Fa-f:.]{2,45}$'; }
valid_cidr()  { matches "$1" '^[0-9A-Fa-f:.]{2,45}(/[0-9]{1,3})?$'; }
valid_pin()   { matches "$1" '^[a-z0-9][a-z0-9._+-]{0,126}\*?$'; }
valid_domain(){ matches "$1" '^[A-Za-z0-9._-]{1,253}$'; }

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
sshd_reload() { { mkdir -p /run/sshd 2>/dev/null; sshd -t; } && { if sshd_socket_active; then if systemctl is-active --quiet ssh.service 2>/dev/null; then systemctl reset-failed ssh 2>/dev/null; systemctl restart ssh; elif systemctl is-active --quiet sshd.service 2>/dev/null; then systemctl reset-failed sshd 2>/dev/null; systemctl restart sshd; fi; else systemctl reload sshd 2>/dev/null || systemctl reload ssh 2>/dev/null || rc-service sshd reload 2>/dev/null || rc-service sshd restart 2>/dev/null || service ssh reload 2>/dev/null || service sshd reload; fi; }; }

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
    grep -vE '^[[:space:]]*(#|$)' "$conf" | head -n1 | grep -qiE '^[[:space:]]*Include[[:space:]]+/etc/ssh/sshd_config\.d/' && return 0
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
    # Optionen, die die Signaturpruefung abschalten - trusted=yes in der
    # Einzeilen-Form, Trusted: im deb822-Format, dazu allow-insecure und
    # allow-weak -, gibt es ueber den Helper nicht: Mit ihnen liesse sich jedes
    # Paket einer fremden Quelle als root installieren.
    if printf '%s' "$line" | grep -Eqi '(trusted|allow-insecure|allow-weak|allow-downgrade-to-insecure)[[:space:]]*[=:]'; then
        die "apt-zeile abgelehnt: trusted und allow-insecure schalten die signaturpruefung ab"
    fi
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

# apt_update_ok: apt-get update laufen lassen und die Ausgabe auswerten. Der
# Exit-Code allein genuegt nicht: apt-get update endet auch dann mit 0, wenn
# einzelne Quellen mit "Err:" scheitern.
apt_update_ok() {
    out=$(LC_ALL=C apt-get update 2>&1)
    rc=$?
    printf '%s\n' "$out"
    [ "$rc" -eq 0 ] && ! printf '%s\n' "$out" | grep -qE '^(Err|E):'
}

# apt_backups: die Sicherungen unter /etc/apt, eine je Zeile. Die Liste steht
# fest, bevor der Aufrufer umbenennt - sonst liefe find durch Verzeichnisse,
# die sich unter ihm aendern. Zeilenweise statt per Wortzerlegung, damit
# Dateinamen mit Leerzeichen heil bleiben.
apt_backups() {
    baks=$(find /etc/apt -name '*.lcm-bak')
    [ -z "$baks" ] || printf '%s\n' "$baks"
}

# restore_sources: gesicherte Quellen zurueck und die Paketlisten neu laden,
# denn apt verwirft beim Update die Listen der alten Adressen.
restore_sources() {
    apt_backups | while IFS= read -r f; do mv "$f" "${f%.lcm-bak}"; done
    apt-get update >/dev/null 2>&1 || true
}

# https_switch_file <datei> [url...]: die genannten http-URLs der Datei (ohne
# Angabe alle) auf https umstellen, mit Sicherung davor. Endet mit 1, wenn sich
# an der Datei nichts geaendert hat. Eine URL zaehlt nur als Ganzes - gefolgt
# von Leerraum oder dem Zeilenende -, sonst stellte .../debian auch
# .../debian-security mit um, obwohl der Anwender es abgewaehlt hat. Ein
# Schraegstrich am Ende zaehlt nicht (deb822 schreibt ".../ubuntu/").
https_switch_file() {
    f="$1"; shift
    grep -q 'http://' "$f" || return 1
    cp "$f" "$f.lcm-bak"
    if [ $# -eq 0 ]; then
        sed -i 's|http://|https://|g' "$f"
    else
        for u in "$@"; do b="${u%/}"; h="https://${b#http://}"; sed -i -e "s|$b\(/\{0,1\}[[:space:]]\)|$h\1|g" -e "s|$b\(/\{0,1\}\)\$|$h\1|" "$f"; done
    fi
    # Nichts geaendert: keine Sicherung, kein Erfolg.
    if cmp -s "$f" "$f.lcm-bak"; then rm -f "$f.lcm-bak"; return 1; fi
}

# repos-https [url...]: die genannten http://-apt-Quellen (ohne Angabe alle)
# auf https umstellen (mit Sicherung und Rollback, falls apt-update danach
# eine fehlerhafte Quelle meldet). Welche Quellen HTTPS sprechen, prueft der
# LCM-Server vorher; hier wird geprueft, dass es unbedenkliche URLs sind.
repos_https() {
    for u in "$@"; do valid_http_repo_url "$u" || die "ungueltige paketquellen-url: $u"; done
    apt-get install -y --no-install-recommends ca-certificates >/dev/null 2>&1 || true
    changed=0
    for f in /etc/apt/sources.list /etc/apt/sources.list.d/*.list /etc/apt/sources.list.d/*.sources; do
        [ -f "$f" ] || continue
        https_switch_file "$f" "$@" && changed=1
    done
    if [ "$changed" -eq 0 ]; then echo 'LCM: keine http-quellen gefunden - nichts zu tun'; return 0; fi
    if apt_update_ok; then
        apt_backups | while IFS= read -r f; do
            orig="${f%.lcm-bak}"; dest="@@HTTPS_BACKUP_DIR@@$orig"
            if [ -f "$dest" ]; then rm -f "$f"; else mkdir -p "$(dirname "$dest")"; mv "$f" "$dest"; fi
        done
        echo 'LCM: paketquellen auf https umgestellt'
    else
        restore_sources
        echo 'LCM: apt-update nach der umstellung fehlgeschlagen - alle aenderungen zurueckgerollt'
        exit 1
    fi
}

# repos-http <url>...: die genannten Paketquellen von https auf http
# zurueckstellen (mit Sicherung und Rollback, falls apt-update eine fehlerhafte
# Quelle meldet).
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
        # Nur als Ganzes, wie bei https_switch_file (httpURLSed).
        for u in $URLS; do b="${u%/}"; h="http://${b#https://}"; sed -i -e "s|$b\(/\{0,1\}[[:space:]]\)|$h\1|g" -e "s|$b\(/\{0,1\}\)\$|$h\1|" "$f"; done
        if cmp -s "$f" "$f.lcm-bak"; then rm -f "$f.lcm-bak"; else changed=1; fi
    done
    if [ "$changed" -eq 0 ]; then echo 'LCM: keine der genannten quellen gefunden - nichts zu tun'; return 0; fi
    if apt_update_ok; then
        find /etc/apt -name '*.lcm-bak' -delete
        echo 'LCM: paketquellen auf http zurueckgestellt'
    else
        restore_sources
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
    out=$(LC_ALL=C apt-get update 2>&1)
    mode='HTTP und HTTPS'
    # apt-cacher-ng tunnelt HTTPS nur mit gesetztem PassThroughPattern; sonst
    # antwortet er mit 403 und JEDE HTTPS-Paketquelle faellt aus (R2-038).
    # Dann HTTPS am Proxy vorbei - lieber ungecacht als unerreichbar.
    if printf '%s' "$out" | grep -qiE 'CONNECT denied|Invalid response from proxy'; then
        printf 'Acquire::http::Proxy "%s";\nAcquire::https::Proxy "DIRECT";\n' "$1" > "$dropin"
        out=$(LC_ALL=C apt-get update 2>&1)
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
    # Funktionstest: mindestens eine Quelle muss ueber den Cache gekommen sein.
    src=$(printf '%s\n' "$out" | grep -cE '^(Hit|Get):')
    if [ "$src" -eq 0 ]; then
        rm -f "$dropin"
        apt-get update >/dev/null 2>&1 || true
        echo 'LCM: funktionstest fehlgeschlagen - keine paketquelle ueber den cache geladen; drop-in wieder entfernt'
        exit 1
    fi
    echo "LCM: funktionstest bestanden - $src paketquelle(n) ueber den cache geladen"
    echo "LCM: apt-anfragen laufen jetzt ueber den cache $1 ($mode)"
}

# timezone <zone>: die Zeitzone setzen und ZURUECKLESEN. Eine geschriebene
# Datei ist kein Beleg: ohne systemd greift timedatectl nicht, und ein blosses
# Ueberschreiben von /etc/timezone bleibt ohne passenden localtime-Symlink
# wirkungslos.
valid_tz() { matches "$1" '^[A-Za-z][A-Za-z0-9+_-]*(/[A-Za-z0-9+._-]+){0,2}$'; }

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
        matches "$s" '^[A-Za-z0-9]([A-Za-z0-9._:-]{0,251}[A-Za-z0-9])?$' || die "ungueltiger zeitserver: $s"
    done
    synced=no; svc=""
    if command -v chronyc >/dev/null 2>&1; then
        svc=chrony
        conf=/etc/chrony/chrony.conf; [ -f "$conf" ] || conf=/etc/chrony.conf
        [ -f "$conf" ] && { [ -f "$conf.lcm-bak" ] || cp -f "$conf" "$conf.lcm-bak"; }
        { grep -vE '^[ \t]*(server|pool)[ \t]' "$conf" 2>/dev/null
          for s in $srv; do echo "server $s iburst"; done; } > "$conf.lcm-new" && mv "$conf.lcm-new" "$conf"
@@NTP_CHRONY_DROPINS@@
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

# dns <ips-csv|off> <testdomain>: bis zu vier Nameserver wirksam setzen
# (systemd-resolved-Drop-in oder /etc/resolv.conf, geschuetzt vor Proxmox und
# DHCP-Clients) mit Aufloesungstest + Rollback, oder die LCM-DNS-Verwaltung
# wieder entfernen. Die Logik teilt sich der Helper mit dem Voll-Modus.
@@DNS_FUNCS@@

dns() {
    ips="$1"; testdom="$2"
    valid_domain "$testdom" || die "ungueltige test-domain"
    if [ "$ips" = "off" ]; then
        lcm_dns_unset
        return 0
    fi
    space=""
    old_ifs="$IFS"; IFS=','
    for ip in $ips; do
        valid_ip "$ip" || die "ungueltige dns-ip: $ip"
        space="${space}${space:+ }${ip}"
    done
    IFS="$old_ifs"
    [ -n "$space" ] || die "keine dns-ip angegeben"
    lcm_dns_set "$space" "$testdom" || exit 1
}

# ssh-harden on|off: die LCM-Haertungs-Drop-ins schreiben/entfernen und sshd
# neu laden (bei Fehlschlag zurueckrollen). 00- setzt sich gegen fremde
# Drop-ins wie 50-cloud-init.conf durch, siehe sshKeysOnlyDropin.
ssh_harden() {
    dropin=/etc/ssh/sshd_config.d/60-lcm-hardening.conf
    first=/etc/ssh/sshd_config.d/00-lcm-hardening.conf
    case "$1" in
        on)
            sshd_ensure_include || die "sshd_config konnte nicht um die Include-Zeile ergaenzt werden"
            install -d -m 755 /etc/ssh/sshd_config.d
            printf 'PasswordAuthentication no\nChallengeResponseAuthentication no\nPubkeyAuthentication yes\nPermitRootLogin prohibit-password\n' > "$dropin"
            printf 'PasswordAuthentication no\nPubkeyAuthentication yes\n' > "$first"
            if ! sshd_reload; then
                rm -f "$dropin" "$first"; sshd_reload
                die "sshd-haertung fehlgeschlagen - zurueckgerollt"
            fi
            # Nachweis statt Annahme: wirkt die Haertung nicht, ist eine
            # Erfolgsmeldung schaedlicher als ein ehrlicher Fehlschlag.
            sshd_password_auth_off; rc=$?
            if [ "$rc" -eq 1 ]; then
                rm -f "$dropin" "$first"; sshd_reload
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
            rm -f "$dropin" "$first"
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
            # Nur ein einzelner Port 22 braucht keine Zeile. Im Uebergang muss er
            # dastehen: Der Generator leitet die Ports der Socket-Unit hieraus ab.
            if [ "$ports" != "22" ]; then body="${body}Port $p
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
    # Der Dienst muss stehen, sonst verweigert systemd den Start der Socket-Unit.
    systemctl daemon-reload || die "daemon-reload fehlgeschlagen"
    systemctl stop "${u%.socket}.service" 2>/dev/null
    systemctl reset-failed "${u%.socket}.service" 2>/dev/null
    systemctl restart "$u" || die "socket-unit $u konnte nicht neu gestartet werden"
}

# nologin_shell: der nologin-Pfad dieser Distribution, auf BusyBox /bin/false
# (wie nologinShellStep im Voll-Modus).
nologin_shell() {
    for s in "$(command -v nologin 2>/dev/null)" /usr/sbin/nologin /sbin/nologin; do
        [ -n "$s" ] && [ -x "$s" ] && { echo "$s"; return 0; }
    done
    echo /bin/false
}

# user-ensure <name> <shell|nologin> <keys_b64> <sudo|nosudo> [fullname_b64|-] [password_b64|-]
# Idempotent: Benutzer anlegen/aktualisieren, LCM-Key-Block in die
# authorized_keys schreiben, sudo-Grant setzen/entfernen. "nologin" steht fuer
# den Kontotyp "nur Dateizugriff" (siehe profile-sftp); der Pfad weicht je
# Distribution ab, deshalb loest ihn der Helper selbst auf.
user_ensure() {
    name="$1"; shell="$2"; keys_b64="$3"; sudo_flag="$4"
    fullname_b64="${5:--}"; password_b64="${6:--}"
    valid_user "$name" || die "ungueltiger benutzername"
    deny_self_and_root "$name"
    [ "$shell" = nologin ] && shell=$(nologin_shell)
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
    @@ACCOUNT_UNLOCK@@
    @@ACCOUNT_UNEXPIRE@@
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

# Programme und Argumente, die faktisch Root bedeuten (Shells, Benutzerwechsel,
# Interpreter, ...). Die Listen kommen aus der Domaene (rootEquivalentBinaries,
# escapeArguments) - dieselben, gegen die LCM beim Speichern prueft.
ROOT_BINS=' @@ROOT_EQUIVALENT_BINARIES@@ '
ESCAPE_ARGS=' @@ESCAPE_ARGUMENTS@@ '

# Pfade, auf die keine Datei- oder Verzeichnisregel zeigen darf - dieselbe
# Liste wie in der Domaene (domain.BlockedRulePaths): "=" vorne heisst nur genau
# dieser Pfad, "*" hinten alles, was so beginnt, sonst der Pfad samt allem
# darunter.
BLOCKED_PATHS='@@BLOCKED_RULE_PATHS@@'

# rule_path_hits <pfad> <eintrag> <modus>: Der Pfad liegt im gesperrten
# Bereich - oder bei read/readwrite darueber, denn setfacl -R gaebe ihn mit frei.
rule_path_hits() {
    case "$2" in
        =*) [ "$1" = "${2#=}" ]; return ;;
        *'*') _pre="${2%?}"; case "$1" in "$_pre"*) return 0 ;; esac ;;
        *) _pre="$2"; case "$1" in "$2"|"$2"/*) return 0 ;; esac ;;
    esac
    [ "$3" = read ] || [ "$3" = readwrite ] || return 1
    case "$_pre" in "$1"/*) return 0 ;; esac
    return 1
}

# clean_abs_path <pfad>: absolut und in kurzer Form - kein "..", kein "//",
# kein "/" am Ende. LCM speichert Pfade bereinigt; eine andere Form kommt nur
# von einem Aufrufer, der an der Pruefung vorbei will.
clean_abs_path() {
    matches "$1" '^/[A-Za-z0-9._+@%:=~^/-]*$' || { echo "unzulaessiger pfad: $1" >&2; return 1; }
    case "$1" in
        /) ;;
        */|*//*|*/./*|*/../*|*/.|*/..) echo "pfad nicht in kurzer form: $1" >&2; return 1 ;;
    esac
}

# rule_path_ok <pfad> <edit|deny|read|readwrite>: in kurzer Form und nicht
# gesperrt. Dazu das Home des aufrufenden Service-Users: Seine authorized_keys
# sind der Zugang von LCM.
rule_path_ok() {
    clean_abs_path "$1" || return 1
    _home=$(getent passwd "${SUDO_USER:-root}" 2>/dev/null | cut -d: -f6)
    set -f
    for _b in $BLOCKED_PATHS ${_home:+"$_home"}; do
        if rule_path_hits "$1" "$_b" "$2"; then
            set +f
            echo "gesperrter pfad: $1 (gesperrt ist $_b)" >&2
            return 1
        fi
    done
    set +f
}

# profile_rule_ok <gruppe> <zeile>: Die Zeile hat genau die Form, die
# profileSudoersContent schreibt: "%<gruppe> ALL=(<benutzer>) NOPASSWD: <kommando>"
# (oder PASSWD:), das Kommando ein absoluter Pfad oder "sudoedit <datei>".
# Abgelehnt wird mit Grund auf stderr.
profile_rule_ok() {
    _g="$1"; _l="$2"
    case "$_l" in
        "%$_g ALL="*) ;;
        *) echo "fremde gruppe in profil-regel: $_l" >&2; return 1 ;;
    esac
    _r="${_l#"%$_g ALL="}"
    # ALL in jeder Form: (ALL), (ALL:ALL), NOPASSWD: ALL, NOPASSWD:ALL, mit
    # Tabulator oder mehreren Leerzeichen. ALL darf nur als Host-Angabe vorne
    # stehen. Kleingeschrieben ist es fuer sudo kein Schluesselwort.
    if matches "$_r" '(^|[^A-Za-z0-9_/.-])ALL([^A-Za-z0-9_/.-]|$)'; then
        echo "uneingeschraenkte regel abgelehnt: $_l" >&2; return 1
    fi
    # Nur die Zeichen, die domain.ValidateSudoCommand zulaesst: kein Komma
    # (zweites Kommando), kein Backslash (Fortsetzungszeile), keine Platzhalter,
    # keine weiteren Tags wie SETENV.
    if ! matches "$_r" '^\([a-z_][a-z0-9_-]{0,31}\) (NOPASSWD|PASSWD): [A-Za-z0-9/._+@%:=~^-]+( [A-Za-z0-9/._+@%:=~^-]+)*$'; then
        echo "unzulaessige profil-regel: $_l" >&2; return 1
    fi
    set -f; set -- ${_r#*: }; set +f
    if [ "$1" = sudoedit ]; then
        [ $# -eq 2 ] || { echo "unzulaessige sudoedit-regel: $_l" >&2; return 1; }
        rule_path_ok "$2" edit
        return
    fi
    case "$1" in
        /*) ;;
        *) echo "kommando ohne absoluten pfad: $_l" >&2; return 1 ;;
    esac
    # Auch unter Nebennamen: mit Versionsnummer (python3.11) und mit Variante
    # hinter einem Punkt (vim.basic) - wie domain.rootEquivalentBinary.
    _b=${1##*/}
    _v=$(printf '%s' "$_b" | sed 's/[0-9.]*$//')
    for _c in "$_b" "$_v" "${_b%%.*}" "${_v%%.*}"; do
        case "$ROOT_BINS" in
            *" $_c "*) echo "root-gleichwertiges kommando abgelehnt: $_l" >&2; return 1 ;;
        esac
    done
    shift
    for _a in "$@"; do
        case "$ESCAPE_ARGS" in
            *" $_a "*) echo "root-gleichwertiges argument abgelehnt: $_l" >&2; return 1 ;;
        esac
    done
}

# profile-apply <slug> <sudoers_b64>: Berechtigungsprofil einrichten - Gruppe
# lcm-prof-<slug> anlegen und die sudoers-Datei schreiben.
#
# Der Inhalt kommt base64-kodiert von LCM, wird hier aber NICHT blind
# uebernommen (profile_rule_ok). Ohne diese Pruefung koennte ein
# kompromittiertes LCM dem eingeschraenkten Service-User ueber eine
# Profil-Datei volle Rechte zurueckgeben - genau das, was der eingeschraenkte
# Modus verhindern soll. Kommentar ist nur "#" mit Leerzeichen: "#include"
# liest eine fremde Datei ein, "#1000" ist fuer sudo eine Benutzer-ID.
profile_apply() {
    slug="$1"; body_b64="$2"
    valid_slug "$slug" || die "ungueltiger profil-slug"
    group="lcm-prof-$slug"
    path="/etc/sudoers.d/$group"
    body=$(b64dec "$body_b64")
    printf '%s\n' "$body" | while IFS= read -r line; do
        case "$line" in
            '#'|'# '*) continue ;;
            '#'*) echo "include oder benutzer-id in profil-datei abgelehnt: $line" >&2; exit 1 ;;
            *[![:space:]]*) ;;
            *) continue ;;
        esac
        profile_rule_ok "$group" "$line" || exit 1
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
    leave_profile_groups "$name" "$want"
    [ -n "$want" ] && { usermod -aG "$want" "$name" 2>/dev/null || addgroup "$name" "$want" 2>/dev/null || true; }
    echo "lcm-helper: profil-mitgliedschaft von $name gesetzt"
}

# leave_profile_groups <name> [behalten]: alle lcm-prof-Gruppen verlassen
# ausser der genannten. delgroup ist der Weg auf BusyBox: Dort fehlt gpasswd,
# und deluser kennt nur "deluser USER" (wie setProfileMembershipScript).
leave_profile_groups() {
    for g in $(id -nG "$1" 2>/dev/null); do
        case "$g" in
            lcm-prof-*)
                [ "$g" = "${2:-}" ] || gpasswd -d "$1" "$g" >/dev/null 2>&1 || deluser "$1" "$g" >/dev/null 2>&1 || delgroup "$1" "$g" >/dev/null 2>&1 || true
                ;;
        esac
    done
}

# profile_sftp_content <slugs-csv|->: das sshd-Drop-in fuer die Profile mit
# Kontotyp "nur Dateizugriff" - dieselben Bloecke wie profileSSHDContent im
# Voll-Modus, mit "Match all" als Abschluss (die Include-Zeile steht oben in
# sshd_config, ohne Abschluss rutschte der Rest der Hauptdatei in den letzten
# Block). Leer, wenn kein Profil es verlangt.
profile_sftp_content() {
    [ "$1" = "-" ] && return 0
    echo '# LCM - Berechtigungsprofile mit Kontotyp "nur Dateizugriff".'
    echo '# Von LCM verwaltet; nicht von Hand bearbeiten.'
    old_ifs="$IFS"; IFS=','; set -f
    for s in $1; do
        printf 'Match Group lcm-prof-%s\n    ForceCommand internal-sftp\n    AllowTcpForwarding no\n    X11Forwarding no\n    PermitTTY no\n\n' "$s"
    done
    set +f; IFS="$old_ifs"
    echo 'Match all'
}

# profile-sftp <slugs-csv|->: das Drop-in aus den geprueften Slugs schreiben
# oder entfernen - kein Freitext, nur Match-Bloecke fuer lcm-prof-Gruppen.
# Wie im Voll-Modus: Sicherung, sshd -t vor dem Neuladen, bei Fehlschlag
# zurueck. Ein unveraendertes Drop-in laedt sshd nicht neu.
profile_sftp() {
    path=/etc/ssh/sshd_config.d/70-lcm-profiles.conf
    [ -n "$1" ] || die "profile-sftp <slugs-csv|->"
    if [ "$1" != "-" ]; then
        old_ifs="$IFS"; IFS=','; set -f
        for s in $1; do valid_slug "$s" || die "ungueltiger profil-slug: $s"; done
        set +f; IFS="$old_ifs"
    fi
    want=$(profile_sftp_content "$1")
    if [ "$(cat "$path" 2>/dev/null)" = "$want" ]; then
        echo "lcm-helper: sftp-drop-in unveraendert"
        return 0
    fi
    cp -a "$path" "$path.lcmbak" 2>/dev/null || true
    if [ -z "$want" ]; then
        rm -f "$path"
    else
        install -d -m 755 /etc/ssh/sshd_config.d
        printf '%s\n' "$want" > "$path"
        chmod 644 "$path"
    fi
    if sshd_reload; then
        rm -f "$path.lcmbak"
    else
        mv "$path.lcmbak" "$path" 2>/dev/null || rm -f "$path"
        sshd_reload
        die "sshd lehnt die konfiguration ab - zurueckgerollt"
    fi
    echo "lcm-helper: sftp-drop-in gesetzt"
}

# profile-acl set <slug> <read|readwrite|deny> <pfad> | remove <slug> - <pfad>:
# Verzeichnisrechte einer Profilgruppe setzen oder zuruecknehmen - wie
# profilePathScript im Voll-Modus (setfacl -RP, Vererbung per Default-ACL).
# Ohne diesen Weg lief setfacl im eingeschraenkten Modus ohne root und brach
# den ganzen Benutzer-Sync ab.
#
# Nur fuer lcm-prof-*-Gruppen und nur auf einem Pfad ohne Symlink: readlink -f
# muss ihn unveraendert liefern, sonst gaebe ein Link sein Ziel mit frei.
# Gesetzt wird nur auf einem freien Pfad (rule_path_ok); zuruecknehmen geht
# ueberall, denn genau auf einem inzwischen gesperrten Pfad soll ein alter
# Eintrag verschwinden.
profile_acl() {
    action="$1"; slug="$2"; mode="$3"; p="$4"
    valid_slug "$slug" || die "ungueltiger profil-slug"
    group="lcm-prof-$slug"
    case "$action/$mode" in
        set/read) spec=rX ;;
        set/readwrite) spec=rwX ;;
        set/deny) spec=--- ;;
        remove/-) spec="" ;;
        *) die "profile-acl set <slug> read|readwrite|deny <pfad> | remove <slug> - <pfad>" ;;
    esac
    if [ "$action" = set ]; then
        rule_path_ok "$p" "$mode" || die "verzeichnisrecht abgelehnt: $p"
    else
        clean_abs_path "$p" || die "verzeichnisrecht abgelehnt: $p"
    fi
    [ -d "$p" ] || { echo "pfad fehlt: $p"; return 0; }
    [ "$(readlink -f "$p")" = "$p" ] || { echo "pfad enthaelt einen symlink, uebersprungen: $p"; return 0; }
    if ! getent group "$group" >/dev/null 2>&1; then
        [ "$action" = remove ] && { echo "gruppe $group fehlt, nichts zurueckzunehmen: $p"; return 0; }
        die "gruppe $group fehlt"
    fi
    command -v setfacl >/dev/null 2>&1 || die "setfacl fehlt (paket acl)"
    if [ "$action" = remove ]; then
        # Ein fehlender Eintrag ist kein Fehler (setfacl -x meldet ihn nicht).
        # Scheitert setfacl sonst, bleibt der Eintrag in LCM stehen, damit der
        # naechste Abgleich es erneut versucht.
        rc=0
        setfacl -RP -x "g:$group" "$p" || rc=$?
        setfacl -RP -d -x "g:$group" "$p" || rc=$?
        [ "$rc" -eq 0 ] || die "setfacl -x fehlgeschlagen: $p"
        return 0
    fi
    # Beide Aufrufe muessen gelingen: Ein gescheiterter erster darf nicht
    # vom Erfolg des zweiten verdeckt werden.
    rc=0
    setfacl -RP -m "g:$group:$spec" "$p" || rc=$?
    if [ "$mode" != deny ]; then setfacl -RP -d -m "g:$group:$spec" "$p" || rc=$?; fi
    [ "$rc" -eq 0 ] || die "setfacl fehlgeschlagen: $p"
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

# self-remove: den aufrufenden Service-User bei der Ziel-Bereinigung entfernen,
# samt Schluessel, sudo-Grant, Shims und diesem Helper. Der Name kommt aus sudo,
# nicht vom Aufrufer: Der eingeschraenkte Benutzer kann so nur sich selbst
# entfernen. Bleibt das Konto bestehen, ist das ein FEHLER; der Helper bleibt
# dann liegen und der Schluessel ist trotzdem weg.
self_remove() {
    name="${SUDO_USER:-}"
    valid_user "$name" || die "self-remove nur ueber sudo"
    [ "$name" != "root" ] || die "root wird nicht entfernt"
    rm -f "/home/$name/.ssh/authorized_keys"
    rm -f "/etc/sudoers.d/$name"
    if command -v userdel >/dev/null 2>&1; then userdel -rf "$name" >/dev/null 2>&1
    elif command -v deluser >/dev/null 2>&1; then deluser --remove-home "$name" >/dev/null 2>&1
    fi
    id -u "$name" >/dev/null 2>&1 && die "konto $name besteht weiterhin"
    rm -rf "/home/${name:?}"
    rm -f "$0"
    echo "lcm-helper: service-user $name entfernt"
}

# user-lock <name>: deaktivierten Benutzer sperren - Passwort gesperrt, Konto
# abgelaufen, LCM-Schluesselblock raus, sudo-Grant weg, Profilgruppen
# verlassen. Konto und Home bleiben (Deaktivierung ist umkehrbar; endgueltig
# ist user-remove). R2-039. Das Ablaufdatum sperrt auch einen Schluessel
# ausserhalb des LCM-Blocks; user-ensure hebt es wieder auf (siehe lockUserScript).
user_lock() {
    name="$1"
    valid_user "$name" || die "ungueltiger benutzername"
    deny_self_and_root "$name"
    if id -u "$name" >/dev/null 2>&1; then
        usermod -L "$name" 2>/dev/null || passwd -l "$name" 2>/dev/null || true
        usermod -e 1 "$name" 2>/dev/null || chage -E 1 "$name" 2>/dev/null || true
        ak="/home/$name/.ssh/authorized_keys"
        if [ -f "$ak" ]; then
            sed -i '/# >>> LCM managed keys >>>/,/# <<< LCM managed keys <<</d' "$ak"
        fi
        rm -f "/etc/sudoers.d/lcm-$name"
        leave_profile_groups "$name"
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

# host-install <trivy|sandbox|apt-cacher>: Einrichtung auf dem LCM-Host.
# Die Skripte kommen aus lcm_host.go (eine Quelle fuer beide Modi); der
# Parameter ist eine feste Auswahl, nichts davon fliesst in ein Kommando ein.
# Damit braucht der Management-Benutzer des LCM-Hosts kein NOPASSWD:ALL mehr:
# apt, Schluesselringe und Dienste erreicht er nur ueber diese Einstiege.
host_install() {
    case "$1" in
        trivy)      @@HOST_TRIVY@@ ;;
        sandbox)    @@HOST_SANDBOX@@ ;;
        apt-cacher) @@HOST_APTCACHER@@ ;;
        *) die "host-install trivy|sandbox|apt-cacher" ;;
    esac
}

# host-apt-cacher <restart|cache-on|cache-off>: apt-cacher-ng bedienen.
host_apt_cacher() {
    case "$1" in
        restart)   @@HOST_APTCACHER_RESTART@@ ;;
        cache-on)  @@HOST_APTCACHER_CACHE_ON@@ ;;
        cache-off) @@HOST_APTCACHER_CACHE_OFF@@ ;;
        *) die "host-apt-cacher restart|cache-on|cache-off" ;;
    esac
}

# host-crowdsec-lapi <bouncer|plain>: CrowdSec-LAPI-Server einrichten.
host_crowdsec_lapi() {
    case "$1" in
        bouncer) @@HOST_CROWDSEC_BOUNCER@@ ;;
        plain)   @@HOST_CROWDSEC_PLAIN@@ ;;
        *) die "host-crowdsec-lapi bouncer|plain" ;;
    esac
}

# host-self-update: das eigene Paket per systemd-run aktualisieren - der
# Lauf ueberlebt den Neustart, den das Paket selbst ausloest.
host_self_update() {
@@HOST_SELF_UPDATE@@
}

# host-apt-channel <community|beta|enterprise> [...]: den apt-Paketkanal des
# LCM-Hosts umstellen. Die Skripte kommen aus subscription_apt.go (eine Quelle
# fuer beide Modi); die Werte, die dort Variablen sind, werden hier geprueft:
#   beta <paketserver-url>
#   enterprise <repo-url> <instanz-id> <zugangsschluessel_b64>
host_apt_channel() {
    mode="$1"; shift
    case "$mode" in
        community)
            [ $# -eq 0 ] || die "host-apt-channel community"
            @@HOST_APT_COMMUNITY@@
            ;;
        beta)
            [ $# -eq 1 ] || die "host-apt-channel beta <paketserver-url>"
            valid_pkg_url "$1" || die "ungueltige paketserver-adresse"
            FALLBACK="$1"
            @@HOST_APT_BETA@@
            ;;
        enterprise)
            [ $# -eq 3 ] || die "host-apt-channel enterprise <repo-url> <instanz-id> <zugangsschluessel_b64>"
            REPO="$1"; LOGIN="$2"; SECRET=$(b64dec "$3") || exit 1
            valid_repo_url "$REPO" || die "ungueltige repository-adresse (nur https)"
            matches "$LOGIN" '^[A-Za-z0-9._-]{1,64}$' || die "ungueltige instanz-id"
            matches "$SECRET" '^[A-Za-z0-9._~+=-]{1,256}$' || die "ungueltiger zugangsschluessel"
            @@HOST_APT_ENTERPRISE@@
            ;;
        *) die "host-apt-channel community|beta|enterprise" ;;
    esac
}

# package-pins <apt|dnf|zypper|apk|pacman> <noremove-csv|-> <hold-csv|->: die
# Paket-Pins in die von LCM verwalteten Dateien schreiben (apt: Schutzdatei,
# dnf: protected.d, pacman: eigene Include-Datei) und die Holds abgleichen
# (apt-mark, versionlock, zypper-Schloesser, apk hold). Freigegeben wird nur,
# was in der Zustandsdatei unter /var/lib/lcm-managed steht - Holds eines
# Admins bleiben. Die Rumpfe kommen aus pkgpin.go; jeder Pin-Name wird gegen
# [a-z0-9._+-] (und ein abschliessendes *) geprueft.
pin_words() {
    [ "$1" = "-" ] && return 0
    old_ifs="$IFS"; IFS=','; set -f
    for n in $1; do
        valid_pin "$n" || { echo "lcm-helper: ungueltiger pin-name: $n" >&2; return 1; }
        printf '%s ' "$n"
    done
    set +f; IFS="$old_ifs"
}

package_pins() {
    kind="$1"
    NOREMOVE=$(pin_words "$2") || exit 1
    HOLD=$(pin_words "$3") || exit 1
    NOREMOVE="${NOREMOVE% }"; HOLD="${HOLD% }"
    case "$kind" in
        apt)    @@PINS_APT@@ ;;
        dnf)    @@PINS_DNF@@ ;;
        zypper) @@PINS_ZYPPER@@ ;;
        apk)    @@PINS_APK@@ ;;
        pacman) @@PINS_PACMAN@@ ;;
        *) die "package-pins apt|dnf|zypper|apk|pacman <noremove-csv|-> <hold-csv|->" ;;
    esac
}

# nft_ruleset_ok prueft den Regelsatz Zeile fuer Zeile gegen die Formen, die
# LCM erzeugt (nftRuleset in firewall_nftables.go): LCMs eigene Tabelle, ein
# input-Hook mit policy drop und accept-Regeln fuer Ports, optional je Quelle
# und mit Bemerkung. Alles andere - fremde Tabellen, andere Hooks, include,
# flush - wird abgelehnt. Gibt die nicht passenden Zeilen aus.
nft_ruleset_bad_lines() {
    addr='[0-9A-Fa-f:.]+(/[0-9]{1,3})?'
    note=' comment "[^[:cntrl:]"\\]*"'
    printf '%s\n' "$1" | grep -Ev \
        -e '^(delete )?table inet lcm$' \
        -e '^table inet lcm \{$' \
        -e '^    chain input \{$' \
        -e '^        type filter hook input priority 0; policy drop;$' \
        -e '^        ct state established,related accept comment "lcm:[0-9a-f]{12}"$' \
        -e '^        (ct state invalid drop|iif "lo" accept|ip protocol icmp accept|meta l4proto ipv6-icmp accept)$' \
        -e "^        (meta nfproto ipv[46] )?(tcp|udp) dport [0-9]{1,5} accept(${note})?\$" \
        -e "^        ip6? saddr \\{ ${addr}(, ${addr})* \\} (tcp|udp) dport [0-9]{1,5} accept(${note})?\$" \
        -e '^    \}$' \
        -e '^\}$'
}

# firewall-apply nftables <ruleset_b64> | firewalld <firewall-cmd-argument>...:
# die Firewall-Regeln von LCM anwenden. Die Rumpfe kommen aus
# firewall_nftables.go und firewall_firewalld.go (eine Quelle fuer beide Modi).
firewall_apply() {
    tool="$1"; shift
    case "$tool" in
        nftables)
            [ $# -eq 1 ] || die "firewall-apply nftables <ruleset_b64>"
            RULESET_B64="$1"
            rs=$(b64dec "$RULESET_B64") || exit 1
            [ "${#rs}" -le 65536 ] || die "regelsatz zu gross"
            if bad=$(nft_ruleset_bad_lines "$rs"); then
                die "regelsatz abgelehnt, unzulaessige zeile: $(printf '%s\n' "$bad" | grep -m 1 .)"
            fi
            # Geschrieben wird genau, was geprueft wurde.
            RULESET_B64=$(printf '%s\n' "$rs" | base64)
            @@FW_NFT_APPLY@@
            ;;
        firewalld)
            [ $# -ge 1 ] || die "firewall-apply firewalld <argument>..."
            for a in "$@"; do
                matches "$a" \
                    -e '^--add-port=[0-9]{1,5}/(tcp|udp)$' \
                    -e '^--add-rich-rule=rule family="ipv[46]"( source address="[0-9A-Fa-f:./]+")? port port="[0-9]{1,5}" protocol="(tcp|udp)" accept$' \
                    || die "ungueltige freigabe: $a"
            done
            @@FW_FIREWALLD_APPLY@@
            ;;
        *) die "firewall-apply nftables|firewalld ..." ;;
    esac
}

# firewall-disable <nftables|firewalld>, firewall-status <nftables|firewalld>
# und firewall-port <allow|remove> <nftables|firewalld> <port>.
firewall_disable() {
    case "$1" in
        nftables)  @@FW_NFT_DISABLE@@ ;;
        firewalld) @@FW_FIREWALLD_DISABLE@@ ;;
        *) die "firewall-disable nftables|firewalld" ;;
    esac
}

firewall_status() {
    case "$1" in
        nftables)  @@FW_NFT_STATUS@@ ;;
        firewalld) @@FW_FIREWALLD_STATUS@@ ;;
        *) die "firewall-status nftables|firewalld" ;;
    esac
}

firewall_port() {
    PORT="$3"
    valid_port "$PORT" || die "ungueltiger port"
    case "$1/$2" in
        allow/nftables)   @@FW_NFT_PORT_ALLOW@@ ;;
        remove/nftables)  @@FW_NFT_PORT_REMOVE@@ ;;
        allow/firewalld)  @@FW_FIREWALLD_PORT_ALLOW@@ ;;
        remove/firewalld) @@FW_FIREWALLD_PORT_REMOVE@@ ;;
        *) die "firewall-port allow|remove nftables|firewalld <port>" ;;
    esac
}

# fail2ban-config <ip>...: LCMs fail2ban-Drop-in mit der Allowlist schreiben
# und fail2ban neu laden. Die Rumpfe der Sicherheits-Tools kommen aus
# securitytools.go und securitytools_manage.go.
fail2ban_config() {
    IGNORE=""
    for ip in "$@"; do
        valid_cidr "$ip" || die "ungueltige allowlist-ip: $ip"
        IGNORE="${IGNORE:+$IGNORE }$ip"
    done
    @@SEC_FAIL2BAN_CONFIG@@
}

# crowdsec-repo <deb|rpm>: das CrowdSec-Herstellerrepo einrichten.
crowdsec_repo() {
    case "$1" in
        deb) CS_REPO_SCRIPT=script.deb.sh ;;
        rpm) CS_REPO_SCRIPT=script.rpm.sh ;;
        *) die "crowdsec-repo deb|rpm" ;;
    esac
    @@SEC_CROWDSEC_REPO@@
}

# crowdsec-collections <name>...: Collections installieren.
crowdsec_collections() {
    for c in "$@"; do
        matches "$c" '^[A-Za-z0-9][A-Za-z0-9._/-]*$' || die "ungueltige collection: $c"
    done
    COLLS="$*"
    @@SEC_CROWDSEC_COLLECTIONS@@
}

# crowdsec-configure <local|keep|remote|console> <lapi-url|-> <lapi-login|->
# <lapi-passwort_b64|-> <console-key|-> <ip>...: LAPI anbinden (nur remote und
# console), Allowlist schreiben, Dienst aktivieren/laden.
crowdsec_configure() {
    LAPI_MODE="$1"; LAPI_URL=""; LAPI_LOGIN=""; LAPI_PW_B64=""; CONSOLE_KEY=""
    case "$LAPI_MODE" in
        local|keep) ;;
        remote)
            valid_url "$2" || die "ungueltige lapi-url"
            matches "$3" '^[A-Za-z0-9._@-]{1,128}$' || die "ungueltiger lapi-login"
            pw=$(b64dec "$4") || exit 1
            [ -n "$pw" ] && [ "${#pw}" -le 256 ] && [ "$(printf '%s' "$pw" | wc -l)" -eq 0 ] || die "ungueltiges lapi-passwort"
            LAPI_URL="$2"; LAPI_LOGIN="$3"; LAPI_PW_B64="$4"
            ;;
        console)
            matches "$5" '^[A-Za-z0-9]{1,128}$' || die "ungueltiger console-key"
            CONSOLE_KEY="$5"
            ;;
        *) die "ungueltiger lapi-modus" ;;
    esac
    shift 5
    [ $# -ge 1 ] || die "crowdsec-configure braucht mindestens eine allowlist-ip"
    IPS=""
    for ip in "$@"; do
        valid_cidr "$ip" || die "ungueltige allowlist-ip: $ip"
        IPS="${IPS:+$IPS }$ip"
    done
    @@SEC_CROWDSEC_CONFIGURE@@
}

# crowdsec-lapi-url: die LAPI-URL aus der (root-eigenen) Credentials-Datei.
crowdsec_lapi_url() {
    @@SEC_CROWDSEC_LAPI_URL@@
}

# security-service <fail2ban|crowdsec> <start|stop|restart|enable|disable>
security_service() {
    case "$1" in
        fail2ban|crowdsec) UNIT="$1" ;;
        *) die "security-service fail2ban|crowdsec <aktion>" ;;
    esac
    case "$2" in
        start)   @@SEC_SVC_START@@ ;;
        stop)    @@SEC_SVC_STOP@@ ;;
        restart) @@SEC_SVC_RESTART@@ ;;
        enable)  @@SEC_SVC_ENABLE@@ ;;
        disable) @@SEC_SVC_DISABLE@@ ;;
        *) die "security-service <tool> start|stop|restart|enable|disable" ;;
    esac
}

# security-purge <fail2ban|crowdsec>: LCMs Konfiguration des Tools entfernen.
security_purge() {
    case "$1" in
        fail2ban) @@SEC_PURGE_FAIL2BAN@@ ;;
        crowdsec) @@SEC_PURGE_CROWDSEC@@ ;;
        *) die "security-purge fail2ban|crowdsec" ;;
    esac
}

# security-unban <fail2ban|crowdsec> <ip> und security-bans <fail2ban|crowdsec>.
security_unban() {
    IP="$2"
    valid_cidr "$IP" || die "ungueltige ip"
    case "$1" in
        fail2ban) @@SEC_UNBAN_FAIL2BAN@@ ;;
        crowdsec) @@SEC_UNBAN_CROWDSEC@@ ;;
        *) die "security-unban fail2ban|crowdsec <ip>" ;;
    esac
}

security_bans() {
    case "$1" in
        fail2ban) @@SEC_BANS_FAIL2BAN@@ ;;
        crowdsec) @@SEC_BANS_CROWDSEC@@ ;;
        *) die "security-bans fail2ban|crowdsec" ;;
    esac
}

# reboot: den Server neu starten, vom SSH-Kanal abgekoppelt. Eigener Name, eine
# Shell-Funktion reboot verdeckte sonst den Befehl.
reboot_host() {
    @@REBOOT@@
}

cmd="${1:-}"
[ $# -ge 1 ] && shift
case "$cmd" in
    host-install)      [ $# -eq 1 ] || die "host-install trivy|sandbox|apt-cacher"; host_install "$@" ;;
    host-apt-cacher)   [ $# -eq 1 ] || die "host-apt-cacher restart|cache-on|cache-off"; host_apt_cacher "$@" ;;
    host-crowdsec-lapi) [ $# -eq 1 ] || die "host-crowdsec-lapi bouncer|plain"; host_crowdsec_lapi "$@" ;;
    host-self-update)  [ $# -eq 0 ] || die "host-self-update"; host_self_update ;;
    host-apt-channel)  [ $# -ge 1 ] || die "host-apt-channel community|beta|enterprise [...]"; host_apt_channel "$@" ;;
    package-pins)      [ $# -eq 3 ] || die "package-pins <apt|dnf|zypper|apk|pacman> <noremove-csv|-> <hold-csv|->"; package_pins "$@" ;;
    firewall-apply)    [ $# -ge 2 ] || die "firewall-apply nftables|firewalld <argument>..."; firewall_apply "$@" ;;
    firewall-disable)  [ $# -eq 1 ] || die "firewall-disable nftables|firewalld"; firewall_disable "$@" ;;
    firewall-status)   [ $# -eq 1 ] || die "firewall-status nftables|firewalld"; firewall_status "$@" ;;
    firewall-port)     [ $# -eq 3 ] || die "firewall-port allow|remove nftables|firewalld <port>"; firewall_port "$@" ;;
    fail2ban-config)   [ $# -ge 1 ] || die "fail2ban-config <ip>..."; fail2ban_config "$@" ;;
    crowdsec-repo)     [ $# -eq 1 ] || die "crowdsec-repo deb|rpm"; crowdsec_repo "$@" ;;
    crowdsec-collections) [ $# -ge 1 ] || die "crowdsec-collections <name>..."; crowdsec_collections "$@" ;;
    crowdsec-configure) [ $# -ge 6 ] || die "crowdsec-configure <modus> <url|-> <login|-> <passwort_b64|-> <console-key|-> <ip>..."; crowdsec_configure "$@" ;;
    crowdsec-lapi-url) [ $# -eq 0 ] || die "crowdsec-lapi-url"; crowdsec_lapi_url ;;
    security-service)  [ $# -eq 2 ] || die "security-service fail2ban|crowdsec <aktion>"; security_service "$@" ;;
    security-purge)    [ $# -eq 1 ] || die "security-purge fail2ban|crowdsec"; security_purge "$@" ;;
    security-unban)    [ $# -eq 2 ] || die "security-unban fail2ban|crowdsec <ip>"; security_unban "$@" ;;
    security-bans)     [ $# -eq 1 ] || die "security-bans fail2ban|crowdsec"; security_bans "$@" ;;
    reboot)            [ $# -eq 0 ] || die "reboot"; reboot_host ;;
    repo-add)    [ $# -eq 3 ] || die "repo-add <key> <keyurl_b64> <line_b64>"; repo_add "$@" ;;
    repos-https) repos_https "$@" ;;
    repos-http)  [ $# -ge 1 ] || die "repos-http <https-url>..."; repos_http "$@" ;;
    apt-proxy)   [ $# -eq 1 ] || die "apt-proxy <url>|off"; apt_proxy "$@" ;;
    dns)         [ $# -eq 2 ] || die "dns <ips-csv|off> <testdomain>"; dns "$@" ;;
    ssh-harden)  [ $# -eq 1 ] || die "ssh-harden on|off"; ssh_harden "$@" ;;
    ssh-options) [ $# -eq 2 ] || die "ssh-options <ports-csv|-> <root:yes|no>"; ssh_options "$@" ;;
    user-ensure) [ $# -ge 4 ] && [ $# -le 6 ] || die "user-ensure <name> <shell> <keys_b64> <sudo|nosudo> [fullname_b64|-] [password_b64|-]"; user_ensure "$@" ;;
    profile-apply)  [ $# -eq 2 ] || die "profile-apply <slug> <sudoers_b64>"; profile_apply "$@" ;;
    profile-member) [ $# -eq 2 ] || die "profile-member <name> <slug|->"; profile_member "$@" ;;
    profile-prune)  [ $# -le 1 ] || die "profile-prune [slugs-csv|-]"; profile_prune "${1:--}" ;;
    profile-acl)    [ $# -eq 4 ] || die "profile-acl set|remove <slug> <modus|-> <pfad>"; profile_acl "$@" ;;
    profile-sftp)   [ $# -eq 1 ] || die "profile-sftp <slugs-csv|->"; profile_sftp "$@" ;;
    user-remove) [ $# -eq 1 ] || die "user-remove <name>"; user_remove "$@" ;;
    user-lock)   [ $# -eq 1 ] || die "user-lock <name>"; user_lock "$@" ;;
    self-remove) [ $# -eq 0 ] || die "self-remove"; self_remove ;;
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
