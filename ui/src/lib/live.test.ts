import { describe, expect, it } from 'vitest';
import type { EventMessage } from './events';
import { targetsFor } from './live';

const message = (event: string, topic: string): EventMessage => ({ type: 'event', event, topic, data: {} });

describe('targetsFor', () => {
  it('maps a server status to the server, the list and the dashboard', () => {
    const targets = targetsFor(message('server.status', 'server:42'));
    expect(targets).toContainEqual({ prefix: '/api/v1/servers/42' });
    expect(targets).toContainEqual({ prefix: '/api/v1/servers', withChildren: false });
    expect(targets).toContainEqual({ prefix: '/api/v1/dashboard' });
  });

  it('skips the single server when the topic names none', () => {
    const prefixes = targetsFor(message('server.busy', 'servers')).map((t) => t.prefix);
    expect(prefixes).not.toContain('/api/v1/servers/undefined');
    expect(prefixes).toContain('/api/v1/servers');
  });

  it('refreshes the job list, but not job details, when a server becomes busy', () => {
    const targets = targetsFor(message('server.busy', 'job:abc'));
    expect(targets).toContainEqual({ prefix: '/api/v1/jobs', withChildren: false });
    expect(targetsFor(message('server.status', 'server:1')).map((t) => t.prefix)).not.toContain('/api/v1/jobs');
  });

  it('maps jobs, alerts and bulk', () => {
    expect(targetsFor(message('job.progress', 'job:1')).map((t) => t.prefix)).toEqual(['/api/v1/jobs']);
    expect(targetsFor(message('job.finished', 'jobs')).map((t) => t.prefix)).toContain('/api/v1/dashboard');
    expect(targetsFor(message('alert.fired', 'alerts')).map((t) => t.prefix)).toContain('/api/v1/alert-events');
    expect(targetsFor(message('bulk.progress', 'bulk'))).toHaveLength(1);
  });

  it('reloads the lock state on system.maintenance', () => {
    expect(targetsFor(message('system.maintenance', 'system')).map((t) => t.prefix)).toEqual(['/api/v1/system/maintenance']);
  });

  it('reloads the self-update state on system.build', () => {
    expect(targetsFor(message('system.build', 'system')).map((t) => t.prefix)).toEqual(['/api/v1/system/self-update', '/api/v1/system/update-info']);
  });

  it('knows nothing about unrelated events', () => {
    expect(targetsFor(message('something.else', 'system'))).toEqual([]);
  });
});
