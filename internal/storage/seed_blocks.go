package storage

import (
	"strings"

	"LCM/internal/core/domain"
)

// Der Katalog der mitgelieferten Regelbausteine.
//
// Warum er so umfangreich ist: Ein Baustein ist erst dann eine Hilfe, wenn er
// den Dienst trifft, den jemand wirklich betreibt. „Systemd-Dienst betreiben“
// deckt formal alles ab - aber wer nginx betreut, braucht auch dessen
// Konfigurationsverzeichnis, sein Protokollverzeichnis und `nginx -t`. Genau
// diese Zusammenstellung von Hand zu schreiben ist der Punkt, an dem in der
// Praxis jemand entnervt „/usr/bin/systemctl“ einträgt und damit volle
// Root-Rechte vergibt.
//
// Zwei Rollen je Dienst, wo beide Sinn ergeben:
//
//   - BETREIBEN: den Dienst bedienen (starten, stoppen, neu laden, Zustand),
//     sein Journal lesen, sein Protokollverzeichnis einsehen und die eine
//     Hauptkonfiguration bearbeiten. Das ist die Bereitschafts-Rolle.
//   - VERWALTEN: zusätzlich die eigenen Werkzeuge der Anwendung
//     (Konfigurationsprüfung, Neuladen ohne Neustart) und Schreibrecht auf
//     ihr Konfigurations- und Datenverzeichnis. Das ist die Betreuer-Rolle.
//
// Die Verzeichnisrechte wirken über POSIX-ACLs auf dem Zielsystem. Fehlt dort
// das Paket „acl“, bleiben sie folgenlos - der Abgleich meldet das.
//
// Pfade und Unit-Namen sind je Distributionsfamilie hinterlegt, wo sie
// abweichen. Fehlt für eine Familie eine Variante, gilt der Baustein dort
// NICHT und der Sync-Bericht sagt es.

// serviceVariant baut den Kern jeder Rolle: die Unit bedienen und ihr Journal
// lesen. `--no-pager` ist Pflicht - ohne das liefe der Pager als root, und in
// `less` genügt `!sh` für eine Root-Shell.
func serviceVariant(family, unit, editPaths, pathRules string) domain.ProfileBlockVariant {
	return domain.ProfileBlockVariant{
		Family: family,
		SudoCommands: strings.Join([]string{
			"/usr/bin/systemctl --no-pager start " + unit,
			"/usr/bin/systemctl --no-pager stop " + unit,
			"/usr/bin/systemctl --no-pager restart " + unit,
			"/usr/bin/systemctl --no-pager reload " + unit,
			"/usr/bin/systemctl --no-pager status " + unit,
			"/usr/bin/journalctl --no-pager -u " + unit,
			"/usr/bin/journalctl --no-pager -u " + unit + " -n 200",
		}, "\n"),
		EditPaths: editPaths,
		PathRules: pathRules,
	}
}

// plus hängt weitere Kommandos an eine Variante - der Weg von „betreiben“ zu
// „verwalten“, ohne die gemeinsamen Zeilen zu wiederholen.
func plus(v domain.ProfileBlockVariant, commands ...string) domain.ProfileBlockVariant {
	v.SudoCommands = v.SudoCommands + "\n" + strings.Join(commands, "\n")
	return v
}

// allFamilies gilt, wenn Unit-Name und Pfade überall gleich sind.
const allFamilies = domain.BlockFamilyAll

// builtinProfileBlocks liefert den vollständigen Katalog. Die Reihenfolge ist
// die Anzeigereihenfolge: erst die allgemeinen Bausteine, dann die Dienste
// nach Themen.
func builtinProfileBlocks() []domain.ProfileBlock {
	blocks := []domain.ProfileBlock{
		// ---- Allgemein ----------------------------------------------------
		{
			Slug: "systemd-dienst", Name: "Systemd-Dienst betreiben",
			NameEN: "Operate a systemd service", DescriptionEN: "Start, stop and reload a single service and look at its state.",
			Description: "Einen einzelnen Dienst starten, stoppen, neu laden und seinen Zustand ansehen.",
			Params:      "service",
			Variants: []domain.ProfileBlockVariant{{
				Family: allFamilies,
				SudoCommands: "/usr/bin/systemctl --no-pager start {service}\n" +
					"/usr/bin/systemctl --no-pager stop {service}\n" +
					"/usr/bin/systemctl --no-pager restart {service}\n" +
					"/usr/bin/systemctl --no-pager reload {service}\n" +
					"/usr/bin/systemctl --no-pager status {service}",
			}},
		},
		{
			Slug: "dienst-neustarten", Name: "Dienst nur neu starten",
			NameEN: "Restart a service only", DescriptionEN: "The smallest sensible role: restart it and check whether it is running - nothing else. For on-call duty that has to get a hung service going again.",
			Description: "Die kleinste sinnvolle Rolle: neu starten und nachsehen, ob er läuft - sonst nichts. " +
				"Für Bereitschaften, die einen hängenden Dienst wieder in Gang bringen sollen.",
			Params: "service",
			Variants: []domain.ProfileBlockVariant{{
				Family: allFamilies,
				SudoCommands: "/usr/bin/systemctl --no-pager restart {service}\n" +
					"/usr/bin/systemctl --no-pager status {service}",
			}},
		},
		{
			Slug: "dienst-logs", Name: "Logs eines Dienstes lesen",
			NameEN: "Read the logs of a service", DescriptionEN: "Inspect the journal of a unit - without the pager, which would otherwise run as root.",
			Description: "Das Journal einer Unit einsehen - ohne Pager, der sonst als root liefe.",
			Params:      "service",
			Variants: []domain.ProfileBlockVariant{{
				Family: allFamilies,
				SudoCommands: "/usr/bin/journalctl --no-pager -u {service}\n" +
					"/usr/bin/journalctl --no-pager -u {service} -n 200",
			}},
		},
		{
			Slug: "pakete-aktualisieren", Name: "Pakete aktualisieren",
			NameEN: "Update packages", DescriptionEN: "Refresh the package list and install updates - the matching calls per package manager.",
			Description: "Paketliste und Aktualisierungen einspielen - je Paketverwaltung die passenden Aufrufe.",
			Variants: []domain.ProfileBlockVariant{
				{Family: "apt", SudoCommands: "/usr/bin/apt-get update\n/usr/bin/apt-get -y upgrade"},
				{Family: "dnf", SudoCommands: "/usr/bin/dnf -y upgrade"},
				{Family: "zypper", SudoCommands: "/usr/bin/zypper -n update"},
				{Family: "pacman", SudoCommands: "/usr/bin/pacman -Syu --noconfirm"},
				{Family: "apk", SudoCommands: "/sbin/apk upgrade"},
			},
		},
		{
			Slug: "firewall-ansehen", Name: "Firewall ansehen und neu laden",
			NameEN: "View and reload the firewall", DescriptionEN: "Read the rule state and re-read the configuration - without being allowed to change any rules.",
			Description: "Den Regelstand lesen und die Konfiguration neu einlesen - ohne Regeln ändern zu dürfen.",
			Variants: []domain.ProfileBlockVariant{
				{Family: "apt", SudoCommands: "/usr/sbin/ufw status\n/usr/sbin/ufw status verbose\n/usr/sbin/ufw reload"},
				{Family: "dnf", SudoCommands: "/usr/bin/firewall-cmd --state\n/usr/bin/firewall-cmd --list-all\n/usr/bin/firewall-cmd --reload"},
				{Family: "zypper", SudoCommands: "/usr/bin/firewall-cmd --state\n/usr/bin/firewall-cmd --list-all\n/usr/bin/firewall-cmd --reload"},
				{Family: allFamilies, SudoCommands: "/usr/sbin/nft list ruleset"},
			},
		},

		// ---- Web und Proxy -------------------------------------------------
		{
			Slug: "nginx-betreiben", Name: "nginx betreiben",
			NameEN: "Operate nginx", DescriptionEN: "Operate nginx, read its journal and log directory, edit the main configuration.",
			Description: "nginx bedienen, sein Journal und Protokollverzeichnis lesen, die Hauptkonfiguration bearbeiten.",
			Variants: []domain.ProfileBlockVariant{
				serviceVariant(allFamilies, "nginx", "/etc/nginx/nginx.conf", "read /var/log/nginx"),
			},
		},
		{
			Slug: "nginx-verwalten", Name: "nginx verwalten",
			NameEN: "Manage nginx", DescriptionEN: "Everything from \"Operate nginx\" plus configuration check, reload without restart and write access to the configuration directory. On Debian/Ubuntu /var/www is included; the RPM distributions put their web directory under /usr, and LCM protects that - there the directory belongs in the profile as a path rule.",
			Description: "Alles aus „nginx betreiben“ plus Konfigurationsprüfung, Neuladen ohne Neustart und " +
				"Schreibrecht auf das Konfigurationsverzeichnis. Auf Debian/Ubuntu ist /var/www mit dabei; " +
				"die RPM-Distributionen legen ihr Web-Verzeichnis unter /usr ab, und das schützt LCM - dort " +
				"gehört das eigene Verzeichnis als Pfadregel ins Profil.",
			Variants: []domain.ProfileBlockVariant{
				plus(serviceVariant("apt", "nginx", "/etc/nginx/nginx.conf",
					"readwrite /etc/nginx\nreadwrite /var/www\nread /var/log/nginx"),
					"/usr/sbin/nginx -t", "/usr/sbin/nginx -s reload"),
				plus(serviceVariant(allFamilies, "nginx", "/etc/nginx/nginx.conf",
					"readwrite /etc/nginx\nread /var/log/nginx"),
					"/usr/sbin/nginx -t", "/usr/sbin/nginx -s reload"),
			},
		},
		{
			Slug: "apache-betreiben", Name: "Apache betreiben",
			NameEN: "Operate Apache", DescriptionEN: "Operate Apache and read its journal. The unit is named differently per distribution (apache2 or httpd), and the configuration lives elsewhere.",
			Description: "Apache bedienen und sein Journal lesen. Die Unit heißt je nach Distribution anders " +
				"(apache2 bzw. httpd), die Konfiguration liegt woanders.",
			Variants: []domain.ProfileBlockVariant{
				serviceVariant("apt", "apache2", "/etc/apache2/apache2.conf", "read /var/log/apache2"),
				serviceVariant("dnf", "httpd", "/etc/httpd/conf/httpd.conf", "read /var/log/httpd"),
				serviceVariant("zypper", "apache2", "/etc/apache2/httpd.conf", "read /var/log/apache2"),
			},
		},
		{
			Slug: "apache-verwalten", Name: "Apache verwalten",
			NameEN: "Manage Apache", DescriptionEN: "Everything from \"Operate Apache\" plus configuration check and write access to the configuration and web directory.",
			Description: "Alles aus „Apache betreiben“ plus Konfigurationsprüfung und Schreibrecht auf " +
				"Konfigurations- und Web-Verzeichnis.",
			Variants: []domain.ProfileBlockVariant{
				plus(serviceVariant("apt", "apache2", "/etc/apache2/apache2.conf",
					"readwrite /etc/apache2\nreadwrite /var/www\nread /var/log/apache2"),
					"/usr/sbin/apache2ctl configtest", "/usr/sbin/apache2ctl graceful"),
				plus(serviceVariant("dnf", "httpd", "/etc/httpd/conf/httpd.conf",
					"readwrite /etc/httpd\nreadwrite /var/www\nread /var/log/httpd"),
					"/usr/sbin/apachectl configtest", "/usr/sbin/apachectl graceful"),
				plus(serviceVariant("zypper", "apache2", "/etc/apache2/httpd.conf",
					"readwrite /etc/apache2\nreadwrite /srv/www\nread /var/log/apache2"),
					"/usr/sbin/apachectl configtest", "/usr/sbin/apachectl graceful"),
			},
		},
		{
			Slug: "caddy-betreiben", Name: "Caddy betreiben",
			NameEN: "Operate Caddy", DescriptionEN: "Operate Caddy, read the journal and edit the Caddyfile.",
			Description: "Caddy bedienen, Journal lesen und das Caddyfile bearbeiten.",
			Variants: []domain.ProfileBlockVariant{
				serviceVariant(allFamilies, "caddy", "/etc/caddy/Caddyfile", ""),
			},
		},
		{
			Slug: "caddy-verwalten", Name: "Caddy verwalten",
			NameEN: "Manage Caddy", DescriptionEN: "Everything from \"Operate Caddy\" plus configuration check, reload and write access to the configuration and data directory (which holds the certificates).",
			Description: "Alles aus „Caddy betreiben“ plus Konfigurationsprüfung, Neuladen und Schreibrecht " +
				"auf Konfigurations- und Datenverzeichnis (dort liegen die Zertifikate).",
			Variants: []domain.ProfileBlockVariant{
				plus(serviceVariant(allFamilies, "caddy", "/etc/caddy/Caddyfile",
					"readwrite /etc/caddy\nread /var/lib/caddy"),
					"/usr/bin/caddy validate --config /etc/caddy/Caddyfile",
					"/usr/bin/caddy reload --config /etc/caddy/Caddyfile"),
			},
		},
		{
			Slug: "haproxy-betreiben", Name: "HAProxy betreiben",
			NameEN: "Operate HAProxy", DescriptionEN: "Operate HAProxy, read the journal and edit the configuration.",
			Description: "HAProxy bedienen, Journal lesen und die Konfiguration bearbeiten.",
			Variants: []domain.ProfileBlockVariant{
				serviceVariant(allFamilies, "haproxy", "/etc/haproxy/haproxy.cfg", ""),
			},
		},
		{
			Slug: "haproxy-verwalten", Name: "HAProxy verwalten",
			NameEN: "Manage HAProxy", DescriptionEN: "Everything from \"Operate HAProxy\" plus write access to the configuration directory. The configuration check is NOT included: it reads \"haproxy -c\", and -c is the flag with which shells execute arbitrary code - LCM rejects it on principle. The restart reports a configuration error anyway.",
			Description: "Alles aus „HAProxy betreiben“ plus Schreibrecht auf das Konfigurationsverzeichnis. " +
				"Die Konfigurationsprüfung ist NICHT dabei: Sie heißt „haproxy -c“, und -c ist das Flag, mit dem " +
				"Shells beliebigen Code ausführen - LCM weist es grundsätzlich ab. Der Neustart meldet einen " +
				"Konfigurationsfehler ohnehin.",
			Variants: []domain.ProfileBlockVariant{
				serviceVariant(allFamilies, "haproxy", "/etc/haproxy/haproxy.cfg",
					"readwrite /etc/haproxy"),
			},
		},

		// ---- Datenbanken ---------------------------------------------------
		{
			Slug: "postgresql-betreiben", Name: "PostgreSQL betreiben",
			NameEN: "Operate PostgreSQL", DescriptionEN: "Operate the database service and read its log. Access to the data itself does NOT come with it - that is governed by PostgreSQL roles.",
			Description: "Den Datenbankdienst bedienen und sein Protokoll lesen. " +
				"Zugriff auf die Daten selbst ist damit NICHT verbunden - das regeln PostgreSQL-Rollen.",
			Variants: []domain.ProfileBlockVariant{
				serviceVariant("apt", "postgresql", "", "read /var/log/postgresql"),
				serviceVariant("dnf", "postgresql", "", "read /var/lib/pgsql/data/log"),
				serviceVariant("zypper", "postgresql", "", "read /var/log/postgresql"),
			},
		},
		{
			Slug: "postgresql-verwalten", Name: "PostgreSQL verwalten",
			NameEN: "Manage PostgreSQL", DescriptionEN: "Everything from \"Operate PostgreSQL\" plus write access to the configuration directory (postgresql.conf, pg_hba.conf). The rights INSIDE the database are still granted by PostgreSQL itself.",
			Description: "Alles aus „PostgreSQL betreiben“ plus Schreibrecht auf das Konfigurationsverzeichnis " +
				"(postgresql.conf, pg_hba.conf). Die Rechte INNERHALB der Datenbank vergibt weiterhin PostgreSQL selbst.",
			Variants: []domain.ProfileBlockVariant{
				serviceVariant("apt", "postgresql", "", "readwrite /etc/postgresql\nread /var/log/postgresql"),
				serviceVariant("dnf", "postgresql", "", "readwrite /var/lib/pgsql/data\nread /var/lib/pgsql/data/log"),
				serviceVariant("zypper", "postgresql", "", "readwrite /etc/postgresql\nread /var/log/postgresql"),
			},
		},
		{
			Slug: "mariadb-betreiben", Name: "MariaDB/MySQL betreiben",
			NameEN: "Operate MariaDB/MySQL", DescriptionEN: "Operate the database service and read its log. On Debian/Ubuntu \"mariadb\" is the alias of the mysql unit.",
			Description: "Den Datenbankdienst bedienen und sein Protokoll lesen. Auf Debian/Ubuntu ist " +
				"„mariadb“ der Aliasname der mysql-Unit.",
			Variants: []domain.ProfileBlockVariant{
				serviceVariant("apt", "mariadb", "", "read /var/log/mysql"),
				serviceVariant("dnf", "mariadb", "", "read /var/log/mariadb"),
				serviceVariant("zypper", "mariadb", "", "read /var/log/mysql"),
			},
		},
		{
			Slug: "mariadb-verwalten", Name: "MariaDB/MySQL verwalten",
			NameEN: "Manage MariaDB/MySQL", DescriptionEN: "Everything from \"Operate MariaDB/MySQL\" plus write access to the configuration directory. Database users and their rights are still granted by the database itself.",
			Description: "Alles aus „MariaDB/MySQL betreiben“ plus Schreibrecht auf das Konfigurationsverzeichnis. " +
				"Datenbank-Benutzer und -Rechte vergibt weiterhin die Datenbank selbst.",
			Variants: []domain.ProfileBlockVariant{
				serviceVariant("apt", "mariadb", "", "readwrite /etc/mysql\nread /var/log/mysql"),
				serviceVariant("dnf", "mariadb", "", "readwrite /etc/my.cnf.d\nread /var/log/mariadb"),
				serviceVariant("zypper", "mariadb", "", "readwrite /etc/my.cnf.d\nread /var/log/mysql"),
			},
		},
		{
			Slug: "redis-betreiben", Name: "Redis/Valkey betreiben",
			NameEN: "Operate Redis/Valkey", DescriptionEN: "Operate the cache service, read its journal and edit the configuration. The unit is called redis-server on Debian/Ubuntu, redis elsewhere.",
			Description: "Den Zwischenspeicher-Dienst bedienen, sein Journal lesen und die Konfiguration bearbeiten. " +
				"Die Unit heißt auf Debian/Ubuntu redis-server, sonst redis.",
			Variants: []domain.ProfileBlockVariant{
				serviceVariant("apt", "redis-server", "/etc/redis/redis.conf", "read /var/log/redis"),
				serviceVariant(allFamilies, "redis", "/etc/redis.conf", "read /var/log/redis"),
			},
		},

		// ---- Container -----------------------------------------------------
		{
			Slug: "docker-container", Name: "Einen Docker-Container bedienen",
			NameEN: "Operate a single Docker container", DescriptionEN: "Start, stop and restart exactly ONE container and read its output. Deliberately narrow: whoever may run \"docker\" in general is effectively root - one container with the root directory mounted is enough for that.",
			Description: "Genau EINEN Container starten, stoppen, neu starten und seine Ausgabe lesen. " +
				"Bewusst eng: Wer „docker“ allgemein darf, ist faktisch root - ein Container mit " +
				"eingehängtem Wurzelverzeichnis genügt dafür.",
			Params: "container",
			Variants: []domain.ProfileBlockVariant{{
				Family: allFamilies,
				SudoCommands: "/usr/bin/docker ps\n" +
					"/usr/bin/docker start {container}\n" +
					"/usr/bin/docker stop {container}\n" +
					"/usr/bin/docker restart {container}\n" +
					"/usr/bin/docker logs {container}\n" +
					"/usr/bin/docker inspect {container}",
			}},
		},

		// ---- Mail ----------------------------------------------------------
		{
			Slug: "postfix-betreiben", Name: "Postfix betreiben",
			NameEN: "Operate Postfix", DescriptionEN: "Operate the mail server, read its journal and edit the main configuration.",
			Description: "Den Mailserver bedienen, sein Journal lesen und die Hauptkonfiguration bearbeiten.",
			Variants: []domain.ProfileBlockVariant{
				serviceVariant(allFamilies, "postfix", "/etc/postfix/main.cf", ""),
			},
		},
		{
			Slug: "postfix-verwalten", Name: "Postfix verwalten",
			NameEN: "Manage Postfix", DescriptionEN: "Everything from \"Operate Postfix\" plus viewing and flushing the queue, configuration check and write access to the configuration directory.",
			Description: "Alles aus „Postfix betreiben“ plus Warteschlange ansehen und erneut zustellen, " +
				"Konfigurationsprüfung und Schreibrecht auf das Konfigurationsverzeichnis.",
			Variants: []domain.ProfileBlockVariant{
				plus(serviceVariant(allFamilies, "postfix", "/etc/postfix/main.cf",
					"readwrite /etc/postfix"),
					"/usr/sbin/postfix check", "/usr/sbin/postqueue -p", "/usr/sbin/postqueue -f"),
			},
		},
		{
			Slug: "dovecot-betreiben", Name: "Dovecot betreiben",
			NameEN: "Operate Dovecot", DescriptionEN: "Operate the IMAP/POP3 service, read its journal and edit the main configuration.",
			Description: "Den IMAP/POP3-Dienst bedienen, sein Journal lesen und die Hauptkonfiguration bearbeiten.",
			Variants: []domain.ProfileBlockVariant{
				serviceVariant(allFamilies, "dovecot", "/etc/dovecot/dovecot.conf", ""),
			},
		},
		{
			Slug: "dovecot-verwalten", Name: "Dovecot verwalten",
			NameEN: "Manage Dovecot", DescriptionEN: "Everything from \"Operate Dovecot\" plus reload through doveadm and write access to the configuration directory.",
			Description: "Alles aus „Dovecot betreiben“ plus Neuladen über doveadm und Schreibrecht auf " +
				"das Konfigurationsverzeichnis.",
			Variants: []domain.ProfileBlockVariant{
				plus(serviceVariant(allFamilies, "dovecot", "/etc/dovecot/dovecot.conf",
					"readwrite /etc/dovecot"),
					"/usr/bin/doveadm reload", "/usr/bin/doveadm who"),
			},
		},

		// ---- DNS und Netz ---------------------------------------------------
		{
			Slug: "bind-betreiben", Name: "BIND (named) betreiben",
			NameEN: "Operate BIND (named)", DescriptionEN: "Operate the name server and read its journal. On all current distributions the unit is called named.",
			Description: "Den Nameserver bedienen und sein Journal lesen. Die Unit heißt auf allen " +
				"aktuellen Distributionen named.",
			Variants: []domain.ProfileBlockVariant{
				serviceVariant("apt", "named", "", "read /var/log/named"),
				serviceVariant(allFamilies, "named", "", "read /var/named/data"),
			},
		},
		{
			Slug: "bind-verwalten", Name: "BIND (named) verwalten",
			NameEN: "Manage BIND (named)", DescriptionEN: "Everything from \"Operate BIND\" plus checking and reloading zones as well as write access to the zone directory.",
			Description: "Alles aus „BIND betreiben“ plus Zonen prüfen und neu laden sowie Schreibrecht " +
				"auf das Zonen-Verzeichnis.",
			Variants: []domain.ProfileBlockVariant{
				plus(serviceVariant("apt", "named", "/etc/bind/named.conf.local",
					"readwrite /etc/bind\nread /var/log/named"),
					"/usr/sbin/rndc status", "/usr/sbin/rndc reload",
					"/usr/bin/named-checkconf", "/usr/bin/named-checkzone"),
				plus(serviceVariant(allFamilies, "named", "/etc/named.conf",
					"readwrite /var/named\nread /var/named/data"),
					"/usr/sbin/rndc status", "/usr/sbin/rndc reload",
					"/usr/bin/named-checkconf", "/usr/bin/named-checkzone"),
			},
		},
		{
			Slug: "adguard-betreiben", Name: "AdGuard Home betreiben",
			NameEN: "Operate AdGuard Home", DescriptionEN: "Operate the DNS filter and read its journal. Independently of the distribution AdGuard Home lives under /opt/AdGuardHome.",
			Description: "Den DNS-Filter bedienen und sein Journal lesen. AdGuard Home liegt " +
				"unabhängig von der Distribution unter /opt/AdGuardHome.",
			Variants: []domain.ProfileBlockVariant{
				serviceVariant(allFamilies, "AdGuardHome", "", "read /opt/AdGuardHome/data"),
			},
		},
		{
			Slug: "adguard-verwalten", Name: "AdGuard Home verwalten",
			NameEN: "Manage AdGuard Home", DescriptionEN: "Everything from \"Operate AdGuard Home\" plus editing AdGuardHome.yaml and write access to the installation directory - that is where configuration, filter lists and statistics live.",
			Description: "Alles aus „AdGuard Home betreiben“ plus Bearbeiten der AdGuardHome.yaml und " +
				"Schreibrecht auf das Installationsverzeichnis - dort liegen Konfiguration, Filterlisten und Statistik.",
			Variants: []domain.ProfileBlockVariant{
				serviceVariant(allFamilies, "AdGuardHome", "/opt/AdGuardHome/AdGuardHome.yaml",
					"readwrite /opt/AdGuardHome"),
			},
		},
		{
			Slug: "pihole-betreiben", Name: "Pi-hole betreiben",
			NameEN: "Operate Pi-hole", DescriptionEN: "Restart the DNS service, look at its state and update the filter lists. The sub-actions are hard-wired - \"pihole\" on its own would put a root shell within reach.",
			Description: "Den DNS-Dienst neu starten, den Zustand ansehen und die Filterlisten aktualisieren. " +
				"Die Unteraktionen sind fest verdrahtet - „pihole“ allein wäre eine Root-Shell in Reichweite.",
			Variants: []domain.ProfileBlockVariant{{
				Family: allFamilies,
				SudoCommands: "/usr/local/bin/pihole status\n" +
					"/usr/local/bin/pihole restartdns\n" +
					"/usr/local/bin/pihole -g",
				PathRules: "read /var/log/pihole",
			}},
		},
		{
			Slug: "wireguard-betreiben", Name: "WireGuard betreiben",
			NameEN: "Operate WireGuard", DescriptionEN: "Bring a WireGuard interface up and down and look at its state. DELIBERATELY WITHOUT configuration access: a WireGuard configuration contains PostUp - whoever may write it executes arbitrary commands as root.",
			Description: "Eine WireGuard-Schnittstelle an- und abschalten und ihren Zustand ansehen. " +
				"BEWUSST OHNE Konfigurationszugriff: In einer WireGuard-Konfiguration steht PostUp - " +
				"wer sie schreiben darf, führt beliebige Kommandos als root aus.",
			Params: "interface",
			Variants: []domain.ProfileBlockVariant{{
				Family: allFamilies,
				SudoCommands: "/usr/bin/systemctl --no-pager start wg-quick@{interface}\n" +
					"/usr/bin/systemctl --no-pager stop wg-quick@{interface}\n" +
					"/usr/bin/systemctl --no-pager restart wg-quick@{interface}\n" +
					"/usr/bin/systemctl --no-pager status wg-quick@{interface}\n" +
					"/usr/bin/wg show",
			}},
		},

		// ---- Datei- und Sicherheitsdienste -----------------------------------
		{
			Slug: "samba-betreiben", Name: "Samba betreiben",
			NameEN: "Operate Samba", DescriptionEN: "Operate the file and name services and read their journals.",
			Description: "Die Datei- und Namensdienste bedienen und ihre Journale lesen.",
			Variants: []domain.ProfileBlockVariant{{
				Family: allFamilies,
				SudoCommands: "/usr/bin/systemctl --no-pager restart smbd\n" +
					"/usr/bin/systemctl --no-pager reload smbd\n" +
					"/usr/bin/systemctl --no-pager status smbd\n" +
					"/usr/bin/systemctl --no-pager restart nmbd\n" +
					"/usr/bin/systemctl --no-pager status nmbd\n" +
					"/usr/bin/journalctl --no-pager -u smbd -n 200",
				PathRules: "read /var/log/samba",
			}},
		},
		{
			Slug: "samba-verwalten", Name: "Samba verwalten",
			NameEN: "Manage Samba", DescriptionEN: "Everything from \"Operate Samba\" plus configuration check, re-reading the configuration and write access to the configuration directory.",
			Description: "Alles aus „Samba betreiben“ plus Konfigurationsprüfung, Neueinlesen der " +
				"Konfiguration und Schreibrecht auf das Konfigurationsverzeichnis.",
			Variants: []domain.ProfileBlockVariant{{
				Family: allFamilies,
				SudoCommands: "/usr/bin/systemctl --no-pager restart smbd\n" +
					"/usr/bin/systemctl --no-pager reload smbd\n" +
					"/usr/bin/systemctl --no-pager status smbd\n" +
					"/usr/bin/systemctl --no-pager restart nmbd\n" +
					"/usr/bin/journalctl --no-pager -u smbd -n 200\n" +
					"/usr/bin/testparm -s\n" +
					"/usr/bin/smbcontrol all reload-config",
				EditPaths: "/etc/samba/smb.conf",
				PathRules: "readwrite /etc/samba\nread /var/log/samba",
			}},
		},
		{
			Slug: "fail2ban-betreiben", Name: "Fail2ban betreiben",
			NameEN: "Operate Fail2ban", DescriptionEN: "Operate the service, look at its state and read its journal.",
			Description: "Den Dienst bedienen, seinen Zustand ansehen und sein Journal lesen.",
			Variants: []domain.ProfileBlockVariant{
				plus(serviceVariant(allFamilies, "fail2ban", "", "read /var/log/fail2ban.log"),
					"/usr/bin/fail2ban-client status", "/usr/bin/fail2ban-client ping"),
			},
		},
		{
			Slug: "fail2ban-verwalten", Name: "Fail2ban verwalten",
			NameEN: "Manage Fail2ban", DescriptionEN: "Everything from \"Operate Fail2ban\" plus lifting bans, reloading the configuration and write access to the configuration directory.",
			Description: "Alles aus „Fail2ban betreiben“ plus Sperren aufheben, Konfiguration neu laden " +
				"und Schreibrecht auf das Konfigurationsverzeichnis.",
			Params: "ip",
			Variants: []domain.ProfileBlockVariant{
				plus(serviceVariant(allFamilies, "fail2ban", "/etc/fail2ban/jail.local",
					"readwrite /etc/fail2ban\nread /var/log/fail2ban.log"),
					"/usr/bin/fail2ban-client status", "/usr/bin/fail2ban-client reload",
					"/usr/bin/fail2ban-client unban {ip}"),
			},
		},
		{
			Slug: "certbot-verwalten", Name: "Zertifikate erneuern (certbot)",
			NameEN: "Renew certificates (certbot)", DescriptionEN: "List and renew certificates. The existing deploy hooks run along with it - whoever may write them has root; that is why there is NO write access to /etc/letsencrypt here.",
			Description: "Zertifikate auflisten und erneuern. Die vorhandenen Deploy-Hooks laufen dabei " +
				"mit - wer sie schreiben darf, hat root; deshalb gibt es hier KEIN Schreibrecht auf /etc/letsencrypt.",
			Variants: []domain.ProfileBlockVariant{{
				Family: allFamilies,
				SudoCommands: "/usr/bin/certbot certificates\n" +
					"/usr/bin/certbot renew\n" +
					"/usr/bin/certbot renew --dry-run",
				PathRules: "read /var/log/letsencrypt",
			}},
		},

		// ---- Anwendungen und Überwachung ------------------------------------
		{
			Slug: "jellyfin-betreiben", Name: "Jellyfin betreiben",
			NameEN: "Operate Jellyfin", DescriptionEN: "Operate the media server and read its logs.",
			Description: "Den Medienserver bedienen und seine Protokolle lesen.",
			Variants: []domain.ProfileBlockVariant{
				serviceVariant(allFamilies, "jellyfin", "", "read /var/log/jellyfin"),
			},
		},
		{
			Slug: "jellyfin-verwalten", Name: "Jellyfin verwalten",
			NameEN: "Manage Jellyfin", DescriptionEN: "Everything from \"Operate Jellyfin\" plus write access to the configuration and data directory (libraries, metadata).",
			Description: "Alles aus „Jellyfin betreiben“ plus Schreibrecht auf Konfigurations- und " +
				"Datenverzeichnis (Bibliotheken, Metadaten).",
			Variants: []domain.ProfileBlockVariant{
				serviceVariant(allFamilies, "jellyfin", "",
					"readwrite /etc/jellyfin\nreadwrite /var/lib/jellyfin\nread /var/log/jellyfin"),
			},
		},
		{
			Slug: "grafana-betreiben", Name: "Grafana betreiben",
			NameEN: "Operate Grafana", DescriptionEN: "Operate the dashboard interface, read its journal and log directory.",
			Description: "Die Auswertungs-Oberfläche bedienen, ihr Journal und Protokollverzeichnis lesen.",
			Variants: []domain.ProfileBlockVariant{
				serviceVariant(allFamilies, "grafana-server", "", "read /var/log/grafana"),
			},
		},
		{
			Slug: "grafana-verwalten", Name: "Grafana verwalten",
			NameEN: "Manage Grafana", DescriptionEN: "Everything from \"Operate Grafana\" plus editing grafana.ini and write access to the configuration directory (data sources, provisioning).",
			Description: "Alles aus „Grafana betreiben“ plus Bearbeiten der grafana.ini und Schreibrecht " +
				"auf das Konfigurationsverzeichnis (Datenquellen, Bereitstellungen).",
			Variants: []domain.ProfileBlockVariant{
				serviceVariant(allFamilies, "grafana-server", "/etc/grafana/grafana.ini",
					"readwrite /etc/grafana\nread /var/log/grafana"),
			},
		},
		{
			Slug: "prometheus-betreiben", Name: "Prometheus betreiben",
			NameEN: "Operate Prometheus", DescriptionEN: "Operate the metrics collector and read its journal.",
			Description: "Den Messwert-Sammler bedienen und sein Journal lesen.",
			Variants: []domain.ProfileBlockVariant{
				serviceVariant(allFamilies, "prometheus", "", ""),
			},
		},
		{
			Slug: "prometheus-verwalten", Name: "Prometheus verwalten",
			NameEN: "Manage Prometheus", DescriptionEN: "Everything from \"Operate Prometheus\" plus configuration check and write access to the configuration directory (targets, alerting rules).",
			Description: "Alles aus „Prometheus betreiben“ plus Konfigurationsprüfung und Schreibrecht auf " +
				"das Konfigurationsverzeichnis (Ziele, Alarmregeln).",
			Variants: []domain.ProfileBlockVariant{
				plus(serviceVariant(allFamilies, "prometheus", "/etc/prometheus/prometheus.yml",
					"readwrite /etc/prometheus"),
					"/usr/bin/promtool check config /etc/prometheus/prometheus.yml"),
			},
		},

		// ---- Dateidienste und Container-Cluster ------------------------------
		{
			Slug: "nextcloud-betreiben", Name: "Nextcloud betreiben",
			NameEN: "Operate Nextcloud", DescriptionEN: "Query the state and toggle maintenance mode. The occ commands run as the WEB user, not as root: starting occ as root wrecks the file permissions of the whole installation, and Nextcloud warns about it itself. The parameter is the folder under /var/www (default: nextcloud). The PHP service and the web server are served by their own blocks - \"Operate a systemd service\" and \"Operate nginx\".",
			Description: "Zustand abfragen und den Wartungsmodus schalten. Die occ-Kommandos laufen als " +
				"WEB-Benutzer, nicht als root: occ als root zu starten verstellt die Dateirechte der " +
				"ganzen Installation, davor warnt Nextcloud selbst. Der Parameter ist der Ordner unter " +
				"/var/www (Voreinstellung: nextcloud). PHP-Dienst und Webserver bedienen ihre eigenen " +
				"Bausteine - „Systemd-Dienst betreiben“ und „nginx betreiben“.",
			Params: "instance",
			Variants: []domain.ProfileBlockVariant{
				nextcloudVariant("apt", "www-data", false),
				nextcloudVariant("dnf", "apache", false),
				nextcloudVariant("zypper", "wwwrun", false),
			},
		},
		{
			Slug: "nextcloud-verwalten", Name: "Nextcloud verwalten",
			NameEN: "Manage Nextcloud", DescriptionEN: "Everything from \"Operate Nextcloud\" plus the maintenance commands (scan files, add missing indices, finish an upgrade, list apps) and write access to the configuration directory. Here too the commands run as the web user.",
			Description: "Alles aus „Nextcloud betreiben“ plus die Wartungskommandos (Dateien einlesen, " +
				"fehlende Indizes anlegen, Aktualisierung abschließen, App-Liste) und Schreibrecht auf das " +
				"Konfigurationsverzeichnis. Auch hier laufen die Kommandos als Web-Benutzer.",
			Params: "instance",
			Variants: []domain.ProfileBlockVariant{
				nextcloudVariant("apt", "www-data", true),
				nextcloudVariant("dnf", "apache", true),
				nextcloudVariant("zypper", "wwwrun", true),
			},
		},
		{
			Slug: "seafile-betreiben", Name: "Seafile betreiben",
			NameEN: "Operate Seafile", DescriptionEN: "Operate Seafile and Seahub and read their logs. Expects the standard installation under /opt/seafile.",
			Description: "Seafile und Seahub bedienen und ihre Protokolle lesen. Erwartet die " +
				"Standard-Installation unter /opt/seafile.",
			Variants: []domain.ProfileBlockVariant{{
				Family: allFamilies,
				SudoCommands: "/usr/bin/systemctl --no-pager start seafile\n" +
					"/usr/bin/systemctl --no-pager stop seafile\n" +
					"/usr/bin/systemctl --no-pager restart seafile\n" +
					"/usr/bin/systemctl --no-pager status seafile\n" +
					"/usr/bin/systemctl --no-pager restart seahub\n" +
					"/usr/bin/systemctl --no-pager status seahub\n" +
					"/usr/bin/journalctl --no-pager -u seafile -n 200\n" +
					"/usr/bin/journalctl --no-pager -u seahub -n 200",
				PathRules: "read /opt/seafile/logs",
			}},
		},
		{
			Slug: "seafile-verwalten", Name: "Seafile verwalten",
			NameEN: "Manage Seafile", DescriptionEN: "Everything from \"Operate Seafile\" plus write access to the configuration directory. The data directory stays out - that is where the users' files live.",
			Description: "Alles aus „Seafile betreiben“ plus Schreibrecht auf das Konfigurationsverzeichnis. " +
				"Das Datenverzeichnis bleibt außen vor - dort liegen die Dateien der Benutzer.",
			Variants: []domain.ProfileBlockVariant{{
				Family: allFamilies,
				SudoCommands: "/usr/bin/systemctl --no-pager start seafile\n" +
					"/usr/bin/systemctl --no-pager stop seafile\n" +
					"/usr/bin/systemctl --no-pager restart seafile\n" +
					"/usr/bin/systemctl --no-pager status seafile\n" +
					"/usr/bin/systemctl --no-pager restart seahub\n" +
					"/usr/bin/systemctl --no-pager status seahub\n" +
					"/usr/bin/journalctl --no-pager -u seafile -n 200\n" +
					"/usr/bin/journalctl --no-pager -u seahub -n 200",
				PathRules: "readwrite /opt/seafile/conf\nread /opt/seafile/logs",
			}},
		},
		{
			Slug: "podman-container", Name: "Einen Podman-Container bedienen",
			NameEN: "Operate a single Podman container", DescriptionEN: "Start, stop and restart exactly ONE container and read its output. Deliberately as narrow as the Docker block: whoever may run \"podman\" as root in general is root. Rootless containers do not need this block - there the user's own account suffices.",
			Description: "Genau EINEN Container starten, stoppen, neu starten und seine Ausgabe lesen. " +
				"Wie beim Docker-Baustein bewusst eng: Wer „podman“ allgemein als root darf, ist root. " +
				"Rootless betriebene Container braucht dieser Baustein nicht - dort genügt der eigene Account.",
			Params: "container",
			Variants: []domain.ProfileBlockVariant{{
				Family: allFamilies,
				SudoCommands: "/usr/bin/podman ps\n" +
					"/usr/bin/podman start {container}\n" +
					"/usr/bin/podman stop {container}\n" +
					"/usr/bin/podman restart {container}\n" +
					"/usr/bin/podman logs {container}\n" +
					"/usr/bin/podman inspect {container}",
			}},
		},
		{
			Slug: "k3s-betreiben", Name: "k3s betreiben",
			NameEN: "Operate k3s", DescriptionEN: "Operate the k3s service and read its journal - server as well as agent nodes. Access to the cluster itself sits in the kubeconfig, not here.",
			Description: "Den k3s-Dienst bedienen und sein Journal lesen - Server- wie Agent-Knoten. " +
				"Der Zugriff auf den Cluster selbst steckt in der kubeconfig, nicht hier.",
			Variants: []domain.ProfileBlockVariant{{
				Family: allFamilies,
				SudoCommands: "/usr/bin/systemctl --no-pager start k3s\n" +
					"/usr/bin/systemctl --no-pager stop k3s\n" +
					"/usr/bin/systemctl --no-pager restart k3s\n" +
					"/usr/bin/systemctl --no-pager status k3s\n" +
					"/usr/bin/systemctl --no-pager restart k3s-agent\n" +
					"/usr/bin/systemctl --no-pager status k3s-agent\n" +
					"/usr/bin/journalctl --no-pager -u k3s -n 200\n" +
					"/usr/bin/journalctl --no-pager -u k3s-agent -n 200",
			}},
		},
		{
			Slug: "kubernetes-knoten", Name: "Kubernetes-Knoten betreiben",
			NameEN: "Operate a Kubernetes node", DescriptionEN: "Operate kubelet and the container runtime and read their journals - the block for kubeadm clusters. What happens on the cluster is still governed by RBAC in Kubernetes.",
			Description: "kubelet und die Container-Laufzeit bedienen und ihre Journale lesen - der " +
				"Baustein für kubeadm-Cluster. Was auf dem Cluster passiert, regelt weiterhin RBAC in Kubernetes.",
			Variants: []domain.ProfileBlockVariant{{
				Family: allFamilies,
				SudoCommands: "/usr/bin/systemctl --no-pager restart kubelet\n" +
					"/usr/bin/systemctl --no-pager status kubelet\n" +
					"/usr/bin/systemctl --no-pager restart containerd\n" +
					"/usr/bin/systemctl --no-pager status containerd\n" +
					"/usr/bin/journalctl --no-pager -u kubelet -n 200\n" +
					"/usr/bin/journalctl --no-pager -u containerd -n 200",
				PathRules: "read /var/log/pods",
			}},
		},
		{
			Slug: "kubectl-lesen", Name: "Cluster ansehen (kubectl)",
			NameEN: "View the cluster (kubectl)", DescriptionEN: "The read-only view of the cluster with the node's administrator kubeconfig. The sub-actions are hard-wired: \"kubectl\" on its own also permits exec and edit - both are a break-out. The path is the one k3s uses; with kubeadm kubectl lives under /usr/bin.",
			Description: "Der Nur-Lese-Blick auf den Cluster mit der Administrator-kubeconfig des Knotens. " +
				"Die Unteraktionen sind fest verdrahtet: „kubectl“ allein erlaubt auch exec und edit - " +
				"beides ist ein Ausbruch. Der Pfad ist der von k3s; bei kubeadm liegt kubectl unter /usr/bin.",
			Variants: []domain.ProfileBlockVariant{{
				Family: allFamilies,
				SudoCommands: "/usr/local/bin/kubectl version\n" +
					"/usr/local/bin/kubectl cluster-info\n" +
					"/usr/local/bin/kubectl get nodes\n" +
					"/usr/local/bin/kubectl get pods -A\n" +
					"/usr/local/bin/kubectl get deployments -A\n" +
					"/usr/local/bin/kubectl get services -A\n" +
					"/usr/local/bin/kubectl get events -A\n" +
					"/usr/local/bin/kubectl top nodes",
			}},
		},
		// ---- Techeve ---------------------------------------------------------
		{
			Slug: "lcm-betreiben", Name: "LCM betreiben",
			NameEN: "Operate LCM", DescriptionEN: "Operate the LCM service itself and read its journal. The data directory /var/lib/lcm stays out - that is where master key and database live, and LCM protects the path against its own rules.",
			Description: "Den LCM-Dienst selbst bedienen und sein Journal lesen. Das Datenverzeichnis " +
				"/var/lib/lcm bleibt außen vor - dort liegen Master-Key und Datenbank, und LCM schützt " +
				"den Pfad gegen seine eigenen Regeln.",
			Variants: []domain.ProfileBlockVariant{
				serviceVariant(allFamilies, "lcm", "", ""),
			},
		},
		{
			Slug: "lcm-verwalten", Name: "LCM verwalten",
			NameEN: "Manage LCM", DescriptionEN: "Everything from \"Operate LCM\" plus editing config.json. CAUTION: that file holds the JWT secret - whoever may write it can issue themselves admin sessions in LCM. This is a role for whoever looks after the LCM host, not for on-call duty.",
			Description: "Alles aus „LCM betreiben“ plus Bearbeiten der config.json. ACHTUNG: In dieser " +
				"Datei steht das JWT-Geheimnis - wer sie schreiben darf, kann sich Admin-Sitzungen in LCM " +
				"ausstellen. Das ist eine Rolle für den Betreuer des LCM-Hosts, nicht für die Bereitschaft.",
			Variants: []domain.ProfileBlockVariant{
				serviceVariant(allFamilies, "lcm", "/etc/lcm/config.json", ""),
			},
		},
		{
			Slug: "form-gateway-betreiben", Name: "Form-Gateway betreiben",
			NameEN: "Operate Form Gateway", DescriptionEN: "Operate the Techeve Form Gateway, read its journal and edit the configuration. config.json holds the credentials for sending mail.",
			Description: "Das Techeve Form-Gateway bedienen, sein Journal lesen und die Konfiguration " +
				"bearbeiten. In der config.json stehen die Zugangsdaten des Mailversands.",
			Variants: []domain.ProfileBlockVariant{
				serviceVariant(allFamilies, "form-gateway", "/etc/form-gateway/config.json", ""),
			},
		},
		{
			Slug: "dnseditor-betreiben", Name: "DNS-Editor betreiben",
			NameEN: "Operate DNS Editor", DescriptionEN: "Operate the DNS Editor and read its journal. The parameter is the unit name of the edition: dnseditor (CE), dnseditor-cloud or dnseditor-ee.",
			Description: "Den DNS-Editor bedienen und sein Journal lesen. Der Parameter ist der Unit-Name " +
				"der Ausgabe: dnseditor (CE), dnseditor-cloud oder dnseditor-ee.",
			Params: "service",
			Variants: []domain.ProfileBlockVariant{{
				Family: allFamilies,
				SudoCommands: "/usr/bin/systemctl --no-pager start {service}\n" +
					"/usr/bin/systemctl --no-pager stop {service}\n" +
					"/usr/bin/systemctl --no-pager restart {service}\n" +
					"/usr/bin/systemctl --no-pager status {service}\n" +
					"/usr/bin/journalctl --no-pager -u {service} -n 200",
			}},
		},
		{
			Slug: "dnseditor-verwalten", Name: "DNS-Editor verwalten",
			NameEN: "Manage DNS Editor", DescriptionEN: "Everything from \"Operate DNS Editor\" plus editing the environment file. The data directory stays out - that is where zones and keys live.",
			Description: "Alles aus „DNS-Editor betreiben“ plus Bearbeiten der Umgebungsdatei. Das " +
				"Datenverzeichnis bleibt außen vor - dort liegen Zonen und Schlüssel.",
			Params: "service",
			Variants: []domain.ProfileBlockVariant{{
				Family: allFamilies,
				SudoCommands: "/usr/bin/systemctl --no-pager start {service}\n" +
					"/usr/bin/systemctl --no-pager stop {service}\n" +
					"/usr/bin/systemctl --no-pager restart {service}\n" +
					"/usr/bin/systemctl --no-pager status {service}\n" +
					"/usr/bin/journalctl --no-pager -u {service} -n 200",
				EditPaths: "/etc/{service}/env",
			}},
		},
		{
			Slug: "dnseditor-agenten", Name: "DNS-Editor-Agenten betreiben",
			NameEN: "Operate the DNS Editor agents", DescriptionEN: "Operate the certificate agent and the DynDNS client and read their journals - the two helpers that run on the target systems.",
			Description: "Zertifikats-Agent und DynDNS-Client bedienen und ihre Journale lesen - die " +
				"beiden Helfer, die auf den Zielsystemen laufen.",
			Variants: []domain.ProfileBlockVariant{{
				Family: allFamilies,
				SudoCommands: "/usr/bin/systemctl --no-pager restart dnseditor-cert-agent\n" +
					"/usr/bin/systemctl --no-pager status dnseditor-cert-agent\n" +
					"/usr/bin/systemctl --no-pager restart dnseditor-dyndns-client\n" +
					"/usr/bin/systemctl --no-pager status dnseditor-dyndns-client\n" +
					"/usr/bin/journalctl --no-pager -u dnseditor-cert-agent -n 200\n" +
					"/usr/bin/journalctl --no-pager -u dnseditor-dyndns-client -n 200",
			}},
		},

		// ---- Weitere verbreitete Dienste --------------------------------------
		{
			Slug: "rustdesk-betreiben", Name: "RustDesk-Server betreiben",
			NameEN: "Operate the RustDesk server", DescriptionEN: "Operate the rendezvous and relay service (hbbs/hbbr) and read their journals.",
			Description: "Signal- und Relay-Dienst (hbbs/hbbr) bedienen und ihre Journale lesen.",
			Variants: []domain.ProfileBlockVariant{{
				Family: allFamilies,
				SudoCommands: "/usr/bin/systemctl --no-pager restart rustdesk-hbbs\n" +
					"/usr/bin/systemctl --no-pager status rustdesk-hbbs\n" +
					"/usr/bin/systemctl --no-pager restart rustdesk-hbbr\n" +
					"/usr/bin/systemctl --no-pager status rustdesk-hbbr\n" +
					"/usr/bin/journalctl --no-pager -u rustdesk-hbbs -n 200\n" +
					"/usr/bin/journalctl --no-pager -u rustdesk-hbbr -n 200",
			}},
		},
		{
			Slug: "odoo-betreiben", Name: "Odoo betreiben",
			NameEN: "Operate Odoo", DescriptionEN: "Operate the Odoo service and read its logs.",
			Description: "Den Odoo-Dienst bedienen und seine Protokolle lesen.",
			Variants: []domain.ProfileBlockVariant{
				serviceVariant(allFamilies, "odoo", "", "read /var/log/odoo"),
			},
		},
		{
			Slug: "odoo-verwalten", Name: "Odoo verwalten",
			NameEN: "Manage Odoo", DescriptionEN: "Everything from \"Operate Odoo\" plus editing odoo.conf and write access to the addons directory - the way to install a module. The data directory (attachments, filestore) stays out.",
			Description: "Alles aus „Odoo betreiben“ plus Bearbeiten der odoo.conf und Schreibrecht auf " +
				"das Addons-Verzeichnis - der Weg, ein Modul einzuspielen. Das Datenverzeichnis " +
				"(Anhänge, Filestore) bleibt außen vor.",
			Params: "addons",
			Variants: []domain.ProfileBlockVariant{
				serviceVariant(allFamilies, "odoo", "/etc/odoo/odoo.conf",
					"readwrite {addons}\nread /var/log/odoo"),
			},
		},
		{
			Slug: "intrexx-betreiben", Name: "Intrexx-Portal betreiben",
			NameEN: "Operate an Intrexx portal", DescriptionEN: "Operate the supervisor, the portal and Solr. The parameter is the portal name - the unit is called upixp_<portalname>. Expects the installation under /opt/intrexx.",
			Description: "Supervisor, Portal und Solr bedienen. Der Parameter ist der Portalname - die " +
				"Unit heißt upixp_<Portalname>. Erwartet die Installation unter /opt/intrexx.",
			Params: "portal",
			Variants: []domain.ProfileBlockVariant{{
				Family: allFamilies,
				SudoCommands: "/usr/bin/systemctl --no-pager restart upixsupervisor\n" +
					"/usr/bin/systemctl --no-pager status upixsupervisor\n" +
					"/usr/bin/systemctl --no-pager restart upixp_{portal}\n" +
					"/usr/bin/systemctl --no-pager status upixp_{portal}\n" +
					"/usr/bin/systemctl --no-pager restart upixsolr\n" +
					"/usr/bin/systemctl --no-pager status upixsolr\n" +
					"/usr/bin/journalctl --no-pager -u upixp_{portal} -n 200",
			}},
		},
		{
			Slug: "gitea-betreiben", Name: "Gitea/Forgejo betreiben",
			NameEN: "Operate Gitea/Forgejo", DescriptionEN: "Operate the git service and read its journal. The parameter is the unit: gitea or forgejo.",
			Description: "Den Git-Dienst bedienen und sein Journal lesen. Der Parameter ist die Unit: " +
				"gitea oder forgejo.",
			Params: "service",
			Variants: []domain.ProfileBlockVariant{{
				Family: allFamilies,
				SudoCommands: "/usr/bin/systemctl --no-pager start {service}\n" +
					"/usr/bin/systemctl --no-pager stop {service}\n" +
					"/usr/bin/systemctl --no-pager restart {service}\n" +
					"/usr/bin/systemctl --no-pager status {service}\n" +
					"/usr/bin/journalctl --no-pager -u {service} -n 200",
				PathRules: "read /var/lib/{service}/log",
			}},
		},
		{
			Slug: "gitea-verwalten", Name: "Gitea/Forgejo verwalten",
			NameEN: "Manage Gitea/Forgejo", DescriptionEN: "Everything from \"Operate Gitea/Forgejo\" plus editing app.ini. The repository directory stays out - that is where the users' source code lives.",
			Description: "Alles aus „Gitea/Forgejo betreiben“ plus Bearbeiten der app.ini. Das " +
				"Repository-Verzeichnis bleibt außen vor - dort liegt der Quellcode der Nutzer.",
			Params: "service",
			Variants: []domain.ProfileBlockVariant{{
				Family: allFamilies,
				SudoCommands: "/usr/bin/systemctl --no-pager start {service}\n" +
					"/usr/bin/systemctl --no-pager stop {service}\n" +
					"/usr/bin/systemctl --no-pager restart {service}\n" +
					"/usr/bin/systemctl --no-pager status {service}\n" +
					"/usr/bin/journalctl --no-pager -u {service} -n 200",
				EditPaths: "/etc/{service}/app.ini",
				PathRules: "read /var/lib/{service}/log",
			}},
		},
		{
			Slug: "gitlab-betreiben", Name: "GitLab betreiben (Omnibus)",
			NameEN: "Operate GitLab (Omnibus)", DescriptionEN: "Operate the GitLab services through gitlab-ctl and read the logs. DELIBERATELY WITHOUT gitlab.rb and without reconfigure: whoever may do both executes arbitrary commands as root through the Chef configuration - that is no longer a bounded role.",
			Description: "Die GitLab-Dienste über gitlab-ctl bedienen und die Protokolle lesen. " +
				"BEWUSST OHNE gitlab.rb und ohne reconfigure: Wer beides darf, führt über die " +
				"Chef-Konfiguration beliebige Kommandos als root aus - das ist keine abgegrenzte Rolle mehr.",
			Variants: []domain.ProfileBlockVariant{{
				Family: allFamilies,
				SudoCommands: "/usr/bin/gitlab-ctl status\n" +
					"/usr/bin/gitlab-ctl restart\n" +
					"/usr/bin/gitlab-ctl stop\n" +
					"/usr/bin/gitlab-ctl start",
				PathRules: "read /var/log/gitlab",
			}},
		},
		{
			Slug: "aptly-betreiben", Name: "Aptly betreiben",
			NameEN: "Operate Aptly", DescriptionEN: "Operate the Aptly API service, read its journal and look at the inventory (repositories, publications, mirrors).",
			Description: "Den Aptly-API-Dienst bedienen, sein Journal lesen und den Bestand ansehen " +
				"(Repositories, Veröffentlichungen, Spiegel).",
			Variants: []domain.ProfileBlockVariant{
				plus(serviceVariant(allFamilies, "aptly-api", "", ""),
					"/usr/bin/aptly repo list", "/usr/bin/aptly publish list", "/usr/bin/aptly mirror list"),
			},
		},
		{
			Slug: "aptly-verwalten", Name: "Aptly verwalten",
			NameEN: "Manage Aptly", DescriptionEN: "Everything from \"Operate Aptly\" plus cleaning up the database, editing aptly.conf and read access to the Aptly directory. Signing publications needs the GPG key and deliberately stays out.",
			Description: "Alles aus „Aptly betreiben“ plus Aufräumen der Datenbank, Bearbeiten der " +
				"aptly.conf und Lesezugriff auf das Aptly-Verzeichnis. Das Signieren von " +
				"Veröffentlichungen braucht den GPG-Schlüssel und bleibt bewusst außen vor.",
			Variants: []domain.ProfileBlockVariant{
				plus(serviceVariant(allFamilies, "aptly-api", "/etc/aptly.conf", "read /var/lib/aptly"),
					"/usr/bin/aptly repo list", "/usr/bin/aptly publish list",
					"/usr/bin/aptly mirror list", "/usr/bin/aptly db cleanup"),
			},
		},
		{
			Slug: "acl-ansehen", Name: "Dateirechte ansehen (ACL)",
			NameEN: "View file permissions (ACL)", DescriptionEN: "List the POSIX ACLs actually set on a directory - the way to check what of a profile's directory permissions arrived on the target. Read-only: ACLs are set by LCM itself, not by hand.",
			Description: "Die tatsächlich gesetzten POSIX-ACLs eines Verzeichnisses auflisten - der " +
				"Weg nachzusehen, was von den Verzeichnisrechten eines Profils auf dem Ziel angekommen " +
				"ist. Rein lesend: Gesetzt werden ACLs von LCM selbst, nicht von Hand.",
			Params: "path",
			Variants: []domain.ProfileBlockVariant{{
				Family:       allFamilies,
				SudoCommands: "/usr/bin/getfacl {path}\n/usr/bin/getfacl -R {path}",
				PathRules:    "read {path}",
			}},
		},
		{
			Slug: "minio-betreiben", Name: "MinIO betreiben",
			NameEN: "Operate MinIO", DescriptionEN: "Operate the object store and read its journal. The data directory stays out - that is where the objects live.",
			Description: "Den Objektspeicher bedienen und sein Journal lesen. Das Datenverzeichnis " +
				"bleibt außen vor - dort liegen die Objekte.",
			Variants: []domain.ProfileBlockVariant{
				serviceVariant(allFamilies, "minio", "", ""),
			},
		},
		{
			Slug: "minio-verwalten", Name: "MinIO verwalten",
			NameEN: "Manage MinIO", DescriptionEN: "Everything from \"Operate MinIO\" plus editing the environment file /etc/default/minio. CAUTION: it holds the access key and the root password.",
			Description: "Alles aus „MinIO betreiben“ plus Bearbeiten der Umgebungsdatei " +
				"/etc/default/minio. ACHTUNG: Dort stehen Zugangsschlüssel und Wurzelkennwort.",
			Variants: []domain.ProfileBlockVariant{
				serviceVariant(allFamilies, "minio", "/etc/default/minio", ""),
			},
		},
		{
			Slug: "mailcow-betreiben", Name: "mailcow betreiben",
			NameEN: "Operate mailcow", DescriptionEN: "Inspect and restart the mailcow stack under /opt/mailcow-dockerized. DELIBERATELY WITHOUT write access to mailcow.conf and the compose file: whoever can change them mounts a directory of their choice into a container - and is thereby root on the host. mailcow is still updated through its own update.sh.",
			Description: "Den mailcow-Stack unter /opt/mailcow-dockerized ansehen und neu starten. " +
				"BEWUSST OHNE Schreibrecht auf mailcow.conf und die Compose-Datei: Wer sie ändern kann, " +
				"hängt ein Verzeichnis seiner Wahl in einen Container - und ist damit root auf dem Wirt. " +
				"Aktualisiert wird mailcow weiterhin über sein eigenes update.sh.",
			Variants: []domain.ProfileBlockVariant{{
				Family: allFamilies,
				SudoCommands: "/usr/bin/docker compose -f /opt/mailcow-dockerized/docker-compose.yml ps\n" +
					"/usr/bin/docker compose -f /opt/mailcow-dockerized/docker-compose.yml restart\n" +
					"/usr/bin/docker compose -f /opt/mailcow-dockerized/docker-compose.yml logs --tail 200",
				PathRules: "read /opt/mailcow-dockerized/data/conf",
			}},
		},
	}
	for i := range blocks {
		blocks[i].Builtin = true
	}
	return blocks
}

// nextcloudVariant baut die occ-Regeln für eine Distribution. manage=true
// nimmt die Wartungskommandos und das Schreibrecht auf die Konfiguration mit.
//
// Der Zielbenutzer ist der Kern: occ MUSS als der Benutzer laufen, dem die
// Installation gehört - auf Debian www-data, auf RHEL apache, auf SUSE
// wwwrun. Als root ausgeführt legt es Dateien an, die der Webserver danach
// nicht mehr lesen kann.
func nextcloudVariant(family, webUser string, manage bool) domain.ProfileBlockVariant {
	occ := "/var/www/{instance}/occ"
	commands := []string{
		occ + " status",
		occ + " maintenance:mode --on",
		occ + " maintenance:mode --off",
	}
	pathRules := "read /var/www/{instance}/data"
	if manage {
		commands = append(commands,
			occ+" app:list",
			occ+" files:scan --all",
			occ+" db:add-missing-indices",
			occ+" upgrade")
		pathRules = "readwrite /var/www/{instance}/config\n" + pathRules
	}
	return domain.ProfileBlockVariant{
		Family:       family,
		RunAs:        webUser,
		SudoCommands: strings.Join(commands, "\n"),
		PathRules:    pathRules,
	}
}
