import { siAlmalinux, siDebian, siFedora, siLinux, siMikrotik, siOpensuse, siProxmox, siRaspberrypi, siRedhat, siRockylinux, siSynology, siUbuntu } from 'simple-icons';

export interface OsLogo {
  title: string;
  path: string;
  hex: string;
}

/** Nur die benötigten Logos aus simple-icons, damit das Bündel klein bleibt. */
export const osLogos: Record<string, OsLogo> = {
  debian: siDebian,
  ubuntu: siUbuntu,
  rocky: siRockylinux,
  alma: siAlmalinux,
  opensuse: siOpensuse,
  rhel: siRedhat,
  fedora: siFedora,
  linux: siLinux,
};


// Das Synology-Grau verschwindet auf der hellen Kachel; dunkler gesetzt bleibt es lesbar.
const SYNOLOGY: OsLogo = { ...siSynology, hex: '3B3B3C' };

export interface OsLogoFields {
  os_id: string;
  proxmox_type?: string;
  hardware_model?: string;
}

/**
 * Das Logo zum Server: Produkt vor Distribution. Proxmox und ein Raspberry Pi melden sich als Debian, RouterOS und DSM
 * kommen über ihre API und haben kein Distributionslogo. Eine Auswahl für Serverliste, Seitenkopf und Tabellen.
 */
export function osLogoFor(server: OsLogoFields): OsLogo {
  if (server.proxmox_type) return siProxmox;
  if (server.os_id === 'routeros') return siMikrotik;
  if (server.os_id === 'dsm') return SYNOLOGY;
  if (/raspberry pi/i.test(server.hardware_model ?? '')) return siRaspberrypi;
  return osLogos[server.os_id] ?? osLogos.linux!;
}
