import { beforeEach, describe, expect, it, vi } from 'vitest';
import { actionFor, serverHref } from './attention-actions';

const can = vi.hoisted(() => vi.fn((_permission: string) => true));
vi.mock('../../lib/auth', () => ({ auth: { can } }));

describe('actionFor', () => {
  beforeEach(() => can.mockReturnValue(true));

  it('führt ohne jobs:read zur Übersicht statt in den Reiter Jobs', () => {
    can.mockReturnValue(false);
    expect(actionFor('actionFailed.rules').tab).toBe('');
  });

  it('sends vulnerabilities to the security tab and disks to storage', () => {
    expect(actionFor('criticalVulns').tab).toBe('security');
    expect(actionFor('diskLow').tab).toBe('storage');
  });

  it('knows the failure categories by their full key', () => {
    expect(actionFor('actionFailed.rules').tab).toBe('jobs');
    expect(actionFor('actionFailed.connection').labelKey).toBe('dashboard.action.connection');
  });

  it('falls back to the server overview', () => {
    expect(actionFor('somethingNew').tab).toBe('');
    expect(actionFor(undefined).labelKey).toBe('dashboard.action.overview');
  });
});

describe('serverHref', () => {
  it('builds the detail address with or without a tab', () => {
    expect(serverHref(7)).toBe('/servers/7');
    expect(serverHref(7, 'packages')).toBe('/servers/7/packages');
  });
});
