import { LitElement, css, html, nothing } from 'lit';
import '../../components/lcm-badge';
import '../../components/lcm-button';
import '../../components/lcm-input';
import '../../components/lcm-notice';
import '../../components/lcm-select';
import type { Option } from '../../components/lcm-select';
import { toast } from '../../components/lcm-toaster';
import { baseStyles, emit } from '../../components/shared';
import { FAMILIES, blockUsageUrl, blocksApi, normalizeUsage, type Block, type BlockVariant } from '../../api/linux-users';
import { I18nController } from '../../lib/controllers';
import { errorText, t } from '../../lib/i18n';
import { LIMITS } from '../../lib/validate';
import { LiveData } from '../../lib/live-data';
import '../../components/lcm-textarea';
import './form-modal';
import { sectionStyles } from './lu-styles';
import { MAX_BLOCK_SLUG, blockFormOf, blockBody, createBlockBody, emptyBlockForm, emptyVariant, isBlockValid, previewValues, sameBlockForm, validateBlockForm, type BlockForm } from './block-model';
import { EXAMPLES } from './examples';

/**
 * <lcm-lu-block-modal open .block=${…} can-write> – Regelbaustein anlegen (ohne `block`), bearbeiten oder, bei
 * mitgelieferten, nur ansehen (dort bleibt „Kopie anlegen“). Beim Bearbeiten nennt eine Warnung die Profile, die den
 * Baustein verwenden, und je Variante zeigt „Vorschau“ die Zeilen, die daraus auf dem Server entstehen.
 * Meldet `lcm-saved`, `lcm-clone` (mit dem Baustein) und `lcm-close`.
 */
export class LcmLuBlockModal extends LitElement {
  static override properties = {
    open: { type: Boolean },
    block: { attribute: false },
    canWrite: { type: Boolean, attribute: 'can-write' },
    _form: { state: true },
    _busy: { state: true },
    _error: { state: true },
    _attempted: { state: true },
    _preview: { state: true },
  };
  static override styles = [
    baseStyles,
    sectionStyles,
    css`
      :host {
        display: contents;
      }
      .variant {
        display: flex;
        flex-direction: column;
        gap: var(--space-3);
        padding: var(--space-3);
        border: 1px solid var(--line);
        border-radius: var(--radius-m);
      }
      .variant-head {
        display: grid;
        grid-template-columns: 8rem minmax(0, 1fr) auto;
        gap: var(--space-2);
        align-items: end;
      }
      .head {
        display: flex;
        align-items: center;
        justify-content: space-between;
        gap: var(--space-3);
      }
      .pair {
        display: grid;
        grid-template-columns: repeat(auto-fit, minmax(12rem, 1fr));
        gap: var(--space-3);
      }
      pre {
        margin: 0;
        padding: var(--space-3);
        overflow-x: auto;
        border-radius: var(--radius-s);
        background: var(--surface-1);
        font-family: var(--font-mono);
        font-size: var(--fs-xs);
        line-height: 1.55;
      }
      .preview-title {
        margin: 0 0 var(--space-1);
        color: var(--text-2);
        font-size: var(--fs-s);
      }
      .row-error {
        margin: 0;
        color: var(--crit-text);
        font-size: var(--fs-s);
      }
    `,
  ];

  declare open: boolean;
  declare block: Block | null;
  declare canWrite: boolean;
  declare _form: BlockForm;
  declare _busy: boolean;
  declare _error: string;
  declare _attempted: boolean;
  declare _preview: { index: number; lines: string[] } | null;
  #initial: BlockForm = emptyBlockForm();
  #key = '';

  readonly #usage = new LiveData<string[]>(this, {
    url: () => (this.open && this.block && !this.block.builtin ? blockUsageUrl(this.block.id) : null),
    transform: normalizeUsage,
  });

  constructor() {
    super();
    this.open = false;
    this.block = null;
    this.canWrite = false;
    this._form = this.#initial;
    this._busy = false;
    this._error = '';
    this._attempted = false;
    this._preview = null;
    new I18nController(this);
  }

  override willUpdate(): void {
    const key = this.open ? `${this.block?.id ?? 'new'}` : '';
    if (key === this.#key) return;
    this.#key = key;
    if (!this.open) return;
    this.#initial = this.block ? blockFormOf(this.block) : emptyBlockForm();
    this._form = this.#initial;
    this._error = '';
    this._attempted = false;
    this._preview = null;
  }

  get #readOnly(): boolean {
    return !this.canWrite || (this.block?.builtin ?? false);
  }

  #set(patch: Partial<BlockForm>): void {
    this._form = { ...this._form, ...patch };
  }

  #setVariant(index: number, patch: Partial<BlockVariant>): void {
    this.#set({ variants: this._form.variants.map((v, i) => (i === index ? { ...v, ...patch } : v)) });
  }

  async #save(): Promise<void> {
    this._attempted = true;
    if (!isBlockValid(validateBlockForm(this._form, this.block === null))) return;
    this._busy = true;
    this._error = '';
    try {
      const name = this._form.name.trim();
      if (this.block) {
        await blocksApi.update(this.block.id, blockBody(this._form));
        toast({ tone: 'ok', title: t('profileBlocks.notice.saved', { name }) });
      } else {
        await blocksApi.create(createBlockBody(this._form));
        toast({ tone: 'ok', title: t('profileBlocks.notice.created', { name }) });
      }
      this._form = this.#initial;
      emit(this, 'lcm-saved');
    } catch (error) {
      this._error = errorText(error);
    } finally {
      this._busy = false;
    }
  }

  async #showPreview(index: number): Promise<void> {
    if (!this.block) return;
    this._error = '';
    try {
      const lines = await blocksApi.preview(this.block.id, this._form.variants[index]?.family ?? 'all', previewValues(this.block.params));
      this._preview = { index, lines };
    } catch (error) {
      this._preview = null;
      this._error = t('profileBlocks.preview.failedText', { error: errorText(error) });
    }
  }

  get #familyOptions(): Option[] {
    return FAMILIES.map((family) => ({ value: family, label: family === 'all' ? t('profileBlocks.family.all') : family }));
  }

  #variant(index: number) {
    return html`<div class="variant">
      ${this.#variantHead(index)} ${this.#variantRules(index)} ${this.#variantPreview(index)}
    </div>`;
  }

  #variantHead(index: number) {
    const variant = this._form.variants[index]!;
    const ro = this.#readOnly;
    return html`<div class="variant-head">
      <lcm-select
        label=${t('profileBlocks.variant.family')}
        width="full"
        .options=${this.#familyOptions}
        .value=${variant.family}
        ?disabled=${ro}
        @lcm-change=${(e: CustomEvent<{ value: string }>) => this.#setVariant(index, { family: e.detail.value })}
      ></lcm-select>
      <lcm-input
        label=${t('profileBlocks.variant.runAs')}
        width="full"
        mono
        maxlength=${LIMITS.name}
        placeholder=${t('profileBlocks.variant.runAsPlaceholder')}
        .value=${variant.run_as}
        ?disabled=${ro}
        @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#setVariant(index, { run_as: e.detail.value })}
      ></lcm-input>
      ${ro
        ? nothing
        : html`<lcm-button variant="ghost" icon="trash" icon-only label=${t('profileBlocks.variant.remove')} @click=${() => this.#set({ variants: this._form.variants.filter((_, i) => i !== index) })}></lcm-button>`}
    </div>`;
  }

  #variantRules(index: number) {
    const variant = this._form.variants[index]!;
    const ro = this.#readOnly;
    const rowError = this._attempted ? this.#errors.rows[index] : undefined;
    return html`<lcm-textarea
        label=${t('profileBlocks.variant.commands')}
        rows="4"
        mono
        maxlength=${LIMITS.script}
        placeholder=${EXAMPLES.blockCommand}
        .value=${variant.sudo_commands}
        ?disabled=${ro}
        @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#setVariant(index, { sudo_commands: e.detail.value })}
      ></lcm-textarea>
      <lcm-textarea
        label=${t('profileBlocks.variant.files')}
        rows="2"
        mono
        maxlength=${LIMITS.script}
        .value=${variant.edit_paths}
        ?disabled=${ro}
        @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#setVariant(index, { edit_paths: e.detail.value })}
      ></lcm-textarea>
      <lcm-textarea
        label=${t('profileBlocks.variant.paths')}
        rows="2"
        mono
        maxlength=${LIMITS.script}
        placeholder=${EXAMPLES.blockPathRule}
        hint=${t('profileBlocks.variant.pathsHint')}
        .error=${rowError ? t(rowError, { max: LIMITS.script }) : ''}
        .value=${variant.path_rules}
        ?disabled=${ro}
        @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#setVariant(index, { path_rules: e.detail.value })}
      ></lcm-textarea>`;
  }

  #variantPreview(index: number) {
    const preview = this._preview?.index === index ? this._preview : null;
    return html`${this.block
        ? html`<div>
            <lcm-button variant="outline" size="small" icon="eye" @click=${() => void this.#showPreview(index)}>${t('profileBlocks.preview.label')}</lcm-button>
          </div>`
        : nothing}
      ${preview
        ? html`<div>
            <p class="preview-title">${t('profileBlocks.preview.title')}</p>
            <pre>${preview.lines.length > 0 ? preview.lines.join('\n') : t('profileBlocks.preview.none')}</pre>
          </div>`
        : nothing}`;
  }

  get #errors() {
    return validateBlockForm(this._form, this.block === null);
  }

  #fields() {
    const errors = this.#errors;
    const ro = this.#readOnly;
    const show = (key: string | undefined, max = 0) => (this._attempted && key ? t(key, { max }) : '');
    const usage = this.#usage.data ?? [];
    return html`
      ${usage.length > 0 && !ro ? html`<lcm-notice tone="warn">${t('profileBlocks.usage', { count: usage.length, list: usage.join(', ') })}</lcm-notice>` : nothing}
      <lcm-input label=${t('profileBlocks.form.name')} ?half=${this.block === null} width="full" required maxlength=${LIMITS.name} placeholder=${t('profileBlocks.form.namePlaceholder')} .value=${this._form.name} .error=${show(errors.name, LIMITS.name)} ?disabled=${ro}
        @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#set({ name: e.detail.value })}></lcm-input>
      ${this.block === null
        ? html`<lcm-input label=${t('profileBlocks.form.slug')} half width="full" mono maxlength=${MAX_BLOCK_SLUG} .value=${this._form.slug} .error=${show(errors.slug)} hint=${t('profileBlocks.form.slugHint')}
            @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#set({ slug: e.detail.value })}></lcm-input>`
        : nothing}
      <lcm-textarea label=${t('profileBlocks.form.description')} rows="2" maxlength=${LIMITS.description} .value=${this._form.description} .error=${show(errors.description, LIMITS.description)} ?disabled=${ro}
        @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#set({ description: e.detail.value })}></lcm-textarea>
      <lcm-input label=${t('profileBlocks.form.nameEn')} width="full" maxlength=${LIMITS.name} .value=${this._form.name_en} .error=${show(errors.nameEn, LIMITS.name)} ?disabled=${ro}
          @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#set({ name_en: e.detail.value })}></lcm-input>
        <lcm-textarea label=${t('profileBlocks.form.descriptionEn')} rows="2" maxlength=${LIMITS.description} .value=${this._form.description_en} .error=${show(errors.descriptionEn, LIMITS.description)} ?disabled=${ro}
          @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#set({ description_en: e.detail.value })}></lcm-textarea>
      <lcm-input label=${t('profileBlocks.form.params')} width="l" mono maxlength=${LIMITS.command} placeholder=${EXAMPLES.blockParams} .value=${this._form.params} .error=${show(errors.params, LIMITS.command)} hint=${t('profileBlocks.form.paramsHint')} ?disabled=${ro}
        @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#set({ params: e.detail.value })}></lcm-input>
      <section>
        <div class="head">
          <h3>${t('profileBlocks.form.variants')}</h3>
          ${ro ? nothing : html`<lcm-button variant="outline" size="small" icon="plus" @click=${() => this.#set({ variants: [...this._form.variants, emptyVariant()] })}>${t('profileBlocks.variant.add')}</lcm-button>`}
        </div>
        <p>${t('profileBlocks.form.variantsHint')}</p>
        ${this._form.variants.map((_, i) => this.#variant(i))}
        ${this._attempted && errors.variants ? html`<p class="row-error">${t(errors.variants)}</p>` : nothing}
      </section>
    `;
  }

  override render() {
    const block = this.block;
    return html`<lcm-lu-form-modal
      ?open=${this.open}
      heading=${block ? block.name : t('profileBlocks.form.newTitle')}
      subtitle=${block ? (block.builtin ? t('profileBlocks.form.builtinSubtitle') : block.slug) : t('profileBlocks.form.newSubtitle')}
      save-label=${block ? t('linuxUsers.action.save') : t('linuxUsers.action.create')}
      ?dirty=${!this.#readOnly && !sameBlockForm(this._form, this.#initial)}
      ?busy=${this._busy}
      ?read-only=${this.#readOnly}
      error=${this._error}
      @lcm-submit=${() => void this.#save()}
    >
      ${this.#fields()}
      ${block && this.canWrite
        ? html`<lcm-button slot="footer-start" variant="outline" icon="copy" @click=${() => emit(this, 'lcm-clone', block)}>${t('profileBlocks.clone')}</lcm-button>`
        : nothing}
    </lcm-lu-form-modal>`;
  }
}
customElements.define('lcm-lu-block-modal', LcmLuBlockModal);
