// Was die Containerseite zeigt, steht in der Adresse (`/containers/instances?all=1&page=2`): der Reiter im Pfad,
// Filter und Seite in der Query. Diese Datei übersetzt zwischen Adresse und Zustand und schneidet die Seiten zu
// (die Listen kommen vollständig vom Backend).

import { PAGE_SIZE, type ContainerImage, type ContainerItem } from '../../api/containers';
import { t } from '../../lib/i18n';
import type { QueryValue } from '../../lib/router';

export const CONTAINER_TABS = ['images', 'instances', 'compose'] as const;
export type ContainerTab = (typeof CONTAINER_TABS)[number];

/** Pfadteil des Reiters; die Images sind die Startseite. */
export function tabPath(tab: ContainerTab): string {
  return tab === 'images' ? '/containers' : `/containers/${tab}`;
}

/** Der Reiter aus dem Pfadrest (`instances`, `compose/…`); Unbekanntes führt zu den Images. */
export function parseTab(rest: string | undefined): ContainerTab {
  const first = (rest ?? '').split('/')[0];
  return CONTAINER_TABS.find((tab) => tab === first) ?? 'images';
}

export interface ListQuery {
  /** Images: nur die mit verfügbarem Update. */
  onlyUpdates: boolean;
  /** Container: auch angehaltene und beendete zeigen (sonst nur laufende). */
  showAll: boolean;
  page: number;
}

export function queryFromUrl(query: URLSearchParams): ListQuery {
  const page = Number.parseInt(query.get('page') ?? '1', 10);
  return { onlyUpdates: query.get('updates') === '1', showAll: query.get('all') === '1', page: Number.isFinite(page) && page > 1 ? page : 1 };
}

/** Query-Änderung für `router.setQuery`. Jede Änderung außer der Seite springt zurück auf Seite 1. */
export function queryPatch(change: Partial<ListQuery>): Record<string, QueryValue> {
  const patch: Record<string, QueryValue> = {};
  if ('onlyUpdates' in change) patch.updates = change.onlyUpdates ? 1 : null;
  if ('showAll' in change) patch.all = change.showAll ? 1 : null;
  if ('page' in change) patch.page = change.page && change.page > 1 ? change.page : null;
  else patch.page = null;
  return patch;
}

export function visibleImages(images: ContainerImage[], onlyUpdates: boolean): ContainerImage[] {
  return onlyUpdates ? images.filter((image) => image.status === 'update') : images;
}

export function visibleContainers(containers: ContainerItem[], showAll: boolean): ContainerItem[] {
  return showAll ? containers : containers.filter((container) => container.state === 'running');
}

export function updateCount(images: ContainerImage[]): number {
  return images.filter((image) => image.status === 'update').length;
}

/** Images mit kritischen oder hohen Sicherheitslücken. */
export function seriousCount(images: ContainerImage[]): number {
  return images.filter((image) => image.critical > 0 || image.high > 0).length;
}

export interface Slice<T> {
  items: T[];
  page: number;
  total: number;
  from: number;
  to: number;
}

/** Die Seite der Liste; eine Seite hinter dem Ende (Filter, Daten haben sich geändert) wird zur letzten. */
export function slicePage<T>(items: T[], page: number, size: number = PAGE_SIZE): Slice<T> {
  const last = Math.max(1, Math.ceil(items.length / size));
  const current = Math.min(Math.max(1, page), last);
  const start = (current - 1) * size;
  return { items: items.slice(start, start + size), page: current, total: items.length, from: items.length === 0 ? 0 : start + 1, to: Math.min(items.length, start + size) };
}

const KNOWN_STATES: Record<string, string> = {
  running: 'containers.state.running',
  exited: 'containers.state.exited',
  restarting: 'containers.state.restarting',
  paused: 'containers.state.paused',
  dead: 'containers.state.dead',
  created: 'containers.state.created',
};

/** Zustand in Worten; ein unbekannter Zustand bleibt, wie die Laufzeit ihn nennt. */
export function stateText(state: string): string {
  const key = KNOWN_STATES[state];
  return key ? t(key) : state || '–';
}
