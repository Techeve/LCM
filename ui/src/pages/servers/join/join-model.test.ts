import { describe, expect, it } from 'vitest';
import {
  agentCommands,
  dsmBody,
  dsmProblems,
  duplicateOf,
  emptyDsmForm,
  emptyRouterOSForm,
  emptySshForm,
  hostKeyFile,
  hostProblem,
  joinBody,
  modeOf,
  parsePort,
  reconnectBody,
  reconnectIdOf,
  routerOSBody,
  routerOSProblems,
  sshProblems,
} from './join-model';

describe('address state', () => {
  it('falls back to SSH for unknown modes', () => {
    expect(modeOf('dsm')).toBe('dsm');
    expect(modeOf('telnet')).toBe('ssh');
    expect(modeOf(null)).toBe('ssh');
  });

  it('accepts only numeric reconnect ids', () => {
    expect(reconnectIdOf('42')).toBe(42);
    expect(reconnectIdOf('4x')).toBeNull();
    expect(reconnectIdOf(null)).toBeNull();
  });
});

describe('validation', () => {
  it('checks host and port', () => {
    expect(hostProblem('')).toBe('required');
    expect(hostProblem('ssh://web01')).toBe('host');
    expect(hostProblem('web 01')).toBe('host');
    expect(hostProblem('web01.example.com')).toBeUndefined();
    expect(hostProblem('fe80::1%eth0')).toBeUndefined();
    expect(parsePort('22')).toBe(22);
    expect(parsePort('0')).toBeNull();
    expect(parsePort('70000')).toBeNull();
    expect(parsePort('22a')).toBeNull();
  });

  it('needs name, host and login user for SSH, but no password', () => {
    expect(sshProblems(emptySshForm())).toEqual({ name: 'required', host: 'required' });
    expect(sshProblems({ ...emptySshForm(), name: 'web01', host: '10.0.0.5', loginUser: ' ' })).toEqual({ loginUser: 'required' });
    expect(sshProblems({ ...emptySshForm(), host: '10.0.0.5' }, false)).toEqual({});
  });

  it('allows an empty RouterOS password but needs the DSM password', () => {
    expect(routerOSProblems({ ...emptyRouterOSForm(), name: 'r1', host: '192.168.88.1' })).toEqual({});
    expect(dsmProblems({ ...emptyDsmForm(), name: 'nas', host: 'nas.lan', account: 'lcm' })).toEqual({ password: 'required' });
  });
});

describe('request bodies', () => {
  const form = { ...emptySshForm(), name: ' web01 ', host: ' 10.0.0.5 ', port: '2222', password: 'geheim', restricted: true };

  it('sends the confirmed fingerprint and the password only for password login', () => {
    expect(joinBody(form, 'SHA256:abc')).toEqual({
      name: 'web01',
      host: '10.0.0.5',
      port: 2222,
      login_user: 'root',
      login_password: 'geheim',
      auth_method: 'password',
      confirmed_fingerprint: 'SHA256:abc',
      restricted_access: true,
    });
    expect(reconnectBody({ ...form, authMethod: 'key' }, 'SHA256:abc').login_password).toBe('');
    expect(reconnectBody(form, 'x')).not.toHaveProperty('name');
  });

  it('builds RouterOS and DSM bodies', () => {
    const ro = routerOSBody({ ...emptyRouterOSForm(), name: 'r1', host: '192.168.88.1', authMethod: 'key', password: 'x' }, 'SHA256:r');
    expect(ro).toMatchObject({ login_user: 'admin', login_password: '', auth_method: 'key', confirmed_fingerprint: 'SHA256:r', port: 22 });
    expect(dsmBody({ ...emptyDsmForm(), name: 'nas', host: 'nas', account: ' lcm ', password: 'p' }, 'AB:CD')).toEqual({
      name: 'nas',
      host: 'nas',
      port: 5001,
      account: 'lcm',
      password: 'p',
      confirmed_fingerprint: 'AB:CD',
    });
  });
});

describe('duplicateOf', () => {
  const servers = [
    { id: 1, name: 'web01', host: '10.0.0.5', ssh_port: 22 },
    { id: 2, name: 'nat01', host: 'gw.example.com', ssh_port: 2201 },
  ];

  it('blocks the same host and port, only notes the same host on another port', () => {
    expect(duplicateOf(servers, '10.0.0.5', 22).taken?.name).toBe('web01');
    expect(duplicateOf(servers, 'GW.example.com', 2202)).toEqual({ taken: null, sameHost: servers[1] });
    expect(duplicateOf(servers, '10.0.0.6', 22)).toEqual({ taken: null, sameHost: null });
  });

  it('ignores the server that is being reconnected', () => {
    expect(duplicateOf(servers, '10.0.0.5', 22, 1).taken).toBeNull();
  });
});

describe('helpers', () => {
  it('names the host key file for the probed key type', () => {
    expect(hostKeyFile('ssh-ed25519')).toBe('/etc/ssh/ssh_host_ed25519_key.pub');
    expect(hostKeyFile('ecdsa-sha2-nistp256')).toBe('/etc/ssh/ssh_host_ecdsa_key.pub');
    expect(hostKeyFile('ssh-rsa')).toBe('/etc/ssh/ssh_host_rsa_key.pub');
  });

  it('points the agent at its own port and downloads the binary from the UI', () => {
    const where = { origin: 'https://lcm.example.com', protocol: 'https:', hostname: 'lcm.example.com' };
    const commands = agentCommands(where, 9333, 'tok');
    expect(commands.enroll).toBe('sudo lcm-agent enroll https://lcm.example.com:9333 tok');
    expect(commands.manual).toContain('https://lcm.example.com/api/v1/agent/download/amd64');
    expect(commands.manual.endsWith(commands.enroll)).toBe(true);
    expect(agentCommands(where, 0, 'tok').enroll).toBe('sudo lcm-agent enroll https://lcm.example.com tok');
  });

  it('accepts the self-signed certificate only together with the key pin of this LCM', () => {
    const where = { origin: 'https://lcm.example.com', protocol: 'https:', hostname: 'lcm.example.com' };
    const target = '-o /usr/local/bin/lcm-agent https://lcm.example.com/api/v1/agent/download/amd64';
    const pinned = agentCommands(where, 0, 'tok', 'cGluCg==').manual;
    expect(pinned.startsWith(`sudo curl -fLk --pinnedpubkey sha256//cGluCg== ${target} || sudo curl -fL ${target} && `)).toBe(true);
    // Ohne Pin kein -k: dann prüft curl gegen die Systemzertifikate, statt jedes anzunehmen.
    const plain = agentCommands(where, 0, 'tok').manual;
    expect(plain.startsWith(`sudo curl -fL ${target} && `)).toBe(true);
    expect(plain).not.toMatch(/ -\w*k/);
  });
});
