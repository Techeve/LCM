package services

import (
	"strconv"
	"strings"
	"time"

	"LCM/internal/core/domain"
)

// storageHealthCmd erhebt den Zustand der Speicher-Verbünde unterhalb der
// reinen Belegung: ZFS-Pools, Btrfs-Dateisysteme, MD-RAID-Verbünde und
// LVM-Thin-Pools.
//
// Jeder Abschnitt prüft zuerst, ob es die Technik auf diesem System überhaupt
// gibt, und schweigt sonst - der Scan läuft auf jedem Server, die wenigsten
// haben ZFS. Alles ist rein lesend und kommt ohne zusätzliche Rechte aus,
// solange der Dienstbenutzer die Werkzeuge aufrufen darf.
//
// Ausgabe je Verbund als TSV:
// kind, name, rawstate, usage%, meta%, frag%, errors, scrub-epoch, message
//
// proc ist produktiv "/proc"; der Parameter macht die Auswertung von mounts
// und mdstat gegen Testdaten prüfbar (gleiches Muster wie aptRepoScanCmdIn).
func storageHealthCmdIn(proc string) string {
	return `T=$(printf '\t')

# --- ZFS ---------------------------------------------------------------
# capacity/fragmentation kommen mit angehängtem Prozentzeichen, die
# Fehlerzähler stehen in der Pool-Zeile von "zpool status" (READ WRITE CKSUM).
if command -v zpool >/dev/null 2>&1; then
  zpool list -H -o name,health,capacity,fragmentation 2>/dev/null | while IFS="$T" read -r name health cap frag; do
    [ -n "$name" ] || continue
    st=$(zpool status "$name" 2>/dev/null)
    errs=$(printf '%s\n' "$st" | awk -v p="$name" '$1==p && NF>=5 && $3 ~ /^[0-9]+$/ {print $3+$4+$5; exit}')
    scrub=$(printf '%s\n' "$st" | sed -n 's/.*scan:.* on \(.*\)$/\1/p' | head -1)
    epoch=$(date -d "$scrub" +%s 2>/dev/null || echo 0)
    msg=$(printf '%s\n' "$st" | sed -n 's/^errors: //p' | head -1)
    printf 'zfs\t%s\t%s\t%s\t0\t%s\t%s\t%s\t%s\n' \
      "$name" "$health" "${cap%\%}" "${frag%\%}" "${errs:-0}" "${epoch:-0}" "$msg"
  done
fi

# --- Btrfs -------------------------------------------------------------
# Je Dateisystem nur einmal (nach Gerät entdoppelt - Subvolumes hängen
# mehrfach). device stats zählt kumulativ: Jeder Wert über 0 gehört angesehen.
if command -v btrfs >/dev/null 2>&1; then
  awk '$3=="btrfs" && !seen[$1]++ {print $1"\t"$2}' ` + proc + `/mounts 2>/dev/null | while IFS="$T" read -r dev mp; do
    [ -n "$mp" ] || continue
    # Ohne Root verweigern sich beide Aufrufe. Dann darf hier NICHT
    # "ONLINE" stehen - das wäre ein falsches Gesund bei fehlendem Gerät.
    state=UNKNOWN
    if stats=$(btrfs device stats "$mp" 2>/dev/null) && show=$(btrfs filesystem show "$mp" 2>/dev/null); then
      state=ONLINE
      printf '%s\n' "$show" | grep -qi 'missing' && state=MISSING_DEV
    fi
    errs=$(printf '%s\n' "${stats:-}" | awk '{s+=$2} END{print s+0}')
    scrub=$(btrfs scrub status "$mp" 2>/dev/null | sed -n 's/.*[Ss]crub started:[ \t]*//p' | head -1)
    epoch=$(date -d "$scrub" +%s 2>/dev/null || echo 0)
    printf 'btrfs\t%s\t%s\t0\t0\t0\t%s\t%s\t\n' "$mp" "$state" "${errs:-0}" "${epoch:-0}"
  done
fi

# --- MD-RAID -----------------------------------------------------------
# Die Zeile unter dem Verbund trägt die Belegungsmaske [UU] bzw. [U_].
# Ein Unterstrich heißt: läuft weiter, aber ohne die vorgesehene Redundanz.
if [ -r ` + proc + `/mdstat ]; then
  awk '/^md[0-9]+ :/ {
         name=$1; raw=$3; getline line;
         state=raw;
         if (match(line, /\[[U_]+\]/)) {
           m=substr(line, RSTART, RLENGTH);
           if (index(m, "_") > 0) state="degraded";
         }
         sub(/^[ \t]+/, "", line);
         print "mdraid\t" name "\t" state "\t0\t0\t0\t0\t0\t" line;
       }' ` + proc + `/mdstat 2>/dev/null
fi

# --- LVM-Thin-Pools ----------------------------------------------------
# Nur Thin-Pools (lv_attr beginnt mit t). Der Metadaten-Anteil ist der
# gefährlichere der beiden Werte: Ist er voll, ist der Pool nicht zu retten.
if command -v lvs >/dev/null 2>&1; then
  lvs --noheadings --nosuffix --separator "$T" \
      -o vg_name,lv_name,lv_attr,data_percent,metadata_percent,lv_health_status 2>/dev/null |
  while IFS="$T" read -r vg lv attr data meta health; do
    attr=$(printf '%s' "$attr" | tr -d ' ')
    case "$attr" in t*) ;; *) continue;; esac
    vg=$(printf '%s' "$vg" | tr -d ' '); lv=$(printf '%s' "$lv" | tr -d ' ')
    printf 'lvm_thin\t%s/%s\t%s\t%s\t%s\t0\t0\t0\t\n' \
      "$vg" "$lv" "${health:-ok}" "${data%%.*}" "${meta%%.*}"
  done
fi`
}

var storageHealthCmd = storageHealthCmdIn("/proc")

// parseStorageHealth wertet die TSV-Ausgabe von storageHealthCmd aus.
func parseStorageHealth(out string) []domain.StorageHealth {
	var befunde []domain.StorageHealth
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(strings.TrimRight(line, "\r"), "\t")
		if len(f) < 9 || strings.TrimSpace(f[1]) == "" {
			continue
		}
		h := domain.StorageHealth{
			Kind:            strings.TrimSpace(f[0]),
			Name:            strings.TrimSpace(f[1]),
			RawState:        strings.TrimSpace(f[2]),
			UsagePercent:    atoiSafe(f[3]),
			MetaPercent:     atoiSafe(f[4]),
			FragmentPercent: atoiSafe(f[5]),
			Errors:          int64(atoiSafe(f[6])),
			Message:         strings.TrimSpace(f[8]),
		}
		if epoch := atoiSafe(f[7]); epoch > 0 {
			t := time.Unix(int64(epoch), 0)
			h.LastScrub = &t
		}
		h.State = storageState(&h)
		h.Message = storageMessage(&h)
		befunde = append(befunde, h)
	}
	return befunde
}

// zfsFaulted sind die ZFS-Zustände, bei denen der Pool nicht mehr benutzbar
// ist oder Daten bereits verloren sind. DEGRADED fehlt hier bewusst: Der Pool
// arbeitet noch, nur ohne Redundanz - dringend, aber nicht dasselbe.
var zfsFaulted = map[string]bool{
	"FAULTED": true, "UNAVAIL": true, "REMOVED": true, "SUSPENDED": true,
}

// storageState übersetzt die Rohangabe der jeweiligen Technik in den
// vereinheitlichten Zustand.
func storageState(h *domain.StorageHealth) string {
	roh := strings.ToUpper(strings.TrimSpace(h.RawState))
	switch h.Kind {
	case domain.StorageKindZFS:
		switch {
		case zfsFaulted[roh]:
			return domain.StorageStateFaulted
		case roh == "DEGRADED" || roh == "OFFLINE" || h.Errors > 0:
			return domain.StorageStateDegraded
		case roh == "ONLINE":
			return domain.StorageStateHealthy
		}
	case domain.StorageKindBtrfs:
		switch {
		case roh == "MISSING_DEV":
			return domain.StorageStateFaulted
		case h.Errors > 0:
			return domain.StorageStateDegraded
		case roh == "ONLINE":
			return domain.StorageStateHealthy
		}
	case domain.StorageKindMDRaid:
		switch {
		case strings.Contains(roh, "DEGRADED"):
			return domain.StorageStateDegraded
		case strings.Contains(roh, "ACTIVE"), strings.Contains(roh, "CLEAN"):
			return domain.StorageStateHealthy
		case roh == "INACTIVE":
			return domain.StorageStateFaulted
		}
	case domain.StorageKindLVMThin:
		// lv_health_status ist im Normalfall leer; "ok" setzt der Scan als
		// Platzhalter. Alles andere ist eine Beanstandung von LVM selbst.
		switch {
		case h.MetaPercent >= lvmThinCriticalPercent || h.UsagePercent >= lvmThinCriticalPercent:
			return domain.StorageStateFaulted
		case roh != "" && roh != "OK":
			return domain.StorageStateDegraded
		case h.MetaPercent >= lvmThinWarnPercent || h.UsagePercent >= lvmThinWarnPercent:
			return domain.StorageStateDegraded
		default:
			return domain.StorageStateHealthy
		}
	}
	return domain.StorageStateUnknown
}

// Grenzen für Thin-Pools. Sie sind bewusst niedriger als bei einem gewöhnlichen
// Dateisystem: Ein voller Thin-Pool lässt sich nicht mehr aufräumen - die
// Erweiterung braucht selbst freien Platz -, und ein volles Metadaten-Volume
// kostet den Pool endgültig. Wer hier erst bei 95% aufwacht, kommt zu spät.
const (
	lvmThinWarnPercent     = 80
	lvmThinCriticalPercent = 95
)

// storageMessage formuliert, was nicht stimmt. Leer, wenn alles in Ordnung
// ist - die Oberfläche zeigt dann nur den Zustand.
func storageMessage(h *domain.StorageHealth) string {
	if h.State == domain.StorageStateHealthy || h.State == domain.StorageStateUnknown {
		return ""
	}
	var teile []string
	switch h.Kind {
	case domain.StorageKindZFS:
		teile = append(teile, "Pool-Zustand "+h.RawState)
		if h.Errors > 0 {
			teile = append(teile, strconv.FormatInt(h.Errors, 10)+" Lese-/Schreib-/Prüfsummenfehler")
		}
	case domain.StorageKindBtrfs:
		if h.RawState == "MISSING_DEV" {
			teile = append(teile, "ein Gerät des Verbunds fehlt")
		}
		if h.Errors > 0 {
			teile = append(teile, strconv.FormatInt(h.Errors, 10)+" gezählte Gerätefehler (device stats)")
		}
	case domain.StorageKindMDRaid:
		teile = append(teile, "RAID-Verbund läuft ohne die vorgesehene Redundanz")
	case domain.StorageKindLVMThin:
		if h.MetaPercent >= lvmThinWarnPercent {
			teile = append(teile, "Metadaten zu "+strconv.Itoa(h.MetaPercent)+"% belegt")
		}
		if h.UsagePercent >= lvmThinWarnPercent {
			teile = append(teile, "Daten zu "+strconv.Itoa(h.UsagePercent)+"% belegt")
		}
		if r := strings.TrimSpace(h.RawState); r != "" && !strings.EqualFold(r, "ok") {
			teile = append(teile, "LVM meldet: "+r)
		}
	}
	if len(teile) == 0 {
		return h.Message
	}
	return strings.Join(teile, ", ")
}
