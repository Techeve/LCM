import { describe, expect, it } from 'vitest';
import de from '../../locales/de.json';
import en from '../../locales/en.json';
import { describeCron, weekdaysOf } from './cron-describe';
import { RULE_TYPES } from './rule-model';
import { aclStatus, filterGroups, statusLevel } from './group-model';
import { normalizeGroup } from '../../api/groups';
import { validateGroupForm, validateScheduleForm } from './forms';
import { validateRuleForm, emptyRuleForm } from './rule-model';

function lookup(tree: unknown, key: string): unknown {
  return key.split('.').reduce<unknown>((node, part) => (node && typeof node === 'object' ? (node as Record<string, unknown>)[part] : undefined), tree);
}

describe('texts', () => {
  it('has a label and help for every rule type in both languages', () => {
    for (const tree of [de, en]) {
      for (const type of RULE_TYPES) {
        expect(lookup(tree, `${type.key}.label`), type.key).toBeTypeOf('string');
        expect(lookup(tree, `${type.key}.help`), type.key).toBeTypeOf('string');
      }
    }
  });

  it('has texts for every cron description and validation message', () => {
    const keys = ['0 3 * * *', '*/15 * * * *', '*/1 * * * *', '5 */2 * * *', '5 * * * *', '0 2 * * 1-5', '0 3 * * 0,3', '0 4 1 * *']
      .map((expression) => describeCron(expression)!.key)
      .concat([
        ...Object.values(validateGroupForm({ name: '', description: 'x'.repeat(1001), priority: 'a' })),
        ...Object.values(validateScheduleForm({ name: '', cron_expr: 'x', spread_minutes: 'a' })),
        ...Object.values(validateRuleForm({ ...emptyRuleForm(null), name: '' })).filter((v): v is string => typeof v === 'string'),
      ]);
    for (const key of keys) {
      const value = lookup(de, key) ?? lookup(de, `${key}.other`);
      expect(value, key).toBeDefined();
    }
  });
});

describe('cron description', () => {
  it('recognises common patterns and leaves the rest alone', () => {
    expect(describeCron('0 3 * * *')).toEqual({ key: 'cron.describe.daily', params: { time: '03:00' } });
    expect(describeCron('0 2 * * 1-5')?.key).toBe('cron.describe.workdays');
    expect(describeCron('0 4 1 * *')).toEqual({ key: 'cron.describe.monthly', params: { day: 1, time: '04:00' } });
    expect(describeCron('0 3 1 6 *')).toBeNull();
    expect(describeCron('nonsense')).toBeNull();
    expect(weekdaysOf('7,1-2')).toEqual([0, 1, 2]);
  });
});

describe('group model', () => {
  it('derives the ACL status', () => {
    expect(aclStatus({ acl_servers: 0, acl_capable: 0 })).toBeNull();
    expect(aclStatus({ acl_servers: 2, acl_capable: 2 })?.level).toBe('ok');
    expect(aclStatus({ acl_servers: 2, acl_capable: 1 })?.level).toBe('warn');
    expect(aclStatus({ acl_servers: 2, acl_capable: 0 })?.level).toBe('unknown');
  });

  it('maps server statuses and filters by every word', () => {
    expect(statusLevel('yellow')).toBe('warn');
    expect(statusLevel('whatever')).toBe('unknown');
    const groups = [normalizeGroup({ id: 1, name: 'Produktion', description: 'Demo' }), normalizeGroup({ id: 2, name: 'Staging' })];
    expect(filterGroups(groups, 'prod demo').map((g) => g.id)).toEqual([1]);
    expect(filterGroups(groups, '').length).toBe(2);
  });
});
