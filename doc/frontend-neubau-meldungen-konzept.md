# Release B: Meldungen einheitlich – Konzept

Stand: 04.11.2026 · **Entscheidung: Ansatz B (Tony, 04.11.2026)** · umgesetzt

Die Regel in einem Satz: Kurzes schwebt unten rechts (Toast), Behebbares
steht am Auslöser (Feld, Speicherleiste, Fensterfuß), Dauerhaftes hängt am
Objekt (Band).

Umgesetzt (`ui/src`):

- `components/lcm-notice.ts`: eine Komponente für alle festen Meldungen
  (`tone` ok/info/warn/crit, `variant` line/band/bar, Titel, ein Satz Text,
  höchstens eine Folgeaktion, optional schließbar; Ampel in Farbe und Form,
  3-px-Tonstreifen; `crit` als `role="alert"`). Ersetzt die eigenen Stile
  `.notice`, `.note`, `.warn`, `.banner`, `.warnbox`, `.error`, `.success`.
  Gedämpfte Hinweistexte (graue `.note`-Zeilen ohne Fläche) bleiben Text.
- `components/lcm-toaster.ts`: Tonstreifen, Erfolg 5 s, Hintergrund
  (`background: true`) und Warnung 8 s, Fehler bleibt; höchstens drei
  sichtbar, darüber „+n weitere“; Hover/Fokus hält an; bei offenem Fenster
  (`components/modal-watch.ts`) warten neue Toasts mit Hinweis „1 Meldung
  wartet“, kritische erscheinen sofort; schmal unten über volle Breite.
- `components/lcm-save-bar.ts`: Speicherleiste aus `SettingsForm` gelöst,
  nennt bei Feldfehlern das Feld und springt mit „Zum Feld“ hin. Genutzt in
  den Einstellungsseiten, Abo (Fehler oben entfernt), Konto (Profil,
  Passwort). Texte in `lib/save-bar.ts`.
- Fenster: `lcm-modal` hat den Slot `note` über dem Fuß, `lcm-dialog` die
  Eigenschaft `error`. Scheitert eine bestätigte Aktion, bleibt der Dialog
  offen und zeigt den Fehler dort statt als Toast.
- Serverdetailseite: offline (mit letztem Fehler), handlungsunfähig, Wartung
  und belegt als Band unter dem Kopf; nicht wegklickbar.
- Systemhinweise unter dem Menü (Pflichtschritt, neue Version, Update-Band)
  als `lcm-notice variant="bar"`; Info wegklickbar; Update-Fehler nur noch
  im Band bzw. im Bestätigungsdialog, nicht zusätzlich als Toast.
- Jobende (Abo-Kanal, Frühwarnung spiegeln) als Toast mit „Job ansehen“
  (`lib/job-link.ts`).
- Katalog `/catalog.html?s=messages` zeigt die echten Bausteine; die Demos
  der Ansätze A bis C sind entfallen.

Die Abschnitte unten sind die Bestandsaufnahme und der Vergleich, die zur
Entscheidung geführt haben; Zeilennummern beziehen sich auf den Stand davor.

## Ausgangslage

Wie die neue Oberfläche heute rückmeldet (`ui/src`):

| Art | Wo | Wofür |
|---|---|---|
| Toast unten rechts | `components/lcm-toaster.ts` (Stapel `:197`, Dauer `:153`: 6 s, `crit` bleibt), eingebunden in `shell/lcm-app.ts:368` | 183 Aufrufe in 65 Dateien: 91 Erfolg, 69 Fehler, 9 Info, 4 Warnung. Ergebnis fast jeder Aktion, auch Fehler beim Speichern in Listen-Formularen (`settings/alerts.ts:190`, `notifications.ts:99/115`, `repositories.ts:93`) und Jobende (`settings/security.ts:103-109`) |
| Speicherleiste | `settings/system/settings-form.ts:278-289`, klebt unten (`settings-styles.ts:111`) | Ungespeichert und Speicherfehler, nur in den 6 Seiten auf Basis von `SettingsForm` (Allgemein, Sicherheit, MCP, Backups, Zeit, DNS) |
| Fehler oben auf der Seite | `settings/subscription.ts:385`, `docs/docs-page.ts:294`, `servers/srv-terminal.ts:204` | Abo: Fehler beim Aktivieren steht über allen Karten, der Knopf liegt weiter unten. Genau das Problem aus der Anfrage |
| Fehler über dem Fensterfuß | `groups/form-modal.ts:144`, `linux-users/form-modal.ts:157`, `settings/access/form-modal.ts:138`, `settings/packages/settings-modal.ts:157`, `servers/srv-settings.ts:264-265`, Dialoge `settings/users.ts:400`, `backups.ts:318`, `access/key-list.ts:300` | Speicherfehler im Fenster, mal `.error`, mal `.note.crit` |
| Fehler in Formularen ohne Fenster | `account/account-*.ts` (`:150`, `:184`, `:77`, `:217`), `public/activate-page.ts:121`, `login-page.ts:315`, `servers/join/*` | `.error` unter dem Formular, Platz je nach Seite |
| Feldfehler | `components/lcm-input.ts:99`, `linux-users/textarea.ts:102`, `groups/groups-textarea.ts:98`, `settings/users.ts:319` | `error` am Feld, in 28 Seitendateien genutzt; einheitlich |
| Hinweisblöcke | `.notice` (`servers/sec-styles.ts`, `ops-styles.ts`, `ov-lcm-host.ts`, `tab-deep-scan.ts`, `srv-terminal.ts`), `.note` (`settings-styles.ts:38`, `account/form-styles.ts`, `lu-styles.ts`, `set-styles.ts`, `jobs-detail.ts`), `.warn` (`join-parts.ts:183`, `srv-more.ts:224`, `overview-tab.ts:253`), `.banner` (`security/sec-advisories.ts:242`) | Über 20 eigene Stildefinitionen derselben Idee, teils mit `role="alert"` für Dauerhinweise |
| Belegt-Band | `servers/server-detail.ts:449-460`, gezeigt unter dem Kopf (`:581`) | Server arbeitet gerade einen Job ab, mit `lcm-job-progress` und Abbrechen |
| Zustand als Marke | `servers/server-detail.ts:355-357` | offline, handlungsunfähig, Wartung: nur `lcm-badge`, Grund im `title` |
| Systemhinweise | `shell/lcm-app.ts:284-304` (Passwort/2FA nötig, neue Version wartet, lädt neu), `shell/update-banner.ts:131/138/151` | Volle Breite unter dem Menü; Update-Fehler zusätzlich als Toast (`update-banner.ts:115-117`) |
| Leer- und Fehlerzustand | `lcm-empty-state` (72 Stellen), z. B. `settings-form.ts:295`, `dashboard-page.ts:97` | Laden fehlgeschlagen, mit „Erneut versuchen“ |
| Bestätigung | `lcm-dialog` (52 Stellen) mit `impacts`, zerstörend mit getipptem Namen | Vor Aktionen; danach Toast |
| Jobfortschritt | `lcm-job-progress` in `jobs/jobs-active.ts:64`, `server-detail.ts:455` | Laufender Job am Ort; Ende kommt als Toast oder gar nicht |

Befund: Die Bausteine sind gut, die **Regel fehlt**. Dieselbe Sorte Meldung
landet je nach Seite oben, unten, im Fenster oder als Toast.

## Kategorien

1. **Ergebnis einer Aktion** – „Paketlisten neu geladen“, „konnte nicht laden“.
2. **Eingabefehler** – ein Feld ist ungültig (im Browser oder vom Backend einem Feld zugeordnet).
3. **Fehler beim Speichern** – das Backend lehnt ab, ohne dass ein Feld schuld ist.
4. **Hintergrundereignis** – Job fertig, Alarm, Server wieder da. Kam nicht vom eigenen Klick.
5. **Zustand eines Objekts** – offline, Wartung, belegt, handlungsunfähig. Dauert an.
6. **Systemhinweis** – Verbindung verloren, Update verfügbar, Passwort ändern.

Für alle gilt **eine Anatomie**: Ampel (Farbe *und* Form), Titel, ein Satz Text,
höchstens eine Folgeaktion als Link, bei Bedarf Schließen. Töne: ok, info,
warn, crit. Nur Ort, Größe und Lebensdauer wechseln. Ein farbiger Streifen
links (3 px) macht jede Meldung auch in Dunkel sofort sichtbar.

## Ansatz A · Alles am Ort

Nichts schwebt. Jede Meldung steht dort, wo sie entsteht.

| Kategorie | Ort | Dauer |
|---|---|---|
| Ergebnis | Erfolg als Zeile neben dem Knopf, Fehler als Block unter der Aktionsleiste | Erfolg 6 s, dann grau; Fehler bis zum nächsten Versuch |
| Eingabefehler | am Feld; die Seite springt zum ersten Fehler | bis korrigiert |
| Speicherfehler | Speicherleiste unten | bis zum nächsten Versuch |
| Hintergrund | Glocke im Hauptmenü mit Zähler (rot bei kritisch), Liste beim Öffnen | bis gelesen |
| Zustand | ruhige Zustandszeile in Karte und Kopf | solange der Zustand dauert |
| System | Band unter dem Menü | solange der Zustand dauert |

- **Fenster offen:** Meldungen zum Fenster stehen im Fenster über dem Fuß. Hintergrund zählt nur die Glocke hoch.
- **Schmal:** Zeile neben dem Knopf rutscht darunter; die Glocke bleibt im Menü.
- **Mehrere:** jede an ihrem Ort, die Glocke sammelt.
- **Barrierefreiheit:** Fehler `role="alert"`, Erfolg `role="status"`; Sprung zum Feld setzt den Fokus hinein. Glocke als Knopf mit Zähler im Namen.
- **Pro:** nichts verdeckt Inhalt; Ursache und Meldung sind nie getrennt.
- **Contra:** Erfolg ist leicht zu übersehen; Hintergrundereignisse fallen kaum auf (kritischer Alarm nur als roter Zähler); jede Seite braucht Platz für die Zeile; der Sprung im Formular reißt aus dem Lesefluss; Glocke braucht Speicher und „gelesen“-Logik (Backend).

## Ansatz B · Drei Orte nach Lebensdauer (Empfehlung)

Kurzes schwebt unten rechts, Behebbares steht am Auslöser, Dauerhaftes hängt
am Objekt. Wer die Lebensdauer kennt, kennt den Ort.

| Kategorie | Ort | Dauer |
|---|---|---|
| Ergebnis | Toast unten rechts | Erfolg 5 s, Fehler bleibt bis geschlossen |
| Eingabefehler | am Feld **und** in der Speicherleiste mit „Zum Feld“ | bis korrigiert |
| Speicherfehler | Speicherleiste unten neben dem Knopf, nie als Toast, nie oben | bis zum nächsten Versuch |
| Hintergrund | Toast mit Link zum Objekt | 8 s, kritisch bleibt |
| Zustand | Zustandsband am Objekt (Karte, Kopf der Detailseite) | solange der Zustand dauert, nicht wegklickbar |
| System | Band unter dem Menü | solange der Zustand dauert; Info wegklickbar |

- **Fenster offen:** Fehler des Fensters stehen im Fenster über dem Fuß (gleiche Anatomie). Neue Toasts warten, bis das Fenster zu ist; ein kleiner Hinweis „1 Meldung wartet“ zeigt das. Kritische erscheinen sofort, ohne Fokus zu nehmen.
- **Schmal (< 40rem):** Toasts unten über volle Breite; Bänder brechen um, die Aktion rutscht unter den Text.
- **Mehrere:** Stapel, neueste unten, höchstens drei, darüber „+n weitere“. Hover oder Fokus hält an.
- **Barrierefreiheit:** Stapel `aria-live="polite"`, kritisch `role="alert"`; Toasts nehmen nie den Fokus. Speicherleiste `role="region"` mit Namen; Fehlertext `role="alert"`. „Zum Feld“ scrollt und setzt den Fokus.
- **Pro:** nah am heutigen Stand (Toaster, Speicherleiste, Belegt-Band und Update-Band gibt es schon), also wenig Umbau; jede Kategorie hat genau einen Ort; Erfolg sichtbar, aber flüchtig; Formularfehler stehen dort, wo man gerade klickt.
- **Contra:** Toasts können Inhalt unten rechts kurz verdecken; drei Orte muss man einmal lernen (die Regel steht aber in einem Satz).

## Ansatz C · Statusleiste

Eine Leiste am unteren Rand für alles, was passiert, mit Verlauf. Sie ist
zugleich die Speicherleiste.

| Kategorie | Ort | Dauer |
|---|---|---|
| Ergebnis | Mitte der Statusleiste | Erfolg verblasst nach 5 s, bleibt lesbar |
| Eingabefehler | am Feld und in der Statusleiste mit „Zum Feld“ | bis korrigiert |
| Speicherfehler | Statusleiste (= Speicherleiste); der Speicherzustand hat dort Vorrang | bis zum nächsten Versuch |
| Hintergrund | Statusleiste, Zähler, Verlauf der Sitzung | im Verlauf |
| Zustand | Zustandsband am Objekt (wie B) | solange der Zustand dauert |
| System | linker Teil der Statusleiste als Marke | solange der Zustand dauert |

- **Fenster offen:** Leiste liegt unter dem Abdunkler und zählt weiter; Fehler des Fensters im Fenster.
- **Schmal:** nur Titel und Knöpfe, Text und Systemtexte entfallen; kollidiert mit der Bildschirmtastatur.
- **Mehrere:** immer nur die neueste sichtbar, Rest im Verlauf. Eine kritische kann von einem späteren Erfolg verdrängt werden, wenn man nicht eine Vorrangregel einbaut.
- **Barrierefreiheit:** eine `aria-live`-Region, leicht zu machen; Verlauf als Knopf mit Zähler.
- **Pro:** immer derselbe Ort, nichts verdeckt; Verlauf zum Nachlesen; Formular und Meldungen teilen eine Leiste.
- **Contra:** der untere Rand wird am wenigsten beachtet; kostet dauerhaft eine Zeile Höhe; ungewohnt für eine Web-Oberfläche; nur eine Meldung sichtbar.

## Der Fall „Speichern in langen Formularen“

Problem: Fehler erscheint oben, man steht unten am Speichern-Knopf.

| Option | Bewertung |
|---|---|
| Fehler direkt neben dem Knopf | gut, aber nur, wenn der Knopf sichtbar ist; bei langen Seiten scrollt er weg |
| **Klebende Speicherleiste** (unten, erscheint bei Änderungen) | Knopf und Fehler sind immer sichtbar, egal wo man steht. Gibt es schon in `SettingsForm` |
| Automatisch zum ersten ungültigen Feld scrollen | eindeutig, reißt aber aus dem Kontext; bei Serverfehlern ohne Feld wirkungslos |
| Toast mit Link zum Feld | verschwindet, trennt Fehler und Ursache, verdoppelt die Meldung |

Empfehlung: **Speicherleiste** für jede Seite mit Speichern (auch Abo,
Konto, Fenster mit langen Reitern). Bei Feldfehlern nennt sie das Feld und
bietet „Zum Feld“; das Feld selbst trägt den Fehler. Kein Toast, kein
Fehler oben. In Fenstern übernimmt der Fensterfuß diese Rolle (Fehler direkt
über den Knöpfen). Erfolg danach als kurzer Toast (B).

## Empfehlung

**Ansatz B**, weil er die vorhandenen Bausteine nur ordnet statt neu zu
erfinden und Fehler immer dort zeigt, wo man gerade hinschaut. Aus A
übernehmen: die ruhige Zustandszeile für sehr kleine Karten. Aus C optional
später: ein Verlauf der Sitzung hinter einem Knopf im Menü, falls Toasts
verpasst werden.

Umsetzung in kleinen Schritten, wenn Tony zustimmt:

1. `lcm-notice` (neu): eine Komponente für alle festen Meldungen mit `tone`,
   `variant` (`band`, `line`, `bar`) und Folgeaktion; ersetzt die über 20
   eigenen `.notice`/`.note`/`.error`/`.warn`-Stile.
2. `lcm-toaster`: Tonstreifen, „+n weitere“, Zurückhalten bei offenem Fenster.
3. Speicherleiste als eigene Komponente aus `SettingsForm` lösen
   (`lcm-save-bar`) und in Abo, Konto und Formularen ohne `SettingsForm`
   nutzen; Fehler oben (`subscription.ts:385`) entfernen.
4. Fensterfehler einheitlich über dem Fuß (`lcm-modal` Slot `note`).
5. Zustände am Server (offline, Wartung, handlungsunfähig) als Band im Kopf
   statt nur als Marke; Belegt-Band in dieselbe Form bringen.
