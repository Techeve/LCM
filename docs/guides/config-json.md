---
sidebar:
  order: 26
title: Konfigurationsdatei config.json
description: Alle Felder der config.json auf einen Blick - Aufbau, Vorgaben, Bedeutung, und wie die Datei zu den Einstellungen in der Oberfläche und zu Umgebungsvariablen steht.
---

Die `config.json` ist die Startdatei von LCM. Sie legt fest, wo LCM lauscht, wo
es seine Daten und Logs ablegt und wie es Zertifikate und den CVE-Scanner findet.
Die meisten Einstellungen von LCM stehen **nicht** hier, sondern in der
Datenbank und in der Oberfläche. Einige Werte stehen an beiden Orten. Diese
Seite zeigt die Datei einmal komplett und sagt dir, wer bei Streit gewinnt.

Die Kurzfassung:

- Die Datei kennt **34 Schlüssel** auf oberster Ebene. 16 davon führt LCM
  zusammen mit den Einstellungen in der Oberfläche (**gespiegelt**), zwei
  liefern nur eine **Vorgabe**, die übrigen stehen **nur in der Datei**.
- Eine **Umgebungsvariable** schlägt die Datei. Für die Ports sperrt sie das Feld
  in der Oberfläche.
- Die Datei enthält **kein Geheimnis**, das LCM braucht. Den Master-Key gibt es
  nur als Datei `lcm.key`, als Umgebungsvariable oder als systemd-Credential.
- LCM liest die Datei **beim Start**. Nur der Abschnitt `tls_certificates` lässt
  sich im Betrieb neu laden.

## Wo die Datei liegt und wann sie entsteht

| Installation | Pfad der Datei | Datenverzeichnis |
|---|---|---|
| **Debian-/Ubuntu-Paket** | `/etc/lcm/config.json` | `/var/lib/lcm` |
| **Docker** | `/data/config.json` | `/data` (auf dem Host der gemountete Ordner, etwa `./data`) |
| **Binary ohne Paket** | `config.json` im Datenverzeichnis | das Verzeichnis, in dem das Programm liegt |

Den Pfad bestimmen zwei Flags und eine Umgebungsvariable:

- `--data <verzeichnis>` oder `LCM_DATA` legt das Datenverzeichnis fest. Dort
  liegen Datenbank, `lcm.key`, Zertifikat, Backups und Logs.
- `--config <pfad>` legt die Datei selbst fest. Ohne das Flag ist es
  `config.json` im Datenverzeichnis.
- Die Dienstdatei des Pakets ruft `lcm -config /etc/lcm/config.json -data
  /var/lib/lcm` auf, im Docker-Image steht `LCM_DATA=/data`.

LCM lehnt ein Systemverzeichnis wie `/usr/bin` als Datenverzeichnis ab. Relative
Pfade in der Datei (etwa `database_path`) gelten ab dem Datenverzeichnis.

**Wann sie entsteht:**

- **Paket:** Beim ersten Installieren legt das Paket die Datei mit zehn Schlüsseln
  an (`host`, `port`, `database_path`, `access_token_ttl_minutes`,
  `admin_initial_password`, `log_level`, `access_log`,
  `api_key_rate_limit_per_minute`, `allowed_ips`, `trust_proxy_header`), und zwar
  nur, wenn es noch keine gibt. Bei jedem Update bleibt sie unverändert.
- **Binary und Docker:** Fehlt die Datei beim Start, legt LCM sie mit allen
  Vorgaben an, mit den Rechten `0600`.
- **Danach ergänzt LCM sie.** Beim Start trägt LCM jeden Schlüssel ein, der in
  deiner Datei fehlt, mit dem Wert, der gerade gilt. So findest du neue Optionen
  nach einem Update beim Öffnen der Datei, statt ihren Namen suchen zu müssen.
  Bestehende Werte und unbekannte Schlüssel bleiben, wie sie sind.

Die Datei ist reines JSON. Kommentare und ein Komma nach dem letzten Eintrag sind
nicht erlaubt, LCM startet dann nicht.

## Ein vollständiges Beispiel

So sieht die Datei nach dem ersten Start eines Binaries oder Containers aus. Die
letzten acht Schlüssel (ab `address_mode`) hängt LCM beim ersten Start an,
weil sie zu den gespiegelten Einstellungen gehören.

```json
{
  "host": "127.0.0.1",
  "port": 9310,
  "agent_host": "0.0.0.0",
  "agent_port": 9320,
  "database_path": "app.db",
  "access_token_ttl_minutes": 60,
  "admin_initial_password": "",
  "log_level": "info",
  "log_file": "",
  "access_log": true,
  "api_key_rate_limit_per_minute": 120,
  "acme_http_listen": ":80",
  "acme_dns_exec": "",
  "trivy_path": "trivy",
  "trivy_url": "",
  "trivy_token": "",
  "backup_dir": "",
  "allowed_ips": [],
  "agent_allowed_ips": [],
  "mcp_allowed_ips": [],
  "trust_proxy_header": false,
  "trusted_proxies": [],
  "tls_ca_cert": "",
  "tls_ca_key": "",
  "tls_certificates": [],
  "shared_tls_listen": "",
  "address_mode": "separate",
  "public_host": "",
  "public_base_url": "",
  "public_agent_url": "",
  "public_mcp_url": "",
  "mcp_port": 9330,
  "agent_enabled": true,
  "mcp_enabled": false
}
```

Ein Beispiel für den Betrieb hinter einem Reverse-Proxy auf demselben Host. Du
änderst nur diese Schlüssel, alle anderen bleiben:

```json
{
  "host": "127.0.0.1",
  "public_base_url": "https://lcm.example.com",
  "trust_proxy_header": true,
  "trusted_proxies": ["127.0.0.1"],
  "allowed_ips": ["private"]
}
```

Die Felder erklären die Tabellen im nächsten Abschnitt. Die Spalte
*Oberfläche* sagt, ob du den Wert auch in LCM einstellen kannst:

- **gespiegelt**: Datei und Oberfläche führen denselben Wert, siehe
  [Datei, Oberfläche und Umgebung](#datei-oberfläche-und-umgebung).
- **geht vor**: Die Oberfläche kennt den Wert und gewinnt, die Datei liefert nur
  die Vorgabe.
- **nein**: Der Wert steht nur in der Datei.

## Die Felder

### Weboberfläche und Adressen

| Feld | Typ | Vorgabe | Bedeutung | Oberfläche |
|---|---|---|---|---|
| `host` | Text | `127.0.0.1` (Paket: `0.0.0.0`) | Bind-Adresse der Weboberfläche und der REST-API. MCP bindet an dieselbe Adresse. Umgebung: `LCM_HOST`. | nein |
| `port` | Zahl | `9310` | Port der Weboberfläche, 1 bis 65535. Umgebung: `LCM_PORT`. | gespiegelt |
| `address_mode` | Text | `separate` | Adressmodus: `separate` (A), `one_name` (B) oder `shared_port` (C). | gespiegelt |
| `public_host` | Text | leer | Modus B: der eine Name für alle Dienste, ohne Schema, Port und Pfad. | gespiegelt |
| `public_base_url` | Text | leer | Adresse der Weboberfläche von außen (Modus A und C). LCM baut daraus Links in E-Mails. | gespiegelt |
| `shared_tls_listen` | Text | leer | Adresse des gemeinsamen TLS-Ports, etwa `:443` oder `127.0.0.1:443`. Gesetzt heißt Modus C. Der Host davor ist die Bind-Adresse, er steht nur in der Datei. Im Entwicklungsmodus (`--dev`) gilt der Schlüssel nicht. | gespiegelt (der Port) |

Die Modi und was LCM daraus ableitet, steht unter
[Öffentliche Adressen & Adressmodus](/guides/adressen/).

### Agent

| Feld | Typ | Vorgabe | Bedeutung | Oberfläche |
|---|---|---|---|---|
| `agent_host` | Text | `0.0.0.0` | Bind-Adresse des Agent-Listeners. Leer heißt alle Schnittstellen. Umgebung: `LCM_AGENT_HOST`. | nein |
| `agent_port` | Zahl | `9320` | Port des Agent-Listeners, 0 bis 65535. Umgebung: `LCM_AGENT_PORT`. | gespiegelt |
| `agent_enabled` | Bool | `true` | Schalter für den Agent-Zugang (LCM Remote). | gespiegelt |
| `public_agent_url` | Text | leer | Adresse des Agent-Ports von außen. Leer heißt: Name der Weboberfläche mit dem Port des Agenten. | gespiegelt |

Ein `agent_port` von `0` stammt aus alten Dateien und hieß „Agent aus“. LCM liest
es als `agent_enabled: false` und behält den bisherigen Port. In Modus C gilt das
nicht, dort heißt `0` nur: kein eigener Port neben dem gemeinsamen.

### MCP

| Feld | Typ | Vorgabe | Bedeutung | Oberfläche |
|---|---|---|---|---|
| `mcp_port` | Zahl | `9330` | Port der MCP-Schnittstelle, 1 bis 65535. | gespiegelt |
| `mcp_enabled` | Bool | `false` (neue Installation) | Schalter für MCP. | gespiegelt |
| `public_mcp_url` | Text | leer | Adresse der MCP-Schnittstelle von außen. | gespiegelt |

Ein eigenes Feld für die Bind-Adresse gibt es nicht: MCP bindet an `host`. Mehr
unter [MCP-Schnittstelle](/guides/mcp/).

### Zugriff und Anmeldung

| Feld | Typ | Vorgabe | Bedeutung | Oberfläche |
|---|---|---|---|---|
| `allowed_ips` | Liste | `[]` | Erlaubte Netze für Weboberfläche und API. Leer heißt alle Netze. Einträge sind `localhost`, `private`, eine IP-Adresse oder ein CIDR-Netz, höchstens 64. Andere Clients bekommen `403`. | gespiegelt |
| `agent_allowed_ips` | Liste | `[]` | Dasselbe für den Agent-Endpunkt. | gespiegelt |
| `mcp_allowed_ips` | Liste | `[]` | Dasselbe für MCP. | gespiegelt |
| `trust_proxy_header` | Bool | `false` | `true`: LCM nimmt die Client-Adresse aus `X-Forwarded-For`, aber nur von den Gegenstellen in `trusted_proxies`. Nur einschalten, wenn ein Reverse-Proxy davor steht. | gespiegelt |
| `trusted_proxies` | Liste | `[]` | Adressen, CIDR-Netze oder `localhost`/`private` der Proxys, denen LCM den Header glaubt. Leer heißt: niemandem. `0.0.0.0/0` und `::/0` übernimmt LCM nicht. | gespiegelt |
| `api_key_rate_limit_per_minute` | Zahl | `120` | Anfragen pro API-Schlüssel und Minute. `0` schaltet die Begrenzung ab, negativ ist ein Fehler. Browser-Sitzungen zählen nicht. | nein |
| `access_token_ttl_minutes` | Zahl | `60` | Leerlauf-Frist einer Browser-Sitzung in Minuten, größer als 0. | geht vor (*Sitzungsdauer in Minuten*) |
| `admin_initial_password` | Text | leer | Passwort des Kontos `admin`, nur beim allerersten Anlegen einer frischen Datenbank. Leer heißt: LCM erzeugt eins und gibt es einmalig auf der Konsole aus (`journalctl -u lcm \| grep -A3 'Admin account'`). Es muss beim ersten Login geändert werden. | nein |

Mehr dazu: [Erlaubte Netzbereiche](/guides/netzbereiche/) und
[Reverse-Proxy & Absicherung von außen](/guides/reverse-proxy/#trusted_proxies-wem-x-forwarded-for-geglaubt-wird).

### TLS, Zertifikate und ACME

| Feld | Typ | Vorgabe | Bedeutung | Oberfläche |
|---|---|---|---|---|
| `tls_ca_cert` | Text | leer | Zertifikat einer eigenen CA (PEM, absoluter Pfad), mit der LCM sein Server-Zertifikat ausstellt. Nur zusammen mit `tls_ca_key`. Leer heißt: LCM legt eine eigene CA im Datenverzeichnis an. | nein |
| `tls_ca_key` | Text | leer | Schlüssel dieser CA (PEM, absoluter Pfad). Die beiden Dateien gehören nicht zum Backup. | nein |
| `tls_certificates` | Liste | `[]` | Eigene Zertifikate des Betreibers. Jeder Eintrag hat `cert` (Zertifikat mit Kette) und `key` (Schlüssel ohne Passwort), beide als absolute Pfade, kein Zertifikat doppelt. Lädt eines nicht, startet LCM trotzdem und warnt im Log. | nein |
| `acme_http_listen` | Text | `:80` | Adresse des Listeners für die Prüfung HTTP-01 von Let's Encrypt, `host:port` oder `:port`. Er ist nur während einer Prüfung offen. | nein |
| `acme_dns_exec` | Text | leer | Absoluter Pfad eines Programms für den DNS-Anbieter `exec` bei DNS-01. Nur hier einstellbar, nie über Oberfläche oder API. Ob die Datei da ist, prüft LCM erst beim Auswählen. | nein |

Let's Encrypt selbst (Namen, Modus, Anbieter) steht nicht in der Datei, nur in der
Oberfläche. Die Zertifikate, die du in LCM hochlädst, ebenfalls nicht. Mehr unter
[Zertifikate, Let's Encrypt & Port 443](/guides/zertifikate/).

### Logging

| Feld | Typ | Vorgabe | Bedeutung | Oberfläche |
|---|---|---|---|---|
| `log_level` | Text | `info` | `debug`, `info`, `warn` oder `error`. Das Flag `--debug` hebt den Wert zur Laufzeit auf `debug`, ohne die Datei zu ändern. | nein |
| `log_file` | Text | leer | Pfad der Logdatei. Leer heißt `<Datenverzeichnis>/logs/lcm.log`. Die Datei rotiert (ab 10 MB, bis zu 7 komprimierte Altstände, höchstens 7 Tage). | nein |
| `access_log` | Bool | `true` | Protokolliert jede API-Anfrage mit Methode, Pfad, Status, Dauer, IP und Benutzer. | nein |

### Daten und Backups

| Feld | Typ | Vorgabe | Bedeutung | Oberfläche |
|---|---|---|---|---|
| `database_path` | Text | `app.db` | Pfad der SQLite-Datenbank, relativ zum Datenverzeichnis oder absolut. Ein Backup, dessen `database_path` außerhalb des Datenverzeichnisses liegt, lehnt LCM beim Wiederherstellen ab. | nein |
| `backup_dir` | Text | leer | Zielverzeichnis der Backups (absolut). Leer heißt `<Datenverzeichnis>/backups`. Die Datei liefert nur die Vorgabe: Beim Start trägt LCM sie in die Einstellungen ein, wenn dort noch nichts steht. | geht vor (*Einstellungen → Backups*, Feld *Verzeichnis*) |

### CVE-Scan (Trivy)

| Feld | Typ | Vorgabe | Bedeutung | Oberfläche |
|---|---|---|---|---|
| `trivy_path` | Text | `trivy` | Das Trivy-Programm auf dem LCM-Host, per `PATH` oder als absoluter Pfad. Fehlt es, schaltet sich der CVE-Scan sauber ab. | nein |
| `trivy_url` | Text | leer | Adresse eines Trivy-Sidecars, etwa `http://trivy:9330`, mit `http://` oder `https://`. Gesetzt heißt: LCM startet kein lokales Trivy. Gebraucht im Container-Betrieb. Umgebung: `LCM_TRIVY_URL`. | nein |
| `trivy_token` | Text | leer | Bearer-Token für den Sidecar. Pflicht, sobald `trivy_url` gesetzt ist. Umgebung: `LCM_TRIVY_TOKEN`, oder ein systemd-Credential `trivy_token`. | nein |

Mehr zum Sidecar unter [Paketierung](/reference/packaging/#trivy-im-container-betrieb).

### Was nicht in der Datei steht

Diese Werte gehören nicht in die `config.json`, und LCM schreibt sie auch nicht
hinein:

- **Entwicklungs- und Demo-Modus:** gibt es nur als Flags `--dev`, `--demo` und
  `--demo-public`. Ein `demo_mode` oder `dev_mode` in der Datei liest LCM nicht.
  Sonst könnte eine alte oder zurückgespielte Datei den Betrieb unbemerkt
  abschwächen oder eine frische Datenbank mit Demodaten füllen.
- **Alles Übrige aus den Einstellungen:** Benutzer, Rollen, Mail, Backups,
  Alarme, Let's Encrypt und so weiter liegen in der Datenbank.

## Datei, Oberfläche und Umgebung

### Die Rangfolge

Ganz oben steht die Umgebung. Darunter hängt es davon ab, zu welcher Gruppe das
Feld gehört:

| Gruppe | Wer gilt | Felder |
|---|---|---|
| **Gespiegelt** | Die Einstellungen in der Datenbank. Die Datei folgt ihnen. Was du in der Datei von Hand änderst, übernimmt LCM beim Start in die Einstellungen. Bei `port` und `agent_port` schlägt eine Umgebungsvariable beides. | `port`, `agent_port`, `mcp_port`, `address_mode`, `public_host`, `public_base_url`, `public_agent_url`, `public_mcp_url`, `shared_tls_listen`, `agent_enabled`, `mcp_enabled`, `trust_proxy_header`, `trusted_proxies`, `allowed_ips`, `agent_allowed_ips`, `mcp_allowed_ips` |
| **Vorgabe** | Die Oberfläche, sobald sie einen Wert hat. Die Datei gilt nur, solange dort nichts steht. | `access_token_ttl_minutes` (*Sitzungsdauer in Minuten*, `0` heißt: Vorgabe aus der Datei), `backup_dir` |
| **Nur Datei** | Die Datei. Die Umgebung übersteuert einige Felder. | alle übrigen |

Die Umgebungsvariablen im Einzelnen:

| Variable | Wirkung | Rang |
|---|---|---|
| `LCM_HOST` | `host` | Umgebung vor Datei |
| `LCM_PORT` | Port der Weboberfläche | Umgebung vor Einstellungen und Datei. **Sperrt das Feld in der Oberfläche.** |
| `LCM_AGENT_HOST` | `agent_host` | Umgebung vor Datei |
| `LCM_AGENT_PORT` | Port des Agent-Listeners; `0` schaltet ihn ab | Umgebung vor Einstellungen und Datei. **Sperrt das Feld.** |
| `LCM_TRIVY_URL` | `trivy_url` | Umgebung vor Datei |
| `LCM_TRIVY_TOKEN` | `trivy_token` | Ein systemd-Credential `trivy_token` geht vor der Umgebung, die Umgebung vor der Datei. |
| `LCM_DATA` | Datenverzeichnis | Das Flag `--data` geht vor. |

Der Docker-Container setzt `LCM_HOST=0.0.0.0`. Dort ändert ein `host` in der Datei
nichts, solange du die Variable nicht überschreibst.

Das Flag `--debug` ist der einzige Weg, der `log_level` übersteuert.

### Gespiegelte Werte: was beim Speichern und beim Start passiert

**Speichern in der Oberfläche.** LCM schreibt den Wert zuerst in die Datenbank und
dann in die Datei. In der Datei ändert es nur die gespiegelten Schlüssel, die
fehlen oder abweichen. Alle anderen Schlüssel bleiben mit Wert und Reihenfolge,
neue Schlüssel kommen ans Ende. Die Datei wird atomar ersetzt und behält ihre
Rechte. Die Einrückung ist danach die von LCM (zwei Leerzeichen), von Hand gesetzte
Formatierung geht verloren. Die Netzlisten schreibt LCM in einheitlicher Form: aus
`10.0.0.5` wird `10.0.0.5/32`.

Klappt das Schreiben nicht, etwa weil die Datei schreibgeschützt eingehängt ist,
gilt die Änderung trotzdem. LCM warnt im Log, und die Antwort der API enthält
`config_sync_error`.

**Start von LCM.** Vor dem Öffnen der Ports vergleicht LCM die Datei mit den
Einstellungen:

1. Ein Schlüssel, der in der Datei fehlt, gilt nicht als Änderung. LCM trägt ihn mit
   dem Wert der Einstellungen ein.
2. Ein Schlüssel, der von den Einstellungen abweicht **und** vom zuletzt von LCM
   geschriebenen Stand, gilt als Handänderung. LCM übernimmt ihn in die
   Einstellungen und nennt ihn im Log (`value changed by hand, adopted into the
   settings`). Dabei gelten dieselben Prüfungen wie bei einer Eingabe in der
   Oberfläche.
3. Ein Wert, der nur dem zuletzt geschriebenen Stand entspricht, ist veraltet, etwa
   nach einem Schreibfehler oder einer zurückkopierten alten Datei. Dann gilt die
   Datenbank, und LCM bringt die Datei auf ihren Stand.
4. Ist ein Wert ungültig, startet LCM trotzdem. Der Wert in den Einstellungen gilt
   weiter, der Schlüssel bleibt in der Datei, wie er ist, und das Log warnt bei
   **jedem** Start (`value not adopted, the database value applies`), bis du ihn
   korrigierst. Zusammengehörige Werte (etwa `trust_proxy_header` und
   `trusted_proxies`) übernimmt LCM dann womöglich erst beim zweiten Start.
   Korrigiere deshalb zuerst den Fehler.

So kommst du wieder in LCM hinein, wenn du dich mit den erlaubten Netzbereichen
ausgesperrt hast: Setze in der Datei `"allowed_ips": []` und starte LCM neu. Ohne
Neustart geht `sudo lcm network reset ui`
([Erlaubte Netzbereiche](/guides/netzbereiche/)).

**Was die Umgebung sperrt.** Nur eine Umgebungsvariable sperrt ein Feld, denn LCM
kann sie nicht ändern. Das betrifft `LCM_PORT` (Port der Oberfläche) und
`LCM_AGENT_PORT` (Port des Agenten, und mit `0` auch den Agent-Schalter). Die
Oberfläche zeigt das Feld gesperrt mit dem Hinweis „festgelegt durch eine
Umgebungsvariable“. Die Schlüssel `port` und `agent_port` in der Datei übernimmt
LCM dann nicht, und es schreibt sie auch nicht. Ein Wert in der Datei sperrt
nichts.

**Läuft LCM, ändere die gespiegelten Schlüssel nicht in der Datei.** LCM liest die
Datei im Betrieb nicht. Beim nächsten Speichern in der Oberfläche überschreibt es
deine Änderung mit dem Wert der Einstellungen. Stoppe LCM, ändere die Datei und
starte neu.

## Änderungen wirksam machen

| Was du änderst | Wie es wirkt |
|---|---|
| Gespiegelte Werte **in der Oberfläche** | sofort, ohne Neustart. Der neue Port der Weboberfläche öffnet zuerst, der alte schließt nach 10 Sekunden. Netzbereiche und Proxy-Vertrauen gelten ab der nächsten Anfrage. |
| `tls_certificates` in der Datei | `sudo lcm tls reload` oder `sudo systemctl reload lcm` (Signal `SIGHUP`). Im Container: `docker kill -s HUP lcm`. Das lädt nur diesen Abschnitt, Uploads und ACME-Zertifikate neu. Ein fehlerhaftes Zertifikat ersetzt kein gutes. |
| **alles andere** in der Datei | Neustart: `sudo systemctl restart lcm`, im Container `docker compose restart`. |

Ein Neustart beendet alle Sitzungen, jeder muss sich neu anmelden.

So bearbeitest du die Datei sicher:

```sh
sudoedit /etc/lcm/config.json
python3 -m json.tool /etc/lcm/config.json > /dev/null && echo "JSON ok"
sudo systemctl restart lcm
journalctl -u lcm -n 50
```

Das Log nennt jeden Wert, den LCM übernommen oder abgelehnt hat.

## Rechte und Geheimnisse

**Rechte.** Das Paket legt die Datei als `root:lcm` mit `0660` in einem Verzeichnis
mit `0770` an: LCM muss sie schreiben können, weil es neue Optionen einträgt und
ein Restore sie ersetzt. Legt LCM die Datei selbst an, hat sie `0600`. LCM prüft die
Rechte beim Start und alle 15 Minuten. Gehört die Datei dem Dienst und darf „anderen“
etwas, nimmt LCM es zurück und protokolliert es. Ein Docker-Volume muss für den
Container-Benutzer (UID 1000) beschreibbar sein. Hängst du die Datei schreibgeschützt
ein, startet LCM trotzdem, kann aber keine Optionen ergänzen und nichts
zurückschreiben.

**Wer die Datei schreiben darf, kann den Zugriffsschutz umgehen.** Über
`allowed_ips` und `trusted_proxies` steuert sie, wer LCM erreicht und welchen
Proxys LCM glaubt. Gib das Recht nur an die, die den LCM-Host betreuen.

**Was nie hineingehört:**

| Geheimnis | Wo es stattdessen liegt |
|---|---|
| **Master-Key** | `lcm.key` im Datenverzeichnis, die Umgebungsvariable `LCM_ENCRYPTION_KEY` oder ein systemd-Credential. Siehe [Sicherheitsmodell](/reference/security-model/). |
| **Schlüssel der 2FA-Challenge** | Entsteht bei jedem Start im Speicher. |
| **Passphrase für Backups** | verschlüsselt in den Einstellungen, oder `LCM_BACKUP_PASSPHRASE`, oder ein Credential `backup_passphrase`. |
| **Passwörter und Zugangsdaten** (SSH, Mail, CrowdSec, Anbieter) | verschlüsselt in der Datenbank. |

Zwei Felder können ein Geheimnis im Klartext tragen: `trivy_token` und
`admin_initial_password`. Beide stehen mit der Datei in jedem Backup. Nimm für das
Token lieber `LCM_TRIVY_TOKEN` oder ein Credential, und leere das
Anfangspasswort nach der ersten Anmeldung.

## Typische Fehler

| Symptom | Ursache und Lösung |
|---|---|
| LCM startet nicht, das Log sagt `config.json parsen` | Das JSON ist ungültig: ein Komma zu viel, ein Kommentar, eine Zahl in Anführungszeichen. Prüfe die Datei mit `python3 -m json.tool`. |
| Eine Änderung wirkt nicht | Ein Tippfehler im Schlüsselnamen. LCM ignoriert unbekannte Schlüssel, ohne zu warnen. Vergleiche den Namen mit den Tabellen oben. |
| LCM startet nicht, das Log nennt einen Wert | Die Prüfung hat ihn abgelehnt: `port` oder `agent_port` außerhalb von 1 bis 65535 (beim Agenten ab 0), `log_level` unbekannt, `access_token_ttl_minutes` nicht größer als 0, `api_key_rate_limit_per_minute` negativ, `acme_http_listen` ohne Port, ein Pfad in `tls_ca_cert`, `tls_ca_key`, `tls_certificates` oder `acme_dns_exec` nicht absolut, nur eins von `tls_ca_cert` und `tls_ca_key` gesetzt, `trivy_url` ohne `http://` oder ohne `trivy_token`. |
| `port` oder `agent_port` in der Datei ändert nichts | `LCM_PORT` oder `LCM_AGENT_PORT` ist gesetzt und gewinnt. Entferne die Variable oder ändere sie dort. |
| `host` ändert im Container nichts | Das Image setzt `LCM_HOST=0.0.0.0`. Setze die Variable neu, wenn du etwas anderes willst. |
| Ein gespiegelter Wert springt zurück | Du hast die Datei bei laufendem LCM bearbeitet, und das nächste Speichern in der Oberfläche hat sie überschrieben. Stoppe LCM, bevor du die Datei änderst. |
| Ein gespiegelter Wert aus der Datei kommt nicht an | Der Wert ist ungültig oder entspricht dem zuletzt geschriebenen Stand. Das Log nennt den Schlüssel (`value not adopted`). |
| Ein eigenes Zertifikat wird nicht ausgeliefert | Es lädt nicht (Pfad, Rechte für den Benutzer `lcm`, Schlüssel passt nicht). LCM startet trotzdem. Das Log nennt den Grund, `sudo lcm tls status` zeigt den Stand. |
| Nach einem Update hat die Datei neue Schlüssel | Das ist gewollt, LCM trägt sie mit ihrem Vorgabewert ein. Du musst nichts tun. |
| LCM kann die Datei nicht ergänzen | Sie gehört einem anderen Benutzer oder ist schreibgeschützt. LCM startet trotzdem und warnt im Log (`could not add new options`). |

## Siehe auch

- [Öffentliche Adressen & Adressmodus](/guides/adressen/): Modi, Ports und die
  gespiegelten Schlüssel im Detail
- [Erlaubte Netzbereiche](/guides/netzbereiche/): `allowed_ips` und `lcm network reset`
- [Zertifikate, Let's Encrypt & Port 443](/guides/zertifikate/): `tls_certificates`,
  `tls_ca_cert`, `acme_http_listen`, `acme_dns_exec`
- [Backups](/guides/backups/): Inhalt des Archivs, `backup_dir`
- [Installation](/getting-started/installation/): Flags und Umgebungsvariablen
- [Sicherheitsmodell](/reference/security-model/): Master-Key und Credentials
