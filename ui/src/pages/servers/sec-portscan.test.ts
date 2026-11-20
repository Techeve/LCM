import { afterEach, describe, expect, it, vi } from 'vitest';
import './sec-portscan';
import type { SrvPortScan } from './sec-portscan';
import { i18n } from '../../lib/i18n';

const scan = {
  scanned_at: '2026-11-08T10:00:00Z',
  targets: [{ address: '192.0.2.10', family: 'v4', open: [80], closed: 29, filtered: 0 }],
  open_ports: [{ port: 80, service: 'http', families: ['v4'], in_firewall: false, risky: false, severity: 'warning', reasons: ['not_in_firewall'] }],
  firewall_compared: true,
  firewall_active: false,
  critical: 0,
  warnings: 1,
};

let card: SrvPortScan;

async function mount(id: number, canWrite: boolean, body: unknown): Promise<string[]> {
  const fetchMock = vi.fn(async (_url: string) => new Response(JSON.stringify(body), { status: 200, headers: { 'content-type': 'application/json' } }));
  vi.stubGlobal('fetch', fetchMock);
  await i18n.setLanguage('de');
  card = document.createElement('lcm-srv-portscan') as SrvPortScan;
  card.serverId = id;
  card.serverName = 'web01';
  card.canWrite = canWrite;
  document.body.append(card);
  await vi.waitFor(() => expect(card.shadowRoot!.querySelector('lcm-data-table, lcm-empty-state')).not.toBeNull());
  return fetchMock.mock.calls.map(([url]) => String(url));
}

afterEach(() => {
  card?.remove();
  vi.unstubAllGlobals();
});

describe('lcm-srv-portscan', () => {
  it('shows the open ports, the warning about the switched off firewall and the action', async () => {
    const urls = await mount(31, true, scan);
    const root = card.shadowRoot!;
    expect(urls.some((url) => url.endsWith('/api/v1/servers/31/port-scan'))).toBe(true);
    expect(root.querySelector('lcm-data-table')).not.toBeNull();
    expect(root.querySelector('lcm-notice[tone=warn]')?.textContent).toContain('nicht eingeschaltet');
    expect(root.querySelector('lcm-srv-action')?.getAttribute('label')).toBe('Ports prüfen');
  });

  it('offers no action without write permission and explains a missing scan', async () => {
    await mount(32, false, { scanned_at: null, targets: [], open_ports: [], firewall_compared: true });
    const root = card.shadowRoot!;
    expect(root.querySelector('lcm-srv-action')).toBeNull();
    expect(root.querySelector('lcm-empty-state')?.getAttribute('heading')).toBe('Noch nicht geprüft');
  });
});
