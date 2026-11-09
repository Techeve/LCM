#!/bin/sh
# probe-postinstall - replaces the old measuring script and starts the
# service. Plain ASCII on purpose (see postinstall.sh).
set -e

OLD_SCRIPT=/usr/local/sbin/lcm-probe
OLD_UNIT=/etc/systemd/system/lcm-probe.service

# The first lcm-probe was a shell script with its own unit, placed by hand on
# lcm-prod in October 2026. Its unit has the same name and lives in /etc, so it
# would shadow the packaged one. Remove both - but only if the unit really
# belongs to that script, never a unit someone wrote for the new binary.
remove_old_probe() {
	[ -f "$OLD_UNIT" ] || return 0
	grep -q "$OLD_SCRIPT" "$OLD_UNIT" || return 0
	if [ -d /run/systemd/system ]; then
		systemctl stop lcm-probe.service >/dev/null 2>&1 || true
		systemctl disable lcm-probe.service >/dev/null 2>&1 || true
	fi
	rm -f "$OLD_UNIT" "$OLD_SCRIPT"
	echo "lcm-probe: old script $OLD_SCRIPT and its unit removed (logs in /var/log/lcm-probe kept)"
}

remove_old_probe

chmod 0700 /etc/lcm-probe

if [ -d /run/systemd/system ]; then
	systemctl daemon-reload >/dev/null 2>&1 || true
	systemctl enable lcm-probe.service >/dev/null 2>&1 || true
	systemctl restart lcm-probe.service >/dev/null 2>&1 || true
fi

if [ ! -s /etc/lcm-probe/key ]; then
	cat <<'MSG'

============================================================
  lcm-probe measures the host now. For the database it
  needs an LCM API key with scope "read" from an admin:

      echo '<key>' > /etc/lcm-probe/key
      chmod 600 /etc/lcm-probe/key

  No restart needed. Report: lcm-probe report
============================================================

MSG
fi

exit 0
