package domain

import "time"

// Funde auf einem Server: was der Anwendungs-Scan gefunden hat.
//
// Zwei Sorten, mit unterschiedlichem Anspruch:
//
//   - DetectedApp ist ein Treffer aus dem Katalog. LCM weiß, was es ist, und
//     kennt (meist) die Version.
//   - UnknownApp ist der generische Fund: ein laufender Dienst, dessen Unit
//     keinem Paket gehört. LCM weiß NICHT, was es ist - aber dass da etwas
//     ist, das an der Paketverwaltung vorbei installiert wurde. Genau diese
//     Funde sind der Grund für den ganzen Reiter: Ein Katalog zeigt nur, was
//     jemand vorher eingetragen hat.

// DetectedApp ist eine auf einem Server erkannte Katalog-Anwendung.
//
// Der Schlüssel ist (ServerID, Slug, Path) und nicht (ServerID, Slug): Eine
// Anwendung kann mehrfach installiert sein - zwei Nextclouds in verschiedenen
// vhosts, mehrere MinIO-Instanzen.
type DetectedApp struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	UpdatedAt time.Time `json:"updated_at"`

	ServerID uint   `gorm:"not null;index:idx_detected_server" json:"server_id"`
	Slug     string `gorm:"not null;index" json:"slug"`
	// Name wird beim Fund mitgeschrieben, damit die Liste ohne Verbindung zum
	// Katalog anzeigbar bleibt - auch wenn der Eintrag später umbenannt wird.
	Name string `json:"name"`
	// Path ist der Fundort (Installationspfad, Unit-Datei oder Binary).
	Path string `json:"path"`
	// Marker hält fest, welches Merkmal getroffen hat (path|unit|bin|proc).
	Marker string `json:"marker"`
	// Version ist die installierte Version, leer wenn nicht ermittelbar.
	Version string `json:"version"`
}

// UnknownApp ist ein laufender Dienst ohne Paketzugehörigkeit.
type UnknownApp struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	UpdatedAt time.Time `json:"updated_at"`

	ServerID uint `gorm:"not null;index:idx_unknown_server" json:"server_id"`
	// Unit ist der Dienstname, FragmentPath die Unit-Datei.
	Unit         string `gorm:"not null" json:"unit"`
	FragmentPath string `json:"fragment_path"`
	// ExecPath ist das gestartete Programm - der brauchbarste Hinweis darauf,
	// was hier eigentlich läuft, und die Vorlage für ein path-Merkmal.
	ExecPath string `json:"exec_path"`
}
