import { afterEach, describe, expect, it, vi } from 'vitest';
import { normaliseServerState } from '../../api/servers-settings';

const state = normaliseServerState({ name: 'lcm', host: 'lcm.example', os_id: 'debian', transport: 'ssh' });

vi.mock('../../lib/live-data', () => ({
  LiveData: class {
    get data() {
      return state;
    }
    error = '';
  },
}));
vi.mock('../../lib/auth', () => ({ auth: { can: () => true, onChange: () => () => undefined } }));
vi.mock('./set-access', () => ({}));
vi.mock('./set-dns', () => ({}));
vi.mock('./set-time', () => ({}));

const { SrvSettings } = await import('./srv-settings');

async function mount(props: Partial<InstanceType<typeof SrvSettings>>): Promise<InstanceType<typeof SrvSettings>> {
  const settings = new SrvSettings();
  Object.assign(settings, { serverId: 7, open: true }, props);
  document.body.append(settings);
  await settings.updateComplete;
  await settings.updateComplete;
  return settings;
}
const tabs = (settings: Element) => settings.shadowRoot!.querySelector('lcm-modal lcm-tabs') as HTMLElement & { value: string };

afterEach(() => document.body.replaceChildren());

describe('Einstellungsfenster eines Servers', () => {
  it('beginnt bei „Allgemein“ und folgt `section`, auch bei einem späteren Wechsel', async () => {
    const settings = await mount({});
    expect(tabs(settings).getAttribute('value')).toBe('general');
    settings.section = 'dns';
    await settings.updateComplete;
    await settings.updateComplete;
    expect(tabs(settings).getAttribute('value')).toBe('dns');
    settings.section = 'operation';
    await settings.updateComplete;
    await settings.updateComplete;
    expect(tabs(settings).getAttribute('value')).toBe('operation');
  });

  it('übernimmt eine unbekannte Seite nicht', async () => {
    const settings = await mount({ section: 'gibt-es-nicht' });
    expect(tabs(settings).getAttribute('value')).toBe('general');
  });

  it('meldet den Wechsel der Seite, damit die Adresse folgen kann', async () => {
    const settings = await mount({});
    const seen: string[] = [];
    settings.addEventListener('lcm-section', (event) => seen.push((event as CustomEvent<{ section: string }>).detail.section));
    tabs(settings).dispatchEvent(new CustomEvent('lcm-tab-change', { detail: { id: 'access' } }));
    expect(seen).toEqual(['access']);
  });

  it('ist während der Einführung nicht modal', async () => {
    const settings = await mount({ tour: true });
    expect(settings.shadowRoot!.querySelector('lcm-modal')!.hasAttribute('non-modal')).toBe(true);
    const plain = await mount({});
    expect(plain.shadowRoot!.querySelector('lcm-modal')!.hasAttribute('non-modal')).toBe(false);
  });

  it('trägt die Ziele der Einführung an den Schaltern der Seite „Betrieb“', async () => {
    const settings = await mount({ section: 'operation' });
    const modal = settings.shadowRoot!.querySelector('lcm-modal')!;
    for (const flag of ['maintenance', 'unreachable_uncritical', 'user_sync_disabled']) expect(modal.querySelector(`[data-tour="flag-${flag}"]`), flag).not.toBeNull();
  });
});
