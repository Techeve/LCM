import { describe, expect, it } from 'vitest';
import { normalizeGroup, normalizeGroups, normalizeUserOptions, userLabel } from './groups';

describe('normalizeGroups', () => {
  const raw = { id: 2, name: 'Produktion', priority: 50, acl_servers: 2, acl_capable: 1, servers: [{ id: 1, name: 'web01', host: '10.0.0.1' }] };

  it('accepts a plain list and the items envelope alike', () => {
    expect(normalizeGroups([raw])).toEqual(normalizeGroups({ items: [raw], total: 1 }));
  });

  it('derives counts from the members and leaves unknown ones null', () => {
    const group = normalizeGroup(raw);
    expect(group.server_count).toBe(1);
    expect(group.has_members).toBe(true);
    expect(group.manager_count).toBeNull();
    expect(group.schedule_count).toBeNull();
  });

  it('prefers counts the server names itself', () => {
    const group = normalizeGroup({ id: 1, name: 'a', server_count: 9, manager_count: 3, schedule_count: 2 });
    expect([group.server_count, group.manager_count, group.schedule_count, group.has_members]).toEqual([9, 3, 2, false]);
  });

  it('fills defaults for missing fields', () => {
    expect(normalizeGroup({})).toMatchObject({ priority: 100, servers: [], managers: [], rules: [], is_system: false });
  });
});

describe('users', () => {
  it('skips system and inactive users and labels by name', () => {
    const users = normalizeUserOptions([
      { id: 1, username: 'system', is_system: true },
      { id: 2, username: 'ann', first_name: 'Ann', last_name: 'Lee', active: true },
      { id: 3, username: 'old', active: false },
    ]);
    expect(users.map((u) => u.id)).toEqual([2]);
    expect(userLabel(users[0]!)).toBe('ann (Ann Lee)');
  });
});
