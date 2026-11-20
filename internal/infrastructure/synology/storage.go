package synology

import "strings"

// Storage ist der Speicher eines DSM-Geräts aus SYNO.Storage.CGI.Storage
// load_info: Speicherpools (RAID-Verbünde), die Volumes darauf und die
// einzelnen Platten.
type Storage struct {
	Pools   []Pool
	Volumes []Volume
	Disks   []Disk
}

// Pool ist ein Speicherpool. DeviceType ist die RAID-Art, wie DSM sie
// nennt ("shr_with_1_disk_protect", "raid_5", "basic").
type Pool struct {
	ID         string
	DeviceType string
	Status     string // "normal", "degraded", "crashed" …
	TotalBytes int64
	UsedBytes  int64
	Disks      []string
	// RepairStep ist der laufende Vorgang aus progress.step ("none" fällt
	// weg), RepairPercent sein Fortschritt (-1 = unbekannt).
	RepairStep    string
	RepairPercent int
}

// Volume ist ein Volume, eingehängt unter Path ("/volume1").
type Volume struct {
	ID         string
	Path       string
	FSType     string
	Status     string
	PoolID     string
	TotalBytes int64
	UsedBytes  int64
	// ReadOnly: DSM meldet is_writable=false - nach einem Fehler hängt DSM
	// ein Volume nur lesbar ein, damit kein weiterer Schaden entsteht.
	ReadOnly bool
}

// Disk ist eine Platte. Fehlende Felder bleiben leer: VirtualDSM hat keine
// SMART-Werte und keine Temperatur, das ist kein Befund.
type Disk struct {
	ID          string
	Name        string // "Drive 1" bzw. "Laufwerk 1"
	Model       string
	Status      string
	SmartStatus string
	Temp        int // °C, 0 = nicht gemeldet
	PoolID      string
	// Schwellen, die DSM selbst bewertet: zu viele defekte Sektoren,
	// Restlebensdauer (SSD) unter der Warnschwelle.
	BadSectorsExceeded bool
	LifeBelowThreshold bool
}

// loadInfo ist die Antwortform von load_info, so tolerant wie nötig:
// Größen kommen als Zeichenketten, Prozente als "-1".
type loadInfo struct {
	Volumes []struct {
		ID         string   `json:"id"`
		Status     string   `json:"status"`
		VolPath    string   `json:"vol_path"`
		FSType     string   `json:"fs_type"`
		PoolPath   string   `json:"pool_path"`
		IsWritable flexBool `json:"is_writable"`
		Size       struct {
			Total flexInt `json:"total"`
			Used  flexInt `json:"used"`
		} `json:"size"`
	} `json:"volumes"`
	Pools []struct {
		ID         string   `json:"id"`
		Desc       string   `json:"desc"`
		DeviceType string   `json:"device_type"`
		RaidType   string   `json:"raidType"`
		Status     string   `json:"status"`
		Disks      []string `json:"disks"`
		Size       struct {
			Total flexInt `json:"total"`
			Used  flexInt `json:"used"`
		} `json:"size"`
		Progress struct {
			Percent flexInt `json:"percent"`
			Step    string  `json:"step"`
		} `json:"progress"`
	} `json:"storagePools"`
	Disks []struct {
		ID          string   `json:"id"`
		Name        string   `json:"name"`
		LongName    string   `json:"longName"`
		Model       string   `json:"model"`
		Status      string   `json:"status"`
		SmartStatus string   `json:"smart_status"`
		Temp        flexInt  `json:"temp"`
		UsedBy      string   `json:"used_by"`
		BadSectors  flexBool `json:"exceed_bad_sector_thr"`
		LifeLow     flexBool `json:"below_remain_life_thr"`
	} `json:"disks"`
}

// collectStorage liest Pools, Volumes und Platten und daraus die Summen für
// Belegung und den schlechtesten Volume-Zustand.
func (c *Client) collectStorage(info *Info) {
	var li loadInfo
	if err := c.call("SYNO.Storage.CGI.Storage", "1", "load_info", nil, &li); err != nil {
		return
	}
	st := li.toStorage()
	info.Storage = st
	var total, used int64
	for _, v := range st.Volumes {
		total += v.TotalBytes
		used += v.UsedBytes
		info.VolumeStatus = worseStatus(info.VolumeStatus, v.Status)
	}
	info.VolumeTotalMB = int(total / (1 << 20))
	info.VolumeUsedMB = int(used / (1 << 20))
}

// worseStatus behält den abweichenden Zustand: „normal" ist der gute Fall,
// jeder andere (degraded, crashed …) gehört sichtbar gemacht.
func worseStatus(cur, next string) string {
	if next != "" && next != "normal" {
		return next
	}
	if cur == "" {
		return next
	}
	return cur
}

func (li *loadInfo) toStorage() *Storage {
	st := &Storage{}
	for _, v := range li.Volumes {
		st.Volumes = append(st.Volumes, Volume{
			ID: v.ID, Path: firstNonEmpty(v.VolPath, "/"+strings.ReplaceAll(v.ID, "_", "")),
			FSType: v.FSType, Status: v.Status, PoolID: v.PoolPath,
			TotalBytes: int64(v.Size.Total), UsedBytes: int64(v.Size.Used),
			ReadOnly: v.IsWritable.Set && !v.IsWritable.Value,
		})
	}
	for _, p := range li.Pools {
		st.Pools = append(st.Pools, Pool{
			ID: p.ID, DeviceType: firstNonEmpty(p.DeviceType, p.Desc, p.RaidType),
			Status: p.Status, Disks: p.Disks,
			TotalBytes: int64(p.Size.Total), UsedBytes: int64(p.Size.Used),
			RepairStep: repairStep(p.Progress.Step), RepairPercent: int(p.Progress.Percent),
		})
	}
	for _, d := range li.Disks {
		st.Disks = append(st.Disks, Disk{
			ID: d.ID, Name: firstNonEmpty(d.LongName, d.Name, d.ID), Model: strings.TrimSpace(d.Model),
			Status: d.Status, SmartStatus: d.SmartStatus, Temp: int(d.Temp), PoolID: d.UsedBy,
			BadSectorsExceeded: d.BadSectors.Set && d.BadSectors.Value,
			LifeBelowThreshold: d.LifeLow.Set && d.LifeLow.Value,
		})
	}
	return st
}

// repairStep behält nur Vorgänge, die Redundanz wiederherstellen. Eine
// Datenbereinigung (Scrubbing) oder Erweiterung ist kein Befund.
func repairStep(step string) string {
	s := strings.ToLower(strings.TrimSpace(step))
	for _, marker := range []string{"repair", "rebuild", "resync", "recover"} {
		if strings.Contains(s, marker) {
			return s
		}
	}
	return ""
}
