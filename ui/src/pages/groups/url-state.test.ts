import { describe, expect, it } from 'vitest';
import { modalQuery, groupPath, parseModal, parseGroupId, parseTab } from './url-state';

const q = (text: string) => new URLSearchParams(text);

describe('url state', () => {
  it('falls back to the overview for unknown tabs', () => {
    expect(parseTab('rules')).toBe('rules');
    expect(parseTab('nonsense')).toBe('overview');
    expect(parseTab(undefined)).toBe('overview');
  });

  it('accepts only positive whole numbers as group id', () => {
    expect(parseGroupId('12')).toBe(12);
    for (const bad of ['0', '-1', '1x', '', undefined]) expect(parseGroupId(bad)).toBeNull();
  });

  it('builds paths', () => {
    expect(groupPath(3)).toBe('/groups/3');
    expect(groupPath(3, 'rules')).toBe('/groups/3/rules');
  });

  it('parses the modals and ignores incomplete ones', () => {
    expect(parseModal(q('modal=new-group'), false)).toEqual({ kind: 'new-group' });
    expect(parseModal(q('modal=edit-group'), false)).toEqual({ kind: 'none' });
    expect(parseModal(q('modal=edit-rule&rule=5'), true)).toEqual({ kind: 'edit-rule', id: 5 });
    expect(parseModal(q('modal=edit-rule'), true)).toEqual({ kind: 'none' });
    expect(parseModal(q('modal=new-rule&trigger=enforce'), true)).toEqual({ kind: 'new-rule', trigger: 'enforce' });
    expect(parseModal(q('modal=new-rule&trigger=7'), true)).toEqual({ kind: 'new-rule', trigger: 7 });
    expect(parseModal(q('modal=new-rule'), true)).toEqual({ kind: 'new-rule', trigger: null });
  });

  it('round-trips a modal through the query', () => {
    const query = modalQuery({ kind: 'edit-schedule', id: 4 });
    expect(query).toMatchObject({ modal: 'edit-schedule', schedule: 4, rule: null });
    expect(modalQuery({ kind: 'none' })).toEqual({ modal: null, schedule: null, rule: null, trigger: null });
  });
});
