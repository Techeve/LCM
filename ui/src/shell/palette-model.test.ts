import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { GROUPS_URL, MAX_HITS_PER_KIND, SERVERS_URL } from '../lib/entity-search';
import { i18n } from '../lib/i18n';
import type { PaletteItem } from '../lib/palette';
import { store } from '../lib/store';
import { buildSections, type PaletteSource, type ShellActions } from './palette-model';

const actions = (): ShellActions => ({
  go: vi.fn(),
  back: vi.fn(),
  setTheme: vi.fn(),
  setLanguage: vi.fn(),
  showShortcuts: vi.fn(),
  logout: vi.fn(),
});

function source(permissions: string[], extra: Partial<PaletteSource> = {}): PaletteSource {
  return { can: (permission) => permissions.includes(permission), actions: actions(), pageItems: [], languages: ['de', 'en'], language: 'de', ...extra };
}

function lists(servers: unknown[], groups: unknown[] = []): void {
  vi.spyOn(store, 'peek').mockImplementation(((url: string) => (url === SERVERS_URL ? { items: servers } : url === GROUPS_URL ? { items: groups } : undefined)) as typeof store.peek);
}

const ids = (sections: ReturnType<typeof buildSections>, section: string) => sections.find((s) => s.id === section)?.items.map((i) => i.id);

beforeAll(() => i18n.setLanguage('de'));
afterEach(() => vi.restoreAllMocks());

describe('without a query', () => {
  it('offers the menu pages the user may see and the general commands, but no settings or entities', () => {
    lists([{ id: 1, name: 'web01' }]);
    const sections = buildSections(source(['servers:read', 'jobs:read']), '');
    expect(sections.map((s) => s.id)).toEqual(['jump', 'general']);
    expect(ids(sections, 'jump')).toEqual(['page:dashboard', 'page:servers', 'page:jobs']);
    expect(ids(sections, 'general')).toEqual([
      'cmd:back',
      'cmd:theme:auto',
      'cmd:theme:light',
      'cmd:theme:dark',
      'cmd:language:en',
      'cmd:shortcuts',
      'cmd:logout',
    ]);
  });

  it('puts the commands of the current page first', () => {
    const pageItem: PaletteItem = { id: 'srv:upgrade', label: 'Pakete aktualisieren', run: vi.fn() };
    const sections = buildSections(source([], { pageItems: [pageItem] }), '');
    expect(sections[0]).toMatchObject({ id: 'page', items: [pageItem] });
  });
});

describe('with a query', () => {
  it('finds settings pages with their group and respects permissions', () => {
    const sections = buildSections(source(['alerts:manage', 'rules:manage']), 'alarm');
    expect(sections.map((s) => s.id)).toEqual(['settings']);
    expect(ids(sections, 'settings')).toEqual(['settings:alerts']);
    expect(buildSections(source([]), 'alarm')).toEqual([]);
  });

  it('finds servers by name or host and groups by name, and jumps to them', () => {
    lists(
      [
        { id: 1, name: 'web01', host: '10.10.0.11' },
        { id: 2, name: 'db01', host: '10.10.0.12' },
      ],
      [{ id: 7, name: 'Produktion' }],
    );
    const s = source(['servers:read', 'groups:read']);
    const byName = buildSections(s, 'web');
    expect(ids(byName, 'servers')).toEqual(['server:1']);
    expect(ids(buildSections(s, '0.12'), 'servers')).toEqual(['server:2']);

    const groups = buildSections(s, 'prod');
    expect(ids(groups, 'groups')).toEqual(['group:7']);
    groups.find((section) => section.id === 'groups')!.items[0].run();
    expect(s.actions.go).toHaveBeenCalledWith('/groups/7');
    byName.find((section) => section.id === 'servers')!.items[0].run();
    expect(s.actions.go).toHaveBeenCalledWith('/servers/1');
  });

  it('limits the entity hits', () => {
    lists(Array.from({ length: 30 }, (_, index) => ({ id: index, name: `web${index}` })));
    const hits = buildSections(source([]), 'web').find((section) => section.id === 'servers')!.items;
    expect(hits).toHaveLength(MAX_HITS_PER_KIND);
  });

  it('survives lists that are not loaded or have an unexpected shape', () => {
    lists({ items: [] } as unknown as unknown[]);
    expect(() => buildSections(source([]), 'web')).not.toThrow();
  });

  it('filters the commands of the page by the query', () => {
    const items: PaletteItem[] = [
      { id: 'a', label: 'Pakete aktualisieren', run: vi.fn() },
      { id: 'b', label: 'Terminal öffnen', run: vi.fn() },
    ];
    expect(ids(buildSections(source([], { pageItems: items }), 'termi'), 'page')).toEqual(['b']);
  });

  it('runs general commands through the actions', () => {
    const s = source([]);
    const general = buildSections(s, 'dunkel').find((section) => section.id === 'general')!;
    expect(general.items.map((item) => item.id)).toEqual(['cmd:theme:dark']);
    general.items[0].run();
    expect(s.actions.setTheme).toHaveBeenCalledWith('dark');

    buildSections(s, 'English').find((section) => section.id === 'general')!.items[0].run();
    expect(s.actions.setLanguage).toHaveBeenCalledWith('en');
  });
});
