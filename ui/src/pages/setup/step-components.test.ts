import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { readGlobal } from '../../api/settings';
import { unsaved } from '../../lib/unsaved';

const owner = {};
const patchGlobal = vi.fn();
const configureBackups = vi.fn();
const testMail = vi.fn();

vi.mock('../../api/settings', async (original) => ({
  ...(await original<typeof import('../../api/settings')>()),
  settingsApi: { patchGlobal: (body: unknown) => patchGlobal(body), configureBackups: (body: unknown) => configureBackups(body), testMail: () => testMail() },
}));
// Die Karten und Gruppen laden sonst ihre Daten; hier zählen die Schritte um sie herum.
vi.mock('../settings/endpoints/acme-card', () => ({}));
vi.mock('../settings/subscription', () => ({}));
vi.mock('../settings/endpoints/network-group', () => ({}));
vi.mock('../settings/endpoints/endpoint-dialog', () => ({}));
const leaveAfterSessionModeChange = vi.fn();
vi.mock('../../lib/auth', () => ({ auth: { can: () => true, onChange: () => () => undefined, leaveAfterSessionModeChange: (path: string) => leaveAfterSessionModeChange(path) } }));
vi.mock('../../lib/live-data', () => ({
  LiveData: class {
    data = { enabled: false, clientAddress: '', endpoints: [] };
    error = '';
    reload(): void {}
  },
}));

const { SetupSession } = await import('./step-session');
const { SetupAddresses } = await import('./step-addresses');
const { SetupServices } = await import('./step-services');
const { SetupAccess } = await import('./step-access');
const { SetupCertificate } = await import('./step-certificate');
const { SetupOnboarding } = await import('./step-onboarding');
const { SetupBackup } = await import('./step-backup');
const { SetupMail } = await import('./step-mail');
const { SetupRetention } = await import('./step-retention');
const { SetupSubscription } = await import('./step-subscription');

const SETTINGS = readGlobal({
  address_mode: 'separate',
  ui_port: 9310,
  agent_port: 9320,
  mcp_port: 9330,
  shared_tls_port: 443,
  agent_enabled: true,
  mcp_enabled: false,
  default_ssh_user: 'root',
  onboarding_pub_key: 'ssh-ed25519 AAAA test',
  backup_enabled: false,
  backup_interval_hours: 24,
  backup_time: '02:00',
  backup_retention: 14,
  mail_enabled: false,
  mail_host: '',
  mail_port: 587,
  mail_security: 'starttls',
  log_retention_days: 90,
  routine_log_retention_days: 7,
  storage_history_retention_days: 90,
  public_base_url: 'https://lcm.example.com',
});

type Step = HTMLElement & { settings: Record<string, unknown>; updateComplete: Promise<boolean> };

async function mount<T extends Step>(Class: new () => T, settings: Record<string, unknown> = SETTINGS): Promise<T> {
  const step = new Class();
  step.settings = settings;
  document.body.append(step);
  await step.updateComplete;
  return step;
}

const root = (step: Step) => step.shadowRoot!;
const button = (step: Step, action: string) => root(step).querySelector<HTMLElement>(`lcm-button[data-action="${action}"]`)!;

/** Klickt und wartet, bis die Anfragen und das Neuzeichnen durch sind. */
async function click(step: Step, action: string): Promise<void> {
  button(step, action).click();
  await vi.waitFor(() => expect(root(step).querySelector('lcm-button[data-action="save"]')?.hasAttribute('busy')).toBe(false));
  await step.updateComplete;
}

function doneEvents(step: Step): CustomEvent[] {
  const seen: CustomEvent[] = [];
  step.addEventListener('lcm-step-done', (event) => seen.push(event as CustomEvent));
  return seen;
}

beforeEach(() => {
  leaveAfterSessionModeChange.mockReset();
  vi.spyOn(Math, 'random').mockReturnValue(0);
  patchGlobal.mockReset().mockResolvedValue(SETTINGS);
  configureBackups.mockReset().mockResolvedValue(undefined);
  testMail.mockReset().mockResolvedValue(undefined);
});
afterEach(() => {
  vi.restoreAllMocks();
  document.body.replaceChildren();
  unsaved.set(owner, false);
});

describe('Schritt Anmeldung', () => {
  const choose = async (step: Step, value: string) => {
    root(step).querySelector('lcm-session-mode-field')!.dispatchEvent(new CustomEvent('lcm-change', { detail: { value } }));
    await step.updateComplete;
  };

  it('hat kein Zurück, bestätigt die Wahl und rückt vor, wenn der Weg gleich bleibt', async () => {
    const step = await mount(SetupSession);
    const seen = doneEvents(step);
    expect(button(step, 'back')).toBeNull();
    await click(step, 'save');
    expect(patchGlobal).toHaveBeenCalledWith({ browser_session_mode: 'memory', browser_session_mode_confirmed: true, setup_step: 'addresses' });
    expect(seen).toHaveLength(1);
    expect(leaveAfterSessionModeChange).not.toHaveBeenCalled();
  });

  it('ein Wechsel des Wegs beendet die Sitzung: weiter geht es nach der Anmeldung', async () => {
    const step = await mount(SetupSession, { ...SETTINGS, browser_session_cookie_available: true });
    const seen = doneEvents(step);
    await choose(step, 'cookie');
    await click(step, 'save');
    expect(patchGlobal).toHaveBeenCalledWith({ browser_session_mode: 'cookie', browser_session_mode_confirmed: true, setup_step: 'addresses' });
    expect(leaveAfterSessionModeChange).toHaveBeenCalledOnce();
    expect(seen).toHaveLength(0);
  });
});

describe('Zurück', () => {
  it('geht zum Schritt davor, ohne etwas anderes zu senden, und meldet sich', async () => {
    const step = await mount(SetupAddresses);
    const seen = doneEvents(step);
    await click(step, 'back');
    expect(patchGlobal).toHaveBeenCalledWith({ setup_step: 'session' });
    expect(patchGlobal).toHaveBeenCalledOnce();
    expect(seen).toHaveLength(1);
  });

  it('gibt es in jedem Schritt außer dem ersten', async () => {
    for (const Class of [SetupAddresses, SetupServices, SetupAccess, SetupCertificate, SetupOnboarding, SetupBackup]) {
      const step = await mount(Class as new () => Step);
      expect(button(step, 'back'), Class.name).not.toBeNull();
      document.body.replaceChildren();
    }
  });
});

describe('Schritt Adressen', () => {
  it('speichert nur Geändertes, rückt zu den Diensten vor und meldet sich', async () => {
    const step = await mount(SetupAddresses);
    const seen = doneEvents(step);
    await click(step, 'save');
    expect(patchGlobal).toHaveBeenCalledWith({ setup_step: 'services' });
    expect(seen).toHaveLength(1);
  });

  it('hat keinen Knopf zum Überspringen, keine Schalter und die Hilfe in der Spalte daneben', async () => {
    const step = await mount(SetupAddresses);
    expect(button(step, 'skip')).toBeNull();
    expect(root(step).querySelector('lcm-explainer[topic="setup-wizard"]')).not.toBeNull();
    expect(root(step).querySelector('lcm-address-block')!.shadowRoot!.querySelector('lcm-explainer')).toBeNull();
    expect(root(step).querySelector('lcm-address-block')!.shadowRoot!.querySelector('[data-switch]')).toBeNull();
  });

  it('lässt zwei gleiche Ports nicht durch', async () => {
    const step = await mount(SetupAddresses, { ...SETTINGS, agent_port: 9310 });
    await click(step, 'save');
    expect(patchGlobal).not.toHaveBeenCalled();
    expect(root(step).querySelector('lcm-notice[tone="crit"]')).not.toBeNull();
  });
});

describe('Schritt Dienste', () => {
  it('schickt die umgelegten Schalter mit dem nächsten Schritt', async () => {
    const step = await mount(SetupServices);
    root(step).querySelector('[data-switch="mcp"] lcm-switch')!.dispatchEvent(new CustomEvent('lcm-change', { detail: { checked: true } }));
    await step.updateComplete;
    await click(step, 'save');
    expect(patchGlobal).toHaveBeenCalledWith({ mcp_enabled: true, setup_step: 'access' });
  });

  it('Modus C: ein Dienst ohne Namen steht aus, gesperrt, mit dem Satz dazu', async () => {
    const step = await mount(SetupServices, { ...SETTINGS, address_mode: 'shared_port', public_base_url: 'https://lcm.example.com', public_agent_url: '', public_mcp_url: 'https://mcp.example.com' });
    const agent = root(step).querySelector<HTMLElement & { checked: boolean; disabled: boolean }>('[data-switch="agent"] lcm-switch')!;
    expect(agent.checked).toBe(false);
    expect(agent.disabled).toBe(true);
    expect(root(step).querySelector('[data-switch-hint="agent"]')?.textContent).toBe('setup.services.noDomain');
  });
});

describe('Schritt Zugriff', () => {
  it('zeigt eine Gruppe für die Oberfläche und je eine für die eingeschalteten Dienste', async () => {
    const step = await mount(SetupAccess);
    const endpoints = [...root(step).querySelectorAll('lcm-network-group')].map((group) => group.getAttribute('endpoint'));
    expect(endpoints).toEqual(['ui', 'agent']);
  });

  it('geht ohne offene Änderung gleich weiter und speichert nichts außer dem Schritt', async () => {
    const step = await mount(SetupAccess);
    await click(step, 'save');
    expect(patchGlobal).toHaveBeenCalledWith({ setup_step: 'certificate' });
  });

  it('warnt vor dem Weitergehen mit ungespeicherten Netzbereichen, der zweite Klick geht ohne sie weiter', async () => {
    const step = await mount(SetupAccess);
    unsaved.set(owner, true);
    await click(step, 'save');
    expect(patchGlobal).not.toHaveBeenCalled();
    expect(root(step).querySelector('[data-field="unsaved"]')).not.toBeNull();
    expect(button(step, 'save').textContent).toContain('setup.continueAnyway');
    await click(step, 'save');
    expect(patchGlobal).toHaveBeenCalledWith({ setup_step: 'certificate' });
  });

  it('nimmt die Warnung zurück, sobald alles gespeichert ist', async () => {
    const step = await mount(SetupAccess);
    unsaved.set(owner, true);
    await click(step, 'save');
    unsaved.set(owner, false);
    await step.updateComplete;
    expect(root(step).querySelector('[data-field="unsaved"]')).toBeNull();
  });
});

describe('Schritt Zertifikat', () => {
  const choose = async (step: Step, value: string) => {
    root(step).querySelector('lcm-choice')!.dispatchEvent(new CustomEvent('lcm-change', { detail: { value } }));
    await step.updateComplete;
  };
  const type = async (step: Step, value: string) => {
    root(step).querySelector('lcm-textarea[data-field="trusted-proxies"]')!.dispatchEvent(new CustomEvent('lcm-input', { detail: { value } }));
    await step.updateComplete;
  };

  const certificate = (over: Record<string, unknown> = {}) => ({ id: '1', names: ['lcm.example.com'], issuer: 'R11', notAfter: '2999-01-01T00:00:00Z', fingerprint: '', renewAt: '', ...over });
  const acmeState = async (step: Step, certificates: unknown[]) => {
    root(step).querySelector('lcm-settings-acme')!.dispatchEvent(new CustomEvent('lcm-acme-state', { detail: { state: { certificates } } }));
    await step.updateComplete;
  };

  it('beginnt bei Let\'s Encrypt mit der geöffneten Karte im Assistenten-Modus, ohne Hinweis, dass es aus ist', async () => {
    const step = await mount(SetupCertificate);
    const card = root(step).querySelector('lcm-settings-acme')!;
    expect(card.hasAttribute('open')).toBe(true);
    expect(card.hasAttribute('wizard')).toBe(true);
    expect(root(step).querySelector('[data-field="letsencrypt-off"]')).toBeNull();
    expect(root(step).querySelector('lcm-textarea')).toBeNull();
  });

  it('Let\'s Encrypt: der Knopf unten bleibt gesperrt, bis ein gültiges Zertifikat alle Namen trägt, dann heißt er Weiter', async () => {
    const step = await mount(SetupCertificate);
    expect(button(step, 'save').hasAttribute('disabled')).toBe(true);
    expect(button(step, 'save').textContent).toContain('setup.next');
    await acmeState(step, [certificate({ notAfter: '2000-01-01T00:00:00Z' })]);
    expect(button(step, 'save').hasAttribute('disabled')).toBe(true);
    await acmeState(step, [certificate({ names: ['other.example.com'] })]);
    expect(button(step, 'save').hasAttribute('disabled')).toBe(true);
    await acmeState(step, [certificate()]);
    expect(button(step, 'save').hasAttribute('disabled')).toBe(false);
    expect(button(step, 'save').textContent).toContain('setup.next');
  });

  it('Let\'s Encrypt speichert nur den Schritt, die Karte speichert selbst', async () => {
    const step = await mount(SetupCertificate);
    await acmeState(step, [certificate()]);
    await click(step, 'save');
    expect(patchGlobal).toHaveBeenCalledWith({ setup_step: 'onboarding' });
  });

  it('der Proxy braucht Adressen, keine zu weiten und keine Namen', async () => {
    const step = await mount(SetupCertificate);
    await choose(step, 'proxy');
    expect(root(step).querySelector('lcm-settings-acme')).toBeNull();
    expect(button(step, 'save').hasAttribute('disabled')).toBe(false);
    expect(button(step, 'save').textContent).toContain('setup.save');
    await click(step, 'save');
    expect(root(step).querySelector('lcm-textarea')!.getAttribute('error')).toBe('setup.certificate.proxy.empty');
    await type(step, '0.0.0.0/0');
    await click(step, 'save');
    expect(root(step).querySelector('lcm-textarea')!.getAttribute('error')).toBe('setup.certificate.proxy.wide');
    await type(step, 'proxy.example.com');
    await click(step, 'save');
    expect(root(step).querySelector('lcm-textarea')!.getAttribute('error')).toBe('setup.certificate.proxy.invalid');
    expect(patchGlobal).not.toHaveBeenCalled();
  });

  it('der Proxy schaltet den Header ein und speichert die Liste', async () => {
    const step = await mount(SetupCertificate);
    await choose(step, 'proxy');
    await type(step, '10.0.0.1\n192.168.0.0/24');
    await click(step, 'save');
    expect(patchGlobal).toHaveBeenCalledWith({ trust_proxy_header: true, trusted_proxies: ['10.0.0.1', '192.168.0.0/24'], setup_step: 'onboarding' });
  });

  it('beginnt beim Proxy, wenn LCM ihm schon glaubt', async () => {
    const step = await mount(SetupCertificate, { ...SETTINGS, trust_proxy_header: true, trusted_proxies: '10.0.0.1' });
    expect(root(step).querySelector<HTMLElement & { value: string }>('lcm-textarea')!.value).toBe('10.0.0.1');
  });

  it('kündigt den Wechsel der Adresse an, statt gleich weiterzugehen', async () => {
    patchGlobal.mockResolvedValue(readGlobal({ public_base_url: 'https://lcm.example.org', effective_urls: { ui: 'https://lcm.example.org' } }));
    vi.useFakeTimers();
    try {
      const step = await mount(SetupCertificate);
      await choose(step, 'proxy');
      await type(step, '10.0.0.1');
      const seen = doneEvents(step);
      button(step, 'save').click();
      await vi.advanceTimersByTimeAsync(0);
      await step.updateComplete;
      expect(seen).toHaveLength(0);
      const notice = root(step).querySelector('lcm-notice[data-field="redirect"]');
      expect(notice?.textContent).toBe('setup.certificate.redirect.text');
      expect(root(step).querySelector('lcm-button[data-action="redirect"]')).not.toBeNull();
      expect(root(step).querySelector('lcm-button[data-action="save"]')).toBeNull();
    } finally {
      vi.clearAllTimers();
      vi.useRealTimers();
    }
  });

  it('bleibt ohne eingetragenen Namen, wo er ist, und geht zum nächsten Schritt', async () => {
    patchGlobal.mockResolvedValue(readGlobal({ effective_urls: { ui: 'https://lcm.example.org' } }));
    const step = await mount(SetupCertificate);
    await choose(step, 'proxy');
    await type(step, '10.0.0.1');
    const seen = doneEvents(step);
    await click(step, 'save');
    expect(seen).toHaveLength(1);
    expect(root(step).querySelector('lcm-notice[data-field="redirect"]')).toBeNull();
  });

  describe('ohne Namen', () => {
    const NO_NAMES = { ...SETTINGS, public_base_url: '', public_agent_url: '', public_mcp_url: '', 'effective_urls.ui': 'https://192.168.1.5:9310', 'effective_urls.agent': 'https://localhost:9320' };

    it('wählt den Reverse-Proxy vor', async () => {
      const step = await mount(SetupCertificate, NO_NAMES);
      expect(root(step).querySelector<HTMLElement & { value: string }>('lcm-choice')!.value).toBe('proxy');
      expect(root(step).querySelector('lcm-textarea')).not.toBeNull();
      expect(root(step).querySelector('lcm-settings-acme')).toBeNull();
    });

    it('zeigt bei Let\'s Encrypt nur den Hinweis, ohne Karte, und sperrt den Knopf unten', async () => {
      const step = await mount(SetupCertificate, NO_NAMES);
      await choose(step, 'letsencrypt');
      expect(root(step).querySelector('lcm-settings-acme')).toBeNull();
      expect(root(step).querySelector('[data-field="letsencrypt-needs-name"] lcm-notice')?.textContent).toBe('setup.certificate.letsencrypt.needsName');
      expect(button(step, 'save').hasAttribute('disabled')).toBe(true);
    });

    it('„Zu Schritt 2“ geht zu den Adressen zurück', async () => {
      const step = await mount(SetupCertificate, NO_NAMES);
      await choose(step, 'letsencrypt');
      const seen = doneEvents(step);
      button(step, 'to-addresses').click();
      await vi.waitFor(() => expect(seen).toHaveLength(1));
      expect(patchGlobal).toHaveBeenCalledWith({ setup_step: 'addresses' });
    });

    it('kennt Namen aus Schritt 2 auch ohne gemeldete Adressen', async () => {
      const step = await mount(SetupCertificate, { ...SETTINGS, public_agent_url: '', public_mcp_url: '' });
      expect(root(step).querySelector<HTMLElement & { value: string }>('lcm-choice')!.value).toBe('letsencrypt');
      expect(root(step).querySelector('lcm-settings-acme')).not.toBeNull();
    });
  });
});

describe('Schritt Onboarding', () => {
  it('zeigt den Schlüssel mit Kopierknopf und die Anleitung', async () => {
    const step = await mount(SetupOnboarding);
    expect(root(step).querySelector<HTMLElement & { value: string }>('lcm-copy-field[data-field="onboarding-key"]')!.value).toBe('ssh-ed25519 AAAA test');
    expect(root(step).querySelector('[data-field="key-guide"]')).not.toBeNull();
  });

  it('hat kein Überspringen: ein leeres Feld bleibt leer und wird nicht gesendet', async () => {
    const step = await mount(SetupOnboarding, { ...SETTINGS, default_ssh_user: '' });
    expect(button(step, 'skip')).toBeNull();
    await click(step, 'save');
    expect(patchGlobal).toHaveBeenCalledWith({ setup_step: 'mail' });
  });

  it('speichert einen geänderten Vorgabe-Benutzer', async () => {
    const step = await mount(SetupOnboarding);
    root(step).querySelector('lcm-input[data-field="default_ssh_user"]')!.dispatchEvent(new CustomEvent('lcm-input', { detail: { value: 'deploy' } }));
    await step.updateComplete;
    await click(step, 'save');
    expect(patchGlobal).toHaveBeenCalledWith({ default_ssh_user: 'deploy', setup_step: 'mail' });
  });
});

describe('Schritt Backup', () => {
  const switchOn = async (step: Step) => {
    root(step).querySelector('lcm-switch[data-field="backup_enabled"]')!.dispatchEvent(new CustomEvent('lcm-change', { detail: { checked: true } }));
    await step.updateComplete;
  };

  it('Überspringen und unverändert Speichern senden keinen Zeitplan', async () => {
    const skipped = await mount(SetupBackup);
    await click(skipped, 'skip');
    await click(skipped, 'save');
    expect(configureBackups).not.toHaveBeenCalled();
    expect(patchGlobal).toHaveBeenCalledWith({ setup_step: 'subscription' });
  });

  it('eingeschaltet braucht es ohne Schlüssel eine Passphrase', async () => {
    const step = await mount(SetupBackup);
    await switchOn(step);
    await click(step, 'save');
    expect(configureBackups).not.toHaveBeenCalled();
    expect(patchGlobal).not.toHaveBeenCalled();
    expect(root(step).querySelector('lcm-password')!.getAttribute('hint')).toBe('setup.backup.passphraseRequired');
  });

  it('speichert den Zeitplan und rückt zur Einführung vor', async () => {
    const step = await mount(SetupBackup);
    await switchOn(step);
    root(step).querySelector('lcm-password')!.dispatchEvent(new CustomEvent('lcm-input', { detail: { value: 'a long enough passphrase' } }));
    await step.updateComplete;
    await click(step, 'save');
    expect(configureBackups).toHaveBeenCalledWith(expect.objectContaining({ enabled: true, interval_hours: 24, retention: 14, time: '02:00', passphrase: 'a long enough passphrase' }));
    expect(patchGlobal).toHaveBeenCalledWith({ setup_step: 'subscription' });
  });

  it('mit gespeichertem Schlüssel fragt der Schritt nicht nach einer Passphrase', async () => {
    const step = await mount(SetupBackup, { ...SETTINGS, backup_recipients_set: true });
    await switchOn(step);
    expect(root(step).querySelector('lcm-password')).toBeNull();
  });
});

const type_ = async (step: Step, key: string, value: string) => {
  root(step).querySelector(`lcm-input[data-field="${key}"]`)!.dispatchEvent(new CustomEvent('lcm-input', { detail: { value } }));
  await step.updateComplete;
};
const field = (step: Step, key: string) => root(step).querySelector<HTMLElement & { value: string }>(`lcm-input[data-field="${key}"]`)!;

describe('Schritt E-Mail-Versand', () => {
  it('zeigt die Felder der Einstellungen und lässt sich überspringen', async () => {
    const step = await mount(SetupMail);
    for (const key of ['mail_host', 'mail_port', 'mail_username', 'mail_password', 'mail_from', 'mail_admin_recipients']) expect(field(step, key), key).not.toBeNull();
    expect(root(step).querySelector('lcm-select')).not.toBeNull();
    expect(root(step).querySelector('lcm-server-action[data-action="test-mail"]')).not.toBeNull();
    await click(step, 'skip');
    expect(patchGlobal).toHaveBeenCalledWith({ setup_step: 'retention' });
  });

  it('ohne Server bleibt der Versand aus und der Schritt geht weiter', async () => {
    const step = await mount(SetupMail);
    await click(step, 'save');
    expect(patchGlobal).toHaveBeenCalledWith({ setup_step: 'retention' });
  });

  it('mit Server schaltet er den Versand ein und speichert nur die Änderungen', async () => {
    const step = await mount(SetupMail);
    await type_(step, 'mail_host', 'smtp.example.com');
    await type_(step, 'mail_from', 'lcm@example.com');
    await click(step, 'save');
    expect(patchGlobal).toHaveBeenCalledWith({ mail_enabled: true, mail_host: 'smtp.example.com', mail_from: 'lcm@example.com', setup_step: 'retention' });
  });

  it('prüft die Angaben und bleibt bei einem Fehler stehen', async () => {
    const step = await mount(SetupMail);
    await type_(step, 'mail_host', 'smtp.example.com');
    await type_(step, 'mail_admin_recipients', 'kein-mail');
    await click(step, 'save');
    expect(patchGlobal).not.toHaveBeenCalled();
    expect(field(step, 'mail_admin_recipients').getAttribute('error')).not.toBe('');
  });

  it('die Test-Mail speichert zuerst, schickt sie dann und beendet den Schritt nicht', async () => {
    const step = await mount(SetupMail);
    const seen = doneEvents(step);
    expect(root(step).querySelector('lcm-server-action[data-action="test-mail"]')!.hasAttribute('disabled')).toBe(true);
    await type_(step, 'mail_host', 'smtp.example.com');
    root(step).querySelector<HTMLElement>('lcm-server-action[data-action="test-mail"]')!.click();
    await vi.waitFor(() => expect(testMail).toHaveBeenCalledOnce());
    expect(patchGlobal).toHaveBeenCalledWith({ mail_enabled: true, mail_host: 'smtp.example.com' });
    expect(seen).toHaveLength(0);
    // Nach dem Speichern ist der Stand gespeichert: Weiter sendet nur noch den Schritt.
    await click(step, 'save');
    expect(patchGlobal).toHaveBeenLastCalledWith({ setup_step: 'retention' });
  });

  it('der Moduswechsel setzt den Standardport', async () => {
    const step = await mount(SetupMail);
    root(step).querySelector('lcm-select')!.dispatchEvent(new CustomEvent('lcm-change', { detail: { value: 'tls' } }));
    await step.updateComplete;
    expect(field(step, 'mail_port').value).toBe('465');
  });
});

describe('Schritt Aufbewahrung', () => {
  it('zeigt die gespeicherten Tage als Vorgabe und hat kein Überspringen', async () => {
    const step = await mount(SetupRetention);
    expect(field(step, 'log_retention_days').value).toBe('90');
    expect(field(step, 'routine_log_retention_days').value).toBe('7');
    expect(field(step, 'storage_history_retention_days').value).toBe('90');
    expect(button(step, 'skip')).toBeNull();
    await click(step, 'save');
    expect(patchGlobal).toHaveBeenCalledWith({ setup_step: 'backup' });
  });

  it('speichert geänderte Tage und prüft die Grenzen', async () => {
    const step = await mount(SetupRetention);
    await type_(step, 'storage_history_retention_days', '30');
    await click(step, 'save');
    expect(patchGlobal).not.toHaveBeenCalled();
    expect(field(step, 'storage_history_retention_days').getAttribute('error')).toBe('settings.form.errors.range');
    await type_(step, 'storage_history_retention_days', '180');
    await type_(step, 'log_retention_days', '30');
    await click(step, 'save');
    expect(patchGlobal).toHaveBeenCalledWith({ log_retention_days: 30, storage_history_retention_days: 180, setup_step: 'backup' });
  });
});

describe('Schritt Subscription', () => {
  // Die Seite selbst ist oben ersetzt; der Schritt ruft nur ihr `applyPicked()`. (Kein vi.fn: Vitest meldet dessen
  // abgelehntes Versprechen als Fehler des Tests, obwohl der Schritt es abfängt.)
  let applied = 0;
  let failure: Error | undefined;
  customElements.define('lcm-settings-subscription', class extends HTMLElement {
    async applyPicked(): Promise<void> {
      applied += 1;
      if (failure) throw failure;
    }
  });
  beforeEach(() => {
    applied = 0;
    failure = undefined;
  });

  it('bettet die Seite der Subscription ein', async () => {
    const step = await mount(SetupSubscription);
    expect(root(step).querySelector('lcm-settings-subscription')!.hasAttribute('embedded')).toBe(true);
    expect(button(step, 'save').textContent).toContain('setup.next');
  });

  it('Weiter stellt auf den gewählten Kanal um und rückt dann zur Einführung vor', async () => {
    const step = await mount(SetupSubscription);
    const seen = doneEvents(step);
    await click(step, 'save');
    expect(applied).toBe(1);
    expect(patchGlobal).toHaveBeenCalledWith({ setup_step: 'intro' });
    expect(seen).toHaveLength(1);
  });

  it('scheitert die Umstellung, bleibt der Schritt stehen und zeigt den Fehler', async () => {
    failure = new Error('Kanal gesperrt');
    const step = await mount(SetupSubscription);
    const seen = doneEvents(step);
    await click(step, 'save');
    expect(patchGlobal).not.toHaveBeenCalled();
    expect(seen).toHaveLength(0);
    expect(root(step).querySelector('lcm-notice[tone="crit"]')).not.toBeNull();
  });

  it('Überspringen lässt den Kanal, wie er ist', async () => {
    const step = await mount(SetupSubscription);
    await click(step, 'skip');
    expect(applied).toBe(0);
    expect(patchGlobal).toHaveBeenCalledWith({ setup_step: 'intro' });
  });
});
