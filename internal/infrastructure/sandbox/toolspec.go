package sandbox

import "os"

// BaseSystemSpec liefert die Pfade, die praktisch jedes Kommandozeilen-
// Programm braucht: Programmverzeichnisse und Bibliotheken, CA-Zertifikate
// (TLS), Namensauflösung und Zeitzone. Bewusst knapp gehalten - was hier
// fehlt, ist für den Kindprozess nicht vorhanden.
//
// Nicht enthalten und damit gesperrt: das LCM-Datenverzeichnis
// (/var/lib/lcm mit app.db und lcm.key), /etc/lcm, die Home-Verzeichnisse
// und alles Übrige.
//
// Fehlende Pfade sind unschädlich - die Regeln werden mit IgnoreIfMissing
// gesetzt, weil sich die Verzeichnislandschaft je Distribution unterscheidet.
func BaseSystemSpec() Spec {
	return Spec{
		ReadDirs: []string{
			// Programme und Bibliotheken (RODirs erlaubt auch das Ausführen).
			"/usr/bin", "/usr/sbin", "/bin", "/sbin",
			"/usr/lib", "/usr/lib64", "/lib", "/lib64",
			"/usr/local/bin", "/usr/local/lib",
			// CA-Zertifikate für TLS.
			"/etc/ssl", "/usr/share/ca-certificates", "/usr/local/share/ca-certificates",
			"/etc/pki", "/etc/ca-certificates",
		},
		ReadFiles: []string{
			// Namensauflösung und Zeitzone.
			"/etc/resolv.conf", "/etc/hosts", "/etc/nsswitch.conf",
			"/etc/host.conf", "/etc/localtime",
			// Trivy erkennt anhand von os-release die eigene Plattform.
			"/etc/os-release", "/usr/lib/os-release",
		},
	}
}

// WithPaths ergänzt eine Spec um weitere Pfade (die Basis bleibt unverändert).
func (s Spec) WithPaths(readDirs, readFiles, writeDirs []string) Spec {
	s.ReadDirs = append(append([]string{}, s.ReadDirs...), readDirs...)
	s.ReadFiles = append(append([]string{}, s.ReadFiles...), readFiles...)
	s.WriteDirs = append(append([]string{}, s.WriteDirs...), writeDirs...)
	return s
}

// WithNet erlaubt bzw. verbietet ausgehende Verbindungen.
func (s Spec) WithNet(allow bool) Spec {
	s.AllowNet = allow
	return s
}

// TempDir liefert das Verzeichnis für temporäre Dateien des Kindprozesses.
// Unter systemd ist es dank PrivateTmp ohnehin ein eigenes, vom übrigen
// System getrenntes Verzeichnis.
func TempDir() string { return os.TempDir() }
