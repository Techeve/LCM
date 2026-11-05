import { siAlmalinux, siDebian, siFedora, siLinux, siOpensuse, siRedhat, siRockylinux, siUbuntu } from 'simple-icons';

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

