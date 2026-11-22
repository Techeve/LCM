import { describe, expect, it } from 'vitest';
import { SETUP_PATH, routeRedirect, setupPending, setupRedirect, setupStepOf } from './address-setup';
import { WIZARD_STEPS } from './setup-steps';

describe('setupStepOf', () => {
  it('knows the steps of the wizard and the introduction, nothing else', () => {
    expect(setupStepOf(null)).toBeNull();
    expect(setupStepOf({})).toBeNull();
    expect(setupStepOf({ setup_step: '' })).toBeNull();
    expect(setupStepOf({ setup_step: 'open' })).toBeNull();
    expect(setupStepOf({ setup_step: 'certificate' })).toBe('certificate');
    expect(setupStepOf({ setup_step: 'intro' })).toBe('intro');
  });
});

describe('setupRedirect', () => {
  it('sends every page to the wizard while a step is open', () => {
    for (const step of WIZARD_STEPS) {
      for (const path of ['/', '/servers/42', '/settings/general', '/account']) {
        expect(setupRedirect({ setup_step: step }, path), `${step} ${path}`).toBe(SETUP_PATH);
      }
    }
  });

  it('leaves the wizard itself alone', () => {
    expect(setupRedirect({ setup_step: 'services' }, SETUP_PATH)).toBeNull();
    expect(setupRedirect({ setup_step: 'services' }, '/setup/addresses')).toBeNull();
  });

  it('lets everything through during the introduction and once done', () => {
    expect(setupRedirect({ setup_step: 'intro' }, '/servers/1')).toBeNull();
    expect(setupRedirect({ setup_step: '' }, '/servers')).toBeNull();
    expect(setupRedirect({}, '/servers')).toBeNull();
    expect(setupRedirect(null, '/servers')).toBeNull();
  });
});

describe('setupPending', () => {
  it('is true only for the steps of the wizard', () => {
    expect(setupPending({ setup_step: 'backup' })).toBe(true);
    expect(setupPending({ setup_step: 'intro' })).toBe(false);
    expect(setupPending({ setup_step: '' })).toBe(false);
  });
});

describe('routeRedirect', () => {
  // Folgt den Umleitungen wie der Router, bis eine Seite bleiben darf; bricht nach zehn Sprüngen ab.
  function settle(user: Parameters<typeof routeRedirect>[0], path: string): string {
    for (let hop = 0; hop < 10; hop++) {
      const next = routeRedirect(user, path.split('?')[0]);
      if (next === null) return path;
      path = next;
    }
    throw new Error(`Umleitung läuft im Kreis: ${path}`);
  }

  it('führt bei offenem Passwortwechsel zur Kontoseite, nicht im Kreis', () => {
    const user = { setup_step: 'addresses', must_change_password: true, must_setup_2fa: false };
    expect(settle(user, '/')).toBe('/account/security?required=1');
    expect(settle(user, '/setup')).toBe('/account/security?required=1&next=%2Fsetup');
  });

  it('wartet auch auf die 2FA-Einrichtung', () => {
    const user = { setup_step: 'addresses', must_change_password: false, must_setup_2fa: true };
    expect(settle(user, '/account/security')).toBe('/account/security');
  });

  it('führt nach den Pflichtschritten zum Assistenten, in jedem Schritt', () => {
    for (const step of WIZARD_STEPS) {
      const user = { setup_step: step, must_change_password: false, must_setup_2fa: false };
      expect(settle(user, '/'), step).toBe('/setup');
    }
  });

  it('lässt die Einführung auf der Seite des LCM-Hosts stehen', () => {
    const user = { setup_step: 'intro', must_change_password: false, must_setup_2fa: false };
    expect(settle(user, '/servers/1')).toBe('/servers/1');
  });
});
