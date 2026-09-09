// Warnung in der Browser-Konsole gegen Self-XSS.
//
// Der Anmelde-Token liegt im localStorage, und die Konsole ist der eine Ort,
// an dem die Content-Security-Policy nicht hilft: Was jemand dort eintippt,
// läuft mit allen Rechten der Sitzung. Die verbreitete Masche ist, ein Opfer
// per Chat oder „Support"-Anruf dazu zu bringen, einen Schnipsel hier
// einzufügen - danach hat der Anrufer den Token und damit Root auf der Flotte.
//
// Die Warnung erscheint einmal beim Laden, in der Sprache der Oberfläche.
import { i18n } from '../stores/i18n.svelte.js';

const TEXT = {
  de: {
    stop: 'STOPP!',
    body:
      'Diese Konsole ist für Entwickler. Wer dich bittet, hier etwas einzufügen ' +
      'oder einzutippen, will deine LCM-Sitzung übernehmen - und damit Root-Zugriff ' +
      'auf alle verwalteten Server.\n\nWenn du nicht genau weißt, was ein Befehl tut: ' +
      'nichts einfügen, Fenster schließen.',
  },
  en: {
    stop: 'STOP!',
    body:
      'This console is for developers. Anyone asking you to paste or type ' +
      'something here wants to take over your LCM session - and with it root ' +
      'access to every managed server.\n\nIf you do not know exactly what a ' +
      'command does: paste nothing, close this window.',
  },
};

export function printConsoleWarning() {
  const t = TEXT[i18n.locale] ?? TEXT.de;
  try {
    console.log('%c' + t.stop, 'color:#dc3545;font-size:48px;font-weight:bold;');
    console.log('%c' + t.body, 'font-size:16px;line-height:1.4;');
  } catch {
    // Ohne Konsole gibt es nichts zu warnen.
  }
}
