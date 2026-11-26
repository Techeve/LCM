# Sicherheitsdurchsicht des Agenten, November 2026

Stand 24.11.2026, Branch `feat/agent-sicherheit`. Zu Paket G (G-1, G-2, G-3) aus
`doc/plan-2026-11-23-agent-robustheit.md`. Geprüft wurden `internal/agent`,
`internal/remote` (Hub, Broker-Hook, WebSocket), `internal/remote/wire`, das
Startskript und die Paketskripte unter `packaging/` und der Update-Job in
`internal/core/services/agent_update.go`.

Der Agent läuft als root auf fremden Maschinen und führt aus, was LCM ihm schickt.
Alles, was hier als Befund steht, ist deshalb nach zwei Fragen eingeordnet: Kann ein
Dritter (Netz, lokaler Benutzer, übernommener Agent) mehr, als er soll? Und: Kann der
Agent durch eine falsche Eingabe sterben oder blockieren?

## Vorgehen und Regel für diese Durchsicht

- Code und Tests gelesen, wo es ging gegen die Bibliotheken geprüft (mochi 2.7.9, paho 1.5.1).
- Nichts an Anmeldung, Einrichtung, Zertifikats-Pins, Token oder Rechten verändert. Wo
  ein Befund dort liegt, steht nur der Vorschlag, die Entscheidung liegt bei Tony.
- Geändert wurde nur, was reine Robustheit ist (Panic, unbegrenzter Speicher, Namensschlag).
  Diese Änderungen stehen gesondert im Abschnitt „Was in dieser Durchsicht geändert wurde“.
- Neu sind Tests (auch Fuzz-Tests, Abschnitt „Fuzzing“) und diese Beschreibung.

## Ergebnis in Kürze

- Kein Befund der Stufe „hoch“, der ein Fehler wäre. Die größte Wirkung hat eine
  Entwurfsentscheidung: Wer den LCM-Server oder dessen TLS-Schlüssel übernimmt, hat root
  auf allen Agenten (S-15, Abschnitt „Grenze“). Das ist so gewollt und hier beschrieben.
- Die Prüfung des Server-Zertifikats ist richtig gebaut, hat aber zwei Lücken, die die
  Pins aushöhlen: Der Systemroot-Fallback gilt auch mit gesetzten Pins (S-01), und der
  Zertifikats-Pin aus dem Token wird nie abgelöst (S-02). Beides ist entschieden: S-01 bleibt so, S-02 ist am 24.11. behoben.
- Das „Einrichtungstoken“ ist kein Einmal-Token, sondern das dauerhafte Zugangs-Secret
  (S-05). Ohne Ablauf, ohne Zähler für Fehlversuche, und es steht auf der Kommandozeile.
- Ein Agent kann nicht auf Topics eines anderen Agenten schreiben oder dessen Ergebnisse
  fälschen (ACL und Abgleich der Agent-ID, geprüft und jetzt auch mit Fuzz-Test).
- Die Job-Ablage hat keinen Angriffsweg für lokale Benutzer ohne root. Zwei kleine
  Robustheitsfehler (ID `acked`, PID 1) sind behoben.
- Zwei Speichergrenzen fehlten und sind ergänzt (Sequencer der Konsole, Inventar).
- Die 15 Fuzz-Ziele (60 Sekunden je Ziel) fanden zwei kleine Fehler, beide behoben: die ID
  `acked` in der Job-Ablage und ein Ergebnis, das nach dem Kürzen 17 Bytes über dem Limit lag.

## Befunde

Schwere: hoch = Übernahme oder dauerhafter Ausfall ohne besondere Voraussetzung; mittel =
braucht eine Voraussetzung (Netzposition, geleakter alter Schlüssel, Strom-Ausfall im
falschen Moment) oder trifft die Verfügbarkeit; niedrig = Härtung, Voraussetzung ist
praktisch schon root oder LCM selbst. „Stand“: behoben = in dieser Durchsicht geändert;
Freigabe = betrifft Anmeldung, Pins, Token oder Rechte, nur Vorschlag; offen = Vorschlag,
keine Änderung.

### G-1: Agent und Hub

| ID | Schwere | Ort | Befund | Vorschlag | Stand |
|----|---------|-----|--------|-----------|-------|
| S-01 | mittel | `internal/agent/peer.go:54-63`, `client.go:200-203` | Der Fallback auf die Systemroots bleibt auch mit gesetzten Pins aktiv. Szenario: Der Agent ist mit `https://lcm.firma.de` eingerichtet und trägt CA- und Schlüssel-Pin. Wer für `lcm.firma.de` ein öffentlich vertrautes Zertifikat bekommt (DNS-Übernahme plus ACME, eine fehlerhaft ausstellende CA), stellt sich zwischen Agent und LCM. Das Zertifikat besteht die reguläre Prüfung, die Pins werden nie gefragt. Ergebnis: root-Shell auf allen Agenten dieses Namens. Genau davor sollen die Pins schützen. | Sobald der Agent einen Pin führt (`p.any()`), den Fallback abschalten. Läuft LCM bewusst hinter einem öffentlichen Zertifikat, trägt das Token keine Pins, und der Fallback gilt weiter (oder ein Feld `trust_system_roots` in `agent.json`). | bleibt so (Entscheidung Tony 24.11.) |
| S-02 | mittel | `internal/agent/client.go:234-257` (`learn`), `peer.go:84-86` | Der Zertifikats-Pin aus dem Token (`CertFingerprint`) wird nie gelöscht, auch nicht, wenn der Agent längst die CA und den Schlüssel gelernt hat. Er prüft keine Laufzeit. Szenario: LCM erneuert den Schlüssel regelmäßig, damit ein Abfluss begrenzt bleibt. Der allererste private Schlüssel von LCM (Backup, alte Platte, ausgemusterter Server) gilt aber für jeden Agenten, der damit eingerichtet wurde, für immer. Wer ihn hat, stellt sich per MITM zwischen Agent und LCM und bekommt root. | Nach dem Lernen des CA-Pins `CertFingerprint` aus `agent.json` entfernen. Der CA-Pin trägt dann allein, und alte Schlüssel sind wirklich alt. | behoben (24.11.) |
| S-03 | niedrig | `peer.go:83-90` | Zertifikats- und Schlüssel-Pin prüfen weder Laufzeit noch Namen. Ein abgelaufenes Zertifikat mit passendem Pin gilt weiter (Test `TestAbgelaufenesZertifikatMitPassendemPinWirdAngenommen`). Das ist gewollt: Der TLS-Handshake belegt den Besitz des privaten Schlüssels, und eine vergessene Erneuerung legt sonst alle Agenten still. Der CA-Pfad prüft die Laufzeit dagegen (CA 20 Jahre, Server-Zertifikat 90 Tage, `tlsx/ca.go:35`, `tlsx/issue.go:32`). Nach Ablauf der CA bliebe der Agent also nur über den Schlüssel-Pin verbunden. | Kein Eingriff. Im Journal warnen, wenn ein gepinntes Zertifikat abgelaufen ist, und LCM im Inventar melden, wann die CA abläuft. | offen |
| S-04 | niedrig | `peer.go:59`, `config.go:156` | Ein leerer Hostname schaltet die Namensprüfung im Fallback ab (x509 überspringt sie bei `DNSName == ""`). Erreichbar nur mit einer Server-Adresse ohne Namen, etwa `https://:8443`: `NormalizeServerURL` prüft `u.Host` (da steht `:8443`), nicht `u.Hostname()`. Der Agent würde dann `localhost` anwählen. Kein praktischer Angriff, aber ein Loch im Pfad. | `checkPeer` verweigert den Fallback bei leerem Hostnamen, `NormalizeServerURL` prüft `u.Hostname()`. | behoben (24.11.) |
| S-05 | mittel | `internal/core/services/agent_service.go:125-230`, `internal/agent/enroll.go:104` | Das „Einrichtungstoken“ ist das Dauer-Secret des Agenten (32 Zufallsbyte, im Hub als SHA-256 gespeichert). Es verfällt nicht, ist nicht einmalig und gilt nach `enroll` weiter. Wer es hat (aus `ps`, Shell-Verlauf, dem sudo-Protokoll oder einer Abschrift), meldet sich als dieser Agent an und verdrängt den echten (Session-Übernahme, `TestTakeoverKeepsOnline`). Er sieht dann alle Kommandos, die LCM diesem Server schickt, samt `stdin` (Passwörter für `sudo -S`). | Echtes Einmal-Token: Es gilt bis zur ersten erfolgreichen Anmeldung oder 24 Stunden und wird dort gegen ein neues, nur dem Agenten bekanntes Betriebs-Secret getauscht. Das Token zusätzlich als Datei oder über stdin annehmen, nicht nur als Argument. | Freigabe |
| S-06 | niedrig | `internal/remote/hub.go:1209-1230` | Fehlversuche bei der Anmeldung werden nicht gezählt und nicht protokolliert. Es gibt nur das Upgrade-Limit je IP (10 je Minute, 120 in den ersten 10 Minuten, 600 für bekannte Agent-IPs). Das Secret ist 256 Bit lang, Raten ist aussichtslos; es fehlt der Hinweis an den Betreiber, dass jemand es versucht. | Fehlversuche je Agent-ID im Audit und in der Ampel zählen (viele in kurzer Zeit = Befund). | offen |
| S-07 | mittel | `hub.go:278`, `ws.go:69`, mochi `server.go:442` | Vor dem CONNECT-Paket gilt weder eine Frist noch eine kleine Größe. mochi setzt die Lese-Frist erst nach dem CONNECT (`refreshDeadline`, Keepalive des Clients; 0 heißt keine Frist). Das Paketlimit ist 8 MiB, auch für das erste Paket. Szenario: Ein Unbekannter macht 10 WebSocket-Upgrades je Minute, sendet nie ein CONNECT und hält die Verbindungen offen; jede kostet eine Goroutine, Puffer und einen Socket, bis er sie schließt. Aus dem Code gelesen, nicht per Test belegt. | Lese-Frist von etwa 10 s auf der Verbindung bis zum CONNECT, danach Keepalive des Clients; bis zur Anmeldung nur Pakete bis wenige KiB (Hook `OnPacketRead`). Obergrenze unangemeldeter Sockets. | behoben (24.11.) |
| S-08 | mittel | `internal/agent/config.go:128-137` (`Save`), `client.go:234-257` | `agent.json` wird nicht atomar geschrieben (`os.WriteFile`, Abschneiden und Schreiben). Der Agent schreibt sie selbst, wenn er einen Pin lernt. Fällt der Strom in diesem Augenblick aus, bleibt eine halbe Datei: `LoadConfig` scheitert, die Unit startet wegen `ConditionPathExists` und `Restart=always` endlos neu, der Agent ist tot, bis jemand neu einrichtet. Außerdem prüft nichts beim Start die Rechte: `WriteFile` ändert die Rechte einer bestehenden Datei nicht, eine auf 0644 gesetzte bleibt 0644. | Über eine Zwischendatei im selben Verzeichnis schreiben (Modus der bestehenden Datei, sonst 0600; `fsync`, `rename`). Beim Start im Journal warnen, wenn Gruppe oder andere lesen können. Nicht umgesetzt, weil es die Datei mit dem Secret betrifft. | behoben (24.11.) |
| S-09 | niedrig | `hub.go` (`publishCommand`), paho `packets/packets.go:146` | In Richtung Agent gibt es keine Größengrenze. Der Hub prüft Kommando plus `stdin` nicht gegen `MaxPacketSize`, und paho legt für jedes Paket einen Puffer in der Länge des Kopfes an (bis 256 MiB), ohne Grenze für die WebSocket-Nachricht. Wer LCM übernimmt, kann den Agenten so aus dem Speicher werfen. Innerhalb der Grenze aus S-15 (er kann ihn auch mit einem Kommando beenden), aber ein Programmierfehler in LCM träfe dasselbe. | Hub weist Kommandos über 4 MiB ab; Agent setzt, sobald paho es erlaubt, ein Lese-Limit auf der WebSocket-Verbindung. | offen |
| S-10 | niedrig | `internal/agent/jobs.go:458-480` (`trim`) | Die Ausgabedatei bleibt klein, weil `trim` die Mitte per `fallocate(PUNCH_HOLE)` freigibt. Unterstützt das Dateisystem es nicht (je nach Version ZFS, manche Netzlaufwerke), setzt `trim` `noPunch` und die Datei wächst weiter. Ein Kommando mit Gigabytes Ausgabe füllt dann `/var`. Ob die ZFS-Hosts im Bestand betroffen sind, ist nicht geprüft (Probe auf einem Host offen). | Bei `noPunch` ein Warnhinweis im Journal und in den Lebenszeichen. Wer die Ausgabe grundsätzlich begrenzen will, ändert die Entscheidung vom 23.11. („Runner hält nie ein Kommando an“) und braucht dafür Tonys Freigabe. | offen |
| S-11 | niedrig | `internal/agent/runner.go:105`, `runner.go:407`, `jobs.go:195` | Befehls-IDs haben keine Längengrenze. Der Agent merkt sich 1024 davon im Speicher, schreibt sie alle in `acked` und meldet sie im Inventar (`known_jobs`). LCM schickt UUIDs mit 36 Zeichen; ein übernommener LCM könnte 1024 IDs zu je mehreren MiB schicken. | IDs über 128 Bytes in `handleCommand` abweisen. | offen |
| S-12 | niedrig | `hub.go:1235-1250` (`OnACLCheck`) | Die ACL prüft nur den Topic-Namen, nicht die Größe oder den Inhalt. Ein übernommener Agent kann auf `status` bis zu 8 MiB als „retained“ ablegen, die der Broker im Speicher hält (bis zum Neustart), und auf `inv` und `ttyo` beliebige Pakete bis 8 MiB schicken. | Hook `OnPublish`: je Topic eine Obergrenze (`status` 16 Bytes, `inv` 64 KiB, `ttyo` 64 KiB, `res` 8 MiB). | offen |
| S-13 | mittel | `internal/remote/wire/tty.go` (`Sequencer`) | Der Sequencer der Konsole hielt vorausgeeilte Rahmen nur nach Anzahl (256), nicht nach Größe. Ein Rahmen darf bis zum Paketlimit (8 MiB) groß sein: Ein Agent (Hub-Seite) oder LCM (Agent-Seite), der Nummer 1 nie schickt, konnte so bis zu 2 GiB aufstauen. | Obergrenze der gehaltenen Nutzdaten (`maxHeldBytes`, 8 MiB). | behoben |
| S-14 | niedrig | `internal/remote/hub.go:596-680` (`handleInventory`, `storeInventory`) | Das Inventar eines Agenten wurde ungekürzt in die Serverzeile geschrieben (`agent_version`, Pins, Fähigkeiten, Rollback-Angaben). Ein übernommener Agent konnte bei jedem Verbindungsaufbau 8 MiB in die Datenbank legen. | Felder auf 256 Bytes, Fähigkeiten auf 32 Stück kürzen (`boundedInventory`). Echte Inventare bleiben unverändert (Test mit den Inventaren der Agenten 2.0.0 bis 2.3.0). | behoben |
| S-15 | hoch (Entwurf) | alles | Ein kompromittierter LCM-Server (oder wer sein TLS-Zertifikat samt Schlüssel hat) führt auf allen Agenten beliebige Kommandos als root aus. Der Agent kennt keine Liste erlaubter Kommandos und prüft nichts außer dem Server-Zertifikat. | Abschnitt „Grenze“. Kein Eingriff ohne Entscheidung, ob Kommandos je signiert werden sollen. | bleibt so (Entscheidung Tony 24.11.) |

Kein Befund, geprüft:

- **Fremde Topics.** `OnACLCheck` (`hub.go:1235`) erlaubt Schreiben nur auf die eigenen vier
  Topics `res`, `inv`, `status`, `ttyo` und Lesen nur auf `cmd` und `tty`, jeweils als
  exakter Vergleich mit der eigenen ID. Filter mit `+`, `#` oder `$share/…` sind keine
  exakte Gleichheit und fallen durch. Die Client-ID muss die Agent-ID sein
  (`OnConnectAuthenticate`), damit kein gültiger Agent die Session eines anderen
  übernimmt. Test: `TestOnACLCheck`, `TestTakeoverKeepsOnline`.
- **Ergebnis-Fälschung.** `handleResult` stellt nur zu, wenn die Agent-ID aus dem Topic
  der des wartenden Aufrufs entspricht (`hub.go:554`), ein „progress“ eines Fremden setzt
  die Uhr nicht zurück. Request-IDs sind UUIDv4. Neuer Test
  `TestFremdesErgebnisSchliesstKeinenAufruf` und der Fuzz-Test `FuzzHandleResult`. Der
  eigene Agent darf für seine eigenen Aufrufe beliebige Ausgaben liefern; was ein
  Server zurückgibt, ist für LCM ohnehin nicht vertrauenswürdig (Parser-Fuzz in
  `internal/core/services/parsers_fuzz_test.go`).
- **Konsolen-Sitzungen.** `handleTTY` stellt nur Rahmen des Agenten zu, der die Sitzung hält
  (`tty.go:306`). Die Sitzungs-ID wird nur als Schlüssel im Speicher benutzt, nie als
  Dateiname. Die Konsole des Hubs begrenzt den ungelesenen Bildschirm auf 8 MiB
  (`maxTerminalBacklog`), der Agent höchstens zwei Konsolen (`maxTerminals`), 30 Minuten
  Stille beenden eine Sitzung, und das Warten auf die Bestätigung bremst `yes`/`cat`.
- **Passwörter.** `stdin` geht über eine Pipe und berührt die Platte nie (`jobrun.go`).
  Die Befehlszeile selbst liegt in `cmd.sh` (0600, Verzeichnis 0700) und verschwindet mit
  dem Ende des Jobs.
- **TLS.** `MinVersion` ist 1.2, der Handshake belegt auch bei `InsecureSkipVerify` den
  Besitz des privaten Schlüssels des vorgelegten Zertifikats, die Pins sind also keine
  bloßen Vergleiche von öffentlichen Daten. Leere Pins nehmen nichts außer dem, was die
  Systemroots mit passendem Namen prüfen (neuer Test
  `TestLerePinsNehmenKeinSelbstsigniertesZertifikat`). Eigene Heim-CAs gelten nur mit
  Namen (`verifyCustomIssuedBy`), die LCM-CA nur mit der Kennung `urn:lcm:server`.
- **Rotation.** Der Schlüssel-Pin zieht mit jeder bestätigten Verbindung mit, der CA-Pin
  trägt neue Schlüssel (`learnFrom`). Ein Agent bis 2.0 ohne Schlüssel-Pin hält LCM davon
  ab, den Schlüssel zu wechseln (`AgentKeyPin` im Inventar). Tests in `pin_test.go` und
  `custom_ca_test.go`.

## Grenze: Was ein kompromittierter LCM-Server am Agenten darf

Der Agent bietet LCM genau diese Wege an (Topic `cmd` und `tty`, `internal/agent/client.go`,
`tty.go`):

| Was LCM schickt | Was der Agent tut | Beschränkung |
|-----------------|-------------------|--------------|
| `cmd` mit `Cmd` und `Stdin` | Führt `/bin/sh cmd.sh` als root aus, in eigenem systemd-Scope | keine; es gibt weder eine Liste erlaubter Kommandos noch eine Signatur |
| `cmd` mit `Cancel` | SIGTERM an die Gruppe, nach 30 s SIGKILL | nur eigene Jobs, nach PID und Startzeit geprüft |
| `cmd` mit `Ping` | antwortet mit Pong | |
| `tty` öffnen, Daten, Größe, schließen | Login-Shell als root im Pseudo-Terminal | zwei Sitzungen; Schalter `lcm-agent terminal off` |
| Update-Job (nur ein gewöhnliches Kommando) | `apt-get install lcm-agent=<ver>`, danach `exec` aufs neue Binary | Version gegen die Feature-Reihe von LCM geprüft, Quelle ist die des Servers |

Es gibt keinen Weg, der mehr kann als ein Kommando: keine Dateiübertragung, keine
Konfigurationsänderung aus der Ferne, keine Daten des Servers werden als Pfad benutzt
(Befehls-IDs laufen über `safeID`, Sitzungs-IDs bleiben im Speicher, die Version eines
Updates über `rePackageVersion`). Das Ziel aus dem Plan („kein Ausbruch über das hinaus,
was ein Kommando ohnehin darf“) ist erreicht.

Folgen, die man kennen muss:

1. **Wer LCM hat, hat root auf allen Agenten.** Die Echtheit eines Kommandos hängt allein
   an der TLS-Verbindung (Pins) und am MQTT-Zugang. Kommandos sind nicht signiert. Wer den
   LCM-Server, seinen TLS-Schlüssel (oder, mit S-01 und S-02, ein Zertifikat für den Namen)
   in die Hand bekommt, kann jeden Agenten anweisen.
2. **`terminal off` schützt nicht vor einem kompromittierten LCM.** Der Schalter ist eine
   Datei (`/etc/lcm-agent/terminal-off`); ein `cmd` kann sie löschen. Er schützt vor
   Fehlbedienung in der Oberfläche und vor einem Benutzer von LCM, nicht vor dem Server.
3. **Der Agent ist nicht gehärtet.** Die Unit hat `User=root` und `NoNewPrivileges=no`,
   weil apt, Docker und die Firewall gebraucht werden (wie eine SSH-Root-Sitzung).
4. **Ein kompromittierter Agent kann LCM wenig anhaben:** nur eigene Topics, nur eigene
   Aufrufe, Größen begrenzt (S-14, S-13), lügen kann er über alles, was er meldet. Dass LCM
   Ausgaben fremder Server als Eingabe von Angreifern behandelt, ist der Grund der
   Parser-Fuzz-Tests.

Mögliche Abhilfen, wenn die Grenze enger werden soll (nur Entscheidungen, nichts davon ist gebaut):
Kommandos mit einem Schlüssel signieren, der nicht auf dem LCM-Host liegt (nur für
Verwaltungsvorgänge), ein Nur-Lese-Modus des Agenten (Scan, kein `apt`), eine Liste
erlaubter Skripte statt freier Shell.

## Einrichtungstoken, Anmeldung

| Frage | Antwort |
|-------|---------|
| Einmalig? | Nein. Das Token ist das Dauer-Secret (`agentToken`, `wire.EncodeToken`). Es wird einmal angezeigt, gilt aber unbegrenzt oft. |
| Ablauf? | Keiner. Es endet nur durch „Token erneuern“ (`RegenerateAgentToken`, trennt die Sitzung) oder Löschen des Servers. |
| Fehlversuche? | Kein Zähler je Agent. Je Quell-IP 10 Upgrades je Minute (`upgradeLimitUnknown`), 120 in den ersten 10 Minuten nach dem Start, 600 für IPs, von denen sich schon ein Agent angemeldet hat. Der Vergleich läuft konstant in der Zeit (`subtle.ConstantTimeCompare`). |
| Wo steht es? | Im Hub nur als SHA-256 (`AgentTokenHash`) und die ersten 12 Zeichen (`AgentTokenPrefix`). Auf dem Agenten im Klartext in `agent.json` (0600). Beim Einrichten auf der Kommandozeile. |
| Was trägt es noch? | Pins: Fingerprint des Zertifikats, der LCM-CA und optional einer eigenen CA (`Token`). Sie sind der Schutz vor einem Mann in der Mitte beim ersten Kontakt. |

## Rechte

| Pfad | Rechte | Wer legt an | Anmerkung |
|------|--------|-------------|-----------|
| `/etc/lcm-agent` | 0700 root | `enroll` (`Config.Save`, `MkdirAll`) | das Paket legt es nicht an |
| `/etc/lcm-agent/agent.json` | 0600 bei Anlage | `enroll`, später der Agent (Pins) | wird atomar ersetzt (Zwischendatei, `fsync`, `rename`), die Rechte der bestehenden Datei bleiben; beim Start warnt der Agent im Journal, wenn Gruppe oder andere lesen können, ändert sie aber nicht (S-08) |
| `/etc/lcm-agent/terminal-off` | 0600 | `lcm-agent terminal off` | Dasein = Konsole aus; nicht lesbar ⇒ gilt als aus (`TerminalDisabled`) |
| `/var/lib/lcm-agent` | 0755 oder 0700, je nachdem, wer zuerst anlegt (Startskript mit `mkdir -p` oder Agent mit `MkdirAll`) | nicht das Paket | unkritisch: Alles darunter ist 0700 |
| `/var/lib/lcm-agent/jobs` | 0700, bei jedem `create` per `Chmod` erneut | Agent | Dateien 0600 |
| `/var/lib/lcm-agent/update` | 0700 (Startskript `chmod 700`, `preinst` `chmod 700`, Agent `MkdirAll(0700)`) | Skript, Agent | Zustandsdateien 0600 (Agent) bzw. 0644 (Skript, im 0700-Verzeichnis); `lcm-agent.prev` 0755 |
| `/usr/bin/lcm-agent`, `/usr/lib/lcm-agent/lcm-agent-start` | 0755 root | dpkg | |
| `/usr/lib/systemd/system/lcm-agent.service` | 0644 root | dpkg | |
| `/etc/apt/preferences.d/lcm-agent` | 0644 (umask) | `postinst` | Pin auf die installierte Version, Priorität 1001 |
| `/etc/needrestart/conf.d/lcm-agent.conf` | 0644 root | dpkg | |

Nichts davon ist von einem unprivilegierten Benutzer beschreibbar. Die Skripte benutzen
feste Zwischendateien (`*.tmp`) in Verzeichnissen, die nur root betreten darf.

## Grenzen und Größen

| Was | Grenze | Wo | Bemerkung |
|-----|--------|----|-----------|
| MQTT-Paket Agent → Hub | 8 MiB (`MaxPacketSize`) | Broker `hub.go:278`, WebSocket `ws.go:69` | vor der Anmeldung nur das erste Paket und höchstens 8 KiB (`preAuthMaxPacket`, `preauth.go`), danach gilt die Grenze (S-07) |
| MQTT-Paket Hub → Agent | keine | Hub prüft nicht, paho legt bis 256 MiB an | S-09 |
| Ergebnis eines Kommandos | `MaxResultBytes` = 8 MiB − 64 KiB | `fitOutput` misst das serialisierte JSON; Fuzz-Test `FuzzFitOutput` | Ausgabe selbst höchstens 4 MiB (Anfang und Ende je 2 MiB) |
| Ausgabedatei des Jobs | Anfang und Ende je 2 MiB werden gelesen; die Datei bleibt ab 12 MiB durch Löcher klein | `trim` (`jobs.go:458`) | ohne `PUNCH_HOLE` wächst sie (S-10) |
| Ergebnisse, die LCM nicht bestätigt hat | 32 Stück, 7 Tage (`keepResults`) | `prune` | höchstens 32 · 8 MiB auf der Platte |
| Verzeichnisse ohne `meta.json` | nach 1 Stunde weg | `prune` | |
| Befehls-IDs im Speicher und in `acked` | 1024 Stück (`commandIDWindow`) | `idWindow` | Länge je ID unbegrenzt (S-11) |
| Gleichzeitige Kommandos | 1 | `Runner.slot` | weitere warten |
| Rahmen der Konsole | 32 KiB je Rahmen beim Absender (`MaxTTYChunk`), beim Empfänger bis zum Paketlimit | `wire/tty.go` | Sequencer hält jetzt höchstens 8 MiB zurück (S-13) |
| Ungelesener Bildschirm im Hub | 8 MiB (`maxTerminalBacklog`) | `remote/tty.go` | danach Sitzung zu |
| Konsolen je Agent | 2 | `maxTerminals` | |
| Beendete Sitzungen im Speicher | 1024 IDs | `terminals.ended` | |
| Konsole ohne Eingabe | 30 Minuten | `terminalIdleTimeout` | |
| Inventar in der Datenbank | Felder 256 Bytes, 32 Fähigkeiten | `boundedInventory` | `known_jobs` bleibt ungekürzt, wird nur verglichen |
| Verbindungen ohne Anmeldung | CONNECT binnen 10 s (`preAuthDeadline`), höchstens 64 gleichzeitig (`preAuthMaxConns`), darüber 503 vor dem Upgrade | `preauth.go`, `ws.go` | S-07 |
| Neue Verbindungen je IP | 10 je Minute, 120 in den ersten 10 Minuten nach dem Start, 600 für bekannte Agent-IPs | `ws.go` | |
| Offene Verbindungen insgesamt | keine Grenze (`MaximumClients` = `MaxInt64`) | mochi-Vorgabe | angemeldete Agenten; unangemeldete begrenzt (S-07) |
| Abbrüche, die auf einen getrennten Agenten warten | 256 je Agent (`maxQueuedCancels`) | `hub.go` | |

## G-3: Neue Angriffsfläche

| ID | Schwere | Ort | Befund | Vorschlag | Stand |
|----|---------|-----|--------|-----------|-------|
| J-01 | niedrig | `internal/agent/jobs.go:134` (`safeID`) | Eine Befehls-ID `acked` ergab das Verzeichnis `jobs/acked` und stieß mit der Datei der bestätigten IDs zusammen: Anlegen scheiterte, und `ack` löschte beim Bestätigen die Liste (danach liefe ein nachgelieferter Befehl nach einem Neustart doppelt). Nur mit einer nicht-UUID-ID von LCM erreichbar. | `safeID` meidet den Namen. | behoben |
| J-02 | niedrig | `internal/agent/jobrun.go:265-275` (`signal`) | Stand in der Datei `pid` die Zahl 1 (beschädigt) und fehlte die Startzeit, ging `kill(-1, SIGTERM/SIGKILL)` an jeden Prozess des Systems. Erreichbar über einen ausdrücklichen Abbruch durch LCM. | Nur `pid > 1`. | behoben |
| J-03 | niedrig | `internal/agent/procfs_linux.go:84` (`groupIsOurs`) | Bei unbekannter Startzeit gilt jede PID als „unser“. Eine wiederverwendete PID träfe dann eine fremde Prozessgruppe. | Dann nur den Scope abbrechen, nicht die Gruppe. | offen |
| J-04 | niedrig | `internal/agent/jobrun.go:282` (`killScope`) | `scope` aus `meta.json` geht ungeprüft als Argument an `systemctl kill`; mit führendem `-` wäre es eine Option. Die Datei gehört root. | Gegen `^lcm-job-[A-Za-z0-9-]+\.scope$` prüfen, `--` davor. | offen |
| J-05 | niedrig | `internal/agent/jobs.go:146-160` | Die Ablage folgt Symlinks (`ReadFile`, `WriteFile`, `Chmod`, `RemoveAll` ohne `O_NOFOLLOW`). Ein lokaler Benutzer kann keinen anlegen (Verzeichnis 0700 root, Elternverzeichnis in `/var/lib`), also kein Angriffsweg. | Härtung: vor dem Gebrauch prüfen, dass `jobs` ein echtes Verzeichnis von root ist. | offen |
| J-06 | niedrig | `packaging/lcm-agent-start.sh:75-80`, `internal/agent/update.go:166` | Das Rückfall-Binary wird nicht gegen dpkg geprüft; ein erzwungener Absturz-Zyklus bringt den Agenten auf die vorige (ältere) Version. Beides braucht root oder einen Absturzfehler im neuen Binary. | Rückfall nur auf Versionen ab `agentcaps.MinAgentVersion`; Quelle der Kopie (preinst, gesunder Agent) im Journal nennen. | offen |
| J-07 | niedrig | `internal/core/services/agent_update.go:160-200` | Der Update-Job prüft nicht, aus welcher Quelle die gewählte Version kommt (`apt-cache madison` über alle Quellen, die Quelle steht nur im Protokoll), und erkennt keine Quelle ohne Signaturprüfung (`[trusted=yes]`). Die Version selbst ist gegen `rePackageVersion` geprüft, bevor sie in eine Shell-Zeile kommt (`agent_update.go:189`). | Ziel nur annehmen, wenn `apt-cache policy lcm-agent` die Techeve-Quelle nennt; Quelle ohne Signaturprüfung als Warnung im Plan. | offen |

Die Einzelheiten dazu stehen in den folgenden Abschnitten.

### Job-Ablage (C-2)

- **Pfade.** Jede Befehls-ID läuft über `safeID` (`jobs.go:134`): nur `[A-Za-z0-9-]`, 1 bis 64
  Zeichen, sonst ein SHA-256-Hash. Daraus wird `JobsDir/<name>`. Kein Schrägstrich, kein
  Punkt, also kein `..`. Neu ausgeschlossen ist der Name `acked` (J-01). Fuzz-Test
  `FuzzSafeID` prüft: immer ein einzelner Verzeichnisname, stabil, `create` legt das
  Verzeichnis direkt unter der Ablage an, `ack` entfernt es.
- **Symlinks.** `os.ReadFile`, `os.WriteFile`, `os.Chmod` und `os.RemoveAll` in der Ablage
  folgen Symlinks (kein `O_NOFOLLOW`). Ein lokaler Benutzer ohne root kann keinen
  anlegen: `jobs` ist 0700 root, und das Verzeichnis darüber erlaubt keinem anderen das
  Anlegen von Einträgen. Der einzige Weg wäre, `/var/lib/lcm-agent` vor der Installation
  mit eigenen Rechten anzulegen, und das darf nur root in `/var/lib`. Kein Angriffsweg
  (J-05, Härtung offen: vor dem ersten Gebrauch prüfen, dass `jobs` ein echtes
  Verzeichnis von root ist).
- **Inhalte aus der Platte.** `meta.json`, `pid`, `exit`, `result.json` und `acked` können
  nach einem Absturz halb geschrieben sein. Der Fuzz-Test `FuzzJobStoreFiles` füttert
  alle mit beliebigen Bytes und ruft das Lesen, Aufräumen, Zusammenstellen von `Known`,
  `Pending` und `Result` auf: kein Panic, und was lesbar ist, schreibt sich lesbar zurück.
- **Signale.** Ein Abbruch trifft die Gruppe `-pid` nur, wenn die Startzeit der PID noch
  stimmt (`groupIsOurs`). Zwei Fehler dort:
  - J-02 (behoben): Stand in `pid` die Zahl 1 (beschädigt) und fehlte die Startzeit,
    ging `kill(-1)` an jeden Prozess des Systems. Jetzt nur `pid > 1`.
  - J-03 (offen, niedrig): `groupIsOurs` ist bei unbekannter Startzeit wahr
    (`procfs_linux.go:84`). Eine wiederverwendete PID trifft dann eine fremde Gruppe.
    Unbekannte Startzeit kommt nur vor, wenn der Agent `/proc/<pid>/stat` beim Start
    nicht lesen konnte. Vorschlag: dann nur den Scope abbrechen, nicht die Gruppe.
- **Scope-Name.** Der Wert `scope` aus `meta.json` geht als Argument an `systemctl kill`
  (`jobrun.go`, `killScope`). Er stammt aus einer root-eigenen Datei; würde er mit `-`
  beginnen, wäre er eine Option (J-04, niedrig). Vorschlag: gegen
  `^lcm-job-[A-Za-z0-9-]+\.scope$` prüfen und `--` davorsetzen.
- **Sonstiges.** Zwischendateien heißen vorhersehbar (`meta.json.tmp`), liegen aber im
  0700-Verzeichnis. Wrapper-Skript und `cmd.sh` stehen in 0700; der Wrapper bekommt das
  Verzeichnis als `$1`, in Anführungszeichen.

### Wächter und Rückfall-Binary (D-4)

- `/var/lib/lcm-agent/update` ist 0700 root; `lcm-agent.prev` 0755 darin. Ersetzen kann es
  nur root. Wer root ist, braucht den Umweg nicht (J-06 ist darum nur ein Vermerk).
- **Rückfall nur auf das Paket-Binary?** Nein, nur auf das *vorige*: `lcm-agent.prev` ist
  eine Kopie, die das `preinst` des neuen Pakets von `/usr/bin/lcm-agent` macht, und die
  jeder gesunde Agent von sich selbst anlegt (`savePrev`). Gegen die Prüfsummen von dpkg
  wird sie nicht geprüft. Das ist in Ordnung, solange nur root schreiben kann.
- **Rückfall als Absenkung (J-06, niedrig).** Wer den neuen Agenten dreimal in Folge zum
  Absturz bringt, erzwingt den Rückfall auf die vorige Version (die ältere, womöglich mit
  bekannter Lücke). Das braucht einen Absturz-Fehler im neuen Binary; die Handler sind
  gegen Panics abgesichert (`safego.Recover`, Fuzz-Tests). LCM zeigt den Rückfall als
  Befund in der Ampel (D-4), und der Update-Job sperrt weitere Läufe bis „Fortsetzen“. Offen:
  Der Rückfall könnte unter der Mindestversion (`agentcaps.MinAgentVersion`) abgelehnt werden.
- **Umgebung.** Das Skript liest `LCM_AGENT_BIN` und `LCM_AGENT_STATE` (Test-Nähte des
  Skripts `agent-scripts-test.sh`), auch im Betrieb. Setzen kann sie nur, wer die Unit
  ändert, also root. Kein Befund, nur der Hinweis, dass die Nähte im Betrieb gelten.
- **Rückfall bei Abweisung durch LCM.** Seit 24.11. rollt eine ausdrückliche Abweisung
  durch LCM kein Update mehr zurück und alte Agenten haben ihre Notbremse wieder
  (Commit `c3cfdcf6`, siehe Review-Befunde im Plan). Hier nicht neu bewertet.

### Update-Job (D-2)

- **Version vor der Shell.** `parseAgentVersions` nimmt nur Versionen, die
  `rePackageVersion` (`^[A-Za-z0-9][A-Za-z0-9+.:~_-]*$`) treffen (`agent_update.go:189`).
  Das Ziel kommt aus `agentcaps.PickTarget` und ist immer ein Element dieser Liste.
  `agentInstallScript` setzt es erst danach in die Zeile ein (`lcm-agent=<ver>`). Die Zeichen
  enthalten nichts, was die Shell deutet, und die Version beginnt nie mit `-`. In Ordnung.
- **Nur Versionen des eigenen Kanals?** `PickTarget` wählt die höchste Version bis zur
  Feature-Reihe von LCM (nie darüber, nie unter der installierten). Woher sie kommt,
  prüft LCM nicht (J-07, niedrig): `apt-cache madison lcm-agent` listet **alle** Quellen des
  Servers, die Quelle landet nur im Protokoll (`Sources`). Hat der verwaltete Server eine
  zweite Quelle mit einem Paket gleichen Namens, kann die Version von dort kommen. Wer
  eine Quelle auf diesem Server einrichten kann, ist ohnehin root. Vorschlag: Das
  Ziel nur annehmen, wenn `apt-cache policy lcm-agent` die Techeve-Quelle als Herkunft
  nennt, und im Plan nennen, welche Quelle gilt.
- **Signatur.** apt prüft sie nach der Konfiguration des Servers. Der Job ruft apt ohne
  `--allow-unauthenticated`; er erkennt aber auch keine Quelle mit `[trusted=yes]` (die
  Einrichtung von LCM lehnt solche Zeilen ab, `lcm-helper.sh:105-110`). Vorschlag: Der Plan
  des Updates meldet eine Quelle ohne Signaturprüfung als Warnung (wie `enterprise_pin`).
- **Ablauf.** `--allow-downgrades` und `--allow-change-held-packages` sind gesetzt,
  weil der Pin des Pakets durchbrochen werden muss. Abwärts geht LCM nie
  (`PickTarget`).

## Was in dieser Durchsicht geändert wurde (reine Robustheit)

Keine dieser Änderungen berührt Anmeldung, Einrichtung, Zertifikats-Pins, Token oder
Rechte.

| Änderung | Datei | Warum es keine Sicherheits- oder Rechteänderung ist |
|----------|-------|------------------------------------------------------|
| Sequencer hält höchstens 8 MiB (`maxHeldBytes`), ein doppelt zugestellter, vorausgeeilter Rahmen zählt einmal | `internal/remote/wire/tty.go` | Speichergrenze für Rahmen der Konsole in beide Richtungen; ehrliche Absender (32 KiB je Rahmen, höchstens 256 vorausgeeilt) bleiben darunter. Wer sie überschreitet, bekommt dieselbe Antwort wie bisher bei zu vielen Rahmen (`ErrTTYGap`). |
| `boundedInventory` kürzt die gespeicherten Felder des Inventars | `internal/remote/hub.go` | Schutz der Datenbankzeile vor Riesenwerten. Gültige Inventare bleiben unverändert (Test mit den Inventaren 2.0.0 bis 2.3.0 und dem aktuellen); `known_jobs` bleibt unberührt, `nil` bleibt `nil`. Die Anmeldung des Agenten ist davon nicht berührt. |
| `safeID` weicht dem Namen `acked` aus | `internal/agent/jobs.go` | Verhindert, dass ein Job die Liste der bestätigten IDs überdeckt und beim Bestätigen löscht. Nur eine ID mit genau diesem Wert ändert sich; UUIDs nicht. |
| `fitOutput` misst `truncated` mit | `internal/agent/output.go` | Das Ergebnis lag nach dem Kürzen um 17 Bytes über dem Limit (Fuzz-Fund). Es bleiben höchstens ein paar Bytes Ausgabe weniger stehen; sonst ändert sich nichts an der Nachricht. |
| Signal nur an PID > 1 (`killProcess` ersetzbar für Tests) | `internal/agent/jobrun.go` | Verhindert `kill(-1)` bei beschädigter `pid`-Datei. Für jede echte PID gleiches Verhalten. |

Tests dazu: `TestSequencerHaeltNurBegrenzteDaten`, `TestSequencerDoppelterVorausRahmen`
(`wire/tty_test.go`), `TestBoundedInventory…` (`remote/fuzz_test.go`),
`TestSafeIDMeidetDieListeDerBestaetigtenIDs`, `TestSignalSendetNieAnPID1`
(`agent/fuzz_test.go`), `TestFremdesErgebnisSchliesstKeinenAufruf`,
`TestLerePinsNehmenKeinSelbstsigniertesZertifikat`,
`TestAbgelaufenesZertifikatMitPassendemPinWirdAngenommen`,
`TestSchluesselPinNimmtJedesZertifikatMitDemSchluessel`,
`TestKaputteKetteLehrtKeineCA` (`agent/peer_review_test.go`). Die letzten drei halten das
heutige Verhalten der Zertifikatsprüfung fest, sie ändern es nicht.

## Fuzzing (G-2)

Vorbild ist `internal/core/services/parsers_fuzz_test.go`. Es gibt 15 Ziele; ohne `-fuzz`
laufen nur ihre Startkorpora (wenige Sekunden zusammen, `go test ./...` bleibt schnell). Die
Läufe mit Mutation gehören lokal vor den Release-Zug (Plan: 10 Minuten je Ziel).

| Ziel | Paket | Eigenschaft |
|------|-------|-------------|
| `FuzzDecodeCommand`, `FuzzDecodeResult`, `FuzzDecodeInventory`, `FuzzDecodeTTYFrame` | `internal/remote/wire` | kein Panic; Kodieren ist nach einem Rundlauf stabil; Speicher fürs Dekodieren höchstens 64 · Eingabe + 1 MiB |
| `FuzzSequencer` | `internal/remote/wire` | jede Nummer höchstens einmal, aufsteigend; gehaltene Nutzdaten höchstens `maxHeldBytes` |
| `FuzzParseToken`, `FuzzAgentIDFromTopic` | `internal/remote/wire` | kein Panic; angenommenes Token läuft rund; die ID enthält nie einen Schrägstrich |
| `FuzzHandleResult` | `internal/remote` | `handleResult` schließt nur den Aufruf des richtigen Agenten, höchstens einmal, auch bei doppelter Zustellung |
| `FuzzHandleInventory` | `internal/remote` | Fähigkeiten merken, wartende Aufrufe scheitern lassen, Rollback-Befund, `boundedInventory`: kein Panic, Felder begrenzt |
| `FuzzHandleTTYFrames` | `internal/remote` | `handleTTY`: Puffer bleibt begrenzt, ein fremder Agent berührt die Sitzung nicht |
| `FuzzTerminalFrames` | `internal/agent` | Rahmen von LCM an die Konsolen (Schein-Shell): höchstens zwei Sitzungen, Nummern lückenlos, nach `closeAll` nichts offen |
| `FuzzJobStoreFiles` | `internal/agent` | beliebige `meta.json`, `pid`, `exit`, `result.json`, `acked`: Lesen, Aufräumen, `Known`, `Pending`, `Result` ohne Panic; `meta.json` läuft rund |
| `FuzzSafeID` | `internal/agent` | immer ein einzelner Verzeichnisname, stabil, `create` und `ack` bleiben in der Ablage |
| `FuzzFitOutput` | `internal/agent` | das serialisierte Ergebnis ist nie größer als das Limit, auch bei Steuerzeichen und ungültigem UTF-8 |
| `FuzzCheckPeer` | `internal/agent` | beliebige Zertifikatsketten und Pins: kein Panic, ohne Pins nie angenommen, bei Fehler nichts gelernt |

Die Startkorpora sind echte Nachrichten: die Inventare der Agenten 2.0.0, 2.1.0, 2.2.0, 2.3.0
(Byte für Byte aus `internal/remote/compat_test.go`) und der aktuelle Stand mit allen Feldern,
dazu Kommandos, Ergebnisse, Lebenszeichen und Konsolen-Rahmen, wie der Hub und der Agent sie
senden, und Randfälle (Pfad in der ID, Nummer 2^64−1, leere Felder).

Kommandos selbst führt kein Fuzz-Ziel aus; sie sind eine Shell-Zeile, getestet wird nur ihre
Dekodierung.

Lauf je Ziel, 60 Sekunden, 24.11.2026, macOS (Go 1.27.4), `-fuzzminimizetime 3s` bei den
langsamen Zielen:

```
go test ./internal/remote/wire -run '^$' -fuzz '^FuzzDecodeResult$' -fuzztime 60s
```

| Ziel | Ausführungen | Ergebnis |
|------|--------------|----------|
| `FuzzDecodeCommand` | 1,56 Mio. | ohne Fund |
| `FuzzDecodeResult` | 1,46 Mio. | ohne Fund |
| `FuzzDecodeInventory` | 1,30 Mio. | ohne Fund |
| `FuzzDecodeTTYFrame` | 1,16 Mio. | ohne Fund |
| `FuzzSequencer` | 67 Tsd. | ohne Fund (die Grenze `maxHeldBytes` ist der Fund aus der Durchsicht, S-13) |
| `FuzzParseToken` | 1,68 Mio. | ohne Fund |
| `FuzzAgentIDFromTopic` | 4,18 Mio. | ohne Fund |
| `FuzzHandleResult` | 177 Tsd. | ohne Fund |
| `FuzzHandleInventory` | 348 Tsd. | ohne Fund |
| `FuzzHandleTTYFrames` | 296 Tsd. | ohne Fund |
| `FuzzTerminalFrames` | 1,21 Mio. | ohne Fund |
| `FuzzJobStoreFiles` | 17 Tsd. | ohne Fund (langsam: je Ausführung ein Verzeichnis mit fünf Dateien) |
| `FuzzSafeID` | 31 Tsd. | Fund beim ersten Lauf der Startwerte (siehe unten), der 60-s-Lauf danach ohne Fund |
| `FuzzFitOutput` | 337 Tsd. | erster Lauf mit Fund (siehe unten), der Lauf nach der Behebung ohne Fund |
| `FuzzCheckPeer` | 131 Tsd. | Absturz von Go unter macOS (siehe unten); unter Linux ohne Fund |

Funde:

1. **`FuzzSafeID`**: Die ID `acked` (Seed) ergab einen Verzeichnisnamen, der mit der Datei der
   bestätigten IDs zusammenstößt (J-01). Behoben, Test `TestSafeIDMeidetDieListeDerBestaetigtenIDs`.
2. **`FuzzFitOutput`**: Gekürzte Ergebnisse lagen um 17 Bytes über dem Limit, weil das Feld
   `truncated` erst nach der Suche gesetzt wurde und nicht mitgemessen war. Im Betrieb
   harmlos (das Limit liegt 64 KiB unter der Paketgrenze), aber das Versprechen „passt immer“
   galt nicht. Behoben (`internal/agent/output.go`), der Fund steht als Regressionsfall in
   `internal/agent/testdata/fuzz/FuzzFitOutput/` und als `TestFitOutputZaehltTruncatedMit`.
3. **`FuzzCheckPeer`**: Unter macOS stürzt `crypto/x509` (Go 1.27.4) mit `SIGILL` ab, wenn der
   Name für die Systemprüfung ungültiges UTF-8 oder ein Leerzeichen enthält
   (`SecPolicyCreateSSL`, `root_darwin.go:40`). Ein Fehler der Standardbibliothek unter macOS,
   nicht im Agenten; der Agent läuft nur unter Linux und nimmt dort die eigene Prüfung. Das
   Ziel überspringt diese Namen unter macOS; der Lauf unter Linux (Docker `golang:1.27-bookworm`)
   deckt sie ab. Der Absturzfall steht deshalb nicht im Repository (er ließe `go test` auf einem
   Mac abstürzen).
4. Alle anderen Ziele ohne Fund. Der Zeiger „0/sec“ in der Ausgabe von `go test -fuzz` für
   Minuten gehört zur Minimierung neuer Funde (Vorgabe 60 s je Fund), nicht zu einem Hänger.

Lauf unter Linux: `go test ./internal/agent` samt der Linux-Teile (Scope, `/proc`, Löcher in
Dateien) und `FuzzCheckPeer` 60 Sekunden: `go test ./internal/agent` grün (82 s); `FuzzCheckPeer` 51 Tsd. Ausführungen mit allen Namen, ohne Fund.


## Offen und zu entscheiden

Entschieden und gebaut am 24.11.2026: S-02, S-04, S-07, S-08. Es bleibt so (Entscheidung
Tony): S-01 und S-15. Offen mit Freigabe von Tony:

1. S-05 Einmal-Token mit Tausch gegen ein Betriebs-Secret, Token nicht nur als Argument
   (eigenes Paket, `doc/plan-2026-11-24-einmal-token.md`).

Ohne Eingriff in die Anmeldung, aber Verhalten: S-06, S-09 bis S-12, J-03, J-04, J-06,
J-07 (siehe Tabellen).
