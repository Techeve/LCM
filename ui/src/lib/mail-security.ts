// Verschlüsselung des SMTP-Versands, für den System-Mailer und den E-Mail-Kanal gleich. Die Kürzel
// entsprechen `domain.MailSecurity*` im Backend.

import { t } from './i18n';

export const MAIL_SECURITY = { tls: 'tls', starttls: 'starttls', none: 'none' } as const;

export type MailSecurity = (typeof MAIL_SECURITY)[keyof typeof MAIL_SECURITY];

/** Vorgabe für neue Kanäle: STARTTLS ist Pflicht, die Verbindung nie still unverschlüsselt. */
export const DEFAULT_MAIL_SECURITY: MailSecurity = MAIL_SECURITY.starttls;

/** Üblicher Port je Modus: SMTPS 465, Submission 587, einfaches SMTP 25. */
export const MAIL_SECURITY_PORT: Record<MailSecurity, number> = { tls: 465, starttls: 587, none: 25 };

export const MAIL_SECURITY_MODES: MailSecurity[] = [MAIL_SECURITY.tls, MAIL_SECURITY.starttls, MAIL_SECURITY.none];

export function isMailSecurity(value: unknown): value is MailSecurity {
  return value === MAIL_SECURITY.tls || value === MAIL_SECURITY.starttls || value === MAIL_SECURITY.none;
}

/** Das alte Ja/Nein: true heißt STARTTLS, false unverschlüsselt. */
export function securityFromLegacy(useTls: boolean): MailSecurity {
  return useTls ? MAIL_SECURITY.starttls : MAIL_SECURITY.none;
}

/** Der Modus aus `security`; fehlt er (Stand vor der Auswahl), gilt das alte `use_tls`, fehlt auch das, STARTTLS. */
export function readMailSecurity(security: unknown, useTls: unknown): MailSecurity {
  if (isMailSecurity(security)) return security;
  return securityFromLegacy(useTls !== false);
}

const LABEL_KEYS: Record<MailSecurity, string> = {
  tls: 'settings.mailSecurity.tls',
  starttls: 'settings.mailSecurity.starttls',
  none: 'settings.mailSecurity.none',
};

/** Auswahlliste mit dem üblichen Port im Text. */
export function mailSecurityOptions(): { value: string; label: string }[] {
  return MAIL_SECURITY_MODES.map((mode) => ({ value: mode, label: t(LABEL_KEYS[mode], { port: MAIL_SECURITY_PORT[mode] }) }));
}

/**
 * Der Port nach einem Moduswechsel: steht er auf einem der Standardports, folgt er dem neuen Modus;
 * ein eigener Port bleibt unangetastet.
 */
export function portForSecurity(port: string, security: MailSecurity): string {
  const standard = Object.values(MAIL_SECURITY_PORT).map(String);
  return standard.includes(port.trim()) ? String(MAIL_SECURITY_PORT[security]) : port;
}
