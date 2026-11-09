// Inhalt der Tastenkürzel-Hilfe.

import type { ShortcutSection } from '../components/lcm-shortcut-help';
import { gotoPages, isMac } from '../lib/hotkeys';
import type { Can } from '../lib/navigation';
import type { PaletteItem } from '../lib/palette';
import { t } from '../lib/i18n';

/** Die Tasten der aktuellen Seite: ihre Befehle mit `key` und das Suchfeld. */
function pageSection(pageItems: PaletteItem[]): ShortcutSection {
  const keyed = pageItems.filter((item) => item.key);
  return {
    label: t('hotkeys.page'),
    rows: [
      ...keyed.map((item) => ({ keys: [(item.key ?? '').toUpperCase()], label: item.label })),
      { keys: ['/'], label: t('hotkeys.search') },
      { keys: ['←', '→'], label: t('hotkeys.arrows') },
    ],
  };
}

export function shortcutSections(can: Can, pageItems: PaletteItem[] = []): ShortcutSection[] {
  const modifier = isMac ? '⌘' : 'Ctrl';
  return [
    pageSection(pageItems),
    {
      label: t('hotkeys.goto'),
      rows: gotoPages(can).map((entry) => ({ keys: ['G', (entry.hotkey ?? '').toUpperCase()], label: t(entry.labelKey) })),
    },
    {
      label: t('hotkeys.general'),
      rows: [
        { keys: [modifier, 'K'], label: t('hotkeys.palette') },
        { keys: ['?'], label: t('hotkeys.help') },
        { keys: [modifier, 'S'], label: t('hotkeys.save') },
        { keys: ['Esc'], label: t('hotkeys.close') },
      ],
    },
  ];
}
