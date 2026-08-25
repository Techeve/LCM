package domain

import "time"

// KnownRepo ist ein Eintrag des pflegbaren Katalogs bekannter Paketquellen.
// LCM richtet einen Eintrag per Knopfdruck auf einem Server ein: GPG-Key nach
// /etc/apt/keyrings, signierte sources-Zeile, apt-Update. Der Katalog wird
// beim ersten Start mit DefaultKnownRepos befüllt und ist unter
// Einstellungen → Repositories pflegbar.
type KnownRepo struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Key identifiziert den Eintrag (Slug) und bestimmt die Dateinamen auf
	// dem Zielsystem (/etc/apt/keyrings/<key>.asc, lcm-<key>.list).
	Key         string `gorm:"uniqueIndex;not null" json:"key"`
	Name        string `gorm:"not null" json:"name"`
	Description string `json:"description"`
	KeyURL      string `json:"key_url"`
	// PackageManager bestimmt, FÜR WELCHE Paketverwaltung diese Quelle gilt
	// (apt/dnf/zypper/pacman/apk) - und damit, wie LCM sie einrichtet und auf
	// welchen Servern sie überhaupt angeboten wird. Leer wird als "apt"
	// gewertet (Altbestand vor der Mehrsprachigkeit des Katalogs).
	PackageManager string `gorm:"default:apt" json:"package_manager"`
	// Line ist die Paketquelle in der Syntax der jeweiligen Paketverwaltung:
	//   apt      - vollständige deb-Zeile ("deb [signed-by=…] URL suite comps");
	//              $ID, $CODENAME und $ARCH werden auf dem Zielsystem aufgelöst.
	//   dnf/yum  - URL einer .repo-Datei (nach /etc/yum.repos.d/ geladen).
	//   zypper   - Repository-URL (via `zypper addrepo`).
	//   pacman   - Server-URL des Repositories (Abschnitt = Key/Name).
	//   apk      - Repository-URL (Zeile in /etc/apk/repositories).
	Line string `gorm:"not null" json:"line"`
}

// RepoPackageManager liefert die Paketverwaltung eines Katalog-Eintrags und
// wertet den Leerwert (Altbestand) als "apt".
func (r KnownRepo) RepoPackageManager() string {
	if r.PackageManager == "" {
		return "apt"
	}
	return r.PackageManager
}

// DefaultKnownRepos sind die mitgelieferten Katalog-Einträge (alle https,
// alle mit dediziertem signed-by-Keyring - kein apt-key). Sie werden beim
// Seeding nur eingefügt, wenn ihr Key noch fehlt - Nutzer-Änderungen am
// Katalog bleiben unangetastet.
func DefaultKnownRepos() []KnownRepo {
	return []KnownRepo{
		{
			Key:            "docker",
			Name:           "Docker CE",
			Description:    "Offizielle Docker-Engine-Pakete (docker-ce, containerd, compose-plugin)",
			KeyURL:         "https://download.docker.com/linux/$ID/gpg",
			PackageManager: "apt",
			Line:           "deb [arch=$ARCH signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/$ID $CODENAME stable",
		},
		{
			Key:            "postgresql",
			Name:           "PostgreSQL (PGDG)",
			Description:    "Aktuelle PostgreSQL-Versionen vom PostgreSQL Global Development Group",
			KeyURL:         "https://www.postgresql.org/media/keys/ACCC4CF8.asc",
			PackageManager: "apt",
			Line:           "deb [signed-by=/etc/apt/keyrings/postgresql.asc] https://apt.postgresql.org/pub/repos/apt $CODENAME-pgdg main",
		},
		{
			Key:            "grafana",
			Name:           "Grafana",
			Description:    "Grafana, Loki und Tempo aus dem offiziellen Grafana-Repository",
			KeyURL:         "https://apt.grafana.com/gpg.key",
			PackageManager: "apt",
			Line:           "deb [signed-by=/etc/apt/keyrings/grafana.asc] https://apt.grafana.com stable main",
		},
		{
			Key:            "hashicorp",
			Name:           "HashiCorp",
			Description:    "Terraform, Vault, Consul und Nomad aus dem offiziellen HashiCorp-Repository",
			KeyURL:         "https://apt.releases.hashicorp.com/gpg",
			PackageManager: "apt",
			Line:           "deb [arch=$ARCH signed-by=/etc/apt/keyrings/hashicorp.asc] https://apt.releases.hashicorp.com $CODENAME main",
		},
		{
			Key:            "techeve",
			Name:           "Techeve",
			Description:    "Techeve-Paketrepository (u. a. LCM) - https://repo.techeve.de",
			KeyURL:         "https://repo.techeve.de/repo-key.gpg",
			PackageManager: "apt",
			Line:           "deb [signed-by=/etc/apt/keyrings/techeve.asc] https://repo.techeve.de stable main",
		},
	}
}
