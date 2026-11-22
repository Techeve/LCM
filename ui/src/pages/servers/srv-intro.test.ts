import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const patchGlobal = vi.fn();
const refresh = vi.fn();
const navigate = vi.fn();
const setQuery = vi.fn();
const FRESH = { is_lcm_host: true, package_manager: 'apt', in_container: false, trivy_installed: false, sandbox_retrofit: false, apt_cacher_installed: false, crowdsec_lapi_installed: false, scanner: null };
let status: Record<string, unknown> | null = FRESH;
let statusError = '';

vi.mock('../../api/settings', async (original) => ({
  ...(await original<typeof import('../../api/settings')>()),
  settingsApi: { patchGlobal: (body: unknown) => patchGlobal(body) },
}));
vi.mock('../../lib/auth', () => ({
  auth: { can: () => true, onChange: () => () => undefined, refresh: () => refresh() },
}));
vi.mock('../../lib/router', () => ({
  router: { navigate: (...args: unknown[]) => navigate(...args), setQuery: (...args: unknown[]) => setQuery(...args) },
}));
vi.mock('../../lib/i18n', async (original) => ({
  ...(await original<typeof import('../../lib/i18n')>()),
  t: (key: string, params?: Record<string, unknown>) => (params ? `${key} ${JSON.stringify(params)}` : key),
}));
vi.mock('../../lib/live-data', () => ({
  LiveData: class {
    get data() {
      return status;
    }
    get error() {
      return statusError;
    }
  },
}));

const { SrvIntro, viewPath } = await import('./srv-intro');

const SERVER = { id: 7, name: 'lcm', os_id: 'debian', proxmox_type: '', has_snap: false, has_docker: false };

async function mount(): Promise<InstanceType<typeof SrvIntro>> {
  const intro = new SrvIntro();
  intro.server = SERVER as never;
  document.body.append(intro);
  await intro.updateComplete;
  return intro;
}
const tourOf = (intro: Element) => intro.shadowRoot!.querySelector('lcm-srv-lcm-tour') as (HTMLElement & { updateComplete: Promise<boolean> }) | null;
const field = (intro: Element, name: string) => tourOf(intro)!.shadowRoot!.querySelector(`[data-field="${name}"]`)?.textContent?.trim();

async function press(intro: InstanceType<typeof SrvIntro>, action: string): Promise<void> {
  tourOf(intro)!.shadowRoot!.querySelector<HTMLElement>(`lcm-button[data-action="${action}"]`)!.click();
  await intro.updateComplete;
  await tourOf(intro)!.updateComplete;
}

beforeEach(() => {
  status = FRESH;
  statusError = '';
  patchGlobal.mockReset().mockResolvedValue({});
  refresh.mockReset().mockResolvedValue(undefined);
  navigate.mockReset().mockResolvedValue(undefined);
  setQuery.mockReset();
});
afterEach(() => document.body.replaceChildren());

describe('viewPath', () => {
  it('nennt den Reiter und für die Einstellungen das offene Fenster auf seiner Seite', () => {
    expect(viewPath(7, { tab: 'overview' })).toBe('/servers/7');
    expect(viewPath(7, { tab: 'packages' })).toBe('/servers/7/packages');
    expect(viewPath(7, { tab: 'overview', section: 'dns' })).toBe('/servers/7?modal=settings&section=dns');
  });
});

describe('Einführung auf der Serverseite', () => {
  it('wartet, bis der Zustand des Hosts bekannt ist', async () => {
    status = null;
    const intro = await mount();
    expect(tourOf(intro)).toBeNull();
    status = FRESH;
    intro.requestUpdate();
    await intro.updateComplete;
    expect(tourOf(intro)).not.toBeNull();
  });

  it('beginnt beim Host, ohne zu navigieren, und führt Schritt für Schritt weiter, der Zähler zählt durchgehend', async () => {
    const intro = await mount();
    expect(field(intro, 'tour-title')).toContain('setup.intro.host.title');
    expect(field(intro, 'tour-progress')).toContain('"number":1,"total":18');
    expect(navigate).not.toHaveBeenCalled();

    await press(intro, 'tour-next');
    expect(field(intro, 'tour-text')).toBe('setup.intro.trivy');
    await press(intro, 'tour-next');
    expect(field(intro, 'tour-text')).toBe('setup.intro.aptCacher');
    expect(field(intro, 'tour-text')).not.toContain('lapi');
  });

  it('wechselt für die Einstellungen und die Reiter die Seite und kehrt für die Gruppen zurück', async () => {
    const intro = await mount();
    // host, trivy, apt-cacher, härtung -> Einstellungen: Betrieb
    for (let i = 0; i < 4; i += 1) await press(intro, 'tour-next');
    expect(navigate).toHaveBeenLastCalledWith('/servers/7?modal=settings&section=operation', { replace: true });
    await press(intro, 'tour-next');
    await press(intro, 'tour-next');
    expect(navigate).toHaveBeenLastCalledWith('/servers/7?modal=settings&section=access', { replace: true });
    await press(intro, 'tour-next');
    await press(intro, 'tour-next');
    // letzte Seite der Einstellungen -> erster Reiter: die Einstellungen sind nicht mehr in der Adresse
    await press(intro, 'tour-next');
    expect(navigate).toHaveBeenLastCalledWith('/servers/7/packages', { replace: true });
    for (let i = 0; i < 6; i += 1) await press(intro, 'tour-next');
    expect(navigate).toHaveBeenLastCalledWith('/servers/7/jobs', { replace: true });
    await press(intro, 'tour-next');
    expect(navigate).toHaveBeenLastCalledWith('/servers/7', { replace: true });
    expect(field(intro, 'tour-title')).toContain('setup.intro.groups.title');

    await press(intro, 'tour-next');
    expect(field(intro, 'tour-title')).toContain('setup.intro.done.title');
    expect(tourOf(intro)!.shadowRoot!.querySelector('[data-action="tour-next"]')).toBeNull();
  });

  it('Zurück navigiert zur Ansicht des früheren Schritts', async () => {
    const intro = await mount();
    for (let i = 0; i < 5; i += 1) await press(intro, 'tour-next');
    await press(intro, 'tour-back');
    await press(intro, 'tour-back');
    expect(navigate).toHaveBeenLastCalledWith('/servers/7', { replace: true });
    expect(field(intro, 'tour-text')).toBe('setup.intro.hardening.text');
  });

  it('lässt Trivy und apt-cacher-ng aus, wo die Karte sie nicht anbietet', async () => {
    status = { ...FRESH, in_container: true };
    const intro = await mount();
    await press(intro, 'tour-next');
    expect(field(intro, 'tour-text')).toBe('setup.intro.hardening.text');
  });

  it('kommt auch ohne Antwort des Hosts weiter', async () => {
    status = null;
    statusError = 'kaputt';
    const intro = await mount();
    await press(intro, 'tour-next');
    expect(field(intro, 'tour-text')).toBe('setup.intro.hardening.text');
  });

  it('Einführung schließen (auch mitten drin) setzt den Schritt auf leer, schließt das Einstellungsfenster und lädt das Konto neu', async () => {
    const intro = await mount();
    await press(intro, 'tour-next');
    await press(intro, 'close-intro');
    await vi.waitFor(() => expect(refresh).toHaveBeenCalled());
    expect(patchGlobal).toHaveBeenCalledWith({ setup_step: '' });
    expect(setQuery).toHaveBeenCalledWith({ modal: null, section: null }, { replace: true });
  });
});
