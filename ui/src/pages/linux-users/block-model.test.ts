import { describe, expect, it } from 'vitest';
import { normalizeBlock } from '../../api/linux-users';
import {
  blockBody,
  blockFormOf,
  createBlockBody,
  emptyBlockForm,
  emptyVariant,
  filterBlocks,
  isBlockValid,
  parseSource,
  pathRuleLinesValid,
  previewValues,
  validateBlockForm,
} from './block-model';

const withRules = () => ({ ...emptyBlockForm(), name: 'Nginx', variants: [{ ...emptyVariant(), sudo_commands: '/usr/bin/systemctl restart {service}' }] });

describe('validation', () => {
  it('needs a name and at least one variant with a rule', () => {
    expect(validateBlockForm(emptyBlockForm(), true).name).toBeDefined();
    expect(validateBlockForm({ ...emptyBlockForm(), name: 'X' }, true).variants).toBeDefined();
    expect(isBlockValid(validateBlockForm(withRules(), true))).toBe(true);
  });

  it('checks the parameter names', () => {
    expect(validateBlockForm({ ...withRules(), params: 'service, path_2' }, true).params).toBeUndefined();
    expect(validateBlockForm({ ...withRules(), params: 'Service' }, true).params).toBeDefined();
    expect(validateBlockForm({ ...withRules(), params: 'a b' }, true).params).toBeDefined();
  });

  it('checks the directory rule lines', () => {
    expect(pathRuleLinesValid('readwrite /etc/nginx\n\n# Kommentar\nread /var/log')).toBe(true);
    for (const bad of ['write /etc', '/etc/nginx', 'read /a /b']) expect(pathRuleLinesValid(bad)).toBe(false);
    const form = { ...withRules(), variants: [{ ...emptyVariant(), sudo_commands: '/bin/true', path_rules: 'oops' }] };
    expect(validateBlockForm(form, true).rows[0]).toBeDefined();
  });

  it('derives the slug from the name when creating', () => {
    expect(createBlockBody(withRules()).slug).toBe('nginx');
    expect(validateBlockForm({ ...withRules(), name: '###' }, true).slug).toBeDefined();
    expect(validateBlockForm({ ...withRules(), name: '###' }, false).slug).toBeUndefined();
  });
});

describe('body', () => {
  it('drops empty variants and normalises the parameter list', () => {
    const form = { ...withRules(), params: ' service , ,path ', variants: [...withRules().variants, emptyVariant()] };
    const body = blockBody(form);
    expect(body.variants).toHaveLength(1);
    expect(body.params).toBe('service,path');
  });

  it('builds example values for the preview', () => {
    expect(previewValues('service,path')).toBe('service=beispiel\npath=beispiel');
    expect(previewValues('')).toBe('');
  });

  it('round-trips a block into a form', () => {
    const block = normalizeBlock({ id: 1, name: 'A', variants: [{ family: 'apt', sudo_commands: '/bin/true' }] });
    expect(blockFormOf(block).variants[0]?.family).toBe('apt');
  });
});

describe('filter', () => {
  const blocks = [
    normalizeBlock({ id: 1, name: 'Apache betreiben', name_en: 'Operate Apache', slug: 'apache', builtin: true }),
    normalizeBlock({ id: 2, name: 'Eigener Dienst', slug: 'eigen', description: 'nginx' }),
  ];

  it('searches both languages, the slug and the description', () => {
    expect(filterBlocks(blocks, 'operate', 'all').map((b) => b.id)).toEqual([1]);
    expect(filterBlocks(blocks, 'NGINX', 'all').map((b) => b.id)).toEqual([2]);
    expect(filterBlocks(blocks, '', 'all')).toHaveLength(2);
  });

  it('filters by origin', () => {
    expect(filterBlocks(blocks, '', 'builtin').map((b) => b.id)).toEqual([1]);
    expect(filterBlocks(blocks, '', 'own').map((b) => b.id)).toEqual([2]);
    expect(parseSource('own')).toBe('own');
    expect(parseSource('x')).toBe('all');
  });
});
