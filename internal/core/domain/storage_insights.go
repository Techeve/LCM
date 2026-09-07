package domain

// Bewertung der zusätzlich überwachten Volumes und der Speicher-Verbünde.
//
// Zwei Dinge werden hier bewusst unterschiedlich behandelt:
//
//   - Die BELEGUNG eines Volumes außer „/" wird nur bewertet, wenn der
//     Betreiber es ausdrücklich angeordnet hat. Ein Datenträger, der zu 95%
//     voll ist, kann völlig in Ordnung sein - ein Archiv soll voll werden.
//     Ohne diese Ansage wäre die Meldung Lärm, und Lärm schaltet man ab.
//
//   - Der ZUSTAND eines Speicher-Verbunds wird immer bewertet. Ein
//     DEGRADED-Pool ist kein Geschmacksfrage, kostet nichts an Erhebung und
//     ist genau das, was sonst niemand mitbekommt.

// volumeInsights bewertet die ausdrücklich überwachten Volumes.
func volumeInsights(volumes []DiskVolume, monitore []VolumeMonitor) []StatusInsight {
	nachMount := make(map[string]*VolumeMonitor, len(monitore))
	for i := range monitore {
		nachMount[monitore[i].Mountpoint] = &monitore[i]
	}

	var befunde []StatusInsight
	for i := range volumes {
		v := &volumes[i]

		// Ein nur lesbares Root-Dateisystem ist immer ein Notfall - dafür
		// braucht es keine Anordnung. Der Kernel hängt nach einem I/O-Fehler
		// selbsttätig auf „ro" um; bis zum ersten Schreibversuch merkt es
		// sonst niemand.
		if v.ReadOnly && v.IsRoot() {
			befunde = append(befunde, insight("critical", "volumeReadOnly",
				"Das Root-Dateisystem ist nur lesbar eingehängt - der Kernel hat es nach einem Fehler abgesichert",
				map[string]string{"mountpoint": v.Mountpoint}))
		}

		m := nachMount[v.Mountpoint]
		// Netz-Mounts sind nicht überwachbar (siehe DiskVolume.Monitorable).
		// Die Prüfung steht hier ein zweites Mal: Wird ein Volume später auf
		// einen Netzspeicher umgestellt, bliebe die einmal gesetzte Anordnung
		// sonst wirksam und meldete bei jedem Netz-Aussetzer.
		if m == nil || !v.Monitorable() {
			continue
		}

		if v.ReadOnly && !v.IsRoot() {
			befunde = append(befunde, insight("critical", "volumeReadOnly",
				"Volume "+v.Mountpoint+" ist nur lesbar eingehängt - der Kernel hat es nach einem Fehler abgesichert",
				map[string]string{"mountpoint": v.Mountpoint}))
		}

		belegung := v.UsagePercent()
		switch {
		case m.CritPercent > 0 && belegung >= m.CritPercent:
			befunde = append(befunde, insight("critical", "volumeCritical",
				"Volume "+v.Mountpoint+" zu "+itoa(belegung)+"% belegt (kritisch ab "+itoa(m.CritPercent)+"%)",
				volumeParams(v.Mountpoint, belegung, m.CritPercent)))
		case belegung >= m.EffectiveWarnPercent():
			befunde = append(befunde, insight("warning", "volumeLow",
				"Volume "+v.Mountpoint+" zu "+itoa(belegung)+"% belegt (Grenze "+itoa(m.EffectiveWarnPercent())+"%)",
				volumeParams(v.Mountpoint, belegung, m.EffectiveWarnPercent())))
		}

		// Inodes getrennt: Ein Dateisystem kann dichtmachen, während df in
		// Bytes noch reichlich Platz zeigt.
		if inoden := v.InodeUsagePercent(); inoden >= m.EffectiveInodeWarnPercent() && v.InodesTotal > 0 {
			befunde = append(befunde, insight("warning", "volumeInodes",
				"Volume "+v.Mountpoint+": Inodes zu "+itoa(inoden)+"% belegt - das Dateisystem kann volllaufen, obwohl noch Platz frei ist",
				volumeParams(v.Mountpoint, inoden, m.EffectiveInodeWarnPercent())))
		}
	}
	return befunde
}

func volumeParams(mountpoint string, percent, limit int) map[string]string {
	return map[string]string{
		"mountpoint": mountpoint,
		"percent":    itoa(percent),
		"limit":      itoa(limit),
	}
}

// storageHealthInsights bewertet den Zustand der Speicher-Verbünde.
func storageHealthInsights(befundeIn []StorageHealth) []StatusInsight {
	var befunde []StatusInsight
	for i := range befundeIn {
		h := &befundeIn[i]
		if !h.Beanstandet() {
			continue
		}
		befunde = append(befunde, insight(h.Severity(), "storageDefect",
			storageKindLabel(h.Kind)+" "+h.Name+": "+h.Message,
			map[string]string{
				"kind": h.Kind, "kindLabel": storageKindLabel(h.Kind), "name": h.Name,
				"state": h.State, "message": h.Message,
			}))
	}
	return befunde
}

// storageKindLabel benennt die Technik so, wie ein Betreiber sie nennt.
func storageKindLabel(kind string) string {
	switch kind {
	case StorageKindZFS:
		return "ZFS-Pool"
	case StorageKindBtrfs:
		return "Btrfs-Dateisystem"
	case StorageKindMDRaid:
		return "RAID-Verbund"
	case StorageKindLVMThin:
		return "LVM-Thin-Pool"
	}
	return "Speicher"
}
