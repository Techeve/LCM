// Beschriftungen und Einträge für das Hauptmenü, aus den Seiten und Übersetzungen gebaut.

import type { MainNavLabels, NavItem } from '../components/lcm-main-nav';
import type { MenuItem } from '../components/lcm-menu';
import type { CurrentUser } from '../lib/auth';
import { languageName, t } from '../lib/i18n';
import { isAvailable, pageFor, PAGES, type Can } from '../lib/navigation';

export function mainNavItems(can: Can, locked: boolean): NavItem[] {
  if (locked) return []; // im Pflichtschritt führt kein Menüpunkt irgendwohin
  return PAGES.filter((entry) => entry.menu && isAvailable(entry, can)).map((entry) => ({
    id: entry.id,
    label: t(entry.labelKey),
    href: entry.path,
  }));
}

/** Kennung des Menüpunkts, zu dem der Pfad gehört (leer, wenn keiner im Menü steht). */
export function currentNavId(path: string): string {
  const entry = pageFor(path);
  return entry?.menu ? entry.id : '';
}

export function navLabels(): MainNavLabels {
  return {
    nav: t('nav.main'),
    search: t('nav.search'),
    docs: t('nav.docs'),
    theme: t('nav.theme'),
    language: t('nav.language'),
    account: t('nav.accountMenu'),
    menu: t('nav.menu'),
  };
}

export function themeItems(): MenuItem[] {
  return [
    { id: 'auto', label: t('theme.auto'), icon: 'auto' },
    { id: 'light', label: t('theme.light'), icon: 'sun' },
    { id: 'dark', label: t('theme.dark'), icon: 'moon' },
  ];
}

export function languageItems(languages: string[]): MenuItem[] {
  return languages.map((language) => ({ id: language, label: languageName(language), hint: language }));
}

/** Name für „Angemeldet als …“: Anzeigename, sonst Benutzername. */
export function userName(user: Pick<CurrentUser, 'username' | 'display_name'> | null): string {
  return (user?.display_name || user?.username || '').trim();
}

/** Kontomenü: oben „Angemeldet als <Name>“ (führt zum Konto), unten „Über LCM“ mit der Version. */
export function accountItems(user: Pick<CurrentUser, 'username' | 'display_name'> | null = null, version = ''): MenuItem[] {
  const name = userName(user);
  return [
    { id: 'account', label: name ? t('account.signedInAs', { name }) : t('nav.account'), hint: name ? t('nav.account') : undefined, icon: 'user' },
    { id: 'shortcuts', label: t('nav.shortcuts'), icon: 'keyboard', hint: '?' },
    { id: 'about', label: t('about.menu'), icon: 'info', hint: version || undefined },
    { id: 'logout', label: t('nav.logout'), icon: 'logout', separatorBefore: true },
  ];
}

/** Ein bis zwei Buchstaben für das Konto-Symbol. */
export function initialsOf(user: Pick<CurrentUser, 'username' | 'display_name'> | null): string {
  const name = (user?.display_name || user?.username || '').trim();
  const parts = name.split(/\s+/).filter(Boolean);
  const letters = parts.length > 1 ? parts[0][0] + parts[parts.length - 1][0] : name.slice(0, 2);
  return letters.toUpperCase();
}
