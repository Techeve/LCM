import { describe, expect, it } from 'vitest';
import { enterpriseAllowed, readSubscription } from '../../../api/settings';
import { subscriptionState } from './subscription-model';

const base = readSubscription({ configured: true, status: 'active', days_left: 200, apt_channel: 'beta' });

describe('Subscription-Zustand', () => {
  it('liest Kanal und Tage', () => {
    expect(base.aptChannel).toBe('beta');
    expect(base.daysLeft).toBe(200);
    expect(readSubscription({ apt_channel: 'unbekannt' }).aptChannel).toBe('community');
    expect(readSubscription({}).daysLeft).toBeNull();
  });

  it('warnt kurz vor dem Ablauf', () => {
    expect(subscriptionState(base).level).toBe('great');
    expect(subscriptionState({ ...base, daysLeft: 30 })).toEqual({ level: 'warn', key: 'settings.subscription.state.expiring' });
    expect(subscriptionState({ ...base, daysLeft: null }).level).toBe('great');
  });

  it('kennt abgelaufene, widerrufene und unerreichbare Verträge', () => {
    expect(subscriptionState({ ...base, status: 'expired' }).level).toBe('crit');
    expect(subscriptionState({ ...base, status: 'revoked' }).level).toBe('crit');
    expect(subscriptionState({ ...base, status: 'unreachable' }).level).toBe('unknown');
    expect(subscriptionState({ ...base, status: 'neu' }).key).toBe('settings.subscription.state.unknown');
  });

  it('erlaubt Enterprise nur mit aktivem Vertrag', () => {
    expect(enterpriseAllowed(base)).toBe(true);
    expect(enterpriseAllowed({ ...base, status: 'expired' })).toBe(false);
    expect(enterpriseAllowed({ ...base, configured: false })).toBe(false);
  });
});
