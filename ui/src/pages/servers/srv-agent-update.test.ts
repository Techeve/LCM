import { afterEach, describe, expect, it, vi } from 'vitest';

const fetchPlan = vi.hoisted(() => vi.fn());
const startUpdate = vi.hoisted(() => vi.fn());
const announce = vi.hoisted(() => vi.fn());

vi.mock('../../api/agent-update', async (original) => ({
  ...(await original<typeof import('../../api/agent-update')>()),
  fetchAgentUpdatePlan: fetchPlan,
  startAgentUpdate: startUpdate,
}));
vi.mock('../../api/servers', () => ({ refreshServer: vi.fn() }));
vi.mock('../../lib/job-toast', () => ({ announceJob: announce }));

import './srv-agent-update';
import type { SrvAgentUpdate } from './srv-agent-update';
import type { LcmButton } from '../../components/lcm-button';
import type { LcmDialog } from '../../components/lcm-dialog';
import { i18n } from '../../lib/i18n';

let element: SrvAgentUpdate;

afterEach(() => {
  element?.remove();
  fetchPlan.mockReset();
  startUpdate.mockReset();
  announce.mockReset();
});

async function mount(): Promise<SrvAgentUpdate> {
  await i18n.setLanguage('de');
  element = document.createElement('lcm-srv-agent-update') as SrvAgentUpdate;
  element.serverId = 7;
  element.serverName = 'web01';
  document.body.append(element);
  await element.updateComplete;
  return element;
}

async function click(): Promise<LcmDialog> {
  element.shadowRoot!.querySelector<LcmButton>('lcm-button')!.click();
  await vi.waitFor(() => expect(element.shadowRoot!.querySelector('lcm-dialog')).not.toBeNull());
  await element.updateComplete;
  return element.shadowRoot!.querySelector<LcmDialog>('lcm-dialog')!;
}

describe('lcm-srv-agent-update', () => {
  it('zeigt „von → nach“ und startet das Update erst nach der Bestätigung', async () => {
    fetchPlan.mockResolvedValue({ action: 'update', reason: '', installed: '2.2.0', target: '2.3.0', server_version: '2.3.0', available: ['2.3.0'], warnings: [] });
    startUpdate.mockResolvedValue({ status: 'started', job_id: 'job-1', job_name: 'Agent aktualisieren @ web01' });
    await mount();
    const dialog = await click();

    expect(startUpdate).not.toHaveBeenCalled();
    expect(dialog.open).toBe(true);
    expect(dialog.impacts.some((impact) => impact.value === '2.2.0 → 2.3.0')).toBe(true);
    expect(dialog.confirmDisabled).toBe(false);

    dialog.dispatchEvent(new CustomEvent('lcm-confirm'));
    await vi.waitFor(() => expect(startUpdate).toHaveBeenCalledWith(7));
    expect(announce).toHaveBeenCalledWith('Agent aktualisieren', 'web01', 'job-1');
  });

  it.each([
    ['current', ''],
    ['newer', ''],
    ['none', 'no_version'],
    ['unsupported', 'not_packaged'],
  ])('bietet bei „%s“ nichts zum Bestätigen an', async (action, reason) => {
    fetchPlan.mockResolvedValue({ action, reason, installed: '2.3.0', target: '', server_version: '2.3.0', available: [], warnings: [] });
    await mount();
    const dialog = await click();
    expect(dialog.confirmDisabled).toBe(true);
    expect(dialog.message).not.toBe('');
    dialog.dispatchEvent(new CustomEvent('lcm-confirm'));
    await Promise.resolve();
    expect(startUpdate).not.toHaveBeenCalled();
  });

  it('zeigt einen Fehler des Starts im Dialog', async () => {
    fetchPlan.mockResolvedValue({ action: 'update', reason: '', installed: '2.2.0', target: '2.3.0', server_version: '2.3.0', available: [], warnings: [] });
    startUpdate.mockRejectedValue(new Error('Auf diesem Server läuft schon ein Job'));
    await mount();
    const dialog = await click();
    dialog.dispatchEvent(new CustomEvent('lcm-confirm'));
    await vi.waitFor(() => expect(startUpdate).toHaveBeenCalled());
    await vi.waitFor(async () => {
      await element.updateComplete;
      expect(element.shadowRoot!.querySelector<LcmDialog>('lcm-dialog')!.error).not.toBe('');
    });
    expect(element.shadowRoot!.querySelector<LcmDialog>('lcm-dialog')!.open).toBe(true);
  });
});
