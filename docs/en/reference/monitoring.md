---
sidebar:
  order: 29
title: Database monitoring
description: What LCM measures about its database and the /api/v1/system/db and /metrics endpoints that expose the numbers.
---

LCM measures what its database does in operation and exposes the numbers on two
endpoints.

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
| `GET /api/v1/system/db` | JSON snapshot |
| `GET /metrics` | Prometheus text format, durations in seconds, prefix `lcm_db_` |
