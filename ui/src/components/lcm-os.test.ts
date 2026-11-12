import { afterEach, describe, expect, it } from 'vitest';
import './lcm-os';
import type { LcmOs } from './lcm-os';
import { osLogoFor } from './os-logos';

let el: LcmOs | undefined;

async function logoOf(props: Partial<Pick<LcmOs, 'os' | 'product' | 'model'>>): Promise<string> {
  el?.remove();
  el = document.createElement('lcm-os') as LcmOs;
  Object.assign(el, props);
  document.body.append(el);
  await el.updateComplete;
  return el.shadowRoot!.querySelector('.tile')!.getAttribute('aria-label') ?? '';
}

afterEach(() => el?.remove());

describe('lcm-os', () => {
  it('zeigt bei API-Geräten das Produktlogo statt des Pinguins', async () => {
    expect(await logoOf({ os: 'dsm' })).toBe('Synology');
    expect(await logoOf({ os: 'routeros' })).toBe('MikroTik');
  });

  it('wählt wie die Serverliste nach Proxmox-Typ und Hardware-Modell', async () => {
    expect(await logoOf({ os: 'debian', product: 'pve' })).toBe('Proxmox');
    expect(await logoOf({ os: 'debian', model: 'Raspberry Pi 4 Model B' })).toBe('Raspberry Pi');
    expect(await logoOf({ os: 'debian' })).toBe('Debian');
    expect(await logoOf({ os: 'unbekannt' })).toBe('Linux');
  });
});

describe('osLogoFor', () => {
  it('setzt das Synology-Logo dunkel, damit es auf der hellen Kachel lesbar bleibt', () => {
    expect(osLogoFor({ os_id: 'dsm' }).hex).toBe('3B3B3C');
  });
});
