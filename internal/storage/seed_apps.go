package storage

import "LCM/internal/core/domain"

// Mitgelieferte Einträge des Anwendungskatalogs.
//
// Die Auswahl folgt dem, was in der Praxis an der Paketverwaltung vorbei
// installiert wird: Software, die ihr eigenes Archiv, ihren eigenen Updater
// oder schlicht ein Tarball-Verzeichnis unter /opt mitbringt.
//
// Zwei Grundsätze:
//
//   - Lieber kein Versionskommando als ein geratenes. Ein Eintrag ohne
//     Versionsabfrage meldet immerhin, DASS die Anwendung da ist - ein
//     falsches Kommando meldet eine falsche Version, und das ist schlimmer.
//   - Die Merkmale zeigen auf die üblichen Installationsorte. Wer woanders
//     installiert, ergänzt eine Zeile im Katalog; dafür ist er pflegbar.
func builtinAppEntries() []domain.AppCatalogEntry {
	return []domain.AppCatalogEntry{
		{
			Slug: "adguard-home", Name: "AdGuard Home",
			NameEN: "AdGuard Home", DescriptionEN: "DNS filter that installs itself as its own directory under /opt and otherwise updates through its own interface.",
			Description: "DNS-Filter, installiert sich als eigenes Verzeichnis unter /opt und " +
				"aktualisiert sich sonst über seine eigene Oberfläche.",
			Markers:        "path /opt/AdGuardHome/AdGuardHome\nunit AdGuardHome.service",
			VersionCommand: "{path} --version",
			VersionPattern: `v?([0-9]+\.[0-9]+\.[0-9]+)`,
			Compare:        domain.CompareSemver,
			LatestSource:   "github:AdguardTeam/AdGuardHome",
		},
		{
			Slug: "nextcloud", Name: "Nextcloud",
			NameEN: "Nextcloud", DescriptionEN: "The version is read from version.php - deliberately from there and not through occ: running occ as root is exactly what Nextcloud warns about.",
			Description: "Die Version steht in version.php - bewusst von dort und nicht über " +
				"occ: occ als root auszuführen ist genau das, wovor Nextcloud warnt.",
			Markers:        "path /var/www/nextcloud\npath /var/www/html/nextcloud",
			VersionCommand: `sed -n "s/.*OC_VersionString *= *'\([^']*\)'.*/\1/p" {path}/version.php`,
			Compare:        domain.CompareSemver,
			LatestSource:   "github:nextcloud/server",
		},
		{
			Slug: "mailcow", Name: "mailcow: dockerized",
			NameEN: "mailcow: dockerized", DescriptionEN: "A Docker stack, but steered through a git repository - the version state is therefore the last tag of the checkout.",
			Description: "Ein Docker-Stack, aber über ein git-Repository gesteuert - der " +
				"Versionsstand ist deshalb der letzte Tag des Checkouts.",
			Markers:        "path /opt/mailcow-dockerized",
			VersionCommand: "git -C {path} describe --tags --abbrev=0",
			Compare:        domain.CompareExact,
			LatestSource:   "github:mailcow/mailcow-dockerized",
		},
		{
			Slug: "seafile", Name: "Seafile Server",
			NameEN: "Seafile Server", DescriptionEN: "Unpacks itself as seafile-server-<version> next to the data directory; the highest one present is the installed one.",
			Description: "Entpackt sich als seafile-server-<version> neben dem Datenverzeichnis; " +
				"die höchste vorhandene Fassung ist die installierte.",
			Markers:        "path /opt/seafile",
			VersionCommand: `ls -1 {path} | sed -n "s/^seafile-server-\([0-9].*\)$/\1/p" | sort -V | tail -1`,
			Compare:        domain.CompareSemver,
			LatestSource:   "github:haiwen/seafile",
		},
		{
			Slug: "minio", Name: "MinIO",
			NameEN: "MinIO", DescriptionEN: "A single binary, usually dropped straight into /usr/local/bin. The releases are named after their date, hence the exact comparison.",
			Description: "Einzelnes Binary, meist direkt nach /usr/local/bin gelegt. Die " +
				"Fassungen heißen nach ihrem Datum, deshalb der exakte Vergleich.",
			Markers:        "path /usr/local/bin/minio\nbin minio",
			VersionCommand: "{path} --version",
			VersionPattern: `(RELEASE\.[0-9TZ:\.-]+)`,
			Compare:        domain.CompareExact,
			LatestSource:   "github:minio/minio",
		},
		{
			Slug: "rustdesk-server", Name: "RustDesk Server",
			NameEN: "RustDesk Server", DescriptionEN: "The two services hbbs (rendezvous) and hbbr (relay) come as binaries; detection goes through hbbs.",
			Description: "Die beiden Dienste hbbs (Rendezvous) und hbbr (Relay) kommen als " +
				"Binaries; erkannt wird über hbbs.",
			Markers:        "path /opt/rustdesk/hbbs\nbin hbbs",
			VersionCommand: "{path} --version",
			VersionPattern: `([0-9]+\.[0-9]+\.[0-9]+)`,
			Compare:        domain.CompareSemver,
			LatestSource:   "github:rustdesk/rustdesk-server",
		},
		{
			Slug: "odoo", Name: "Odoo",
			NameEN: "Odoo", DescriptionEN: "Installed from source, Odoo usually lives under /opt/odoo. Installed from the Odoo repository it belongs to the package manager - then it does not show up here but among the packages.",
			Description: "Aus der Quelle installiert liegt Odoo üblicherweise unter /opt/odoo. " +
				"Aus dem Odoo-Repository installiert gehört es der Paketverwaltung - dann " +
				"taucht es hier nicht auf, sondern bei den Paketen.",
			Markers:        "path /opt/odoo\nbin odoo",
			VersionCommand: "{path}/odoo-bin --version 2>/dev/null || odoo --version",
			VersionPattern: `([0-9]+\.[0-9]+)`,
			Compare:        domain.CompareSemver,
		},
		{
			Slug: "intrexx", Name: "Intrexx",
			NameEN: "Intrexx", DescriptionEN: "Set up through its own installer. LCM reports THAT an installation is there; a version query is deliberately not stored here - it depends on the edition and is added in the catalog.",
			Description: "Wird über den eigenen Installer eingerichtet. LCM meldet, DASS eine " +
				"Installation da ist; eine Versionsabfrage ist hier bewusst nicht hinterlegt " +
				"- sie hängt von der Fassung ab und wird im Katalog nachgetragen.",
			Markers: "path /opt/intrexx\npath /usr/local/intrexx",
			Compare: domain.CompareNone,
		},
		{
			Slug: "form-gateway", Name: "Techeve Form-Gateway",
			NameEN: "Techeve Form Gateway", DescriptionEN: "A single Go binary with its own systemd unit.",
			Description:    "Einzelnes Go-Binary mit eigener systemd-Unit.",
			Markers:        "unit form-gateway.service\npath /opt/form-gateway",
			VersionCommand: "form-gateway --version",
			Compare:        domain.CompareSemver,
		},
		{
			Slug: "dns-editor", Name: "Techeve DNS-Editor",
			NameEN: "Techeve DNS Editor", DescriptionEN: "A single Go binary with its own systemd unit.",
			Description:    "Einzelnes Go-Binary mit eigener systemd-Unit.",
			Markers:        "unit dns-editor.service\npath /opt/dns-editor",
			VersionCommand: "dns-editor --version",
			Compare:        domain.CompareSemver,
		},
		{
			Slug: "uploader", Name: "Techeve Uploader",
			NameEN: "Techeve Uploader", DescriptionEN: "A single Go binary with its own systemd unit.",
			Description:    "Einzelnes Go-Binary mit eigener systemd-Unit.",
			Markers:        "unit uploader.service\npath /opt/uploader",
			VersionCommand: "uploader --version",
			Compare:        domain.CompareSemver,
		},
	}
}
