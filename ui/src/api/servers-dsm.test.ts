import { describe, expect, it } from 'vitest';
import { normaliseDsmFirewall, type DsmCheck, type DsmFirewallRule } from './servers';
import {
  applyHints,
  dsmApplyUrl,
  firewallPolicy,
  firewallPorts,
  firewallProtocol,
  firewallSource,
  isSettable,
  settableDeviations,
  targetKey,
} from './servers-dsm';

const check = (key: string, state: DsmCheck['state']): DsmCheck => ({ key, state, value: '', params: {} });

describe('isSettable', () => {
  it('knows the settable checks and leaves the firewall out', () => {
    expect(isSettable('csp')).toBe(true);
    expect(isSettable('spectre')).toBe(true);
    expect(isSettable('firewall')).toBe(false);
    expect(isSettable('unknownKey')).toBe(false);
  });
});

describe('settableDeviations', () => {
  it('keeps deviations LCM can fix, not the firewall, not ok or unknown ones', () => {
    const checks = [check('firewall', 'deviation'), check('csp', 'deviation'), check('csrf', 'ok'), check('dos', 'unknown'), check('spectre', 'deviation')];
    expect(settableDeviations(checks).map((c) => c.key)).toEqual(['csp', 'spectre']);
  });
});

describe('dialog texts', () => {
  it('names the target of each check', () => {
    expect(targetKey('skipIpCheck')).toBe('serverSecurity.device.target.off');
    expect(targetKey('csp')).toBe('serverSecurity.device.target.on');
    expect(targetKey('autoBlock')).toBe('serverSecurity.device.target.autoBlock');
    expect(targetKey('tlsProfile')).toBe('serverSecurity.device.target.tlsProfile');
  });

  it('warns about DoS, TLS and Spectre only, in the order of the control panel', () => {
    expect(applyHints(['spectre', 'csp', 'dos', 'tlsProfile'])).toEqual([
      'serverSecurity.device.apply.hint.dos',
      'serverSecurity.device.apply.hint.tlsProfile',
      'serverSecurity.device.apply.hint.spectre',
    ]);
    expect(applyHints(['csp', 'iframe'])).toEqual([]);
  });

  it('posts to the apply route of the server', () => {
    expect(dsmApplyUrl(7)).toBe('/api/v1/servers/7/dsm/apply');
  });
});

const rule = (fields: Partial<DsmFirewallRule>): DsmFirewallRule => ({
  enabled: true,
  policy: 'allow',
  protocol: 'all',
  ports: 'all',
  port_group: 'all',
  source_ip: 'all',
  source_ip_group: 'all',
  name: '',
  ...fields,
});

describe('normaliseDsmFirewall', () => {
  it('keeps profile, sections and rules of the device answer', () => {
    const firewall = normaliseDsmFirewall({
      profile: 'default',
      sections: [{ name: 'global', policy: 'none', rules: [{ enabled: false, policy: 'drop', ports: 'all', protocol: 'all' }, 'broken'] }],
    });
    expect(firewall?.profile).toBe('default');
    expect(firewall?.sections[0]).toMatchObject({ name: 'global', policy: 'none' });
    expect(firewall?.sections[0].rules).toEqual([rule({ enabled: false, policy: 'drop', port_group: '', source_ip: '', source_ip_group: '' })]);
  });

  it('is null until LCM has read the profile, and tolerates missing lists', () => {
    expect(normaliseDsmFirewall(undefined)).toBeNull();
    expect(normaliseDsmFirewall({ profile: '' })).toBeNull();
    expect(normaliseDsmFirewall({ profile: 'default' })).toEqual({ profile: 'default', sections: [] });
    expect(normaliseDsmFirewall({ profile: 'default', sections: [{ name: 'eth0' }] })?.sections[0].rules).toEqual([]);
  });

  it('treats a rule without enabled as enabled', () => {
    expect(normaliseDsmFirewall({ profile: 'p', sections: [{ name: 'global', rules: [{}] }] })?.sections[0].rules[0].enabled).toBe(true);
  });
});

describe('firewall words', () => {
  it('reads drop and deny as deny, none and empty as not set', () => {
    expect(firewallPolicy('drop')).toBe('deny');
    expect(firewallPolicy('DENY')).toBe('deny');
    expect(firewallPolicy('allow')).toBe('allow');
    expect(firewallPolicy('none')).toBe('none');
    expect(firewallPolicy('')).toBe('none');
  });

  it('leaves "all" empty and shows the DSM names as given', () => {
    expect(firewallPorts(rule({}))).toBe('');
    expect(firewallPorts(rule({ ports: 'dms,dms_https' }))).toBe('dms, dms_https');
    expect(firewallProtocol(rule({}))).toBe('');
    expect(firewallProtocol(rule({ protocol: 'tcp' }))).toBe('TCP');
  });

  it('names the source address first, then the group', () => {
    expect(firewallSource(rule({}))).toBe('');
    expect(firewallSource(rule({ source_ip: '192.168.1.0/24', source_ip_group: 'subnet' }))).toBe('192.168.1.0/24');
    expect(firewallSource(rule({ source_ip: '', source_ip_group: 'geoip' }))).toBe('geoip');
  });
});
