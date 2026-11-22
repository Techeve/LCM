import { describe, expect, it } from 'vitest';
import { WIZARD_STEPS, isSkippable, isWizardStep, nextStep, previousStep, readStep, stepNumber } from './setup-steps';

describe('setup steps', () => {
  it('run in the order of the plan, ending with the introduction', () => {
    expect(WIZARD_STEPS).toEqual(['session', 'addresses', 'services', 'access', 'certificate', 'onboarding', 'mail', 'retention', 'backup', 'subscription']);
    expect(nextStep('session')).toBe('addresses');
    expect(nextStep('addresses')).toBe('services');
    expect(nextStep('services')).toBe('access');
    expect(nextStep('access')).toBe('certificate');
    expect(nextStep('certificate')).toBe('onboarding');
    expect(nextStep('onboarding')).toBe('mail');
    expect(nextStep('mail')).toBe('retention');
    expect(nextStep('retention')).toBe('backup');
    expect(nextStep('backup')).toBe('subscription');
    expect(nextStep('subscription')).toBe('intro');
    expect(nextStep('intro')).toBe('');
  });

  it('count from one for the progress display', () => {
    expect(WIZARD_STEPS.map(stepNumber)).toEqual([1, 2, 3, 4, 5, 6, 7, 8, 9, 10]);
  });

  it('can be skipped for mail, backup and subscription', () => {
    expect(WIZARD_STEPS.filter(isSkippable)).toEqual(['mail', 'backup', 'subscription']);
  });

  it('goes back to the step before, except from the first', () => {
    expect(previousStep('session')).toBeNull();
    expect(previousStep('addresses')).toBe('session');
    expect(previousStep('backup')).toBe('retention');
    expect(previousStep('subscription')).toBe('backup');
  });

  it('reads the state from the server, unknown values mean done', () => {
    expect(readStep('access')).toBe('access');
    expect(readStep('intro')).toBe('intro');
    for (const value of ['', 'open', 'done', undefined, null, 3]) expect(readStep(value)).toBeNull();
  });

  it('keeps the introduction out of the wizard', () => {
    expect(isWizardStep('intro')).toBe(false);
    expect(isWizardStep(null)).toBe(false);
    expect(isWizardStep('backup')).toBe(true);
  });
});
