// Die Schritte des Einrichtungsassistenten nach Passwort und 2FA, in der Reihenfolge, in der der Server sie führt
// (`setup_step` am Konto und in den globalen Einstellungen). Reine Daten und Regeln; die Seite steht in pages/setup/.

/** Die Schritte der Assistentenseite (`/setup`). */
export const WIZARD_STEPS = ['session', 'addresses', 'services', 'access', 'certificate', 'onboarding', 'mail', 'retention', 'backup', 'subscription'] as const;
export type WizardStep = (typeof WIZARD_STEPS)[number];

/** `intro` ist der letzte Stand: Die Einführung steht auf der Seite des LCM-Hosts, nicht im Assistenten. */
export type SetupStep = WizardStep | 'intro';

/** E-Mail, Backup und Subscription lassen sich überspringen; alle anderen Schritte bauen auf der Eingabe auf. Onboarding: ein leeres Feld bleibt leer. */
const SKIPPABLE: readonly WizardStep[] = ['mail', 'backup', 'subscription'];

/** Der Stand aus einer Antwort des Servers; `null` heißt: erledigt oder unbekannt. */
export function readStep(value: unknown): SetupStep | null {
  if (value === 'intro') return 'intro';
  return WIZARD_STEPS.find((step) => step === value) ?? null;
}

export function isWizardStep(step: SetupStep | null): step is WizardStep {
  return step !== null && step !== 'intro';
}

export function isSkippable(step: WizardStep): boolean {
  return SKIPPABLE.includes(step);
}

/** Der Schritt danach; nach `intro` kommt „fertig“ (leer). */
export function nextStep(step: SetupStep): SetupStep | '' {
  if (step === 'intro') return '';
  return WIZARD_STEPS[WIZARD_STEPS.indexOf(step) + 1] ?? 'intro';
}

/** Der Schritt davor; `null` beim ersten Schritt (dort gibt es kein „Zurück“). */
export function previousStep(step: WizardStep): WizardStep | null {
  return WIZARD_STEPS[WIZARD_STEPS.indexOf(step) - 1] ?? null;
}

/** Die Nummer des Schritts in der Anzeige „Schritt x von n“ (ab 1). */
export function stepNumber(step: WizardStep): number {
  return WIZARD_STEPS.indexOf(step) + 1;
}
