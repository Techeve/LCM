---
sidebar:
  order: 29
title: Monitoring von Host und Datenbank
description: Was LCM über seine Datenbank misst, die Endpunkte /api/v1/system/db und /metrics, und das Werkzeug lcm-probe, das Host und Datenbank dauerhaft aufzeichnet und selbst auswertet.
---

LCM misst, was seine Datenbank im Betrieb tut, und stellt die Zahlen über zwei
Endpunkte bereit. Das Werkzeug `lcm-probe` zeichnet sie zusammen mit den
Werten des Hosts auf und wertet sie aus, auch als HTML mit Diagrammen.

## Was LCM misst

Ohne Schema-Änderung und ohne Last auf die Datenbank, im Speicher des
Prozesses. Alle Zähler beginnen bei jedem Start bei null (`started_at`).

- **Wartezeit auf die Schreibsperre**: Dauer von `BEGIN IMMEDIATE`, gemessen
  an einem Wrapper um den Verbindungspool. Stufen von 0,1 ms bis 10 s.
- **Dauer der Transaktionen** und die zehn längsten mit Aufrufstelle.
- **Dauer der Anweisungen** je Tabelle und Art (SELECT, INSERT, UPDATE, DELETE)
  in den festen Stufen 1, 10, 100, 1.000 und 10.000 ms, dazu die 50 langsamsten
  Anweisungen ohne Werte.
- **Fehler durch Sperren**: `database is locked` (`SQLITE_BUSY`) und
  `SQLITE_LOCKED`.
- **Größe**: Datei, WAL, `page_count`, `freelist_count`; stündlich die Zeilen
  jeder Tabelle.
- **Läufe**: Checkpoint, `VACUUM`, `VACUUM INTO` (Backup) und jeder System-Job
  wie Log-Bereinigung oder CVE-Scan, mit Dauer, Ergebnis und geschriebenen
  Zeilen.
- **Prozess**: Heap, Goroutines, offene Dateien, Verbindungspool.

## Endpunkte

Beide verlangen `settings:manage`, auch per API-Schlüssel mit Bereich `read`.

| Endpunkt | Format |
|---|---|
| `GET /api/v1/system/db` | JSON-Momentaufnahme, Quelle für `lcm-probe` |
| `GET /metrics` | Prometheus-Textformat, Dauern in Sekunden, Präfix `lcm_db_` |

## lcm-probe

Eigenes Paket, läuft als eigener Dienst und schreibt weiter, wenn LCM steht.

```bash
apt install lcm-probe
```

Für die Datenbank braucht es einen API-Schlüssel mit Bereich `read`, angelegt von
einem Administrator:

```bash
echo '<schlüssel>' > /etc/lcm-probe/key && chmod 600 /etc/lcm-probe/key
```

Ein Neustart ist nicht nötig. Das Paket entfernt beim Einspielen den früheren
Messschreiber `/usr/local/sbin/lcm-probe` samt Unit; dessen Protokolle unter
`/var/log/lcm-probe` bleiben liegen.

**Was es schreibt:** alle 2 Sekunden Last, CPU-Anteile samt `iowait` und
`steal`, Speicher samt Cache, Swap und Grenze des Containers, freien Platz unter
`/var/lib/lcm` und `/`, Verkehr und mittlere Wartezeit je Platte, Druck (PSI)
für CPU, Speicher und I/O, Wanduhr und monotone Uhr, den eigenen Verbrauch. Jede
Minute eine Momentaufnahme von `GET /api/v1/system/db`; antwortet LCM nicht,
steht das als eigene Zeile da.

**Wohin:** eine Datei je Tag unter `/var/lib/lcm-probe/`, eine Zeile JSON je
Messung, nach jeder Zeile `fdatasync`. Fertige Tage werden gepackt, nach 90
Tagen gelöscht. Die Dateien lassen sich jederzeit kopieren.

**Container oder VM:** Im LXC liefern nicht alle Quellen Werte des Containers.
`lcm-probe check` zeigt, woher jeder Wert kommt:

| Herkunft | Bedeutung |
|---|---|
| `system` | eigener Kernel, VM oder Blech |
| `container` | Wert des Containers, meist aus der cgroup |
| `host` | Wert des Hosts, durch den Container gereicht |
| `mixed` | lxcfs `/proc/stat`: Benutzer und System vom Container, `iowait` und `steal` vom Host |

## Auswerten

Auf dem Host oder auf jedem Rechner mit einer Kopie der Tagesdateien, auch auf
dem Mac (`go run ./cmd/probe ...`). Relative Zeiten zählen ab der jüngsten Zeile.

```bash
lcm-probe report --since 7d
```

Lücken mit Dauer und dem, was in den fünf Minuten davor lief; Wartezeit auf die
Schreibsperre mit Median, 95 % und Maximum; die teuersten Anweisungen; Wachstum
je Tag und Tabelle; Spitzen von Druck, Last und Speicher; Zeiten ohne LCM.

```bash
lcm-probe report --since 7d --html bericht.html
```

Eine einzelne HTML-Datei mit zoombaren Zeitreihen auf gemeinsamer Achse, Lücken
und Läufe markiert. Sie läuft ohne Netz.

```bash
lcm-probe query --from 04:00 --to 04:30 --fields psi.io_some,cpu.iowait,disks.sda.await_ms
```

Zeilen als CSV oder mit `--format json` als JSON Lines. Felder stehen als
Punkt-Pfad; `--type db` liefert die Momentaufnahmen, etwa
`db.lock_wait.max_ms` oder `db.statements.jobs.SELECT.count`.
