import { describe, expect, it } from 'vitest';
import { emptyBlockForm, validateBlockForm } from './block-model';
import { emptyProfileForm, validateProfileForm } from './profile-model';
import { emptyUserForm, validateUserForm } from './user-model';

const user = (patch: object) => validateUserForm({ ...emptyUserForm([]), username: 'deploy', ...patch }, true);

describe('Linux-Benutzer: Grenzen wie im Backend', () => {
  it('begrenzt den Namen auf 64 Zeichen (nicht 128) und lehnt Zeilenumbrüche ab', () => {
    expect(user({ full_name: 'ä'.repeat(64) })).toEqual({});
    expect(user({ full_name: 'a'.repeat(65) }).full_name).toBe('linuxUsers.form.error.fullNameLong');
    expect(user({ full_name: 'a\nb' }).full_name).toBe('check.oneLine');
  });

  it('nimmt Benutzernamen wie das Backend: kein $ am Ende, höchstens 32 Zeichen', () => {
    expect(user({ username: 'web$' }).username).toBe('linuxUsers.form.error.usernameInvalid');
    expect(user({ username: 'a'.repeat(33) }).username).toBe('linuxUsers.form.error.usernameInvalid');
    expect(user({ username: '1abc' }).username).toBe('linuxUsers.form.error.usernameInvalid');
  });

  it('prüft E-Mail-Adresse und Shell', () => {
    expect(user({ email: 'a@b.de' })).toEqual({});
    expect(user({ email: 'Tony <a@b.de>' }).email).toBe('linuxUsers.form.error.emailInvalid');
    expect(user({ email: 'a@b' }).email).toBe('linuxUsers.form.error.emailInvalid');
    expect(user({ shell: '/bin/bash; id' }).shell).toBe('linuxUsers.form.error.shellInvalid');
    expect(user({ shell: `/${'a'.repeat(64)}` }).shell).toBe('linuxUsers.form.error.shellInvalid');
  });
});

describe('Profile: Grenzen', () => {
  const profile = (patch: object) => validateProfileForm({ ...emptyProfileForm(), name: 'Admin', ...patch }, false, []);

  it('begrenzt Name, Beschreibung, Befehle und Pfade', () => {
    expect(profile({}).name).toBeUndefined();
    expect(profile({ name: 'n'.repeat(65) }).name).toBe('check.tooLong');
    expect(profile({ description: 'd'.repeat(1001) }).description).toBe('check.tooLong');
    expect(profile({ sudo_rules: [{ command: `/${'x'.repeat(512)}`, run_as: 'root', require_password: false, allow_root_equivalent: false }] }).sudo[0]).toBe('check.tooLong');
    expect(profile({ edit_rules: [`/${'x'.repeat(4096)}`] }).edit[0]).toBe('check.tooLong');
    expect(profile({ path_rules: [{ path: '/a\nb', mode: 'read' }] }).path[0]).toBe('check.oneLine');
  });
});

describe('Bausteine: Grenzen', () => {
  const block = (patch: object) => validateBlockForm({ ...emptyBlockForm(), name: 'Dienste', variants: [{ family: 'all', run_as: '', sudo_commands: '/bin/ls', edit_paths: '', path_rules: '' }], ...patch }, false);

  it('begrenzt Namen, Beschreibungen und Parameter', () => {
    expect(block({}).name).toBeUndefined();
    expect(block({ name: 'n'.repeat(65) }).name).toBe('check.tooLong');
    expect(block({ name_en: 'n'.repeat(65) }).nameEn).toBe('check.tooLong');
    expect(block({ description: 'd'.repeat(1001) }).description).toBe('check.tooLong');
    expect(block({ description_en: 'd'.repeat(1001) }).descriptionEn).toBe('check.tooLong');
    expect(block({ params: 'p'.repeat(513) }).params).toBe('check.tooLong');
  });

  it('begrenzt die Texte einer Variante und lehnt Steuerzeichen ab', () => {
    const variant = (patch: object) => ({ family: 'all', run_as: '', sudo_commands: '/bin/ls', edit_paths: '', path_rules: '', ...patch });
    expect(block({ variants: [variant({ sudo_commands: 'x'.repeat(32769) })] }).rows[0]).toBe('check.tooLong');
    expect(block({ variants: [variant({ run_as: 'r'.repeat(65) })] }).rows[0]).toBe('check.tooLong');
    expect(block({ variants: [variant({ edit_paths: '/etc/a\u0000' })] }).rows[0]).toBe('check.controlChars');
  });
});
