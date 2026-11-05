// Interne Pfade und das Ziel `next` nach der Anmeldung. `next` kommt aus der
// Adresszeile und ist damit fremde Eingabe: nur ein Pfad auf dieser Seite darf
// durch (kein Open Redirect auf eine andere Domain).

const PLACEHOLDER_ORIGIN = 'http://internal.invalid';

/** Beginnt mit genau einem `/`, ohne Backslash, Steuerzeichen oder Schema. */
export function isInternalPath(value: unknown): value is string {
  if (typeof value !== 'string' || !value.startsWith('/')) return false;
  if (value.startsWith('//') || value.includes('\\')) return false;
  // Browser entfernen Tabulator und Zeilenumbruch aus Adressen: `/\t/host` würde `//host`
  if ([...value].some((char) => char.charCodeAt(0) < 0x20 || char.charCodeAt(0) === 0x7f)) return false;
  return new URL(value, PLACEHOLDER_ORIGIN).origin === PLACEHOLDER_ORIGIN;
}

function isLoginPath(path: string): boolean {
  return path === '/login' || path.startsWith('/login/') || path.startsWith('/login?');
}

/** Das `next` aus der Adresszeile, wenn es ein interner Pfad ist (und nicht die Anmeldung selbst). */
export function safeNext(value: string | null | undefined): string | null {
  return isInternalPath(value) && !isLoginPath(value) ? value : null;
}

/** Warum die Anmeldeseite erscheint, wenn es mehr als „Sitzung abgelaufen“ zu sagen gibt. */
export type LoginReason = 'session-mode';

/** Adresse der Anmeldeseite, die danach zu `next` zurückführt. */
export function loginHref(next: string, reason?: LoginReason): string {
  const base = safeNext(next) && next !== '/' ? `/login?next=${encodeURIComponent(next)}` : '/login';
  return reason ? `${base}${base.includes('?') ? '&' : '?'}reason=${reason}` : base;
}
