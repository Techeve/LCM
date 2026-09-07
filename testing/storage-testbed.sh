#!/bin/sh
# storage-testbed.sh - stellt in einer Test-VM alle vier Störfälle her, die
# die Speicher-Diagnose von LCM erkennen soll:
#
#   MD-RAID    degradiert     ([U_] in /proc/mdstat)
#   LVM-Thin   Metadaten voll (metadata_percent > 80)
#   Btrfs      Gerät fehlt    (filesystem show: "missing")
#   ZFS        DEGRADED       (ein vdev des Mirrors offline)
#
# Voraussetzung: Debian/Ubuntu-VM mit ZWEI zusätzlichen leeren Platten.
# Vorgabe /dev/vda und /dev/vdb - so heißen virtio-blk-Platten, wenn die
# Systemplatte über SCSI (sda) hängt, wie in der Proxmox-Test-VM 113.
# Überschreibbar per DISK1/DISK2. Btrfs und ZFS laufen über Dateien als
# Geräte und brauchen keine eigene Platte.
#
# Danach LCM aus dem mitgegebenen .deb installieren (LCM_DEB=/pfad/lcm.deb):
# Es registriert seinen eigenen Host und scannt ihn - die Befunde stehen dann
# in storage_healths, ohne dass ein API-Key nötig wäre.
#
# Aufruf als root. Idempotent nur in Grenzen - für einen sauberen zweiten
# Lauf die VM auf den Snapshot zurückrollen.
set -eu

DISK1=${DISK1:-/dev/vda}
DISK2=${DISK2:-/dev/vdb}
WORK=/var/lib/storage-testbed
mkdir -p "$WORK"

log() { printf '\n==> %s\n' "$*"; }

log "Werkzeuge"
export DEBIAN_FRONTEND=noninteractive
# Ein frisches Cloud-Image hat leere Paketlisten - und auf Debian liegt ZFS
# in "contrib", das dort nicht eingeschaltet ist. zfs-dkms baut das Modul
# gegen die Kernel-Header; das dauert auf zwei Kernen einige Minuten.
if [ -f /etc/apt/sources.list.d/debian.sources ]; then
  sed -i 's/^Components: main$/Components: main contrib/' /etc/apt/sources.list.d/debian.sources
fi
apt-get update -q >/dev/null
apt-get install -y -q mdadm lvm2 btrfs-progs parted >/dev/null
ZFS_OK=yes
HEADERS=linux-headers-$(uname -r)
apt-cache show "$HEADERS" >/dev/null 2>&1 || HEADERS=linux-headers-cloud-amd64
apt-get install -y -q "$HEADERS" zfs-dkms zfsutils-linux >/dev/null 2>&1 && modprobe zfs || ZFS_OK=no
[ "$ZFS_OK" = yes ] || echo "ZFS nicht verfuegbar - Abschnitt wird uebersprungen" >&2
# Die Platten dürfen nicht schon belegt sein - sonst stünde hier ein
# halbfertiger Zustand aus einem früheren Lauf.
for d in "$DISK1" "$DISK2"; do
  [ -b "$d" ] || { echo "Platte $d fehlt" >&2; exit 1; }
  wipefs -aq "$d"
done

# --- MD-RAID: Mirror aus zwei Partitionen, danach eine ausfallen lassen ------
log "MD-RAID1 auf $DISK1/$DISK2 und degradieren"
for d in "$DISK1" "$DISK2"; do
  parted -s "$d" mklabel gpt mkpart raid 1MiB 2049MiB mkpart lvm 2049MiB 100%
done
# Direkt nach parted kennt der Kernel die Partitionen noch nicht - ohne das
# Warten scheitert mdadm an einem /dev/vda1, das es noch nicht gibt.
partprobe "$DISK1" "$DISK2" 2>/dev/null || true
udevadm settle 2>/dev/null || sleep 3
for p in "${DISK1}1" "${DISK2}1" "${DISK1}2"; do
  i=0; until [ -b "$p" ] || [ $i -ge 20 ]; do sleep 1; i=$((i+1)); done
  [ -b "$p" ] || { echo "Partition $p erscheint nicht" >&2; exit 1; }
done
mdadm --create /dev/md0 --level=1 --raid-devices=2 --run --metadata=1.2 \
  "${DISK1}1" "${DISK2}1"
sleep 2
mdadm --fail /dev/md0 "${DISK2}1" >/dev/null
mdadm --remove /dev/md0 "${DISK2}1" >/dev/null
grep -A1 '^md0' /proc/mdstat

# --- LVM-Thin: winzige Metadaten, dann volllaufen lassen ---------------------
log "LVM-Thin-Pool mit vollen Metadaten"
pvcreate -y "${DISK1}2" >/dev/null
vgcreate vgtest "${DISK1}2" >/dev/null
# 2 MiB Metadaten sind das Minimum - sie sind schnell voll.
lvcreate -y -L 512M --poolmetadatasize 2M -T vgtest/pool >/dev/null
lvcreate -y -V 400M -T vgtest/pool -n thin1 >/dev/null
mkfs.ext4 -q /dev/vgtest/thin1
mkdir -p /mnt/thin && mount /dev/vgtest/thin1 /mnt/thin
# Der Pool füllt sich über die Zahl ZUGEORDNETER Chunks, nicht über Dateien:
# Ein Dateisystem im Thin-Volume schreibt in längst zugeordnete Blöcke, die
# Zuordnungen wachsen nicht (43.000 kleine Dateien: Metadaten unverändert).
# Deshalb ein 20-GB-Thin-Volume und je ein Byte pro 64-KiB-Chunk direkt aufs
# Gerät - bis der Pool voll ist und mit EIO abweist. Genau so sieht der
# Störfall im Betrieb aus.
lvcreate -y -V 20G -T vgtest/pool -n thin2 >/dev/null
python3 - <<'PY_FILL' || true
import os
fd = os.open("/dev/vgtest/thin2", os.O_WRONLY)
try:
    for i in range(400000):
        os.pwrite(fd, b"x", i * 65536)
        if i % 20000 == 0:
            os.fsync(fd)
except OSError as e:
    print("Pool voll:", e)
os.close(fd)
PY_FILL
lvs -o lv_name,data_percent,metadata_percent vgtest

# --- Btrfs: RAID1 aus zwei Dateien, dann eines "verlieren" ------------------
log "Btrfs RAID1 mit fehlendem Gerät"
truncate -s 1G "$WORK/btrfs-a.img" "$WORK/btrfs-b.img"
LA=$(losetup --show -f "$WORK/btrfs-a.img")
LB=$(losetup --show -f "$WORK/btrfs-b.img")
mkfs.btrfs -q -d raid1 -m raid1 "$LA" "$LB"
mkdir -p /mnt/btrfs && mount "$LA" /mnt/btrfs
echo test > /mnt/btrfs/probe && sync
umount /mnt/btrfs
losetup -d "$LB"
# Ohne das zweite Gerät nur degradiert einhängbar - genau der Zustand, in
# dem "filesystem show" das Gerät als missing führt.
mount -o degraded "$LA" /mnt/btrfs
btrfs filesystem show /mnt/btrfs | grep -i missing || echo "(kein missing - Kernel/Version prüfen)"

# --- ZFS: Mirror aus zwei Dateien, ein vdev offline -------------------------
if [ "$ZFS_OK" = yes ]; then
  log "ZFS-Mirror degradieren"
  truncate -s 1G "$WORK/zfs-a.img" "$WORK/zfs-b.img"
  zpool create -f tank mirror "$WORK/zfs-a.img" "$WORK/zfs-b.img"
  zpool offline tank "$WORK/zfs-b.img"
  zpool list -H -o name,health
fi

# --- LCM ----------------------------------------------------------------------
if [ -n "${LCM_DEB:-}" ] && [ -f "$LCM_DEB" ]; then
  log "LCM installieren"
  dpkg -i "$LCM_DEB" >/dev/null 2>&1 || apt-get -f install -y -q >/dev/null
  systemctl is-active lcm
fi

log "Erwartete Befunde"
cat <<'EOF'
  mdraid    md0            degraded
  lvm_thin  vgtest/pool    faulted (Daten 100 %, Schreibzugriffe scheitern mit EIO)
  btrfs     /mnt/btrfs     faulted (MISSING_DEV)
  zfs       tank           degraded (DEGRADED)
Btrfs und LVM brauchen Root: LCM läuft dafür über sudo. Im eingeschränkten
Modus melden beide "unknown" - nie ein falsches Gesund.
Prüfen mit:
  sqlite3 -line /var/lib/lcm/app.db "SELECT kind, state, usage_percent, meta_percent FROM storage_healths;"
EOF
