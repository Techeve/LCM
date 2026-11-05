# Release B: Frontend-Neubau

Stand: 31.10.2026 · Status: abgestimmter Entwurf · Teil des Fahrplans
(`doc/plan-2026-10-31-fahrplan.md`)

## Ziel

Das LCM-Frontend wird neu gebaut, als würde man es neu entwerfen:
- **Lit** mit eigenen Web Components, **TypeScript**, eigene Designsprache und
  eigenes CSS. Bootstrap und Svelte werden vollständig entfernt.
- **Vollbild:** volle Fensterbreite, mehr Platz für Tabellen und Details.
- **Einheitlicher, organischer Look**, der nach LCM aussieht.
- **Schneller als heute** (Leistungsziele unten) und **sicherer** (Abschnitt
  Sicherheit).
- **Vollständig übersetzbar:** jeder Text über i18n, weitere Sprachen ohne
  Codeänderung.
- **Selbsterklärend:** große Erklärblasen an Aktionsknöpfen und
  zusammenklappbare Erklärspalten auf komplexen Seiten sagen, was passiert, was
  es bedeutet und was auf dem Server ausgeführt wird.
- Gebaut wird von den **Aufgaben** her („Zustand der Flotte erfassen“, „Server
  finden und Probleme beheben“, „Regeln einer Gruppe pflegen“). Screenshots des
  alten Frontends sind keine Vorlage.

## Navigation

### Hauptmenü oben

```
[Logo LCM]  Dashboard  Server  Gruppen  Linux-Benutzer  Sicherheit  Container  Jobs  Einstellungen     [Suche] [Doku] [Theme] [Sprache] [Konto]
```

- Das Logo führt zum Dashboard.
- Rechts: Suche (Befehlspalette, siehe unten), Doku, hell/dunkel/automatisch,
  Sprachauswahl, Kontomenü (Mein Konto, Tastenkürzel, Abmelden).
- Menüpunkte erscheinen nur mit passender Berechtigung (wie heute).
- Auf schmalen Bildschirmen klappt das Menü in ein Menü-Symbol.

### Seiten

| Menüpunkt | Inhalt |
|---|---|
| **Dashboard** (neu) | Überblick über die Flotte, siehe unten. |
| **Server** (neu als eigene Seite) | Die Serverliste mit Ampel, Filtern und Serverdetail, die heute auf dem Dashboard liegt. |
| **Gruppen** | Liste und Detail: Server, Manager, Vorrang, ACL-Status, Zeitpläne und Regeln. |
| **Linux-Benutzer** | Benutzer, Schlüssel, Aktivierung, Gruppen-Zuordnung, Profile, Regelbausteine. |
| **Sicherheit** | CVE-Übersicht, Sammel-Update, Scanner-Datenbank. |
| **Container** | Bisher „Docker“, umbenannt. Inhalt wie heute (Images, Container, Compose-Projekte). Name und URL sind so gewählt, dass weitere Container-Laufzeiten später ihren Platz haben. |
| **Jobs** | Verlauf mit Filtern, Ausgabe, SSH-Sitzungen je Job, Abbrechen. Laufende und wartende Jobs live. |
| **Einstellungen** | Mit Seitenmenü links, neu gegliedert (unten). |

### Dashboard

Oben die **Statuskacheln** wie heute (hervorragend, in Ordnung, Warnung,
kritisch), ergänzt um „LCM handlungsunfähig“ aus Release A. Jede Kachel führt
zur gefilterten Serverliste.

Darunter in Spalten:
- **Braucht Aufmerksamkeit:** rote und gelbe Server mit dem wichtigsten
  Insight, direkt verlinkt.
- **Zuletzt gelaufen:** die letzten Jobs mit Server, Art, Ergebnis, Dauer.
- **Demnächst:** die nächsten Läufe der Zeitpläne (berechnet aus den
  Cron-Ausdrücken) mit Gruppe und Regeln.
- **Letzte Alarme.**

Alles aktualisiert sich live über den Ereigniskanal. Das Dashboard holt seine
Daten mit **einem** zusammengefassten Aufruf (`GET /api/v1/dashboard`), der im
Backend kurz zwischengespeichert wird.

### Server

- Tabelle mit Ampel, Name, Host, OS (mit Distributionslogo), Kernel, zuletzt
  gesehen, Speicher. Filter: Name, Host, OS, Kernel, Status; Sortierung;
  Spaltenwahl; Auswahl für Sammelaktionen.
- „Server hinzufügen“ (Assistent für Agent, SSH, RouterOS, Synology DSM).
- **Liste und Detail nebeneinander** auf breiten Bildschirmen: Die Liste
  bleibt links stehen, rechts das Serverdetail. Auf schmalen Bildschirmen
  normale Seitenfolge.
- **Serverdetail** in Reitern, jeder Reiter ein eigenes, erst bei Bedarf
  geladenes Modul mit eigener URL: Übersicht (mit Betriebsfähigkeit aus A),
  Repositories, Pakete, Snaps, Container, Apps, Benutzer, Sicherheit,
  Deep-Scan, Speicher, Jobs, SSH-Sitzungen. Aktionen (Terminal, Firewall, sudo
  einschränken, Einstellungen, Entfernen) in einer Aktionsleiste im Kopf.

### Einstellungen: Seitenmenü, neu gegliedert

| Abschnitt | Seiten |
|---|---|
| System | Allgemein, Abo, Backups, Zeit, DNS |
| Zugang | Benutzer, API-Schlüssel, MCP, Sicherheit |
| Pakete & Quellen | Repositories, APT-Cache, Apps |
| Schutz | CrowdSec, Allowlists |
| Automatisierung | Zeitpläne, Eigene Aktionen |
| Meldungen | Benachrichtigungen, Alarme, Ereignisse (Live-Log) |

Release C ergänzt einen Abschnitt „Dienste & Patchen“ (siehe Plan C).

## Designsprache

- **Ruhig und organisch:** weiche Radien, geschichtete Flächen statt harter
  Rahmen, sparsame Verläufe, Bewegung nur zur Orientierung, abschaltbar über
  `prefers-reduced-motion`.
- **Die Ampel als Markenzeichen:** eigene, unverwechselbare Statusform (Punkt
  mit Ring, Puls bei „läuft gerade“), überall gleich, immer mit Text.
- **Dichte wählbar:** komfortabel und kompakt.
- **Icons als Inline-SVG**, Distributions- und Herstellerlogos aus
  simple-icons, keine Emoji.
- **Schrift im Binary eingebettet**, keine externen Abrufe.
- **Design-Tokens** als CSS Custom Properties: Flächenstufen, Textstufen,
  Akzent, Statusfarben (ok, Warnung, kritisch, Info, unbekannt, läuft),
  Abstands-Skala, Radien, Erhebung, Schriftgrößen, Bewegungsdauern. Hell und
  dunkel nur über Tokens.

**Gestaltungsablauf:**
1. Drei Designrichtungen als statischer Komponentenkatalog mit Testdaten
   (Ampel, Tabelle, Karte, Formular, Hauptmenü, Dashboard, Serverdetail).
2. Tony wählt bzw. kombiniert. Daraus werden Tokens und Komponenten.
3. Erst dann entstehen die Seiten.

**Entschieden (01.11.2026):** hell wie Richtung „Ruhe“ (warmes Papier), dunkel
wie „Leitstand“ (Graphit-Blau, leuchtende Ampel), weiche Formen, technische
Daten in Monospace. Umschalter hell/dunkel/automatisch im Hauptmenü. Es werden
nur Daten gezeigt, die LCM tatsächlich erfasst (keine Auslastungsverläufe).

## Komponenten

Hauptmenü · Einstellungs-Seitenmenü · Brotkrumen · **Befehlspalette**
(siehe unten) · Statusanzeige mit
Insight-Popover · **Datentabelle** (Sortieren, Filtern, Spaltenwahl, Auswahl,
Tastaturnavigation, virtuelles Scrollen) · Reiter (an die URL gebunden) ·
Karte · Formularbausteine (Eingabe, Auswahl, Combobox, Schalter,
Cron-Baukasten, Passwort mit Stärkeanzeige) · **Seitenschublade** zum
Bearbeiten (Kontext bleibt sichtbar) · Bestätigungsdialog, bei zerstörenden
Aktionen mit getipptem Servernamen · Toasts · Leerzustände, Ladeskelette ·
eigene kleine SVG-Diagramme · Terminal (xterm) · Live-Logansicht ·
Job-Fortschritt.

Alle Komponenten stehen in einem Komponentenkatalog (nur im
Entwicklungsmodus) und werden dort mit axe geprüft. Tastenkürzel (`g` +
Buchstabe, `?`, Strg/Cmd+S) bleiben.

**Befehlspalette (Strg/Cmd+K)**, bewusst schlank, nicht für jede Funktion:
- Seiten des Hauptmenüs und die Einstellungsseiten anspringen,
- **Suche** nach Servern und Gruppen per Name,
- die wenigen **Kernfunktionen der aktuellen Seite**, die die Seite selbst
  anmeldet (z. B. im Serverdetail: Pakete aktualisieren, Updates einspielen,
  Terminal öffnen),
- allgemeine Befehle: zurück, Theme, Sprache, abmelden.

## Mehrsprachigkeit

Ziel: Eine weitere Sprache hinzuzufügen heißt **eine Sprachdatei anlegen**,
ohne Codeänderung. Dafür wird ausnahmslos **jeder** sichtbare Text übersetzt:
Schaltflächen, Menüs, Überschriften, Formularfelder, Platzhalter,
Validierungsmeldungen, Fehlermeldungen, Toasts, Bestätigungen, Leerzustände,
Hilfetexte, Tooltips, `aria-label`s, Seitentitel.

### Frontend

- **Eine Datei je Sprache** (`locales/de.json`, `locales/en.json` …), Schlüssel
  nach Bereichen gegliedert (`servers.packages.upgrade.label`). Geladen wird nur
  die aktive Sprache, als eigener Teil des Builds.
- **Sprachliste** aus den vorhandenen Dateien. Die Auswahl im Kopf ist eine
  Liste, kein Umschalter zwischen zwei Sprachen.
- **Plural** über `Intl.PluralRules`, damit auch Sprachen mit mehr als zwei
  Pluralformen (z. B. Polnisch, Russisch) funktionieren. Platzhalter benannt
  (`{count}`, `{server}`).
- **Datum, Uhrzeit, Zahlen, relative Zeiten** („vor 3 Minuten“) über
  `Intl.DateTimeFormat`, `Intl.NumberFormat`, `Intl.RelativeTimeFormat` in der
  gewählten Sprache. Keine selbst gebauten Formate.
- **Rechts-nach-links vorbereitet:** CSS nur mit logischen Eigenschaften
  (`margin-inline-start` statt `margin-left`), `dir` am `<html>` je Sprache.
- **Rückfall:** fehlt ein Schlüssel in einer Sprache, gilt Englisch, dann
  Deutsch. Deutsch und Englisch sind immer vollständig.
- **Sprache am Benutzerkonto** gespeichert (gilt auf allen Geräten), beim
  ersten Aufruf aus der Browsersprache vorgeschlagen.

### Backend

- **Fehler mit Code statt Text:** Heute liefert die API deutsche Texte
  (`{"error": "ungültiger Request-Body"}`, `router.go` `jsonErrorHandler`).
  Neu: `{"error": "invalid request body", "code": "request.invalid_body",
  "params": {…}}`. Das Frontend übersetzt `code` in die gewählte Sprache und
  setzt `params` ein (Servername, Paket, Pfad …). Der Text in `error` ist
  **Englisch** und für API-Clients und MCP gedacht. Alle
  `fiber.NewError`-Aufrufe werden auf einen Helfer mit Code, Parametern und
  englischem Text umgestellt.
- **Ampel-Insights** haben schon Schlüssel und Parameter, das bleibt so.
- **Kataloginhalte** (App-Katalog, Regelbausteine) haben heute je ein festes
  Feld `name_en`/`description_en`. Das wird durch eine Übersetzungstabelle bzw.
  ein Übersetzungsfeld je Sprache ersetzt. Mitgelieferte Einträge kommen mit
  Übersetzungen, selbst angelegte Einträge in der Sprache, in der sie angelegt
  wurden.
- Job-Ausgaben und Protokolle bleiben unübersetzt, wie sie vom Server kommen.

### Prüfung im CI

- Kein fest eingetragener Text in Komponenten (Lint-Regel).
- Jeder Schlüssel in Deutsch und Englisch vorhanden, keine verwaisten Schlüssel,
  gleiche Platzhalter in allen Sprachen.
- Jeder Fehlercode des Backends hat eine Übersetzung.
- Für weitere Sprachen: Bericht über fehlende Schlüssel statt Abbruch.

## Eine Oberfläche, die sich selbst erklärt

Ziel: Wer eine Funktion zum ersten Mal sieht, versteht ohne Doku, **was
passiert, was es für ihn bedeutet und was ausgeführt wird**. Je komplexer die
Funktion, desto mehr Erklärung. Die Oberfläche bleibt dabei ruhig: **kein Text
unter jedem Knopf**, Navigation und selbsterklärende Knöpfe bekommen keine
Erklärung.

LCM wird fast nur im Desktop-Browser genutzt. Erklärungen sitzen deshalb
vor allem in **großen Hover-Blasen**, nicht dauerhaft auf der Seite.

### Bausteine

- **Erklärblase an Aktionsknöpfen:** Knöpfe, die etwas auf Servern auslösen,
  zeigen beim Überfahren mit der Maus (und bei Tastaturfokus) nach kurzer
  Verzögerung eine **große Erklärungsblase**:
  - was passiert und was es für dich bedeutet, z. B. *„Installiert alle
    verfügbaren Updates auf diesem Server. Dienste werden dabei ggf. neu
    gestartet.“*,
  - **was ausgeführt wird:** die tatsächlichen Befehle, betroffene Pakete bzw.
    Dienste, ob ein Neustart nötig wird, ob es umkehrbar ist, ungefähre Dauer.
    Die Befehle liefert das Backend aus **demselben Code**, der sie später
    ausführt (`GET /api/v1/…/preview`, erst beim Öffnen der Blase geladen). So
    kann die Vorschau nie etwas anderes zeigen, als tatsächlich passiert.

  Das gilt auch für Knöpfe in Werkzeugleisten. Die Leiste selbst bleibt
  schlicht.
- **Erklärspalte auf Seiten** (wie in ZPO, `zpo-explainer`): Bei komplexen
  Seiten (z. B. Gruppenregeln, Firewall, Zeitpläne, in C „Dienste & Patchen“)
  eine Spalte bzw. ein Block, der erklärt, wozu die Funktion da ist und wie sie
  mit dem Rest zusammenhängt. **Zusammenklappbar**, der Zustand wird je Seite im
  Browser gemerkt.
- **Einstellungen mit Folgen:** Bei Feldern, deren Wirkung nicht offensichtlich
  ist, eine kurze Beschreibung. Bei Schaltern in der Erklärblase, was **an** und
  was **aus** bewirkt (*„An: Kerndienst-Pakete werden nur über die
  Patch-Richtlinie aktualisiert. Aus: Der Zeitplan aktualisiert alle Pakete.“*).
- **Bestätigungen mit Wirkung:** welche Server, was läuft, ob Ausfallzeit
  entsteht, ob es umkehrbar ist. Kein bloßes „Sind Sie sicher?“.
- **Leerzustände erklären die Seite:** wozu sie da ist und was der erste
  Schritt ist.
- **Insights mit „Was tun?“:** Jede Ampelmeldung hat eine kurze Erklärung und,
  wo möglich, die passende Aktion direkt daneben.
- **Begriffe erklären:** Fachbegriffe (Kerndienst, Verbund, Vorrang,
  Grundsatz-Regel …) zeigen beim Überfahren eine Erklärung aus einem
  gemeinsamen Glossar.
- **„Mehr in der Doku“** in Blasen und Erklärspalten springt direkt zum
  passenden Abschnitt.

### Regeln

- Alle Hilfetexte sind i18n-Schlüssel nach festem Muster:
  `….label`, `….help` (Erklärblase), `….on` / `….off` (Schalter),
  `….confirm` (Bestätigung), `….explainer` (Erklärspalte).
- Die Komponente für **Server-Aktionen** verlangt einen `help`-Text. Fehlt er,
  schlägt der Build fehl. Für alle anderen Knöpfe ist er freiwillig.
- Schreibweise: Du-Form wie heute, kurze Sätze, Folge vor Technik („Der Server
  startet neu“ vor „führt `reboot` aus“).
- Befehlsvorschauen zeigen keine Geheimnisse (Passwörter, Tokens werden
  maskiert) und nur Benutzern mit Rechten auf den Server.

## Routing und Adressleiste

- **History-Routing mit echten Pfaden:** `/servers/42/packages`,
  `/groups/7/rules`, `/containers`, `/settings/alerts`. Das Backend liefert für
  unbekannte Pfade schon heute `index.html` aus. Alte `#/…`-Links und `/docker`
  werden umgeleitet, Lesezeichen funktionieren weiter.
- **Alles, was eine Ansicht ausmacht, steht in der URL:** Reiter, Filter,
  Sortierung, Seite, Suche, offene Schublade. Ein kopierter Link zeigt genau
  dieselbe Ansicht. Zurück/Vor funktioniert auch für Reiter und Schubladen.
- **Direktsprung:** `/servers/42`, zusätzlich `/servers/by-name/web03` als
  Weiterleitung.
- **Rückkehr nach Anmeldung:** Ohne Sitzung führt jede geschützte Seite zu
  `/login?next=…`. Nach Passwort und 2FA geht es genau dorthin zurück. Läuft
  die Sitzung während der Arbeit ab, wird die aktuelle Adresse gemerkt. `next`
  wird nur als interner Pfad angenommen.
- Öffentlich ohne Anmeldung bleiben: Doku, Aktivierung (LCM-Konto,
  Linux-Benutzer), Passwort zurücksetzen.

## Kommunikation: Service Worker und Ereigniskanal

### Aufgabenteilung

| Baustein | Aufgabe |
|---|---|
| **Service Worker** | Nur Cache: statische Dateien des aktuellen Builds zwischenspeichern, neue Version erkennen und sofort übernehmen. |
| **Tab** | Eigene WebSocket-Verbindung zum Ereigniskanal, eigener Datenspeicher im Arbeitsspeicher. |
| **BroadcastChannel** | Nur zwei Meldungen zwischen den Tabs: „abgemeldet“ und „neue Version“. |

Jeder Tab hat seine eigene Verbindung. Eine gemeinsame Verbindung für alle
Tabs (Shared Worker) bringt bei einer Handvoll Admin-Tabs kaum etwas, kostet
aber eine eigene Schicht mit Nachrichtenaustausch, eigenem Lebenszyklus,
Rückfallweg für Browser ohne Shared Worker und schwererer Fehlersuche. Die
Logik liegt im Backend, der Browser zeigt an.

### Ereigniskanal (WebSocket)

Die API bleibt **REST**. Der WebSocket meldet nur, was im Hintergrund
passiert:
- `server.busy`: Job gestartet, beendet, wartet („web03 ist belegt: Updates
  werden eingespielt“)
- `server.status`: Ampel geändert
- `job.progress`, `job.finished`
- `alert.fired`
- `bulk.progress`: Sammel-Update
- `system.build`: neue Version, Selbst-Update-Fortschritt

Ablauf:
- Je Tab eine Verbindung: `GET /api/v1/events`. Der Tab abonniert nur, was er
  gerade anzeigt (z. B. „Server 42“, „Jobs“).
- Ein Ereignis heißt „hat sich geändert“. Der Tab holt die Details per REST nach.
- Wiederverbinden mit Backoff. Danach werden sichtbare Daten einmal neu geladen
  (verpasste Ereignisse). Ist der WebSocket blockiert, wird langsam abgefragt.
- **Tabs im Hintergrund:** Ist ein Tab länger als 5 Minuten verborgen, schließt
  er seine Verbindung. Wird er wieder sichtbar, verbindet er neu und lädt die
  Ansicht frisch. So bleiben nur Verbindungen offen, die jemand ansieht.
- Backend: schlanker In-Process-Ereignisbus. Veröffentlicht wird an Jobende
  (`job_service.go` `OnFinished`), beim Verwerfen des Statuscaches
  (`server_status.go` `ForgetStatus`), beim Alarm-Versand und beim
  Selbst-Update. Wartende Jobs je Server werden neu ausgeliefert
  (`QueuedForServer` gibt es schon).
- Damit entfallen alle heutigen Abfragen im Takt: Build-Info (60 s),
  Selbst-Update (5 s), aktiver Job im Serverdetail (5 s), Jobverlauf (5 s),
  Sammel-Update (2,5 s).
- Terminal (eigener WebSocket mit Ticket) und Live-Log (SSE) bleiben, wie sie
  sind.

### Neue Version

- `index.html` und das Service-Worker-Skript werden nie zwischengespeichert.
- Der Service Worker speichert nur die **gehashten Dateien des aktuellen
  Builds**. Alte Builds werden beim Aktivieren gelöscht.
- Erkannt wird eine neue Version über das Ereignis `system.build` und beim
  Laden der Seite. Der neue Service Worker übernimmt sofort (`skipWaiting`,
  `clients.claim`) und meldet es per BroadcastChannel allen Tabs.
- Jeder Tab lädt automatisch neu, sobald nichts Ungespeichertes offen ist.
  Sonst erscheint ein Hinweis, und neu geladen wird nach dem Speichern. So
  spricht nie ein altes Frontend dauerhaft mit einem neuen Backend.

## Leistung

**Ziele** (im LAN, gemessen in der E2E-Suite):
- Erster Aufruf bis bedienbar: unter 1 s. Wiederholter Aufruf aus dem Cache:
  unter 300 ms.
- Seitenwechsel mit vorhandenen Daten: unter 100 ms.
- Start-JavaScript höchstens 150 KB (gzip), jede Seite und jeder Reiter als
  eigener, bei Bedarf geladener Teil. Nur die aktive Sprache wird geladen.
- Serverliste mit 1.000 Servern ohne Ruckeln (virtuelles Scrollen).
- Keine Abfragen im Takt.

**Mittel:**
- Bereits geladene Daten werden beim Seitenwechsel sofort aus dem
  Datenspeicher des Tabs gezeigt und im Hintergrund aufgefrischt. Gleiche
  gleichzeitige Anfragen innerhalb eines Tabs werden zusammengelegt.
- Listen-Endpunkte filtern, sortieren und blättern im Backend.
  `ETag`/`If-None-Match` für Listen: Unverändertes kostet nur ein `304`.
- Statische Dateien vorkomprimiert (Brotli und gzip) ins Binary eingebettet,
  gehasht, ein Jahr cachebar.
- Messung im CI: Bundle-Größe und Ladezeiten als Prüfschritt mit Grenzwerten.

## Sicherheit

- **Anmeldung im Browser, zwei einstellbare Wege** (Entscheidung 05.11.2026,
  Einstellung `browser_session_mode`, Einstellungen → Allgemein; die Frage kommt
  einmalig nach den Pflichtschritten). Kein JWT in `localStorage`, kein
  Bearer-Login mehr; Skripte nutzen `X-API-Key`, der MCP-Port bleibt wie er ist.
  - **Arbeitsspeicher (Standard):** keinerlei Cookies. Der Login liefert
    `session_token`, die Oberfläche hält ihn nur in einer Modulvariable und
    sendet ihn als `X-LCM-Session`. Gilt nur in diesem Tab; Neuladen, neuer Tab
    und Schließen melden ab (beim `pagehide` beendet ein `keepalive`-Logout die
    Sitzung auf dem Server). Kein CSRF nötig.
  - **Cookie (nur HTTPS):** `__Host-lcm_session` (`Secure`, `HttpOnly`,
    `SameSite=Strict`, `Path=/`, ohne Domain) plus `__Host-lcm_csrf`. Über HTTP
    nicht einstellbar (422); kommt bei dieser Einstellung ein Login über HTTP,
    läuft er als Arbeitsspeicher-Sitzung.
  - Eine Sitzung gilt nur über ihren Transport; ein Wechsel beendet alle
    Browser-Sitzungen und schließt die WebSockets.
- **CSRF-Schutz** (nur Cookie-Variante): `SameSite=Strict` plus CSRF-Token im
  Header bei allen ändernden Anfragen.
- **WebSocket:** Der Browser kann keine Kopfzeile setzen; der Ereigniskanal
  verbindet wie die Konsole mit einer Einmal-Fahrkarte (`POST /events/ticket`,
  30 s, an Benutzer und Sitzung gebunden, im Zugriffsprotokoll geschwärzt).
  Origin-Prüfung beim Verbindungsaufbau (Pflicht, sonst wäre
  Cross-Site-WebSocket-Hijacking möglich), die Sitzung wird weiter im Takt
  geprüft. Bei Abmeldung, Sitzungsablauf oder Rechteänderung schließt der Server
  die Verbindung. Der Tab baut sie mit den neuen Rechten neu auf. Höchstens 20
  Ereignis-Verbindungen je Sitzung.
- **Was in der Arbeitsspeicher-Variante nicht gehen darf:** Links auf
  geschützte Pfade (Downloads laufen per fetch und Blob), `EventSource` (das
  Live-Log läuft per fetch-Strom) und stilles `location.reload()` (Fehlerzustände
  laden Daten oder Module neu; nur ein Versionswechsel lädt die Seite neu).
- **Mandantentrennung:** Das Backend filtert jedes Ereignis nach Rechten und
  Gruppen des Benutzers. Ein Tab bekommt nur, was der Benutzer sehen darf.
- **Nichts Sensibles auf der Platte:** API-Daten liegen nur im Arbeitsspeicher
  des Tabs, nie in Cache Storage, IndexedDB oder `localStorage`. Dort stehen
  nur Oberflächen-Einstellungen (Theme, Sprache, Dichte). Der Service Worker
  speichert ausschließlich statische Build-Dateien.
- **Abmelden gilt überall:** Der abmeldende Tab meldet es per BroadcastChannel.
  Alle Tabs schließen ihren WebSocket, leeren ihren Speicher und gehen zur
  Anmeldung. Über den BroadcastChannel laufen keine Daten, nur diese Meldung
  und „neue Version“.
- **Content Security Policy streng:** `default-src 'self'`, kein Inline-Skript,
  kein `eval`, `connect-src 'self'`, `worker-src 'self'`,
  `frame-ancestors 'none'`, `object-src 'none'`, `base-uri 'none'`. Dazu
  Trusted Types. Lit maskiert Ausgaben selbst. Die Doku wird beim Erzeugen
  bereinigt.
- **Worker:** nur gleicher Ursprung, Service Worker mit Scope `/`, Skripte ohne
  Cache.
- Weitere Header: HSTS (bei TLS), `Referrer-Policy: no-referrer`,
  `X-Content-Type-Options: nosniff`, `Permissions-Policy`.
- Wenige Abhängigkeiten (Lit, xterm), feste Versionen, `npm audit` im CI.

## Technik

- Lit, TypeScript (wie DNS-Editor-CE), Vite, xterm. Versionen beim Anlegen aus
  der Registry (`npm view`).
- Eigene schlanke Bausteine: Router, i18n (siehe Mehrsprachigkeit), Theme,
  Rechte, API-Client mit Datenspeicher je Tab, Ereigniskanal-Client.
- Weiter **ein Binary**, das Frontend wird eingebettet.
- Muster aus ZPO und DNS-Editor-CE werden gesichtet und übernommen, wo sie
  passen.

## Vorgehen

1. Drei Designrichtungen, Entscheidung, Tokens und Komponentenkatalog.
2. Fundament: Hauptmenü, Routing mit Rückkehr nach Login, Browser-Sitzung
   (Arbeitsspeicher oder Cookie mit CSRF), i18n mit Fehlercodes im Backend, Hilfe-Bausteine und Glossar,
   Befehlsvorschau im Backend, Theme, Rechte, API-Client, Service Worker,
   Ereigniskanal (Backend und Client).
3. Seiten: Dashboard, Server mit Detail, Jobs, Gruppen, Sicherheit,
   Linux-Benutzer, Container, Einstellungen, Konto, Doku, öffentliche Abläufe.
4. Vollständigkeit gegen die API-Endpunkte prüfen: Jeder heute genutzte
   Endpunkt hat eine Heimat.
5. Playwright-E2E neu gegen die neue Oberfläche, die alten Tests als
   Funktions-Checkliste. axe-Prüfungen, Leistungsgrenzen und CSP-Verstöße als
   Testschritte.
6. Das neue Frontend entsteht in einem eigenen Verzeichnis gegen dieselbe API.
   Mit Release B wird das alte entfernt.
