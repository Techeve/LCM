package domain

import (
	"regexp"
	"strings"
	"time"

	"gorm.io/gorm"
)

// Ergebnis des zentralen Registry-Update-Checks eines Docker-Images.
// "unauthorized" bedeutet: privates Image, anonym nicht prüfbar (v1 kennt
// keine Registry-Zugangsdaten) - die UI zeigt „nicht prüfbar (privat)".
const (
	DockerCheckOK           = "ok"           // Registry-Digest ermittelt, Vergleich gültig
	DockerCheckUnauthorized = "unauthorized" // privates Image - anonym nicht prüfbar
	DockerCheckNotFound     = "not_found"    // Repo/Tag existiert in der Registry nicht (mehr)
	DockerCheckLocal        = "local"        // lokal gebautes Image ohne Registry-Digest
	DockerCheckError        = "error"        // Netz-/Registry-Fehler beim Check
)

// Quellen von Vulnerability-Einträgen: klassischer OS-Paketbestand vs.
// Docker-Image-Scan. Die Scans laufen getrennt und ersetzen jeweils nur
// die Funde ihrer eigenen Quelle.
const (
	VulnSourceOS     = "os"
	VulnSourceDocker = "docker"
)

// DockerContainer ist ein Container auf einem verwalteten Server
// (docker ps -a). Compose-verwaltete Container tragen die
// com.docker.compose.*-Labels - ComposeProject leer = Standalone.
type DockerContainer struct {
	ID        string    `gorm:"type:text;primarykey" json:"id"` // UUID
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	ServerID    uint   `gorm:"not null;index:idx_dockerc_server_cid,unique" json:"server_id"`
	ContainerID string `gorm:"not null;index:idx_dockerc_server_cid,unique" json:"container_id"` // volle ID (--no-trunc)
	Name        string `gorm:"not null" json:"name"`
	Image       string `json:"image"`                 // Ref wie gestartet, z.B. "nginx:1.25"
	ImageID     string `gorm:"index" json:"image_id"` // sha256:… (Join auf DockerImage)
	State       string `json:"state"`                 // running|exited|restarting|paused|created|dead
	Status      string `json:"status"`                // Text, z.B. "Up 3 days"
	Ports       string `json:"ports"`

	ComposeProject string `gorm:"index" json:"compose_project"`
	ComposeService string `json:"compose_service"`
	// ComposeWorkingDir/-ConfigFiles stammen aus den Projekt-Labels und
	// erlauben ein gezieltes `docker compose pull && up -d` am richtigen Ort.
	ComposeWorkingDir  string `json:"compose_working_dir"`
	ComposeConfigFiles string `json:"compose_config_files"`
}

// BeforeCreate vergibt eine UUID, falls noch keine gesetzt ist.
func (c *DockerContainer) BeforeCreate(*gorm.DB) error {
	if c.ID == "" {
		c.ID = newUUID()
	}
	return nil
}

// DockerImage ist ein Image auf einem verwalteten Server (docker images
// --digests) inklusive des Ergebnisses des zentralen Update-Checks, der
// den lokalen RepoDigest mit dem aktuellen Registry-Digest des Tags
// vergleicht.
type DockerImage struct {
	ID        string    `gorm:"type:text;primarykey" json:"id"` // UUID
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	ServerID   uint   `gorm:"not null;index:idx_dockeri_server_ref,unique" json:"server_id"`
	Repository string `gorm:"not null;index:idx_dockeri_server_ref,unique" json:"repository"` // z.B. "nginx", "ghcr.io/foo/bar"
	Tag        string `gorm:"index:idx_dockeri_server_ref,unique" json:"tag"`                 // z.B. "1.25"
	ImageID    string `gorm:"index" json:"image_id"`                                          // sha256:… (Image-Config-ID)
	RepoDigest string `json:"repo_digest"`                                                    // Manifest-Digest; "" = lokal gebaut
	SizeText   string `json:"size_text"`                                                      // human-readable ("213MB")
	InUse      bool   `gorm:"default:false" json:"in_use"`                                    // von mind. einem Container referenziert

	CandidateDigest string     `json:"candidate_digest"` // aktueller Digest des Tags in der Registry
	UpdateAvailable bool       `gorm:"default:false;index" json:"update_available"`
	CheckStatus     string     `json:"check_status"` // ""|ok|unauthorized|not_found|local|error
	CheckError      string     `json:"check_error"`
	LastCheckedAt   *time.Time `json:"last_checked_at"`
}

// BeforeCreate vergibt eine UUID, falls noch keine gesetzt ist.
func (i *DockerImage) BeforeCreate(*gorm.DB) error {
	if i.ID == "" {
		i.ID = newUUID()
	}
	return nil
}

// Ref liefert die Image-Referenz "repo:tag" (ohne Tag nur das Repo).
func (i *DockerImage) Ref() string {
	if i.Tag == "" {
		return i.Repository
	}
	return i.Repository + ":" + i.Tag
}

// PublishedPort ist eine von Docker auf dem HOST veröffentlichte Portbindung
// (`docker run -p …`).
type PublishedPort struct {
	HostIP   string `json:"host_ip"`   // "0.0.0.0", "::", "127.0.0.1", …
	HostPort string `json:"host_port"` // Port auf dem Host
	Proto    string `json:"proto"`     // "tcp"/"udp"
}

// ExternallyReachable meldet, ob die Bindung von außen erreichbar ist -
// also NICHT auf Loopback beschränkt.
func (p PublishedPort) ExternallyReachable() bool {
	switch p.HostIP {
	case "127.0.0.1", "::1", "localhost":
		return false
	}
	// Das gesamte 127.0.0.0/8 ist Loopback, nicht nur 127.0.0.1.
	// "0.0.0.0" und "::" sind dagegen die Any-Adressen und damit erreichbar.
	return !strings.HasPrefix(p.HostIP, "127.")
}

// rePublishedPort liest die Einträge der Ports-Spalte von `docker ps`.
// Format je Eintrag: "<hostip>:<hostport>-><containerport>/<proto>";
// Einträge ohne "->" sind nur exponiert (EXPOSE), aber nicht veröffentlicht
// und damit von außen nicht erreichbar.
var rePublishedPort = regexp.MustCompile(`(?:^|,\s*)([0-9.]+|\[?::\]?|[0-9a-fA-F:]+):(\d+)->\d+/(\w+)`)

// PublishedPorts zerlegt die Ports-Spalte in die veröffentlichten Bindungen.
//
// Wichtig für die Firewall-Bewertung: Docker legt sein DNAT in nat/PREROUTING
// ab, also VOR der INPUT-Kette, in der ufw filtert. Eine an 0.0.0.0 gebundene
// Veröffentlichung ist deshalb von außen erreichbar, auch wenn ufw aktiv ist
// und den Port nicht freigegeben hat.
func (c *DockerContainer) PublishedPorts() []PublishedPort {
	var out []PublishedPort
	seen := map[string]bool{}
	for _, m := range rePublishedPort.FindAllStringSubmatch(c.Ports, -1) {
		hostIP := strings.Trim(m[1], "[]")
		p := PublishedPort{HostIP: hostIP, HostPort: m[2], Proto: m[3]}
		key := p.HostIP + ":" + p.HostPort + "/" + p.Proto
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, p)
	}
	return out
}

// BypassesHostFirewall meldet, ob der Container mindestens einen von außen
// erreichbaren Port veröffentlicht - und damit an einer Host-Firewall wie ufw
// vorbei erreichbar ist.
func (c *DockerContainer) BypassesHostFirewall() bool {
	for _, p := range c.PublishedPorts() {
		if p.ExternallyReachable() {
			return true
		}
	}
	return false
}
