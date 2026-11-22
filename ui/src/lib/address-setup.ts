// Der Einrichtungsassistent: Das Backend nennt den Stand am Konto (`setup_step`, nur für Konten, die die Einstellungen
// ändern dürfen). Solange er auf einem Schritt der Assistentenseite steht, führt der Router nach den Pflichtschritten
// des Kontos (Passwort, 2FA) dorthin; `intro` (die Einführung auf der Seite des LCM-Hosts) und leer (erledigt) lassen
// jede Seite durch. Die Seite des Assistenten steht in pages/setup/.

import { remediationOf, remediationRedirect } from './remediation';
import { isWizardStep, readStep, type SetupStep } from './setup-steps';

export const SETUP_PATH = '/setup';

interface SetupState {
  setup_step?: string;
  must_change_password?: boolean;
  must_setup_2fa?: boolean;
}

/** Der Stand des Assistenten; `null`, wenn nichts offen ist. */
export function setupStepOf(user: SetupState | null): SetupStep | null {
  return readStep(user?.setup_step);
}

/** Muss das Konto den Assistenten noch durchlaufen? */
export function setupPending(user: SetupState | null): boolean {
  return isWizardStep(setupStepOf(user));
}

/**
 * Wohin eine Adresse umgeleitet wird, solange der Assistent offen ist; `null`, wenn sie bleiben darf. Solange das
 * Konto noch Pflichtschritte hat (Passwort, 2FA), wartet der Assistent: Sonst schickte er die Kontoseite weiter, die
 * Pflichtschritte schickten zurück, und der Router liefe im Kreis.
 */
export function setupRedirect(user: SetupState | null, path: string): string | null {
  if (remediationPending(user) || !setupPending(user) || path === SETUP_PATH || path.startsWith(`${SETUP_PATH}/`)) return null;
  return SETUP_PATH;
}

/** Erst die Pflichtschritte des Kontos, dann der Assistent. */
export function routeRedirect(user: SetupState | null, path: string): string | null {
  return remediationRedirect(remediationState(user), path) ?? setupRedirect(user, path);
}

function remediationState(user: SetupState | null) {
  return user ? { must_change_password: !!user.must_change_password, must_setup_2fa: !!user.must_setup_2fa } : null;
}

function remediationPending(user: SetupState | null): boolean {
  return remediationOf(remediationState(user)) !== null;
}
