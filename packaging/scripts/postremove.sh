#!/bin/sh
# postremove - cleans up after removal.
#
# "remove" keeps everything: database, master key and backups stay, so that a
# later reinstall picks up where the old installation stopped.
#
# "purge" removes what LCM created on this host, down to the files LCM marks
# as its own. It does NOT touch Linux users that LCM distributed to this host:
# their home directories belong to people, not to LCM. Firewall rules that
# LCM set with ufw stay as well - ufw does not reliably tell them apart.
set -e

# Verschluesseltes Hauptschluessel-Credential (lcm credentials init) und das
# Unit-Drop-in, das es laedt. Die Ordner bleiben nur, wenn sie anderes enthalten.
remove_credentials() {
	rm -f /etc/credstore.encrypted/lcm.key
	rm -f /etc/systemd/system/lcm.service.d/credentials.conf
	rmdir /etc/systemd/system/lcm.service.d /etc/credstore.encrypted 2>/dev/null || true
}

# Den sshd-Include, den LCM oben in die sshd_config geschrieben hat, rueckgaengig
# machen: Die Sicherung ist das Original, es wird komplett zurueckgespielt.
restore_sshd_config() {
	conf=/etc/ssh/sshd_config
	backup=/etc/ssh/sshd_config.lcm-backup
	if [ -f "$backup" ] && head -n 1 "$conf" 2>/dev/null | grep -q 'von LCM erg'; then
		cp -f "$backup" "$conf" && rm -f "$backup"
	fi
	# Neustart statt reload: Der sshd auf Debian 13 ist socket-aktiviert; ein
	# SIGHUP-Neuladen fand dort keinen Port mehr und beendete den Dienst. LCM
	# selbst laedt den sshd genauso neu (sshd_reload im Helper).
	mkdir -p /run/sshd
	if command -v sshd >/dev/null 2>&1 && sshd -t >/dev/null 2>&1; then
		if systemctl is-active --quiet ssh.service 2>/dev/null; then
			systemctl restart ssh >/dev/null 2>&1 || true
		elif systemctl is-active --quiet sshd.service 2>/dev/null; then
			systemctl restart sshd >/dev/null 2>&1 || true
		fi
	fi
}

# Einstellungen, die LCM auf diesem Rechner gesetzt hat. Jede Datei traegt den
# Namen von LCM; Installationen fremder Pakete (Trivy, bubblewrap, apt-cacher-ng)
# bleiben installiert.
remove_host_settings() {
	rm -f /usr/local/sbin/lcm-helper
	rm -f /etc/apt/apt.conf.d/02lcm-apt-cache
	rm -f /etc/ssh/sshd_config.d/10-lcm-ssh.conf \
		/etc/ssh/sshd_config.d/55-lcm-2fa.conf \
		/etc/ssh/sshd_config.d/00-lcm-hardening.conf \
		/etc/ssh/sshd_config.d/60-lcm-hardening.conf
	restore_sshd_config
	if command -v nft >/dev/null 2>&1; then
		nft delete table inet lcm >/dev/null 2>&1 || true
	fi
	rm -f /etc/nftables.d/lcm.nft
}

# Das Verwaltungskonto lcm-svc, das LCM beim Installieren auf diesem Rechner
# anlegt. Entfernt wird es nur, wenn sein Schluessel den LCM-Kommentar traegt -
# ein von Hand angelegtes Konto gleichen Namens bleibt unangetastet.
remove_management_account() {
	home=$(getent passwd lcm-svc | cut -d: -f6)
	if [ -n "$home" ] && grep -q ' lcm-self$' "$home/.ssh/authorized_keys" 2>/dev/null; then
		rm -f /etc/sudoers.d/lcm-svc
		deluser --remove-home lcm-svc >/dev/null 2>&1 || true
	fi
}

if [ "$1" = "purge" ]; then
	remove_credentials
	remove_host_settings
	remove_management_account
	rm -rf /var/lib/lcm /etc/lcm
	if getent passwd lcm >/dev/null 2>&1; then
		deluser --system lcm >/dev/null 2>&1 || true
	fi
	if getent group lcm >/dev/null 2>&1; then
		delgroup --system lcm >/dev/null 2>&1 || true
	fi
fi

if [ -d /run/systemd/system ]; then
	systemctl daemon-reload >/dev/null 2>&1 || true
fi

exit 0
