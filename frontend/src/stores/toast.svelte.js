// Globale Meldungen (Toasts).
//
// Vorher stand jede Rueckmeldung als Alert ganz oben auf der Seite. Bei einer
// Aktion weit unten - Paket entfernen, Docker-Image ziehen, Sperre aufheben -
// erschien die Meldung damit ausserhalb des Sichtbereichs: Der Klick wirkte
// folgenlos. Die Toasts liegen stattdessen fest am unteren Bildschirmrand,
// sind also unabhaengig von der Scrollposition sichtbar, und liegen ueber dem
// Modal-Backdrop - Fehler aus einem Dialog verschwinden nicht mehr dahinter.
//
// Bewusste Entscheidung zur Lebensdauer: Erfolgs- und Info-Meldungen blenden
// sich nach kurzer Zeit selbst aus, FEHLER bleiben stehen, bis sie
// weggeklickt (oder von einer neuen Aktion ersetzt) werden. Eine
// Fehlermeldung, die von selbst verschwindet, bevor man sie gelesen hat, ist
// schlimmer als keine.

const AUTO_DISMISS_MS = 6000;

function createToasts() {
  let items = $state([]);
  let seq = 0;
  // Laufende Timer je Toast-ID, damit ein vorzeitiges Schliessen den Timer
  // nicht als Leiche zuruecklaesst.
  const timers = new Map();

  function dismiss(id) {
    const timer = timers.get(id);
    if (timer) {
      clearTimeout(timer);
      timers.delete(id);
    }
    items = items.filter((t) => t.id !== id);
  }

  function show(kind, text, opts = {}) {
    const message = String(text ?? '').trim();
    if (!message) return 0;
    const id = ++seq;
    // Dieselbe Meldung nicht doppelt stapeln (z.B. zwei Klicks auf denselben
    // fehlschlagenden Knopf) - stattdessen die alte ersetzen.
    items = [...items.filter((t) => !(t.kind === kind && t.text === message)), { id, kind, text: message, testid: opts.testid ?? '' }];
    if (kind !== 'error') {
      timers.set(id, setTimeout(() => dismiss(id), opts.timeout ?? AUTO_DISMISS_MS));
    }
    return id;
  }

  return {
    get items() {
      return items;
    },
    success: (text, opts) => show('success', text, opts),
    error: (text, opts) => show('error', text, opts),
    info: (text, opts) => show('info', text, opts),
    dismiss,
    /** Alle Meldungen entfernen - z.B. zu Beginn einer neuen Aktion. */
    clear() {
      for (const timer of timers.values()) clearTimeout(timer);
      timers.clear();
      items = [];
    },
  };
}

export const toasts = createToasts();
