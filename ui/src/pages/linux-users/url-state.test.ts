import { describe, expect, it } from 'vitest';
import type { QueryValue } from '../../lib/router';
import { modalQuery, parseModal, parseSection, sectionPath } from './url-state';

describe('sections', () => {
  it('falls back to the users for unknown sections', () => {
    expect(parseSection(undefined)).toBe('users');
    expect(parseSection('nope')).toBe('users');
    expect(parseSection('profile-blocks')).toBe('profile-blocks');
  });

  it('keeps the plain path for the users', () => {
    expect(sectionPath('users')).toBe('/linux-users');
    expect(sectionPath('profiles')).toBe('/linux-users/profiles');
  });
});

describe('modal', () => {
  it('reads new and edit from the query', () => {
    expect(parseModal(new URLSearchParams('modal=new'))).toEqual({ kind: 'new' });
    expect(parseModal(new URLSearchParams('edit=7'))).toEqual({ kind: 'edit', id: 7 });
  });

  it('opens nothing for invalid ids', () => {
    for (const edit of ['0', '-1', 'x', '1.5', '']) expect(parseModal(new URLSearchParams({ edit }))).toEqual({ kind: 'none' });
  });

  it('round-trips through the query patch', () => {
    const apply = (patch: Record<string, QueryValue>) =>
      new URLSearchParams(Object.entries(patch).flatMap(([k, v]) => (v === null || v === undefined ? [] : [[k, String(v)] as [string, string]])));
    expect(parseModal(apply(modalQuery({ kind: 'edit', id: 4 })))).toEqual({ kind: 'edit', id: 4 });
    expect(parseModal(apply(modalQuery({ kind: 'new' })))).toEqual({ kind: 'new' });
    expect(parseModal(apply(modalQuery({ kind: 'none' })))).toEqual({ kind: 'none' });
  });
});
