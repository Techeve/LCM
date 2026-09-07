---
sidebar:
  order: 21
title: Sicherheitsmodell
description: Authentifizierung, JWT, RBAC, API-Keys und die weiteren Härtungsmaßnahmen von LCM.
---

## Überblick

| Baustein | Technologie | Ort |
|---|---|---|
| Passwort-Hashing | argon2id (OWASP-Parameter) | `internal/core/services/auth_service.go` |
| User-Auth | JWT (HS256), 60 min TTL | `AuthService` + `middlewares.Authenticate` |
| Service-Auth | API-Keys (SHA-256-gehasht) | `APIKeyService` |
| Autorisierung | RBAC: User → Rollen → Permissions | `middlewares.RequirePermission` |
| Secrets | generierte `config.json` (0600) | `internal/config/config.go` |

## Passwörter: argon2id

Passwörter werden ausnahmslos mit **argon2id** gehasht (64 MiB Speicher, 4 Threads - resistent gegen GPU-Cracking). Es gibt im gesamten System keinen Ort, an dem ein Klartext-Passwort gespeichert wird. Der Login vergleicht bei unbekanntem Usernamen gegen einen Dummy-Hash, damit sich „User existiert nicht" und „Passwort falsch" zeitlich nicht unterscheiden lassen, und liefert für beide Fälle denselben Fehler (kein User-Enumeration-Leak).

## Passwort-Policy

Der Server ist die **alleinige Autorität**: jede Stelle, die ein Passwort setzt
(Benutzer anlegen, Passwort zurücksetzen, Einladungs-/Aktivierungslink,
Linux-Benutzer-Aktivierung), ruft dieselbe Funktion
`services.EnforcePasswordPolicy` auf. Die Stärkeanzeige in der Oberfläche
spiegelt diese Regeln nur für sofortiges Feedback beim Tippen - sie lässt sich
umgehen und ist bewusst **kein** Sicherheitsmerkmal.

Abgelehnt wird ein Passwort, wenn mindestens eine Regel greift:

| Regel | Begründung |
|---|---|
| kürzer als **12 Zeichen** (in Zeichen, nicht Bytes gezählt) | Untergrenze gegen Offline-Cracking |
| länger als 200 Zeichen | Missbrauch über große Request-Bodies |
| weniger als **3 Zeichenklassen** (Groß, Klein, Ziffern, Sonderzeichen) | ab 20 Zeichen genügen 2 - Länge ersetzt Komplexität (NIST SP 800-63B) |
| weniger als 6 **verschiedene** Zeichen | „AAAAbbbb1111" ist trotz Länge schwach |
| enthält **Benutzernamen, Vor-/Nachnamen oder E-Mail-Teil** | erste Kandidaten jedes gezielten Angriffs |
| wird von einem naheliegenden Begriff **dominiert** (`admin`, `passwort`, `lcm`, …) | „admin-admin-1A!" ist geraten, nicht gewählt |
| ist ein bekanntes **Standard-Passwort** | inklusive Leetspeak und angehängter Jahreszahl: `P4ssw0rt!2026` fliegt genauso auf wie `passwort` |
| enthält eine **vorhersagbare Folge** (`1234`, `abcd`, `qwertz`, `1qaz2wsx`) | vorwärts wie rückwärts geprüft |
| enthält zu viele **Wiederholungen** (`aaa`, oder ein kurzer Block `abcabcabc`) | wirkt lang, ist es aber nicht |
| beginnt/endet mit **Leerzeichen** oder enthält **Steuerzeichen** | Eingabefehler und Kopier-Artefakte |

Die Prüfung liefert **maschinenlesbare Codes** (`too_short`, `contains_identity`,
`common_password`, …). Die Oberfläche übersetzt sie zweisprachig und zeigt
konkret, was fehlt - statt einer pauschalen Ablehnung.

:::note[Bestehende Passwörter]
Die Policy greift beim **Setzen** eines Passworts. Bereits vergebene Passwörter
bleiben gültig, bis sie das nächste Mal geändert werden.
:::

## Zwei-Faktor-Authentifizierung (TOTP)

**Im regulären Betrieb ist 2FA für Administratoren voreingestellt.** Beim
Erst-Seeding einer frischen Datenbank setzt LCM `require_2fa_roles = admin`.
Grund: das Admin-Konto verwaltet SSH-Zugänge und Root-Rechte der gesamten
Flotte - ein alleiniges Passwort ist dafür zu wenig. Die Einstellung ist unter
*Einstellungen → Allgemein* änderbar; unbekannte Rollennamen werden dabei
**abgelehnt**, damit ein Tippfehler die Pflicht nicht lautlos aushebelt.

Im **Entwicklungs- (`--dev`) und Demo-Modus (`--demo`)** bleibt die Pflicht
bewusst aus - dort wäre sie nur ein Hindernis, und beide Modi sind nicht für den
Produktivbetrieb gedacht.

Die Durchsetzung ist serverseitig und lückenlos: `middlewares.AccountRemediation`
arbeitet als Allowlist (fail-closed) und lässt ein Konto, das 2FA einrichten
muss, nur an die dafür nötigen Endpunkte. Alle Stellen, die einen TOTP-Code
prüfen - Login, 2FA-Deaktivierung, eigener Passwortwechsel - teilen sich
**denselben** kontobezogenen Brute-Force-Zähler; ein Wechsel des Endpunkts
umgeht die Sperre also nicht.

## Brute-Force-Schutz

Fehlversuche werden auf **zwei** Schlüsseln gleichzeitig gezählt:

- **pro Client-IP** (Schwelle 5) - stoppt schnelles Durchprobieren von einer Quelle;
- **pro Konto** (Schwelle 15) - stoppt Password-Spraying, das über viele IPs
  verteilt wird und die IP-Sperre gezielt unterläuft. Die höhere Schwelle und die
  gedeckelte Sperrdauer verhindern, dass ein Angreifer ein fremdes Konto billig
  aussperrt.

Beide Sperren wachsen exponentiell (max. 15 Minuten) und zerfallen nach 15
Minuten ohne Fehlversuch. Prüfung und Zählung laufen in **einer** Operation unter
demselben Lock - sonst könnten hunderte parallele Anfragen gemeinsam an der
Prüfung vorbeilaufen, bevor der erste Fehlversuch verbucht ist.

Die Client-IP stammt aus derselben Funktion wie die IP-Allowlist
(`middlewares.ClientIP`) und berücksichtigt `trust_proxy_header`. Ohne das wäre
die Adresse hinter einem Reverse-Proxy für **alle** Clients dieselbe - fünf
Fehlversuche eines Angreifers hätten die Anmeldung der gesamten Installation
gesperrt.

## Links in E-Mails: `public_base_url`

Links in Passwort-Reset- und Einladungs-Mails werden **ausschließlich** aus der
Einstellung `public_base_url` gebildet (*Einstellungen → Allgemein*), niemals aus
dem `Host`-Header des Requests.

:::caution[Warum das wichtig ist]
Würde die Basis-Adresse aus dem Request stammen, könnte ein Angreifer einen
Passwort-Reset für ein **fremdes** Konto mit gefälschtem `Host`-Header anstoßen.
Das Opfer bekäme eine echte LCM-Mail - mit einem gültigen Token auf der Domain
des Angreifers. Ein Klick genügte für die vollständige Kontoübernahme.
:::

Ist `public_base_url` nicht gesetzt, fällt LCM auf die eigene Konfiguration
zurück (Schema, Host und Port des Listeners). Der Link ist dann je nach
Netzaufbau von außen möglicherweise nicht erreichbar - aber niemals
irreführend. Für den Produktivbetrieb sollte die Adresse gesetzt werden.

## JWT-Lebenszyklus

1. **Login** (`POST /api/v1/auth/login`): Nach argon2id-Verifikation stellt der `AuthService` ein HS256-signiertes JWT aus. Claims: User-ID (`sub`), Username, `iat`/`exp`.
2. **Request**: Das Frontend sendet `Authorization: Bearer <jwt>`. Die `Authenticate`-Middleware validiert Signatur und Ablauf - die Signaturmethode ist explizit auf HS256 festgenagelt (`jwt.WithValidMethods`), was Algorithm-Confusion-Angriffe verhindert.
3. **Rollenauflösung**: Permissions stehen **nicht** im Token. Bei jedem Request wird der User samt Rollen/Permissions frisch aus der DB geladen - Rollenänderungen und Deaktivierungen greifen sofort, nicht erst beim nächsten Login.
4. **Ablauf**: Nach `access_token_ttl_minutes` (Default 60) ist das Token ungültig → der Server antwortet 401 → das Frontend loggt automatisch aus (siehe [API-Referenz](/reference/api/)).

Das JWT-Secret wird beim ersten Start kryptografisch zufällig generiert (48 Bytes aus `crypto/rand`) und liegt nur in der `config.json` (Dateirechte 0600). Secrets unter 32 Zeichen werden beim Laden abgelehnt.

**Sitzungs-Invalidierung bei jedem Neustart:** Das effektive HS256-Signaturmaterial ist **nicht** direkt das `jwt_secret`, sondern `HMAC-SHA256(jwt_secret, Instanz-Nonce)`. Das Nonce wird bei jedem Prozessstart neu aus `crypto/rand` gezogen und lebt ausschließlich im Arbeitsspeicher (`deriveSigningKey` in `auth_service.go`). Damit entsteht bei jedem (Neu-)Start ein anderer Signaturschlüssel und **alle** zuvor ausgestellten Tokens fallen bei der Signaturprüfung durch - jede Sitzung endet, ein neues Login ist nötig. Das gilt auch bei unverändertem `jwt_secret` und deckt insbesondere den Fall ab, dass eine alte, weiterhin gültige Session sonst einem frisch geseedeten Admin mit derselben ID vertrauen würde (Rebuild, Prozess-Neustart, neu angelegte Datenbank).

## RBAC: User → Rolle → Permission

```
User "alice" ──> Rolle "admin"   ──> Permissions: users:read, users:write, servers:write, ...
User "bob"   ──> Rolle "manager" ──> Permissions: servers:read, servers:write, jobs:read, ...
```

Permission-Codes sind Konstanten in `internal/core/domain/rbac.go`. Routen schützt man deklarativ:

```go
// Nur eingeloggt:
auth.Post("/2fa/setup", middlewares.RequireAuth(), authCtrl.SetupTOTP)

// Eingeloggt UND Permission:
servers.Get("/:id/storage-history", middlewares.RequirePermission(domain.PermServersRead), serverCtrl.StorageHistory)
```

`RequirePermission` antwortet 401 (nicht eingeloggt) bzw. 403 (eingeloggt, aber Recht fehlt). Die Rollenauflösung passiert im Hintergrund: `Authenticate` lädt den User mit `Preload("Roles.Permissions")`, `HasPermission` prüft dann nur noch in-memory.

**Neue Permission einführen:**

1. Konstante in `domain/rbac.go` definieren (Konvention: `ressource:aktion`).
2. In `storage/seed.go` beschreiben und den passenden Rollen zuweisen.
3. Route damit schützen.
4. Im Frontend optional `auth.can('...')` für das UI nutzen - das ist reine Kosmetik, die echte Prüfung macht immer der Server.

## API-Keys für Service-Kommunikation

Für Prozesse ohne Browser (CI, Cron, andere Dienste) gibt es API-Keys (`X-API-Key`-Header):

- Erzeugung über `POST /api/v1/apikeys` (Permission `apikeys:manage`); der Klartext-Key (`lcm_…`) ist **nur in dieser einen Response** enthalten.
- Gespeichert wird ausschließlich der SHA-256-Hash. Ein DB-Leak verrät keine gültigen Keys.
- Jeder Key läuft im Rechte-Kontext des erstellenden Users - RBAC gilt unverändert.
- Keys können ablaufen (`expires_in_days`) und widerrufen werden.

**Scopes:** Beim Erstellen wählt man `"scope": "read"`, `"readwrite"` (Default) oder `"mcp"`. Ein `read`-Key darf nur `GET`/`HEAD`/`OPTIONS` - jede schreibende Methode wird in der `Authenticate`-Middleware mit **403** abgelehnt, *bevor* Controller-Code läuft und zusätzlich zur RBAC-Prüfung. Damit lassen sich z.B. Monitoring-Keys ausgeben, die selbst im Admin-Kontext nichts verändern können. Ein `mcp`-Key ist strikt isoliert: Er funktioniert **ausschließlich** auf dem separaten MCP-Listener (siehe unten) und wird auf der regulären REST-API/UI abgewiesen. Migration 1 setzt Keys aus älteren Versionen automatisch auf `readwrite` (bisheriges Verhalten).

**Rate-Limiting:** `api_key_rate_limit_per_minute` in der config.json (Default 120, `0` = aus) begrenzt Requests **pro API-Key und Minute** (Fixed-Window, in-memory). Das Limit greift vor der Key-Validierung - auch Brute-Force mit ungültigen Keys wird gedrosselt. Bei Überschreitung: **429** mit `Retry-After`-Header. JWT-Browser-Sessions sind nicht betroffen. Für Multi-Instanz-Deployments gehört das Limit in den Reverse-Proxy oder einen geteilten Store.

Der Seeding-User `system` existiert für Hintergrundprozesse ohne User-Kontext; er kann sich nicht per Passwort einloggen (`IsSystem`).

## Logging & Access-Log

Der Log-Service (`internal/logging/logging.go`) basiert auf `log/slog`:

- **Level** über `log_level` in der config.json (`debug`, `info`, `warn`, `error`).
- **Debug-Modus beim Start:** `./lcm -debug` hebt das Level auf `debug`, ohne die Config zu ändern - für Entwicklung und Fehlersuche.
- **Access-Log** (`access_log: true`): jede API-Anfrage wird mit Methode, Pfad, Status, Dauer, IP und Username protokolliert; 4xx als `WARN`, 5xx als `ERROR`. Im Debug-Level zusätzlich Query-String und User-Agent.
- Es werden niemals Passwörter, Tokens oder Request-Bodies geloggt.

## CVE-Scan des Paketbestands (Trivy)

Der zentral in der DB erfasste Paketbestand aller Server wird täglich (Cron einstellbar, Default `30 2 * * *`) gegen die Trivy-Schwachstellendatenbank geprüft - **agentenlos**: pro Server wird aus den Paket-Records ein CycloneDX-SBOM erzeugt und lokal mit `trivy sbom` gescannt; die verwalteten Server werden dafür nicht kontaktiert.

- **Status-Wirkung:** kritische CVE → Ampel rot, hohe CVE → gelb (mit Insight-Begründung).
- **Graceful Degrade:** Ist Trivy auf dem LCM-Host nicht installiert, deaktiviert sich das Feature sauber (Hinweis in UI und Job-Log); alles andere läuft normal. Trivy wird separat eingerichtet (siehe Installation).
- **Alarme:** Die Alarm-Regel „Security/CVE" benachrichtigt ab einer konfigurierbaren Mindest-Schwere (siehe Einstellungen → Alarme).
- Code: `internal/core/services/cvescan.go` (SBOM/PURL-Bau), `internal/infrastructure/trivy/` (CLI-Anbindung).

## Eingeschränkter Modus des Management-Benutzers

Beim Onboarding (oder nachträglich über *Rechte einschränken*) lässt sich der
LCM-Benutzer von `NOPASSWD:ALL` auf eine sudoers-Whitelist umstellen:
Paketverwaltung (apt, dnf/yum, zypper, pacman, apk), Docker, ufw und der streng
validierende `lcm-helper`. Gesperrt sind damit freie Skripte, Custom-Aktionen
und der Neustart.

**Wirkungsprobe statt Annahme.** Nach dem Umschalten prüft LCM als
eingeschränkter Benutzer, ob der Helper und die Paketverwaltung des Systems
über `sudo` tatsächlich erreichbar sind. Schlägt das fehl, wird der
Voll-Modus im selben Lauf wiederhergestellt und der Fehlschlag gemeldet -
statt einen Server zu hinterlassen, dessen Kernfunktionen tot sind und dessen
Rückweg nur über die Serverkonsole führt. Dazu gehört, dass LCM den
sudo-Suchpfad (`secure_path`) selbst setzt: RHEL 10 und seine Klone liefern
`/usr/local/sbin` nicht mit, wo der Helper liegt; auf openSUSE schaltet LCM
zusätzlich `targetpw` für diesen Benutzer ab.

**Was der Modus leistet - und was nicht.** Er verkleinert die Angriffsfläche
gegen Bedienfehler und versehentliche Eingriffe und macht nachvollziehbar,
was LCM auf dem System tun darf. Er ist **kein** Schutz gegen einen
Angreifer, der den Service-Schlüssel erlangt hat:

- `apt-get`/`dpkg` führen über Hooks (`-o APT::Update::Pre-Invoke::=…`)
  beliebigen Code als root aus,
- `docker run -v /:/host …` bindet das gesamte Wirts-Dateisystem ein.

Beides ist der Zweck dieser Programme, und `sudo` kann ihre Argumente nicht
zuverlässig filtern. Ohne sie wäre der Modus funktionslos, denn Paket-Updates
und Docker sind gerade die Aktionen, die eingeschränkt weiterlaufen sollen.

Wer echten Schutz gegen einen kompromittierten Service-Zugang braucht, müsste
diese Kommandos hinter eng validierende `lcm-helper`-Unterkommandos legen
(nur bestimmte apt-Transaktionen ohne `-o`-Overrides, Docker ohne
Host-Einbindungen und ohne `--privileged`) - das beschneidet den
Funktionsumfang erheblich und ist bewusst nicht der aktuelle Stand.

## Berechtigungsprofile: die Argumente sind Teil der Regel

Ein von LCM verteilter Linux-Benutzer bekommt seine Root-Rechte über ein
**Berechtigungsprofil** (siehe [Linux-Benutzer](/guides/linux-users/)). Die
Sicherheitsgrenze ist dabei die sudoers-Whitelist, und sie steht und fällt mit
den Argumenten: `sudo` vergleicht die **komplette** Kommandozeile.

Die Eingabeprüfung weist deshalb ab, was sich nicht begrenzen lässt:

| Abgewiesen | Warum |
|---|---|
| relativer Pfad | der Suchpfad des Benutzers entschiede, welches Programm als root läuft |
| Platzhalter (`*`, `?`, `[]`) | `apt-get install *` erlaubt jedes Paket - Paketskripte laufen als root |
| Shell-Sonderzeichen, **Komma** | in sudoers **trennt** das Komma Kommandos; ein Komma schmuggelte ein zweites in dieselbe Regel |
| nacktes `systemctl`, `apt-get`, `docker` … | ohne Unteraktion ist jede erlaubt, einschließlich `systemctl edit` - ein Editor als root |

Zwei Ergänzungen setzt LCM selbst:

- **`--no-pager`** bei `systemctl` und `journalctl`. Ohne das läuft der Pager
  als root, und in `less` genügt `!sh` für eine Root-Shell - ein vermeintlich
  lesendes `status`-Kommando wäre ein vollwertiger Rechteaufstieg.
- **`sudoedit` statt Editor-Kommando.** `sudo nano /etc/…` ist faktisch eine
  Root-Shell. `sudoedit` startet den Editor als der Benutzer und schreibt die
  Datei danach als root zurück.

Programme, aus denen sich **unabhängig von den Argumenten** ein beliebiges
Kommando als root starten lässt - Shells, Interpreter, Editoren, Pager, `dd`,
`tee`, `chmod` … - sind nicht verboten, verlangen aber eine ausdrückliche
Bestätigung je Regel samt Audit-Eintrag. Die Liste ist bewusst **nicht**
vollständig: Sie fängt die verbreiteten Fälle ab, die Verantwortung für die
Zusammenstellung eines Profils bleibt beim Betreiber.

Im **eingeschränkten Modus** schreibt nicht LCM die Datei, sondern der
`lcm-helper` - und der übernimmt die Spezifikation nicht blind: Jede Zeile muss
auf die eigene Profilgruppe ausgestellt sein und darf kein `ALL` als Kommando
tragen, danach läuft `visudo`. Ohne diese Prüfung könnte ein kompromittiertes
LCM dem eingeschränkten Service-User über eine Profil-Datei genau die vollen
Rechte zurückgeben, die der Modus verhindern soll.

## SSH-Härtung: belegt, nicht behauptet

Die Härtung schreibt ein eigenes Drop-in (`60-lcm-hardening.conf`) und liest
danach die **effektive** Konfiguration zurück (`sshd -T` wertet Includes und
Match-Blöcke aus). Als gehärtet gilt ein Server nur, wenn `sshd` die
Passwort-Anmeldung nachweislich als `no` meldet:

- meldet er weiterhin `yes` - etwa wegen einer lexikalisch früheren Drop-in-
  Datei, die gewinnt -, wird das Drop-in zurückgerollt und der Fehlschlag
  gemeldet;
- liefert die Prüfung **gar kein Ergebnis**, gilt das ebenfalls nicht als
  Erfolg: `ssh_hardened` bleibt aus und die Antwort benennt den fehlenden
  Nachweis. Bei einer Sicherheitsfunktion ist eine unbelegte Erfolgsmeldung
  schädlicher als ein ehrlicher Fehlschlag - wer „gehärtet" liest, schaut
  nicht mehr hin.

Die Konfigurationsdatei wird dabei nicht unter `/etc/ssh` vorausgesetzt:
openSUSE Leap 16 hat ein stateless `/etc` und liefert sie unter
`/usr/etc/ssh/sshd_config` aus.

## Selbstverwaltung des LCM-Hosts

Die Paketinstallation richtet den eigenen Rechner als verwalteten Server
**`lcm-host`** ein. Ohne das wären die host-spezifischen Funktionen (Trivy,
apt-cacher-ng, CrowdSec-LAPI) auf einer frischen Installation erst nach
manuellem Onboarding erreichbar.

:::caution[Was das bedeutet]
`postinstall.sh` legt dafür das Konto **`lcm-svc` mit `NOPASSWD:ALL`** an
(`/etc/sudoers.d/lcm-svc`, Rechte 0440) und hinterlegt LCM einen SSH-Schlüssel
darauf. **Der Dienst kann anschließend auf dieser Maschine als root handeln,
ohne dass jemand Zugangsdaten eingegeben hat.**

Das ist eine bewusste Abwägung: Ein Werkzeug, das den eigenen Host verwaltet,
braucht dort dieselben Rechte wie auf jedem anderen verwalteten Server. Die
Installationsausgabe benennt den Vorgang bei **jeder** Installation.
:::

### Übergabe des Schlüssels

Der private Schlüssel wird **nicht dauerhaft im Dateisystem abgelegt**:

1. `postinstall.sh` erzeugt das Schlüsselpaar lokal, trägt den Public Key in
   die `authorized_keys` von `lcm-svc` ein und schreibt den Private Key nach
   `/var/lib/lcm/self-onboard.json` (Rechte 0600, Eigentümer `lcm`).
2. Beim nächsten Start liest LCM die Datei, verschlüsselt den Schlüssel mit dem
   Master-Key in die Datenbank und **löscht die Datei** - auch dann, wenn die
   Aufnahme scheitert oder bewusst unterbleibt. Ein Klartext-Schlüssel darf
   nicht liegen bleiben.

Der Host-Key von `127.0.0.1` wird dabei wie bei jedem anderen Server geprobt
und als Vertrauensanker gespeichert: Selbstverwaltung hebt das strikte
Host-Key-Checking nicht auf.

### Wann LCM sich NICHT selbst aufnimmt

| Fall | Verhalten |
|---|---|
| `LCM_NO_SELF_MANAGE=1` bei der Installation | Konto, sudoers-Regel und Übergabedatei entstehen gar nicht erst |
| Container (Docker/Podman/LXC) | Kein Eintrag - dort ist „localhost" der Container, nicht der Host |
| Kein SSH-Dienst erreichbar | Kein Eintrag |
| localhost bereits aufgenommen | Kein Zweiteintrag (erkannt über Loopback + Port 22, nicht über den Namen) |
| Eintrag wurde gelöscht | Kommt nicht zurück - das Löschen setzt `self_server_disabled` |

Der letzte Fall ist der wichtigste Ausweg: Wer die Selbstverwaltung nicht will,
löscht den Server in der Weboberfläche. Das entfernt den Eintrag **und** hält
fest, dass er nicht erneut angelegt werden soll - sonst käme er beim nächsten
`apt upgrade` zurück, weil `postinstall.sh` erneut läuft.

:::note[Rückbau von Hand]
Das Löschen des Servers entfernt den LCM-seitigen Zugang. Konto und sudoers-Regel
auf dem Host bleiben bestehen; wer auch die entfernen will:

```bash
sudo rm -f /etc/sudoers.d/lcm-svc
sudo userdel -r lcm-svc
```
:::

## At-Rest-Verschlüsselung & Master-Key-Rotation

Alle Geheimnisse in der Datenbank werden feldweise mit **AES-256-GCM** verschlüsselt. Der **Master-Key** liegt getrennt von der DB in `lcm.key` (Dateirechte 0600) im Datenverzeichnis und wird beim ersten Start erzeugt (`internal/infrastructure/crypto`). Ohne ihn sind die verschlüsselten Felder unlesbar - deshalb gehört er in jedes [Backup](/guides/backups/).

Verschlüsselt gespeichert werden u.&nbsp;a. (vollständige Liste in `internal/storage/rotate.go`):

- **SSH-Zugänge:** Server-Private-Key (`servers.private_key_enc`), das Default-SSH-Passwort und der Onboarding-SSH-Key in den Einstellungen;
- **RouterOS-Login-Passwort** (`servers.login_password_enc`) für Geräte mit Passwort-Authentifizierung;
- **2FA-Secrets** der Benutzer (`users.totp_secret_enc`) und Linux-User-Passwörter;
- **System-Mailer**-SMTP-Passwort und **Benachrichtigungskanal-Secrets** (SMTP-Passwort bzw. Webhook-URL);
- **CrowdSec-Zugänge** auf dem LCM-Host: LAPI-Maschinen-Passwort (`crowd_sec_lapi_password_enc`) und Console-Key (`crowd_sec_console_key_enc`);
- das hinterlegte **TLS-Key-PEM**.

Großvolumige Konsolen-Ausgaben (Job-/SSH-Output) sowie der Server-Host/-Name laufen über einen GORM-Serializer (`aesgcm`); der Servername trägt zusätzlich einen aus dem Master-Key abgeleiteten **Blindindex** für die Suche, ohne den Klartext zu speichern.

**Rotation:** Das Unterkommando `lcm rotate-db-key` erzeugt einen neuen Master-Key und verschlüsselt alle registrierten Felder in **einer** Transaktion neu - die DB bleibt nie in einem gemischten Zustand. Der Blindindex des Servernamens wird dabei mit dem neuen Schlüssel neu berechnet. Neu eingeführte verschlüsselte Spalten müssen in `encryptedColumns` (bzw. `serializerColumns`) registriert werden, damit die Rotation sie erfasst.

## LCM Remote (Agent-Listener)

Server hinter NAT verbinden sich **ausgehend** per `lcm-agent` (MQTT-über-WebSocket) auf einem **eigenen, dedizierten Port** (Default `9320`, `internal/remote`) - getrennt von UI/REST. Der Enrollment-Token wird nur bei der Erstanzeige im Klartext gezeigt; at rest liegt ausschließlich sein Hash. Der Agent-Listener nutzt dasselbe TLS-Zertifikat wie die UI, dessen Fingerprint der Agent beim Enrollment **pinnt** (MitM-Schutz). Kommandos über den Agent-Transport unterliegen derselben Stille-Frist wie der Job-Watchdog: Sie werden abgebrochen, wenn über die erlaubte Zeit hinaus keine Ausgabe mehr entsteht. Details: [LCM Remote](/guides/remote/).

## MCP-Schnittstelle (KI-Agenten)

Der optionale MCP-Listener (`internal/mcp`, Default aus, Bind `127.0.0.1:9330`) gibt KI-Agenten **ausschließlich read-only** Server-Eigenschaften heraus. Sicherheitsrelevant:

- **Eigener Scope:** Zugriff nur mit einem API-Key vom Scope `mcp` (Bearer-Token); solche Keys funktionieren nirgends sonst.
- **Whitelist-DTO:** Serialisiert wird ausschließlich das kuratierte `ServerView`-Struct - **niemals** das `domain.Server`. Es trägt per Konstruktion keine Passwörter, Login-Benutzer, privaten/öffentlichen Schlüssel, Host-Key-Fingerprints oder Agent-Token.
- **Keine Schreib-Tools:** Es gibt nur `list_servers`, `get_server` und `fleet_summary` - keine konfigurierenden Aktionen.
- **Zur Laufzeit schaltbar:** über *Einstellungen → MCP*. Der Endpunkt spricht HTTP; für Fernzugriff gehört ein TLS-terminierender Reverse-Proxy davor. Details: [MCP-Schnittstelle](/guides/mcp/).

## Weitere Maßnahmen

- **Fehler-Handling:** Interne Fehler erreichen den Client nur als generisches „interner Serverfehler" - keine Stacktraces oder SQL-Details.
- **Security-Header:** `X-Content-Type-Options`, `X-Frame-Options`, `Referrer-Policy`, `Permissions-Policy` (siehe `middlewares/security.go`).
- **XSS:** Das Frontend rendert ausschließlich über Svelte-Templating (automatisches Escaping); `{@html}` wird nicht verwendet.
- **Build-Gates:** `make build` bricht bei `npm audit`- oder `govulncheck`-Funden ab.
- **Default-Bind:** `127.0.0.1` - wer nach außen exponiert, setzt bewusst `"host": "0.0.0.0"` und sollte TLS über einen Reverse-Proxy (Caddy, nginx) terminieren.
- **IP-Allowlist:** `allowed_ips` in der config.json beschränkt den Netzwerk-Zugriff auf zugelassene Client-Adressen (Schlüsselwörter `localhost`/`private` oder IP/CIDR); nicht passende Clients erhalten früh **403** (Middleware `IPAllowlist`, vor Auth/Logging). Gefiltert wird die direkte TCP-Verbindung; hinter einem Reverse-Proxy `trust_proxy_header: true` (wertet `X-Forwarded-For` aus - nur bei vertrauenswürdigem Proxy). Der Matcher liegt im Paket `internal/netfilter`. Siehe [Sicherheit & CVE-Scans](/guides/security-cve/).

## Bewusste Vereinfachungen des Templates

- **Kein Refresh-Token-Flow:** Nach Token-Ablauf ist ein erneuter Login nötig. Für längere Sessions TTL erhöhen oder einen Refresh-Endpunkt ergänzen.
- **Token im localStorage:** einfach und für Desktop-/Intranet-Apps angemessen. Wer strikteren XSS-Schutz braucht, stellt auf httpOnly-Cookies um (dann CSRF-Schutz ergänzen, z.B. Fiber-CSRF-Middleware).
- **Kein Rate-Limiting:** Für exponierte Deployments `limiter`-Middleware von Fiber auf `/auth/login` legen.
