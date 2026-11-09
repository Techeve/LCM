// Was die Ansicht ausmacht, steht in der Adresse: der Abschnitt im Pfad, Suche und offene Fenster
// in der Query. Diese Datei übersetzt zwischen Adresse und Zustand.

import type { QueryValue } from '../../lib/router';

export const SECTIONS = ['users', 'profiles', 'profile-blocks'] as const;
export type Section = (typeof SECTIONS)[number];

/** Unbekannte Abschnitte führen zu den Benutzern. */
export function parseSection(raw: string | undefined): Section {
  return SECTIONS.find((section) => section === raw) ?? 'users';
}

export function sectionPath(section: Section): string {
  return section === 'users' ? '/linux-users' : `/linux-users/${section}`;
}

export type Modal = { kind: 'none' } | { kind: 'new' } | { kind: 'edit'; id: number };

const NONE: Modal = { kind: 'none' };

function positiveInt(raw: string | null): number | null {
  return raw !== null && /^[1-9]\d*$/.test(raw) ? Number(raw) : null;
}

/** Die offene Fenster: `?modal=new` oder `?edit=<id>`. Eine ungültige ID öffnet nichts. */
export function parseModal(query: URLSearchParams): Modal {
  if (query.get('modal') === 'new') return { kind: 'new' };
  const id = positiveInt(query.get('edit'));
  return id === null ? NONE : { kind: 'edit', id };
}

/** Query-Änderung, die genau diese Fenster öffnet und die anderen Angaben löscht. */
export function modalQuery(modal: Modal): Record<string, QueryValue> {
  switch (modal.kind) {
    case 'new':
      return { modal: 'new', edit: null };
    case 'edit':
      return { modal: null, edit: modal.id };
    default:
      return { modal: null, edit: null };
  }
}
