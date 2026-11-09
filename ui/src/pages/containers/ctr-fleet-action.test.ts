import { describe, expect, it } from 'vitest';
import { runOnServers } from './ctr-fleet-action';

describe('runOnServers', () => {
  it('runs on every server and collects failures without stopping', async () => {
    const calls: number[] = [];
    const result = await runOnServers(
      [
        { id: 1, name: 'web01' },
        { id: 2, name: 'db01' },
        { id: 3, name: 'cache01' },
      ],
      async (id) => {
        calls.push(id);
        if (id === 2) throw new Error('busy');
        return { status: 'started', job_id: `j${id}`, job_name: '' };
      },
    );
    expect(calls).toEqual([1, 2, 3]);
    expect(result.started.map((s) => s.name)).toEqual(['web01', 'cache01']);
    expect(result.failed).toHaveLength(1);
    expect(result.failed[0]!.server.name).toBe('db01');
    expect(result.failed[0]!.error).not.toBe('');
  });
});
