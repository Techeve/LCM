import { describe, expect, it } from 'vitest';
import { normalizeGroup } from '../../api/groups';
import { normalizeProfile, normalizeUser } from '../../api/linux-users';
import {
  aclShortfall,
  activationUrl,
  changesServers,
  createBody,
  emptyUserForm,
  filterUsers,
  isProvisioned,
  isValid,
  userBody,
  userFormOf,
  userServers,
  userStatus,
  validateUserForm,
} from './user-model';

const standard = normalizeProfile({ id: 2, name: 'Standardbenutzer', slug: 'standard', builtin: true });
const admin = normalizeProfile({ id: 1, name: 'Voll-Administrator', slug: 'full-admin', builtin: true, grants_full_root: true });

describe('form', () => {
  it('preselects the built-in profile without root rights', () => {
    expect(emptyUserForm([admin, standard]).profile_id).toBe('2');
    expect(emptyUserForm([]).profile_id).toBe('');
  });

  it('validates the user name like the backend', () => {
    const form = { ...emptyUserForm([standard]), username: 'Deploy' };
    expect(isValid(validateUserForm(form, true))).toBe(true); // wird kleingeschrieben
    expect(validateUserForm({ ...form, username: '' }, true).username).toBe('linuxUsers.form.error.usernameRequired');
    for (const bad of ['1abc', '-x', 'a b', 'ä', 'x'.repeat(33)]) {
      expect(validateUserForm({ ...form, username: bad }, true).username).toBe('linuxUsers.form.error.usernameInvalid');
    }
    expect(validateUserForm({ ...form, username: '' }, false).username).toBeUndefined();
  });

  it('checks email and shell', () => {
    const form = emptyUserForm([standard]);
    expect(validateUserForm({ ...form, username: 'a', email: 'nope' }, true).email).toBeDefined();
    expect(validateUserForm({ ...form, username: 'a', email: '' }, true).email).toBeUndefined();
    expect(validateUserForm({ ...form, username: 'a', shell: 'bash' }, true).shell).toBeDefined();
    expect(validateUserForm({ ...form, username: 'a', shell: '/bin/zsh' }, true).shell).toBeUndefined();
  });

  it('builds trimmed bodies; creating carries the lowercase name and no active flag', () => {
    const form = { ...emptyUserForm([standard]), username: ' Deploy ', full_name: ' Bot ', email: ' a@b.de ' };
    expect(createBody(form)).toEqual({ username: 'deploy', full_name: 'Bot', email: 'a@b.de', shell: '/bin/bash', default_profile_id: 2 });
    expect(userBody({ ...form, profile_id: '' }).default_profile_id).toBeNull();
  });

  it('knows which edits reach the servers', () => {
    const user = normalizeUser({ id: 1, username: 'a', full_name: 'A', email: 'a@b.de', shell: '/bin/bash', default_profile_id: 2 });
    const initial = userFormOf(user);
    expect(changesServers(initial, { ...initial, email: 'other@b.de' })).toBe(false);
    for (const change of [{ shell: '/bin/sh' }, { active: false }, { profile_id: '1' }, { full_name: 'B' }]) {
      expect(changesServers(initial, { ...initial, ...change })).toBe(true);
    }
  });
});

describe('servers of a user', () => {
  const groups = [normalizeGroup({ id: 5, name: 'Prod', servers: [{ id: 2, name: 'web02' }, { id: 1, name: 'web01' }] })];

  it('joins direct servers and group servers without duplicates, sorted by name', () => {
    const user = normalizeUser({ id: 1, servers: [{ id: 1, name: 'web01', user_sync_disabled: true }], groups: [{ id: 5, name: 'Prod' }] });
    const servers = userServers(user, groups);
    expect(servers.map((s) => s.name)).toEqual(['web01', 'web02']);
    expect(servers[0]?.user_sync_disabled).toBe(true); // die direkte Angabe gewinnt
  });

  it('knows when a user must be removed from the servers first', () => {
    expect(isProvisioned(normalizeUser({ id: 1 }))).toBe(false);
    expect(isProvisioned(normalizeUser({ id: 1, groups: [{ id: 5 }] }))).toBe(true);
    expect(isProvisioned(normalizeUser({ id: 1, servers: [{ id: 1 }] }))).toBe(true);
  });
});

describe('acl shortfall', () => {
  const withPaths = normalizeProfile({ id: 9, path_rules: [{ path: '/srv', mode: 'read' }] });
  it('warns only for profiles with path rules and groups that cannot all do ACLs', () => {
    expect(aclShortfall(withPaths, normalizeGroup({ id: 1, acl_servers: 3, acl_capable: 1 }))).toEqual({ capable: 1, total: 3 });
    expect(aclShortfall(withPaths, normalizeGroup({ id: 1, acl_servers: 3, acl_capable: 3 }))).toBeNull();
    expect(aclShortfall(withPaths, normalizeGroup({ id: 1, acl_servers: 0, acl_capable: 0 }))).toBeNull();
    expect(aclShortfall(standard, normalizeGroup({ id: 1, acl_servers: 3, acl_capable: 0 }))).toBeNull();
  });
});

describe('list', () => {
  const users = [
    normalizeUser({ id: 1, username: 'anna', full_name: 'Anna Admin', default_profile_id: 2 }),
    normalizeUser({ id: 2, username: 'deploy', email: 'bot@example.org', default_profile_id: 1, active: false }),
  ];
  const profileName = (id: number | null) => (id === 1 ? 'Voll-Administrator' : id === 2 ? 'Standardbenutzer' : '');

  it('searches name, email and profile', () => {
    expect(filterUsers(users, '', profileName)).toHaveLength(2);
    expect(filterUsers(users, 'ANNA', profileName).map((u) => u.id)).toEqual([1]);
    expect(filterUsers(users, 'example.org', profileName).map((u) => u.id)).toEqual([2]);
    expect(filterUsers(users, 'voll-admin', profileName).map((u) => u.id)).toEqual([2]);
  });

  it('ranks inactive before password before keys', () => {
    expect(userStatus(users[1]!)).toBe('inactive');
    expect(userStatus(normalizeUser({ id: 3, has_password: true }))).toBe('password');
    expect(userStatus(normalizeUser({ id: 3 }))).toBe('keys');
  });

  it('builds the activation link with an encoded token', () => {
    expect(activationUrl('https://lcm.example', 'a+b/c')).toBe('https://lcm.example/linux-activate?token=a%2Bb%2Fc');
  });
});
