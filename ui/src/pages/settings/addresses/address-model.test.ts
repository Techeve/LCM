import { describe, expect, it } from 'vitest';
import { readGlobal } from '../../../api/settings';
import type { Draft } from '../system/form-state';
import {
  acmeNames,
  addressChanged,
  addressOf,
  certificateNames,
  cleanEntry,
  enabledOf,
  entryOf,
  hostOf,
  isLocked,
  keysOf,
  portDefaults,
  previewUrls,
  readMode,
  sharedNames,
  redirectTarget,
  sharedSwitchOff,
  shownUrls,
  switchState,
  uiPortChange,
  urlOfEntry,
  validateAddresses,
} from './address-model';

function draft(over: Draft = {}): Draft {
  return {
    address_mode: 'separate',
    public_host: '',
    public_base_url: '',
    public_agent_url: '',
    public_mcp_url: '',
    ui_port: '9310',
    agent_port: '9320',
    mcp_port: '9330',
    shared_tls_port: '443',
    ...over,
  };
}

describe('Adressmodus', () => {
  it('kennt drei Modi und fällt auf A zurück', () => {
    expect(readMode('one_name')).toBe('one_name');
    expect(readMode('shared_port')).toBe('shared_port');
    expect(readMode('separate')).toBe('separate');
    expect(readMode(undefined)).toBe('separate');
    expect(readMode('irgendwas')).toBe('separate');
  });

  it('zeigt je Modus nur die Felder, die gelten', () => {
    expect(keysOf('separate')).toEqual(['address_mode', 'public_base_url', 'public_agent_url', 'public_mcp_url', 'ui_port', 'agent_port', 'mcp_port']);
    expect(keysOf('one_name')).toEqual(['address_mode', 'public_host', 'ui_port', 'agent_port', 'mcp_port']);
    expect(keysOf('shared_port')).toEqual(['address_mode', 'public_base_url', 'public_agent_url', 'public_mcp_url', 'shared_tls_port']);
  });

  it('füllt beim Wechsel nur leere oder null gesetzte Ports mit der Vorgabe', () => {
    expect(portDefaults(draft({ shared_tls_port: '' }), 'shared_port')).toEqual({ shared_tls_port: '443' });
    expect(portDefaults(draft({ shared_tls_port: '8443' }), 'shared_port')).toEqual({});
    expect(portDefaults(draft({ ui_port: '0', mcp_port: ' ' }), 'one_name')).toEqual({ ui_port: '9310', mcp_port: '9330' });
  });
});

describe('Wechsel in Modus C', () => {
  it('macht aus den Adressen reine Namen ohne Port und Pfad', () => {
    const names = sharedNames(draft({ public_base_url: 'https://lcm.example.com:9310/x', public_agent_url: 'agent.example.com', public_mcp_url: '' }));
    expect(names).toEqual({ public_base_url: 'https://lcm.example.com', public_agent_url: 'https://agent.example.com' });
  });
});

describe('Antwort mit gesperrten Feldern und Adressen', () => {
  const settings = readGlobal({
    address_mode: 'one_name',
    ui_port: 9310,
    address_locks: { ui_port: true, agent_port: false, mcp_port: false, shared_tls_port: false },
    effective_urls: { ui: 'https://lcm.example.com:9310', agent: 'https://lcm.example.com:9320', mcp: 'https://lcm.example.com:9330' },
  });

  it('löst die verschachtelten Objekte in flache Schlüssel auf', () => {
    expect(settings['address_locks.ui_port']).toBe(true);
    expect(settings['effective_urls.agent']).toBe('https://lcm.example.com:9320');
    expect(settings.address_mode).toBe('one_name');
  });

  it('erkennt gesperrte Felder', () => {
    expect(isLocked(settings, 'ui_port')).toBe(true);
    expect(isLocked(settings, 'agent_port')).toBe(false);
    expect(isLocked(settings, 'shared_tls_port')).toBe(false);
    expect(isLocked({}, 'ui_port')).toBe(false);
  });
});

describe('Namen und Adressen', () => {
  it('schneidet Schema, Port und Pfad ab', () => {
    expect(hostOf('https://lcm.example.com:9310/pfad')).toBe('lcm.example.com');
    expect(hostOf(' agent.example.com ')).toBe('agent.example.com');
    expect(hostOf('http://mcp.example.com')).toBe('mcp.example.com');
    expect(hostOf('')).toBe('');
  });

  it('Modus A: das Feld zeigt name[:port], ohne Schema, Pfad und den Port 443', () => {
    expect(entryOf('https://lcm.example.com:9310/')).toBe('lcm.example.com:9310');
    expect(entryOf('https://lcm.example.com:443')).toBe('lcm.example.com');
    expect(entryOf('https://lcm.example.com')).toBe('lcm.example.com');
    expect(entryOf('')).toBe('');
    expect(cleanEntry('https://x.example.com:443/pfad')).toBe('x.example.com:443');
  });

  it('Modus A: aus der Eingabe wird für die API immer https://name[:port]', () => {
    expect(urlOfEntry('lcm.example.com')).toBe('https://lcm.example.com');
    expect(urlOfEntry('lcm.example.com:8443')).toBe('https://lcm.example.com:8443');
    expect(urlOfEntry('lcm.example.com:443')).toBe('https://lcm.example.com');
    expect(urlOfEntry('https://lcm.example.com:8443/x')).toBe('https://lcm.example.com:8443');
    expect(urlOfEntry('  ')).toBe('');
  });

  it('lässt den Port bei 443 weg', () => {
    expect(addressOf('lcm.example.com', 443)).toBe('https://lcm.example.com');
    expect(addressOf('lcm.example.com', '8443')).toBe('https://lcm.example.com:8443');
    expect(addressOf('', 443)).toBe('');
  });
});

describe('Abgeleitete Adressen (Vorschau)', () => {
  it('A: nimmt die eingetragenen Adressen, wie sie stehen', () => {
    const urls = previewUrls(draft({ public_base_url: 'https://lcm.example.com', public_agent_url: ' https://agent.example.com:8443 ' }));
    expect(urls).toEqual({ ui: 'https://lcm.example.com', agent: 'https://agent.example.com:8443', mcp: '' });
  });

  it('B: baut aus einem Namen und den Ports drei Adressen', () => {
    const urls = previewUrls(draft({ address_mode: 'one_name', public_host: 'lcm.example.com', agent_port: '9999' }));
    expect(urls).toEqual({ ui: 'https://lcm.example.com:9310', agent: 'https://lcm.example.com:9999', mcp: 'https://lcm.example.com:9330' });
  });

  it('B: MCP spricht immer https', () => {
    const urls = previewUrls(draft({ address_mode: 'one_name', public_host: 'lcm.example.com' }));
    expect(urls.mcp).toBe('https://lcm.example.com:9330');
    expect(urls.ui).toBe('https://lcm.example.com:9310');
  });

  it('B: Agent-Port 0 heißt keine Agent-Adresse', () => {
    const urls = previewUrls(draft({ address_mode: 'one_name', public_host: 'lcm.example.com', agent_port: '0' }));
    expect(urls.agent).toBe('');
    expect(urls.ui).toBe('https://lcm.example.com:9310');
  });

  it('B: ohne Namen gibt es keine Adresse', () => {
    expect(previewUrls(draft({ address_mode: 'one_name' }))).toEqual({ ui: '', agent: '', mcp: '' });
  });

  it('C: nimmt die Namen mit dem gemeinsamen Port, bei 443 ohne Port', () => {
    const named = { address_mode: 'shared_port', public_base_url: 'https://lcm.example.com', public_agent_url: 'agent.example.com', public_mcp_url: 'https://mcp.example.com:1234' };
    expect(previewUrls(draft(named))).toEqual({ ui: 'https://lcm.example.com', agent: 'https://agent.example.com', mcp: 'https://mcp.example.com' });
    expect(previewUrls(draft({ ...named, shared_tls_port: '8443' })).agent).toBe('https://agent.example.com:8443');
  });
});

describe('Anzeige nach dem Speichern', () => {
  const saved = readGlobal({
    address_mode: 'one_name',
    public_host: 'lcm.example.com',
    ui_port: 9310,
    agent_port: 9320,
    mcp_port: 9330,
    effective_urls: { ui: 'https://lcm.example.com:9310', agent: 'https://lcm.example.com:9320', mcp: 'https://lcm.example.com:9330' },
  });
  const same = draft({ address_mode: 'one_name', public_host: 'lcm.example.com' });

  it('zeigt gespeichert die Adressen, die LCM meldet', () => {
    expect(addressChanged(same, saved)).toBe(false);
    expect(shownUrls(same, saved).agent).toBe('https://lcm.example.com:9320');
  });

  it('zeigt nach einer Änderung die Vorschau des Entwurfs', () => {
    const edited = { ...same, public_host: 'neu.example.com' };
    expect(addressChanged(edited, saved)).toBe(true);
    expect(shownUrls(edited, saved).ui).toBe('https://neu.example.com:9310');
    expect(addressChanged({ ...same, ui_port: '9400' }, saved)).toBe(true);
    expect(addressChanged({ ...same, address_mode: 'separate' }, saved)).toBe(true);
  });

  it('übersieht Felder anderer Modi', () => {
    expect(addressChanged({ ...same, public_base_url: 'https://x.example.com' }, saved)).toBe(false);
  });
});

describe('Namen für Let\'s Encrypt', () => {
  it('B: ein Name für alle Dienste', () => {
    const urls = previewUrls(draft({ address_mode: 'one_name', public_host: 'LCM.example.com' }));
    expect(acmeNames(urls)).toEqual(['lcm.example.com']);
  });

  it('A und C: bis zu drei verschiedene Namen', () => {
    expect(acmeNames({ ui: 'https://lcm.example.com', agent: 'https://agent.example.com:8443', mcp: 'https://mcp.example.com' })).toEqual(['lcm.example.com', 'agent.example.com', 'mcp.example.com']);
    expect(acmeNames({ ui: 'https://lcm.example.com', agent: 'https://lcm.example.com:8443', mcp: '' })).toEqual(['lcm.example.com']);
  });

  it('lässt die MCP-Adresse weg, solange MCP aus ist', () => {
    const urls = { ui: 'https://lcm.example.com', agent: 'https://agent.example.com', mcp: 'https://mcp.example.com' };
    expect(acmeNames(urls, { ui: true, agent: true, mcp: false })).toEqual(['lcm.example.com', 'agent.example.com']);
  });

  it('lässt die Agent-Adresse weg, solange der Agent aus ist', () => {
    const urls = { ui: 'https://lcm.example.com', agent: 'https://agent.example.com', mcp: 'https://mcp.example.com' };
    expect(acmeNames(urls, { ui: true, agent: false, mcp: true })).toEqual(['lcm.example.com', 'mcp.example.com']);
  });

  it('lässt leere Adressen und IP-Adressen weg', () => {
    expect(acmeNames({ ui: '', agent: 'https://10.0.0.5:9320', mcp: 'https://[::1]:9330' })).toEqual([]);
  });

  it('lässt Namen ohne Punkt weg, wie das Backend: localhost bekommt von keiner CA ein Zertifikat', () => {
    expect(acmeNames({ ui: 'https://localhost:9310', agent: 'https://lcm:9320', mcp: '' })).toEqual([]);
  });
});

describe('Namen der gespeicherten Adressen', () => {
  const saved = readGlobal({
    address_mode: 'shared_port',
    public_base_url: 'https://lcm.example.com',
    public_agent_url: 'https://agent.example.com',
    public_mcp_url: 'https://mcp.example.com',
    shared_tls_port: 443,
    mcp_enabled: true,
  });
  const same = draft({ address_mode: 'shared_port', public_base_url: 'https://lcm.example.com', public_agent_url: 'https://agent.example.com', public_mcp_url: 'https://mcp.example.com', agent_enabled: true, mcp_enabled: true });

  it('nennt die gespeicherten Namen, auch ohne effective_urls', () => {
    expect(certificateNames(same, saved)).toEqual({ saved: ['lcm.example.com', 'agent.example.com', 'mcp.example.com'], pending: false });
  });

  it('nimmt den MCP-Namen nicht, solange MCP aus ist', () => {
    expect(certificateNames(same, { ...saved, mcp_enabled: false }).saved).toEqual(['lcm.example.com', 'agent.example.com']);
  });

  it('nimmt den Agent-Namen nicht, solange der Agent aus ist', () => {
    expect(certificateNames(same, { ...saved, agent_enabled: false }).saved).toEqual(['lcm.example.com', 'mcp.example.com']);
  });

  it('zählt für den Entwurf dessen Schalter: Einschalten von MCP bringt einen Namen dazu', () => {
    const off = { ...saved, mcp_enabled: false };
    expect(certificateNames({ ...same, mcp_enabled: true }, off)).toEqual({ saved: ['lcm.example.com', 'agent.example.com'], pending: true });
    expect(certificateNames({ ...same, mcp_enabled: false, agent_enabled: false }, off)).toEqual({ saved: ['lcm.example.com', 'agent.example.com'], pending: true });
  });

  it('merkt, wenn der Entwurf auf andere Namen kommt', () => {
    const edited = { ...same, address_mode: 'one_name', public_host: 'neu.example.com' };
    expect(certificateNames(edited, saved)).toEqual({ saved: ['lcm.example.com', 'agent.example.com', 'mcp.example.com'], pending: true });
  });

  it('nimmt effective_urls vor den Feldern', () => {
    const withEffective = { ...saved, 'effective_urls.ui': 'https://neu.example.com:9310', 'effective_urls.agent': 'https://neu.example.com:9320', 'effective_urls.mcp': 'https://neu.example.com:9330' };
    expect(certificateNames(same, withEffective).saved).toEqual(['neu.example.com']);
  });
});

describe('Prüfung der Adressen', () => {
  it('A und B: Ports müssen gültig und verschieden sein', () => {
    expect(validateAddresses(draft())).toEqual({});
    expect(validateAddresses(draft({ agent_port: '9310' }))).toEqual({ agent_port: 'settings.general.address.errors.portsDiffer' });
    expect(validateAddresses(draft({ mcp_port: '70000' }))).toEqual({ mcp_port: 'settings.general.address.errors.port' });
    expect(validateAddresses(draft({ ui_port: '' }))).toEqual({ ui_port: 'settings.general.address.errors.port' });
  });

  it('Modus A prüft Name und optionalen Port der Adressen', () => {
    const own = (over: Draft) => validateAddresses(draft(over));
    expect(own({ public_base_url: 'https://lcm.example.com', public_agent_url: 'https://agent.example.com:8443' })).toEqual({});
    expect(own({ public_base_url: 'https://-bad-.example.com' })).toEqual({ public_base_url: 'settings.general.address.errors.nameInvalid' });
    expect(own({ public_agent_url: 'https://agent.example.com:' })).toEqual({ public_agent_url: 'settings.general.address.errors.entryPort' });
    expect(own({ public_mcp_url: 'https://mcp.example.com:99999' })).toEqual({ public_mcp_url: 'settings.general.address.errors.entryPort' });
  });

  it('B: verlangt einen Namen ohne Schema, Port und Pfad', () => {
    const one = (host: string) => validateAddresses(draft({ address_mode: 'one_name', public_host: host }));
    expect(one('lcm.example.com')).toEqual({});
    expect(one('')).toEqual({ public_host: 'settings.general.address.errors.hostRequired' });
    expect(one('https://lcm.example.com')).toEqual({ public_host: 'settings.general.address.errors.hostFormat' });
    expect(one('lcm.example.com:9310')).toEqual({ public_host: 'settings.general.address.errors.hostFormat' });
    expect(one('lcm.example.com/x')).toEqual({ public_host: 'settings.general.address.errors.hostFormat' });
  });

  it('B: prüft zusätzlich die Ports', () => {
    const errors = validateAddresses(draft({ address_mode: 'one_name', public_host: 'lcm.example.com', mcp_port: '9320' }));
    expect(errors).toEqual({ mcp_port: 'settings.general.address.errors.portsDiffer' });
  });

  it('C: nur der Name der Oberfläche ist Pflicht, gesetzte Namen müssen sich unterscheiden', () => {
    const shared = (over: Draft) => validateAddresses(draft({ address_mode: 'shared_port', ...over }));
    const names = { public_base_url: 'https://lcm.example.com', public_agent_url: 'https://agent.example.com', public_mcp_url: 'https://mcp.example.com' };
    expect(shared(names)).toEqual({});
    expect(shared({ ...names, public_mcp_url: '' })).toEqual({});
    expect(shared({ ...names, public_agent_url: '', public_mcp_url: '' })).toEqual({});
    expect(shared({ ...names, public_base_url: '' })).toEqual({ public_base_url: 'settings.general.address.errors.nameRequired' });
    expect(shared({ ...names, public_mcp_url: 'mcp bad' })).toEqual({ public_mcp_url: 'settings.general.address.errors.nameInvalid' });
    expect(shared({ ...names, public_agent_url: 'https://LCM.example.com:9320' })).toEqual({ public_agent_url: 'settings.general.address.errors.namesDiffer' });
    expect(shared({ ...names, public_mcp_url: 'agent.example.com' })).toEqual({ public_mcp_url: 'settings.general.address.errors.namesDiffer' });
    expect(shared({ public_base_url: 'x.example.com', public_agent_url: 'x.example.com', public_mcp_url: 'x.example.com' })).toEqual({
      public_agent_url: 'settings.general.address.errors.namesDiffer',
      public_mcp_url: 'settings.general.address.errors.namesDiffer',
    });
  });

  it('C: der gemeinsame Port muss gültig sein, die Einzelports zählen nicht', () => {
    const names = { address_mode: 'shared_port', public_base_url: 'https://a.example.com', public_agent_url: 'https://b.example.com', public_mcp_url: 'https://c.example.com' };
    expect(validateAddresses(draft({ ...names, shared_tls_port: '0' }))).toEqual({ shared_tls_port: 'settings.general.address.errors.port' });
    expect(validateAddresses(draft({ ...names, ui_port: '', agent_port: '' }))).toEqual({});
  });
});

describe('Wechsel des Ports der Oberfläche', () => {
  const saved = readGlobal({ address_mode: 'separate', ui_port: 9310 });
  const here = 'https://10.0.0.5:9310/settings/general';

  it('nennt vorab die neue Adresse', () => {
    expect(uiPortChange(draft({ ui_port: '9400' }), saved, here)).toBe('https://10.0.0.5:9400');
  });

  it('B: nimmt den Namen aus dem Entwurf', () => {
    expect(uiPortChange(draft({ address_mode: 'one_name', public_host: 'lcm.example.com', ui_port: '9400' }), saved, here)).toBe('https://lcm.example.com:9400');
  });

  it('schweigt bei gleichem, ungültigem oder gesperrtem Port und in Modus C', () => {
    expect(uiPortChange(draft(), saved, here)).toBeNull();
    expect(uiPortChange(draft({ ui_port: 'x' }), saved, here)).toBeNull();
    expect(uiPortChange(draft({ ui_port: '9400' }), { ...saved, 'address_locks.ui_port': true }, here)).toBeNull();
    expect(uiPortChange(draft({ address_mode: 'shared_port', ui_port: '9400' }), saved, here)).toBeNull();
  });
});

describe('Weiterleitung nach dem Speichern', () => {
  const here = 'https://10.0.0.5:9310/settings/general';

  it('folgt ui_url der Antwort', () => {
    expect(redirectTarget(readGlobal({ ui_url: 'https://10.0.0.5:9400/' }), here, false, null)).toBe('https://10.0.0.5:9400/');
  });

  it('nimmt nach einem Portwechsel effective_urls.ui', () => {
    const updated = readGlobal({ effective_urls: { ui: 'https://lcm.example.com:9400' } });
    expect(redirectTarget(updated, here, true, 'https://10.0.0.5:9400')).toBe('https://lcm.example.com:9400');
    expect(redirectTarget(updated, here, false, null)).toBeUndefined();
  });

  it('fällt ohne beides auf die vorab genannte Adresse zurück', () => {
    expect(redirectTarget(readGlobal({}), here, true, 'https://10.0.0.5:9400')).toBe('https://10.0.0.5:9400');
    expect(redirectTarget(readGlobal({}), here, true, null)).toBeUndefined();
  });

  it('bleibt stehen, wenn das Ziel dort liegt, wo die Seite läuft (Proxy)', () => {
    const behindProxy = 'https://lcm.example.com/settings/general';
    expect(redirectTarget(readGlobal({ effective_urls: { ui: 'https://lcm.example.com' } }), behindProxy, true, 'https://lcm.example.com:9400')).toBeUndefined();
  });

  it('tut ohne Antwort nichts', () => {
    expect(redirectTarget(undefined, here, true, 'https://10.0.0.5:9400')).toBeUndefined();
  });
});

describe('Schalter für Agent und MCP', () => {
  const none = readGlobal({});

  it('liest sie aus dem Entwurf: der Agent ist ohne Angabe an, MCP aus', () => {
    expect(enabledOf({})).toEqual({ ui: true, agent: true, mcp: false });
    expect(enabledOf({ agent_enabled: false, mcp_enabled: true })).toEqual({ ui: true, agent: false, mcp: true });
  });

  it('A und B: der Schalter folgt dem Entwurf und ist frei', () => {
    expect(switchState(draft({ agent_enabled: true }), none, 'agent')).toEqual({ checked: true });
    expect(switchState(draft({ address_mode: 'one_name', mcp_enabled: false }), none, 'mcp')).toEqual({ checked: false });
  });

  it('C ohne Namen: aus und gesperrt, auch wenn der Entwurf an sagt', () => {
    const shared = draft({ address_mode: 'shared_port', public_base_url: 'https://lcm.example.com', agent_enabled: true, mcp_enabled: true });
    expect(switchState(shared, none, 'agent')).toEqual({ checked: false, lock: 'noName' });
    expect(switchState({ ...shared, public_mcp_url: 'https://mcp.example.com' }, none, 'mcp')).toEqual({ checked: true });
  });

  it('die config.json sperrt den Agenten und zeigt den gespeicherten Stand', () => {
    const locked = readGlobal({ address_locks: { agent_enabled: true } });
    expect(switchState(draft({ agent_enabled: false }), locked, 'agent')).toEqual({ checked: false, lock: 'config' });
    expect(switchState(draft({ mcp_enabled: true }), locked, 'mcp')).toEqual({ checked: true });
  });

  it('beim Wechsel in Modus C gehen Dienste ohne Namen aus', () => {
    const before = draft({ agent_enabled: true, mcp_enabled: true });
    expect(sharedSwitchOff(before, { public_base_url: 'https://lcm.example.com' })).toEqual({ agent_enabled: false, mcp_enabled: false });
    expect(sharedSwitchOff(before, { public_base_url: 'https://lcm.example.com', public_mcp_url: 'https://mcp.example.com' })).toEqual({ agent_enabled: false });
    expect(sharedSwitchOff(draft({ agent_enabled: false, mcp_enabled: false }), {})).toEqual({});
  });
});
