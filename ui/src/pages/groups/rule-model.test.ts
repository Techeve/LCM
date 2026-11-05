import { describe, expect, it } from 'vitest';
import {
  RULE_TYPES,
  commandParams,
  effectsOf,
  emptyFirewallRow,
  emptyRuleForm,
  firewallErrors,
  firewallRules,
  firewallSummary,
  hasFirewallErrors,
  mergeProtocols,
  parseFirewall,
  ruleBody,
  serializeFirewall,
  typesFor,
  validateRuleForm,
  withTrigger,
  withType,
} from './rule-model';

describe('types per trigger', () => {
  it('offers only the four baseline types for baseline rules', () => {
    expect(typesFor('enforce').map((t) => t.id)).toEqual(['firewall', 'apt-proxy', 'acl-setup', 'perm-sync']);
  });

  it('hides firewall for schedules and offers scripts there', () => {
    const ids = typesFor(3).map((t) => t.id);
    expect(ids).not.toContain('firewall');
    expect(ids).toContain('script');
  });

  it('switches to a valid type when the trigger changes', () => {
    const form = withType(emptyRuleForm(3), 'script');
    const changed = withTrigger({ ...form, command: 'ls' }, 'enforce');
    expect(changed.type).toBe('firewall');
    expect(changed.command).toBe('');
  });

  it('has a unique id per type', () => {
    expect(new Set(RULE_TYPES.map((t) => t.id)).size).toBe(RULE_TYPES.length);
  });
});

describe('firewall rows', () => {
  it('reads JSON and legacy port lists', () => {
    expect(parseFirewall('[{"port":443,"proto":"udp","comment":"web","source_ips":["10.0.0.1"]}]')).toMatchObject([
      { port: '443', proto: 'udp', comment: 'web', source_ips: '10.0.0.1' },
    ]);
    expect(parseFirewall('80, 443').map((r) => r.port)).toEqual(['80', '443']);
    expect(parseFirewall('[broken')).toEqual([]);
  });

  it('omits empty sources and notes, because the backend rejects an empty source list', () => {
    const json = JSON.parse(serializeFirewall([{ port: '22', proto: 'tcp', ip_version: 'any', allowlist_ids: [], source_ips: ' ', comment: '' }]));
    expect(json).toEqual([{ port: 22, proto: 'tcp', ip_version: 'any' }]);
  });

  it('flags bad ports and sources', () => {
    const rows = parseFirewall('[{"port":1},{"port":70000},{"port":22,"source_ips":["nope"]}]');
    expect(firewallErrors(rows).map((e) => [e.port, e.sources])).toEqual([[false, false], [true, false], [false, true]]);
  });

  it('names the sources that are neither address nor network', () => {
    const row = { ...emptyFirewallRow(), port: '22', source_ips: '10.0.0.1, host.lan, abc, deadbeef, 10.0.0.0/33, ::1, fe80::/129, 2001:db8::/32, x' };
    expect(firewallErrors([row])[0]?.badSources).toEqual(['host.lan', 'abc', 'deadbeef', '10.0.0.0/33', 'fe80::/129', 'x']);
  });

  it('accepts addresses and networks of both families, separated by commas or spaces', () => {
    const row = { ...emptyFirewallRow(), port: '22', source_ips: '203.0.113.7,10.0.0.0/24 2001:db8::1, 2001:db8::/32' };
    expect(hasFirewallErrors([row])).toBe(false);
  });

  it('rejects sources of the wrong family for a fixed IP version, but not for any', () => {
    const mixed = { ...emptyFirewallRow(), port: '22', source_ips: '10.0.0.1, 2001:db8::1' };
    expect(firewallErrors([{ ...mixed, ip_version: 'any' }])[0]?.wrongFamily).toEqual([]);
    expect(firewallErrors([{ ...mixed, ip_version: 'v4' }])[0]?.wrongFamily).toEqual(['2001:db8::1']);
    expect(firewallErrors([{ ...mixed, ip_version: 'v6' }])[0]?.wrongFamily).toEqual(['10.0.0.1']);
  });

  it('limits the note and the number of sources', () => {
    const base = { ...emptyFirewallRow(), port: '22' };
    expect(firewallErrors([{ ...base, comment: 'a'.repeat(120) }])[0]?.comment).toBe(false);
    expect(firewallErrors([{ ...base, comment: 'a'.repeat(121) }])[0]?.comment).toBe(true);
    const many = Array.from({ length: 101 }, (_, i) => `10.0.${Math.floor(i / 250)}.${i + 1}`).join(',');
    expect(firewallErrors([{ ...base, source_ips: many }])[0]?.tooManySources).toBe(true);
  });

  it('wants a port made of digits from 1 to 65535', () => {
    const ports = ['', '0', '1', '65535', '65536', '8o', '-1', ' 80 '];
    const flagged = ports.map((port) => firewallErrors([{ ...emptyFirewallRow(), port }])[0]?.port);
    expect(flagged).toEqual([true, true, false, false, true, true, true, false]);
  });

  it('writes a TCP/UDP row as two rules that differ only in the protocol', () => {
    const row = { ...emptyFirewallRow(), port: '53', proto: 'both' as const, ip_version: 'v4' as const, source_ips: '10.0.0.0/8', allowlist_ids: [2], comment: 'DNS' };
    const rules = JSON.parse(serializeFirewall([row]));
    expect(rules).toEqual([
      { port: 53, proto: 'tcp', ip_version: 'v4', allowlist_ids: [2], source_ips: ['10.0.0.0/8'], comment: 'DNS' },
      { port: 53, proto: 'udp', ip_version: 'v4', allowlist_ids: [2], source_ips: ['10.0.0.0/8'], comment: 'DNS' },
    ]);
  });

  it('reads two rules that differ only in the protocol as one TCP/UDP row', () => {
    const json = '[{"port":53,"proto":"tcp","comment":"DNS"},{"port":53,"proto":"udp","comment":"DNS"}]';
    expect(parseFirewall(json)).toMatchObject([{ port: '53', proto: 'both', comment: 'DNS' }]);
  });

  it('survives the way there and back', () => {
    const rows = [
      { ...emptyFirewallRow(), port: '22', source_ips: '10.0.0.1' },
      { ...emptyFirewallRow(), port: '53', proto: 'both' as const },
      { ...emptyFirewallRow(), port: '443', proto: 'both' as const, ip_version: 'v6' as const, allowlist_ids: [3, 1], comment: 'Web' },
      { ...emptyFirewallRow(), port: '5060', proto: 'udp' as const },
    ];
    expect(parseFirewall(serializeFirewall(rows))).toEqual([rows[0], rows[1], { ...rows[2], allowlist_ids: [3, 1] }, rows[3]]);
  });

  it('keeps the order when the backend sorts by port, protocol and version', () => {
    // So liefert das Backend: tcp/any, tcp/v4, udp/any, udp/v4 - die Partner liegen nicht nebeneinander.
    const json = JSON.stringify([
      { port: 80, proto: 'tcp', ip_version: 'any' },
      { port: 80, proto: 'tcp', ip_version: 'v4' },
      { port: 80, proto: 'udp', ip_version: 'any' },
      { port: 80, proto: 'udp', ip_version: 'v4' },
      { port: 443, proto: 'tcp', ip_version: 'any' },
    ]);
    expect(parseFirewall(json).map((r) => `${r.port}/${r.proto}/${r.ip_version}`)).toEqual(['80/both/any', '80/both/v4', '443/tcp/any']);
  });

  it('does not merge rules that differ in more than the protocol', () => {
    const base = { port: '80', ip_version: 'any' as const, allowlist_ids: [], source_ips: '', comment: '' };
    const tcp = { ...base, proto: 'tcp' as const };
    const udp = { ...base, proto: 'udp' as const };
    expect(mergeProtocols([tcp, { ...udp, port: '81' }]).map((r) => r.proto)).toEqual(['tcp', 'udp']);
    expect(mergeProtocols([tcp, { ...udp, ip_version: 'v4' }]).map((r) => r.proto)).toEqual(['tcp', 'udp']);
    expect(mergeProtocols([tcp, { ...udp, allowlist_ids: [1] }]).map((r) => r.proto)).toEqual(['tcp', 'udp']);
    expect(mergeProtocols([tcp, { ...udp, source_ips: '10.0.0.1' }]).map((r) => r.proto)).toEqual(['tcp', 'udp']);
    expect(mergeProtocols([tcp, { ...udp, comment: 'x' }]).map((r) => r.proto)).toEqual(['tcp', 'udp']);
    expect(mergeProtocols([tcp, udp]).map((r) => r.proto)).toEqual(['both']);
  });

  it('merges each UDP rule into one TCP rule only', () => {
    const base = { port: '80', ip_version: 'any' as const, allowlist_ids: [], source_ips: '', comment: '' };
    const rows = [
      { ...base, proto: 'tcp' as const },
      { ...base, proto: 'tcp' as const },
      { ...base, proto: 'udp' as const },
    ];
    expect(mergeProtocols(rows).map((r) => r.proto)).toEqual(['both', 'tcp']);
  });

  it('treats allowlists as a set when merging', () => {
    const base = { port: '80', ip_version: 'any' as const, source_ips: '', comment: '' };
    const rows = [
      { ...base, proto: 'tcp' as const, allowlist_ids: [2, 1] },
      { ...base, proto: 'udp' as const, allowlist_ids: [1, 2] },
    ];
    expect(mergeProtocols(rows)).toHaveLength(1);
  });

  it('gives the rules without empty parts', () => {
    expect(firewallRules([{ ...emptyFirewallRow(), port: '22', proto: 'both', comment: '  ' }])).toEqual([
      { port: 22, proto: 'tcp', ip_version: 'any' },
      { port: 22, proto: 'udp', ip_version: 'any' },
    ]);
  });

  it('summarises ports with notes', () => {
    expect(firewallSummary('[{"port":80,"comment":"Shop"},{"port":53,"proto":"udp"}]')).toBe('80/tcp (Shop), 53/udp');
  });

  it('shows a port open for both protocols once', () => {
    expect(firewallSummary('[{"port":53,"proto":"tcp"},{"port":53,"proto":"udp"},{"port":80}]')).toBe('53/tcp+udp, 80/tcp');
  });
});

describe('rule form', () => {
  it('needs name, trigger and type specific input', () => {
    expect(validateRuleForm({ ...emptyRuleForm(null), name: '' })).toMatchObject({ name: 'rules.error.nameRequired', trigger: 'rules.error.triggerRequired' });
    expect(validateRuleForm({ ...withType(emptyRuleForm(3), 'packages'), name: 'x' }).command).toBe('rules.error.packagesRequired');
    expect(validateRuleForm({ ...withType(emptyRuleForm(3), 'custom'), name: 'x' }).command).toBe('rules.error.customRequired');
    expect(validateRuleForm({ ...withType(emptyRuleForm(3), 'script'), name: 'x', command: 'id' })).toEqual({});
    expect(validateRuleForm({ ...emptyRuleForm(3), name: 'a'.repeat(65) }).name).toBe('rules.error.nameTooLong');
  });

  it('checks package names, docker scope and domains by content', () => {
    const packages = (command: string) => validateRuleForm({ ...withType(emptyRuleForm(3), 'packages'), name: 'x', command }).command;
    expect(packages('htop, unzip')).toBeUndefined();
    expect(packages('htop, rm -rf $x')).toBe('rules.error.packagesInvalid');
    expect(commandParams({ ...withType(emptyRuleForm(3), 'packages'), command: 'htop, -rf, $x' }).list).toBe('-rf, $x');
    const scope = (command: string) => validateRuleForm({ ...withType(emptyRuleForm(3), 'docker-container-update'), name: 'x', command }).command;
    expect(scope('')).toBeUndefined();
    expect(scope('shop-web_1')).toBeUndefined();
    expect(scope('a b; id')).toBe('rules.error.dockerScopeInvalid');
    const domains = (command: string) => validateRuleForm({ ...withType(emptyRuleForm(3), 'dns-test'), name: 'x', command }).command;
    expect(domains('')).toBeUndefined();
    expect(domains('intranet.example\ngit.example.org')).toBeUndefined();
    expect(domains('x/y')).toBe('rules.error.domainsInvalid');
  });

  it('limits the name by characters and the script by the backend limit', () => {
    expect(validateRuleForm({ ...emptyRuleForm(3), name: 'ä'.repeat(64) }).name).toBeUndefined();
    expect(validateRuleForm({ ...emptyRuleForm(3), name: 'a\nb' }).name).toBe('check.oneLine');
    const script = (command: string) => validateRuleForm({ ...withType(emptyRuleForm(3), 'script'), name: 'x', command }).command;
    expect(script('a\nb'.repeat(10))).toBeUndefined();
    expect(script('x'.repeat(32769))).toBe('check.tooLong');
    expect(script('echo \u0007')).toBe('check.controlChars');
  });

  it('builds the body with the trigger and drops commands of types without one', () => {
    expect(ruleBody({ ...emptyRuleForm(3), name: ' Update ', command: 'ignored' })).toEqual({ name: 'Update', type: 'update', command: '', trigger: 3 });
    const fw = ruleBody({ ...emptyRuleForm('enforce'), name: 'fw' });
    expect(fw).toMatchObject({ type: 'firewall', command: '[]', trigger: 'enforce' });
  });

  it('summarises effects', () => {
    expect(effectsOf([{ type: 'update' }, { type: 'reboot' }])).toMatchObject({ reboot: true, reversible: false });
    expect(effectsOf([{ type: 'health' }])).toMatchObject({ readOnly: true, reversible: true });
    expect(effectsOf([{ type: 'reboot-if-needed' }]).maybeReboot).toBe(true);
  });
});
