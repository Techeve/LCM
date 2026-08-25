#!/bin/sh
# postinstall - sets up the LCM service after package installation:
# system user, directories, a production config with a stable JWT secret
# (only on the very first install) and the enabled systemd service.
#
# All output is plain ASCII on purpose: package messages end up in terminals,
# log files and CI output whose encoding we do not control, and German
# umlauts routinely turn into mojibake there. The German texts therefore use
# ue/ae/oe/ss.
set -e

LCM_USER=lcm
LCM_GROUP=lcm
DATA_DIR=/var/lib/lcm
CONF_DIR=/etc/lcm
CONF_FILE="$CONF_DIR/config.json"

# lcm_lang picks the output language: English by default, German only if the
# system is actually set to German.
#
# Order follows POSIX (LC_ALL beats LC_MESSAGES beats LANG). dpkg often runs
# maintainer scripts with a stripped environment, so we fall back to the
# system-wide locale configuration - otherwise a German system would still
# get English output.
lcm_lang() {
	_l="${LC_ALL:-}"
	[ -z "$_l" ] && _l="${LC_MESSAGES:-}"
	[ -z "$_l" ] && _l="${LANG:-}"
	# grep -E/cut/tr statt sed: BSD- und busybox-sed beherrschen die
	# \|-Alternation in BRE nicht, das Skript laeuft aber auf allen dreien.
	for _f in /etc/default/locale /etc/locale.conf; do
		[ -n "$_l" ] && break
		[ -r "$_f" ] || continue
		_l=$(grep -E '^[[:space:]]*(LANG|LC_ALL|LC_MESSAGES)=' "$_f" 2>/dev/null |
			head -n 1 | cut -d= -f2- | tr -d '"'\''' | tr -d '[:space:]')
	done
	case "$_l" in
	de | de_* | De_* | DE_*) echo de ;;
	*) echo en ;;
	esac
}

# 1. Dedicated, unprivileged system user (idempotent).
if ! getent group "$LCM_GROUP" >/dev/null 2>&1; then
	addgroup --system "$LCM_GROUP"
fi
if ! getent passwd "$LCM_USER" >/dev/null 2>&1; then
	adduser --system --ingroup "$LCM_GROUP" --home "$DATA_DIR" \
		--no-create-home --disabled-password --shell /usr/sbin/nologin \
		--gecos "LCM service" "$LCM_USER"
fi

# 2. Directories with matching permissions. /etc/lcm belongs to group lcm and is
#    group-writable: the service has to be able to write config.json - it adds
#    newly introduced options there, and a restore replaces the file. Without
#    that write permission the restore aborted at startup and, because the
#    staging directory stayed behind, kept aborting on every following start.
#    The systemd unit says the same (ReadWritePaths=/var/lib/lcm /etc/lcm);
#    the permissions now match that intent. Owner stays root, and nobody
#    outside the group can read the file - it holds the JWT secret.
mkdir -p "$DATA_DIR" "$CONF_DIR"
chown "$LCM_USER:$LCM_GROUP" "$DATA_DIR"
chown "root:$LCM_GROUP" "$CONF_DIR"
chmod 0750 "$DATA_DIR"
chmod 0770 "$CONF_DIR"

# 3. Create the production config only if none exists - that keeps the JWT
#    secret stable across reinstalls/upgrades (otherwise every restart would
#    invalidate all sessions).
if [ ! -e "$CONF_FILE" ]; then
	if command -v openssl >/dev/null 2>&1; then
		JWT_SECRET=$(openssl rand -base64 48 | tr -d '\n')
	else
		JWT_SECRET=$(head -c 48 /dev/urandom | base64 | tr -d '\n')
	fi
	cat > "$CONF_FILE" <<EOF
{
  "host": "0.0.0.0",
  "port": 9310,
  "database_path": "app.db",
  "jwt_secret": "$JWT_SECRET",
  "access_token_ttl_minutes": 60,
  "admin_initial_password": "",
  "log_level": "info",
  "access_log": true,
  "api_key_rate_limit_per_minute": 120,
  "allowed_ips": [],
  "trust_proxy_header": false
}
EOF
	chown "root:$LCM_GROUP" "$CONF_FILE"
	chmod 0660 "$CONF_FILE"
	NEW_INSTALL=1
fi

# 3a. Existing installations were set up read-only for the service (0640 in a
#     0750 directory). Correct that on upgrade too - otherwise exactly those
#     installations keep the broken restore.
if [ -e "$CONF_FILE" ]; then
	chown "root:$LCM_GROUP" "$CONF_FILE"
	chmod 0660 "$CONF_FILE"
fi

# 3b. Self-management: give LCM SSH access to this very machine, so it appears
#     as a managed server ("lcm-host") without anyone onboarding it by hand.
#     Without it the host-specific features (Trivy, apt-cacher-ng, CrowdSec
#     LAPI) stay out of reach on a fresh install.
#
#     SECURITY: This creates a service account with sudo rights and hands LCM
#     a key for it - the service can act as root on this machine afterwards.
#     That is the price of managing the host itself. Set LCM_NO_SELF_MANAGE=1
#     during installation to skip it:
#         LCM_NO_SELF_MANAGE=1 apt install lcm
#
#     The private key is written to a file readable only by the service user.
#     LCM reads it once on the next start, stores it encrypted and deletes the
#     file. It is NOT re-created for a host that was removed on purpose - LCM
#     remembers that decision.
SELF_ONBOARD="$DATA_DIR/self-onboard.json"
SVC_USER=lcm-svc

if [ "${LCM_NO_SELF_MANAGE:-0}" = "1" ]; then
	SELF_MANAGE=skipped
elif [ ! -d /run/systemd/system ]; then
	# No systemd - typically a container. There "localhost" is the container,
	# not a host worth managing.
	SELF_MANAGE=skipped
elif ! command -v sshd > /dev/null 2>&1 && [ ! -x /usr/sbin/sshd ]; then
	SELF_MANAGE=nossh
else
	if ! getent passwd "$SVC_USER" > /dev/null 2>&1; then
		adduser --system --group --home "/home/$SVC_USER" --shell /bin/sh \
			--disabled-password --gecos "LCM management account" "$SVC_USER" \
			> /dev/null 2>&1 || true
	fi
	if getent passwd "$SVC_USER" > /dev/null 2>&1; then
		SVC_HOME=$(getent passwd "$SVC_USER" | cut -d: -f6)
		mkdir -p "$SVC_HOME/.ssh"
		chmod 0700 "$SVC_HOME/.ssh"

		# One key pair per installation, generated locally - it never leaves
		# this machine.
		KEYFILE=$(mktemp)
		rm -f "$KEYFILE"
		if ssh-keygen -t ed25519 -N '' -C "lcm-self" -f "$KEYFILE" > /dev/null 2>&1; then
			grep -qxF "$(cat "$KEYFILE.pub")" "$SVC_HOME/.ssh/authorized_keys" 2> /dev/null ||
				cat "$KEYFILE.pub" >> "$SVC_HOME/.ssh/authorized_keys"
			chmod 0600 "$SVC_HOME/.ssh/authorized_keys"
			chown -R "$SVC_USER" "$SVC_HOME/.ssh"

			echo "$SVC_USER ALL=(ALL) NOPASSWD:ALL" > /etc/sudoers.d/lcm-svc
			chmod 0440 /etc/sudoers.d/lcm-svc

			# Handover file - only the service user may read it.
			umask 077
			printf '{"service_user":"%s","private_key_pem":%s,"public_key":%s,"restricted_sudo":false}\n' \
				"$SVC_USER" \
				"$(awk 'BEGIN{printf "\""} {gsub(/\\/,"\\\\"); printf "%s\\n", $0} END{printf "\""}' "$KEYFILE")" \
				"$(awk 'BEGIN{printf "\""} {gsub(/"/,"\\\""); printf "%s", $0} END{printf "\""}' "$KEYFILE.pub")" \
				> "$SELF_ONBOARD"
			chown "$LCM_USER:$LCM_GROUP" "$SELF_ONBOARD"
			chmod 0600 "$SELF_ONBOARD"
			SELF_MANAGE=ready
		fi
		rm -f "$KEYFILE" "$KEYFILE.pub"
	fi
fi

# 4. Register and start the systemd service (only if systemd is running).
if [ -d /run/systemd/system ]; then
	systemctl daemon-reload >/dev/null 2>&1 || true
	systemctl enable lcm.service >/dev/null 2>&1 || true
	# Restart on upgrade, start on first install.
	systemctl restart lcm.service >/dev/null 2>&1 || true
fi

# 5. First-login instructions.
if [ "${NEW_INSTALL:-0}" = "1" ]; then
	if [ "$(lcm_lang)" = "de" ]; then
		cat <<'EOF'

============================================================
  LCM wurde installiert und als Dienst gestartet.

  Weboberflaeche:  https://<server-ip>:9310
                   (HTTPS, selbst signiert - die Warnung im
                   Browser ist zu erwarten)
  Konfiguration:   /etc/lcm/config.json
  Daten/Backups:   /var/lib/lcm

  Das erste Admin-Passwort wurde einmalig ins Journal
  geschrieben. Anzeigen mit:

      journalctl -u lcm | grep -A3 'Admin account'

  SICHERHEITSHINWEIS: Der Dienst lauscht auf 0.0.0.0:9310.
  Fuer den Produktivbetrieb den Zugriff per Firewall
  einschraenken oder einen Reverse-Proxy mit gueltigem
  Zertifikat davorschalten. Bind-Adresse aendern in
  /etc/lcm/config.json
============================================================

EOF
	else
		cat <<'EOF'

============================================================
  LCM has been installed and started as a service.

  Web interface:  https://<server-ip>:9310
                  (HTTPS, self-signed - the browser warning
                  is expected)
  Configuration:  /etc/lcm/config.json
  Data/backups:   /var/lib/lcm

  The initial admin password was written to the journal
  once. Show it with:

      journalctl -u lcm | grep -A3 'Admin account'

  SECURITY NOTE: The service listens on 0.0.0.0:9310. For
  production use, restrict access with a firewall or put a
  reverse proxy with a valid certificate in front of it.
  Change the bind address in /etc/lcm/config.json
============================================================

EOF
	fi
fi

# 6. Self-management notice. Shown on every install, not just the first one -
#    granting a service root access on this machine must never be silent.
if [ "${SELF_MANAGE:-}" = "ready" ]; then
	if [ "$(lcm_lang)" = "de" ]; then
		cat <<EOF
  Selbstverwaltung: Dieser Rechner wurde als Server
  "lcm-host" eingerichtet und erscheint nach dem naechsten
  Start in der Uebersicht.

  Dafuer wurde das Konto "$SVC_USER" mit sudo-Rechten
  angelegt (/etc/sudoers.d/lcm-svc) und ein Schluessel dafuer
  hinterlegt. LCM kann auf diesem Rechner also als root
  handeln.

  Nicht gewuenscht? Den Server in der Weboberflaeche
  loeschen - er wird dann nicht wieder angelegt. Bei kuenftigen
  Installationen vorher abschalten mit:
      LCM_NO_SELF_MANAGE=1 apt install lcm

EOF
	else
		cat <<EOF
  Self-management: This machine was set up as the server
  "lcm-host" and appears in the overview after the next start.

  For that, the account "$SVC_USER" was created with sudo
  rights (/etc/sudoers.d/lcm-svc) and a key for it was stored.
  LCM can therefore act as root on this machine.

  Not wanted? Delete the server in the web interface - it will
  not be re-created. To opt out of future installs up front:
      LCM_NO_SELF_MANAGE=1 apt install lcm

EOF
	fi
fi

exit 0
