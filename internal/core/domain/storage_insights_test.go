package domain

import (
	"strings"
	"testing"
)

func schluessel(befunde []StatusInsight) []string {
	var k []string
	for _, b := range befunde {
		k = append(k, b.Key)
	}
	return k
}

func enthaelt(befunde []StatusInsight, key string) *StatusInsight {
	for i := range befunde {
		if befunde[i].Key == key {
			return &befunde[i]
		}
	}
	return nil
}

// TestVolleVolumesOhneAnordnungSchweigen ist die Kernregel: Ein Datenträger
// darf voll sein. Ein Archiv SOLL voll werden. Ohne ausdrückliche Anordnung
// wäre die Meldung Lärm - und Lärm schaltet man ab, samt der Meldungen, auf
// die es ankommt.
func TestVolleVolumesOhneAnordnungSchweigen(t *testing.T) {
	volumes := []DiskVolume{
		{Mountpoint: "/archiv", Fstype: "ext4", TotalMB: 1000, UsedMB: 990},
		{Mountpoint: "/daten", Fstype: "xfs", TotalMB: 1000, UsedMB: 999},
	}
	if befunde := volumeInsights(volumes, nil); len(befunde) != 0 {
		t.Errorf("ohne Anordnung erwartet kein Befund, bekam %v", schluessel(befunde))
	}
}

// TestAngeordnetesVolumeMeldetAbDerGrenze - und zwar erst ab ihr.
func TestAngeordnetesVolumeMeldetAbDerGrenze(t *testing.T) {
	volume := DiskVolume{Mountpoint: "/daten", Fstype: "ext4", TotalMB: 1000}
	monitor := VolumeMonitor{Mountpoint: "/daten", WarnPercent: 90}

	volume.UsedMB = 890 // 89% - noch darunter
	if befunde := volumeInsights([]DiskVolume{volume}, []VolumeMonitor{monitor}); len(befunde) != 0 {
		t.Errorf("unterhalb der Grenze erwartet kein Befund, bekam %v", schluessel(befunde))
	}

	volume.UsedMB = 910 // 91%
	befunde := volumeInsights([]DiskVolume{volume}, []VolumeMonitor{monitor})
	b := enthaelt(befunde, "volumeLow")
	if b == nil {
		t.Fatalf("oberhalb der Grenze erwartet volumeLow, bekam %v", schluessel(befunde))
	}
	if b.Severity != "warning" {
		t.Errorf("erwartet warning, bekam %q", b.Severity)
	}
	if b.Params["mountpoint"] != "/daten" || b.Params["limit"] != "90" {
		t.Errorf("die Meldung trägt den Mountpoint/die Grenze nicht mit: %v", b.Params)
	}
}

// TestKritischeGrenzeSchlaegtDieWarnung: Bei zwei gesetzten Grenzen darf nicht
// beides zugleich melden - sonst steht derselbe Sachverhalt doppelt da.
func TestKritischeGrenzeSchlaegtDieWarnung(t *testing.T) {
	volume := DiskVolume{Mountpoint: "/daten", Fstype: "ext4", TotalMB: 1000, UsedMB: 970}
	monitor := VolumeMonitor{Mountpoint: "/daten", WarnPercent: 80, CritPercent: 95}

	befunde := volumeInsights([]DiskVolume{volume}, []VolumeMonitor{monitor})
	if enthaelt(befunde, "volumeLow") != nil {
		t.Error("neben dem kritischen Befund steht zusätzlich die Warnung")
	}
	b := enthaelt(befunde, "volumeCritical")
	if b == nil || b.Severity != "critical" {
		t.Fatalf("erwartet kritischen Befund, bekam %v", schluessel(befunde))
	}
}

// TestNetzMountsBleibenUnueberwacht: Für Füllstand und Gesundheit ist der
// Speicher zuständig, der sie anbietet. Von hier aus sieht man nur die Sicht
// des Clients - jeder Netz-Aussetzer würde zum Alarm.
func TestNetzMountsBleibenUnueberwacht(t *testing.T) {
	for _, fs := range []string{"nfs4", "cifs", "fuse.sshfs", "glusterfs"} {
		volume := DiskVolume{Mountpoint: "/netz", Fstype: fs, TotalMB: 1000, UsedMB: 999}
		if volume.Monitorable() {
			t.Errorf("%s wird als überwachbar angeboten", fs)
		}
		// Selbst wenn eine Anordnung existiert - etwa weil das Volume früher
		// lokal war und später auf Netzspeicher umgestellt wurde.
		monitor := VolumeMonitor{Mountpoint: "/netz", WarnPercent: 80}
		if befunde := volumeInsights([]DiskVolume{volume}, []VolumeMonitor{monitor}); len(befunde) != 0 {
			t.Errorf("%s: trotz Netz-Mount gemeldet: %v", fs, schluessel(befunde))
		}
	}
	// Die Gegenprobe: Ein lokales Dateisystem bleibt überwachbar.
	lokal := DiskVolume{Mountpoint: "/daten", Fstype: "ext4", TotalMB: 1000}
	if !lokal.Monitorable() {
		t.Error("ein lokales ext4-Volume wird nicht als überwachbar angeboten")
	}
	// Das Root-Volume ist immer überwacht - es darf keinen Schalter bekommen,
	// sonst stünde derselbe Sachverhalt doppelt in den Befunden.
	root := DiskVolume{Mountpoint: "/", Fstype: "ext4", TotalMB: 1000}
	if root.Monitorable() {
		t.Error("das Root-Volume wird zur Überwachung angeboten")
	}
}

// TestInodesMeldenGetrennt: Ein Dateisystem kann dichtmachen, während df in
// Bytes noch reichlich Platz zeigt.
func TestInodesMeldenGetrennt(t *testing.T) {
	volume := DiskVolume{
		Mountpoint: "/var", Fstype: "ext4",
		TotalMB: 1000, UsedMB: 100, // 10% Platz belegt - unauffällig
		InodesTotal: 1000000, InodesUsed: 960000, // 96% Inodes
	}
	monitor := VolumeMonitor{Mountpoint: "/var", WarnPercent: 90}

	befunde := volumeInsights([]DiskVolume{volume}, []VolumeMonitor{monitor})
	if enthaelt(befunde, "volumeLow") != nil {
		t.Error("die Belegung ist unauffällig, wird aber gemeldet")
	}
	if b := enthaelt(befunde, "volumeInodes"); b == nil {
		t.Errorf("die erschöpften Inodes werden nicht gemeldet: %v", schluessel(befunde))
	}
}

// TestInodesOhneAngabeMeldenNicht: Meldet ein Dateisystem keine Inodes, darf
// die 0 nicht als „voll" durchgehen.
func TestInodesOhneAngabeMeldenNicht(t *testing.T) {
	volume := DiskVolume{Mountpoint: "/mnt", Fstype: "vfat", TotalMB: 1000, UsedMB: 100}
	monitor := VolumeMonitor{Mountpoint: "/mnt", WarnPercent: 90}
	if b := enthaelt(volumeInsights([]DiskVolume{volume}, []VolumeMonitor{monitor}), "volumeInodes"); b != nil {
		t.Errorf("ohne Inode-Angabe wurde gemeldet: %v", b)
	}
}

// TestInodesAufZfsSchlagenNichtAn: ZFS nennt eine gerechnete, praktisch
// unerschöpfliche Zahl - auf der Testumgebung 65 Millionen bei 1% Belegung.
// Die Quote darf dort nie zu einem Befund führen.
func TestInodesAufZfsSchlagenNichtAn(t *testing.T) {
	volume := DiskVolume{
		Mountpoint: "/", Fstype: "zfs", TotalMB: 32768, UsedMB: 884,
		InodesTotal: 65331522, InodesUsed: 32202,
	}
	monitor := VolumeMonitor{Mountpoint: "/", WarnPercent: 85}
	if b := enthaelt(volumeInsights([]DiskVolume{volume}, []VolumeMonitor{monitor}), "volumeInodes"); b != nil {
		t.Errorf("ZFS-Inodes haben einen Befund erzeugt: %v", b)
	}
}

// TestNurLesbaresRootMeldetImmer: Der Kernel hängt nach einem I/O-Fehler
// selbsttätig auf „ro" um. Das ist keine Frage der Anordnung.
func TestNurLesbaresRootMeldetImmer(t *testing.T) {
	root := DiskVolume{Mountpoint: "/", Fstype: "ext4", TotalMB: 1000, UsedMB: 100, ReadOnly: true}
	b := enthaelt(volumeInsights([]DiskVolume{root}, nil), "volumeReadOnly")
	if b == nil {
		t.Fatal("ein nur lesbares Root-Dateisystem wird nicht gemeldet")
	}
	if b.Severity != "critical" {
		t.Errorf("erwartet critical, bekam %q", b.Severity)
	}
}

// TestSpeicherVerbuendeMeldenOhneAnordnung: Ein degradierter Pool ist keine
// Geschmacksfrage - und genau das, was sonst niemand mitbekommt.
func TestSpeicherVerbuendeMeldenOhneAnordnung(t *testing.T) {
	befunde := storageHealthInsights([]StorageHealth{
		{Kind: StorageKindZFS, Name: "tank", State: StorageStateHealthy},
		{Kind: StorageKindZFS, Name: "backup", State: StorageStateDegraded, Message: "Pool-Zustand DEGRADED"},
		{Kind: StorageKindLVMThin, Name: "vg0/pool", State: StorageStateFaulted, Message: "Metadaten zu 97% belegt"},
	})
	if len(befunde) != 2 {
		t.Fatalf("erwartet zwei Befunde (der gesunde Pool schweigt), bekam %v", schluessel(befunde))
	}
	if !strings.Contains(befunde[0].Message, "ZFS-Pool backup") {
		t.Errorf("die Meldung benennt den Pool nicht: %q", befunde[0].Message)
	}
	if befunde[1].Severity != "critical" {
		t.Errorf("ein faulted Thin-Pool muss kritisch sein, ist aber %q", befunde[1].Severity)
	}
}

// TestUnbekannterZustandMeldetNicht: Fehlt das Werkzeug oder das Recht, ist
// das kein Befund. Nichtwissen ist keine Beanstandung.
func TestUnbekannterZustandMeldetNicht(t *testing.T) {
	befunde := storageHealthInsights([]StorageHealth{
		{Kind: StorageKindZFS, Name: "tank", State: StorageStateUnknown},
	})
	if len(befunde) != 0 {
		t.Errorf("unbekannter Zustand wurde gemeldet: %v", schluessel(befunde))
	}
}
