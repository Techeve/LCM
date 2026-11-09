import { describe, expect, it } from 'vitest';
import { enterpriseAllowed, readAptChannels, readSubscription } from '../../../api/settings';
import { displayVersion, featureBehind, featureLabel, returnBlocked, subscriptionState } from './subscription-model';

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

describe('Paketkanäle', () => {
  const view = readAptChannels({
    installed: '2.0.0-beta.12',
    channels: [
      { channel: 'community', latest: '1.48.1', lock: 'behind' },
      { channel: 'beta', latest: '2.0.0~beta.12', lock: '' },
      { channel: 'enterprise', latest: '', lock: 'subscription' },
      { channel: 'quatsch', latest: '9.9.9' },
    ],
  });

  it('liest Stände und Sperren', () => {
    expect(view.installed).toBe('2.0.0-beta.12');
    expect(view.channels.map((c) => [c.channel, c.lock])).toEqual([
      ['community', 'behind'],
      ['beta', ''],
      ['enterprise', 'subscription'],
    ]);
  });

  it('zählt nur Major- und Feature-Nummer', () => {
    expect(featureBehind('1.48.1', '2.0.0~beta.12')).toBe(true);
    expect(featureBehind('2.0.0', '2.0.1-beta.1')).toBe(false);
    expect(featureBehind('2.0.0~beta.12', '1.48.1')).toBe(false);
    expect(featureBehind('1.48.1', '0.0.0-dev')).toBe(false);
    expect(featureLabel('1.x.3')).toBe('0.0');
    expect(featureLabel('2.0.0~beta.12')).toBe('2.0');
    expect(displayVersion('2.0.0~beta.12')).toBe('2.0.0-beta.12');
  });

  it('erkennt einen Wechsel, nach dem der Weg zurück zu ist', () => {
    expect(returnBlocked(view, 'community', 'beta')).toBe(true);
    expect(returnBlocked(view, 'beta', 'community')).toBe(false);
    expect(returnBlocked(undefined, 'community', 'beta')).toBe(false);
  });
});
