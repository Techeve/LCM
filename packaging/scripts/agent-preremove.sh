#!/bin/sh
# agent-preremove - stops the service before any files are removed.
set -e

if [ -d /run/systemd/system ]; then
	systemctl stop lcm-agent.service >/dev/null 2>&1 || true
	if [ "$1" = "remove" ] || [ "$1" = "0" ]; then
		systemctl disable lcm-agent.service >/dev/null 2>&1 || true
	fi
fi

exit 0
