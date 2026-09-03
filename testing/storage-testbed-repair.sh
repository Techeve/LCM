#!/bin/sh
# storage-testbed-repair.sh - Gegenstück zu storage-testbed.sh: behebt alle
# vier Störfälle wieder.
#
# Der Zweck ist nicht das Aufräumen, sondern die GEGENPROBE. Ein Prüfwerk, das
# einen Defekt meldet, ist nur die halbe Miete - es muss ihn auch wieder
# zurücknehmen. Ein Befund, der hängenbleibt, nachdem die Platte längst
# getauscht ist, kostet genauso viel Vertrauen wie ein übersehener Defekt:
# Beim nächsten Mal glaubt ihn niemand mehr.
#
#   MD-RAID    Gerät wieder aufnehmen, Resync abwarten   -> [UU]
#   ZFS        vdev online nehmen, Resilver abwarten     -> ONLINE
#   Btrfs      Gerät ersetzen, auf RAID1 zurückwuchten   -> kein missing
#   LVM-Thin   Füll-Volume entfernen                     -> Chunks frei
#
# Aufruf als root, nach storage-testbed.sh.
#
# Die zugehörige Prüfung im Code steht in
# internal/core/services/storage_health_clear_test.go - sie bildet denselben
# Ablauf ohne Hardware nach (Störfall, Reparatur, Befund muss weg sein).
set -eu

DISK2=${DISK2:-/dev/vdb}
WORK=/var/lib/storage-testbed

log() { printf '\n==> %s\n' "$*"; }

# --- MD-RAID ---------------------------------------------------------------
if [ -e /dev/md0 ]; then
  log "MD-RAID: Gerät wieder aufnehmen"
  mdadm --add /dev/md0 "${DISK2}1" >/dev/null 2>&1 || true
  # Der Resync läuft im Hintergrund; bis er durch ist, steht der Verbund
  # weiter auf [U_] - die Prüfung danach wäre sonst zufällig.
  i=0
  while grep -qE 'resync|recovery' /proc/mdstat && [ $i -lt 120 ]; do
    sleep 5; i=$((i+1))
  done
  grep -A1 '^md0' /proc/mdstat
fi

# --- ZFS -------------------------------------------------------------------
if command -v zpool >/dev/null 2>&1 && zpool list tank >/dev/null 2>&1; then
  log "ZFS: vdev online nehmen"
  zpool online tank "$WORK/zfs-b.img" >/dev/null 2>&1 || true
  i=0
  while zpool status tank | grep -q 'resilver in progress' && [ $i -lt 60 ]; do
    sleep 5; i=$((i+1))
  done
  # Die Fehlerzähler bleiben nach einem Resilver stehen - sie sind kumulativ.
  # Ohne Zurücksetzen meldete LCM den Pool weiter als beanstandet, obwohl er
  # wieder vollständig ist. Genau dafür gibt es "zpool clear".
  zpool clear tank
  zpool list -H -o name,health tank
fi

# --- Btrfs -----------------------------------------------------------------
if command -v btrfs >/dev/null 2>&1 && mountpoint -q /mnt/btrfs; then
  log "Btrfs: fehlendes Gerät ersetzen"
  # Das alte Abbild trägt einen veralteten Stand desselben Dateisystems -
  # btrfs würde es als Doppelgänger abweisen. Deshalb leeren und als neues
  # Gerät aufnehmen, dann das fehlende entfernen.
  wipefs -aq "$WORK/btrfs-b.img" 2>/dev/null || true
  LB=$(losetup --show -f "$WORK/btrfs-b.img")
  btrfs device add -f "$LB" /mnt/btrfs >/dev/null
  btrfs device remove missing /mnt/btrfs >/dev/null 2>&1 || true
  # Nach dem Tausch liegen die Blöcke einseitig - erst das Wuchten stellt
  # die Spiegelung wieder her.
  btrfs balance start -dconvert=raid1 -mconvert=raid1 /mnt/btrfs >/dev/null 2>&1 || true
  btrfs filesystem show /mnt/btrfs | grep -i missing && echo "(noch missing)" || echo "kein fehlendes Gerät mehr"
fi

# --- LVM-Thin --------------------------------------------------------------
if command -v lvs >/dev/null 2>&1 && lvs vgtest/pool >/dev/null 2>&1; then
  log "LVM-Thin: Füll-Volume entfernen"
  # thin2 diente nur dazu, den Pool volllaufen zu lassen. Mit ihm gehen alle
  # von ihm belegten Chunks zurück an den Pool.
  lvremove -y vgtest/thin2 >/dev/null 2>&1 || true
  lvs -o lv_name,data_percent,metadata_percent vgtest
fi

log "Erwartung nach der Reparatur"
cat <<'EOF'
  Alle vier Verbünde melden wieder "healthy" - LCM darf KEINEN Befund
  mehr führen. Bleibt einer stehen, ist der Fehler in LCM, nicht hier.
Prüfen (nach dem nächsten Scan):
  python3 -c "import sqlite3;[print(r) for r in sqlite3.connect('/var/lib/lcm/app.db').execute('SELECT kind,state,message FROM storage_healths')]"
EOF
