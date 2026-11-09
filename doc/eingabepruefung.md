# Eingabeprüfung (intern)

Stand: Release B, Zweig `feat/b-valid`. Regel: Das Backend ist die Autorität, die Oberfläche meldet dasselbe früher
und am Feld. Grenzen stehen an zwei Stellen und müssen gleich bleiben:

- Backend: Konstanten in `internal/api/controllers/validate.go` (`maxNameLen` 64, `maxDescriptionLen` 1000,
  `maxHostLen` 253, `maxEmailLen` 254, `maxPersonNameLen` 64, `maxLoginUserLen` 64, `maxPasswordLen` 1024,
  `maxScriptLen` 32768, `maxQueryLen` 100, `maxCronLen` 100, `maxCommandLen` 512, `maxPathLen` 4096, neu:
  `maxListLen` 4096, `maxItems` 500, `maxRecipients` 50, `maxTokenLen` 256, `maxTTLHours` 8760,
  `maxAPIKeyDays` 3650). Helfer: `checkLine`, `checkText`, `checkHost`, `checkEmail`, `checkPort`, neu `checkCount`,
  `checkRange`, `checkEmailList`.
- Oberfläche: `LIMITS` und die Prüffunktionen in `ui/src/lib/validate.ts` (Zeichen werden als Unicode-Zeichen gezählt,
  wie `utf8.RuneCountInString`). Seitenspezifische Prüfung steht in den Modelldateien neben der Seite, nicht im
  Seitencode. Jedes Feld trägt `maxlength` aus `LIMITS`; Zahlenfelder sind Ziffernfelder (`digits`).

Fehlertexte, die mehrere Seiten teilen, stehen unter `check.*` in den Sprachdateien; API-Fehler unter
`errors.<code>` (neu: `validation.too_many`, `validation.out_of_range`, `group.name_taken`,
`custom_action.name_taken`, `notification.name_taken`, `conflict.duplicate`).

## Prüfungen für alle Felder

`middlewares.InputGuard` läuft vor jedem Handler unter `/api/v1` und gilt damit auch für künftige Felder:

- **SQL in Strings:** Pfad, Abfrageparameter und alle Strings im JSON-Rumpf laufen durch die Suchmetrik
  (`inputcheck.SQLScore`). Treffer zählen Punkte, ab 10 gilt ein Text als SQL (`validation.sql_suspicious`, 422,
  Warnung im Log ohne den Wert). Eindeutige Muster wie `UNION SELECT` erreichen das allein, schwächere nur zusammen,
  deshalb bleiben `O'Brien` und „select the servers from the list“ erlaubt. Der eigentliche Schutz bleiben gebundene
  Parameter; die Metrik ist die zweite Linie. Ausgenommen sind Passwörter, Schlüssel, Tokens und Befehle
  (`sqlExemptKeys`): Dort sind beliebige Zeichen und auch SQL legitim, und nichts davon wandert in eine Abfrage.
- **Zahlen in Abfragen:** `limit`, `offset`, `page`, `page_size`, `cols`, `rows` haben feste Bereiche, Unsinn gibt
  `validation.out_of_range` statt einer stillen Vorgabe. Zahlen in Rümpfen dürfen ±2^53 nicht überschreiten.
- **Typen:** `bindBody` nennt das Feld (`validation.not_a_number`, `validation.wrong_type`).

Adressen prüft `internal/inputcheck`, Controller und Services nutzen dieselben Funktionen:

| Eingabe | Fehlercode |
|---|---|
| Buchstabe in IPv4 (`192.168.1.x`) | `validation.ip_letter` |
| Zahl über 255 (`300.1.1.1`) | `validation.ip_octet` |
| Führende Null (`010.0.0.1`, oktal oder dezimal?) | `validation.ip_leading_zero` |
| Nicht vier Zahlen (`192.168.1`) | `validation.ip_format` |
| Kaputtes IPv6 (`2001:db8::g`) | `validation.ipv6` |
| Präfix fehlt oder zu groß (`10.0.0.0/33`) | `validation.ip_prefix` |
| Host-Bits im Netz (`10.0.0.5/24`) | `validation.ip_host_bits` |

`10.0.0.5/24` wurde früher still zu `10.0.0.0/24`. Jetzt ist es ein Fehler: Gemeint kann auch die einzelne Adresse sein.
Hostfelder verlangen: Was wie IPv4 aussieht, muss IPv4 sein. Geprüft sind `host`, `mail_host`, `bind_host`,
Firewall-Quellen, Allowlists, DNS-Server und `dns_server_presets`, CrowdSec-`allowlist_ips` und `unban_ip`.

Einstellungen, die Zahlen bisher still auf die nächste Grenze setzten (Verlauf, Session-Dauer, Job-Fristen,
Advisory-Cache, Kulanzfrist), lehnen sie jetzt ab (`checkSettingsNumbers`). Das Terminal begrenzt `resize`-Nachrichten
weiter selbst (`sshx.clampWindow`), über den WebSocket gibt es keinen Fehlerkanal.

## Firewall

| Feld | Grenze | Oberfläche | Backend |
|---|---|---|---|
| Port | 1–65535 | nur Ziffern, `maxlength` 5, Fehler am Feld | `normalizeFirewallRules` |
| Protokoll | tcp, udp (UI zusätzlich „TCP/UDP“) | „TCP/UDP“ wird beim Speichern zu zwei Regeln, beim Laden bei sonst gleichen Angaben wieder zu einer Zeile (`mergeProtocols`) | tcp/udp je Regel |
| IP-Version | any, v4, v6 (Anzeige IPv4/6, IPv4, IPv6) | Auswahl | `normalizeFirewallRules` |
| Quellen | IPv4, IPv6, CIDR; höchstens 100 je Regel | `isAddressOrNet`, Fehler nennt die ungültigen Einträge; Adresse der falschen Familie bei fester IP-Version | `normalizeSourceIPs` (jetzt mit Obergrenze 100, gilt auch für die SSH-Quellen) |
| Bemerkung | 120 Zeichen | `maxlength`, Fehler | früher stilles Kürzen auf 120 Byte (konnte mitten in einem Zeichen schneiden); jetzt Ablehnung über 120 Zeichen, Kürzen zeichenweise |
| Regeln je Server/Regel | 500 | – | neu abgelehnt |
| Allowlists je Regel | 100 | – | neu abgelehnt |

Dieselben Zeilen nutzt die Serverfirewall (`sec-firewall.ts`, `rulesPayload`); „TCP/UDP“ wirkt dort gleich, auch in
den Vorschlägen aus den lauschenden Diensten und in der Kurzfassung („80/tcp+udp“).

## Gruppen, Zeitpläne, Regeln

| Feld | Grenze | Oberfläche | Backend |
|---|---|---|---|
| Gruppenname | 64, eine Zeile, eindeutig | volle Breite, `maxlength`, Namenskonflikt am Feld | `checkLine`; `ErrGroupNameTaken` (409) |
| Beschreibung | 1000, mehrzeilig | `lcm-textarea`, volle Breite | `checkText` |
| Vorrang | 1–9999 | Ziffern | `applyGroupPriority` |
| Zeitplanname / Cron | 64 / 100 | `maxlength`, Cron-Felder je 19 Zeichen | `checkLines`, Cron-Parser |
| Streuung | 0–1439 | Ziffern | `validateSchedule` |
| Regelname | 64 | `maxlength` | `checkLine` |
| Skript | 32768 | `maxlength`, keine Steuerzeichen außer Umbruch | `checkText` |
| Paketliste | je Name `^[a-z0-9][a-z0-9+._-]*$` | Fehler nennt die ungültigen Namen | `parsePackageNames` |
| Docker-Auswahl | Compose-Name | `isComposeName` | `validDockerScope` |
| DNS-Test-Domains | `^[A-Za-z0-9._-]{1,253}$` | Fehler nennt Einträge | `normalizeDNSTestDomains` |

## Linux-Benutzer, Profile, Bausteine

| Feld | Grenze | Oberfläche | Backend |
|---|---|---|---|
| Benutzername | `^[a-z_][a-z0-9_-]*$`, 32 | wie Backend (kein `$` mehr) | `ValidLinuxUsername` |
| Name | 64 (Oberfläche erlaubte vorher 128) | `maxlength`, eine Zeile | `checkLine` |
| E-Mail | 254 | `isEmail` | `checkEmail` |
| Shell | absoluter Pfad, 64 (Controller), Zeichenmenge | Regex + 64 | `ValidLinuxShell` (128), Controller 64 |
| Profil: Name / Kurzname / Beschreibung | 64 / 20 / 1000 | `maxlength`, Fehler am Feld | `checkProfileRequest` |
| Profil: Befehl / Pfad | 512 / 4096, absolut | `maxlength`, Fehler je Zeile | `checkProfileRequest`, Inhalt im Service |
| Profil: Regelzahl | 500 je Liste | – | neu `checkCount` |
| Baustein: Name, Beschreibungen, Parameter | 64, 1000, 512 | `maxlength`, Fehler am Feld | neu `checkBlockRequest` (vorher keine Grenze) |
| Baustein: Varianten | 32768 je Text, 500 Varianten | `maxlength`, Fehler je Variante | neu `checkBlockRequest`; Inhalt `ValidateBlockVariant` |
| SSH-Schlüssel: Name / Schlüssel | 64 / 32768 | `maxlength` | `checkLine`, `checkText`, Schlüsselprüfung im Service |

## Konto, Anmeldung, Benutzerverwaltung

| Feld | Grenze | Oberfläche | Backend |
|---|---|---|---|
| Anmeldename / Passwort | 64 / 1024 | `maxlength` | Anmeldename neu `checkLine` (vor dem Sperrzähler), Passwort `MaxLoginPasswordBytes` → 401 |
| 2FA-Code | 6 Ziffern | Ziffernfeld | neu `checkLine` |
| E-Mail (Reset, Konto, Benutzer) | 254 | `isEmail`, Fehler am Feld | `checkEmail`, Reset neu geprüft |
| Vor-/Nachname | 64 | `maxlength`, Fehler am Feld | `checkLines` |
| Neues Passwort | 12–200 | `maxlength` 200, Stärkeanzeige | Passwortregeln; Controller neu `checkLine` 1024 |
| Rollenliste | 500, je 64 | – | neu `checkRoleNames` |
| Aktivierungslink-Gültigkeit | 0–8760 Stunden | – | neu `checkRange` (Überlauf von `time.Duration` möglich) |
| API-Schlüssel Name / Tage | 64 / 1–3650 | `maxlength`, Ziffern | Tage neu begrenzt (vorher beliebig groß) |

## Einstellungen

| Bereich | Felder und Grenzen | Oberfläche | Backend |
|---|---|---|---|
| Allgemein | SSH-Benutzer 64, Port 1–65535, Basis-URL 4096 (http/https), Mail-Host (Host/IP), Port, Benutzer 64, Passwort 1024, Absender (E-Mail), Admin-Empfänger (Liste, 50) | `FieldSpec` mit `limit` und `check` in `system/form-state.ts` | neu `checkGlobalSettings` (vorher keine Prüfung der Textfelder); `mail_from` verlangt eine Adresse mit oder ohne Anzeigenamen (`checkSender`: Name höchstens 64, Adresse 254 Zeichen) |
| DNS / Zeit | Listen 4096, Zeitzone 64 | Domains und Zeitzone geprüft | `checkGlobalSettings`, `validTimezone` |
| Sicherheit | 2FA-Rollen, CVE-Cron 100, Gewichtsliste 4096 | Cron-Prüfung | `checkGlobalSettings`, `cronParser` |
| Backups | Uhrzeit, Verzeichnis (absolut, ohne Shell-Zeichen, 4096), Empfänger 4096, Passphrase 1024 | `check: path` | `normalizeBackupDir`; neu `checkLines` im Controller |
| MCP | Bind-Adresse (Host), Port | Host, Loopback | neu `checkHost`/`checkPort` |
| Benachrichtigungen | Name 64, Host, Port, Benutzer 64, Passwort 1024, Absender, Empfänger (50, je eine Adresse), Webhook-URL (https, 4096) | Fehler am Feld, Empfängerliste nennt Einträge | `EmailProvider.Validate` prüft jetzt Host, Absender und Empfänger; Kopfzeilen werden von Zeilenumbrüchen befreit |
| Alarme | Name 64, Schwelle 1–100, Tage 1–3650, veraltet 0–100000, Stunden 1–8760, Sperrfrist 0–525600 | Ziffern, Bereiche | neu `checkAlertRequest` (vorher beliebige Zahlen, auch negative) |
| Repositories | Name 64, Schlüssel 64, Beschreibung 1000, URL/Zeile 4096 | Fehler am Feld | neu `checkLines` + Service-Prüfung |
| APT-Cache | Name 64 (ohne Anführungszeichen), URL 4096 | Fehler am Feld | neu `checkLines`, Service |
| CrowdSec-LAPI | Name 64, URL 4096, Anmeldung 128, Passwort 1024, Konsolen-Schlüssel 1024 | Fehler am Feld | neu `checkLines`, Service |
| Allowlists | Name 64, Beschreibung 1000, Einträge 32768, Fehler nennt ungültige Einträge | volle Breite | neu `checkLine`/`checkText` |
| Eigene Aktionen | Name 64, Beschreibung 1000, Kommandos 32768 | Fehler am Feld, Namenskonflikt | `checkLine`/`checkText`; `ErrCustomActionNameTaken` |
| Anwendungen | Kennung 40, Namen 64, Beschreibungen 1000, Kommando/Muster 512, Quelle 4096, Merkmale 4096 | `APP_LIMITS` | neu `checkAppRequest` |
| Abo | Schlüssel 256, URL 4096 (http/https) | Speichern gesperrt bei ungültiger URL | `maxTokenLen` |

## Server

| Feld | Grenze | Oberfläche | Backend |
|---|---|---|---|
| Name, Host, Port, Benutzer, Passwort (SSH, RouterOS, DSM, Agent) | 64, 253 (`isHost`, auch IPv6 mit Zone), 1–65535, 64, 1024 | `join-model.ts`, Fehler am Feld | `checkHost`, `checkPort`, `checkLines` |
| Verzeichnis abschotten | Pfad absolut ohne Shell-Zeichen und Platzhalter (4096), Gruppe (Benutzername-Regel), Dienst | `hardenErrors` | neu `checkLines`; `ValidateHardenTarget` |
| fail2ban/CrowdSec | Allowlist (IP/Netz), Collections | Fehler nennt Einträge, Speichern gesperrt | `canonicalAddr`, `reCollection` |
| Paket-Pin | Name `^[a-z0-9._+-]+\*?$` bis 128, Notiz bis 1000 | `pinNameProblem` | Name/Notiz neu begrenzt; `validPinName` |
| Suchfelder (alle Listen, Tabellenfilter, Befehlspalette) | 100 | `maxlength` | `maxQueryLen` |

`srv-settings.ts` und `set-*.ts` hat ein anderer Auftrag umgebaut und sind hier nicht erfasst.

## Audit-Befunde

**Behoben**

- Einstellungen (`PATCH /settings/global`): keine Länge-, Zeichen- oder Formprüfung für SSH-Benutzer, Passwörter, Mail-Host, Absender, Empfänger, Listenfelder → `checkGlobalSettings`.
- Regelbausteine: Name, Beschreibung, Parameter, Variantentexte und Zahl der Varianten unbegrenzt (nur der 64-MiB-Body war die Grenze) → `checkBlockRequest`; Klone von Profilen und Bausteinen ohne Prüfung → `checkLines`.
- Alarmregeln: Zahlenfelder ohne Bereich (negativ, riesig) → `checkAlertRequest`.
- E-Mail-Kanal: Host, Absender und Empfänger nicht geprüft; Kopfzeilen konnten Zeilenumbrüche enthalten (SMTP-Header-Einschleusung; `net/smtp` lehnt CR/LF in `MAIL FROM`/`RCPT TO` ab, aber nicht in den Kopfzeilen) → `Validate` und `headerValue`.
- Anmeldung: Benutzername unbegrenzt, wird im Speicher des Sperrzählers als Schlüssel gehalten → `checkLine` vor dem Zähler. Passwort-Reset ohne Adressprüfung, 2FA-Codes ohne Grenze, Aktivierungs-Token und Passwörter unbegrenzt.
- API-Schlüssel: `expires_in_days` ohne Obergrenze (`AddDate` mit riesigen Werten), Aktivierungslink `ttl_hours` (Überlauf von `time.Duration`) → `checkRange`.
- Firewall: Bemerkung wurde still auf 120 Byte gekürzt (konnte ein Mehrbyte-Zeichen zerreißen), Quellenzahl und Regelzahl unbegrenzt.
- Anwendungskatalog, Repositories, APT-Cache, LAPI, Allowlists, Pakete/Docker/Snaps/Pins/Härtung (Namenslisten, Notizen, Pfade): Controller prüften nichts, der Service nur die Form → Länge/Zeichen/Zahl im Controller ergänzt.
- Doppelte Namen (Gruppen, Eigene Aktionen, Benachrichtigungskanäle) endeten als 500 durch den Unique-Index → eigene Fehler (409) mit Vorabprüfung, im Formular am Namensfeld; zusätzlich fängt `jsonErrorHandler` jede übrige Unique-Verletzung als `conflict.duplicate` (409) ab. Allowlist, LAPI, APT-Cache, Baustein, Profil, Katalog-Repository, Linux-Benutzer und Benutzer prüften schon vorher.

**Geprüft, ohne Befund**

- SQL: Kein `Raw`/`Exec`/`Where`/`Order` mit Nutzereingabe. Verkettungen in `storage/repositories` sind Konstanten (`IN (SELECT …)`), die Sortierung der Job-Liste ist eine Positivliste (`jobOrder`), Suchen laufen über `likeContains` mit `ESCAPE`. `storage/database.go` setzt Tabellen- und Spaltennamen aus festen Listen in Migrationen ein (nicht aus Eingaben).
- Shell: Hostnamen/Benutzer (`checkHost`, `ValidLinuxUsername`), Shell (`ValidLinuxShell`), Pakete (`rePackageName`), Snaps, Docker-Namen (`reComposeName`, `reDockerImageRef`), Zeitzone/NTP (`reTimezone`, `reNTPServer`), DNS-Server (IP), Härtungspfade (`ValidateHardenTarget`), Katalog-Quellen (`unsafeShellChars`), Firewall-Bemerkung (`firewallCommentClean`), Allowlist-Adressen (`canonicalAddr`), Anzeigename von Linux-Benutzern (`shellQuote`/Base64) sind gequotet oder per Positivliste geprüft. Eigene Aktionen, Skript-Regeln und das Versionskommando der Anwendungen sind absichtlich frei und nur Berechtigten (`settings:manage`, `rules:manage`) zugänglich.
- Anmeldung und Passwortregeln: Passwort wird erst nach der Längengrenze gehasht (`MaxLoginPasswordBytes`).

**Datenbank**

SQLite kennt keine Spaltenlänge. Das einzige Modell mit `size:` ist `User.Language` (`size:10`, API: Sprachkennung `^[a-z]{2,3}(-…)*$` bis 10 Zeichen, passt). Alle übrigen Textspalten sind unbegrenzt; die API-Grenzen oben sind damit die einzige Obergrenze. Bei einem Wechsel auf PostgreSQL müssten Längen ergänzt werden (Namen 64, Beschreibung 1000, Host 253, E-Mail 254). Bemerkenswert: viele Felder sind per `serializer:aesgcm` verschlüsselt, die Länge in der Datenbank ist dort länger als der Klartext.

**Offen** (größere Punkte stehen zusätzlich in `doc/frontend-neubau-backend-probleme.md`)

- Der Rumpf-Deckel ist ein einziger Wert je Routengruppe (`MaxBodyBytes`); verschachtelte Listen in Rümpfen (Quellen, Rollen, Varianten) sind jetzt gezählt, aber nicht jede Liste in jedem Rumpf (z. B. `names` bei Docker, `uris` beim Zurückstellen auf http).
- `mail_from` und der Absender eines E-Mail-Kanals sind eine einzelne Adresse, mit oder ohne Anzeigenamen (`lcm@example.com`, `LCM <lcm@example.com>`); im SMTP-Umschlag steht nur die Adresse, in `From:` der (nach RFC 2047 kodierte) Absender. Empfänger bleiben einzelne Adressen ohne Namen.
- Der CrowdSec-Konsolen-Schlüssel und Passwörter haben nur die Obergrenze 1024, keine Formprüfung.
- Zeichensatz: Eingaben werden nicht Unicode-normalisiert (NFC); zwei optisch gleiche Namen können als verschieden gelten.
