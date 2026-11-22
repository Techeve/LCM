import { describe, expect, it } from 'vitest';
import type { GlobalSettings } from '../../api/settings';
import { WIZARD_STEPS } from '../../lib/setup-steps';
import { addressBody, changesUiPort, closeIntro, completeBody, completeStep, goBack, lcmHostPath, onboardingBody, onboardingDraft, onboardingErrors, servicesBody, sessionBody, sessionModeOf, setupDraft, setupErrors, type SetupApi } from './setup-model';

const stored: GlobalSettings = {
  address_mode: 'separate',
  public_host: '',
  ui_port: 9310,
  agent_port: 9320,
  mcp_port: 9330,
  shared_tls_port: 443,
  public_base_url: '',
  public_agent_url: '',
  public_mcp_url: '',
  agent_enabled: true,
  mcp_enabled: false,
  default_ssh_user: 'root',
};

function recorder(answer: GlobalSettings = stored): { api: SetupApi; bodies: Record<string, unknown>[] } {
  const bodies: Record<string, unknown>[] = [];
  return {
    bodies,
    api: {
      patchGlobal: (body) => {
        bodies.push(body);
        return Promise.resolve(answer);
      },
    },
  };
}

describe('completeStep', () => {
  it('sends the data of the step together with the next step', async () => {
    const { api, bodies } = recorder();
    await completeStep(api, 'addresses', { address_mode: 'one_name' });
    expect(bodies).toEqual([{ address_mode: 'one_name', setup_step: 'services' }]);
  });

  it('skipping the backup sends only the next step', async () => {
    const { api, bodies } = recorder();
    await completeStep(api, 'backup');
    await completeStep(api, 'subscription');
    expect(bodies).toEqual([{ setup_step: 'subscription' }, { setup_step: 'intro' }]);
  });

  it('moves every step forward to the following one', () => {
    const next = WIZARD_STEPS.map((step) => completeBody(step).setup_step);
    expect(next).toEqual(['addresses', 'services', 'access', 'certificate', 'onboarding', 'mail', 'retention', 'backup', 'subscription', 'intro']);
  });

  it('lets a failure through, so the step can show it', async () => {
    const api: SetupApi = { patchGlobal: () => Promise.reject(new Error('offline')) };
    await expect(completeStep(api, 'addresses')).rejects.toThrow('offline');
  });
});

describe('goBack', () => {
  it('sends only the step before', async () => {
    const { api, bodies } = recorder();
    await goBack(api, 'addresses');
    await goBack(api, 'backup');
    expect(bodies).toEqual([{ setup_step: 'session' }, { setup_step: 'retention' }]);
  });

  it('has no way back from the first step', () => {
    const { api, bodies } = recorder();
    expect(goBack(api, 'session')).toBeNull();
    expect(bodies).toEqual([]);
  });
});

describe('session step', () => {
  it('confirms the choice, so the one-time question does not come again', () => {
    expect(sessionBody('cookie')).toEqual({ browser_session_mode: 'cookie', browser_session_mode_confirmed: true });
  });

  it('reads the saved way, empty means memory', () => {
    expect(sessionModeOf({})).toBe('memory');
    expect(sessionModeOf({ browser_session_mode: 'cookie' })).toBe('cookie');
  });
});

describe('closeIntro', () => {
  it('sets the step to empty, which is done', async () => {
    const { api, bodies } = recorder();
    await closeIntro(api);
    expect(bodies).toEqual([{ setup_step: '' }]);
  });
});

describe('address step', () => {
  it('sends only the changed fields', () => {
    const base = setupDraft(stored);
    expect(addressBody(base, base)).toEqual({});
    expect(addressBody({ ...base, address_mode: 'one_name', public_host: 'lcm.example.org' }, base)).toEqual({ address_mode: 'one_name', public_host: 'lcm.example.org' });
  });

  it('reports a new port of the interface, so the page can follow it', () => {
    const base = setupDraft(stored);
    expect(changesUiPort(addressBody({ ...base, ui_port: '9400' }, base))).toBe(true);
    expect(changesUiPort(addressBody({ ...base, agent_port: '9400' }, base))).toBe(false);
  });

  it('applies the same rules as the settings page', () => {
    const base = setupDraft(stored);
    expect(setupErrors(base)).toEqual({});
    expect(Object.keys(setupErrors({ ...base, agent_port: '9310' }))).toContain('agent_port');
    expect(Object.keys(setupErrors({ ...base, address_mode: 'one_name' }))).toContain('public_host');
    expect(Object.keys(setupErrors({ ...base, address_mode: 'shared_port' }))).toContain('public_base_url');
  });
});

describe('services step', () => {
  it('sends only the switches that changed', () => {
    const base = setupDraft(stored);
    expect(servicesBody(base, base)).toEqual({});
    expect(servicesBody({ ...base, agent_enabled: false, mcp_enabled: true, ui_port: '1' }, base)).toEqual({ agent_enabled: false, mcp_enabled: true });
  });
});

describe('onboarding step', () => {
  it('sends the default user only when it changed', () => {
    const base = onboardingDraft(stored);
    expect(onboardingBody(base, base)).toEqual({});
    expect(onboardingBody({ ...base, default_ssh_user: ' deploy ' }, base)).toEqual({ default_ssh_user: 'deploy' });
  });

  it('rejects a user name that is too long', () => {
    expect(onboardingErrors({ default_ssh_user: 'x'.repeat(200) })).toHaveProperty('default_ssh_user');
    expect(onboardingErrors({ default_ssh_user: 'deploy' })).toEqual({});
  });
});

describe('lcmHostPath', () => {
  it('finds the server LCM runs on', () => {
    expect(lcmHostPath([{ id: 3, is_lcm_host: false }, { id: 7, is_lcm_host: true }])).toBe('/servers/7');
  });

  it('falls back to the start page', () => {
    expect(lcmHostPath([{ id: 3, is_lcm_host: false }])).toBe('/');
    expect(lcmHostPath([])).toBe('/');
  });
});
