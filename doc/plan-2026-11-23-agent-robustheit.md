# Plan: LCM-Agent robust, kontrolliert aktualisierbar, abwärtskompatibel

Stand 23.11.2026. Status je Paket: offen · in Arbeit · zu prüfen · fertig.

## Ziel

Der Agent ist oft der einzige Zugang zu einem Server. Fällt er aus, ist der Server
verloren. Daraus folgen vier Anforderungen:

1. **Er kommt immer wieder.** Nach jedem Verbindungsabbruch, Absturz oder Hänger
   verbindet er sich ohne Eingriff neu. Ein Neustart des Dienstes von Hand ist nie nötig.
2. **Er aktualisiert sich nur gezielt.** Ein normales `apt upgrade` lässt ihn in Ruhe.
   Er wird nie neuer als der LCM-Server.
3. **Ältere Agenten funktionieren weiter.** Der Server erkennt, was ein Agent kann.
   Was der Agent nicht kann, bietet LCM nicht an, und es schlägt „Agent aktualisieren“ vor.
4. **Es ist bewiesen.** Fehlerinjektion, Langlauf und Distro-Matrix belegen die
   Robustheit, statt sie anzunehmen.

## Ausgangslage (Befunde aus dem Code)

Verbindung ist MQTT über wss (paho 1.5.1 → eingebetteter mochi 2.7.9). Der Agent
wählt sich ein. Ein Kommando läuft zugleich (`execMu`).

| # | Befund | Fundstelle | Folge |
|---|---|---|---|
| B1 | `Subscribe` ohne Timeout und ohne Fehlerprüfung, danach trotzdem `online` | `internal/agent/client.go:239-252` | Server hält Agent für bereit, Kommandos verpuffen (CleanSession). Passt zu „verbunden, reagiert aber nicht“ |
| B2 | Kein Watchdog, kein `sd_notify`, keine Lebendprüfung | `packaging/lcm-agent.service`, `client.go:111` (`select {}`) | Hängt paho intern, merkt es niemand; systemd startet nie neu |
| B3 | `handleCommand` und `tty.handle` laufen ohne `recover` im paho-Callback | `client.go` | Ein Panic beendet den Prozess, während apt läuft |
| B4 | `KillMode=control-group`: Stirbt der Agent, reißt systemd apt/dpkg mit | `packaging/lcm-agent.service:19` | Halb konfigurierte Pakete, dpkg-Lock-Reste |
| B5 | Stille-Notbremse schickt sofort `SIGKILL`, kein `SIGTERM` | `internal/agent/runner.go:185` | wie B4 |
| B6 | Verbindungsabbruch lässt alle laufenden Aufrufe sofort scheitern, verspätete Ergebnisse werden verworfen | `internal/remote/hub.go:316-345`, `:426-460` | Job gilt als gescheitert, apt läuft weiter, Folgejob hängt an `execMu` oder am dpkg-Lock. Passt zu „blockiert weitere Updates“ |
| B7 | Wartende Kommandos senden kein Lebenszeichen | `runner.go:95` | Server-Stille-Uhr (45 min) läuft ab, obwohl nur gewartet wird |
| B8 | Ergebnis-Wiederholung bis 2 h, blockiert solange den Selbstwechsel | `client.go:306-320`, `switch.go` | Agent bleibt lange auf alter Version, QoS-1-Stau im Speicher |
| B9 | `lcm-agent` kommt mit jedem `apt upgrade` mit, kein Hold/Pin | `subscription_apt.go:60-62`, Paketskripte | Unkontrollierte Updates, auch durch unattended-upgrades und needrestart |
| B10 | postinst macht `try-restart`, wenn nicht unter dem Agenten | `packaging/scripts/agent-postinstall.sh` | Update von außen tötet ein laufendes LCM-Kommando |
| B11 | Kein Rollback: defektes Binary → Neustartschleife alle 10 s | `switch.go`, Unit | Server dauerhaft unerreichbar |
| B12 | Keine Protokollversion, keine Min-/Max-Prüfung, Verträglichkeit nur über Capabilities (`tty`, `tty-off`) | `internal/remote/wire/wire.go:113-133`, `hub.go:370` | Server kann nicht sagen, was ein alter Agent nicht kann |
| B13 | Backoff ohne Jitter | `client.go:145-157` | Nach Server-Neustart verbinden sich alle Agenten im Gleichschritt |
| B14 | `Makefile:120-121` baut ohne `CGO_ENABLED=0` (CI schon) | `Makefile` | Lokal gebautes Binary womöglich glibc-gebunden |
| B16 | WS-Upgrade auf 10 je Minute und IP begrenzt | `internal/remote/ws.go` (`WSHandler`) | Ein flatternder Agent oder viele Agenten hinter einem NAT sperren sich selbst aus |
| B17 | Panic in der Kommando-Goroutine wird zwar abgefangen, aber es geht kein Ergebnis raus | `runner.go` über `safego.Go` | Server wartet bis zur Stille-Frist |
| B15 | Kein Agent-Test auf echten Distros, keine Fehlerinjektion, kein Langlauf | `.gitlab-ci.yml`, `internal/remote/integration_test.go` | Robustheit ist unbewiesen |

B16 gebaut, zu prüfen (`internal/remote/ws.go`, `hub.go`): Upgrade-Limit je Minute und IP jetzt 600 für bekannte Agent-IPs (nach erfolgreichem MQTT-CONNECT 24 h im Speicher gemerkt, höchstens 50 000), 120 für unbekannte IPs in den ersten 10 Minuten nach dem Start, sonst weiter 10. Tests in `internal/remote/ws_limit_test.go`.

Gut ist schon: Selbstwechsel per `exec` statt postinst-Neustart (49dd4bf9),
Ausgabelimit (e12c1125), Doppel-IDs und Abbruch (e247a06a), apt-Lock-Vorspann
(`aptlock.go`), Integrationstests gegen den echten Broker.

## Paket A: Analyse und Prüfstand (zuerst)

Erst reproduzieren, dann bauen. Jedes Fehlerbild aus dem Feld wird ein Test, der vor
dem Fix rot ist.

**A1 Fehlerkatalog aus dem Feld** (offen): Journale betroffener Agenten und des
Servers sammeln (lcm-test, Distro-Testbett, Tester-Meldungen). Jedes Symptom einer
Hypothese zuordnen:
- H1 „verbunden, reagiert nicht“ → B1, B2
- H2 „nach Fehler im Job weg, erst Neustart hilft“ → B3, B4, paho-Hänger
- H3 „keine Updates mehr möglich“ → B6, B7, dpkg-Lock-Rest nach B4/B5
- H4 „nach Update weg“ → B10, B11

**A2 Fehlerinjektion in Go** (zu prüfen), neues Paket `internal/remote/chaos_test.go`
mit einem TCP-Proxy zwischen Agent und Broker, der kappen, verzögern, Pakete
verschlucken und halb offen hängen lassen kann. Szenarien:
- Netz weg 5 s / 2 min / 30 min, dann zurück
- halb offene Verbindung (Proxy liest, leitet nicht weiter)
- Broker-Neustart während eines Kommandos, während des Ergebnis-Sendens
- Subscribe wird abgelehnt oder beantwortet nie (B1)
- Panic im Kommando-Handler (B3)
- 1000 zufällige Abbrüche in Folge
Erwartet jedes Mal: Agent wieder bereit binnen Backoff-Obergrenze plus Jitter,
Job-Ergebnis kommt an oder ist eindeutig als verloren markiert, keine Goroutine bleibt
übrig (`goleak`).

Ergebnis 23.11. (`internal/remote/chaos_test.go`, `faultproxy_test.go`,
`internal/agent/chaos_test.go`; überspringen sich unter `-short`):
- Grün: Netz weg 5 s und 2 min, halb offen in beide Richtungen (erkannt nach 3–5 s bei
  Ping 2 s), Server-Neustart während Kommando und Ergebnis, Subscribe ohne Antwort,
  200 zufällige Abbrüche ohne Hänger und ohne Goroutinen-Wachstum.
- Rot und damit belegt: B1 (Agent meldet sich trotz abgelehntem Abonnement bereit und
  abonniert danach **nie** neu, bleibt also taub, bis der Dienst neu startet), B3 (Panic
  im Callback beendet den Prozess), B6 (Ergebnis nach Abbruch verloren, zwei Tests), B7
  (wartendes Kommando 33 s ohne Lebenszeichen), B13 (6 Agenten im Gleichtakt, Streuung
  unter 1 ms).
- Neu gefunden: B16, B17.
- Test-Nähte `SetTimingForTest`/`DisconnectForTest` waren in `internal/agent/timing.go`; Paket B
  hat sie durch `agent.Timing`/`WithTiming` und `Stop` ersetzt.
- Nicht abgedeckt: Netz weg 30 min, 1000 statt 200 Abbrüche, `-race`.

**A3 Prozess-Tests auf echten Systemen** (fertig): pve-test per SSH, CT 320 (Debian 11,
systemd 247), 321 (Debian 12, LCM 2.4.0 aus dem Branch), 322 (Debian 13, systemd 257),
jeweils mit Snapshot `vor-agent-robust`. Agent-Pakete 2.4.0 bis 2.4.4 aus dem Branch
(2.4.2 und 2.4.4 absichtlich kaputt), 2.5.0 als höhere Reihe, lokale Paketquelle, Testpaket
`a3-langsam` mit 150 s postinst. Ergebnisse 24.11.:

| Fall | Ergebnis |
| --- | --- |
| Pin: `apt upgrade`/`dist-upgrade` mit 2.4.1 und 2.5.0 in der Quelle | Agent bleibt 2.4.0 (über 100 andere Pakete samt systemd aktualisiert) |
| Zielwahl LCM 2.4.0 | 2.4.1 (Bugfix), nie 2.5.0 |
| `kill -9` mitten im postinst (Deb 13, Deb 11) | apt läuft im Scope weiter, Agent nach 10 s zurück, Job übernommen, Ergebnis kommt an, dpkg sauber |
| `systemctl stop` mitten im postinst (Deb 11) | Kommando läuft weiter |
| Agent eingefroren (SIGSTOP) | systemd-Watchdog beendet ihn nach 90 s, Neustart, verbunden |
| Update per Knopf 2.4.0 → 2.4.1 | Wechsel per exec ohne Neustart, Pin nachgezogen, Job erfolgreich |
| Kaputtes Update | Rückfall nach 3 Fehlstarts in 30 s, Befund in LCM, Paket an die laufende Version angeglichen |
| Gruppenregel gestaffelt | erster Server voraus, zurückgerollter Server übersprungen, „Erneut versuchen“ löscht den Befund |
| Abbruch per LCM mitten im postinst | Kommando beendet, nächster Paketlauf repariert dpkg |
| OOM-Killer trifft den Agent mitten im postinst (Deb 11) | systemd startet ihn nach 10 s neu, apt läuft weiter, Job erfolgreich, dpkg sauber |
| nft sperrt 4 min den Weg zu LCM mitten im postinst (Deb 13) | Ergebnis bleibt in der Ablage, 31 s nach Freigabe verbunden und zugestellt, Job erfolgreich |
| Server-Neustart mitten im postinst (Deb 13) | Job endet mit Exit 143 und Fehlermeldung, der nächste Paketlauf repariert dpkg |
| unattended-upgrades mit allen Quellen erlaubt (Deb 13) | aktualisiert das Testpaket, lässt den Agent trotz 2.4.4 in der Quelle auf 2.4.3 |
| needrestart im Modus „a“, Agent-Binary ersetzt (Deb 13) | meldet `lcm-agent.service`, startet ihn nicht neu (gleiche PID); Paketlauf per LCM mit needrestart-Hook ohne Hänger |
| Echter Agent 2.3.0 aus der Paketquelle → 2.4.3 per Knopf (Deb 11) | 2.3.0 wechselt selbst per exec, kein Dienst-Neustart, Pin 2.4.3, dpkg sauber, Job erfolgreich |
| LCM-Neustart mitten im Agent-Job (B18, Deb 11) | Job sofort „UNTERBROCHEN“, nach dem postinst „NACHGETRAGEN“ mit Exit 0 und Ausgabe |

Dabei gefunden und behoben (zu prüfen):
- Agenten melden „2.3.0 (Build 412, …)“; LCM las das als unbekannte Version. Folge: Regel
  übersprang alle Agenten, der Update-Job wartete 45 min ins Leere. `version.Core`.
- Ein Abbruch gleich nach dem Start ging verloren (trap vor dem Start des Kommandos);
  `TestRunnerCancel` war seit Paket C rot. Wrapper startet das Kommando zuerst, Eingabe
  über fd 3 (dash setzt sonst /dev/null).
- Nach einem Rückfall führte dpkg die kaputte Version samt Pin; „Erneut versuchen“ hielt den
  Agent für aktuell, und der Befund verschwand beim nächsten Connect. Jetzt: Paket wird
  angeglichen, Befund bleibt bis zu einer anderen Version, der Wächter nennt die
  Paketversion, wenn das Programm seine nicht nennen kann.
- busybox-`flock` kennt kein `-w`: Test überspringt dort (CI-Image Alpine).

**B18 LCM-Neustart verwirft Agent-Ergebnisse** (zu prüfen): Startete LCM während eines
Agent-Jobs neu (etwa beim LCM-Update), blieb der Job „Vom Dienst-Neustart unterbrochen“,
und LCM verwarf das spätere Ergebnis des Agents als „late result“. Gebaut:
- `domain.Job.AgentRequestID` hält das Kommando, auf dessen Ergebnis der Job wartet. Nur
  eigene Repository-Methoden schreiben es, `Update` lässt die Spalte aus.
- Der Hub meldet Kommandos über `remote.JobTracker` (`CommandSent`, `CommandDone`,
  `LateResult`, umgesetzt in `services/job_agent.go`). Die Job-ID kommt per `BindJob` vom
  `SSHRecorder` in die `agentConn`.
- Beim Neustart steht am Job „UNTERBROCHEN“, die Kennung bleibt. Das späte Ergebnis setzt
  Status, Exit-Code und Ausgabe („NACHGETRAGEN“), nur vom Agent des Job-Servers und nur bei
  fehlgeschlagenem Job, jedes Ergebnis einmal. Dasselbe greift, wenn LCM nach langer Stille
  aufgegeben hat (`ErrResultUnknown`). Den Bestand holt der nächste Scan nach.
- Tests: `hub_job_tracker_test.go`, `job_agent_test.go`; auf Debian 11 echt geprüft (A3).

**A4 Langlauf** (offen): 72 h mit Jobs im Minutentakt und zufälligen Netzabbrüchen.
Gemessen: RSS, Goroutinen, offene Dateien, Reconnect-Dauer. Abnahme: kein Wachstum,
kein Hänger.

**A5 Distro-Matrix** (offen): Debian 11/12/13, Ubuntu 22.04/24.04/26.04, dazu Alpine,
Rocky/Alma, Arch, je amd64 und arm64. Geprüft werden Installation, Einrichtung,
Paketskripte unter `dash`/`busybox sh`, systemd-Version (Debian 11: 247), OpenRC,
`/proc/locks`, Upgrade und Deinstallation. Debian-spezifische Fehler gibt es im
Changelog bisher keine, aber auch keinen Test.

## Paket B: Verbindung, die sich selbst heilt

**B-1 Bereitschaft erst nach erfolgreichem Subscribe** (zu prüfen): `WaitTimeout`
mit Fehlerprüfung. Scheitert es, Verbindung trennen und neu aufbauen. `online`
erst danach (behebt B1).

**B-2 Eigene Lebendprüfung** (zu prüfen): Der Server schickt regelmäßig einen Ping
auf dem Kommando-Topic, der Agent antwortet. Bleibt die Antwort n-mal aus, baut der
Agent die Verbindung selbst neu auf. So zählt die ganze Strecke einschließlich
Subscription, nicht nur TCP.

**B-3 systemd-Watchdog** (zu prüfen): Der Agent richtet den Wächter zur Laufzeit
selbst ein (`sd_notify WATCHDOG_USEC=90s`, ab systemd 236) und füttert ihn
(`WATCHDOG=1`) nur, solange die Hauptschleife und die Lebendprüfung laufen. Die Unit
bleibt `Type=simple` mit `NotifyAccess=main`, **ohne** `WatchdogSec=`: Beim
Paket-Update lädt postinst die Units neu, während der alte, nicht fütternde Agent
noch läuft, womöglich mitten im apt-Lauf. Ein Wächter in der Unit würde ihn samt apt
töten. Vor dem exec auf ein anderes Binary hebt der Agent den Wächter auf
(`WATCHDOG_USEC=0`), damit ein älteres Binary (Rückfall D-4) nicht getötet wird. Auf echtem
systemd (Debian 11 mit 247 bis 13) in A3 nachweisen: Wächter greift bei Hänger,
Update von alt auf neu mitten in apt überlebt. `StartLimitIntervalSec=0`, damit systemd nie aufgibt. OpenRC bekommt
`supervise-daemon` mit Respawn (behebt B2).

**B-4 Panic-Schutz** (zu prüfen): `recover` um alle paho-Callbacks; ein Panic wird
gemeldet und kostet nur das eine Kommando (behebt B3).

**B-5 Backoff mit Jitter** (zu prüfen), Obergrenze bleibt 2 min (behebt B13).

Umsetzung (`internal/agent/connection.go`, `timing.go`, `notify.go`, `internal/remote/ping.go`):
eine eigene Verbindungsschleife statt pahos Auto-Reconnect, mit frischem paho-Client je
Aufbau. Bereit erst nach bestätigten Abonnements (auch die Ablehnung im SUBACK-Code wird
geprüft, pahos Token-Fehler bleibt dabei leer); sonst wird die Verbindung verworfen und neu
aufgebaut. Backoff 0,5 s verdoppelnd bis 2 min, ±25 % Streuung, Obergrenze gilt nach der
Streuung. Ping: Capability `ping`, Server pingt alle 30 s, Agent baut nach drei
ausbleibenden Pings neu auf (nur, wenn LCM überhaupt schon einmal gepingt hat). Watchdog: wie
unter B-3, gefüttert nur, solange die Verbindungsschleife vorankommt.
`recover` um alle paho-Callbacks und um jedes Terminal-Rahmen; ein Panic im Kommando liefert
ein Fehlerergebnis (B17). Zeiten über `agent.Timing`/`Client.WithTiming`, `Client.Stop` beendet
Run (SIGTERM trennt sauber).

## Paket C: Jobs überleben Abbrüche

**C-1 Kommandos außerhalb der Control-Group des Agenten** (zu prüfen): Kommandos laufen
über `systemd-run --scope` (OpenRC: eigene Session plus Prozessgruppe). Ein Absturz,
Neustart oder `systemctl stop` des Agenten hält laufende Kommandos nicht mehr an
(Entscheidung Tony 23.11.). Abbrechen geht nur noch ausdrücklich über LCM; der Agent
findet den Job nach einem Neustart über die Job-Ablage (C-2) wieder.

**C-2 Lokale Job-Ablage** (zu prüfen): Der Agent führt laufende und fertige Jobs in
`/var/lib/lcm-agent/jobs/` (ID, Status, Exit, gekürzte Ausgabe). Nach Reconnect oder
Neustart meldet er sie. Damit kommen Ergebnisse auch nach einem Agent-Neustart an.

**C-3 Kein Abbruch durch Server-Timeouts** (zu prüfen): Der Server bricht Jobs nicht mehr
nach Zeit ab und erklärt sie bei Trennung nicht für gescheitert. Solange der Agent auf
Pings antwortet (B-2), gilt der Job als laufend. Ist der Agent getrennt, steht der Job
auf „Agent nicht erreichbar, Ergebnis folgt“ und wird nach Reconnect aus der
Job-Ablage (C-2) aufgelöst (behebt B6). Die heutige Stille-Notbremse (Server 45 min,
Agent 60 min, `hub.go:45`, `runner.go:20`) entfällt.

**C-4 Der Agent beurteilt, ob ein Kommando noch läuft** (zu prüfen): Statt Zeitgrenzen
meldet der Agent im Lebenszeichen, was er sieht: Prozessbaum lebt, CPU- und I/O-Zeit
seit dem letzten Zeichen, letzte Ausgabe, wartet auf Lock (dpkg, `/proc/locks`). Die
Oberfläche zeigt „läuft, seit X ohne Ausgabe“ und erst bei echtem Stillstand einen
Hinweis; anhalten tut nur der Anwender. Ausdrücklicher Abbruch: erst `SIGTERM` an den
Scope, nach Frist `SIGKILL`. Nach jedem Abbruch einer apt/dpkg-Kette prüft der nächste
Job `dpkg --audit` und führt bei Bedarf `dpkg --configure -a` aus (behebt B5,
entschärft H3).

**C-5 Lebenszeichen auch beim Warten** (zu prüfen): Ein Kommando, das auf `execMu`
wartet, meldet „wartet auf vorheriges Kommando“ (behebt B7).

**C-6 Ergebnis-Wiederholung begrenzen** (zu prüfen): Mit C-2 und C-3 genügt ein
einmaliges Senden nach jedem Reconnect statt einer Schleife über 2 h (behebt B8).

Umsetzung 23.11. (Capability `jobs-v2`, nur additive Wire-Felder, Kopf von
`internal/remote/wire/wire.go`; Alt-Agenten behalten das bisherige Protokoll):
- **Start (C-1)**: `internal/agent/jobrun.go`. Wrapper-Skript (`sh -c`), das seine PID,
  die Ausgabe (`>>out`, keine Pipe zum Agenten, also kein SIGPIPE) und zuletzt den Exit-Code
  (`exit`, über Zwischendatei) in die Job-Ablage schreibt. Unter systemd in einem Scope
  (`systemd-run --scope --collect --quiet --unit=lcm-job-<id>`, einmalige Probe beim ersten
  Kommando), sonst `Setsid`. stdin über eine Pipe (Passwörter berühren die Platte nie; ein Leser
  ohne Schreiber sieht nur EOF). `KillMode=control-group` bleibt: Die Kommandos gehören
  wegen des Scopes nicht mehr zur Group.
- **Ablage (C-2)**: `internal/agent/jobs.go`, `JobsDir=/var/lib/lcm-agent/jobs` (0700). Je Job
  `meta.json`, `cmd.sh`, `pid`, `out`, `exit`, nach dem Ende nur `result.json`. Beim Start des
  Agenten nimmt `Runner.Adopt` laufende Jobs wieder auf (PID aus der Ablage, Startzeit gegen
  PID-Wiederverwendung) und hält den Platz für Kommandos besetzt; fertige, unbestätigte
  Ergebnisse stellt `Client.resendResults` nach jedem Verbindungsaufbau zu. Aufräumen: höchstens
  32 unbestätigte Ergebnisse, höchstens 7 Tage, Müll ohne `meta.json` nach 1 h; laufende nie.
  Die Ausgabedatei bleibt klein: `trim` stanzt die Mitte frei (fallocate PUNCH_HOLE), gelesen
  werden Anfang und Ende wie bisher (je 2 MiB).
- **Bestätigung (C-6)**: Das PUBACK des Brokers gilt als Zustellung; ein neuer Nachrichtentyp
  wäre für ältere LCM unbekannt. Ein Ergebnis nach der Bestätigung hätte nach einem LCM-Neustart
  ohnehin keinen Aufrufer mehr. Gesendet wird einmal je Verbindung (`deliver`), die
  2-h-Schleife entfällt; der Selbstwechsel wartet nicht mehr auf die Zustellung.
- **Selbstwechsel**: wartet weiterhin auf laufende Jobs (bewusst konservativ; die Wiederaufnahme
  nach `exec` ist derselbe Pfad wie nach einem Absturz und getestet, ein Wechsel mitten im Job
  brächte aber nur Stunden Vorsprung).
- **Server (C-3)**: `hub.go`. Aufrufe an Agenten mit `jobs-v2` überstehen die Trennung (`pendingReq.durable`),
  die Conn bleibt, ein neuer Aufruf bei getrenntem Agent scheitert sofort. Kein Idle-Kill mehr
  (`WithIdleTimeout` entfernt); Obergrenze ohne Lebenszeichen `defaultMaxSilence` = 24 h
  (`Hub.WithMaxSilence`), danach `ErrResultUnknown`, ohne Cancel. Verspätete Ergebnisse werden
  protokolliert. Ein Abbruch (`Conn.Close`) bei getrenntem Agent wird nach der Rückkehr
  nachgereicht. Widerruf des Tokens und Ausschalten des Agent-Zugangs lassen wartende Aufrufe
  scheitern. Alt-Agenten: Trennung lässt den Aufruf wie bisher scheitern; seit 24.11.
  (Entscheidung 13) haben sie wieder die Stille-Frist des Hubs (`WithIdleTimeout`, Vorgabe
  45 min) und ihre eigene Notbremse (`IdleTimeoutSec`), Agenten mit `jobs-v2` keine.
- **Job-Watchdog**: Für Aufrufe an `jobs-v2`-Agenten feuert `OnActivity` im Takt von 30 s auch
  während einer Trennung; der Watchdog der Jobs (`JobIdleMinutes`) bricht sie also nicht ab. Für
  SSH und Alt-Agenten gilt er unverändert. Zu klären: ob die Einstellung dort entfallen soll.
- **Lebenszeichen (C-4, C-5)**: `Result.Vitals` (`alive`, `procs`, `cpu_ms`, `io_bytes`,
  `silent_sec`, `waiting`), `internal/agent/vitals.go`, `procfs_linux.go`. Abfragbar über
  `Hub.CommandStatus(agentID)`. Abbruch: SIGTERM an die Prozessgruppe, nach 30 s SIGKILL.
  Wartende Kommandos melden `waiting: previous-command`.
- **dpkg-Vorspann**: `lcm_dpkg_repair` in `internal/core/services/aptlock.go`, aufgerufen von `lcm_apt`.
- **Tests**: `internal/agent/jobs_test.go`, `jobs_linux_test.go` (Wrapper und Exit-Datei,
  Job überlebt Agent-Neustart, Wiederaufnahme, Aufräumen, SIGTERM→SIGKILL, Vitals, Platz der
  Ausgabedatei, Scope-Aufruf mit Ersatz-`systemd-run`, Zustellung nach Reconnect),
  `internal/remote/hub_jobs_test.go` (Warten über Trennung, Alt-Agent-Pfad samt Idle-Wert,
  Obergrenze, Nachreichen eines Abbruchs), `chaos_test.go` (`TestChaosAgentRestartDuringCommand`,
  B6 grün), `aptlock_repair_test.go`. Linux-Teile laufen in `docker run golang:1.27`.
  Bekannt, unabhängig von C: `-race` meldet in `TestSetAcceptingTrenntUndWeistAb` und
  `TestCloseDisconnectsAgents` ein Rennen innerhalb von mochi beim Schließen (auch ohne diese Änderung).
- Für die Proxmox-Probe (A3) offen: echtes `systemd-run --scope` (Scope entsteht, überlebt
  `systemctl stop`/`kill -9` des Agenten), `/proc/locks` mit echtem dpkg, Debian 11 (systemd 247;
  die Optionen `--scope --collect --quiet --unit --description` sind dort in `systemd-run --help`
  geprüft), OpenRC/Alpine (stirbt `supervise-daemon` samt Sitzung-Kindern beim Stopp, falls cgroup-
  Aufräumen aktiv ist?), Deinstallation mit laufendem Job.

**Review-Korrekturen 23.11.** (Durchsicht von B und C, alle Punkte gegen den Code geprüft):
- **Hängende Aufrufe**: Der Agent sagt ein Kommando beim Selbstwechsel mit einem Fehlerergebnis ab
  (`Client.refuse`), statt zu schweigen. LCM lässt wartende Aufrufe sofort scheitern, wenn der
  Agent nach einem Rollback kein `jobs-v2` mehr meldet (`Hub.failUnreachable`) oder wenn sein
  Inventar die bekannten Kommandos nennt (`Inventory.KnownJobs`, additiv; `nil` = meldet nicht) und ein
  vor dieser Verbindung abgeschickter Aufruf fehlt. Bei VERBUNDENEM Agent endet ein Aufruf ohne
  Lebenszeichen nach 5 × `ProgressInterval` mit `ErrCommandUnknown` (`Hub.WithJobSilence`); nur
  die Wartezeit bei getrenntem Agent behält die 24-h-Grenze.
- **Doppel und Neustart**: `Runner.Adopt` belegt `seen` mit allen Jobs der Ablage und einer
  Liste der letzten 1024 bestätigten IDs (`acked` in der Ablage); eine erneut zugestellte ID mit
  fertigem Job sendet das echte Ergebnis noch einmal, nie einen Fehler.
- **Scope**: Die Probe von `systemd-run` läuft vor dem Start (nicht unter `r.mu`), wird nach einem
  Fehlschlag nach 60 s wiederholt und jeder Lauf ohne Scope trotz systemd steht im Protokoll.
- **Prozess-Erkennung**: Startzeit wird bei der Wiederaufnahme nachgetragen, die Boot-ID steht in
  `meta.json` (Job einer früheren Boot-Sitzung = beendet, „durch einen Neustart des Servers
  beendet“), `noPIDGrace` 60 s. Abbruch prüft PID und Startzeit, nutzt mit Scope
  `systemctl kill --signal=…`, und das SIGKILL nach der Frist trifft alles, was noch lebt. Beendete,
  wieder aufgenommene Wrapper werden mit `Wait4` eingesammelt (nur bei passender Startzeit).
  `Disconnect` vor dem `exec` hat eine Frist von 1 s.
- **Wächter**: Er prüft die Verbindungsschleife. Den Nachrichtenweg deckt die Lebendprüfung
  (Pings, `liveness.silent`) ab; ein eigener Rundlauf scheitert an der ACL (der Agent darf `cmd`
  nicht schreiben). Bleibt offen: Gegenüber einem LCM ohne Pings merkt niemand einen hängenden
  Handler. Beschrieben an `heartbeat` in `notify.go`.
- **dpkg-Vorspann**: nur vor `lcm_apt`-Aufrufen, die nicht lesend sind (`lcm_apt_mutates` kennt die lesenden Verben und Simulationen), `dpkg --configure -a`
  mit `timeout` (30 min) und `--force-confold`, bei reinen reinstreq-Befunden nur Protokoll.
- **Ablage**: `create` setzt 0700 auch auf ein vorhandenes Verzeichnis.
- **Tests**: `internal/agent/review_test.go`, `jobs_linux_test.go` (unten), `internal/remote/hub_unreachable_test.go`,
  `aptlock_repair_test.go`.

## Paket D: Kontrolliertes Selbst-Update

**D-1 Agent vom normalen Update ausnehmen** (zu prüfen): Einrichtung und postinst
setzen `apt-mark hold lcm-agent`. Für rpm `versionlock`, für apk ein fester
Versionsstand in `/etc/apk/world`, für pacman `IgnorePkg`. Damit greifen auch
unattended-upgrades nicht. needrestart bekommt eine Ausnahme für `lcm-agent.service`
(behebt B9).

**D-2 Update nur gezielt, Version bestimmt der Server** (zu prüfen): Es gibt nur einen
Agenten, das Community-Paket (dazu Beta); Enterprise nutzt denselben (Entscheidung
Tony 23.11.). Deshalb legt der Server die Zielversion fest: höchste verfügbare Version
der eigenen Feature-Reihe samt Bugfixes (LCM 2.3.0 nimmt Agent 2.3.7, nie 2.4.0; ein
Beta-LCM genau bis zu sich, Entscheidung 24.11.), installiert als `apt-get install lcm-agent=<ver>` mit kurzem
Unhold/Hold (rpm/apk/pacman entsprechend). Ein Enterprise-Server, der hinter community
zurückliegt, bekommt so Agent-Fixes seiner Reihe, aber nie eine höhere Reihe. Die
Gruppenregel schickt in jedem Lauf den ersten Server voraus; nur wer unter seinem Stand
liegt, folgt (gestaffelt höchstens bis dahin). Nie abwärts.
Auslöser:
- Knopf „Agent aktualisieren“ je Server und für eine Auswahl.
- Regeln in den Gruppeneinstellungen: automatisch ja/nein, Zeitfenster (wie bei den
  Paket-Zeitplänen), gestaffelt (erst ein Server der Gruppe, dann der Rest, wenn der
  erste sich mit neuer Version zurückmeldet).

**D-2a Alte Agent-Versionen bleiben abrufbar** (zu prüfen): Geprüft am 23.11. auf
repo.techeve.de (RSM): `community`/`stable` führen `lcm-agent` 2.0.0, 2.1.0, 2.2.0,
2.2.1, 2.3.0, `beta` alle 2.x-Betas. Vor 2.0 fehlen Versionen (z. B. 1.39–1.47), es gibt
also keine zugesicherte Aufbewahrung. Offen:
- Aufbewahrung ab 2.0.0 in RSM festschreiben (nichts ab der Mindestversion löschen).
- `/enterprise/lcm` enthält keinen `lcm-agent`. Klären, welche Quelle die Clients eines
  Enterprise-Servers eingerichtet haben, und sicherstellen, dass der Update-Job die
  Community-Quelle nutzt.

**D-3 Update sicher ausführen** (zu prüfen): Das Paket-Update läuft über
`systemd-run` außerhalb des Agenten. postinst startet nie neu (B10). Der Agent
wechselt danach per `exec` wie heute.

**D-4 Rückfallebene** (zu prüfen): Vor dem Wechsel bleibt das alte Binary als
`lcm-agent.prev`. Verbindet sich das neue nicht binnen z. B. 5 min oder stirbt es
dreimal in Folge, startet ein kleiner Wächter (eigener systemd-Timer bzw.
`ExecStartPre`-Zähler) wieder das alte Binary und meldet „Update zurückgerollt“
(behebt B11).

**D-5 Offene Web-Konsolen** (zu prüfen): Ein Wechsel wartet auf offene Konsolen oder
kündigt sich in ihnen an.

Umsetzung 24.11. (D): postinst pinnt die installierte Version
(`/etc/apt/preferences.d/lcm-agent`, Priorität 1001; `apt-mark hold` ginge im
Paketskript nicht, dpkg hält die Sperre), needrestart-Ausnahme, kein Neustart im
postinst mehr. In debian:11/12 mit echten Paketen belegt: `upgrade`, `dist-upgrade`,
unattended-upgrades lassen den Agenten stehen, `install lcm-agent=<ver>` geht hoch und
runter, der Pin wandert mit, Entfernen räumt ihn weg. Rückfall über das Startskript
`/usr/lib/lcm-agent/lcm-agent-start` (zählt Starts, nach drei ungesunden das vorige Binary
aus `/var/lib/lcm-agent/update/lcm-agent.prev`), Meldung im Inventar (`rolled_back`,
`rolled_back_to`, `rolled_back_reason`), Fähigkeit `self-update-v2`. Konsolen werden vor
dem Wechsel gewarnt (bis 30 min). Server: Job `agent-update` wählt mit
`agentcaps.PickTarget` die höchste Version ≤ LCM (stabiles LCM nie Vorabversion), wartet
bis 45 min auf die neue Version, Rückfall wird Befund in der Status-Ampel. Knopf im
Server-Detail, Karte „Agent-Updates“ in der Gruppe (aus/automatisch, Zeitfenster,
gestaffelt, Sperre nach Rückfall bis „Fortsetzen“ oder neue LCM-Version). Nur apt; rpm,
apk und pacman melden „nicht unterstützt“, weil das Repo nur deb veröffentlicht. Clients
holen den Agenten immer aus der Community-Quelle (`setup.sh`), auch unter einem
Enterprise-LCM. Offen in A3: echtes systemd/OpenRC mit Startskript, Wechsel samt
Wächter, Ablauf des Rückfalls auf echtem Host.

## Paket E: Versionsvertrag und Abwärtskompatibilität

**E-1 Protokollversion** (zu prüfen): `Inventory` bekommt `ProtocolVersion` und eine
vollständige Fähigkeitsliste (z. B. `jobs-spool`, `scope`, `ping`, `tty`,
`self-update-v2`). Alte Agenten ohne Feld gelten als Protokoll 1.

**E-2 Regeln auf dem Server** (zu prüfen):
- Mindestversion des Agenten, die der Server noch bedient: **2.0.0** (Entscheidung
  Tony 23.11.). Darunter nur noch „Agent aktualisieren“.
- Jede Funktion, die eine Fähigkeit braucht, prüft sie. Fehlt sie, ist der Knopf aus
  und zeigt „Braucht Agent ≥ x.y, jetzt aktualisieren“ (verallgemeinert das heutige
  `agent_outdated` der Konsole).
- Agent neuer als Server: Warnung in der Server-Ansicht. Der Server bietet keine
  Funktionen an, die er selbst nicht kennt.

**E-3 Kompatibilitätstest in der CI** (zu prüfen): Agent-Binaries der Tags v2.0.0,
v2.1.0, v2.2.0, v2.3.0 bauen und gegen den aktuellen Hub laufen lassen
(Grundbefehl, Abbruch, Konsole, Ergebnis nach Reconnect). Ab dann wächst die Liste
mit jedem Release. Dazu der umgekehrte Fall: neuer Agent gegen älteren Server.

Umsetzung 24.11. (E-3): CI-Job `compat:agents` (Variable `COMPAT_AGENT_TAGS` =
`v2.0.0,v2.1.0,v2.2.0,v2.2.1,v2.3.0`; ein neuerer Final-Tag als v2.3.0 existiert noch nicht), Binaries je
Tag im CI-Cache `compat-agents`. `compat_agent_test.go` liegt jetzt im Paket `remote`, nutzt den
`chaosEnv`/`faultProxy` der Chaos-Tests und prüft je Tag Inventar, Befehl, Eingabe, Abbruch, „Ergebnis nach
Reconnect“ (Agent mit `jobs-v2`: Ergebnis kommt an; ohne: Aufruf scheitert sauber, Agent kommt wieder) und die
Konsole (nur bei Fähigkeit `tty`, Linux und root; also in der CI, nicht auf dem Mac). Kein bisheriger Tag hat
`jobs-v2`; der Zweig ist mit `HEAD` als Tag belegt (grün). Umgekehrt: `compat_server_test.go`
(`LCM_COMPAT_SERVER_TAGS`, nur auf Wunsch, nicht in der CI) baut `cmd/app` des Tags, startet ihn mit `--demo`,
legt einen Agent-Server an und verbindet den aktuellen Agent mit dem Token (Version im Inventar, ein Auftrag
bis zum Ende, bleibt über mehrere Ping-Fristen verbunden). Grün gegen v2.3.0. Offen: der Abbruch eines
laufenden Auftrags über die REST-Schnittstelle (die Aufträge des Servers rufen feste Befehle auf, die sich
nicht ersetzen lassen) und v2.4.x, sobald es den Tag gibt.

**E-4 Anzeige** (zu prüfen): Agent-Version, Mindestversion, „Update verfügbar“ in
Server-Liste und Server-Detail. Mit dem Wunsch nach einem Status je Server: eine Pille,
z. B. gelb „Agent veraltet“.

Umsetzung 23.11. (E-1, E-2, E-4; Gerüst für E-3):
- **Protokoll (E-1)**: `wire.ProtocolVersion = 2`, `Inventory.ProtocolVersion` (omitempty; fehlt es,
  gilt Protokoll 1). Der Agent meldet 2 und die volle Liste `tty|tty-off`, `ping`, `jobs-v2`. Das
  Netzformat bleibt additiv. Die Fähigkeiten sind unverändert benannt; sie bleiben die Entscheidung,
  die Protokollversion hebt nur, wer eine bestehende Nachricht nicht mehr additiv erweitern kann.
- **Eine Stelle (E-2)**: `internal/core/agentcaps`. `Agent{Version, Protocol, Capabilities}` aus dem
  Inventar (`FromInventory`) oder dem Gespeicherten (`FromStored`, `domain.Server.Agent()`);
  `Check(feature)` liefert `Support{Supported, Reason, NeedsVersion}`; `Assess(agent, serverVersion)`
  liefert den `Report` (Zustand `ok|outdated|unsupported|newer`, `UpdateRecommended`, `NeedsVersion`,
  Antwort je Funktion). Fähigkeit geht vor Version; die Version dient nur der Mindestprüfung
  (`MinAgentVersion = 2.0.0`), dem Vergleich mit LCM und dem Hinweis „Braucht Agent ≥ x.y“. Selbstbauten
  (`0.0.0-dev`, leer) gelten weder als zu alt noch als zu neu. Vergleich über `internal/version.Compare`
  (kennt `-` und `~`). Unter der Mindestversion antwortet `Check` für jede Funktion „nein“.
- **Funktionen**: Konsole (`tty`, `tty-off` = am Agent abgeschaltet; `terminalPossible`), Lebendprüfung
  (`ping`; `Hub.startPinging`), dauerhafte Jobs (`jobs-v2`; `Hub.rememberCapabilities`). Neue
  Funktionen tragen sich in die Tabelle `features` ein.
- **Speicher**: neue Spalte `servers.agent_protocol` (int, Vorgabe 0 = noch nie gemeldet) über
  AutoMigrate, es gibt nur SQLite. `agent_version` und `agent_capabilities` standen schon da. Die
  Einordnung (`agent_support`) wird nicht gespeichert, sondern bei Liste und Detail berechnet
  (`fillAgentState`), die Ampel rechnet sie frisch.
- **Eine Pille (E-4)**: Der Agent geht als Befund in die bestehende Ampel (`agentUnsupported` kritisch,
  also rot; `agentOutdated` und `agentNewer` Warnung, also gelb), nicht als zweite Anzeige. Das
  Popover der Pille nennt den Grund. Server-Detail: Band „Agent veraltet / zu alt / neuer als LCM“ mit
  ausgegrautem Knopf „Agent aktualisieren“ (Platzhalter für Paket D, `update_recommended` ist der
  Haken); unter der Mindestversion entfallen die Verwalten-Knöpfe der Leiste. Terminal-Knopf grau mit
  „Braucht Agent ≥ x.y – jetzt aktualisieren“. Liste: Blitz-Zeichen am Agent-Server mit Farbton und
  Blase (Version, Zustand).
- **Tests**: `agentcaps_test.go`, `domain/agent_insights_test.go`, `remote/compat_test.go` (Fake auf
  Protokollebene mit den echten Inventar-Bytes von 2.0.0 bis 2.3.0: Befehl, kein Ping, Stille-Wert,
  Mindestversion, Funktionsprüfung), `remote/compat_agent_test.go` (echte Agenten der Tags aus einem
  temporären Worktree: `LCM_COMPAT_AGENT_TAGS=v2.0.0,v2.1.0,v2.2.0,v2.3.0 go test -run
  TestEchteAeltereAgenten ./internal/remote/`; Befehl, Eingabe, Abbruch, Inventar; läuft lokal grün für
  alle vier). Der Rest von E-3 steht unter „Umsetzung 24.11.“ weiter oben.

## Paket F: Debian und Paketbau

**F-1 Statisch gelinkt** (zu prüfen): `CGO_ENABLED=0` steht im Makefile und im
CI-Job `binaries` (erledigt 23.11.). Offen: Prüfschritt in `binaries`, der für jedes
Agent-Binary `go version -m` liest und ohne `CGO_ENABLED=0` scheitert. Das alpine-Image
hat kein `file`, deshalb nicht `file`.

Umsetzung 24.11. (F-1): `binaries` prüft nach dem Bau jedes `bin/lcm-*-linux-*` mit
`go version -m … | grep -q CGO_ENABLED=0` und scheitert sonst.

**F-2 Paketskripte unter jeder Shell** (zu prüfen):
- `agent-scripts-test.sh` läuft heute über `go test` mit `/bin/sh`. Neu: derselbe
  Test nacheinander unter `dash`, `busybox sh` und `bash --posix`, jeweils mit `set -e`.
  Eigener CI-Job im alpine-Image, das alle drei per `apk add` bekommt. Läuft nur, wenn
  sich `packaging/` ändert.
- Units: `systemd-analyze verify` für `lcm-agent.service` im Image `debian:11`, damit
  keine Option aus B-3/C-1 dort unbekannt ist. Neue Unit-Optionen brauchen einen Eintrag
  in der Liste „ab systemd-Version“ im Unit-Kommentar.

Umsetzung 24.11. (F-2): `agent-scripts-test.sh` nimmt die Shell aus `LCM_TEST_SHELL` (Vorgabe `sh`) für
die Paket-Skripte, das Startskript und den Syntaxlauf; CI-Job `packaging:shells` (alpine:3.24, nur bei
Änderungen unter `packaging/`) ruft ihn unter `dash`, `busybox sh` und `bash --posix` auf, alle 113 Fälle
grün, keine Unverträglichkeit gefunden. CI-Job `packaging:units` (debian:11 aus dem Archiv, weil Bullseye
aus dem Support ist) führt `systemd-analyze verify` für `lcm-agent.service` mit systemd 247 aus. Der
Befehl endet bei unbekannten Schlüsseln mit 0 und nur einer Warnung, deshalb scheitert der Job an jeder
Ausgabe (geprüft mit einer erfundenen Option).

**F-3 Distro-Matrix auf Knopfdruck** (zu prüfen): `tools/distro-matrix.sh` auf pve-test.
- Das Skript setzt CT 300 und CT 310–333 auf den Snapshot `vor-agent-robust` zurück.
  Rollback geht nur auf den jüngsten Snapshot.
- Dann installiert es das gebaute `.deb`, richtet den Agenten gegen lcm-test ein und
  fährt Grundbefehl, Abbruch, Reconnect nach Netzsperre und Update per Knopf.
- Am Ende schreibt es eine Tabelle je Distribution.
- Pflicht vor jedem Release-Zug nach community. Kein CI-Job, weil die Runner pve-test
  nicht erreichen sollen.

Umsetzung 24.11. (F-3): `tools/distro-matrix.sh` (Aufruf in `docs/reference/ci-release.md`).
Vorgabe-Snapshot `vor-agent-robust`, Container 300, 310–312, 320–322, 330–333 (`-c` ändert die Liste). Je
Container: stop, `pct rollback`, `pct start`, .deb per `pct push`, Agent-Server über die REST-Schnittstelle
der Test-LCM anlegen (API-Schlüssel aus `LCM_API_KEY`), `lcm-agent enroll`, dann Dienst, Version, Online,
ein Auftrag und Wiederaufbau nach `systemctl restart`. Nicht dabei: Netzsperre und „Update per Knopf“
(braucht ein neueres Paket im Kanal). Der Snapshot `vor-agent-robust` muss vor dem ersten Lauf auf jedem
Container angelegt werden. Gegen Attrappen für `sshc` und die Schnittstelle trocken geprüft, noch nicht
gegen pve-test (offen: ob `sshc` Eingabe an `cat > Datei` durchreicht, sonst muss das .deb anders auf den
Host).

## Paket G: Sicherheit

**G-1 Review** (zu prüfen), schriftlich in `doc/sicherheit-agent-2026-11.md`, Befunde mit
Schwere und Behebung:
- `VerifyPeerCertificate`: leere Pins, Rotation, abgelaufenes Zertifikat mit gültigem Pin.
- Einrichtungstoken: Einmaligkeit, Ablauf, Fehlversuche.
- Rechte von `/etc/lcm-agent/agent.json` (0600) und von `/var/lib/lcm-agent` (0700).
- Was ein kompromittierter LCM-Server am Agenten darf (root). Ziel: dokumentierte Grenze,
  kein Ausbruch über das hinaus, was ein Kommando ohnehin darf.
- Grenzen für Nachrichtengrößen, TTY-Puffer, Ausgabe-Spool und Anzahl gehaltener Ergebnisse.

Umsetzung 24.11.: Durchsicht in `doc/sicherheit-agent-2026-11.md` (Befunde S-01 bis S-15 mit
Schwere, Ort und Vorschlag, Grenze zum kompromittierten LCM, Rechte, Größengrenzen). Behoben
nur Robustheit (Sequencer-Grenze, Inventar kürzen); alles an Anmeldung, Pins, Token und Rechten
ist Vorschlag und braucht Tonys Freigabe (S-01, S-02, S-04, S-05, S-07, S-08).

**G-2 Fuzzing** (zu prüfen): `Fuzz…`-Tests für `wire.Command`, `wire.Result`, `wire.Inventory`,
TTY-Frames und `jobMeta` aus der Ablage. Vorbild ist `parsers_fuzz_test.go`. Die
Startkorpora bilden die echten Nachrichten der Agenten 2.0.0 bis 2.4. In der CI läuft
nur der Korpus; Fuzz-Läufe von 10 Minuten je Ziel gibt es lokal vor dem Release-Zug.

Umsetzung 24.11.: 15 Fuzz-Ziele in `internal/remote/wire/fuzz_test.go`, `internal/remote/fuzz_test.go`
und `internal/agent/fuzz_test.go`, Ergebnisse und Lauf-Befehle im Abschnitt „Fuzzing“ von
`doc/sicherheit-agent-2026-11.md`. Die Startkorpora tragen die echten Inventare der Agenten 2.0.0
bis 2.3.0 und den aktuellen Stand; `go test` führt nur sie aus.

**G-3 Neue Angriffsfläche** (zu prüfen):
- Job-Ablage: Pfade nur über `safeID`, Symlinks im Job-Verzeichnis werden nicht verfolgt.
- Wächter und Rückfall-Binary (D-4): Rechte, Austausch durch Dritte, Rückfall nur auf
  das von dpkg gesicherte Binary.
- Update-Job (D-2): nur Versionen aus dem eigenen Kanal, Signatur über apt, Prüfung
  `rePackageVersion` vor jeder Shell.
- Rückfall bei Abweisung durch LCM: gebaut 24.11., siehe Review-Befunde.

Umsetzung 24.11.: Abschnitt „G-3“ und Tabelle J-01 bis J-07 in `doc/sicherheit-agent-2026-11.md`.
Behoben: ID `acked` in der Ablage und Signal an PID 1; kein Angriffsweg über Symlinks für lokale
Benutzer ohne root; Update-Job prüft die Version vor der Shell, aber nicht die Quelle (J-07).

## Paket H: Beobachtbarkeit

**H-1 `lcm-agent status`** (zu prüfen): Der Agent schreibt seinen Zustand alle 10 s und bei
jedem Wechsel nach `/run/lcm-agent/status.json` (0644, ohne Geheimnisse):
- Verbindung, letzter Kontakt, Zahl der Reconnects seit Start, letzte Trennursache;
- laufende und unbestätigte Jobs, Version, „unbewährt“ nach einem Update.

`lcm-agent status` liest die Datei und gibt sie lesbar aus, mit `--json` roh. Kein
Socket, damit kein neuer Zugang entsteht.

Umsetzung (`internal/agent/status.go`, `cmd/agent/main.go`): Zustand und Schreiber im Client
(`WithStatusDir`, Takt `Timing.StatusEvery` = 10 s, zusätzlich bei jedem Verbindungswechsel und
Jobstart/-ende; atomar über temporäre Datei und Umbenennen, 0644 unabhängig von der umask, legt das
Verzeichnis selbst an). Inhalt: Version, Start, Verbindung (`connected`/`connecting`/`disconnected`
seit wann), letzter Kontakt (Verbindung, Ping, Kommando), Reconnects seit Start, letzte Trennung mit
Grund, Jobs der Ablage (laufend, oder fertig mit unbestätigtem Ergebnis; ID, Zustand, Start),
`unproven` (Binary nach Update, noch nicht gesund gemeldet), Rückfall-Vermerk. Keine Zugangsdaten,
keine Adresse. Dienst: `RuntimeDirectory=lcm-agent`, `RuntimeDirectoryPreserve=restart` (der letzte
Stand eines abgestürzten Agents bleibt lesbar; beides in der Paket-Unit, der Selbstinstallations-Unit
und per `checkpath` im OpenRC-Skript; systemd 247 kennt beides). `lcm-agent status` ist älter als
30 s: Hinweis „scheint nicht zu laufen“ samt letztem Stand und Exit-Code 1; fehlt die Datei, nur der
Hinweis. Die Ausgabe folgt der Sprache des Systems (`i18n.T`, Deutsch bei `LANG=de_*`).

**H-2 Abbrüche am Server** (zu prüfen): Der Hub zählt je Agent Verbindungen und
Trennungen mit Ursache (Ping-Ausfall, Broker-Fehler, gewollt). Liegen mehr als 5
ungewollte Trennungen in einer Stunde vor, ist das ein Befund in der bestehenden Ampel
(gelb, „Verbindung instabil“, Popover mit Zahl und letzter Ursache). Es kommt keine
zweite Pille dazu. Die Zähler leben nur im Speicher, ein LCM-Neustart setzt sie zurück.

Umsetzung (`internal/remote/link.go`, `internal/core/domain/agent_link.go`): `linkBook` im Hub mit
einstellbarer Uhr. Ursachen: ungewollt sind `ping` (Keepalive des Brokers abgelaufen), `network`
(Abriss ohne Abschied oder anderer Fehler) und `takeover` (der Agent verband sich neu, bevor die
alte Verbindung als tot galt); gewollt sind `agent` (DISCONNECT des Agents), `lcm` (Hub wird
beendet), `revoked` (`Hub.Disconnect`) und `access_off` (`SetAccepting(false)`): Der Hub vermerkt
den eigenen Grund vor dem Trennen, weil der Hook es nur als Netzfehler sähe. Ungewollte Trennungen
bleiben eine Stunde (höchstens 64 je Agent). Mehr als 5 davon: Befund `agentLinkUnstable` (gelb,
Parameter `count`, `disconnect`) in der bestehenden Ampel, die Oberfläche setzt den Text samt
übersetzter Ursache zusammen. Die Zahlen stehen auch als `agent_link` in der Server-Antwort und in
der Kopfzeile der Server-Detailseite („Trennungen (1 h): n, zuletzt: …“, nur bei mindestens einer).
Grenze: Baut der Agent selbst neu auf, weil LCM nicht mehr pingt, und die Leitung lebt noch, sendet
er vorher einen DISCONNECT; der Hub verbucht das als `agent` (gewollt). Zählen würde es nur mit
Agent-Änderung (Abbruch ohne Abschied).

**H-3 Journal** (zu prüfen):
- `log_level` in `agent.json` (debug, info, warn) und `--log-level` beim Start.
- Jede Trennung, jeder Reconnect und jeder Binary-Wechsel steht immer mit Grund im
  Journal, auch auf `warn`.

Umsetzung: `Config.LogLevel` (`log_level`), `lcm-agent run --log-level`, der Schalter geht vor. Eine
unbrauchbare Angabe bricht den Dienst nicht ab (er ist oft der einzige Weg auf den Server), sie
fällt mit einer Warnung auf `info`. Auf Warn stehen jetzt: Trennung („connection to lcm ended“, mit
`reason`), Wiederverbindung („reconnected to lcm server“ mit `reason` und Zeitpunkt der Trennung;
die erste Verbindung bleibt Info), gewolltes Trennen bei Signal oder Stop, Wechsel des Binarys
(`reason=new binary installed`, Version); der Rückfall stand schon auf Warn/Error. Auf Debug neu:
beantwortete Pings.

**E-3 im Detail** (zu prüfen, Stand siehe Umsetzung bei E-3):
- CI-Job `compat:agents` (MR, nur bei Änderungen in `internal/remote`, `internal/agent`
  und `cmd/agent`): `LCM_COMPAT_AGENT_TAGS=v2.0.0,v2.1.0,v2.2.0,v2.3.0 go test -run
  TestEchteAeltereAgenten ./internal/remote/`. Der Job braucht alle Tags
  (`GIT_DEPTH: 0`, `git fetch --tags`). Bauzeit begrenzen: Die Agent-Binaries der Tags
  liegen im CI-Cache, Schlüssel ist der Tag.
- Fehlende Fälle: „Ergebnis nach Reconnect“ (Netzsperre über den Fault-Proxy) und
  Konsole für Agenten mit `tty`.
- Umgekehrt: Der aktuelle Agent läuft gegen den Hub der Tags v2.3.0 und v2.4.x. Der
  Test baut `cmd/app` des Tags mit `--demo` und prüft Grundbefehl, Inventar und Abbruch.
  Neue Funktionen bleiben dabei stumm, es gibt keine Fehler.
- Die Liste wächst mit jedem Release: Der Release-Zug trägt den neuen Tag in die
  CI-Variable ein.

## Reihenfolge

1. A1, A2, A5 (Prüfstand und Matrix stehen, Fehlerbilder reproduziert)
2. B (Selbstheilung): schnellster Gewinn für „kommt nicht wieder“
3. C (Jobs) und E-1/E-2 (Vertrag), weil D darauf aufbaut
4. D (kontrolliertes Update), dann E-3/E-4
5. A3, A4 als Abnahme, danach F, G, H

## Abnahme (messbar)

- Netzabbruch jeder Länge: Agent binnen 2 min plus Jitter wieder bereit, 1000 von 1000.
- Halb offene Verbindung: erkannt und neu aufgebaut binnen 3 Ping-Intervallen.
- `kill -9`, OOM, Panic mitten in `apt-get upgrade`: dpkg sauber, Ergebnis kommt an.
- `apt upgrade` von außen ändert den Agenten nicht.
- Defektes Agent-Binary: Rückfall binnen 5 min, Server zeigt es an.
- Agent 2.0.0 gegen Server 2.3.0+: Grundfunktionen gehen, neue Funktionen sind aus mit
  Update-Hinweis.
- 72 h Langlauf: kein Speicher- oder Goroutinen-Wachstum.
- Distro-Matrix vollständig grün.

## Entscheidungen (Tony, 23.11.2026)

1. Laufende Kommandos werden nicht mehr angehalten, weder durch Agent-Stop/-Absturz
   noch durch Server-Timeouts. Ob ein Kommando noch läuft, beurteilt der Agent (C-1, C-3, C-4).
2. Mindestversion des Agenten: 2.0.0 (E-2).
3. Agent-Update per Knopf und über Regeln in den Gruppeneinstellungen (D-2).
4. Es gibt nur einen Agenten (Community, dazu Beta), auch für Enterprise. Der Server
   bestimmt die genaue Zielversion, nie höher als er selbst (D-2, D-2a).

## Entscheidungen (Tony, 24.11.2026)

5. Zielversion: Bugfixes über der LCM-Version sind erlaubt, solange die Feature-Reihe
   gleich bleibt; LCM steuert die genaue Version (D-2, gebaut, zu prüfen).
6. B16: Upgrade-Limit für eingerichtete Agenten lockern (gebaut, zu prüfen).
7. Job-Wächter (erlaubte Stille) gilt nur noch für SSH-Server und Agenten ohne `jobs-v2`
   (gebaut, zu prüfen; erweitert durch Entscheidung 13).
8. Agenten aus den 2.0-Betas gelten als 2.0.0 (E-2, gebaut, zu prüfen).
9. Gelb heißt „Funktion fehlt“, nicht „Version hinter LCM“; automatische Updates nie
   abwärts (bleibt so).
10. rpm und apk sollen kommen: als nächster Schritt nach den Agent-Paketen (neues Paket I).
11. Tests auf echten Systemen über pve-test (192.168.6.240, SSH); lcm-prod-Journal
    darf gelesen werden.
12. Nachgetragenes Ergebnis: Erfolg nur bei Jobs mit einem Schritt; bei mehreren
    Schritten bleibt der Job fehlgeschlagen, Exit-Code und Ausgabe stehen drin (B18).
13. Alte Agenten ohne `jobs-v2` bekommen die erlaubte Stille wieder; Entscheidung 1 gilt
    nur für Agenten mit dauerhaften Jobs.
14. Weist LCM den Agenten ab (Token, Agent-Zugang aus), wird ein neues Binary nicht
    zurückgerollt (D-4). Statt Qodana (braucht ein Cloud-Konto) läuft golangci-lint.

## Paket I: rpm, apk und Arch (in Arbeit, Plan: `doc/plan-2026-11-25-paket-i-pakete.md`)

Agent-Pakete für rpm (dnf/zypper) und apk bauen und im Repo veröffentlichen, dazu
Halten gegen normale Updates (`versionlock`, fester Stand in `/etc/apk/world`) und der
Update-Job für diese Paketmanager. Kommt nach A3 bis H.

## Arbeitsweise

Entwicklung und Tests im Worktree `~/dev/LCM/LCM-agent-robust`, Branch
`feat/agent-robustheit` (von develop), damit andere LCM-Sitzungen ungestört bleiben.
