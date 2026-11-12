package services

// dnsShellFuncs setzt die Nameserver auf dem Server und nimmt sie wieder
// zurück. Beide Modi nutzen genau diesen Text: das Inline-Skript im
// Voll-Sudo-Modus (dnsApplyScript) und der Helper über @@DNS_FUNCS@@.
//
// Wirksam heißt: Anwendungen fragen danach wirklich diese Server, auch nach
// einem Neustart. Drei Stellen verhindern das sonst:
//
//   - Proxmox schreibt /etc/resolv.conf eines Containers bei jedem Start neu,
//     ohne eigene DNS-Angabe mit den Servern des Hosts. Dabei wird ein
//     Symlink zu systemd-resolved durch eine eigene Datei ersetzt, und
//     Anwendungen umgehen den Dienst. /etc/.pve-ignore.resolv.conf schaltet
//     das ab.
//   - systemd-resolved bevorzugt die DNS-Server, die DHCP je Schnittstelle
//     mitgibt. Domains=~. leitet alle Anfragen an die Server von LCM.
//   - Ohne systemd-resolved schreiben DHCP-Clients die Datei bei jeder
//     Lease neu: dhclient, NetworkManager, udhcpc.
//
// Was LCM dafür anlegt, trägt die Marke LCM_DNS_MARK. lcm_dns_unset räumt nur
// solche Dateien wieder ab, nie eine, die der Betreiber selbst angelegt hat.
const dnsShellFuncs = `LCM_DNS_MARK='# LCM-DNS'
LCM_DNS_DROPIN=/etc/systemd/resolved.conf.d/lcm-dns.conf
LCM_DNS_RESOLV=/etc/resolv.conf

lcm_dns_resolved() {
    command -v resolvectl >/dev/null 2>&1 && systemctl is-active --quiet systemd-resolved 2>/dev/null
}

lcm_dns_resolve_ok() { getent hosts "$1" >/dev/null 2>&1 || nslookup "$1" >/dev/null 2>&1; }

lcm_dns_in_lxc() {
    [ "$(systemd-detect-virt -c 2>/dev/null)" = lxc ] || grep -qa 'container=lxc' /proc/1/environ 2>/dev/null
}

lcm_dns_exists() { [ -e "$1" ] || [ -L "$1" ]; }

# lcm_dns_guard_on resolved|file: Proxmox und DHCP-Clients lassen
# /etc/resolv.conf danach in Ruhe. Mit systemd-resolved genuegt Proxmox,
# die DHCP-Server landen dann je Schnittstelle im Dienst.
lcm_dns_guard_on() {
    if lcm_dns_in_lxc && ! lcm_dns_exists /etc/.pve-ignore.resolv.conf; then
        echo "$LCM_DNS_MARK" > /etc/.pve-ignore.resolv.conf
        echo 'LCM: Proxmox ueberschreibt /etc/resolv.conf beim Containerstart nicht mehr'
    fi
    [ "$1" = resolved ] && return 0
    if [ -d /etc/dhcp ] && [ ! -f /etc/dhcp/dhclient-enter-hooks.d/zz-lcm-dns ]; then
        mkdir -p /etc/dhcp/dhclient-enter-hooks.d
        printf '%s\nmake_resolv_conf() { :; }\n' "$LCM_DNS_MARK" > /etc/dhcp/dhclient-enter-hooks.d/zz-lcm-dns
        echo 'LCM: dhclient ueberschreibt /etc/resolv.conf nicht mehr'
    fi
    if [ -d /etc/NetworkManager/conf.d ] && [ ! -f /etc/NetworkManager/conf.d/90-lcm-dns.conf ]; then
        printf '%s\n[main]\ndns=none\n' "$LCM_DNS_MARK" > /etc/NetworkManager/conf.d/90-lcm-dns.conf
        systemctl reload NetworkManager 2>/dev/null || true
        echo 'LCM: NetworkManager ueberschreibt /etc/resolv.conf nicht mehr'
    fi
    if [ -f /usr/share/udhcpc/default.script ] && ! grep -q "$LCM_DNS_MARK" /etc/udhcpc/udhcpc.conf 2>/dev/null; then
        mkdir -p /etc/udhcpc
        printf '\nRESOLV_CONF=no %s\n' "$LCM_DNS_MARK" >> /etc/udhcpc/udhcpc.conf
        echo 'LCM: udhcpc ueberschreibt /etc/resolv.conf nicht mehr'
    fi
    return 0
}

lcm_dns_guard_off() {
    if grep -qx "$LCM_DNS_MARK" /etc/.pve-ignore.resolv.conf 2>/dev/null; then
        rm -f /etc/.pve-ignore.resolv.conf
    fi
    if grep -qx "$LCM_DNS_MARK" /etc/dhcp/dhclient-enter-hooks.d/zz-lcm-dns 2>/dev/null; then
        rm -f /etc/dhcp/dhclient-enter-hooks.d/zz-lcm-dns
    fi
    if grep -qx "$LCM_DNS_MARK" /etc/NetworkManager/conf.d/90-lcm-dns.conf 2>/dev/null; then
        rm -f /etc/NetworkManager/conf.d/90-lcm-dns.conf
        systemctl reload NetworkManager 2>/dev/null || true
    fi
    if grep -q "^RESOLV_CONF=no $LCM_DNS_MARK\$" /etc/udhcpc/udhcpc.conf 2>/dev/null; then
        sed -i "/^RESOLV_CONF=no $LCM_DNS_MARK\$/d" /etc/udhcpc/udhcpc.conf
    fi
    return 0
}

# lcm_dns_link_stub: /etc/resolv.conf muss zu systemd-resolved fuehren,
# sonst gilt das Drop-in fuer keine Anwendung.
lcm_dns_link_stub() {
    case "$(readlink "$LCM_DNS_RESOLV" 2>/dev/null)" in
        */run/systemd/resolve/*) return 0 ;;
    esac
    _lcm_stub=stub-resolv.conf
    [ -e /run/systemd/resolve/stub-resolv.conf ] || _lcm_stub=resolv.conf
    ln -sf "../run/systemd/resolve/$_lcm_stub" "$LCM_DNS_RESOLV"
    echo 'LCM: /etc/resolv.conf fuehrt jetzt ueber systemd-resolved, vorher wurde der Dienst umgangen'
}

# lcm_dns_set "<ip> <ip> ..." <testdomain>: setzen, Aufloesung pruefen, bei
# Fehlschlag auf den Stand vor diesem Lauf zurueck. resolv.conf.lcm-bak haelt
# den Stand vor der ersten Verwaltung durch LCM, *.lcm-prev den vor diesem Lauf.
lcm_dns_set() {
    _lcm_ips="$1"; _lcm_dom="$2"
    _lcm_bak="$LCM_DNS_RESOLV.lcm-bak"; _lcm_prev="$LCM_DNS_RESOLV.lcm-prev"
    if [ -z "$_lcm_ips" ]; then echo 'LCM: keine DNS-Server angegeben'; return 1; fi
    _lcm_managed=no
    if lcm_dns_exists "$LCM_DNS_DROPIN" || lcm_dns_exists "$_lcm_bak"; then _lcm_managed=yes; fi
    rm -f "$_lcm_prev" "$LCM_DNS_DROPIN.lcm-prev"
    # Ohne Sicherung kein Eingriff: Ein Rollback ohne sie liesse den Server
    # ohne /etc/resolv.conf zurueck.
    if lcm_dns_exists "$LCM_DNS_RESOLV"; then
        if ! cp -P "$LCM_DNS_RESOLV" "$_lcm_prev" || { ! lcm_dns_exists "$_lcm_bak" && ! cp -P "$LCM_DNS_RESOLV" "$_lcm_bak"; }; then
            rm -f "$_lcm_prev"
            echo 'LCM: Sicherung von /etc/resolv.conf fehlgeschlagen - nichts geaendert'
            return 1
        fi
    fi
    if lcm_dns_resolved; then
        _lcm_mode=systemd-resolved
        install -d -m 755 /etc/systemd/resolved.conf.d
        if [ -f "$LCM_DNS_DROPIN" ]; then cp -f "$LCM_DNS_DROPIN" "$LCM_DNS_DROPIN.lcm-prev"; fi
        printf '[Resolve]\nDNS=%s\nDomains=~.\n' "$_lcm_ips" > "$LCM_DNS_DROPIN"
        lcm_dns_guard_on resolved
        systemctl restart systemd-resolved 2>/dev/null || true
        lcm_dns_link_stub
    else
        _lcm_mode=/etc/resolv.conf
        lcm_dns_guard_on file
        for _lcm_ip in $_lcm_ips; do echo "nameserver $_lcm_ip"; done > "$LCM_DNS_RESOLV.lcm-new"
        mv -f "$LCM_DNS_RESOLV.lcm-new" "$LCM_DNS_RESOLV"
    fi
    if lcm_dns_resolve_ok "$_lcm_dom"; then
        rm -f "$_lcm_prev" "$LCM_DNS_DROPIN.lcm-prev" "$LCM_DNS_DROPIN.lcm-bak"
        echo "LCM: DNS gesetzt ($_lcm_mode): $_lcm_ips"
        return 0
    fi
    if [ -f "$LCM_DNS_DROPIN.lcm-prev" ]; then
        mv -f "$LCM_DNS_DROPIN.lcm-prev" "$LCM_DNS_DROPIN"
    elif [ "$_lcm_mode" = systemd-resolved ]; then
        rm -f "$LCM_DNS_DROPIN"
    fi
    if lcm_dns_exists "$_lcm_prev"; then mv -f "$_lcm_prev" "$LCM_DNS_RESOLV"; else rm -f "$LCM_DNS_RESOLV"; fi
    if [ "$_lcm_managed" = no ]; then
        lcm_dns_guard_off
        rm -f "$_lcm_bak"
    fi
    if [ "$_lcm_mode" = systemd-resolved ]; then systemctl restart systemd-resolved 2>/dev/null || true; fi
    echo 'LCM: DNS-Test nach dem Setzen fehlgeschlagen - zurueckgerollt'
    return 1
}

# lcm_dns_unset: Verwaltung abgeben, Ausgangszustand wiederherstellen.
lcm_dns_unset() {
    rm -f "$LCM_DNS_DROPIN" "$LCM_DNS_DROPIN.lcm-bak" "$LCM_DNS_DROPIN.lcm-prev"
    if lcm_dns_exists "$LCM_DNS_RESOLV.lcm-bak"; then mv -f "$LCM_DNS_RESOLV.lcm-bak" "$LCM_DNS_RESOLV"; fi
    lcm_dns_guard_off
    if lcm_dns_resolved; then systemctl restart systemd-resolved 2>/dev/null || true; fi
    echo 'LCM: DNS-Verwaltung entfernt'
}`
