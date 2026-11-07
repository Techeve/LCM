import { LitElement, css, html, nothing, type TemplateResult } from 'lit';
import '../../components/lcm-badge';
import '../../components/lcm-button';
import '../../components/lcm-card';
import '../../components/lcm-data-table';
import '../../components/lcm-dialog';
import '../../components/lcm-input';
import '../../components/lcm-server-action';
import '../../components/lcm-skeleton';
import type { Column } from '../../components/lcm-data-table';
import type { Impact } from '../../components/lcm-dialog';
import { toast } from '../../components/lcm-toaster';
import { baseStyles } from '../../components/shared';
import { refreshServer } from '../../api/servers';
import { hardenedPathsUrl, normaliseHardenedPaths, securityApi, type HardenSuggestion, type HardenTarget, type HardenedPath } from '../../api/servers-security';
import { I18nController } from '../../lib/controllers';
import { errorText, t } from '../../lib/i18n';
import { LIMITS, charCount, isLinuxUser } from '../../lib/validate';
import { LiveData } from '../../lib/live-data';
import { store } from '../../lib/store';
import type { ActionPreviewData } from '../../api/servers';
import { actionLabels, effectImpacts } from './preview';
import './preview-panel';
import './set-action';
import './srv-action';
import { plainLabels } from './srv-tab';
import { secStyles } from './sec-styles';

export interface HardenForm {
  path: string;
  group: string;
  unit: string;
}

/** Was die Bestätigung gerade fragt: mehrere Vorschläge, ein Pfad von Hand oder das Zurücknehmen eines Pfads. */
type Pending = { kind: 'harden'; targets: HardenTarget[]; single: boolean } | { kind: 'restore'; row: HardenedPath } | null;

/** Die angehakten Vorschläge als Ziele; die Gruppe ist die, unter der die Inhalte liegen. */
export function chosenTargets(suggestions: readonly HardenSuggestion[], chosen: readonly string[]): HardenTarget[] {
  return suggestions.filter((s) => chosen.includes(s.path)).map((s) => ({ path: s.path, group: s.serviceGroup, unit: '' }));
}

// Dieselben Zeichen lehnt das Backend ab (`hasShellMeta`, `hasWildcard`, `ValidateHardenTarget`).
/** Ein Vorschlag als Tabellenzeile: der Pfad dient als Kennung. */
type SuggestionRow = HardenSuggestion & { id: string };

const SHELL_META = /[;|&$`\n\r\\<>()!{}'"#,]/;
const WILDCARD = /[*?[\]]/;

/** Was am Formular für ein Verzeichnis von Hand nicht stimmt, je Feld als Übersetzungsschlüssel. */
export function hardenErrors(form: HardenForm): Partial<Record<keyof HardenForm, string>> {
  const errors: Partial<Record<keyof HardenForm, string>> = {};
  const path = form.path.trim();
  if (charCount(path) > LIMITS.path) errors.path = 'check.tooLong';
  else if (path !== '' && !path.startsWith('/')) errors.path = 'serverSecurity.harden.error.relative';
  else if (SHELL_META.test(path) || WILDCARD.test(path)) errors.path = 'serverSecurity.harden.error.meta';
  const group = form.group.trim();
  if (group !== '' && !isLinuxUser(group)) errors.group = 'check.linuxUser';
  const unit = form.unit.trim();
  if (charCount(unit) > LIMITS.name) errors.unit = 'check.tooLong';
  else if (SHELL_META.test(unit) || WILDCARD.test(unit) || /[\s/]/.test(unit)) errors.unit = 'serverSecurity.harden.error.unit';
  return errors;
}

const EMPTY_FORM: HardenForm = { path: '', group: '', unit: '' };
/** Beispielwerte der Felder; Pfade und Kennungen, keine übersetzbaren Texte. */
const EXAMPLE: HardenForm = { path: '/srv/kundendaten', group: 'www-data', unit: 'nginx' };

/**
 * `<lcm-srv-harden server-id server-name has-acl acl-usable can-write>`: Verzeichnisrechte des Servers. Zeigt, ob ACLs
 * wirken (Voraussetzung für die Verzeichnisrechte der Berechtigungsprofile), und schottet Verzeichnisse ab: Welt-Zugriff
 * entfernen, vorher die Gruppe der Inhalte setzen. Vorschläge sucht LCM selbst; jeder Eingriff lässt sich zurücknehmen.
 */
export class SrvHarden extends LitElement {
  static override properties = {
    _dialogError: { state: true },
    serverId: { type: Number, attribute: 'server-id' },
    serverName: { type: String, attribute: 'server-name' },
    hasAcl: { type: Boolean, attribute: 'has-acl' },
    aclUsable: { type: Boolean, attribute: 'acl-usable' },
    canWrite: { type: Boolean, attribute: 'can-write' },
    _suggestions: { state: true },
    _chosen: { state: true },
    _searching: { state: true },
    _form: { state: true },
    _pending: { state: true },
    _preview: { state: true },
    _busy: { state: true },
  };
  static override styles = [
    baseStyles,
    secStyles,
    css`
      :host {
        display: block;
        min-inline-size: 0;
      }
      .acl {
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        gap: var(--space-2) var(--space-3);
      }
      .acl strong {
        font-weight: var(--fw-semibold);
      }
      .bulk-actions {
        display: contents;
      }
    `,
  ];

  declare serverId: number;
  declare serverName: string;
  declare hasAcl: boolean;
  declare aclUsable: boolean;
  declare canWrite: boolean;
  /** Fehler der bestätigten Aktion; steht im Dialog über dem Fuß. */
  declare _dialogError: string;
  declare _suggestions: HardenSuggestion[] | undefined;
  declare _chosen: string[];
  declare _searching: boolean;
  declare _form: HardenForm;
  declare _pending: Pending;
  declare _preview: ActionPreviewData | undefined;
  declare _busy: boolean;

  readonly #paths = new LiveData<HardenedPath[]>(this, {
    url: () => (this.serverId ? hardenedPathsUrl(this.serverId) : null),
    topics: () => [`server:${this.serverId}`],
    transform: normaliseHardenedPaths,
  });

  constructor() {
    super();
    this._dialogError = '';
    this.serverId = 0;
    this.serverName = '';
    this.hasAcl = false;
    this.aclUsable = false;
    this.canWrite = false;
    this._suggestions = undefined;
    this._chosen = [];
    this._searching = false;
    this._form = { ...EMPTY_FORM };
    this._pending = null;
    this._preview = undefined;
    this._busy = false;
    new I18nController(this);
  }

  async #search(): Promise<void> {
    this._searching = true;
    try {
      const found = await securityApi.hardenSuggestions(this.serverId);
      this._suggestions = found;
      this._chosen = found.map((s) => s.path);
    } catch (error) {
      toast({ tone: 'crit', title: t('serverSecurity.harden.suggest.failed'), message: errorText(error) });
    } finally {
      this._searching = false;
    }
  }

  #changed(): void {
    store.invalidate(hardenedPathsUrl(this.serverId));
    refreshServer(this.serverId);
  }

  /** Ein Pfad von Hand geht einzeln (Fehler kommen als Fehler zurück), Vorschläge gesammelt mit Ergebnis je Pfad. */
  async #harden(targets: HardenTarget[], single: boolean): Promise<void> {
    if (single) {
      await securityApi.hardenPath(this.serverId, targets[0]);
      toast({ tone: 'ok', title: t('serverSecurity.harden.done', { path: targets[0].path }) });
      this._form = { ...EMPTY_FORM };
      return;
    }
    const results = await securityApi.hardenPaths(this.serverId, targets);
    const failed = results.filter((r) => !r.ok);
    const ok = results.length - failed.length;
    if (failed.length === 0) toast({ tone: 'ok', title: t('serverSecurity.harden.bulkDone', { count: ok }) });
    else toast({ tone: 'warn', title: t('serverSecurity.harden.bulkPartial', { ok, failed: failed.length }), message: failed.map((r) => `${r.path}: ${r.message}`).join('\n') });
    this._suggestions = undefined;
    this._chosen = [];
  }

  override willUpdate(changed: Map<string, unknown>): void {
    if (changed.has('_pending')) this._preview = undefined;
  }

  async #confirm(): Promise<void> {
    const pending = this._pending;
    if (!pending) return;
    this._busy = true;
    this._dialogError = '';
    try {
      if (pending.kind === 'restore') {
        await securityApi.restorePath(this.serverId, pending.row.id);
        toast({ tone: 'ok', title: t('serverSecurity.harden.restored', { path: pending.row.path }) });
      } else {
        await this.#harden(pending.targets, pending.single);
      }
      this._pending = null;
      this.#changed();
    } catch (error) {
      this._dialogError = errorText(error);
    } finally {
      this._busy = false;
    }
  }

  #askForm(): void {
    const target = { path: this._form.path.trim(), group: this._form.group.trim(), unit: this._form.unit.trim() };
    this._pending = { kind: 'harden', targets: [target], single: true };
  }

  // ---- ACL ----

  #renderAcl(): TemplateResult {
    const [tone, key] = this.aclUsable
      ? (['ok', 'serverSecurity.acl.usable'] as const)
      : this.hasAcl
        ? (['warn', 'serverSecurity.acl.noFs'] as const)
        : (['neutral', 'serverSecurity.acl.missing'] as const);
    return html`<div class="acl">
      <strong>${t('serverSecurity.acl.title')}</strong>
      <lcm-badge tone=${tone}>${t(key)}</lcm-badge>
      ${this.canWrite && !this.aclUsable && !this.hasAcl
        ? html`<lcm-set-action
            variant="outline"
            icon="download"
            server-id=${this.serverId}
            server-name=${this.serverName}
            action="acl-install"
            label=${t('serverSecurity.acl.install.label')}
            help=${t('serverSecurity.acl.install.help')}
            heading=${t('serverSecurity.acl.install.heading', { name: this.serverName })}
            .run=${() => securityApi.installAcl(this.serverId)}
          ></lcm-set-action>`
        : nothing}
    </div>`;
  }

  // ---- Abgeschottete Verzeichnisse ----

  get #pathColumns(): Column<HardenedPath>[] {
    return [
      { key: 'path', label: t('serverSecurity.harden.path'), kind: 'mono', sortable: true },
      { key: 'before', label: t('serverSecurity.harden.before'), kind: 'mono', render: (r) => `${r.prevMode} ${r.prevGroup}`.trim() },
      { key: 'after', label: t('serverSecurity.harden.after'), kind: 'mono', render: (r) => `${r.mode} ${r.group}`.trim() },
      { key: 'unit', label: t('serverSecurity.harden.unit'), kind: 'mono', render: (r) => r.unit || '–' },
      ...(this.canWrite
        ? [
            {
              key: 'restore',
              label: t('serverSecurity.harden.actions'),
              align: 'end' as const,
              render: (r: HardenedPath) => {
                const row = (this.#paths.data ?? []).find((x) => String(x.id) === String(r.id));
                return row ? html`<lcm-button size="small" variant="ghost" icon="undo" @click=${() => (this._pending = { kind: 'restore', row })}>${t('serverSecurity.harden.restore')}</lcm-button>` : '';
              },
            },
          ]
        : []),
    ];
  }

  #renderPaths(): TemplateResult {
    const { data } = this.#paths;
    if (data === undefined) return html`<lcm-skeleton shape="row" lines="2" label=${t('servers.loading')}></lcm-skeleton>`;
    if (data.length === 0) return html`<p class="muted">${t('serverSecurity.harden.none')}</p>`;
    const rows = data.map((p) => ({ ...p, id: String(p.id) }));
    return html`<lcm-data-table no-toolbar density="compact" .columns=${this.#pathColumns} .rows=${rows} .labels=${plainLabels(t('serverSecurity.harden.listTitle'), t('serverSecurity.harden.none'))}></lcm-data-table>`;
  }

  // ---- Vorschläge und Eingabe von Hand ----

  get #suggestionColumns(): Column<SuggestionRow>[] {
    return [
      { key: 'path', label: t('serverSecurity.harden.path'), kind: 'mono', sortable: true },
      { key: 'mode', label: t('serverSecurity.harden.suggest.now'), kind: 'mono', render: (r) => `${r.mode} ${r.group}`.trim() },
      { key: 'serviceGroup', label: t('serverSecurity.harden.suggest.group'), kind: 'mono' },
      { key: 'kind', label: t('serverSecurity.harden.suggest.kind'), render: (r) => t(r.kind === 'config' ? 'serverSecurity.harden.suggest.config' : 'serverSecurity.harden.suggest.data') },
    ];
  }

  #renderSuggestions(): TemplateResult {
    const found = this._suggestions;
    const targets = found ? chosenTargets(found, this._chosen) : [];
    return html`<section class="section">
      <div class="row">
        <h3>${t('serverSecurity.harden.suggest.title')}</h3>
        <lcm-server-action
          variant="ghost"
          icon="search"
          label=${t('serverSecurity.harden.suggest.label')}
          help=${t('serverSecurity.harden.suggest.help')}
          meaning=${t('serverSecurity.harden.suggest.meaning')}
          ?busy=${this._searching}
          .labels=${actionLabels()}
          .preview=${{}}
          @click=${() => void this.#search()}
        ></lcm-server-action>
      </div>
      ${found === undefined
        ? nothing
        : found.length === 0
          ? html`<p class="muted">${t('serverSecurity.harden.suggest.none')}</p>`
          : html`<lcm-data-table
              no-toolbar
              no-chooser
              selectable
              density="compact"
              .columns=${this.#suggestionColumns}
              .rows=${found.map((s) => ({ ...s, id: s.path }))}
              .labels=${plainLabels(t('serverSecurity.harden.suggest.title'), t('serverSecurity.harden.suggest.none'))}
              .selected=${this._chosen}
              @lcm-selection-change=${(e: CustomEvent<{ selected: string[] }>) => (this._chosen = e.detail.selected)}
            >
              <span slot="bulk" class="bulk-actions">
                <lcm-button size="small" variant="primary" icon="lock" ?disabled=${targets.length === 0} @click=${() => (this._pending = { kind: 'harden', targets, single: false })}>
                  ${t('serverSecurity.harden.suggest.apply', { count: targets.length })}
                </lcm-button>
              </span>
            </lcm-data-table>`}
    </section>`;
  }

  #renderForm(): TemplateResult {
    const form = this._form;
    const patch = (change: Partial<HardenForm>) => (this._form = { ...this._form, ...change });
    const errors = hardenErrors(form);
    const shown = (key: string | undefined, max: number) => (key ? t(key, { max }) : '');
    return html`<section class="section">
      <h3>${t('serverSecurity.harden.manual')}</h3>
      <div class="row end">
        <lcm-input label=${t('serverSecurity.harden.path')} width="m" mono maxlength=${LIMITS.path} placeholder=${EXAMPLE.path} .value=${form.path} error=${shown(errors.path, LIMITS.path)} @lcm-input=${(e: CustomEvent<{ value: string }>) => patch({ path: e.detail.value })}></lcm-input>
        <lcm-input label=${t('serverSecurity.harden.group')} width="s" mono maxlength=${LIMITS.linuxUser} placeholder=${EXAMPLE.group} .value=${form.group} error=${shown(errors.group, 0)} @lcm-input=${(e: CustomEvent<{ value: string }>) => patch({ group: e.detail.value })}></lcm-input>
        <lcm-input label=${t('serverSecurity.harden.unitInput')} width="s" mono maxlength=${LIMITS.name} placeholder=${EXAMPLE.unit} .value=${form.unit} error=${shown(errors.unit, LIMITS.name)} @lcm-input=${(e: CustomEvent<{ value: string }>) => patch({ unit: e.detail.value })}></lcm-input>
        <lcm-server-action
          variant="outline"
          icon="lock"
          label=${t('serverSecurity.harden.apply')}
          help=${t('serverSecurity.harden.applyHelp')}
          ?disabled=${form.path.trim() === '' || Object.keys(errors).length > 0}
          .labels=${actionLabels()}
          .preview=${{}}
          @click=${() => this.#askForm()}
        ></lcm-server-action>
      </div>
    </section>`;
  }

  // ---- Bestätigung ----

  /** Was nur die Oberfläche weiß (welche Verzeichnisse); die Wirkung kommt aus der Vorschau des Backends. */
  #impacts(pending: NonNullable<Pending>): Impact[] {
    const server = { label: t('servers.confirm.server'), value: this.serverName };
    const paths =
      pending.kind === 'restore'
        ? { label: t('serverSecurity.harden.path'), value: pending.row.path }
        : { label: t('serverSecurity.harden.impact.paths'), value: pending.targets.map((x) => (x.group ? `${x.path} (${x.group})` : x.path)).join(', ') };
    return [server, paths, ...effectImpacts(this._preview)];
  }

  #previewPanel(pending: Pending) {
    if (!pending) return nothing;
    const restore = pending.kind === 'restore';
    return html`<lcm-preview-panel
      server-id=${this.serverId}
      action=${restore ? 'path-restore' : 'path-harden'}
      .params=${restore ? { pathId: pending.row.id } : { targets: pending.targets }}
      @lcm-preview=${(e: CustomEvent<ActionPreviewData>) => (this._preview = e.detail)}
    ></lcm-preview-panel>`;
  }

  #renderDialog(): TemplateResult {
    const pending = this._pending;
    const restore = pending?.kind === 'restore';
    const count = pending?.kind === 'harden' ? pending.targets.length : 0;
    return html`<lcm-dialog
      ?open=${pending !== null}
      heading=${restore ? t('serverSecurity.harden.restoreHeading', { path: pending.row.path }) : t('serverSecurity.harden.heading', { count, name: this.serverName })}
      message=${restore ? t('serverSecurity.harden.restoreMessage') : t('serverSecurity.harden.message')}
      confirm-label=${restore ? t('serverSecurity.harden.restore') : t('serverSecurity.harden.apply')}
      cancel-label=${t('servers.confirm.cancel')}
      .impacts=${pending ? this.#impacts(pending) : []}
      error=${this._dialogError}
      ?busy=${this._busy}
      @lcm-cancel=${() => (this._pending = null)}
      @lcm-confirm=${() => void this.#confirm()}
      >${this.#previewPanel(pending)}</lcm-dialog
    >`;
  }

  override render() {
    return html`<lcm-card collapsible heading=${t('serverSecurity.harden.title')} subtitle=${t('serverSecurity.harden.subtitle')}>
      <section class="section">${this.#renderAcl()}</section>
      <section class="section">
        <h3>${t('serverSecurity.harden.listTitle')}</h3>
        ${this.#renderPaths()}
      </section>
      ${this.canWrite ? html`${this.#renderSuggestions()} ${this.#renderForm()} ${this.#renderDialog()}` : nothing}
    </lcm-card>`;
  }
}
customElements.define('lcm-srv-harden', SrvHarden);
