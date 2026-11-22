import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { readAptChannels, readSubscription } from '../../api/settings';

const live = vi.hoisted(() => ({ subscription: undefined as unknown, channels: undefined as unknown }));
const activateSubscription = vi.fn();
const switchAptChannel = vi.fn();
const watchJob = vi.fn();

vi.mock('../../api/settings', async (original) => ({
  ...(await original<typeof import('../../api/settings')>()),
  settingsApi: { activateSubscription: (key: string) => activateSubscription(key), switchAptChannel: (channel: string) => switchAptChannel(channel) },
}));
vi.mock('../../lib/job-watch', () => ({ watchJob: (id: string, done: (status: string) => void) => watchJob(id, done) }));
vi.mock('../../components/lcm-toaster', () => ({ toast: () => undefined }));
// Die beiden Abrufe der Seite: der Vertragsstand und der Stand der Kanäle (Adresse mit `/apt/channels`).
vi.mock('../../lib/live-data', () => ({
  LiveData: class {
    error = '';
    #options: { url: () => string | null };
    constructor(_host: unknown, options: { url: () => string | null }) {
      this.#options = options;
    }
    get data(): unknown {
      return this.#options.url()?.includes('/apt/channels') ? live.channels : live.subscription;
    }
    reload(): void {}
  },
}));

const { SettingsSubscription } = await import('./subscription');

const subscription = (over: Record<string, unknown> = {}) => readSubscription({ configured: false, apt_available: true, apt_channel: 'community', ...over });
const channels = () =>
  readAptChannels({
    installed: '2.3.0',
    channels: [
      { channel: 'community', latest: '2.3.0' },
      { channel: 'beta', latest: '2.4.0-beta.1' },
      { channel: 'enterprise', latest: '2.3.0' },
    ],
  });

async function mount(embedded: boolean): Promise<InstanceType<typeof SettingsSubscription>> {
  const page = new SettingsSubscription();
  page.embedded = embedded;
  document.body.append(page);
  await page.updateComplete;
  return page;
}

const root = (page: HTMLElement) => page.shadowRoot!;
const radio = (page: HTMLElement, index: number) => root(page).querySelectorAll<HTMLInputElement>('input[type="radio"]')[index]!;

async function pick(page: InstanceType<typeof SettingsSubscription>, index: number): Promise<void> {
  radio(page, index).dispatchEvent(new Event('change'));
  await page.updateComplete;
}

async function activate(page: InstanceType<typeof SettingsSubscription>): Promise<void> {
  root(page).querySelector('lcm-save-bar')!.dispatchEvent(new CustomEvent('lcm-save'));
  await vi.waitFor(() => expect(activateSubscription).toHaveBeenCalled());
  await vi.waitFor(() => expect(root(page).querySelector('lcm-save-bar')?.hasAttribute('busy')).toBe(false));
  await page.updateComplete;
}

beforeEach(() => {
  live.subscription = subscription();
  live.channels = channels();
  activateSubscription.mockReset().mockResolvedValue(undefined);
  switchAptChannel.mockReset().mockResolvedValue({ jobId: 'job-1' });
  watchJob.mockReset().mockReturnValue(() => undefined);
});
afterEach(() => document.body.replaceChildren());

describe('Subscription im Assistenten (embedded)', () => {
  it('wählt nur aus: kein Sofort-Wechsel, kein Knopf zum Anwenden', async () => {
    const page = await mount(true);
    await pick(page, 1);
    expect(radio(page, 1).checked).toBe(true);
    expect(switchAptChannel).not.toHaveBeenCalled();
    expect(root(page).querySelector('lcm-server-action')).toBeNull();
    expect(root(page).querySelector('[data-field="apply-hint"]')).not.toBeNull();
  });

  it('applyPicked stellt auf den gewählten Kanal um und wartet auf den Job', async () => {
    const page = await mount(true);
    await pick(page, 1);
    await page.applyPicked();
    expect(switchAptChannel).toHaveBeenCalledExactlyOnceWith('beta');
    expect(watchJob).toHaveBeenCalledWith('job-1', expect.any(Function));
  });

  it('applyPicked tut bei unveränderter Wahl nichts', async () => {
    const page = await mount(true);
    await page.applyPicked();
    await pick(page, 0);
    await page.applyPicked();
    expect(switchAptChannel).not.toHaveBeenCalled();
  });

  it('ein Fehler der Umstellung fliegt zum Schritt', async () => {
    switchAptChannel.mockImplementation(async () => {
      throw new Error('gesperrt');
    });
    const page = await mount(true);
    await pick(page, 1);
    await expect(page.applyPicked()).rejects.toThrow('gesperrt');
  });

  it('nach der Aktivierung empfiehlt sie Enterprise und wählt es vor, umgestellt wird erst mit applyPicked', async () => {
    const page = await mount(true);
    expect(root(page).querySelector('[data-field="recommend"]')).toBeNull();
    await activate(page);
    live.subscription = subscription({ configured: true, status: 'active' });
    await page.requestUpdate();
    await page.updateComplete;
    expect(root(page).querySelector('[data-field="recommend"]')).not.toBeNull();
    expect(radio(page, 2).checked).toBe(true);
    expect(switchAptChannel).not.toHaveBeenCalled();
    await page.applyPicked();
    expect(switchAptChannel).toHaveBeenCalledExactlyOnceWith('enterprise');
  });
});

describe('Subscription als Einstellungsseite', () => {
  it('stellt weiter über den Knopf um, mit Bestätigung, und empfiehlt nach der Aktivierung nichts', async () => {
    const page = await mount(false);
    expect(root(page).querySelector('lcm-server-action')).not.toBeNull();
    expect(root(page).querySelector('[data-field="apply-hint"]')).toBeNull();
    await activate(page);
    expect(root(page).querySelector('[data-field="recommend"]')).toBeNull();
    expect(radio(page, 0).checked).toBe(true);
  });
});
