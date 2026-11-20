---
sidebar:
  order: 29
title: Monitoring der Datenbank
description: Was LCM über seine Datenbank misst und die Endpunkte /api/v1/system/db und /metrics, über die sich die Zahlen abrufen lassen.
---

LCM misst, was seine Datenbank im Betrieb tut, und stellt die Zahlen über zwei
Endpunkte bereit.

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
| `GET /api/v1/system/db` | JSON-Momentaufnahme |
| `GET /metrics` | Prometheus-Textformat, Dauern in Sekunden, Präfix `lcm_db_` |
