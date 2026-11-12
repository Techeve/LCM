package services

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/synology"
)

// Speicher eines DSM-Geräts in der gemeinsamen Speicher-Überwachung.
//
// Volumes landen in disk_volumes wie die Dateisysteme eines Linux-Servers -
// mit ihrem Pfad (/volume1) als Mountpoint, damit eine dort angeordnete
// Überwachung (volume_monitors) jeden Scan überlebt. Pools, Volumes und
// Platten landen mit ihrem Zustand in storage_healths; Ausfall, SMART-Warnung
// und degradiertes RAID werden damit zu Befunden mit Ampel, ohne Sonderweg.

// bytesPerMB rechnet die Byte-Angaben von DSM in die MB der Tabellen um.
const bytesPerMB = 1 << 20

// dsmVolumes bildet die DSM-Volumes auf disk_volumes ab.
func dsmVolumes(st *synology.Storage) []domain.DiskVolume {
	vols := make([]domain.DiskVolume, 0, len(st.Volumes))
	for _, v := range st.Volumes {
		vols = append(vols, domain.DiskVolume{
			Mountpoint: v.Path, Device: firstNonEmpty(v.PoolID, v.ID), Fstype: v.FSType,
			TotalMB: v.TotalBytes / bytesPerMB, UsedMB: v.UsedBytes / bytesPerMB,
			ReadOnly: v.ReadOnly,
		})
	}
	return vols
}

// dsmStorageHealth bewertet Pools, Volumes und Platten.
func dsmStorageHealth(st *synology.Storage) []domain.StorageHealth {
	var out []domain.StorageHealth
	for _, p := range st.Pools {
		out = append(out, dsmPoolHealth(p))
	}
	for _, v := range st.Volumes {
		out = append(out, dsmVolumeHealth(v))
	}
	for _, d := range st.Disks {
		out = append(out, dsmDiskHealth(d))
	}
	return out
}

// Zustände, die DSM für Pools und Volumes meldet.
var (
	dsmStatusFaulted  = map[string]bool{"crashed": true, "failed": true, "broken": true}
	dsmStatusDegraded = map[string]bool{"degraded": true, "attention": true, "warning": true, "repairing": true}
)

// dsmUnifiedState übersetzt einen DSM-Zustand. Unbekanntes bleibt unbekannt
// und ist kein Befund.
func dsmUnifiedState(status string) string {
	s := strings.ToLower(strings.TrimSpace(status))
	switch {
	case s == "normal":
		return domain.StorageStateHealthy
	case dsmStatusFaulted[s]:
		return domain.StorageStateFaulted
	case dsmStatusDegraded[s]:
		return domain.StorageStateDegraded
	}
	return domain.StorageStateUnknown
}

func dsmPoolHealth(p synology.Pool) domain.StorageHealth {
	h := domain.StorageHealth{
		Kind: domain.StorageKindDSMPool, Name: p.ID, RawState: p.Status,
		State: dsmUnifiedState(p.Status), UsagePercent: percentOf(p.UsedBytes, p.TotalBytes),
	}
	raid := firstNonEmpty(p.DeviceType, "RAID")
	switch {
	case h.State == domain.StorageStateFaulted:
		h.Cause, h.Message = "dsmPoolCrashed", raid+" ist ausgefallen - Daten auf diesem Pool sind gefährdet"
	case p.RepairStep != "":
		// Eine laufende Reparatur heißt: bis zum Abschluss ohne volle
		// Redundanz. Fällt jetzt eine weitere Platte aus, ist der Pool weg.
		h.State, h.Cause = domain.StorageStateDegraded, "dsmPoolRepairing"
		h.Message = raid + " wird repariert (" + p.RepairStep + progressText(p.RepairPercent) + ") - bis zum Abschluss ohne volle Redundanz"
	case h.State == domain.StorageStateDegraded:
		h.Cause, h.Message = "dsmPoolDegraded", raid+" läuft ohne die vorgesehene Redundanz - Platten prüfen und im Speicher-Manager reparieren"
	}
	return h
}

func progressText(percent int) string {
	if percent < 0 {
		return ""
	}
	return ", " + strconv.Itoa(percent) + " %"
}

func dsmVolumeHealth(v synology.Volume) domain.StorageHealth {
	h := domain.StorageHealth{
		Kind: domain.StorageKindDSMVolume, Name: v.Path, RawState: v.Status,
		State: dsmUnifiedState(v.Status), UsagePercent: percentOf(v.UsedBytes, v.TotalBytes),
	}
	switch {
	case h.State == domain.StorageStateFaulted:
		h.Cause, h.Message = "dsmVolumeCrashed", "Volume ist abgestürzt - Daten sichern, im Speicher-Manager prüfen"
	case v.ReadOnly:
		// DSM hängt ein Volume nach einem Fehler nur lesbar ein. Bis zum
		// ersten Schreibversuch merkt das sonst niemand.
		h.State, h.Cause = domain.StorageStateFaulted, "dsmVolumeReadOnly"
		h.Message = "Volume ist nur lesbar eingehängt - DSM hat es nach einem Fehler abgesichert"
	case h.State == domain.StorageStateDegraded:
		h.Cause, h.Message = "dsmVolumeDegraded", "Volume ist beeinträchtigt - Zustand des Pools darunter prüfen"
	}
	return h
}

// Platten: DSMs eigene Bewertung (status, smart_status, Schwellen). Fehlt
// ein Feld - VirtualDSM hat kein SMART -, gibt es dazu auch keinen Befund.
var (
	dsmDiskFailed   = map[string]bool{"crashed": true, "failing": true, "failed": true, "broken": true, "system_partition_failed": true}
	dsmDiskFine     = map[string]bool{"normal": true, "initialized": true, "not_initialized": true}
	dsmSmartFine    = map[string]bool{"": true, "normal": true, "safe": true, "-": true}
	dsmSmartFailing = map[string]bool{"failing": true, "failed": true, "danger": true, "damaged": true, "bad": true}
)

func dsmDiskHealth(d synology.Disk) domain.StorageHealth {
	h := domain.StorageHealth{
		Kind: domain.StorageKindDSMDisk, Name: d.Name, RawState: d.Status, State: domain.StorageStateHealthy,
	}
	status := strings.ToLower(strings.TrimSpace(d.Status))
	if !dsmDiskFine[status] && !dsmDiskFailed[status] {
		h.State = domain.StorageStateUnknown
	}
	befunde := dsmDiskFindings(d)
	if len(befunde) == 0 {
		return h
	}
	// Der schwerste Befund bestimmt Zustand und Schlüssel, die Meldung nennt alle.
	h.State, h.Cause = domain.StorageStateDegraded, befunde[0].cause
	var teile []string
	for _, b := range befunde {
		teile = append(teile, b.text)
		if b.faulted && h.State != domain.StorageStateFaulted {
			h.State, h.Cause = domain.StorageStateFaulted, b.cause
		}
	}
	h.Message = strings.Join(teile, ", ") + diskModel(d.Model) + " - Austausch vorbereiten"
	return h
}

// diskFinding ist ein einzelner Befund an einer Platte.
type diskFinding struct {
	faulted     bool
	cause, text string
}

func dsmDiskFindings(d synology.Disk) []diskFinding {
	var out []diskFinding
	status := strings.ToLower(strings.TrimSpace(d.Status))
	if dsmDiskFailed[status] {
		out = append(out, diskFinding{true, "dsmDiskFailed", "Laufwerk ausgefallen (DSM: " + d.Status + ")"})
	}
	if smart := strings.ToLower(strings.TrimSpace(d.SmartStatus)); !dsmSmartFine[smart] {
		out = append(out, diskFinding{dsmSmartFailing[smart], "dsmDiskSmart", "SMART meldet " + d.SmartStatus})
	}
	if d.BadSectorsExceeded {
		out = append(out, diskFinding{false, "dsmDiskBadSectors", "defekte Sektoren über der Warnschwelle"})
	}
	if d.LifeBelowThreshold {
		out = append(out, diskFinding{false, "dsmDiskWearOut", "Restlebensdauer unter der Warnschwelle"})
	}
	return out
}

func diskModel(model string) string {
	if model == "" {
		return ""
	}
	return " (" + model + ")"
}

func percentOf(used, total int64) int {
	if total <= 0 {
		return 0
	}
	return int(used * 100 / total)
}

// storeDSMStorage schreibt den erhobenen Speicher in die Tabellen und hält
// höchstens stündlich einen Punkt im Speicher-Verlauf fest (wie der
// Health-Check bei Linux). Ohne lesbares load_info bleibt alles stehen -
// eine leere Antwort ist keine Entwarnung.
func (s *ServerService) storeDSMStorage(id uint, info *synology.Info) {
	if info.Storage == nil {
		return
	}
	_ = s.servers.ReplaceDiskVolumes(id, dsmVolumes(info.Storage))
	_ = s.servers.ReplaceStorageHealth(id, dsmStorageHealth(info.Storage))
	if info.VolumeTotalMB <= 0 {
		return
	}
	last, err := s.servers.LatestStorageSampleAt(id)
	if err == nil && !last.IsZero() && time.Since(last) < storageSampleInterval {
		return
	}
	now := time.Now()
	_ = s.servers.RecordStorageSample(id, now.Format("2006-01-02"),
		int64(info.VolumeTotalMB), int64(info.VolumeUsedMB), now)
}

// dsmStorageLog beschreibt Pools, Volumes und Platten fürs Job-Protokoll.
func dsmStorageLog(st *synology.Storage) string {
	if st == nil {
		return "Speicher: load_info nicht lesbar, letzter Stand bleibt.\n"
	}
	var b strings.Builder
	for _, p := range st.Pools {
		fmt.Fprintf(&b, "Pool %s: %s, %s, %d Platte(n)\n", p.ID, firstNonEmpty(p.DeviceType, "?"), p.Status, len(p.Disks))
	}
	for _, v := range st.Volumes {
		fmt.Fprintf(&b, "Volume %s (%s): %s, %d von %d MB belegt\n",
			v.Path, v.FSType, v.Status, v.UsedBytes/bytesPerMB, v.TotalBytes/bytesPerMB)
	}
	for _, d := range st.Disks {
		fmt.Fprintf(&b, "Laufwerk %s %s: %s, SMART %s, %s\n", d.Name, d.Model, d.Status,
			firstNonEmpty(d.SmartStatus, "nicht gemeldet"), tempText(d.Temp))
	}
	return b.String()
}

func tempText(temp int) string {
	if temp <= 0 {
		return "Temperatur nicht gemeldet"
	}
	return strconv.Itoa(temp) + " °C"
}
