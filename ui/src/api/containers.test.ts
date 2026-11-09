import { describe, expect, it } from 'vitest';
import { imageStatus, isIncomplete, normaliseCompose, normaliseContainers, normaliseImages, stateLevel } from './containers';

describe('normaliseImages', () => {
  it('builds the name from repository and tag', () => {
    const [image] = normaliseImages([{ repository: 'nginx', tag: '1.25', servers: [{ id: 1, name: 'web01' }], server_count: 1, in_use_count: 2, update_available: true, high_vulns: 1 }]);
    expect(image).toMatchObject({ id: 'nginx:1.25', name: 'nginx:1.25', inUse: 2, status: 'update', high: 1, critical: 0 });
    expect(image.servers).toEqual([{ id: 1, name: 'web01' }]);
  });

  it('leaves out an empty tag', () => {
    expect(normaliseImages([{ repository: 'redis', tag: '' }])[0].name).toBe('redis');
  });

  it('survives anything that is not a list', () => {
    expect(normaliseImages(null)).toEqual([]);
    expect(normaliseImages({ items: [] })).toEqual([]);
  });
});

describe('imageStatus', () => {
  it('puts an available update first', () => {
    expect(imageStatus({ update_available: true, unverifiable: true, local_only: true })).toBe('update');
  });

  it('tells the other states apart', () => {
    expect(imageStatus({ unverifiable: true })).toBe('unverifiable');
    expect(imageStatus({ local_only: true })).toBe('local');
    expect(imageStatus({})).toBe('current');
  });
});

describe('containers', () => {
  it('reads a container with its compose project', () => {
    const [item] = normaliseContainers([{ server_id: 1, server_name: 'web01', name: 'web-1', image: 'nginx:1', state: 'running', compose_project: 'shop', compose_service: 'web' }]);
    expect(item).toMatchObject({ id: '1/web-1', serverName: 'web01', project: 'shop', service: 'web' });
  });

  it('rates the states', () => {
    expect(stateLevel('running')).toBe('ok');
    expect(stateLevel('restarting')).toBe('crit');
    expect(stateLevel('dead')).toBe('crit');
    expect(stateLevel('paused')).toBe('warn');
    expect(stateLevel('exited')).toBe('unknown');
  });
});

describe('compose projects', () => {
  it('marks a project where not every container runs', () => {
    const [project] = normaliseCompose([{ project: 'shop', services: ['web', 'db'], containers: 3, running: 2 }]);
    expect(isIncomplete(project)).toBe(true);
    expect(project.services).toEqual(['web', 'db']);
  });

  it('does not mark a complete project', () => {
    expect(isIncomplete(normaliseCompose([{ project: 'a', containers: 2, running: 2 }])[0])).toBe(false);
  });
});
