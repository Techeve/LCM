import { describe, expect, it } from 'vitest';
import { jobsUrl, liveOutput, normaliseJob, normaliseJobs, normaliseServers, type JobsQuery, type SshSession } from './jobs';

const base: JobsQuery = { status: [], kind: [], q: '', since: '', until: '', hideHealth: false, sort: '-started_at', limit: 50, offset: 0 };

describe('normaliseJobs', () => {
  const old = {
    id: 'a',
    server_id: 3,
    server: { id: 3, name: 'web01', host: '10.0.0.1' },
    type: 'update',
    name: 'Updates',
    status: 'success',
    triggered_by: 'admin',
    created_at: '2026-11-01T10:00:00Z',
    started_at: '2026-11-01T10:00:00Z',
    finished_at: '2026-11-01T10:01:30Z',
    exit_code: 0,
  };

  it('reads the envelope', () => {
    const page = normaliseJobs({ items: [old], total: 120 });
    expect(page.total).toBe(120);
    expect(page.items[0]).toMatchObject({ id: 'a', kind: 'update', server_name: 'web01', duration_seconds: 90, exit_code: 0 });
  });

  it('reads the old history shape with its extra fields', () => {
    expect(normaliseJobs({ items: [old], total: 1, page: 1, page_size: 100 }).total).toBe(1);
  });

  it('reads a bare list and counts it', () => {
    expect(normaliseJobs([old, old]).total).toBe(2);
  });

  it('survives nonsense', () => {
    expect(normaliseJobs(null)).toEqual({ items: [], total: 0 });
    expect(normaliseJobs({ items: 'x' })).toEqual({ items: [], total: 0 });
  });

  it('prefers the flat fields of the new shape', () => {
    const job = normaliseJob({ id: 'b', kind: 'reboot', server_name: 'db01', duration_seconds: 7, status: 'running' });
    expect(job).toMatchObject({ kind: 'reboot', server_name: 'db01', duration_seconds: 7, started_at: null });
  });
});

describe('jobsUrl', () => {
  it('leaves empty filters out', () => {
    expect(jobsUrl(base)).toBe('/api/v1/jobs?sort=-started_at&limit=50&offset=0');
  });

  it('joins lists with commas', () => {
    const url = jobsUrl({ ...base, serverId: 4, status: ['failed', 'aborted'], kind: ['update'], q: 'nacht', triggeredBy: 'scheduler', hideHealth: true, offset: 100 });
    const params = new URL(url, 'http://x').searchParams;
    expect(params.get('server_id')).toBe('4');
    expect(params.get('status')).toBe('failed,aborted');
    expect(params.get('kind')).toBe('update');
    expect(params.get('q')).toBe('nacht');
    expect(params.get('triggered_by')).toBe('scheduler');
    expect(params.get('hide_health')).toBe('1');
    expect(params.get('offset')).toBe('100');
  });
});

describe('normaliseServers', () => {
  it('accepts a list or an envelope and sorts by name', () => {
    expect(normaliseServers([{ id: 2, name: 'b' }, { id: 1, name: 'a' }]).map((s) => s.name)).toEqual(['a', 'b']);
    expect(normaliseServers({ items: [{ id: 1, name: 'a' }] })).toEqual([{ id: 1, name: 'a' }]);
  });
});

describe('liveOutput', () => {
  const sessions = [
    {
      commands: [
        { seq: 2, started_at: '2026-11-01T10:00:02Z', command: 'apt-get upgrade', output: 'done\n' },
        { seq: 1, started_at: '2026-11-01T10:00:01Z', command: 'apt-get update', output: 'ok' },
      ],
    },
  ] as unknown as SshSession[];

  it('uses the stored output once the job is finished', () => {
    expect(liveOutput({ job_id: 'a', name: '', status: 'success', exit_code: 0, output: 'final' }, sessions)).toBe('final');
  });

  it('builds the output from the ssh commands while the job runs', () => {
    expect(liveOutput(undefined, sessions)).toBe('$ apt-get update\nok\n\n$ apt-get upgrade\ndone');
  });
});
