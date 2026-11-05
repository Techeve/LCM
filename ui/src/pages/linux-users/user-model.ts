// Formular, Prüfungen und Ableitungen für Linux-Benutzer. Hier steht nur Logik ohne Oberfläche.

import type { Group } from '../../api/groups';
import { DEFAULT_SHELL, type CreateUserBody, type LinuxUser, type Profile, type UserBody, type UserServer } from '../../api/linux-users';
import { LIMITS, charCount, hasControlChars, isEmail, isLinuxUser } from '../../lib/validate';

export interface UserForm {
  username: string;
  full_name: string;
  email: string;
  shell: string;
  active: boolean;
  /** ID des Standardprofils als Text; leer, solange es keines gibt. */
  profile_id: string;
}

export type UserField = 'username' | 'email' | 'shell' | 'full_name';
export type UserErrors = Partial<Record<UserField, string>>;

/** Neuer Benutzer: das mitgelieferte Standardprofil (ohne Root-Rechte) ist vorgewählt. */
export function emptyUserForm(profiles: Profile[]): UserForm {
  const standard = profiles.find((p) => p.builtin && !p.grants_full_root);
  return { username: '', full_name: '', email: '', shell: DEFAULT_SHELL, active: true, profile_id: standard ? String(standard.id) : '' };
}

export function userFormOf(user: LinuxUser): UserForm {
  return {
    username: user.username,
    full_name: user.full_name,
    email: user.email,
    shell: user.shell,
    active: user.active,
    profile_id: user.default_profile_id === null ? '' : String(user.default_profile_id),
  };
}

// Wie das Backend (`ValidLinuxShell`, aber der Controller deckelt die Zeile bei `LIMITS.name`): absoluter Pfad aus harmlosen Zeichen.
const SHELL = /^\/[A-Za-z0-9/._-]{1,}$/;

/** Fehlerschlüssel je Feld (`linuxUsers.form.error.*`); ein leeres Ergebnis heißt: gültig. Der Name wird wie im Backend kleingeschrieben. */
export function validateUserForm(form: UserForm, creating: boolean): UserErrors {
  const errors: UserErrors = {};
  if (creating) {
    const username = form.username.trim().toLowerCase();
    if (!username) errors.username = 'linuxUsers.form.error.usernameRequired';
    else if (!isLinuxUser(username)) errors.username = 'linuxUsers.form.error.usernameInvalid';
  }
  if (charCount(form.full_name.trim()) > LIMITS.personName) errors.full_name = 'linuxUsers.form.error.fullNameLong';
  else if (hasControlChars(form.full_name)) errors.full_name = 'check.oneLine';
  if (form.email.trim() && !isEmail(form.email.trim())) errors.email = 'linuxUsers.form.error.emailInvalid';
  const shell = form.shell.trim();
  if (!SHELL.test(shell) || shell.length < 2 || charCount(shell) > LIMITS.name) errors.shell = 'linuxUsers.form.error.shellInvalid';
  return errors;
}

export const isValid = (errors: object): boolean => Object.keys(errors).length === 0;

export function sameUserForm(a: UserForm, b: UserForm): boolean {
  return (Object.keys(a) as Array<keyof UserForm>).every((key) => a[key] === b[key]);
}

function profileId(form: UserForm): number | null {
  return form.profile_id ? Number(form.profile_id) : null;
}

export function userBody(form: UserForm): UserBody {
  return {
    full_name: form.full_name.trim(),
    email: form.email.trim(),
    shell: form.shell.trim(),
    active: form.active,
    default_profile_id: profileId(form),
  };
}

export function createBody(form: UserForm): CreateUserBody {
  const { active: _active, ...rest } = userBody(form);
  return { ...rest, username: form.username.trim().toLowerCase() };
}

/**
 * Ändert das Speichern etwas, das LCM auf den Servern nachzieht? Name (GECOS), Shell, Aktiv-Status und Profil
 * landen im Konto; die E-Mail-Adresse dient nur dem Aktivierungslink.
 */
export function changesServers(initial: UserForm, form: UserForm): boolean {
  return (['full_name', 'shell', 'active', 'profile_id'] as const).some((key) => initial[key] !== form[key]);
}

/** Alle Server, auf denen der Benutzer eingerichtet ist: direkt zugeordnete und die der zugewiesenen Gruppen. */
export function userServers(user: LinuxUser, groups: Group[]): UserServer[] {
  const byId = new Map<number, UserServer>(user.servers.map((s) => [s.id, s]));
  for (const ref of user.groups) {
    for (const server of groups.find((g) => g.id === ref.id)?.servers ?? []) {
      if (!byId.has(server.id)) byId.set(server.id, { id: server.id, name: server.name, host: server.host, user_sync_disabled: false });
    }
  }
  return [...byId.values()].sort((a, b) => a.name.localeCompare(b.name));
}

/** Server einer Gruppe in der Form, die die Auswirkungen brauchen. */
export function groupServers(group: Group | undefined): UserServer[] {
  return (group?.servers ?? []).map((s) => ({ id: s.id, name: s.name, host: s.host, user_sync_disabled: false }));
}

export interface AclShortfall {
  capable: number;
  total: number;
}

/**
 * Verzeichnisrechte brauchen POSIX-ACLs auf dem Zielsystem. Hat ein Profil Pfadregeln, aber nicht jeder Server der
 * Gruppe kann ACLs, wirken sie dort nicht: das gehört vor der Zuweisung auf den Bildschirm.
 */
export function aclShortfall(profile: Profile | undefined, group: Group | undefined): AclShortfall | null {
  if (!profile || !group || profile.path_rules.length === 0) return null;
  if (group.acl_servers === 0 || group.acl_capable === group.acl_servers) return null;
  return { capable: group.acl_capable, total: group.acl_servers };
}

export type UserStatus = 'inactive' | 'password' | 'keys';

/** Inaktiv vor Passwort vor „nur Schlüssel“. */
export function userStatus(user: LinuxUser): UserStatus {
  if (!user.active) return 'inactive';
  return user.has_password ? 'password' : 'keys';
}

/** Der Benutzer steht noch irgendwo: dann muss er erst von den Servern entfernt werden, bevor er sich löschen lässt. */
export function isProvisioned(user: LinuxUser): boolean {
  return user.servers.length > 0 || user.groups.length > 0;
}

/** Suche über Benutzername, Name, E-Mail und Profilname. */
export function filterUsers(users: LinuxUser[], query: string, profileName: (id: number | null) => string): LinuxUser[] {
  const needle = query.trim().toLowerCase();
  if (!needle) return users;
  return users.filter((u) => [u.username, u.full_name, u.email, profileName(u.default_profile_id)].join(' ').toLowerCase().includes(needle));
}

/** Aktivierungslinks sind 48 Stunden gültig. */
export const ACTIVATION_TTL_HOURS = 48;

export function activationUrl(origin: string, token: string): string {
  return `${origin}/linux-activate?token=${encodeURIComponent(token)}`;
}
