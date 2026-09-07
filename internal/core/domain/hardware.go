package domain

import (
	"strconv"
	"strings"
)

// Hardware-Erkennung: welches Gerät steckt unter der Distribution?
//
// /etc/os-release beantwortet nur die halbe Frage. Ein Raspberry Pi meldet
// sich dort als Debian - dass es ein Einplatinenrechner mit einem Bruchteil
// der Leistung eines Servers ist, steht nirgends. Genau diese Information
// entscheidet aber, wie lange ein Paket-Upgrade dauern darf (siehe
// IsSlowHardware) und welches Logo die Oberfläche zeigt.
//
// Quellen: /proc/device-tree/model (ARM/Einplatinenrechner) und die
// DMI-Tabelle unter /sys/class/dmi/id (x86). Der Revisions-Code aus
// /proc/cpuinfo dient als Rückfallebene für sehr alte Pi-Systeme ohne
// Device-Tree und liefert zusätzlich den SoC.

// IsRaspberryPi meldet, ob das erfasste Gerät ein Raspberry Pi ist.
func (s *Server) IsRaspberryPi() bool {
	return strings.Contains(strings.ToLower(s.HardwareModel), "raspberry pi")
}

// Schwellen für IsSlowHardware: bis zu zwei Kerne UND höchstens 2 GB RAM
// zählen als schwach. Beides muss zutreffen - ein Zwei-Kern-Server mit 16 GB
// ist kein Sorgenkind, und eine Kiste mit 1 GB RAM und acht Kernen gibt es
// praktisch nicht.
const (
	slowHardwareMaxCores = 2
	slowHardwareMaxMemMB = 2048
)

// IsSlowHardware meldet, ob das Gerät für lange Läufe (Paket-Upgrades,
// dpkg-Trigger) deutlich mehr Zeit braucht als ein normaler Server.
//
// Der Raspberry Pi zählt unabhängig von seinen Eckdaten dazu: sein Flaschenhals
// ist die SD-Karte, nicht die CPU - ein Pi 4 mit vier Kernen und 8 GB RAM
// braucht für dieselbe dpkg-Trigger-Kette trotzdem ein Vielfaches der Zeit.
func (s *Server) IsSlowHardware() bool {
	if s.IsRaspberryPi() {
		return true
	}
	if s.CPUCores <= 0 || s.MemTotalMB <= 0 {
		return false // unbekannt ⇒ keine Sonderbehandlung
	}
	return s.CPUCores <= slowHardwareMaxCores && s.MemTotalMB <= slowHardwareMaxMemMB
}

// --- Raspberry-Pi-Revisionscode ---------------------------------------------

// Der Revisions-Code aus /proc/cpuinfo kodiert im „neuen" Format (Bit 23
// gesetzt) das Modell bitweise. Maßgeblich ist die Aufstellung der Raspberry
// Pi Ltd. („Raspberry Pi revision codes"):
//
//	Bits 4-11  Typ (Modell)
//	Bits 12-15 Prozessor (SoC)
//	Bits 20-22 Speichergröße
//	Bit  23    Neues Format
//
// Alte Codes (Pi 1 / Zero, Bit 23 = 0) sind eine schlichte Liste.
const (
	rpiNewFormatBit = 1 << 23
	rpiTypeShift    = 4
	rpiTypeMask     = 0xff
	rpiProcShift    = 12
	rpiProcMask     = 0xf
)

var rpiTypes = map[uint64]string{
	0x00: "Model A", 0x01: "Model B", 0x02: "Model A+", 0x03: "Model B+",
	0x04: "2 Model B", 0x06: "Compute Module 1", 0x08: "3 Model B",
	0x09: "Zero", 0x0a: "Compute Module 3", 0x0c: "Zero W",
	0x0d: "3 Model B+", 0x0e: "3 Model A+", 0x10: "Compute Module 3+",
	0x11: "4 Model B", 0x12: "Zero 2 W", 0x13: "400",
	0x14: "Compute Module 4", 0x15: "Compute Module 4S",
	0x17: "5 Model B", 0x18: "Compute Module 5", 0x19: "500",
	0x1a: "Compute Module 5 Lite",
}

var rpiSoCs = map[uint64]string{
	0: "BCM2835", 1: "BCM2836", 2: "BCM2837", 3: "BCM2711", 4: "BCM2712",
}

// rpiOldRevisions sind die Codes vor Einführung des Bitfelds (Pi 1 und Zero).
// Mehrere Codes zeigen auf dasselbe Modell (Fertigungsstätte/Speicherausbau).
var rpiOldRevisions = map[uint64]string{
	0x0002: "Model B", 0x0003: "Model B", 0x0004: "Model B", 0x0005: "Model B",
	0x0006: "Model B", 0x0007: "Model A", 0x0008: "Model A", 0x0009: "Model A",
	0x000d: "Model B", 0x000e: "Model B", 0x000f: "Model B",
	0x0010: "Model B+", 0x0011: "Compute Module 1", 0x0012: "Model A+",
	0x0013: "Model B+", 0x0014: "Compute Module 1", 0x0015: "Model A+",
}

// rpiRevMask blendet die Warranty-/Overvoltage-Bits (24-31) aus: Sie
// verändern den Code, ohne etwas über das Modell zu sagen.
const rpiRevMask = 0xffffff

// RaspberryFromRevision übersetzt einen Revisions-Code aus /proc/cpuinfo in
// Modellnamen und SoC. Ist der Code keiner bekannten Fassung zuzuordnen,
// bleiben beide Rückgaben leer.
//
// Sie ist die Rückfallebene für Systeme ohne lesbaren Device-Tree (sehr alte
// Raspbian-Kernel) und liefert daneben den SoC, den neuere Kernel nicht mehr
// in der Hardware-Zeile von /proc/cpuinfo nennen.
func RaspberryFromRevision(rev string) (model, soc string) {
	rev = strings.ToLower(strings.TrimSpace(rev))
	if rev == "" {
		return "", ""
	}
	code, err := strconv.ParseUint(rev, 16, 64)
	if err != nil {
		return "", ""
	}
	code &= rpiRevMask
	if code&rpiNewFormatBit == 0 {
		if name, ok := rpiOldRevisions[code]; ok {
			return "Raspberry Pi " + name, rpiSoCs[0]
		}
		return "", ""
	}
	name, ok := rpiTypes[(code>>rpiTypeShift)&rpiTypeMask]
	if !ok {
		return "", "" // neueres Modell als diese Tabelle kennt
	}
	return "Raspberry Pi " + name, rpiSoCs[(code>>rpiProcShift)&rpiProcMask]
}
