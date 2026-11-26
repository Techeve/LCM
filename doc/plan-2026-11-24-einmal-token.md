# Plan: Echtes Einmal-Token für die Einrichtung des Agenten (S-05)

Stand 24.11.2026. Status je Paket: offen · in Arbeit · zu prüfen · fertig.
Gehört zu Paket G von `doc/plan-2026-11-23-agent-robustheit.md`; Befund S-05 in
`doc/sicherheit-agent-2026-11.md`. Tony hat den Weg freigegeben (Einmal-Token, gültig bis zur
ersten erfolgreichen Anmeldung, höchstens 24 Stunden, Tausch gegen ein Betriebs-Secret, Übergang
für Agenten 2.0.0 bis 2.4).

## Ziel

1. Das Token, das ein Mensch sieht, abtippt oder in einen Befehl einsetzt, ist **nur eine Tür**:
   einmal benutzbar, höchstens 24 Stunden gültig, danach wertlos.
2. Das Secret, mit dem sich der Agent dauerhaft am Broker anmeldet (**Betriebs-Secret**), erzeugt
   der Agent selbst. Es steht nur in `agent.json` (0600) und als SHA-256 in der LCM-Datenbank. Es
   erscheint nie auf einer Kommandozeile, in einer Oberfläche oder in einem Protokoll.
3. Das Token lässt sich über Datei oder stdin übergeben, nicht nur als Argument.
4. Agenten 2.0.0 bis 2.4 laufen unverändert weiter und tauschen nach dem Update selbst. Danach ist
   ihr altes Dauer-Secret ungültig.
5. Ein Abbruch an irgendeiner Stelle des Tauschs sperrt den Agenten nie aus.

## Ausgangslage (Code, Stand develop + feat/agent-haertung)

| Ort | Heute |
|---|---|
| `internal/core/services/agent_service.go` `CreateAgentServer`, `RegenerateAgentToken` | erzeugt `config.RandomSecret(32)`, speichert `HashAgentSecret` in `Server.AgentTokenHash` und die ersten 12 Zeichen in `AgentTokenPrefix`; das Token (`agentToken`) trägt dasselbe Secret plus Pins. Kein Ablauf, kein Zähler. |
| `internal/remote/wire/wire.go` `Token`, `EncodeToken`, `ParseToken`, `HashSecret` | Format `lcma1.` + base64url(JSON `u`,`k`,`f`,`c`,`h`). Das Token **ist** das Secret. |
| `internal/remote/hub.go` `OnConnectAuthenticate` | Benutzername = Client-ID = Agent-ID, Passwort = Secret. Vergleich `ConstantTimeCompare(HashSecret(pw), AgentTokenHash)`. Leerer Hash lehnt ab. Keine Zählung von Fehlversuchen (S-06). |
| `internal/agent/enroll.go` `Enroll`, `configFromToken` | Token kommt als `os.Args[3]` (`cmd/agent/main.go:45`), steht in `ps`, Shell-Verlauf, sudo-Protokoll. Verbindungstest, dann `cfg.Save`. |
| `internal/agent/config.go` `Config`, `LoadConfig`, `Save` | `secret` Pflichtfeld; `Save` schreibt atomar (`writeFileDurable`, S-08, auf dem Branch). |
| `internal/agent/client.go` `clientOptions` | `SetUsername(AgentID)`, `SetPassword(cfg.Secret)`; TLS-Prüfung `verifyPeer` (Pins ODER Systemroots), lernt Pins. |
| `internal/agent/health.go` `agentCapabilities` | Fähigkeitsliste im Inventar; `internal/core/agentcaps` wertet sie aus. |
| `internal/api/router/router.go` `NewAgentGateway` | Agent-Port bietet **nur** `GET /mqtt`, davor `IPAllowlist` und Ratelimit im WS-Handler. |
| Anzeige des Befehls | `ui/src/pages/servers/join/join-model.ts` `agentCommands` (`sudo lcm-agent enroll <url> <token>`), `join-agent.ts`, `srv-more.ts` (Token erneuern), `ui/src/locales/{de,en}.json` (`agent.once`, `agentToken.command`). |
| weitere Stellen mit dem Befehl | `packaging/scripts/agent-postinstall.sh`, `cmd/agent/main.go` (usage), `internal/appdocs/pages/{de,en}/30-agent.md`, `docs/guides/{remote,zertifikate,adressen}.md` (+`docs/en`), `tools/distro-matrix.sh:146-151`, `ui/e2e/doku.spec.ts`. |

## Design

### 1. Lebenslauf des Einmal-Tokens

- **Erzeugen** (Server anlegen, „Agent-Token erneuern“): 32 Zufallsbyte; gespeichert wird nur der
  Hash (`agent_onetime_hash`) und das Ablaufdatum (`agent_onetime_expires_at` = jetzt + 24 h).
  Ein neues Token macht jedes frühere ungültig.
- **Anzeigen**: einmal, mit Ablaufzeit. Format `lcma2.` + base64url(JSON), dieselben Felder wie
  `lcma1.` (Agent-ID, Secret, Pins `f`,`c`,`h`) plus `x` (Ablauf, Unix-Sekunden, nur für die
  Meldung im Agent; entschieden wird allein auf dem Server). Ein Agent vor 2.5 weist `lcma2.` mit
  „Präfix lcma1. fehlt“ ab; das ist gewollt, er kann es nicht einlösen.
- **Einlösen** in zwei Schritten (Abschnitt 2): Vorschlag des Betriebs-Secrets, dann erste
  erfolgreiche MQTT-Anmeldung damit. Erst diese zweite Stufe **verbraucht** das Token
  (`agent_onetime_hash` und Ablauf werden geleert).
- **Ablauf**: nach 24 Stunden lehnt der Server den Vorschlag mit 410 ab (nur wenn der Hash passt,
  sonst 401, damit niemand Tokens abtasten kann). Ein abgelaufenes Token wird bei der nächsten
  Wartung verworfen und protokolliert.
- Bis zum Verbrauch ist das Token ein Inhaber-Token: Wer es vor dem Agenten einlöst, gewinnt. Das
  Fenster ist kurz (Rest des Befehls, höchstens 24 h), der Gewinner ist im Audit mit IP sichtbar,
  der echte Agent bekommt 409 und meldet es laut (Abschnitt 5).

### 2. Tausch: HTTPS-Endpunkt auf dem Agent-Port (Wahl)

Der Agent erzeugt das Betriebs-Secret `S` (32 Byte aus `crypto/rand`, base64url) und schickt dem
Server nur dessen Hash:

```
POST /api/v1/agent/enroll          (Agent-Port, derselbe TLS-Kanal mit denselben Pins)
{"agent_id": "...", "token": "<k aus dem Token>" | "secret": "<altes Dauer-Secret>",
 "next_hash": "<SHA-256(S)>"}
200 {"state": "pending"|"active", "ttl_sec": 3600}
401 unbekannt/falsch · 409 anderer Vorschlag schon hinterlegt · 410 Token abgelaufen · 429 zu viele
```

Der Server prüft die Berechtigung (Token-Hash oder, für den Übergang, das aktuelle Secret) und
legt `next_hash` als `agent_pending_hash` ab (Gültigkeit 1 Stunde). **Das Token bleibt gültig.**
Dann verbindet sich der Agent per MQTT mit `S` als Passwort. Der Hook erkennt den ausstehenden
Hash, macht ihn in einem Schritt zum aktiven (`UPDATE … WHERE agent_pending_hash = ?`), verbraucht
das Token und lässt die Anmeldung zu. **Die erste erfolgreiche Anmeldung mit `S` ist die
Bestätigung des Servers.** Erst danach streicht der Agent seinen Zwischenstand.

Begründung gegen die Alternative (neue MQTT-Nachricht nach dem ersten CONNECT):
- Anfrage und Antwort sind eindeutig und idempotent; ein MQTT-Austausch bräuchte neue Topics,
  ACL-Ausnahmen für eine Halb-Sitzung („angemeldet, aber nur zum Tauschen“) und Zuordnung der
  Antwort bei QoS 1 und CleanSession.
- Das Einmal-Token berührt den Broker-Hook nie. Der Hook kennt weiter nur „Hash passt oder
  nicht“ (aktiv, ausstehend), die Angriffsfläche vor der Anmeldung bleibt klein.
- Alte Agenten und ältere LCM-Fassungen merken nichts: neue Route, keine neue MQTT-Nachricht.
- Kosten: Der Agent-Port bietet nicht mehr ausschließlich `/mqtt`, und ein Reverse Proxy muss den
  Pfad `/api/v1/agent/enroll` mit durchlassen (Risiko R-1). Der Endpunkt bekommt ein Limit je IP
  und je Agent-ID, Body höchstens 4 KiB, keine Protokollierung des Bodys.

### 3. Zustände am Server

Neue Spalten an `servers` (alle `json:"-"` außer den abgeleiteten Angaben, nicht im MCP-Sichtmodell
`buildServerView`): `agent_onetime_hash`, `agent_onetime_expires_at`, `agent_pending_hash`,
`agent_pending_expires_at`, `agent_secret_exchanged_at`. `agent_token_hash` bleibt die Spalte des
gültigen Secrets (Dauer-Secret oder Betriebs-Secret). Daraus folgt:

- Neuer Server mit `lcma2.`: `agent_token_hash` leer, der Hook lehnt jede Anmeldung ab, bis der
  Tausch stattgefunden hat.
- Bestehender Server: `agent_secret_exchanged_at` leer = Dauer-Secret (Zustand „legacy“).
- **Herabstufen von LCM bleibt sicher**: Ein älteres LCM liest nur `agent_token_hash`; das ist
  nach dem Tausch der Hash des Betriebs-Secrets, die Anmeldung geht weiter.
- Abgeleiteter Zustand (`agent_secret_state`): `onetime-open` (mit Ablauf), `onetime-expired`,
  `legacy`, `operating`.
- Logik in einem **eigenen Paket `internal/core/agentenroll`** (ohne GORM, gegen ein kleines
  `Store`-Interface): `Issue`, `Propose`, `Promote`, `State`, Fristen, Fehlerarten. Hub, Service
  und API nutzen es; das Agent-Binary importiert es nicht (nur `wire`).

### 4. Zustände am Agent (`agent.json`)

Neue Felder: `next_secret` (Vorschlag `S`, solange der Server ihn noch nicht bestätigt hat) und
`exchanged` (wahr, sobald `secret` ein Betriebs-Secret ist). `secret` bleibt das zuletzt
bewährte Secret. Das Token selbst steht **nie** in der Datei.

Reihenfolge bei jedem Verbindungsaufbau, wenn `next_secret` gesetzt ist:
1. `POST /enroll` mit `next_hash` (Fehler sind nicht tödlich, 404 = Server kennt den Tausch nicht).
2. MQTT-CONNECT mit `next_secret`. Bei Erfolg: `secret = next_secret`, `next_secret` löschen,
   atomar speichern (`writeFileDurable`).
3. Wird `next_secret` abgewiesen und es gibt ein `secret`, mit diesem verbinden (Übergang).
   Ohne `secret` (Neueinrichtung) abwarten und neu vorschlagen.

Nach dem Tausch gilt `secret` wie bisher als Passwort; ein Rückfall auf ein älteres Agent-Binary
(D-4) liest dieselbe Datei und meldet sich mit `S` an.

### 5. Abbruch mitten im Tausch

| Abbruch | Zustand danach | Ausgang |
|---|---|---|
| vor dem Speichern von `next_secret` | nichts geändert | Neustart, alles von vorn |
| nach dem Speichern, vor `POST` | Agent kennt `S`, Server nicht | nächster Start schlägt `S` vor; altes `secret` (Übergang) trägt bis dahin |
| `POST` gesendet, Antwort verloren | Server hat `pending` | Wiederholung ist idempotent (gleicher Hash = 200) |
| `pending` abgelaufen (1 h) | Token noch gültig | erneuter Vorschlag; Token läuft erst nach 24 h ab |
| Server hat befördert, CONNACK verloren | `S` aktiv | nächster Versuch: `POST` meldet `active`, CONNECT mit `S` gelingt |
| befördert, Speichern von `secret` scheitert | `next_secret` liegt noch in der Datei | nächster Start: CONNECT mit `next_secret` gelingt, erneut speichern |
| ein Fremder löst das Token zuerst ein | `pending` mit fremdem Hash | der Agent bekommt 409, `enroll` bricht laut ab; Audit zeigt IP; Token neu erzeugen sperrt den Zugang |
| Datenbank-Rücksicherung älter als der Tausch | Server kennt `S` nicht | Agent wird abgewiesen; neues Token erzeugen (dokumentiert) |

Regel dahinter: Das alte Secret wird erst ungültig, wenn der Server die Anmeldung mit dem neuen
selbst gesehen hat; der Agent löscht das alte erst nach demselben Ereignis.

### 6. Widerruf, Rotation, Neuanmeldung

- „Agent-Token erneuern“ behält die heutige Bedeutung: Der aktive Zugang wird **sofort** ungültig,
  die Sitzung getrennt, ein neues Einmal-Token entsteht. Gilt für Verlust, Neuinstallation und
  Verdacht.
- Rotation eines gesunden Agenten ist derselbe Weg mit dem aktuellen Secret als Berechtigung
  (`"secret"` statt `"token"`). Der Agent kann ihn für `lcm-agent rotate-secret` nutzen; ein
  automatischer Takt ist nicht Teil dieses Plans (offene Frage 6).
- Neuer `enroll` auf einem Server mit vorhandener Konfiguration derselben Agent-ID verwendet das
  vorhandene `next_secret` weiter (Wiederholung nach Abbruch ohne neues Token, solange der Server
  nicht befördert hat; hat er befördert, genügt der CONNECT).

### 7. Fähigkeit und alte Agenten

- `wire.CapabilitySecretExchange = "secret-exchange"`; `agentcaps.FeatureSecretExchange`,
  `sinceSecretExchange = "2.5.0"` (beim Release prüfen). Das Netzformat bleibt additiv,
  `ProtocolVersion` bleibt 2. Optional meldet der Agent `Inventory.SecretState`
  (`operating`, `legacy`, `exchange-failed`), damit die Oberfläche ein Scheitern des Tauschs
  (Proxy blockt den Pfad) von „Agent noch alt“ unterscheiden kann.
- Alte Agenten (2.0.0 bis 2.4): ändern nichts; ihr Dauer-Secret funktioniert, bis sie tauschen.
  Neue Server geben für sie ein `lcma1.`-Token aus, nur auf ausdrücklichen Wunsch (Parameter
  `legacy`, Oberfläche: „Mein Agent ist älter als 2.5“).
- Neuer Agent an altem LCM: `POST` liefert 404, der Agent bleibt beim Dauer-Secret und versucht
  es beim nächsten Verbindungsaufbau wieder (höchstens einmal je Stunde).

### 8. Übergang bestehender Agenten

1. LCM mit dem neuen Hub ausrollen; Spalten kommen über das Migrationsverfahren, alle Server
   stehen auf `legacy`.
2. Agent-Update (D-2) auf ≥ 2.5. Beim ersten Start bemerkt der Agent „`secret` gesetzt, aber nie
   getauscht“ (Merkmal: kein `exchanged: true` in `agent.json`) und schlägt `S` mit dem
   Dauer-Secret als Berechtigung vor.
3. Erste Anmeldung mit `S`: Server befördert, **das alte Secret ist ungültig**, die Sitzung eines
   etwaigen Angreifers mit dem alten Secret endet durch die Übernahme der Client-ID.
4. Die Oberfläche zeigt bis dahin ein Befund-Hinweis im Reiter Sicherheit („Dauer-Secret, Agent
   aktualisieren, dann Einmal-Token“), keine zweite Statuspille. Agenten ohne Fähigkeit bekommen
   den Hinweis „Braucht Agent ≥ 2.5, jetzt aktualisieren“ aus `agentcaps`.

### 9. Token über Datei und stdin

```
sudo lcm-agent enroll <url> -                    # liest das Token von stdin (Eingabe verdeckt, wenn Terminal)
sudo lcm-agent enroll <url> --token-file <pfad>  # liest die erste Zeile, warnt bei Rechten über 0600
sudo lcm-agent enroll <url> <token>              # bleibt, warnt für lcma2 auf stderr (steht in ps)
```

Höchstens 4 KiB, Leerraum am Rand wird entfernt, ein leeres Ergebnis ist ein Fehler. Das Token
wird nie ausgegeben oder protokolliert (auch nicht in Fehlermeldungen).

### 10. Oberfläche und API

- `POST /servers/agent` und `POST /servers/:id/agent-token` nehmen `legacy` (Standard falsch) und
  liefern `token_expires_at`. Das Server-JSON bekommt `agent_secret_state` und
  `agent_token_expires_at`; Hashes bleiben unsichtbar.
- Der Assistent zeigt als Hauptbefehl die stdin-Form (`sudo lcm-agent enroll <url> -`, Token
  getrennt mit Kopierknopf), darunter „für Skripte: `--token-file`“. Ablaufzeit und
  „nur einmal benutzbar“ stehen direkt am Token. Der ganze Kettenbefehl (`manual`: Download und
  Enroll) nutzt ebenfalls die stdin-Form.
- Unter „Mein Agent ist älter als 2.5“ erscheint der alte Befehl mit Dauer-Token und einer Warnung;
  er ersetzt das bisherige Token.
- Server-Detail: „Wartet auf Agent, Token gültig bis …“, „Token abgelaufen, neues erzeugen“,
  „Betriebs-Secret aktiv seit …“. Ein Status je Server bleibt: der Zustand fließt in die
  bestehende Pille, nicht in eine zweite.

### 11. Audit

Ohne je ein Secret oder Token zu nennen (nur Kennung aus dem Hash, 8 Zeichen):
`server.agent_token_issue` (Ablauf, `legacy` ja/nein), `server.agent_enroll_propose` (IP, Art
`token`/`secret`), `server.agent_enroll_reject` (Grund `expired`/`mismatch`/`conflict`/`rate`;
je Agent höchstens einmal je Minute, damit Spam das Protokoll nicht füllt), `server.agent_secret_promoted`
(erste Anmeldung mit dem Betriebs-Secret, IP), `server.agent_token_expired`. Zählt zugleich
Fehlversuche je Agent-ID für S-06.

## Pakete

**T-1 Vertrag und Fähigkeit** (offen). `wire`: `TokenPrefixV2 = "lcma2."`, Feld `x`,
`EnrollRequest`/`EnrollResponse`, `CapabilitySecretExchange`, `NewSecret()`. `agentcaps`:
`FeatureSecretExchange`. Akzeptanz: `ParseToken` liest beide Formate, `lcma1.`-Tokens aller
bisherigen Fassungen bleiben gültig (Tabellentest mit den Tokens aus `agent_trust_test.go`);
Fuzz-Ziel für `ParseToken` mit beiden Präfixen; Inventare der Agenten 2.0.0 bis 2.4 melden die
Fähigkeit nicht, `Check` verneint sie.

**T-2 Paket `internal/core/agentenroll` und Datenmodell** (offen). Spalten, `Store`-Interface mit
Gorm-Umsetzung, `Issue`, `Propose`, `Promote` (Vergleich und Setzen in einem `UPDATE`), `State`.
Akzeptanz: Token wird beim Verbrauch gelöscht und lässt sich nicht erneut einlösen; Ablauf nach
24 h mit fester Uhr getestet; `Propose` ist idempotent (gleicher Hash 200, anderer 409); zwei
gleichzeitige `Promote` ergeben einen Gewinner und keinen Fehler für den zweiten mit gleichem Hash;
alle Fehlerarten ohne Hinweis, ob ein Hash existiert (gleiche Antwort für unbekannt und falsch).

**T-3 Hub und Endpunkt** (offen). `OnConnectAuthenticate` vergleicht zeitkonstant gegen aktiven und
ausstehenden Hash und befördert; `EnrollHandler` in `internal/remote`, eingehängt in
`NewAgentGateway` und in den gemeinsamen Port (Modus C); Limit je IP und Agent-ID; Audit.
Akzeptanz: Anmeldung mit Einmal-Secret als MQTT-Passwort wird immer abgewiesen; erste Anmeldung
mit `S` verbraucht das Token und macht das alte Secret ungültig (Test gegen echten Broker wie
`TestTakeoverKeepsOnline`); der Body taucht in keinem Log auf (Test mit Log-Puffer); `/mqtt` und
Limits bleiben unverändert (`ws_limit_test.go` grün); ein Fehlversuch-Zähler nach 5 Fehlern in
15 Minuten antwortet 429 und sperrt die MQTT-Anmeldung des echten Agenten nicht.

**T-4 Agent: Tausch** (offen). `next_secret` in `Config`, `LoadConfig` erlaubt leeres `secret`
bei gesetztem `next_secret`; gemeinsame TLS-Konfiguration für paho und `http.Client`
(`verifyPeer` mit Pin-Lernen); Verbindungsreihenfolge aus Abschnitt 4; Fähigkeit im Inventar;
`SecretState`. Akzeptanz: Tabelle aus Abschnitt 5 als Tests mit Fehlereinspielung an jeder Stelle
(Schreiben, `POST`, CONNECT); Agent ohne Server-Unterstützung (404) bleibt beim Dauer-Secret;
ein älteres Binary liest die Datei nach dem Tausch und verbindet sich; Pins werden beim Tausch
wie bisher gelernt.

**T-5 `lcm-agent enroll`** (offen). `-`, `--token-file`, Warnung für das Argument, Verdecken der
Eingabe (ohne neue Abhängigkeit; sonst Eingabe sichtbar), Wiederverwendung von `next_secret`,
`rotate-secret`. `Enroll` schreibt die Konfiguration **vor** dem Vorschlag und räumt bei einem
Fehler vor der Beförderung auf. Akzeptanz: stdin, Datei und Argument liefern dasselbe Ergebnis;
leeres, zu langes und beschädigtes Token geben klare Fehler; zweiter Aufruf nach Abbruch gelingt
mit demselben Token; `enroll_test.go` prüft, dass das Token in keiner Ausgabe steht.

**T-6 API und Oberfläche** (offen). Dienste (`CreateAgentServer`, `RegenerateAgentToken` mit
`legacy`), Controller, `agentCommands` und Assistent, Server-Detail, Reiter Sicherheit, de/en,
Unit-Tests `join-model.test.ts`, E2E `doku.spec.ts`. Akzeptanz: Der Anwender sieht Ablauf und
Einmaligkeit, kopiert die stdin-Form ohne Token in der Zeile; „älter als 2.5“ liefert das alte
Token; der Zustand kommt aus dem Server, nicht aus dem Frontend geraten; Hashes erscheinen nirgends
in JSON, MCP-Sichtmodell oder Logs (Test auf `buildServerView`).

**T-7 Übergang bestehender Agenten** (offen). Selbsttausch beim Start (Abschnitt 8), Befund und
Hinweistext, Zählung „Server mit Dauer-Secret“ für Übersicht und Ampel. Akzeptanz: ein 2.4-Agent
bleibt vor dem Update voll funktionsfähig; nach dem Update steht der Server innerhalb von zwei
Minuten auf `operating`, das alte Secret scheitert; der Hinweis verschwindet.

**T-8 Verträglichkeit und Chaos** (offen). `compat_agent_test.go` um den Tausch erweitert:
v2.0.0 bis v2.3.0 (und v2.4.x, sobald der Tag da ist) gegen den neuen Hub mit Dauer-Secret
(`legacy`); der neue Agent gegen den alten Server (`compat_server_test.go`, `LCM_COMPAT_SERVER_TAGS`):
404, bleibt am Dauer-Secret. `chaos_test.go`: Verbindungsabbruch mit dem `faultProxy` nach
`POST`, nach Beförderung vor CONNACK, während des Speicherns; Kill des Agenten an jeder Stelle des
Tauschs. Akzeptanz: in 200 Läufen je Abbruchstelle verbindet sich der Agent am Ende, ohne neues
Token, mit genau einem gültigen Secret auf dem Server.

**T-9 Dokumentation und Skripte** (offen). `docs/` und `internal/appdocs` (de/en), Postinstall-Text,
`usage()` des Agenten, `tools/distro-matrix.sh` (Token per stdin an `pct exec`), Release-Hinweis
(Skill `lcm-release`), `doc/sicherheit-agent-2026-11.md` S-05 auf „behoben“, Eintrag in
`doc/plan-2026-11-23-agent-robustheit.md` (Paket G).

**T-10 Später, nur mit Entscheidung** (offen). Schalter „Dauer-Secrets nicht mehr annehmen“, sobald
kein Server mehr auf `legacy` steht (oder die Mindestversion 2.5 ist); automatische Rotation.

Reihenfolge: T-1, T-2, danach T-3 und T-4 parallel, T-5, T-6, T-7, T-8, T-9. Vor jedem Push der
volle lokale Lauf nach Skill `gitlab-workflow`; die Tests gezielt je Paket, voller Lauf ohne
laufende Sub-Agents.

## Tests (Überblick)

- **Einheit**: `agentenroll` (Fristen, Idempotenz, Verbrauch, Rennen), Token-Format, Eingabewege
  des Enroll-Befehls, Konfiguration mit und ohne `next_secret`.
- **Hub**: Anmeldung mit aktivem, ausstehendem, falschem, abgelaufenem Secret; Übernahme der
  Sitzung beim Beförderen; Limit und Audit.
- **Verträglichkeit**: alte Agenten (v2.0.0 bis v2.3.0, v2.4.x) gegen neuen Hub; neuer Agent
  gegen alten Server; ältere Binary nach dem Tausch (Rückfall D-4).
- **Chaos**: Abbruch an jeder Stelle aus Abschnitt 5, Kill -9, Strom-Aus beim Speichern.
- **Ende zu Ende**: Distro-Matrix mit stdin-Enroll; ein Testlauf auf `lcm-test` mit einem 2.4-Agenten,
  der per Update tauscht.

## Risiken

- **R-1 Reverse Proxy** lässt nur `/mqtt` durch: Neueinrichtung scheitert, bestehende Agenten
  bleiben auf dem Dauer-Secret. Gegenmittel: `exchange-failed` im Inventar, Hinweis im Assistenten
  und Prüfung der Adresse; Dokumentation. Wer den Pfad nicht freigeben will, bleibt bei `legacy`.
- **R-2 Inhaber-Token**: Das Einmal-Token schützt nicht vor jemandem, der es vor dem Agenten
  sieht und einlöst. Verkürzt auf die Zeit bis zur ersten Anmeldung, im Audit sichtbar, mit
  stdin-Weg praktisch aus `ps` und Verlauf entfernt. Ein zweiter Faktor wäre ein eigenes Thema.
- **R-3 Zeitfenster beim Rückfall**: Stürzt der neue Agent zwischen Beförderung und Speichern ab
  und rollt dann auf ein älteres Binary zurück, kennt dieses nur das alte Secret. Sehr selten;
  Abhilfe ist ein neues Token. Wird zuerst im Chaos-Test gemessen.
- **R-4 Datenbank-Rücksicherung** älter als ein Tausch: betroffene Agenten brauchen ein neues Token.
- **R-5 Rate-Limit je Agent-ID** kann das Einlösen von außen stören, wer die UUID kennt; die
  MQTT-Anmeldung bleibt unberührt, und die UUID ist nicht erratbar.
- **R-6 Datenbankschreiben im Anmeldepfad**: nur bei der Beförderung (einmal je Agent); scheitert
  es, lehnt der Hook ab, der Agent versucht es erneut.
- **R-7 Uhr**: Ablauf zählt allein die Serveruhr; eine falsche Uhr des Agenten betrifft nur die
  Meldung „Token abgelaufen“, nie die Entscheidung.

## Entscheidungen (Tony, 25.11.2026)

1. **Versionsnummer**: legt der Release-Zug fest; im Code heißt die Schwelle über die Fähigkeit
   `secret-exchange`, nicht über eine feste Nummer.
2. **Selbsttausch automatisch** beim ersten Start nach dem Update. Scheitert er, läuft der Agent
   mit dem alten Secret weiter und versucht es später erneut.
3. **Token als Argument**: geht weiter, mit Warnung.
4. **Zweiter Pfad** `/api/v1/agent/enroll` auf dem Agent-Port: ja. Doku und Proxy-Beispiele
   bekommen den Pfad.
5. **Erneuern**: beides anbieten, „Secret wechseln“ ohne Unterbrechung und „Zugang sperren und
   neu einrichten“ wie heute.
6. **Rotation**: nur von Hand.
7. **Fristen**: fest, 24 h für das Token, 1 h für den ausstehenden Vorschlag.
8. **Dauer-Secrets abschalten**: später entscheiden; bis dahin laufen beide Wege, LCM zeigt je
   Server, wer noch alt ist.
