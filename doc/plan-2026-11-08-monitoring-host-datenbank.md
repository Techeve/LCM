# Monitoring des LCM-Hosts und der Datenbank vor Release C

Stand 08.11.2026. Tonys Auftrag: Als erster Schritt von Release C ein Monitoring, als
Schnittstelle in LCM plus ein kleines Werkzeug für lcm-prod, früh in den Beta-Kanal,
damit über die Entwicklungszeit von C echte Zahlen zusammenkommen. Das Werkzeug misst
die Datenbank und die Umstände des Hosts, schreibt jede Zeile sofort dauerhaft weg und
bringt seine eigene Auswertung mit, auch grafisch.

## Warum jetzt

- lcm-prod trug am 02.10.2026 eine Datei von 5,6 GB, bei rund zwanzig Servern. Die
  Flottenmessung für 1.000 Server im Schlimmstfall ergab 4,9 GB. Irgendetwas auf
  lcm-prod ist also deutlich größer, als die Flotte es erklärt. Was, weiß niemand.
  Heute konnte ich es nicht prüfen, der Lesezugriff auf lcm-prod war gesperrt.
- Der nächtliche Stillstand um 04:03 ist seit September ungeklärt. Der Messschreiber
  `lcm-probe` sieht nur, dass der Container steht, nicht was die Datenbank in dem
  Moment tut.
- Release C schreibt mehr: Funde je Server, Bewertungsläufe, Scans nach jeder neuen
  Trivy-Datenbank. Die Entscheidung für SQLite (Plan C-Kernumfang, Abschnitt
  „Datenbank bei tausenden Servern“) steht auf einer Messung auf dem Mac. Sie braucht
  Zahlen aus dem Betrieb.

## Was gemessen wird

Alles aus einem Prozess heraus, ohne Schema-Änderung, ohne Last auf die Datenbank.

**Warten und Dauer**

- Wartezeit auf die Schreibsperre: Dauer von `BEGIN IMMEDIATE`. Das ist seit
  `_txlock=immediate` die eine Zahl, die sagt, ob sich Schreiber im Weg stehen.
  Gemessen über einen Wrapper um den Verbindungspool (`gorm.ConnPool`, `TxBeginner`),
  kein Eingriff in die Repositories.
- Dauer je Anweisung, als Histogramm je Tabelle und Art (SELECT, INSERT, UPDATE,
  DELETE). Woher: GORM-Callback vor und nach jeder Anweisung. Zu teuer wäre, jede
  Anweisung zu speichern, deshalb Histogramm mit festen Stufen (1, 10, 100, 1.000,
  10.000 ms) plus die 50 langsamsten mit Anweisungstext ohne Werte.
- Dauer der Transaktionen, Histogramm, plus die längsten zehn mit Aufrufstelle.
- Zahl der `database is locked` und `SQLITE_BUSY`, auch wenn sie durch Warten
  verschwinden sollten. Wenn sie nicht null sind, wissen wir sofort Bescheid.

**Größe und Wachstum**

- Dateigröße, WAL-Größe, `page_count`, `freelist_count`, einmal je Minute.
- Zeilen je Tabelle, einmal je Stunde, über `sqlite_stat1` nach `ANALYZE` oder
  `count(*)` für die großen Tabellen (Jobs, SSH-Protokolle, Funde, Bestand).
- Dauer und Ergebnis von Checkpoint, `VACUUM`, `VACUUM INTO` (Backup) und der
  Log-Bereinigung. Das sind die Kandidaten für den Stillstand um 04:03.

**Prozess**

- Heap, Goroutines, offene Dateien, Datenbankverbindungen im Pool, freier Platz auf
  `/var/lib/lcm`.
- Die Läufe, die die Datenbank treiben: CVE-Scan, Bewertung (Release C), Alarm-
  Auswertung, Frühwarnung, mit Dauer und geschriebenen Zeilen.

## Schnittstelle

Zwei Wege zu denselben Zahlen, beide ohne Oberfläche in der ersten Fassung:

1. `GET /api/v1/system/db` liefert eine JSON-Momentaufnahme: die Zähler oben, die
   Histogramme, die Listen der langsamsten Anweisungen. Recht `settings:manage`, also
   auch per API-Schlüssel. Das ist die Schnittstelle für das Werkzeug.
2. `GET /metrics` im Prometheus-Textformat, dieselben Zahlen. Kostet nichts extra, und
   wer Prometheus hat, braucht unser Werkzeug nicht. Hinter demselben Recht.

Die Zähler leben im Speicher des Prozesses und beginnen bei jedem Start bei null. Den
Verlauf hält das Werkzeug, nicht LCM. So bleibt die Datenbank außen vor und die
Messung verfälscht nicht, was sie misst.

## Die Umstände des Hosts

Die Datenbank ist nur eine Hälfte. Ein Stillstand um 04:03 kann vom Backup des Hosts
kommen, von vollem Speicher oder von einer Platte, die nicht hinterherkommt. Das
Werkzeug misst deshalb auch, worauf LCM läuft:

- **CPU:** Last (1, 5, 15 min), Anteil in Benutzer, System, Warten auf I/O und
  gestohlen (`steal`, auf einer VM der Hinweis auf einen überlasteten Host).
- **Arbeitsspeicher:** genutzt, verfügbar, Dateicache, Swap, und was der Container an
  Grenze hat (`memory.max`).
- **Platte:** freier Platz unter dem Datenverzeichnis und unter `/`, gelesene und
  geschriebene Bytes und Vorgänge je Intervall, mittlere Wartezeit je Vorgang aus
  `/proc/diskstats`.
- **Druck (PSI):** `some` und `full` für CPU, Speicher und I/O. Das ist die direkte
  Antwort auf „I/O-Delay“: Wie viel Zeit haben Prozesse gewartet, weil die Platte nicht
  hinterherkam.
- **Takt:** Jede Zeile trägt Wanduhr und monotone Uhr. Klafft zwischen zwei Zeilen eine
  Lücke, stand der Container oder die VM. Das ist, was `lcm-probe` heute als
  `STILLSTAND` meldet, nur mit allem drumherum.

**Geht das in einer VM und in einem LXC?** In einer VM ja, dort ist der Kernel eigener.
Im LXC sind die Werte unter `/proc` die des Hosts, außer dort, wo lxcfs sie umbiegt
(`loadavg`, `meminfo`, `cpuinfo`, `stat`, `uptime`). Container-eigene Werte liefert die
eigene cgroup unter `/sys/fs/cgroup/`: `cpu.stat`, `memory.current`, `memory.max`,
`io.stat`, und mit cgroup v2 auch `cpu.pressure`, `memory.pressure`, `io.pressure` je
Container. Proxmox läuft seit Version 7 mit cgroup v2. Das Werkzeug liest zuerst die
cgroup, dann `/proc`, und schreibt in jede Zeile, woher der Wert kam. Was es nicht
sicher zuordnen kann, kennzeichnet es als Hostwert statt zu raten.

### Befund in CT 120 und VM 150 (08.11.2026)

Nachgesehen auf pve-test (PVE 9.2.10, Kernel 7.0.14-5-pve, cgroup v2): CT 120
(`lcm-test`, Debian 13, 2 Kerne, 2 GB, 512 MB Swap, lxcfs aktiv) und VM 150
(`patch-kernel`, Debian 13, Kernel 6.12). Verglichen wurde jeweils mit denselben
Dateien auf dem Host im selben Augenblick.

**Im Container** ist `/sys/fs/cgroup` die Wurzel des cgroup-Namensraums, also der
Container selbst (auf dem Host `lxc/120/ns`). Ob man im Container ist, verrät
`memory.current` in der Wurzel: Die echte Wurzel eines Kernels hat diese Datei nicht.
lxcfs biegt `cpuinfo`, `diskstats`, `loadavg`, `meminfo`, `pressure`, `slabinfo`,
`stat`, `swaps`, `uptime` und `/sys/devices/system/cpu` um, aber nicht alle davon
liefern Containerwerte:

| Quelle | Im LXC | Anmerkung |
|---|---|---|
| `/sys/fs/cgroup/{cpu,memory,io}.pressure` | Container | gleich `lxc/120/ns` auf dem Host |
| `/sys/fs/cgroup/cpu.stat`, `io.stat`, `memory.current`, `memory.stat` | Container | `io.stat` nennt die Geräte, die der Container nutzt (`8:0`) |
| `/sys/fs/cgroup/memory.max`, `memory.swap.max` | `max` | die Grenze sitzt eine Stufe höher in `lxc/120` und ist von innen unsichtbar |
| `/proc/meminfo` | Container | lxcfs: `MemTotal` ist die Grenze von 2 GB, `SwapTotal` aber 0 trotz 512 MB Swap |
| `/proc/stat` | gemischt | lxcfs zeigt nur die zugeteilten Kerne; Benutzer und System aus der cgroup, `iowait` und `steal` sind die Hostwerte dieser Kerne |
| `/proc/loadavg` | Host | lxcfs ohne `-l`: gleich dem Host, 1.063 Tasks bei 20 Prozessen im Container |
| `/proc/pressure/cpu`, `memory` | Container | lxcfs reicht die cgroup durch |
| `/proc/pressure/io` | Host | lxcfs liefert hier den Hostwert, gleich `/proc/pressure/io` des Hosts |
| `/proc/diskstats` | Host | lxcfs reicht den Host durch, mit allen zvols der anderen Gäste |
| `/proc/uptime` | Container | Laufzeit des Containers |
| monotone Uhr | Host | kein Zeit-Namensraum, die Uhr läuft auch, wenn der Container eingefroren ist |

**In der VM** ist alles eigener Kernel. `/sys/fs/cgroup` ist die echte Wurzel ohne
`memory.current` und `memory.max`, die Druckwerte der Wurzel sind gleich
`/proc/pressure`. `steal` in `/proc/stat` ist belegt, `diskstats` zeigt die
virtuelle Platte `sda`.

**Was daraus für den Sammler folgt:**

- Druck aus der cgroup-Wurzel, Rückfall auf `/proc/pressure`. Im Container gilt der
  Rückfall als Hostwert, weil lxcfs dort mischt.
- CPU-Anteile aus `/proc/stat`, im Container als `lxcfs` gekennzeichnet; dazu der
  Verbrauch des Containers aus `cpu.stat` samt Drosselung.
- Speicher aus `/proc/meminfo`, dazu `memory.current` der cgroup. Steht `memory.max`
  auf `max`, gilt im Container `MemTotal` aus lxcfs als Grenze.
- Platten: im Container nur die Geräte aus `io.stat`, deren Zähler aus `diskstats`
  sind dann Hostwerte des ganzen Geräts; die Bytes des Containers selbst stehen in
  `io.stat`. In VM und auf Blech alle ganzen Platten aus `diskstats`.
- Last: im Container als Hostwert gekennzeichnet, wenn `loadavg` deutlich mehr Tasks
  nennt, als der Container Threads hat.
- Die Herkunft steht als eigene Zeile `sources` am Anfang jeder Tagesdatei und bei
  jeder Änderung, nicht in jeder Hostzeile; so bleibt die Zeile klein und jede Datei
  ist für sich lesbar.

## Das Werkzeug: lcm-probe

Ein Go-Programm im selben Repository (`cmd/probe`), eigenes deb `lcm-probe`, wie
`lcm-agent` über die bestehende Pipeline gebaut. Der Name übernimmt den des
Messschreibers, den Tony im Oktober auf lcm-prod gelegt hat
(`/usr/local/sbin/lcm-probe`); das Paket löst ihn ab und entfernt das Skript beim
Einspielen. Es läuft als eigener Dienst, nicht als Timer: Es muss weiterlaufen, wenn LCM
steht, denn genau dann sind seine Zeilen am wertvollsten.

- **Hostwerte alle 2 Sekunden**, eine kompakte Zeile von rund 150 Byte. Die
  Datenbank-Momentaufnahme von `GET /api/v1/system/db` **alle 60 Sekunden**, mit
  API-Schlüssel aus `/etc/lcm-probe/key` (Bereich `read`). Ist LCM nicht erreichbar,
  steht das als eigene Zeile drin, und die Hostzeilen laufen weiter.
- **Schreibweise:** Eine Datei je Tag unter `/var/lib/lcm-probe/`, Zeilen als JSON
  (JSON Lines), geöffnet mit `O_APPEND`, nach jeder Zeile `fdatasync`. Ein Anhängen ist
  atomar, nach einem Absturz fehlt höchstens die angefangene letzte Zeile, und der Leser
  überspringt eine unvollständige Zeile. Die Datei ist jederzeit kopierbar, sie ist
  reiner Text ohne Sperre. Fertige Tage packt das Werkzeug mit gzip, Standard 90 Tage
  Aufbewahrung, rund 25 MB je Monat.
- **Aufwand:** 2-Sekunden-Takt mit `fdatasync` sind 43.200 kleine Schreibvorgänge am
  Tag, auf SSD oder ZFS kein Thema. Das Werkzeug misst seinen eigenen Verbrauch mit und
  schreibt ihn in die Zeile.

## Auswertung: lcm-probe report

Dasselbe Binary wertet aus, auf dem Host oder auf jedem Rechner, auf den man die
Dateien kopiert hat. Kein Dienst, keine Datenbank, keine Installation daneben.

- `lcm-probe report --since 7d` schreibt einen Textbericht für Menschen und Agenten:
  Lücken mit Dauer und Uhrzeit und was in den fünf Minuten davor lief (Backup, VACUUM,
  Checkpoint, Bereinigung, CVE-Scan); Verteilung der Schreibwartezeit mit Median,
  95 % und Maximum samt Uhrzeit; die zehn teuersten Anweisungen; Wachstum je Tag und
  Tabelle; Spitzen von I/O-Druck, Last und Speicher mit Uhrzeit; wie oft LCM nicht
  erreichbar war.
- `lcm-probe report --html bericht.html` erzeugt eine einzelne HTML-Datei mit
  Zeitreihen zum Hineinzoomen: Schreibwartezeit, I/O-Druck, Last, Speicher,
  Dateigröße, WAL, mit markierten Lücken und Ereignissen wie Backup und VACUUM auf
  derselben Zeitachse. Daten und eine kleine Diagrammbibliothek stecken in der Datei,
  sie läuft in jedem Browser ohne Netz. Das ist der Weg, Schwachpunkte zu sehen:
  Steigt die Wartezeit, wenn der I/O-Druck steigt? Kommt der Stillstand nach dem
  Backup?
- `lcm-probe query --from 04:00 --to 04:30 --fields ...` gibt Zeilen als CSV oder
  JSON heraus, für alles, was Bericht und Grafik nicht vorsehen, und für mich als
  Agenten, um gezielt nachzusehen.
- `lcm-probe check` prüft beim Start und auf Wunsch, welche Quellen es auf diesem
  System lesen kann, und sagt, welche Werte Host- und welche Containerwerte sind.

## Weg in den Beta-Kanal

Release C ist ein langer Zweig, das Monitoring darf nicht an ihm hängen.

1. Zweig `feat/db-monitoring` von `develop`. Umfang: Messung in LCM, die beiden
   Endpunkte, das Werkzeug mit Hostwerten, Schreibweise und Auswertung, Dokumentation
   in `docs/reference/`. Schätzung vier bis fünf Tage.
2. MR nach `develop`, Beta-Release, Tony spielt `lcm` und `lcm-probe` auf lcm-prod ein.
   Für den Schlüssel: API-Schlüssel mit Bereich `read` in LCM anlegen, in
   `/etc/lcm-probe/key` legen.
3. Nach `develop` zieht `feat/release-c` den Stand nach, dann misst auch der Test auf
   lcm-test damit. Dort lässt sich LCM mit der Datenbank der synthetischen Flotte
   starten: 1.000 Server, Bewertung und Scans unter Last, mit denselben Zahlen.
4. Wöchentlich ein Bericht von lcm-prod, bis Release C in den Community-Kanal geht.
   Befunde gehen in den Plan C-Kernumfang, Abschnitt Datenbank. Die Tagesdateien
   lassen sich kopieren, die Auswertung läuft dann auf dem Mac.

## Stand des Baus (08.11.2026)

Gebaut auf `feat/db-monitoring` von `develop`, ungepusht. Wo der Bau vom Text
oben abweicht, und warum:

- **HTTPS statt HTTP.** LCM spricht auf seinem Port HTTPS mit eigenem
  Zertifikat. `lcm-probe` macht es wie der Healthcheck: HTTPS gegen das
  Zertifikat aus `/var/lib/lcm`, HTTP nur, wenn LCM mit `--dev` läuft. Der
  Schlüssel geht nur an den eigenen Rechner oder an eine Gegenstelle mit
  diesem Zertifikat. Die Pinnung liegt jetzt in `tlsx.PinnedTo`.
- **Herkunft je Datei, nicht je Zeile.** Eine Zeile `sources` am Anfang jeder
  Tagesdatei und bei jeder Änderung (siehe Befund oben).
- **Alle Tabellen zählen, nicht nur die großen.** Welche groß sind, ist genau
  die offene Frage auf lcm-prod. `count(*)` läuft stündlich je Tabelle mit
  Zeitgrenze, am rohen Pool vorbei; die Dauer steht in der Momentaufnahme.
- **Läufe sind alle System-Jobs**, nicht nur die Log-Bereinigung: Backup,
  CVE-Scan, Alarm-Auswertung, Frühwarnung, Docker- und Anwendungs-Check, dazu
  VACUUM, Checkpoint und `VACUUM INTO`. Die geschriebenen Zeilen eines Laufs
  zählen alle Schreiber in dieser Zeit.
- **Zeilen größer als geschätzt.** Eine Hostzeile hat im LXC rund 900 Byte,
  nicht 150; eine Momentaufnahme einige Kilobyte, ohne die unveränderten
  Abschnitte. Die tatsächliche Größe je Tag steht nach dem Testlauf unten.
- **Eine angehaltene VM** lässt auch die monotone Uhr stehen. Der Bericht
  nennt eine Lücke, die nur die Wanduhr zeigt, „Pause der VM oder Uhr
  vorgestellt".
- **Maxima seit dem Start von LCM.** LCM kennt das Maximum der Wartezeit und
  die langsamsten Anweisungen seit seinem Start. Liegt der Wert vor dem
  Zeitraum eines Berichts, nennt der Bericht die höchste belegte Stufe.

Der Testlauf auf CT 120 lief mit einem Testbuild: der dort eingespielte
Release-C-Stand `71b01a72` plus die Commits dieses Zweigs, damit der Teststand
von Release C unverändert bleibt.

## Was die Zahlen entscheiden sollen

- Woher die 5,6 GB auf lcm-prod kommen, und ob eine Frist oder ein Schema fehlt.
- Ob der Stillstand um 04:03 mit Backup, `VACUUM` oder Checkpoint zusammenfällt, oder
  mit I/O-Druck und Speicher des Hosts, die LCM gar nicht verursacht.
- Wie die Schreibwartezeit im Alltag verteilt ist, und ob die Bewertung von Release C
  sie sichtbar anhebt.
- Ob Funde je Server (heute) oder je Paketprofil (Plan, Faktor 25) gespeichert werden.
- Ob die Schwelle für Postgres (Plan: ab etwa 10 GB oder wenn eine Bewertung die
  Patch-Fenster berührt) in Reichweite ist. Die Erwartung ist nein.

## Später

- Eine Karte auf der Betriebsseite mit Dateigröße, Wachstum je Woche und letzter
  Schreibwartezeit, wenn die Zahlen sich bewährt haben.
- Ein Alarm „Datenbank wächst ungewöhnlich“ aus denselben Zahlen.
- Dieselben Zähler für die Agenten, sobald sie Trivy-Scans liefern.
- Die Hostwerte des Werkzeugs auch für verwaltete Server, über den Agenten.
