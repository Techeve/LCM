import { describe, expect, it } from 'vitest';
import { osDisplay } from '../pages/servers/server-os';
import { consolesByServer, hostLabel, isOffline, isToleratedOutage, normaliseServerList } from './servers-list';
import { dsmUrl } from './servers';

describe('server list extras', () => {
  it('reads the OS choices next to the page', () => {
    expect(normaliseServerList({ items: [], total: 0, os_names: ['Debian GNU/Linux', '', 3] }).osNames).toEqual(['Debian GNU/Linux']);
    expect(normaliseServerList({ items: [], total: 0 }).osNames).toEqual([]);
  });

  it('groups open consoles by server without duplicates', () => {
    const map = consolesByServer([
      { server_id: 3, actor: 'admin' },
      { server_id: 3, actor: 'admin' },
      { server_id: 3, actor: 'tony' },
      { server_id: 'x', actor: 'nope' },
    ]);
    expect(map.get(3)).toEqual(['admin', 'tony']);
    expect(map.size).toBe(1);
  });

  it('calls a server offline only from the second failed check, tolerated when not red', () => {
    expect(isOffline({ reachable: false, failed_checks: 1 })).toBe(false);
    expect(isOffline({ reachable: false, failed_checks: 2 })).toBe(true);
    expect(isOffline({ reachable: true, failed_checks: 5 })).toBe(false);
    expect(isToleratedOutage({ reachable: false, status: 'excellent' })).toBe(true);
    expect(isToleratedOutage({ reachable: false, status: 'red' })).toBe(false);
  });

  it('shows host and SSH port, agents without port, IPv6 in brackets', () => {
    expect(hostLabel({ host: '10.0.0.1', ssh_port: 2222, transport: 'ssh' })).toBe('10.0.0.1:2222');
    expect(hostLabel({ host: 'fd00::1', ssh_port: 22, transport: 'ssh' })).toBe('[fd00::1]:22');
    expect(hostLabel({ host: 'nas', ssh_port: 22, transport: 'agent' })).toBe('nas');
  });
});

describe('osDisplay', () => {
  const base = { os_id: 'debian', os_name: 'Debian GNU/Linux', os_version: '12', proxmox_type: '', proxmox_version: '', hardware_model: '' };

  it('names Proxmox by product and version instead of Debian', () => {
    const shown = osDisplay({ ...base, proxmox_type: 'pve', proxmox_version: '8.2.4' });
    expect(shown.label).toBe('Proxmox VE 8.2.4');
    expect(shown.logo.title).toBe('Proxmox');
  });

  it('recognises devices by OS id and hardware model', () => {
    expect(osDisplay({ ...base, os_id: 'routeros', os_name: 'RouterOS', os_version: '7.15' }).logo.title).toBe('MikroTik');
    expect(osDisplay({ ...base, os_id: 'dsm', os_name: '', os_version: '' })).toMatchObject({ label: 'Synology DSM' });
    expect(osDisplay({ ...base, hardware_model: 'Raspberry Pi 4 Model B' }).logo.title).toBe('Raspberry Pi');
    expect(osDisplay(base)).toMatchObject({ label: 'Debian GNU/Linux 12' });
    expect(osDisplay({ ...base, os_id: 'unknown' }).logo.title).toBe('Linux');
  });
});

describe('dsmUrl', () => {
  it('öffnet HTTPS auf dem DSM-Port, ohne Port auf 5001', () => {
    expect(dsmUrl({ host: '192.168.6.133', dsm_port: 0 })).toBe('https://192.168.6.133:5001/');
    expect(dsmUrl({ host: 'nas.example.org', dsm_port: 5443 })).toBe('https://nas.example.org:5443/');
  });
  it('setzt IPv6-Adressen in eckige Klammern', () => {
    expect(dsmUrl({ host: 'fd00::5', dsm_port: 0 })).toBe('https://[fd00::5]:5001/');
    expect(dsmUrl({ host: '[fd00::5]', dsm_port: 0 })).toBe('https://[fd00::5]:5001/');
  });
});
