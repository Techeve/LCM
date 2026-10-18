# Iteration 2026-10-17 – Arbeitsliste

Mehrere Features und Fehlerkorrekturen für eine gemeinsame Version. Jeder
Punkt: Befund (mit Fundstellen), Lösung, Prüfung. Reihenfolge der Umsetzung
unten. Commits je Punkt lokal, Push nach develop; MR erst auf Ansage.

## 1. Konsole: Beenden und Leerlauf trennen nicht, LCM sperrt sich aus (Fehler)

**Befund**
- `internal/api/controllers/terminal_controller.go` `pump`: Die Eingabe-Schleife
  endet bei „Beenden" (WebSocket zu) und beim Leerlauf-Timeout, wartet dann aber
  auf `<-done` der Ausgabe-Goroutine. Die hängt in `term.Read`, solange die
  Shell nichts ausgibt. `term.Close()` und `ws.Close()` stehen als `defer`
  dahinter und laufen nie.
- Folgen: SSH-Sitzung bleibt offen, kein Audit „terminal.close", kein
  Transkript, und der Ausführungs-Slot bleibt belegt.
- `OpenTerminal` holt die Verbindung über `connect` → `connLimit.AcquireExec`
  (ein Slot je Server, `conn_limiter.go`). Solange die Konsole (oder ihr Leck)
  besteht, scheitert jede Job-Verbindung zu dem Server nach 30 s mit
  `ErrTooManyConnections`.
- Der Leerlauf-Timeout (15 Min.) setzt nur die Lese-Deadline; er endet im
  selben Hänger.
- Ob jemand eine Konsole offen hat, ist nirgends sichtbar.

**Lösung**
- `pump`: Endet die Eingabe (Beenden, Timeout, Fehler), wird die Sitzung sofort
  geschlossen; das beendet `term.Read`, danach wird auf die Ausgabe gewartet.
  Endet die Ausgabe zuerst (Shell `exit`), wird der WebSocket geschlossen.
- Konsolen bekommen einen eigenen Verbindungs-Slot (nicht den Job-Slot), damit
  LCM während einer offenen Konsole weiterarbeitet. Begrenzung: wenige
  gleichzeitige Konsolen je Server.
- Register offener Konsolen (Server → Benutzer, seit) im TerminalService;
  Eintrag beim Öffnen, Austrag garantiert beim Schließen. Endpunkt
  `GET /servers/consoles` (servers:read).
- Anzeige: Kennzeichen „Konsole offen (Benutzer, seit …)" im Server-Detail und
  in der Server-Liste des Dashboards.
- Tests: `pump` schließt bei Ende der Eingabe und beim Leerlauf; Register trägt
  aus; Konsole belegt keinen Job-Slot.

## 2. Regel-Dialog zu schmal für Firewall-Regeln (Fehler)

**Befund**: Der Regel-Dialog in `Groups.svelte` nutzt die Standardbreite; der
`FirewallRulesEditor` braucht Platz (im Server-Detail läuft er schon in
`modal-xl`). `Modal.svelte` kennt `size`.

**Lösung**: Regel-Dialog `size="modal-xl"` beim Typ Firewall, sonst
`modal-lg`.

## 3. „Rechte-Soll halten" auch per Zeitplan (Feature)

**Befund**: `perm-sync` (und `acl-setup`) sind nur als Grundsatz-Regel erlaubt
(`ErrEnforceOnlyRuleType`, `enforce_priority.go: enforceOnlyRuleType`). Am
Zeitplan gäbe es keinen Ausführungspfad (`commandForRule` liefert "").

**Lösung**: Beide Typen auch am Zeitplan zulassen. Ausführung über denselben
Code wie die Grundsatz-Regel (`enforcePermSync` / `enforceACLSetup`), als Job
mit Protokoll. UI: Typen in der Zeitplan-Auswahl.

## 4. DNS-Test-Regel mit eigenen Domains (Feature)

**Befund**: `runDNSTestRule` nimmt immer die Domains aus den Einstellungen;
`Rule.Command` ist für `dns-test` verboten (`ruleTypeUsesCommand`).

**Lösung**: Optionale Domainliste in der Regel (`Command`, eine je Zeile bzw.
kommagetrennt, geprüft wie die Einstellungen über `sanitizeDNSDomains`). Leer =
Domains aus den Einstellungen; der Knopf am Server bleibt unverändert. UI:
Textfeld im Regel-Dialog, Anzeige in der Regel-Übersicht.

## 5. Mehrere APT-Caches, Auswahl in der Regel, Funktionstest (Feature)

**Befund**: Genau ein `AptCacheURL` in den Einstellungen. Regel `apt-proxy`,
Grundsatz-Drift, Server-Aktion, Übersicht, Alarm `apt_cacher_down` lesen alle
diese eine URL. Am Server gibt es nur `apt_proxy_active` (Drop-in vorhanden),
nicht welcher Cache. Der Einrichtungs-Lauf prüft `apt-get update` auf
`Err:`-Zeilen und rollt dann zurück.

**Lösung** (nach dem Muster der CrowdSec-LAPIs):
- Tabelle `apt_caches` (Name, URL); alte Einstellung einmalig als
  „Standard-Cache" übernehmen.
- Regel `apt-proxy`: Auswahl des Caches (`Command` = Cache-ID, wie bei
  Custom-Aktionen); leer = der einzige, wenn es genau einen gibt. Grundsatz-Drift
  vergleicht gegen die URL des gewählten Caches.
- Server-Aktion „APT-Cache anbinden" mit Cache-Auswahl.
- Server merkt sich die angebundene URL (`apt_proxy_url`, beim Scan aus dem
  Drop-in gelesen); Anzeige des Cache-Namens.
- Einstellungen → APT-Cache: Liste mit Anlegen/Bearbeiten/Entfernen und
  Erreichbarkeits-Check je Cache; Statistik und Verwaltung des eigenen
  apt-cacher-ng bleiben für den Cache auf dem LCM-Host.
- Alarm `apt_cacher_down` prüft jeden Cache.
- Funktionstest nach dem Setzen: Der Lauf meldet ausdrücklich das Ergebnis –
  `apt-get update` über den Cache ohne Fehler UND mindestens eine Quelle
  tatsächlich geladen (Get/Hit-Zeilen), sonst Rückbau wie bisher.

## 6. Gruppen-Seite: Regeln nach Auslöser, Reihenfolge, Klappen (Feature)

**Befund**
- `Rule` hat kein Reihenfolge-Feld; ausgeführt wird in ID-Reihenfolge
  (`Preload("Rules", Order("rules.id"))`, `RunSchedule`).
- Die Seite zeigt Zeitpläne und Regeln als zwei getrennte Tabellen; die
  Zuordnung steht nur in der Spalte „Ziel".
- Server und Verwaltungs-User sind immer ausgeklappt.

**Lösung**
- Backend: `Rule.Position`; Laden und Ausführen nach `position, id`; neue
  Regeln ans Ende ihres Auslösers; Endpunkt
  `PUT /server-groups/:id/rules/order {rule_ids}` setzt die Reihenfolge
  (alle IDs müssen zur Gruppe gehören). Gilt für Zeitplan- und
  Grundsatz-Regeln.
- UI: Ein Abschnitt „Regeln": je Auslöser eine Karte (Zeitplan mit Cron,
  Fenster, An/Aus, Jetzt, Bearbeiten, Löschen; dazu die Karte
  „Grundsatz-Regeln"). Darin die Regeln von oben nach unten in
  Ausführungsreihenfolge, mit Nummer, per Ziehen (Drag & Drop) und per
  Pfeil-Knöpfen verschiebbar. „+ Regel" an jeder Karte (Auslöser ist damit
  gesetzt), „+ Zeitplan" oben.
- Server und Verwaltungs-User als einklappbare Abschnitte, standardmäßig zu,
  mit Anzahl im Titel („Server (5)", „Verwaltungs-User (6)").
- E2E-Tests der Gruppen-Seite anpassen.

## Reihenfolge der Umsetzung

1. Konsole (Fehler mit Betriebsfolgen)
2. Regel-Dialog Firewall breiter
3. Rechte-Soll am Zeitplan
4. DNS-Test-Domains in der Regel
5. Mehrere APT-Caches + Funktionstest
6. Gruppen-Seite (Reihenfolge, Gruppierung, Klappen)

## Protokoll

| Punkt | Stand | Commit |
|---|---|---|
| 1 Konsole | erledigt | a3e989c |
| 2 Dialogbreite | erledigt | 19d726f |
| 3 Rechte-Soll | erledigt | 23e6edb, cd615b6 |
| 4 DNS-Test | erledigt | 1184db8 |
| 5 APT-Caches | erledigt | 4db5b56 |
| 6 Gruppen-Seite | erledigt | 5df6fc8, d680d8b |

Alle Punkte lokal geprüft (gofmt, vet, go test, npm audit, Build, 164/164
E2E, govulncheck) und nach develop gepusht.
