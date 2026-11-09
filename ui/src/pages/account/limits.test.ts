import { describe, expect, it } from 'vitest';
import { profileErrors } from './account-model';

describe('Konto: Profil', () => {
  const draft = { email: 'a@b.de', firstName: 'Tony', lastName: 'Grätscher' };

  it('lässt eine leere E-Mail-Adresse zu, prüft aber die Form', () => {
    expect(profileErrors(draft)).toEqual({});
    expect(profileErrors({ ...draft, email: '' })).toEqual({});
    expect(profileErrors({ ...draft, email: 'kaputt' }).email).toBe('check.email');
    expect(profileErrors({ ...draft, email: 'Tony <a@b.de>' }).email).toBe('check.email');
  });

  it('begrenzt Vor- und Nachname auf 64 Zeichen', () => {
    expect(profileErrors({ ...draft, firstName: '😀'.repeat(64) })).toEqual({});
    expect(profileErrors({ ...draft, firstName: 'a'.repeat(65) }).firstName).toBe('check.tooLong');
    expect(profileErrors({ ...draft, lastName: 'a\nb' }).lastName).toBe('check.oneLine');
  });
});
