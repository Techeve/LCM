// Benutzer: Formulare, Prüfung, Körper der Anfragen und Zuordnung von Serverfehlern zu Feldern.
// Das Backend prüft alles noch einmal (auch die Passwortregeln); hier geht es um frühes Feedback.

import type { LoginUser, NewUserBody, ProfileBody, Role } from '../../../api/settings';
import { LIMITS, charCount, hasControlChars, isEmail } from '../../../lib/validate';
import { checkPassword, type PasswordIdentity } from '../../account/password-policy';

export interface UserForm {
  username: string;
  email: string;
  firstName: string;
  lastName: string;
  password: string;
  confirm: string;
  /** Aktuelles Passwort; nur nötig, wenn jemand seine eigene E-Mail-Adresse ändert. */
  current: string;
  roles: string[];
}

export function emptyUserForm(defaultRoles: string[] = []): UserForm {
  return { username: '', email: '', firstName: '', lastName: '', password: '', confirm: '', current: '', roles: defaultRoles };
}

export function userFormOf(user: LoginUser): UserForm {
  return { username: user.username, email: user.email, firstName: user.firstName, lastName: user.lastName, password: '', confirm: '', current: '', roles: [...user.roles] };
}

export interface UserFormErrors {
  username?: string;
  email?: string;
  firstName?: string;
  lastName?: string;
  password?: string;
  confirm?: string;
  current?: string;
}

const USERNAME = /^[a-z0-9_-]{3,32}$/;

/**
 * Fehler sind Übersetzungsschlüssel. `create` verlangt Benutzername und Passwort, `edit` nur eine gültige
 * E-Mail-Adresse und, wenn jemand die eigene Adresse ändert (`needsCurrent`), das aktuelle Passwort.
 */
export function validateUserForm(form: UserForm, mode: 'create' | 'edit', needsCurrent = false): UserFormErrors {
  const errors: UserFormErrors = {};
  if (mode === 'create') {
    if (!USERNAME.test(form.username.trim())) errors.username = 'settings.users.form.usernameInvalid';
    errors.password = passwordError(form.password, identityOfForm(form));
    if (!errors.password && form.confirm !== form.password) errors.confirm = 'settings.users.form.confirmMismatch';
  }
  const email = form.email.trim();
  if (email !== '' && !isEmail(email)) errors.email = 'settings.users.form.emailInvalid';
  for (const key of ['firstName', 'lastName'] as const) {
    if (charCount(form[key].trim()) > LIMITS.personName) errors[key] = 'check.tooLong';
    else if (hasControlChars(form[key])) errors[key] = 'check.oneLine';
  }
  if (needsCurrent && form.current === '') errors.current = 'settings.users.form.currentRequired';
  return Object.fromEntries(Object.entries(errors).filter(([, value]) => value)) as UserFormErrors;
}

/** Angaben, die nicht im Passwort des Kontos stehen dürfen. */
export function identityOfForm(form: Pick<UserForm, 'username' | 'email' | 'firstName' | 'lastName'>): PasswordIdentity {
  return { username: form.username, email: form.email, firstName: form.firstName, lastName: form.lastName };
}

/** Die einzelnen Gründe nennt die Liste unter dem Feld; hier nur, dass es so nicht geht. */
function passwordError(password: string, identity: PasswordIdentity): string | undefined {
  if (password === '') return 'settings.users.form.passwordRequired';
  if (charCount(password) > LIMITS.newPassword) return 'check.tooLong';
  if (!checkPassword(password, identity).ok) return 'settings.users.form.passwordRules';
  return undefined;
}

/** Prüfung eines neuen Passworts samt Wiederholung (Zurücksetzen). */
export function validateNewPassword(password: string, confirm: string, identity: PasswordIdentity = {}): { password?: string; confirm?: string } {
  const error = passwordError(password, identity);
  if (error) return { password: error };
  return confirm === password ? {} : { confirm: 'settings.users.form.confirmMismatch' };
}

export function newUserBody(form: UserForm): NewUserBody {
  return {
    username: form.username.trim(),
    email: form.email.trim(),
    first_name: form.firstName.trim(),
    last_name: form.lastName.trim(),
    password: form.password,
    roles: form.roles,
  };
}

export function profileBody(form: UserForm): ProfileBody {
  const body: ProfileBody = { email: form.email.trim(), first_name: form.firstName.trim(), last_name: form.lastName.trim() };
  if (form.current) body.current_password = form.current;
  return body;
}

/** Hat sich am Profil (E-Mail, Vor- und Nachname) etwas geändert? */
export function profileChanged(user: LoginUser, form: UserForm): boolean {
  return form.email.trim() !== user.email || form.firstName.trim() !== user.firstName || form.lastName.trim() !== user.lastName;
}

/** Haben sich die Rollen gegenüber dem Benutzer geändert (Reihenfolge egal)? */
export function rolesChanged(user: LoginUser, roles: string[]): boolean {
  return [...user.roles].sort().join('\n') !== [...roles].sort().join('\n');
}

export function toggleRole(roles: string[], name: string): string[] {
  return roles.includes(name) ? roles.filter((role) => role !== name) : [...roles, name];
}

const FIELD_OF_CODE: Record<string, keyof UserFormErrors> = {
  'user.invalid_username': 'username',
  'user.username_taken': 'username',
  'user.reserved_username': 'username',
  'user.email_taken': 'email',
  'password.too_weak': 'password',
  'auth.password_required_for_email': 'current',
};

/** Zu welchem Feld gehört ein Fehler des Servers? Prüffehler (`validation.*`) nennen das Feld in `params.field`. */
export function userFieldForError(code: string, params: Record<string, unknown>): keyof UserFormErrors | undefined {
  const known = FIELD_OF_CODE[code];
  if (known) return known;
  if (code.startsWith('validation.') && params.field === 'email') return 'email';
  if (code.startsWith('validation.') && params.field === 'username') return 'username';
  return undefined;
}

/** Der Link, über den ein Benutzer sein Passwort selbst setzt (derselbe Aufbau wie in den Mails des Servers). */
export function activationUrl(origin: string, token: string): string {
  return `${origin}/#/aktivierung?token=${encodeURIComponent(token)}`;
}

/** Name zur Anzeige: Vor- und Nachname, sonst leer. */
export function fullName(user: Pick<LoginUser, 'firstName' | 'lastName'>): string {
  return [user.firstName, user.lastName].filter(Boolean).join(' ');
}

/** Rollen, die sich vergeben lassen; die interne Rolle `system` gehört keinem Menschen. */
export function assignableRoles(roles: Role[]): Role[] {
  return roles.filter((role) => role.name !== 'system');
}

export function sameUserForm(a: UserForm, b: UserForm): boolean {
  const { roles: rolesA, ...restA } = a;
  const { roles: rolesB, ...restB } = b;
  return JSON.stringify(restA) === JSON.stringify(restB) && [...rolesA].sort().join('\n') === [...rolesB].sort().join('\n');
}

/** Die eigene E-Mail-Adresse zu ändern verlangt das aktuelle Passwort (so prüft es auch der Server). */
export function needsCurrentPassword(user: LoginUser, form: UserForm, selfId: number | undefined): boolean {
  return user.id === selfId && form.email.trim().toLowerCase() !== user.email.trim().toLowerCase();
}

export interface UserActions {
  edit: boolean;
  roles: boolean;
  invite: boolean;
  password: boolean;
  block: boolean;
  remove: boolean;
}

/**
 * Was darf der angemeldete Benutzer mit diesem Konto tun? System-Konten bleiben unberührt. Sich selbst sperrt,
 * löscht oder setzt man nicht zurück (das eigene Passwort ändert man unter Konto); `admin` lässt sich nicht löschen.
 */
export function userActions(user: LoginUser, selfId: number | undefined, can: (permission: string) => boolean): UserActions {
  const self = user.id === selfId;
  const write = can('users:write') && !user.isSystem;
  return {
    edit: !user.isSystem && (can('users:write') || self),
    roles: !user.isSystem && can('roles:write') && can('roles:read'),
    invite: write && !self,
    password: write && !self,
    block: write && !self,
    remove: write && !self && user.username !== 'admin',
  };
}
