package trivy

import "LCM/internal/core/domain"

// Der HTTP-Vertrag zwischen LCM und dem Trivy-Sidecar (cmd/trivyd).
//
// Warum ein eigener kleiner Dienst und nicht Trivys Client/Server-Modus:
// Dessen Client ist wieder das Trivy-Binary (~90 MB) - es müsste also doch
// ins LCM-Image, und läge dann doppelt vor. Trivys internes Server-Protokoll
// direkt zu sprechen wäre möglich, koppelt aber an ein versionsgebundenes
// Schema, das nicht als Schnittstelle gedacht ist. Der Sidecar ruft statt
// dessen die DOKUMENTIERTE Kommandozeile auf und reicht deren JSON durch.
//
// Beide Seiten benutzen diese Typen - ein Feld umzubenennen, ohne die andere
// Seite zu brechen, ist damit nicht möglich.
const (
	// PathHealth ist die schlichte Lebendprüfung (ohne Token).
	PathHealth = "/healthz"
	// PathInfo liefert Version und DB-Stand.
	PathInfo = "/info"
	// PathScanSBOM nimmt ein CycloneDX-SBOM entgegen.
	PathScanSBOM = "/scan/sbom"
	// PathScanImage prüft ein Image aus der Registry.
	PathScanImage = "/scan/image"
	// PathUpdateDB lädt die Schwachstellen-Datenbank.
	PathUpdateDB = "/db/update"
)

// ImageRequest ist die Anfrage an PathScanImage.
type ImageRequest struct {
	Ref string `json:"ref"`
}

// InfoResponse ist die Antwort von PathInfo. Sie trägt denselben Zustand, den
// der lokale Weg aus `trivy --version` liest.
type InfoResponse struct {
	Available bool   `json:"available"`
	Version   string `json:"version,omitempty"`
	// UpdatedAt/NextUpdate/DownloadedAt sind RFC-3339-Zeitpunkte; nil heißt
	// „nie geladen" und ist ein wichtiger Zustand, kein fehlender Wert.
	UpdatedAt    *string `json:"updated_at,omitempty"`
	NextUpdate   *string `json:"next_update,omitempty"`
	DownloadedAt *string `json:"downloaded_at,omitempty"`
	Error        string  `json:"error,omitempty"`
}

// UpdateResponse ist die Antwort von PathUpdateDB: die Ausgabe des Werkzeugs
// fürs Job-Protokoll.
type UpdateResponse struct {
	Output string `json:"output"`
}

// ErrorResponse ist der Fehlerkörper aller Endpunkte. Der Text landet im
// Klartext beim Betreiber - er muss also erklären, nicht nur benennen.
type ErrorResponse struct {
	Error string `json:"error"`
}

// SidecarBackend steht im Abschottungs-Feld, wenn Trivy im Sidecar läuft.
//
// Der Container IST hier die Abschottung: Er enthält weder LCMs Datenbank
// noch den Master-Key, also gibt es nichts zu erreichen. Bubblewrap zusätzlich
// wäre Theater - es braucht Rechte, die ein gehärteter Container gerade nicht
// hat. Wichtig ist, das genau so zu benennen: „ohne Sandbox" wäre falsch
// (es gibt eine Grenze), „bubblewrap" wäre gelogen.
const SidecarBackend = "container"

// sidecarNote erklärt die Abschottung im Sidecar-Betrieb im Klartext.
const sidecarNote = "Trivy läuft in einem eigenen Container; LCM startet keinen Kindprozess."

// infoFromSidecar übersetzt die Antwort in den Zustand, den die Oberfläche
// und die Alarme kennen.
func infoFromSidecar(r InfoResponse) domain.CVEDBStatus {
	st := domain.CVEDBStatus{
		Available: r.Available,
		Version:   r.Version,
		Freshness: domain.CVEDBUnknown,
		Error:     r.Error,
		// Die Grenze ist der Container - und die gibt es immer, wenn der
		// Sidecar antwortet.
		Sandboxed:      true,
		SandboxBackend: SidecarBackend,
		SandboxNote:    sidecarNote,
	}
	st.UpdatedAt = parseTime(r.UpdatedAt)
	st.NextUpdate = parseTime(r.NextUpdate)
	st.DownloadedAt = parseTime(r.DownloadedAt)
	return st
}
