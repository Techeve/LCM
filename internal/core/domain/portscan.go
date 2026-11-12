package domain

import "time"

// PortScan ist der letzte Portscan eines Servers von LCM aus: welche der
// gängigen TCP-Ports an seinen Adressen tatsächlich antworten. Je Server
// gibt es genau eine Zeile, ein neuer Lauf ersetzt den alten.
//
// Gespeichert wird nur die Beobachtung. Die Bewertung (Port nicht in der
// LCM-Firewall, riskanter Dienst) entsteht beim Lesen gegen den aktuellen
// Regelsatz - wer nach dem Scan eine Freigabe einträgt, sieht die Warnung
// sofort verschwinden und muss nicht erneut scannen.
type PortScan struct {
	ID        uint      `gorm:"primarykey" json:"-"`
	ServerID  uint      `gorm:"not null;uniqueIndex:idx_port_scan_server" json:"server_id"`
	ScannedAt time.Time `gorm:"not null" json:"scanned_at"`
	JobID     string    `json:"job_id"`
	// Result ist ein JSON von PortScanResult. Verschlüsselt wie das
	// Port-Inventar am Server (ListeningPorts): Adressen und offene Dienste
	// sind eine Landkarte für Angreifer.
	Result string `gorm:"serializer:aesgcm" json:"-"`
}

// PortScanResult sind die Beobachtungen eines Laufs.
type PortScanResult struct {
	Targets []PortScanTarget `json:"targets"`
}

// PortScanTarget ist eine gescannte Adresse.
type PortScanTarget struct {
	Address string `json:"address"`
	Family  string `json:"family"` // v4|v6
	Open    []int  `json:"open"`
	// Closed und Filtered zählen die übrigen Ports: geschlossen heißt, der
	// Host lehnt ab; gefiltert, er antwortet gar nicht (Firewall, Route).
	Closed   int `json:"closed"`
	Filtered int `json:"filtered"`
}
