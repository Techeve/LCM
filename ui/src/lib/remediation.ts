// Pflichtschritte nach der Anmeldung: neues Passwort setzen oder 2FA einrichten.
// Das Backend sperrt bis dahin alles außer Konto, Abmelden und der Einrichtung
// (`internal/api/middlewares/remediation.go`). Die Oberfläche führt deshalb auf
// die Kontoseite und lässt keine andere Seite zu.

export type Remediation = 'password' | 'two-factor';

export const REMEDIATION_PATH = '/account/security';
const REMEDIATION_HREF = `${REMEDIATION_PATH}?required=1`;

interface RemediationState {
  must_change_password: boolean;
  must_setup_2fa: boolean;
}

/** Was der Benutzer nachholen muss. Das Passwort kommt zuerst, wie im Backend. */
export function remediationOf(user: RemediationState | null): Remediation | null {
  if (!user) return null;
  if (user.must_change_password) return 'password';
  return user.must_setup_2fa ? 'two-factor' : null;
}

function isRemediationPath(path: string): boolean {
  return path === REMEDIATION_PATH || path.startsWith(`${REMEDIATION_PATH}/`);
}

/**
 * Wohin eine Adresse im Pflichtzustand umgeleitet wird; `null`, wenn sie bleiben darf. Das ursprüngliche Ziel
 * reist als `next` mit, damit die Kontoseite nach dem Pflichtschritt dorthin weiterführt.
 */
export function remediationRedirect(user: RemediationState | null, path: string): string | null {
  if (!remediationOf(user) || isRemediationPath(path)) return null;
  return path === '/' ? REMEDIATION_HREF : `${REMEDIATION_HREF}&next=${encodeURIComponent(path)}`;
}
