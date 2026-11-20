package services

import (
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/synology"
	"LCM/internal/storage/repositories"
)

// degradedDSM ist der Speicher aus dem DSM-7.2-Fixture des Clients: ein
// SHR-Pool in Reparatur mit ausgefallener Platte, eine SSD mit SMART-Warnung
// am Ende ihrer Lebensdauer, ein nur lesbares Volume.
func degradedDSM() *synology.Storage {
	return &synology.Storage{
		Pools: []synology.Pool{
			{ID: "reuse_1", DeviceType: "shr_with_1_disk_protect", Status: "degraded", RepairStep: "repairing", RepairPercent: 37,
				TotalBytes: 100, UsedBytes: 99},
			{ID: "reuse_2", DeviceType: "basic", Status: "normal", RepairPercent: -1},
		},
		Volumes: []synology.Volume{
			{ID: "volume_1", Path: "/volume1", FSType: "btrfs", Status: "degraded", PoolID: "reuse_1",
				TotalBytes: 7489556193280, UsedBytes: 5240121606144},
			{ID: "volume_2", Path: "/volume2", FSType: "ext4", Status: "normal", PoolID: "reuse_2", ReadOnly: true,
				TotalBytes: 367001600000, UsedBytes: 120259084288},
		},
		Disks: []synology.Disk{
			{ID: "sata1", Name: "Drive 1", Model: "HAT5300-8T", Status: "normal", SmartStatus: "normal", Temp: 36},
			{ID: "sata2", Name: "Drive 2", Model: "HAT5300-8T", Status: "crashed", SmartStatus: "failing", BadSectorsExceeded: true},
			{ID: "nvme0n1", Name: "M.2 Drive 1", Model: "SNV3410-400G", Status: "normal", SmartStatus: "attention", LifeBelowThreshold: true},
		},
	}
}

func healthByName(t *testing.T, all []domain.StorageHealth, name string) domain.StorageHealth {
	t.Helper()
	for _, h := range all {
		if h.Name == name {
			return h
		}
	}
	t.Fatalf("kein Zustand für %q in %+v", name, all)
	return domain.StorageHealth{}
}

// TestDSMSpeicherBefunde: Ausfall, SMART und degradiertes RAID werden zu
// Zuständen mit Schlüssel und Ampel-Stufe.
func TestDSMSpeicherBefunde(t *testing.T) {
	all := dsmStorageHealth(degradedDSM())
	if len(all) != 7 {
		t.Fatalf("erwartet 2 Pools + 2 Volumes + 3 Platten, got %d", len(all))
	}
	cases := []struct{ name, state, cause string }{
		{"reuse_1", domain.StorageStateDegraded, "dsmPoolRepairing"},
		{"reuse_2", domain.StorageStateHealthy, ""},
		{"/volume1", domain.StorageStateDegraded, "dsmVolumeDegraded"},
		{"/volume2", domain.StorageStateFaulted, "dsmVolumeReadOnly"},
		{"Drive 1", domain.StorageStateHealthy, ""},
		{"Drive 2", domain.StorageStateFaulted, "dsmDiskFailed"},
		{"M.2 Drive 1", domain.StorageStateDegraded, "dsmDiskSmart"},
	}
	for _, c := range cases {
		h := healthByName(t, all, c.name)
		if h.State != c.state || h.Cause != c.cause {
			t.Errorf("%s: %s/%s, erwartet %s/%s (%s)", c.name, h.State, h.Cause, c.state, c.cause, h.Message)
		}
	}
	drive2 := healthByName(t, all, "Drive 2")
	for _, want := range []string{"ausgefallen", "SMART meldet failing", "defekte Sektoren", "HAT5300-8T"} {
		if !strings.Contains(drive2.Message, want) {
			t.Errorf("Meldung nennt %q nicht: %s", want, drive2.Message)
		}
	}
	if pool := healthByName(t, all, "reuse_1"); !strings.Contains(pool.Message, "37 %") || pool.UsagePercent != 99 {
		t.Errorf("Reparaturfortschritt/Belegung fehlt: %+v", pool)
	}
}

// TestVirtualDSMOhneFehlalarm: VirtualDSM meldet weder SMART noch
// Temperatur - daraus darf kein Befund werden. Ein unbekannter Zustand ist
// ebenfalls kein Alarm.
func TestVirtualDSMOhneFehlalarm(t *testing.T) {
	st := &synology.Storage{
		Pools:   []synology.Pool{{ID: "reuse_1", DeviceType: "basic", Status: "normal", RepairPercent: -1}},
		Volumes: []synology.Volume{{ID: "volume_1", Path: "/volume1", FSType: "btrfs", Status: "normal"}},
		Disks: []synology.Disk{
			{ID: "sata1", Name: "Drive 1", Model: "Virtual Disk", Status: "normal"},
			{ID: "sata2", Name: "Drive 2", Status: "irgendwas_neues"},
		},
	}
	for _, h := range dsmStorageHealth(st) {
		if h.Beanstandet() {
			t.Errorf("Fehlalarm: %+v", h)
		}
	}
	if h := dsmDiskHealth(st.Disks[1]); h.State != domain.StorageStateUnknown {
		t.Errorf("unbekannter Plattenzustand = %s", h.State)
	}
}

// TestDSMVolumesFuerUeberwachung: Volumes landen mit ihrem Pfad als
// Mountpoint in disk_volumes - darüber hängt die Überwachung je Volume.
func TestDSMVolumesFuerUeberwachung(t *testing.T) {
	vols := dsmVolumes(degradedDSM())
	if len(vols) != 2 || vols[0].Mountpoint != "/volume1" || vols[0].Fstype != "btrfs" || vols[0].Device != "reuse_1" {
		t.Fatalf("Volumes falsch: %+v", vols)
	}
	if vols[0].TotalMB != 7142597 || !vols[0].Monitorable() || !vols[1].ReadOnly {
		t.Errorf("Größe/Überwachbarkeit falsch: %+v", vols)
	}
}

// TestDSMSollwerte: Jede Abweichung vom Soll wird erkannt, Unbekanntes
// bleibt unbekannt statt als Abweichung zu zählen.
func TestDSMSollwerte(t *testing.T) {
	on := synology.Setting{Known: true, On: true}
	off := synology.Setting{Known: true}
	unknown := synology.Setting{Reason: "Wert nicht deutbar", Raw: `{"x":1}`}
	soll := synology.Settings{
		Firewall: synology.Setting{Known: true, On: true, Text: "default"}, SkipIPCheck: off,
		CSRFProtection: on, CSP: on, IframeBlocked: on, SessionsCleared: on,
		AutoBlock: on, AutoBlockAttempts: 10, AutoBlockMinutes: 5, DoS: on,
		TLSProfile: synology.Setting{Known: true, On: true, Text: "modern"}, SpectreMeltdown: on,
	}
	for _, c := range dsmChecks(soll) {
		if c.State != domain.DSMCheckOK {
			t.Errorf("Soll erfüllt, aber %s = %s", c.Key, c.State)
		}
	}

	ist := soll
	ist.Firewall, ist.SkipIPCheck, ist.CSP = off, on, off
	ist.AutoBlockAttempts = 20
	ist.TLSProfile = synology.Setting{Known: true, Text: "intermediate"}
	ist.SpectreMeltdown = unknown
	got := map[string]domain.DSMCheck{}
	for _, c := range dsmChecks(ist) {
		got[c.Key] = c
	}
	for _, key := range []string{domain.DSMCheckFirewall, domain.DSMCheckSkipIPCheck, domain.DSMCheckCSP, domain.DSMCheckAutoBlock, domain.DSMCheckTLSProfile} {
		if got[key].State != domain.DSMCheckDeviation {
			t.Errorf("%s: Abweichung nicht erkannt (%+v)", key, got[key])
		}
	}
	if got[domain.DSMCheckAutoBlock].Params["attempts"] != "20" {
		t.Errorf("AutoBlock nennt die Werte nicht: %+v", got[domain.DSMCheckAutoBlock])
	}
	if got[domain.DSMCheckSpectre].State != domain.DSMCheckUnknown {
		t.Errorf("unbekannt darf keine Abweichung sein: %+v", got[domain.DSMCheckSpectre])
	}
	// Zu kurzes Zeitfenster ist ebenfalls zu großzügig.
	ist.AutoBlockAttempts, ist.AutoBlockMinutes = 5, 1
	if c := autoBlockCheck(ist); c.State != domain.DSMCheckDeviation {
		t.Errorf("1-Minuten-Fenster nicht beanstandet: %+v", c)
	}

	log := dsmSettingsLog(ist, dsmChecks(ist))
	if !strings.Contains(log, `Rohantwort: {"x":1}`) || !strings.Contains(log, "weicht vom Soll ab") {
		t.Errorf("Protokoll ohne Rohantwort oder Abweichung:\n%s", log)
	}
}

// TestDSMZustandInDenTabellen: Prüfungen, Firewall-Regeln, Volumes, Zustände und ein
// Verlaufspunkt landen in der Datenbank; ohne load_info bleibt der alte
// Stand stehen.
func TestDSMZustandInDenTabellen(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.Server{}, &domain.DiskVolume{}, &domain.StorageHealth{}, &domain.StorageHistory{}); err != nil {
		t.Fatal(err)
	}
	repo := repositories.NewServerRepository(db)
	server := &domain.Server{Name: "nas", Host: "10.0.0.5", OSID: domain.OSIDSynologyDSM}
	if err := repo.Create(server); err != nil {
		t.Fatal(err)
	}
	s := &ServerService{servers: repo}
	info := &synology.Info{Model: "DS923+", CPUModel: "AMD Ryzen R1600", Storage: degradedDSM(), VolumeTotalMB: 1000, VolumeUsedMB: 400}
	info.Settings.FirewallProfile = domain.DSMFirewall{Profile: "default", Sections: []domain.DSMFirewallSection{
		{Name: "global", Policy: "none", Rules: []domain.DSMFirewallRule{{Enabled: true, Policy: "drop", Ports: "all"}}},
	}}
	fresh := *server
	applyDSMInfo(&fresh, info)
	if err := repo.UpdateFields(server.ID, dsmFields(&fresh)); err != nil {
		t.Fatal(err)
	}
	s.storeDSMStorage(server.ID, info)

	var back domain.Server
	if err := db.First(&back, server.ID).Error; err != nil {
		t.Fatal(err)
	}
	if len(back.DSMChecks) != 10 || back.CPUModel != "AMD Ryzen R1600" {
		t.Errorf("Prüfungen/CPU nicht gespeichert: %d %q", len(back.DSMChecks), back.CPUModel)
	}
	if fw := back.DSMFirewall; fw.Profile != "default" || len(fw.Sections) != 1 || fw.Sections[0].Rules[0].Ports != "all" {
		t.Errorf("Firewall-Regeln nicht gespeichert: %+v", fw)
	}
	vols, _ := repo.FindDiskVolumes(server.ID)
	health, _ := repo.FindStorageHealth(server.ID)
	if len(vols) != 2 || len(health) != 7 {
		t.Errorf("Speicher nicht gespeichert: %d Volumes, %d Zustände", len(vols), len(health))
	}
	if last, _ := repo.LatestStorageSampleAt(server.ID); last.IsZero() {
		t.Error("kein Punkt im Speicher-Verlauf")
	}

	s.storeDSMStorage(server.ID, &synology.Info{})
	if health, _ = repo.FindStorageHealth(server.ID); len(health) != 7 {
		t.Errorf("ohne load_info wurde der Stand verworfen: %d", len(health))
	}
}
