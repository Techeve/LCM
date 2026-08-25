/**
 * Seitenweise Anzeige für Tabellen, die im Betrieb lang werden können.
 *
 * Der Anlass ist nicht Kosmetik: Eine Paketliste hat auf einem gewöhnlichen
 * Server vierstellig viele Zeilen. Sie alle zu rendern kostet spürbar Zeit im
 * Browser, und gefunden hat man damit trotzdem nichts - gescrollt wird nicht
 * bis Zeile 1200.
 *
 * Die Funktionen sind bewusst reine Rechnungen ohne Zustand: Jede Tabelle
 * hält ihre eigene Seitennummer als $state und schneidet sich ihre Zeilen
 * heraus. Die Darstellung darunter kommt einheitlich aus Pagination.svelte.
 */

/** Standard-Seitengröße für lange Tabellen. */
export const PAGE_SIZE = 50;

/**
 * Größere Seite für Katalog-Tabellen (Regelbausteine, Anwendungen).
 *
 * Der Unterschied ist kein Geschmack, sondern die Nutzungsart: Eine
 * Paketliste durchsucht man gezielt, einen Katalog überfliegt man. Bei 50
 * begänne das Blättern mitten im Sichten.
 */
export const PAGE_SIZE_CATALOG = 100;

/** Anzahl der Seiten (mindestens 1, damit die Anzeige nie „0 von 0" sagt). */
export function pageCount(total, size = PAGE_SIZE) {
  return Math.max(1, Math.ceil((total || 0) / size));
}

/**
 * Die Zeilen einer Seite. Eine Seitennummer außerhalb des Bereichs wird
 * eingefangen - das passiert regelmäßig, wenn ein Filter die Liste kürzt,
 * während man auf Seite 7 steht.
 */
export function pageSlice(rows, page, size = PAGE_SIZE) {
  const all = rows ?? [];
  const current = Math.min(Math.max(1, page), pageCount(all.length, size));
  const from = (current - 1) * size;
  return all.slice(from, from + size);
}
