import { describe, expect, it } from 'vitest';
import { readUsers } from '../../../api/settings';
import {
  activationUrl,
  assignableRoles,
  emptyUserForm,
  fullName,
  needsCurrentPassword,
  newUserBody,
  profileBody,
  profileChanged,
  rolesChanged,
  sameUserForm,
  toggleRole,
  userActions,
  userFieldForError,
  userFormOf,
  validateNewPassword,
  validateUserForm,
} from './users-model';

const [user] = readUsers([{ id: 7, username: 'lena', email: 'l@x.de', first_name: 'Lena', last_name: 'Berg', roles: [{ name: 'manager' }, { name: 'admin' }] }]);

describe('Benutzerformular', () => {
  const valid = { ...emptyUserForm(['manager']), username: 'lena', password: 'ein-langes-Passwort-1', confirm: 'ein-langes-Passwort-1' };

  it('akzeptiert ein vollständiges Formular', () => {
    expect(validateUserForm(valid, 'create')).toEqual({});
  });

  it('prüft Benutzername, Passwort, Wiederholung und E-Mail', () => {
    expect(validateUserForm({ ...valid, username: 'Lena Berg' }, 'create').username).toBe('settings.users.form.usernameInvalid');
    expect(validateUserForm({ ...valid, password: '', confirm: '' }, 'create').password).toBe('settings.users.form.passwordRequired');
    expect(validateUserForm({ ...valid, password: 'kurz', confirm: 'kurz' }, 'create').password).toBe('settings.users.form.passwordRules');
    expect(validateUserForm({ ...valid, confirm: 'anders' }, 'create').confirm).toBe('settings.users.form.confirmMismatch');
    expect(validateUserForm({ ...valid, email: 'kein-at' }, 'create').email).toBe('settings.users.form.emailInvalid');
  });

  it('verlangt beim Bearbeiten weder Namen noch Passwort', () => {
    expect(validateUserForm(userFormOf(user!), 'edit')).toEqual({});
  });

  it('prüft ein neues Passwort mit Wiederholung', () => {
    expect(validateNewPassword('ein-langes-Passwort-1', 'ein-langes-Passwort-1')).toEqual({});
    expect(validateNewPassword('kurz', 'kurz')).toEqual({ password: 'settings.users.form.passwordRules' });
    expect(validateNewPassword('ein-langes-Passwort-1', 'x')).toEqual({ confirm: 'settings.users.form.confirmMismatch' });
  });

  it('baut den Körper getrimmt', () => {
    expect(newUserBody({ ...valid, username: ' lena ', email: ' l@x.de ' })).toMatchObject({ username: 'lena', email: 'l@x.de', roles: ['manager'] });
  });
});

describe('Rollen', () => {
  it('erkennt Änderungen unabhängig von der Reihenfolge', () => {
    expect(rolesChanged(user!, ['admin', 'manager'])).toBe(false);
    expect(rolesChanged(user!, ['admin'])).toBe(true);
  });

  it('schaltet Rollen um', () => {
    expect(toggleRole(['a'], 'b')).toEqual(['a', 'b']);
    expect(toggleRole(['a', 'b'], 'a')).toEqual(['b']);
  });
});

describe('Serverfehler', () => {
  it('ordnet bekannte Codes Feldern zu', () => {
    expect(userFieldForError('user.username_taken', {})).toBe('username');
    expect(userFieldForError('user.email_taken', {})).toBe('email');
    expect(userFieldForError('password.too_weak', {})).toBe('password');
    expect(userFieldForError('validation.invalid_email', { field: 'email' })).toBe('email');
    expect(userFieldForError('user.last_admin', {})).toBeUndefined();
  });
});

describe('Anzeige', () => {
  it('baut den Aktivierungslink wie der Server', () => {
    expect(activationUrl('https://lcm.example.com', 'a b')).toBe('https://lcm.example.com/#/aktivierung?token=a%20b');
  });

  it('nennt den vollen Namen', () => {
    expect(fullName(user!)).toBe('Lena Berg');
    expect(fullName({ firstName: '', lastName: '' })).toBe('');
  });
});

describe('Bearbeiten', () => {
  it('verlangt das aktuelle Passwort nur für die eigene, geänderte Adresse', () => {
    const form = userFormOf(user!);
    expect(needsCurrentPassword(user!, { ...form, email: 'neu@x.de' }, 7)).toBe(true);
    expect(needsCurrentPassword(user!, { ...form, email: ' L@X.de ' }, 7)).toBe(false);
    expect(needsCurrentPassword(user!, { ...form, email: 'neu@x.de' }, 8)).toBe(false);
    expect(validateUserForm({ ...form, email: 'neu@x.de' }, 'edit', true).current).toBe('settings.users.form.currentRequired');
    expect(profileBody({ ...form, current: 'geheim' }).current_password).toBe('geheim');
    expect(profileBody(form)).not.toHaveProperty('current_password');
  });

  it('erkennt Änderungen an Profil und Formular', () => {
    const form = userFormOf(user!);
    expect(profileChanged(user!, form)).toBe(false);
    expect(profileChanged(user!, { ...form, lastName: 'Stein' })).toBe(true);
    expect(sameUserForm(form, { ...form, roles: ['admin', 'manager'] })).toBe(true);
    expect(sameUserForm(form, { ...form, firstName: 'Lea' })).toBe(false);
  });

  it('bietet die Rolle system nicht an', () => {
    expect(assignableRoles([{ id: 1, name: 'admin', description: '' }, { id: 3, name: 'system', description: '' }]).map((role) => role.name)).toEqual(['admin']);
  });
});

describe('Erlaubte Aktionen', () => {
  const all = () => true;
  const none = () => false;
  const admin = { ...user!, username: 'admin' };
  const system = { ...user!, isSystem: true };

  it('erlaubt einem Admin alles an fremden Konten außer dem Löschen von admin', () => {
    expect(userActions(user!, 1, all)).toEqual({ edit: true, roles: true, invite: true, password: true, block: true, remove: true });
    expect(userActions(admin, 1, all).remove).toBe(false);
  });

  it('lässt das eigene Konto nur bearbeiten', () => {
    expect(userActions(user!, 7, all)).toEqual({ edit: true, roles: true, invite: false, password: false, block: false, remove: false });
    expect(userActions(user!, 7, none)).toMatchObject({ edit: true, roles: false });
  });

  it('fasst System-Konten nicht an', () => {
    expect(Object.values(userActions(system, 1, all)).some(Boolean)).toBe(false);
  });
});
