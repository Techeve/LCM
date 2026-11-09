import { LitElement, html, nothing, svg } from 'lit';
import { siMikrotik, siProxmox, siRaspberrypi, siSynology } from 'simple-icons';
import { osTileStyles } from '../../components/lcm-os';
import { osLogos, type OsLogo } from '../../components/os-logos';
import { baseStyles } from '../../components/shared';
import type { ServerItem } from '../../api/servers';

// Proxmox-Systeme melden sich im os-release schlicht als Debian, ein Raspberry Pi ebenso. Die Serverliste zeigt
// deshalb das Produkt und sein Logo: „Proxmox VE 8.2“ statt „Debian 12“. RouterOS und DSM kommen über ihre API.
export const PROXMOX_PRODUCTS: Record<string, string> = {
  pve: 'Proxmox VE',
  pbs: 'Proxmox Backup Server',
  pmg: 'Proxmox Mail Gateway',
  pdm: 'Proxmox Datacenter Manager',
};

// Das Synology-Grau verschwindet auf der hellen Kachel; dunkler gesetzt bleibt es lesbar.
const SYNOLOGY: OsLogo = { ...siSynology, hex: '3B3B3C' };

export interface OsDisplay {
  logo: OsLogo;
  label: string;
}

type OsFields = Pick<ServerItem, 'os_id' | 'os_name' | 'os_version' | 'proxmox_type' | 'proxmox_version' | 'hardware_model'>;

export function osDisplay(server: OsFields): OsDisplay {
  const plain = [server.os_name, server.os_version].filter(Boolean).join(' ');
  if (server.proxmox_type) {
    const product = PROXMOX_PRODUCTS[server.proxmox_type] ?? 'Proxmox';
    return { logo: siProxmox, label: [product, server.proxmox_version].filter(Boolean).join(' ') };
  }
  if (server.os_id === 'routeros') return { logo: siMikrotik, label: plain || 'RouterOS' };
  if (server.os_id === 'dsm') return { logo: SYNOLOGY, label: plain || 'Synology DSM' };
  if (/raspberry pi/i.test(server.hardware_model)) return { logo: siRaspberrypi, label: plain };
  return { logo: osLogos[server.os_id] ?? osLogos.linux!, label: plain };
}

/** <lcm-server-os .server=${…} ?compact> – Logo (Kachel wie `lcm-os`) und Name von Betriebssystem oder Produkt. */
export class LcmServerOs extends LitElement {
  static override properties = { server: { attribute: false }, compact: { type: Boolean } };
  static override styles = [baseStyles, osTileStyles];

  declare server: OsFields | undefined;
  declare compact: boolean;

  constructor() {
    super();
    this.server = undefined;
    this.compact = false;
  }

  override render() {
    if (!this.server) return null;
    const { logo, label } = osDisplay(this.server);
    const title = label || logo.title;
    return html`<span class="tile" role=${this.compact ? 'img' : 'presentation'} aria-label=${this.compact ? title : nothing} title=${this.compact ? title : nothing}>
        <svg viewBox="0 0 24 24" aria-hidden="true">${svg`<path d=${logo.path} fill=${`#${logo.hex}`}></path>`}</svg>
      </span>
      ${this.compact ? null : html`<span class="name">${label}</span>`}`;
  }
}
customElements.define('lcm-server-os', LcmServerOs);
