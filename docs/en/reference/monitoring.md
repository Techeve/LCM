---
sidebar:
  order: 29
title: Host and database monitoring
description: What LCM measures about its database, the /api/v1/system/db and /metrics endpoints, and the lcm-probe tool that records host and database continuously and analyses the data itself.
---

LCM measures what its database does in operation and exposes the numbers on two
endpoints. The `lcm-probe` tool records them together with the host's values and
analyses them, also as HTML with charts.

## What LCM measures

No schema change and no load on the database; everything lives in process
memory. All counters start at zero on every start (`started_at`).

- **Wait for the write lock**: duration of `BEGIN IMMEDIATE`, measured by a
  wrapper around the connection pool. Buckets from 0.1 ms to 10 s.
- **Transaction duration** and the ten longest with their call site.
- **Statement duration** per table and kind (SELECT, INSERT, UPDATE, DELETE) in
  the fixed buckets 1, 10, 100, 1,000 and 10,000 ms, plus the 50 slowest
  statements without values.
- **Lock errors**: `database is locked` (`SQLITE_BUSY`) and `SQLITE_LOCKED`.
- **Size**: file, WAL, `page_count`, `freelist_count`; rows of every table,
  hourly.
- **Runs**: checkpoint, `VACUUM`, `VACUUM INTO` (backup) and every system job
  such as log cleanup or CVE scan, with duration, result and rows written.
- **Process**: heap, goroutines, open files, connection pool.

## Endpoints

Both require `settings:manage`, also via an API key with scope `read`.

| Endpoint | Format |
|---|---|
| `GET /api/v1/system/db` | JSON snapshot, the source for `lcm-probe` |
| `GET /metrics` | Prometheus text format, durations in seconds, prefix `lcm_db_` |

## lcm-probe

A package of its own, running as its own service; it keeps writing while LCM is
down.

```bash
apt install lcm-probe
```

For the database it needs an API key with scope `read`, created by an
administrator:

```bash
echo '<key>' > /etc/lcm-probe/key && chmod 600 /etc/lcm-probe/key
```

No restart needed. On installation the package removes the earlier measuring
script `/usr/local/sbin/lcm-probe` and its unit; its logs in
`/var/log/lcm-probe` stay.

**What it writes:** every 2 seconds load, CPU shares including `iowait` and
`steal`, memory with cache, swap and the container limit, free space under
`/var/lib/lcm` and `/`, traffic and mean wait per disk, pressure (PSI) for CPU,
memory and I/O, wall clock and monotonic clock, its own usage. Every minute a
snapshot of `GET /api/v1/system/db`; if LCM does not answer, that is a line of
its own.

**Where:** one file per day under `/var/lib/lcm-probe/`, one JSON line per
measurement, `fdatasync` after every line. Finished days are compressed and
deleted after 90 days. A compressed day takes about 3 MB, so 90 days take
about 300 MB. The files can be copied at any time.

**Container or VM:** inside LXC not every source reports the container's
values. `lcm-probe check` shows where each value comes from:

| Origin | Meaning |
|---|---|
| `system` | own kernel, VM or bare metal |
| `container` | the container's value, mostly from the cgroup |
| `host` | the host's value, passed through the container |
| `mixed` | lxcfs `/proc/stat`: user and system from the container, `iowait` and `steal` from the host |

## Analysis

On the host or on any machine with a copy of the day files, including a Mac
(`go run ./cmd/probe ...`). Relative times count back from the newest line.

```bash
lcm-probe report --since 7d
```

Gaps with duration and what ran in the five minutes before; wait for the write
lock with median, 95 % and maximum; the most expensive statements; growth per day
and table; peaks of pressure, load and memory; times without LCM.

```bash
lcm-probe report --since 7d --html report.html
```

A single HTML file with zoomable time series on a shared axis, gaps and runs
marked. It works offline.

```bash
lcm-probe query --from 04:00 --to 04:30 --fields psi.io_some,cpu.iowait,disks.sda.await_ms
```

Lines as CSV or, with `--format json`, as JSON Lines. Fields are dotted paths;
`--type db` returns the snapshots, for example `db.lock_wait.max_ms` or
`db.statements.jobs.SELECT.count`.
