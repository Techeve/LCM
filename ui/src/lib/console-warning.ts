// Warnung gegen Self-XSS in der Browser-Konsole. Die Konsole ist der eine Ort, an
// dem die Content-Security-Policy nicht hilft: Was dort jemand eintippt, läuft mit
// allen Rechten der Sitzung. Die übliche Masche ist ein „Support“-Anruf, der das
// Opfer einen Schnipsel einfügen lässt. Einmal beim Start, in der Sprache der Oberfläche.

import { t } from './i18n';

export function printConsoleWarning(log: (...args: string[]) => void = console.log): void {
  log(`%c${t('console.stop')}`, 'color:#dc3545;font-size:48px;font-weight:bold;');
  log(`%c${t('console.body')}`, 'font-size:16px;line-height:1.4;');
}
