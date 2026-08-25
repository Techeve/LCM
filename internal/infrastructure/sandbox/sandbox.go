// Package sandbox sperrt externe Programme ein, die LCM auf seinem EIGENEN
// Host startet - derzeit ausschließlich Trivy.
//
// Warum das nötig ist: Ein Kindprozess läuft als derselbe Benutzer wie LCM
// und käme damit an /var/lib/lcm - dort liegen die Datenbank und der
// Master-Key nebeneinander. Ein manipuliertes Trivy bräuchte keinen Exploit,
// es müsste nur zwei Dateien lesen, um an die SSH-Schlüssel und Passwörter
// ALLER verwalteten Server zu kommen. Genau darauf zielte die
// Trivy-Lieferkettenkompromittierung im März 2026 (TeamPCP): die
// untergeschobenen Binaries durchsuchten über 50 Pfade nach Schlüsseln und
// Zugangsdaten. Uns hat es nicht getroffen - die Bauart, die es möglich
// gemacht hätte, bestand aber.
//
// Umsetzung unter Linux: Landlock (siehe sandbox_linux.go). Ein Extra-Benutzer
// wäre der naheliegende Weg, scheitert aber an NoNewPrivileges=true in unserem
// systemd-Unit - sudo/setuid ist dort abgeschaltet, und das soll so bleiben.
// Dieselbe Einstellung ist umgekehrt die Voraussetzung für Landlock, das genau
// für diesen Fall gebaut ist: ein unprivilegierter Prozess sperrt seine
// eigenen Kinder ein.
package sandbox

import (
	"context"
	"os/exec"
)

// Spec beschreibt, was der eingesperrte Prozess sehen darf. Alles, was hier
// nicht steht, ist für ihn nicht vorhanden - auch Verzeichnisse, die dem
// Benutzer sonst offenstehen.
type Spec struct {
	// ReadDirs: Verzeichnisbäume zum Lesen (Programme darin sind ausführbar).
	ReadDirs []string
	// ReadFiles: einzelne Dateien zum Lesen.
	ReadFiles []string
	// WriteDirs: Verzeichnisbäume zum Lesen UND Schreiben.
	WriteDirs []string
	// ScratchTmp: das Programm braucht ein beschreibbares /tmp. Bubblewrap
	// gibt ihm ein eigenes (das echte /tmp bleibt unsichtbar), Landlock kann
	// nur das vorhandene freigeben.
	ScratchTmp bool
	// AllowNet erlaubt ausgehende Verbindungen. false sperrt sie - bei
	// Bubblewrap vollständig über einen eigenen Netz-Namespace, bei Landlock
	// erst ab ABI 4 (Kernel 6.7); darunter bleibt das Netz notgedrungen
	// offen, die Dateisperre greift trotzdem.
	AllowNet bool
}

// Status meldet, ob die Sandbox auf diesem System trägt. Ein stiller Rückfall
// auf „läuft eben ungesperrt" wäre die schlechteste Variante - dann hielte
// man sich für geschützt, ohne es zu sein.
type Status struct {
	// Active: Kindprozesse werden tatsächlich eingesperrt.
	Active bool
	// Backend: welcher Mechanismus greift ("bubblewrap", "landlock", "").
	Backend string
	// NetEnforced: ausgehende Verbindungen sind sperrbar.
	NetEnforced bool
	// Reason: bei Active=false die Ursache, für die Anzeige im Klartext.
	Reason string
}

// Command baut den Aufruf eines externen Programms so, dass es unter den
// Regeln von spec läuft. Ist die Sandbox nicht verfügbar (Available()==false),
// entsteht der gewöhnliche Aufruf - der Aufrufer weiß über Available(), woran
// er ist, und meldet es weiter.
func Command(ctx context.Context, spec Spec, name string, args ...string) *exec.Cmd {
	if !Available().Active {
		return exec.CommandContext(ctx, name, args...)
	}
	return sandboxedCommand(ctx, spec, name, args...)
}
