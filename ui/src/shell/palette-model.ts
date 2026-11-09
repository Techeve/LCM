// Was die Befehlspalette zeigt: Befehle der aktuellen Seite, Sprungziele, Server und
// Gruppen, allgemeine Befehle. Ohne Eingabe stehen nur Seitenbefehle, Seiten und
// allgemeine Befehle da; Einstellungen, Server und Gruppen erscheinen beim Tippen.

import { findEntities, type EntityHit } from '../lib/entity-search';
import { isAvailable, PAGES, visibleSettings, type Can } from '../lib/navigation';
import { filterItems, type PaletteItem } from '../lib/palette';
import { languageName, t } from '../lib/i18n';
import type { Theme } from '../lib/theme';

export interface PaletteSection {
  id: string;
  label: string;
  items: PaletteItem[];
}

export interface ShellActions {
  go(path: string): void;
  back(): void;
  setTheme(theme: Theme): void;
  setLanguage(language: string): void;
  showShortcuts(): void;
  logout(): void;
}

export interface PaletteSource {
  can: Can;
  actions: ShellActions;
  /** Befehle, die die aktuelle Seite angemeldet hat. */
  pageItems: PaletteItem[];
  languages: string[];
  language: string;
}

const THEMES: Array<{ theme: Theme; labelKey: string; icon: 'auto' | 'sun' | 'moon' }> = [
  { theme: 'auto', labelKey: 'theme.auto', icon: 'auto' },
  { theme: 'light', labelKey: 'theme.light', icon: 'sun' },
  { theme: 'dark', labelKey: 'theme.dark', icon: 'moon' },
];

function pageJumps({ can, actions }: PaletteSource): PaletteItem[] {
  return PAGES.filter((entry) => entry.menu && isAvailable(entry, can)).map((entry) => ({
    id: `page:${entry.id}`,
    label: t(entry.labelKey),
    icon: entry.icon,
    run: () => actions.go(entry.path),
  }));
}

function settingsJumps({ can, actions }: PaletteSource): PaletteItem[] {
  return visibleSettings(can).flatMap((group) =>
    group.sections.map((section) => ({
      id: `settings:${section.id}`,
      label: t(section.labelKey),
      hint: t(group.labelKey),
      icon: 'settings' as const,
      run: () => actions.go(`/settings/${section.id}`),
    })),
  );
}

function generalCommands({ actions, languages, language }: PaletteSource): PaletteItem[] {
  return [
    { id: 'cmd:back', label: t('palette.back'), icon: 'chevron-left', run: actions.back },
    ...THEMES.map((entry) => ({
      id: `cmd:theme:${entry.theme}`,
      label: t('palette.theme', { theme: t(entry.labelKey) }),
      icon: entry.icon,
      run: () => actions.setTheme(entry.theme),
    })),
    ...languages
      .filter((candidate) => candidate !== language)
      .map((candidate) => ({
        id: `cmd:language:${candidate}`,
        label: t('palette.language', { language: languageName(candidate) }),
        icon: 'globe' as const,
        run: () => actions.setLanguage(candidate),
      })),
    { id: 'cmd:shortcuts', label: t('nav.shortcuts'), hint: '?', icon: 'keyboard', run: actions.showShortcuts },
    { id: 'cmd:logout', label: t('nav.logout'), icon: 'logout', run: actions.logout },
  ];
}

function entityItems(hits: EntityHit[], kind: 'server' | 'group', actions: ShellActions): PaletteItem[] {
  const base = kind === 'server' ? '/servers' : '/groups';
  return hits.map((hit) => ({
    id: `${kind}:${hit.id}`,
    label: hit.label,
    hint: hit.hint,
    icon: kind === 'server' ? 'server' : 'users',
    run: () => actions.go(`${base}/${hit.id}`),
  }));
}

/** Ein Seitenbefehl mit eigener Taste zeigt sie rechts an. */
function withKeyHint(item: PaletteItem): PaletteItem {
  return item.key && !item.hint ? { ...item, hint: item.key.toUpperCase() } : item;
}

/** Die Abschnitte der Palette zur Eingabe, ohne leere. */
export function buildSections(source: PaletteSource, query: string): PaletteSection[] {
  const typing = query.trim() !== '';
  const { servers, groups } = findEntities(query);
  const sections: PaletteSection[] = [
    { id: 'page', label: t('palette.group.page'), items: filterItems(source.pageItems.map(withKeyHint), query) },
    { id: 'jump', label: t('palette.group.pages'), items: filterItems(pageJumps(source), query) },
    { id: 'settings', label: t('nav.settings'), items: typing ? filterItems(settingsJumps(source), query) : [] },
    { id: 'servers', label: t('palette.group.servers'), items: entityItems(servers, 'server', source.actions) },
    { id: 'groups', label: t('palette.group.groups'), items: entityItems(groups, 'group', source.actions) },
    { id: 'general', label: t('palette.group.general'), items: filterItems(generalCommands(source), query) },
  ];
  return sections.filter((section) => section.items.length > 0);
}
