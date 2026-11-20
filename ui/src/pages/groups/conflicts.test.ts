import { describe, expect, it } from 'vitest';
import { normalizeGroup, normalizeRule, type Group, type Rule } from '../../api/groups';
import { findConflicts } from './conflicts';

const group = (id: number, priority: number, servers: number[], extra: object = {}): Group =>
  normalizeGroup({ id, name: `g${id}`, priority, servers: servers.map((s) => ({ id: s, name: `s${s}` })), ...extra });
const rule = (id: number, groupId: number, type = 'firewall', extra: object = {}): Rule =>
  normalizeRule({ id, group_id: groupId, enforce: true, enabled: true, type, ...extra });
const scheduled = (id: number, groupId: number, type = 'firewall', extra: object = {}): Rule =>
  rule(id, groupId, type, { enforce: false, schedule_id: id + 100, schedule_enabled: true, ...extra });

describe('findConflicts', () => {
  it('lets the smaller priority win on shared servers', () => {
    const groups = [group(1, 100, [1, 2]), group(2, 50, [2, 3])];
    const rules = [rule(10, 1), rule(20, 2)];
    const mine = findConflicts(1, groups, rules).get(10)!;
    expect(mine.overriddenOn).toEqual([2]);
    expect(mine.overriddenBy[0]).toMatchObject({ groupId: 2, priority: 50 });
    expect(mine.tie).toBe(false);
    const other = findConflicts(2, groups, rules).get(20)!;
    expect(other.winsOn).toEqual([2]);
    expect(other.winsOver[0]?.groupId).toBe(1);
  });

  it('flags a tie and lets the smaller group id win', () => {
    const groups = [group(1, 100, [1]), group(2, 100, [1])];
    const rules = [rule(10, 1), rule(20, 2)];
    expect(findConflicts(1, groups, rules).get(10)).toMatchObject({ winsOn: [1], tie: true });
    expect(findConflicts(2, groups, rules).get(20)).toMatchObject({ overriddenOn: [1], tie: true });
  });

  it('ignores disabled rules, other types and groups without shared servers', () => {
    const groups = [group(1, 100, [1]), group(2, 10, [1]), group(3, 10, [9])];
    expect(findConflicts(1, groups, [rule(10, 1), rule(20, 2, 'firewall', { enabled: false })]).size).toBe(0);
    expect(findConflicts(1, groups, [rule(10, 1, 'acl-setup'), rule(20, 2, 'acl-setup')]).size).toBe(0);
    expect(findConflicts(1, groups, [rule(10, 1), rule(30, 3)]).size).toBe(0);
  });

  it('applies rules of the system group to every server', () => {
    const groups = [group(1, 100, [1]), group(9, 1000, [], { is_system: true })];
    const conflicts = findConflicts(1, groups, [rule(10, 1), rule(90, 9)]);
    expect(conflicts.get(10)).toMatchObject({ winsOn: [1] });
  });

  it('lets a scheduled rule of the stronger group set aside rules of other groups', () => {
    const groups = [group(1, 100, [1]), group(2, 10, [1])];
    const rules = [rule(10, 1), scheduled(11, 1), scheduled(20, 2)];
    const mine = findConflicts(1, groups, rules);
    expect(mine.get(10)).toMatchObject({ overriddenOn: [1], overriddenBy: [{ groupId: 2 }] });
    expect(mine.get(11)).toMatchObject({ overriddenOn: [1], overriddenBy: [{ groupId: 2 }] });
    expect(findConflicts(2, groups, rules).get(20)).toMatchObject({ winsOn: [1], winsOver: [{ groupId: 1 }] });
  });

  it('does not set rules of the same group against each other unless both are baseline rules', () => {
    const groups = [group(1, 100, [1])];
    expect(findConflicts(1, groups, [rule(10, 1), scheduled(11, 1)]).size).toBe(0);
    expect(findConflicts(1, groups, [scheduled(10, 1), scheduled(11, 1)]).size).toBe(0);
    expect(findConflicts(1, groups, [rule(10, 1), rule(11, 1)]).get(11)).toMatchObject({ overriddenOn: [1], overriddenBy: [{ groupId: 1 }] });
  });

  it('ignores rules of a disabled schedule', () => {
    const groups = [group(1, 100, [1]), group(2, 10, [1])];
    expect(findConflicts(1, groups, [scheduled(10, 1), scheduled(20, 2, 'firewall', { schedule_enabled: false })]).size).toBe(0);
  });
});
