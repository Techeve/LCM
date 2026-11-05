import { describe, expect, it } from 'vitest';
import {
  advisoriesUrl,
  bulkPercent,
  hitRate,
  normaliseAdvisoryPage,
  normaliseBulk,
  normaliseCaches,
  normaliseCvePage,
  normaliseScanner,
  severityOf,
  vulnerabilitiesUrl,
} from './security';

describe('addresses', () => {
  it('leaves the source out when all are shown', () => {
    expect(vulnerabilitiesUrl({ source: '', page: 1 })).toBe('/api/v1/security/vulnerabilities?page=1&page_size=50');
    expect(vulnerabilitiesUrl({ source: 'os', page: 2 })).toBe('/api/v1/security/vulnerabilities?page=2&page_size=50&source=os');
  });

  it('writes the advisory filters', () => {
    expect(advisoriesUrl({ minSeverity: '', resolved: false, page: 1 })).toBe('/api/v1/security/advisories?page=1&page_size=50');
    expect(advisoriesUrl({ minSeverity: 'high', resolved: true, page: 3 })).toBe('/api/v1/security/advisories?page=3&page_size=50&resolved=1&min_severity=high');
  });
});

describe('normaliseCvePage', () => {
  it('reads a page', () => {
    const page = normaliseCvePage({
      items: [{ server_id: 1, server_name: 'web01', cve_id: 'CVE-1', package_name: 'openssl', severity: 'critical', source: 'docker', image_ref: 'nginx:1' }],
      total: 7,
      page: 2,
      summary: { critical: 1, high: 3 },
    });
    expect(page.total).toBe(7);
    expect(page.page).toBe(2);
    expect(page.summary).toEqual({ critical: 1, high: 3, medium: 0, low: 0 });
    expect(page.items[0]).toMatchObject({ serverId: 1, cve: 'CVE-1', source: 'docker', imageRef: 'nginx:1', severity: 'critical' });
  });

  it('survives empty and broken answers', () => {
    expect(normaliseCvePage(null)).toEqual({ items: [], total: 0, page: 1, summary: { critical: 0, high: 0, medium: 0, low: 0 } });
    expect(normaliseCvePage({ items: null }).items).toEqual([]);
  });

  it('keeps rows with the same CVE on different servers apart', () => {
    const page = normaliseCvePage({ items: [{ server_id: 1, cve_id: 'CVE-1', package_name: 'a' }, { server_id: 2, cve_id: 'CVE-1', package_name: 'a' }] });
    expect(new Set(page.items.map((i) => i.id)).size).toBe(2);
  });

  it('maps unknown severities to unknown', () => {
    expect(severityOf('critical')).toBe('critical');
    expect(severityOf('negligible')).toBe('unknown');
  });
});

describe('normaliseBulk', () => {
  it('reads a running update', () => {
    const status = normaliseBulk({ running: true, total: 10, completed: 3, failed: 1, current: 'web01', actor: 'admin', started_at: '2026-11-01T10:00:00Z' });
    expect(status).toMatchObject({ running: true, total: 10, completed: 3, failed: 1, detailed: true });
    expect(bulkPercent(status)).toBe(40);
  });

  it('knows a restricted view: only "running" arrives', () => {
    const status = normaliseBulk({ running: true, total: 0, completed: 0, failed: 0, finished_at: null });
    expect(status.detailed).toBe(false);
    expect(bulkPercent(status)).toBe(0);
  });

  it('reads a finished update', () => {
    const status = normaliseBulk({ running: false, total: 2, completed: 2, failed: 0, finished_at: '2026-11-01T10:05:00Z' });
    expect(status).toMatchObject({ running: false, finishedAt: '2026-11-01T10:05:00Z', detailed: true });
    expect(bulkPercent(status)).toBe(100);
  });

  it('treats the zero time as missing', () => {
    expect(normaliseBulk({ finished_at: '0001-01-01T00:00:00Z' }).finishedAt).toBeNull();
  });
});

describe('normaliseScanner', () => {
  it('reads the state', () => {
    const scanner = normaliseScanner({ available: true, version: '0.72.0', updated_at: '2026-10-30T00:00:00Z', freshness: 'stale', age_hours: 60, sandboxed: true, sandbox_backend: 'bubblewrap' });
    expect(scanner).toMatchObject({ available: true, freshness: 'stale', ageHours: 60, sandboxed: true, sandboxBackend: 'bubblewrap' });
  });

  it('falls back to unknown freshness', () => {
    expect(normaliseScanner({ freshness: 'seltsam' }).freshness).toBe('unknown');
    expect(normaliseScanner({}).available).toBe(false);
  });
});

describe('normaliseAdvisoryPage', () => {
  it('reads items and status', () => {
    const page = normaliseAdvisoryPage({
      items: [{ id: 'a1', kind: 'malware', advisory_id: 'MAL-1', server_id: 3, exploited: true, acknowledged_by: 'tony', resolved_at: null }],
      total: 1,
      summary: { high: 1 },
      status: { enabled: true, local_copy: true, last_poll_at: '2026-11-01T09:00:00Z' },
    });
    expect(page.items[0]).toMatchObject({ id: 'a1', malware: true, exploited: true, acknowledgedBy: 'tony', resolvedAt: null });
    expect(page.status).toEqual({ enabled: true, localCopy: true, lastPollAt: '2026-11-01T09:00:00Z', mirroredAt: null });
  });

  it('survives the empty list of a switched-off early warning', () => {
    const page = normaliseAdvisoryPage({ items: null, total: 0, status: { enabled: false, mirrorable: null } });
    expect(page.items).toEqual([]);
    expect(page.status.enabled).toBe(false);
  });
});

describe('caches', () => {
  it('reads 64-bit counters that arrive as text', () => {
    const report = normaliseCaches({ scan: { hits: 3, misses: 1, entries: 2, limit: 256 }, advisory: { hits: '30', misses: '10', runs: '4', since_at: '0001-01-01T00:00:00Z' }, snapshot: { entries: 5, fresh: 4, details: 9 }, ttl_minutes: 20 });
    expect(report.advisory).toEqual({ hits: 30, misses: 10, runs: 4, sinceAt: null });
    expect(report.ttlMinutes).toBe(20);
  });

  it('has no rate before the first access', () => {
    expect(hitRate(0, 0)).toBeNull();
    expect(hitRate(3, 1)).toBe(75);
    expect(hitRate(0, 4)).toBe(0);
  });
});
