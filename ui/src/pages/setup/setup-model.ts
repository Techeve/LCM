// Was die Schritte des Einrichtungsassistenten an das Backend schicken und wie sie prüfen. Reine Logik; die Seite steht
// in setup-page.ts, die Schritte in step-*.ts.

import type { GlobalSettings } from '../../api/settings';
import type { ServerItem } from '../../api/servers';
import { SETUP_PATH } from '../../lib/address-setup';
import { nextStep, previousStep, type WizardStep } from '../../lib/setup-steps';
import { LIMITS } from '../../lib/validate';
import { ADDRESS_FIELDS, ENABLED_KEY, PORT_KEY, validateAddresses } from '../settings/addresses/address-model';
import { draftOf, flag, patchBody, text, validateDraft, type Draft, type FieldErrors } from '../settings/system/form-state';

/** Der Teil des Einstellungs-Zugriffs, den der Assistent braucht (`settingsApi`). */
export interface SetupApi {
  patchGlobal(body: Record<string, unknown>): Promise<GlobalSettings>;
}

/** Der Entwurf der Adressfelder aus den gespeicherten Einstellungen; Schritt 2 liest die Schalter daraus mit. */
export function setupDraft(settings: GlobalSettings): Draft {
  return draftOf(settings, ADDRESS_FIELDS);
}

/** Was beim Speichern der Adressen nicht durchgeht: Werte sind Übersetzungsschlüssel, die Schlüssel die der Felder. */
export function setupErrors(draft: Draft): FieldErrors {
  return { ...validateDraft(draft, ADDRESS_FIELDS), ...validateAddresses(draft) };
}

/** Nur die geänderten Adressfelder. */
export function addressBody(draft: Draft, base: Draft): Record<string, unknown> {
  return patchBody(draft, base, ADDRESS_FIELDS);
}

/** Hat sich der Port der Oberfläche geändert? Dann folgt die Seite der neuen Adresse. */
export function changesUiPort(body: Record<string, unknown>): boolean {
  return PORT_KEY.ui in body;
}

/** Der Weg der Anmeldung im Browser samt Bestätigung; die einmalige Frage der Oberfläche entfällt damit. */
export function sessionBody(mode: string): Record<string, unknown> {
  return { browser_session_mode: mode, browser_session_mode_confirmed: true };
}

/** Der gespeicherte Weg der Anmeldung; leer heißt Arbeitsspeicher. */
export function sessionModeOf(settings: GlobalSettings): string {
  return settings.browser_session_mode === 'cookie' ? 'cookie' : 'memory';
}

const SERVICE_FIELDS = [flag(ENABLED_KEY.agent), flag(ENABLED_KEY.mcp)];

/** Nur die geänderten Schalter für Agent und MCP. */
export function servicesBody(draft: Draft, base: Draft): Record<string, unknown> {
  return patchBody(draft, base, SERVICE_FIELDS);
}

export const ONBOARDING_FIELDS = [text('default_ssh_user', LIMITS.loginUser)];

export function onboardingDraft(settings: GlobalSettings): Draft {
  return draftOf(settings, ONBOARDING_FIELDS);
}

export function onboardingErrors(draft: Draft): FieldErrors {
  return validateDraft(draft, ONBOARDING_FIELDS);
}

export function onboardingBody(draft: Draft, base: Draft): Record<string, unknown> {
  return patchBody(draft, base, ONBOARDING_FIELDS);
}

/** Der Assistent auf einer Adresse der Oberfläche (Ursprung oder volle Adresse); dort geht es beim gespeicherten Schritt weiter. */
export function wizardUrl(address: string): string {
  return new URL(SETUP_PATH, address).href;
}

/** Der Körper, der einen Schritt abschließt: seine Daten und der nächste Schritt. Der Server lässt nur vorwärts gehen. */
export function completeBody(step: WizardStep, data: Record<string, unknown> = {}): Record<string, unknown> {
  return { ...data, setup_step: nextStep(step) };
}

/** Speichert die Daten des Schritts und rückt zum nächsten vor (beim Backup auch „Überspringen“: ohne Daten). */
export function completeStep(api: SetupApi, step: WizardStep, data: Record<string, unknown> = {}): Promise<GlobalSettings> {
  return api.patchGlobal(completeBody(step, data));
}

/** Zu einem früheren Schritt: Der Server speichert nur den Stand; was die Schritte dazwischen gespeichert haben, bleibt. */
export function goToStep(api: SetupApi, target: WizardStep): Promise<GlobalSettings> {
  return api.patchGlobal({ setup_step: target });
}

/** Einen Schritt zurück. `null` beim ersten Schritt. */
export function goBack(api: SetupApi, step: WizardStep): Promise<GlobalSettings> | null {
  const previous = previousStep(step);
  return previous ? goToStep(api, previous) : null;
}

/** Die Einführung auf der Seite des LCM-Hosts schließen; sie kommt nicht wieder. */
export function closeIntro(api: SetupApi): Promise<GlobalSettings> {
  return api.patchGlobal({ setup_step: '' });
}

/** Die Seite des LCM-Hosts in der Serverliste; ohne ihn (oder ohne Recht auf die Liste) bleibt die Startseite. */
export function lcmHostPath(servers: readonly Pick<ServerItem, 'id' | 'is_lcm_host'>[]): string {
  const host = servers.find((server) => server.is_lcm_host);
  return host ? `/servers/${host.id}` : '/';
}
