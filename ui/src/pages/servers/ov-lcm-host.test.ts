import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const installTrivy = vi.fn();
const installSandbox = vi.fn();
const FRESH = { is_lcm_host: true, package_manager: 'apt', in_container: false, trivy_installed: false, sandbox_retrofit: false, apt_cacher_installed: false, crowdsec_lapi_installed: false, scanner: null };
let status: Record<string, unknown> = FRESH;

vi.mock('../../api/server-detail', async (original) => ({
  ...(await original<typeof import('../../api/server-detail')>()),
  detailApi: { installTrivy: (id: number) => installTrivy(id), installSandbox: (id: number) => installSandbox(id) },
}));
vi.mock('../../lib/auth', () => ({
  auth: { can: () => true, onChange: () => () => undefined },
}));
vi.mock('./set-action', () => ({}));
vi.mock('../../lib/live-data', () => ({
  LiveData: class {
    get data() {
      return status;
    }
    error = '';
    reload(): void {}
  },
}));

const { SrvLcmHost } = await import('./ov-lcm-host');

async function mount(): Promise<InstanceType<typeof SrvLcmHost>> {
  const card = new SrvLcmHost();
  card.server = { id: 1, name: 'lcm' } as never;
  card.canWrite = true;
  document.body.append(card);
  await card.updateComplete;
  return card;
}

beforeEach(() => {
  status = FRESH;
  installTrivy.mockReset();
  installSandbox.mockReset();
});
afterEach(() => document.body.replaceChildren());

const labelsOf = (card: Element) => [...card.shadowRoot!.querySelectorAll('lcm-set-action')].map((action) => action.getAttribute('label'));

describe('LCM-Host-Karte', () => {
  it('zeigt ihre Knöpfe selbst und trägt kein Einführungsfenster', async () => {
    const card = await mount();
    const root = card.shadowRoot!;
    expect(root.querySelector('lcm-card')!.getAttribute('heading')).toBe('serverDetail.lcmHost.title');
    expect(root.querySelector('lcm-srv-lcm-tour')).toBeNull();
    expect(labelsOf(card)).toEqual(['serverDetail.lcmHost.trivy.label', 'serverDetail.lcmHost.aptCacher.label', 'serverDetail.lcmHost.lapi.label']);
  });

  it('trägt die Ziele der Einführung: Karte und Dienste', async () => {
    const card = await mount();
    const root = card.shadowRoot!;
    expect(root.querySelector('[data-card="lcm-host"]')).not.toBeNull();
    for (const service of ['trivy', 'aptCacher', 'lapi']) expect(root.querySelector(`[data-service="${service}"]`), service).not.toBeNull();
  });

  it('im Container und ohne apt steht nur die Erklärung da', async () => {
    status = { ...FRESH, in_container: true };
    const card = await mount();
    expect(card.shadowRoot!.querySelectorAll('lcm-set-action')).toHaveLength(0);
    expect(card.shadowRoot!.querySelector('lcm-notice')).not.toBeNull();
  });
});

describe('Trivy mit Sandbox', () => {
  it('bietet das Installieren mit Sandbox als Standard an und das Nachrüsten nicht', async () => {
    const card = await mount();
    const labels = [...card.shadowRoot!.querySelectorAll('lcm-set-action')].map((action) => action.getAttribute('label'));
    expect(labels).toContain('serverDetail.lcmHost.trivy.label');
    expect(labels).not.toContain('serverDetail.lcmHost.sandbox.label');
  });

  it('rüstet die Sandbox nur nach, wenn Trivy ohne sie installiert ist', async () => {
    status = { ...FRESH, trivy_installed: true, sandbox_retrofit: true, scanner: null };
    const card = await mount();
    const labels = [...card.shadowRoot!.querySelectorAll('lcm-set-action')].map((action) => action.getAttribute('label'));
    expect(labels).toContain('serverDetail.lcmHost.sandbox.label');
    expect(labels).not.toContain('serverDetail.lcmHost.trivy.label');
  });
});

describe('Sandbox nachrüsten', () => {
  const OFF = { available: true, version: '0.60.0', update_version: '', updated_at: null, freshness: 'fresh', sandboxed: false, sandbox_backend: '', sandbox_note: '' };

  it('erscheint, sobald Trivy da ist und der Scanner nicht in der Sandbox läuft - gleich, wer Trivy eingerichtet hat', async () => {
    status = { ...FRESH, trivy_installed: true, sandbox_retrofit: true, scanner: OFF };
    expect(labelsOf(await mount())).toContain('serverDetail.lcmHost.sandbox.label');
  });

  it('erscheint auch, wenn nur der Scanner „ohne Sandbox“ meldet', async () => {
    status = { ...FRESH, trivy_installed: true, sandbox_retrofit: false, scanner: OFF };
    expect(labelsOf(await mount())).toContain('serverDetail.lcmHost.sandbox.label');
  });

  it('bleibt weg, wo die Sandbox trägt, und ohne Trivy', async () => {
    status = { ...FRESH, trivy_installed: true, sandbox_retrofit: false, scanner: { ...OFF, sandboxed: true, sandbox_backend: 'bubblewrap' } };
    expect(labelsOf(await mount())).not.toContain('serverDetail.lcmHost.sandbox.label');
    document.body.replaceChildren();
    status = { ...FRESH, trivy_installed: false, scanner: null };
    expect(labelsOf(await mount())).not.toContain('serverDetail.lcmHost.sandbox.label');
  });
});
