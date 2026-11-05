// Eigene Aktionen: Formular, Prüfung, Zählung der Kommandos und das Maskieren von Geheimnissen.
// Die Muster folgen der Befehlsvorschau des Backends (`maskPreviewSecrets`, `redactSecrets`), damit die
// Liste nichts zeigt, was Vorschau und Job-Protokoll verbergen.

import type { CustomAction } from '../../../api/settings-more';
import { LIMITS, charCount, hasControlChars } from '../../../lib/validate';

export const MASK = '••••';

const PATTERNS: [RegExp, string][] = [
  // curl/wget: -u user:pass
  [/((?:-u|--user)[ =][^\s:]+:)(\S+)/g, `$1${MASK}`],
  // Schlüssel=Wert bzw. Schlüssel: Wert (password=…, token: …)
  [/(password|passwd|pwd|secret|token|api[_-]?key|access[_-]?key|auth[_-]?token)(\s*[=:]\s*)("[^"]*"|'[^']*'|\S+)/gi, `$1$2${MASK}`],
  // Basic-Auth in Adressen: https://user:pass@host
  [/([a-zA-Z][a-zA-Z0-9+.-]*:\/\/[^:/@\s]+:)([^@/\s]+)(@)/g, `$1${MASK}$3`],
  // Umgebungsvariablen mit sprechendem Namen: DB_PASSWORD=…
  [/\b([A-Z][A-Z0-9_]*(?:KEY|SECRET|TOKEN|PASS|PASSWORD|PWD|CREDENTIAL|AUTH)[A-Z0-9_]*=)("[^"]*"|'[^']*'|\S+)/g, `$1${MASK}`],
  // --password geheim, --token abc
  [/(--(?:password|passwd|token|secret|api-key|apikey|auth-token)\s+)("[^"]*"|'[^']*'|\S+)/gi, `$1${MASK}`],
  // Authorization: Bearer …
  [/(authorization:\s*(?:bearer|basic|token)\s+)([^\s"']+)/gi, `$1${MASK}`],
  // echo 'geheim' | sudo -S
  [/((?:echo|printf)\s+(?:-n\s+)?)("[^"]*"|'[^']*'|\S+)(\s*\|\s*sudo\s+-S\b)/g, `$1${MASK}$3`],
  // Private Schlüssel im Klartext
  [/-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----/g, MASK],
];

/** Ersetzt Passwörter, Tokens und Schlüssel durch ••••. */
export function maskSecrets(text: string): string {
  return PATTERNS.reduce((result, [pattern, replacement]) => result.replace(pattern, replacement), text);
}

export function hasSecrets(text: string): boolean {
  return maskSecrets(text) !== text;
}

/** Ausgeführte Zeilen: ohne Leerzeilen und Kommentare (wie der Executor). */
export function commandLines(text: string): string[] {
  return text
    .split('\n')
    .map((line) => line.trim())
    .filter((line) => line && !line.startsWith('#'));
}

export interface ActionForm {
  name: string;
  description: string;
  commands: string;
}

export function actionFormOf(action: CustomAction): ActionForm {
  return { name: action.name, description: action.description, commands: action.commands };
}

export type ActionErrors = Partial<Record<keyof ActionForm, string>>;

export function validateActionForm(form: ActionForm): ActionErrors {
  const errors: ActionErrors = {};
  if (!form.name.trim()) errors.name = 'settings.customActions.form.nameRequired';
  else if (charCount(form.name.trim()) > LIMITS.name) errors.name = 'settings.customActions.form.nameTooLong';
  else if (hasControlChars(form.name)) errors.name = 'check.oneLine';
  if (charCount(form.description.trim()) > LIMITS.description) errors.description = 'check.tooLong';
  else if (hasControlChars(form.description, true)) errors.description = 'check.controlChars';
  if (commandLines(form.commands).length === 0) errors.commands = 'settings.customActions.form.commandsRequired';
  else if (charCount(form.commands) > LIMITS.script) errors.commands = 'check.tooLong';
  else if (hasControlChars(form.commands, true)) errors.commands = 'check.controlChars';
  return errors;
}

export function actionBody(form: ActionForm): Omit<CustomAction, 'id'> {
  return { name: form.name.trim(), description: form.description.trim(), commands: form.commands };
}
