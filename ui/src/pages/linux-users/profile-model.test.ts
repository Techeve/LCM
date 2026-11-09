import { describe, expect, it } from 'vitest';
import { normalizeBlock, normalizeProfile } from '../../api/linux-users';
import {
  ruleCell,
  availableBlocks,
  blockParams,
  createProfileBody,
  emptyProfileForm,
  emptySudoRule,
  filterProfiles,
  freeCopyName,
  freeSlug,
  isProfileValid,
  paramValue,
  profileBody,
  profileFormOf,
  sameProfileForm,
  setParamValue,
  slugFromName,
  validateProfileForm,
  validSlug,
} from './profile-model';

describe('slug', () => {
  it('derives a valid short name from the name', () => {
    expect(slugFromName('Webserver-Betrieb')).toBe('webserver-betrieb');
    expect(slugFromName('Größe & Maß')).toBe('groesse-mass');
    expect(slugFromName('1. Admin')).toBe('admin');
    expect(slugFromName('Ein sehr langer Profilname mit vielen Wörtern')).toHaveLength(slugFromName('Ein sehr langer Profilname mit vielen Wörtern').length);
    expect(slugFromName('Ein sehr langer Profilname mit vielen Wörtern').length).toBeLessThanOrEqual(20);
    expect(slugFromName('x'.repeat(19) + ' y')).not.toMatch(/-$/);
  });

  it('accepts what the backend accepts', () => {
    expect(validSlug('web-1')).toBe(true);
    for (const bad of ['', '1web', '-web', 'Web', 'we b', 'a'.repeat(21)]) expect(validSlug(bad)).toBe(false);
  });
});

describe('validation', () => {
  const block = normalizeBlock({ id: 5, params: 'service, path' });

  it('needs a name and a usable short name when creating', () => {
    const form = emptyProfileForm();
    expect(validateProfileForm(form, true, []).name).toBeDefined();
    expect(validateProfileForm({ ...form, name: 'Web' }, true, []).slug).toBeUndefined();
    expect(validateProfileForm({ ...form, name: '###' }, true, []).slug).toBeDefined();
    expect(validateProfileForm({ ...form, name: '###' }, false, []).slug).toBeUndefined();
  });

  it('flags relative commands and paths per row, ignoring blank rows', () => {
    const form = {
      ...emptyProfileForm(),
      name: 'x',
      sudo_rules: [{ ...emptySudoRule(), command: '' }, { ...emptySudoRule(), command: 'systemctl restart nginx' }],
      edit_rules: ['/etc/nginx/nginx.conf', 'etc/x'],
      path_rules: [{ path: 'srv', mode: 'read' as const }],
    };
    const errors = validateProfileForm(form, false, []);
    expect(Object.keys(errors.sudo)).toEqual(['1']);
    expect(Object.keys(errors.edit)).toEqual(['1']);
    expect(Object.keys(errors.path)).toEqual(['0']);
    expect(isProfileValid(errors)).toBe(false);
  });

  it('requires every block parameter to have a value without spaces', () => {
    const form = { ...emptyProfileForm(), name: 'x', block_uses: [{ block_id: 5, values: 'service=nginx' }] };
    expect(validateProfileForm(form, false, [block]).blocks[0]).toBeDefined();
    const filled = { ...form, block_uses: [{ block_id: 5, values: 'service=nginx\npath=/srv/www' }] };
    expect(isProfileValid(validateProfileForm(filled, false, [block]))).toBe(true);
    const spaced = { ...form, block_uses: [{ block_id: 5, values: 'service=a b\npath=/srv' }] };
    expect(validateProfileForm(spaced, false, [block]).blocks[0]).toBeDefined();
  });
});

describe('body', () => {
  it('drops blank rows and trims, keeping the filled ones', () => {
    const form = {
      ...emptyProfileForm(),
      name: ' Web ',
      sudo_rules: [{ ...emptySudoRule(), command: '  /usr/bin/systemctl restart nginx ' }, emptySudoRule()],
      edit_rules: ['', ' /etc/nginx/nginx.conf '],
      path_rules: [{ path: ' ', mode: 'deny' as const }, { path: '/srv', mode: 'readwrite' as const }],
    };
    const body = profileBody(form);
    expect(body.name).toBe('Web');
    expect(body.sudo_rules.map((r) => r.command)).toEqual(['/usr/bin/systemctl restart nginx']);
    expect(body.edit_rules).toEqual([{ path: '/etc/nginx/nginx.conf' }]);
    expect(body.path_rules).toEqual([{ path: '/srv', mode: 'readwrite' }]);
  });

  it('takes the slug from the name unless one is typed', () => {
    expect(createProfileBody({ ...emptyProfileForm(), name: 'Web Betrieb' }).slug).toBe('web-betrieb');
    expect(createProfileBody({ ...emptyProfileForm(), name: 'Web', slug: ' mein-slug ' }).slug).toBe('mein-slug');
  });

  it('notices edits and ignores identical forms', () => {
    const profile = normalizeProfile({ id: 1, name: 'A', sudo_rules: [{ command: '/bin/true' }] });
    const form = profileFormOf(profile);
    expect(sameProfileForm(form, profileFormOf(profile))).toBe(true);
    expect(sameProfileForm(form, { ...form, description: 'x' })).toBe(false);
  });
});

describe('block parameters', () => {
  it('reads and writes lines without touching the others', () => {
    const block = normalizeBlock({ id: 1, params: ' service ,path,' });
    expect(blockParams(block)).toEqual(['service', 'path']);
    const values = setParamValue(setParamValue('', 'service', 'nginx'), 'path', '/srv');
    expect(paramValue(values, 'service')).toBe('nginx');
    expect(paramValue(setParamValue(values, 'service', 'apache2'), 'path')).toBe('/srv');
    expect(paramValue(setParamValue(values, 'service', 'apache2'), 'service')).toBe('apache2');
    expect(paramValue(values, 'missing')).toBe('');
  });

  it('keeps equals signs inside values', () => {
    expect(paramValue('opt=a=b', 'opt')).toBe('a=b');
  });

  it('offers only blocks not yet attached', () => {
    const blocks = [normalizeBlock({ id: 1 }), normalizeBlock({ id: 2 })];
    expect(availableBlocks(blocks, [{ block_id: 1, values: '' }]).map((b) => b.id)).toEqual([2]);
  });
});

describe('list helpers', () => {
  it('searches name, slug and description', () => {
    const profiles = [normalizeProfile({ id: 1, name: 'Web', slug: 'web', description: 'nginx' }), normalizeProfile({ id: 2, name: 'DB', slug: 'db' })];
    expect(filterProfiles(profiles, ' NGINX ').map((p) => p.id)).toEqual([1]);
    expect(filterProfiles(profiles, '')).toHaveLength(2);
  });

  it('shows full root and empty profiles instead of a bare 0', () => {
    expect(ruleCell(normalizeProfile({ id: 1, grants_full_root: true }))).toBe('all');
    expect(ruleCell(normalizeProfile({ id: 2 }))).toBe('none');
    expect(ruleCell(normalizeProfile({ id: 3, sudo_rules: [{ command: '/usr/bin/true', run_as: 'root' }], edit_rules: [{ path: '/etc/hosts' }] }))).toBe(2);
  });

  it('counts up for taken copy names', () => {
    expect(freeCopyName('Web', '(Kopie)', ['Web'])).toBe('Web (Kopie)');
    expect(freeCopyName('Web', '(Kopie)', ['Web (Kopie)', 'web (kopie) 2'])).toBe('Web (Kopie) 3');
  });

  it('keeps copy slugs unique although the 20 character limit cuts the counter off', () => {
    const first = freeSlug('Standardbenutzer (Kopie)', ['standardbenutzer']);
    const second = freeSlug('Standardbenutzer (Kopie) 2', ['standardbenutzer', first]);
    expect(first).toBe('standardbenutzer-kop');
    expect(second).toBe('standardbenutzer-k-2');
    expect(second.length).toBeLessThanOrEqual(20);
    expect(freeSlug('Web', ['x'])).toBe('web');
  });
});
