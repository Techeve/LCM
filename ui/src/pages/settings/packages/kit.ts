// Gemeinsame Bausteine der Einstellungsabschnitte „Pakete & Quellen“, „Schutz“, „Automatisierung“ und
// „Meldungen“: Stile, Tabellentexte, offene Fenster in der Adresse, Fehler am richtigen Feld und
// der Stand einer Überwachungsregel.

import { css, type ReactiveControllerHost } from 'lit';
import { columnStyles, formLayoutStyles } from '../../../components/layout-styles';
import type { TableLabels } from '../../../components/lcm-data-table';
import type { ActionLabels } from '../../../components/lcm-server-action';
import { utilStyles } from '../../../components/shared';
import { ApiError } from '../../../lib/api';
import { errorText, t } from '../../../lib/i18n';
import { router, type QueryValue } from '../../../lib/router';

/** Seitenraster (Inhalt, optional Erklärspalte, siehe `columnStyles`) und wiederkehrende Kleinteile. */
export const sectionStyles = [
  columnStyles,
  formLayoutStyles,
  utilStyles,
  css`
  :host {
    display: block;
    min-inline-size: 0;
  }
  .main {
    display: flex;
    flex-direction: column;
    gap: var(--gap);
    min-inline-size: 0;
    max-inline-size: var(--content-w, 48rem);
  }
  .main.wide {
    max-inline-size: var(--content-wide-w, 68rem);
  }
  /* Protokolle (Ereignisse): lange Zeilen brauchen den ganzen Platz. */
  .main.full {
    max-inline-size: none;
  }
  .lead {
    margin: 0;
    max-inline-size: 70ch;
    color: var(--text-2);
  }
  .small {
    font-size: var(--fs-s);
  }
  .mono {
    font-family: var(--font-mono);
    font-size: var(--fs-s);
    overflow-wrap: anywhere;
  }
  .tools {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
  }
  .callout {
    margin: 0;
    padding: var(--space-3) var(--space-4);
    border-radius: var(--radius-m);
    background: var(--info-soft);
    color: var(--text-1);
    font-size: var(--fs-s);
  }
  .callout.warn {
    background: var(--warn-soft);
    color: var(--warn-text);
  }
  .callout a,
  a.link {
    color: var(--accent-text);
    font-weight: var(--fw-semibold);
  }
  .pair {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: var(--space-4);
  }
  .pair.wide-first {
    grid-template-columns: minmax(0, 2fr) minmax(0, 1fr);
  }
  .group-title {
    margin: var(--space-2) 0 0;
    color: var(--text-2);
    font-size: var(--fs-xs);
    font-weight: var(--fw-semibold);
    letter-spacing: 0.04em;
    text-transform: uppercase;
  }
  dl.facts {
    display: grid;
    grid-template-columns: max-content minmax(0, 1fr);
    gap: var(--space-1) var(--space-4);
    margin: 0;
    font-size: var(--fs-s);
  }
  dl.facts dt {
    color: var(--text-2);
  }
  dl.facts dd {
    margin: 0;
  }
  @media (max-width: 40rem) {
    .pair,
    .pair.wide-first {
      grid-template-columns: minmax(0, 1fr);
    }
  }
  `,
];

/** Tabellentexte für schlichte Tabellen ohne Werkzeugleiste. */
export function tableLabels(caption: string, empty: string): TableLabels {
  return {
    caption,
    filter: '',
    all: '',
    columns: '',
    densityComfortable: '',
    densityCompact: '',
    selectAll: '',
    selectRow: '',
    selected: '',
    clear: '',
    empty,
    sortedAsc: t('settings.more.table.sortedAsc'),
    sortedDesc: t('settings.more.table.sortedDesc'),
  };
}

/** Texte der Erklärblase an Knöpfen, die auf Servern etwas auslösen. */
export function actionLabels(): ActionLabels {
  return {
    meaning: t('settings.more.action.meaning'),
    executes: t('settings.more.action.executes'),
    commands: t('settings.more.action.steps'),
    packages: t('settings.more.action.servers'),
    reboot: t('settings.more.action.reboot'),
    reversible: t('settings.more.impact.reversible'),
    duration: '',
    yes: t('settings.more.impact.yes'),
    no: t('settings.more.impact.no'),
    docs: '',
    loading: '',
  };
}

/** Texte der Erklärspalte zum Ein- und Ausklappen. */
export function explainerToggle() {
  return { collapse: t('settings.more.explainer.collapse'), expand: t('settings.more.explainer.expand') };
}

// ---- Offene Fenster in der Adresse: `?modal=new` oder `?edit=<id>` -------------------------------

export type Modal = { kind: 'none' } | { kind: 'new' } | { kind: 'edit'; id: number };

export function parseModal(query: URLSearchParams): Modal {
  if (query.get('modal') === 'new') return { kind: 'new' };
  const raw = query.get('edit');
  return raw !== null && /^[1-9]\d*$/.test(raw) ? { kind: 'edit', id: Number(raw) } : { kind: 'none' };
}

export function modalQuery(modal: Modal): Record<string, QueryValue> {
  if (modal.kind === 'new') return { modal: 'new', edit: null };
  if (modal.kind === 'edit') return { modal: null, edit: modal.id };
  return { modal: null, edit: null };
}

/**
 * Formular in der Fenster, an die Adresse gebunden. `make` liefert zur offenen Fenster die
 * Ausgangswerte; `undefined` heißt „Daten noch nicht da“ (später erneut), `null` „nichts zu öffnen“.
 * `sync()` gehört in `willUpdate` der Seite.
 */
export class ModalForm<F extends object> {
  form: F | null = null;
  /** Ein Speicherversuch lief: ab jetzt zeigen die Felder ihre Prüffehler. */
  tried = false;
  busy = false;
  error: FieldError | null = null;
  #key = '';
  #initial = '';
  #host: ReactiveControllerHost;
  #make: (modal: Modal) => F | null | undefined;

  constructor(host: ReactiveControllerHost, make: (modal: Modal) => F | null | undefined) {
    this.#host = host;
    this.#make = make;
  }

  get modal(): Modal {
    return parseModal(router.state.query);
  }

  get editId(): number | null {
    const modal = this.modal;
    return modal.kind === 'edit' ? modal.id : null;
  }

  get open(): boolean {
    return this.form !== null;
  }

  get dirty(): boolean {
    return this.form !== null && JSON.stringify(this.form) !== this.#initial;
  }

  sync(): void {
    const modal = this.modal;
    const key = JSON.stringify(modal);
    if (key === this.#key) return;
    const form = modal.kind === 'none' ? null : this.#make(modal);
    if (form === undefined) return;
    this.#key = key;
    this.form = form;
    this.#initial = JSON.stringify(form);
    this.tried = false;
    this.busy = false;
    this.error = null;
  }

  set(patch: Partial<F>): void {
    if (!this.form) return;
    this.form = { ...this.form, ...patch };
    this.error = null;
    this.#host.requestUpdate();
  }

  show(modal: Modal): void {
    router.setQuery(modalQuery(modal));
  }

  close(): void {
    router.setQuery(modalQuery({ kind: 'none' }));
  }

  /**
   * Prüffehler eines Felds, sobald gespeichert werden sollte; sonst der Fehler des Backends am Feld. `params` füllt die
   * Platzhalter des Texts; eine Zahl gilt als `max` der Längenmeldung.
   */
  fieldText(errors: Record<string, string | undefined>, field: string, params: number | Record<string, string | number> = 0): string {
    const key = this.tried ? errors[field] : undefined;
    return key ? t(key, typeof params === 'number' ? { max: params } : params) : errorFor(this.error, field);
  }

  /** Führt das Speichern aus: Prüfen, Aufruf, Fehler ans Feld. `true`, wenn gespeichert. */
  async submit(valid: boolean, save: () => Promise<void>, fields: Record<string, string>): Promise<boolean> {
    this.tried = true;
    if (!valid || this.busy) {
      this.#host.requestUpdate();
      return false;
    }
    this.busy = true;
    this.error = null;
    this.#host.requestUpdate();
    try {
      await save();
      this.#initial = JSON.stringify(this.form);
      this.close();
      return true;
    } catch (error) {
      this.error = fieldError(error, fields);
      return false;
    } finally {
      this.busy = false;
      this.#host.requestUpdate();
    }
  }
}

// ---- Fehler des Backends am Feld --------------------------------------------------------------------

export interface FieldError {
  /** Formularfeld, an dem der Fehler steht; `null` = allgemein (unter dem Formular). */
  field: string | null;
  message: string;
}

/**
 * Ordnet einen Fehler des Backends einem Formularfeld zu: über `params.field` (Prüfungen wie
 * `validation.too_long`) oder über den Fehlercode. `fields` übersetzt beide in Feldnamen des Formulars.
 */
export function fieldError(error: unknown, fields: Record<string, string>): FieldError {
  const message = errorText(error);
  if (!(error instanceof ApiError)) return { field: null, message };
  const param = typeof error.params.field === 'string' ? error.params.field : '';
  const field = (param && fields[`field:${param}`]) || fields[error.code] || null;
  return { field, message };
}

/** Fehlertext für ein Feld, falls der Fehler dort steht. */
export function errorFor(error: FieldError | null, field: string): string {
  return error?.field === field ? error.message : '';
}

/** Fehlertext unter dem Formular, falls er keinem Feld zugeordnet ist. */
export function generalError(error: FieldError | null): string {
  return error && error.field === null ? error.message : '';
}

// ---- Überwachung über eine Alarmregel ----------------------------------------------------------------

export type MonitorState = 'active' | 'logOnly' | 'disabled' | 'none';

/** Wie eine Selbstbeobachtung (APT-Cache, CrowdSec-LAPI) überwacht wird: aus der ersten Regel ihres Typs. */
export function monitorState(rules: { type: string; enabled: boolean; channel_id: number | null }[], type: string): MonitorState {
  const rule = rules.find((r) => r.type === type && r.enabled) ?? rules.find((r) => r.type === type);
  if (!rule) return 'none';
  if (!rule.enabled) return 'disabled';
  return rule.channel_id ? 'active' : 'logOnly';
}

/** Zeilen eines Textfelds ohne Leerzeilen. */
export function nonEmptyLines(value: string): string[] {
  return value
    .split('\n')
    .map((line) => line.trim())
    .filter(Boolean);
}
