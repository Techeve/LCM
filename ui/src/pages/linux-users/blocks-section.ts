import { LitElement, css, html, nothing } from 'lit';
import '../../components/lcm-badge';
import '../../components/lcm-button';
import '../../components/lcm-dialog';
import type { Column } from '../../components/lcm-data-table';
import { toast } from '../../components/lcm-toaster';
import { baseStyles, emit, utilStyles } from '../../components/shared';
import { blocksApi, blockUsageUrl, normalizeUsage, type Block } from '../../api/linux-users';
import { api } from '../../lib/api';
import { I18nController } from '../../lib/controllers';
import { errorText, i18n, t } from '../../lib/i18n';
import { cloneBlock } from './copy-actions';
import type { TableView } from './lu-paging';
import { pagedTable, plainLabels } from './lu-table';
import { blockName } from './profile-rules';

/** Beschreibung in der Sprache der Oberfläche; ohne englische Fassung die deutsche. */
function blockDescription(block: Block): string {
  return i18n.language.startsWith('en') && block.description_en ? block.description_en : block.description;
}

/**
 * <lcm-lu-blocks .blocks=${…} active-id="3" can-write> – Tabelle der Regelbausteine mit Kopieren und Löschen je Zeile.
 * Meldet `lcm-open-row` mit { id }, `lcm-copied` mit { id }.
 */
export class LcmLuBlocks extends LitElement {
  static override properties = {
    _dialogError: { state: true },
    blocks: { attribute: false },
    activeId: { type: String, attribute: 'active-id' },
    view: { attribute: false },
    canWrite: { type: Boolean, attribute: 'can-write' },
    _delete: { state: true },
    _usage: { state: true },
    _busy: { state: true },
  };
  static override styles = [
    baseStyles,
    utilStyles,
    css`
      :host {
        display: block;
        min-inline-size: 0;
      }
      .name {
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        gap: var(--space-2);
      }
      .row-actions {
        display: flex;
        justify-content: flex-end;
        gap: var(--space-1);
      }
    `,
  ];

  declare blocks: Block[];
  declare activeId: string;
  declare view: TableView;
  declare canWrite: boolean;
  /** Fehler der bestätigten Aktion; steht im Dialog über dem Fuß. */
  declare _dialogError: string;
  declare _delete: Block | null;
  declare _usage: string[];
  declare _busy: boolean;

  constructor() {
    super();
    this._dialogError = '';
    this.blocks = [];
    this.activeId = '';
    this.view = { page: 1, sort: null };
    this.canWrite = false;
    this._delete = null;
    this._usage = [];
    this._busy = false;
    new I18nController(this);
  }

  async #copy(block: Block): Promise<void> {
    const id = await cloneBlock(block, this.blocks);
    if (id !== null) emit(this, 'lcm-copied', { id });
  }

  /** Vor dem Löschen: welche Profile den Baustein verwenden (das Backend lehnt das Löschen dann ab). */
  async #askDelete(block: Block): Promise<void> {
    this._usage = [];
    this._delete = block;
    try {
      this._usage = normalizeUsage(await api.get(blockUsageUrl(block.id)));
    } catch {
      this._usage = [];
    }
  }

  async #confirmDelete(): Promise<void> {
    const block = this._delete;
    if (!block) return;
    this._busy = true;
    this._dialogError = '';
    try {
      await blocksApi.remove(block.id);
      toast({ tone: 'ok', title: t('profileBlocks.notice.deleted', { name: blockName(block) }) });
      this._delete = null;
    } catch (error) {
      this._dialogError = errorText(error);
    } finally {
      this._busy = false;
    }
  }

  #actions(block: Block) {
    if (!this.canWrite) return nothing;
    const stop = (run: () => void) => (e: Event) => {
      e.stopPropagation();
      run();
    };
    return html`<span class="row-actions">
      <lcm-button variant="ghost" size="small" icon="copy" icon-only label=${t('profileBlocks.clone')} @click=${stop(() => void this.#copy(block))}></lcm-button>
      ${block.builtin
        ? nothing
        : html`<lcm-button variant="ghost" size="small" icon="trash" icon-only label=${t('profileBlocks.delete.label')} @click=${stop(() => void this.#askDelete(block))}></lcm-button>`}
    </span>`;
  }

  get #columns(): Column<Block>[] {
    return [
      {
        key: 'name',
        label: t('profileBlocks.column.name'),
        sortable: true,
        value: (r) => blockName(r),
        render: (r) => html`<span class="name"><strong>${blockName(r)}</strong>${r.builtin ? html`<lcm-badge>${t('linuxUsers.builtin')}</lcm-badge>` : nothing}</span>`,
      },
      { key: 'params', label: t('profileBlocks.column.params'), kind: 'mono', render: (r) => r.params || '–' },
      { key: 'variants', label: t('profileBlocks.column.variants'), render: (r) => r.variants.map((v) => (v.family === 'all' ? t('profileBlocks.family.all') : v.family)).join(', ') },
      { key: 'description', label: t('profileBlocks.column.description'), render: (r) => html`<span class="muted">${blockDescription(r) || '–'}</span>` },
      { key: 'actions', label: t('app.actions'), labelHidden: true, align: 'end', render: (r) => this.#actions(r) },
    ];
  }

  #deleteDialog() {
    const block = this._delete;
    const name = block ? blockName(block) : '';
    const used = this._usage.length;
    return html`<lcm-dialog
      ?open=${block !== null}
      heading=${t('profileBlocks.delete.title', { name })}
      message=${used > 0 ? t('profileBlocks.delete.inUse', { list: this._usage.join(', ') }) : t('profileBlocks.delete.message')}
      .impacts=${[
        { label: t('profileBlocks.delete.profiles'), value: used > 0 ? this._usage.join(', ') : t('profileBlocks.delete.none'), tone: used > 0 ? 'crit' : 'ok' },
        { label: t('linuxUsers.impact.reversible'), value: t('linuxUsers.impact.no'), tone: 'crit' },
      ]}
      expect=${name}
      expect-label=${t('profileBlocks.delete.expect', { name })}
      confirm-label=${t('profileBlocks.delete.confirm')}
      cancel-label=${t('linuxUsers.action.cancel')}
      tone="danger"
      error=${this._dialogError}
      ?busy=${this._busy}
      @lcm-confirm=${() => void this.#confirmDelete()}
      @lcm-cancel=${() => (this._delete = null)}
    ></lcm-dialog>`;
  }

  override render() {
    return html`${pagedTable({
        items: this.blocks,
        columns: this.#columns,
        labels: plainLabels(t('profileBlocks.caption'), t('profileBlocks.noMatch.title')),
        view: this.view,
        activeId: this.activeId,
        onActivate: (id) => emit(this, 'lcm-open-row', { id }),
      })}
      ${this.#deleteDialog()}`;
  }
}
customElements.define('lcm-lu-blocks', LcmLuBlocks);
