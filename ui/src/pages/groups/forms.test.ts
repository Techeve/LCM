import { describe, expect, it } from 'vitest';
import { emptyGroupForm, emptyScheduleForm, groupBody, scheduleBody, validateGroupForm, validateScheduleForm } from './forms';

describe('group form', () => {
  it('starts valid except for the missing name', () => {
    expect(validateGroupForm(emptyGroupForm())).toEqual({ name: 'groups.form.nameRequired' });
  });

  it('checks the priority range and builds the body', () => {
    expect(validateGroupForm({ name: 'a', description: '', priority: '0' }).priority).toBeDefined();
    expect(validateGroupForm({ name: 'a', description: '', priority: '10000' }).priority).toBeDefined();
    expect(validateGroupForm({ name: 'a', description: '', priority: '9999' })).toEqual({});
    expect(groupBody({ name: ' a ', description: ' d ', priority: '5' })).toEqual({ name: 'a', description: 'd', priority: 5 });
  });
});

describe('group form limits', () => {
  it('counts characters, not UTF-16 units, and allows line breaks only in the description', () => {
    expect(validateGroupForm({ name: '😀'.repeat(64), description: '', priority: '1' }).name).toBeUndefined();
    expect(validateGroupForm({ name: 'a'.repeat(65), description: '', priority: '1' }).name).toBe('groups.form.nameTooLong');
    expect(validateGroupForm({ name: 'a\nb', description: '', priority: '1' }).name).toBe('check.oneLine');
    expect(validateGroupForm({ name: 'a', description: 'x\ny', priority: '1' }).description).toBeUndefined();
    expect(validateGroupForm({ name: 'a', description: 'x'.repeat(1001), priority: '1' }).description).toBe('groups.form.descriptionTooLong');
    expect(validateGroupForm({ name: 'a', description: 'x\u0000', priority: '1' }).description).toBe('check.controlChars');
  });
});

describe('schedule form', () => {
  it('validates cron and spread', () => {
    expect(validateScheduleForm({ ...emptyScheduleForm(), name: 'n' })).toEqual({});
    expect(validateScheduleForm({ name: 'n', cron_expr: '61 * * * *', spread_minutes: '0' }).cron).toBeDefined();
    expect(validateScheduleForm({ name: 'n', cron_expr: '0 3 * * *', spread_minutes: '1440' }).spread).toBeDefined();
    expect(validateScheduleForm({ name: 'n', cron_expr: '0 3 * * *', spread_minutes: '-1' }).spread).toBeDefined();
  });

  it('normalises whitespace in the expression', () => {
    expect(scheduleBody({ name: 'n', cron_expr: ' 0  3 * * * ', spread_minutes: '15' })).toEqual({ name: 'n', cron_expr: '0 3 * * *', spread_minutes: 15 });
  });
});
