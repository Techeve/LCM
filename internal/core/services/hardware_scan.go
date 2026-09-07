package services

import (
	"strings"

	"LCM/internal/core/domain"
)

// hardwareScanCmd liest Geräte- und CPU-Kennung aus den Dateien, die jedes
// Linux ohne Zusatzwerkzeug und ohne root bereitstellt:
//
//   - /proc/device-tree/model - der Gerätename bei ARM-Boards („Raspberry Pi 4
//     Model B Rev 1.4"). Die Datei ist NUL-terminiert, daher `tr -d`.
//   - /sys/class/dmi/id/{sys_vendor,product_name} - dasselbe für x86.
//   - Revision/Hardware aus /proc/cpuinfo - Rückfallebene für alte Pi-Kernel
//     ohne Device-Tree und Quelle für den SoC.
//   - lscpu - der CPU-Name auf ARM; dort gibt es die Zeile `model name` nicht,
//     die auf x86 die CPU benennt.
//
// Ausgabe je Fund als „schlüssel=wert"-Zeile; fehlende Quellen bleiben stumm.
const hardwareScanCmd = `{ for f in /proc/device-tree/model /sys/firmware/devicetree/base/model; do ` +
	`[ -r "$f" ] && { printf 'model='; tr -d '\000' < "$f"; echo; break; }; done; ` +
	`for f in sys_vendor product_name; do ` +
	`[ -r /sys/class/dmi/id/$f ] && printf '%s=%s\n' "$f" "$(cat /sys/class/dmi/id/$f)"; done; ` +
	`sed -n 's/^Revision[[:space:]]*:[[:space:]]*/revision=/p;s/^Hardware[[:space:]]*:[[:space:]]*/soc=/p' /proc/cpuinfo; ` +
	`lscpu 2>/dev/null | sed -n 's/^Model name:[[:space:]]*/cpu=/p' | head -1; ` +
	`} 2>/dev/null; true`

// dmiPlaceholders sind die Platzhalter, die Mainboard-Hersteller ab Werk in
// die DMI-Tabelle schreiben. Sie als Modell anzuzeigen wäre schlechter als
// gar nichts - „Default string" beschreibt kein Gerät.
var dmiPlaceholders = map[string]bool{
	"to be filled by o.e.m.": true, "default string": true,
	"system product name": true, "system manufacturer": true,
	"not specified": true, "not applicable": true,
	"none": true, "unknown": true, "o.e.m.": true, "oem": true,
}

// hardwareInfo ist das Ergebnis des Hardware-Scans.
type hardwareInfo struct {
	Model string // "Raspberry Pi 4 Model B Rev 1.4" / "Dell Inc. PowerEdge R640"
	CPU   string // "BCM2711 (Cortex-A72)" / "Intel(R) Xeon(R) Silver 4210R"
}

// scanHardware ermittelt Gerätemodell und CPU-Bezeichnung. cpuModelName ist
// die bereits gelesene `model name`-Zeile aus /proc/cpuinfo (auf x86 gesetzt,
// auf ARM leer) - sie hat Vorrang, der Rest füllt nur die Lücken.
func scanHardware(cpuModelName string, run func(label, cmd string) string) hardwareInfo {
	f := parseKeyValueLines(run("hardware", hardwareScanCmd))
	info := hardwareInfo{Model: f["model"], CPU: cpuModelName}

	// Kein Device-Tree: alte Pi-Kernel verraten das Modell nur noch über den
	// Revisions-Code, x86 über die DMI-Tabelle.
	rpiModel, rpiSoC := domain.RaspberryFromRevision(f["revision"])
	if info.Model == "" {
		info.Model = rpiModel
	}
	if info.Model == "" {
		info.Model = dmiModel(f["sys_vendor"], f["product_name"])
	}

	if info.CPU == "" {
		soc := f["soc"]
		if soc == "" {
			soc = rpiSoC
		}
		info.CPU = joinSoCAndCore(soc, f["cpu"])
	}
	return info
}

// dmiModel setzt Hersteller und Produktname zusammen und verwirft
// Werksplatzhalter. Steckt der Hersteller schon im Produktnamen („ASUSTeK
// COMPUTER INC. / ASUS ..."), wird er nicht doppelt genannt.
func dmiModel(vendor, product string) string {
	vendor, product = cleanDMI(vendor), cleanDMI(product)
	switch {
	case product == "":
		return vendor
	case vendor == "" || strings.HasPrefix(strings.ToLower(product), strings.ToLower(vendor)):
		return product
	default:
		return vendor + " " + product
	}
}

func cleanDMI(v string) string {
	v = strings.TrimSpace(v)
	if dmiPlaceholders[strings.ToLower(v)] {
		return ""
	}
	return v
}

// joinSoCAndCore verbindet SoC und CPU-Kern zur vollständigen Bezeichnung:
// „BCM2711 (Cortex-A72)". Fehlt eines von beidem, bleibt das andere allein
// stehen.
func joinSoCAndCore(soc, core string) string {
	soc, core = strings.TrimSpace(soc), strings.TrimSpace(core)
	switch {
	case soc == "":
		return core
	case core == "" || strings.Contains(soc, core):
		return soc
	default:
		return soc + " (" + core + ")"
	}
}

// parseKeyValueLines zerlegt eine Ausgabe aus „schlüssel=wert"-Zeilen. Leere
// Werte und Zeilen ohne „=" werden übergangen.
func parseKeyValueLines(out string) map[string]string {
	fields := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		if value = strings.TrimSpace(value); value != "" {
			fields[strings.TrimSpace(key)] = value
		}
	}
	return fields
}
