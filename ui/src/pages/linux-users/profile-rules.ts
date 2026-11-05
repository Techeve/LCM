import { LitElement, css, html, nothing } from 'lit';
import '../../components/lcm-badge';
import '../../components/lcm-button';
import '../../components/lcm-combobox';
import '../../components/lcm-input';
import '../../components/lcm-select';
import '../../components/lcm-switch';
import type { Option } from '../../components/lcm-select';
import { baseStyles, emit } from '../../components/shared';
import { PATH_MODES, type Block, type PathMode } from '../../api/linux-users';
import { I18nController } from '../../lib/controllers';
import { i18n, t } from '../../lib/i18n';
import { LIMITS } from '../../lib/validate';
import { sectionStyles } from './lu-styles';
import { EXAMPLES } from './examples';
import {
  availableBlocks,
  blockParams,
  emptyPathRule,
  emptySudoRule,
  paramValue,
  setParamValue,
  type ProfileErrors,
  type ProfileForm,
} from './profile-model';

const MODE_LABEL: Record<PathMode, string> = {
  read: 'linuxUsers.profiles.mode.read',
  readwrite: 'linuxUsers.profiles.mode.readwrite',
  deny: 'linuxUsers.profiles.mode.deny',
};

/** Anzeigename eines Bausteins in der Sprache der Oberfläche; ohne englische Fassung der deutsche. */
export function blockName(block: Block): string {
  return i18n.language.startsWith('en') && block.name_en ? block.name_en : block.name;
}

/**
 * <lcm-lu-profile-rules .form=${…} .errors=${…} .blocks=${…} ?read-only> – die Regeln eines Profils: Bausteine mit
 * Parametern, Kommandos als root, Dateien zum Bearbeiten, Verzeichnisrechte. Meldet `lcm-change` mit einem Teil
 * des Formulars ({ sudo_rules } …); der Aufrufer führt es zusammen.
 */
export class LcmLuProfileRules extends LitElement {
  static override properties = {
    form: { attribute: false },
    errors: { attribute: false },
    blocks: { attribute: false },
    attempted: { type: Boolean },
    readOnly: { type: Boolean, attribute: 'read-only' },
  };
  static override styles = [
    baseStyles,
    sectionStyles,
    css`
      :host {
        display: flex;
        flex-direction: column;
        gap: var(--space-4);
        padding-block-start: var(--space-4);
        border-block-start: 1px solid var(--line);
      }
      section + section {
        padding-block-start: var(--space-4);
        border-block-start: 1px solid var(--line);
      }
      .rule {
        display: flex;
        flex-direction: column;
        gap: var(--space-3);
        padding: var(--space-3);
        border: 1px solid var(--line);
        border-radius: var(--radius-m);
      }
      .rule-head {
        display: flex;
        align-items: center;
        gap: var(--space-2);
      }
      .rule-head .grow {
        flex: 1;
        min-inline-size: 0;
      }
      .path-row {
        display: grid;
        grid-template-columns: minmax(0, 1fr) 12rem auto;
        gap: var(--space-2);
        align-items: end;
      }
      .path-row.single {
        grid-template-columns: minmax(0, 1fr) auto;
      }
      .toggles {
        display: flex;
        flex-wrap: wrap;
        gap: var(--space-2) var(--space-5);
      }
      .row-error {
        margin: 0;
        color: var(--crit-text);
        font-size: var(--fs-s);
      }
      .head {
        display: flex;
        align-items: center;
        justify-content: space-between;
        gap: var(--space-3);
      }
    `,
  ];

  declare form: ProfileForm;
  declare errors: ProfileErrors;
  declare blocks: Block[];
  declare attempted: boolean;
  declare readOnly: boolean;

  constructor() {
    super();
    this.blocks = [];
    this.attempted = false;
    this.readOnly = false;
    new I18nController(this);
  }

  #patch(patch: Partial<ProfileForm>): void {
    emit(this, 'lcm-change', patch);
  }

  /** Der Fehlertext einer Zeile; `max` füllt den Platzhalter der Längenmeldung. */
  #error(key: string | undefined, max = 0): string {
    return this.attempted && key ? t(key, { max }) : '';
  }

  #replace<T>(list: T[], index: number, value: T): T[] {
    return list.map((item, i) => (i === index ? value : item));
  }

  #remove<T>(list: T[], index: number): T[] {
    return list.filter((_, i) => i !== index);
  }

  #removeButton(label: string, run: () => void) {
    return this.readOnly ? nothing : html`<lcm-button variant="ghost" icon="trash" icon-only label=${label} @click=${run}></lcm-button>`;
  }

  #sectionHead(title: string, add: () => void) {
    return html`<div class="head">
      <h3>${title}</h3>
      ${this.readOnly ? nothing : html`<lcm-button variant="outline" size="small" icon="plus" @click=${add}>${t('linuxUsers.profiles.addRule')}</lcm-button>`}
    </div>`;
  }

  // ---- Bausteine ----

  #blockUse(index: number) {
    const use = this.form.block_uses[index]!;
    const block = this.blocks.find((b) => b.id === use.block_id);
    const params = block ? blockParams(block) : [];
    const error = this.#error(this.errors.blocks[index], LIMITS.description);
    return html`<div class="rule">
      <div class="rule-head">
        <span class="grow"><strong>${block ? blockName(block) : `#${use.block_id}`}</strong> ${block?.builtin ? html`<lcm-badge>${t('linuxUsers.builtin')}</lcm-badge>` : nothing}</span>
        ${this.#removeButton(t('linuxUsers.profiles.removeBlock'), () => this.#patch({ block_uses: this.#remove(this.form.block_uses, index) }))}
      </div>
      ${params.map(
        (name) => html`<lcm-input
          label=${name}
          mono
          .value=${paramValue(use.values, name)}
          ?disabled=${this.readOnly}
          .error=${error && paramValue(use.values, name) === '' ? error : ''}
          @lcm-input=${(e: CustomEvent<{ value: string }>) =>
            this.#patch({ block_uses: this.#replace(this.form.block_uses, index, { ...use, values: setParamValue(use.values, name, e.detail.value.trim()) }) })}
        ></lcm-input>`,
      )}
      ${error && params.every((name) => paramValue(use.values, name) !== '') ? html`<p class="row-error">${error}</p>` : nothing}
    </div>`;
  }

  #blocks() {
    const options: Option[] = availableBlocks(this.blocks, this.form.block_uses).map((b) => ({ value: String(b.id), label: blockName(b) }));
    return html`<section>
      <h3>${t('linuxUsers.profiles.blocks')}</h3>
      <p>${t('linuxUsers.profiles.blocksHint')}</p>
      ${this.form.block_uses.map((_, i) => this.#blockUse(i))}
      ${this.readOnly
        ? this.form.block_uses.length === 0
          ? html`<p class="empty">${t('linuxUsers.profiles.noBlocks')}</p>`
          : nothing
        : html`<lcm-combobox
            label=${t('linuxUsers.profiles.addBlock')}
            width="l"
            placeholder=${t('linuxUsers.profiles.addBlockPlaceholder')}
            empty-text=${t('linuxUsers.profiles.noBlockMatch')}
            .options=${options}
            .value=${''}
            @lcm-change=${(e: CustomEvent<{ value: string }>) => this.#patch({ block_uses: [...this.form.block_uses, { block_id: Number(e.detail.value), values: '' }] })}
          ></lcm-combobox>`}
    </section>`;
  }

  // ---- Kommandos als root ----

  #sudoRule(index: number) {
    const rule = this.form.sudo_rules[index]!;
    const change = (patch: Partial<typeof rule>) => this.#patch({ sudo_rules: this.#replace(this.form.sudo_rules, index, { ...rule, ...patch }) });
    return html`<div class="rule">
      <div class="rule-head">
        <lcm-input
          class="grow"
          label=${t('linuxUsers.profiles.command')}
          width="full"
          mono
          placeholder=${EXAMPLES.sudoCommand}
          .value=${rule.command}
          maxlength=${LIMITS.command}
          .error=${this.#error(this.errors.sudo[index], LIMITS.command)}
          ?disabled=${this.readOnly}
          @lcm-input=${(e: CustomEvent<{ value: string }>) => change({ command: e.detail.value })}
        ></lcm-input>
        ${this.#removeButton(t('linuxUsers.profiles.removeRule'), () => this.#patch({ sudo_rules: this.#remove(this.form.sudo_rules, index) }))}
      </div>
      <div class="toggles">
        <lcm-switch
          label=${t('linuxUsers.profiles.requirePassword')}
          ?checked=${rule.require_password}
          ?disabled=${this.readOnly}
          on-label=${t('linuxUsers.profiles.on')}
          on-text=${t('linuxUsers.profiles.requirePasswordOn')}
          off-label=${t('linuxUsers.profiles.off')}
          off-text=${t('linuxUsers.profiles.requirePasswordOff')}
          @lcm-change=${(e: CustomEvent<{ checked: boolean }>) => change({ require_password: e.detail.checked })}
        ></lcm-switch>
        <lcm-switch
          label=${t('linuxUsers.profiles.rootEquivalent')}
          ?checked=${rule.allow_root_equivalent}
          ?disabled=${this.readOnly}
          on-label=${t('linuxUsers.profiles.on')}
          on-text=${t('linuxUsers.profiles.rootEquivalentOn')}
          off-label=${t('linuxUsers.profiles.off')}
          off-text=${t('linuxUsers.profiles.rootEquivalentOff')}
          @lcm-change=${(e: CustomEvent<{ checked: boolean }>) => change({ allow_root_equivalent: e.detail.checked })}
        ></lcm-switch>
      </div>
    </div>`;
  }

  #sudo() {
    return html`<section>
      ${this.#sectionHead(t('linuxUsers.profiles.sudo'), () => this.#patch({ sudo_rules: [...this.form.sudo_rules, emptySudoRule()] }))}
      <p>${t('linuxUsers.profiles.sudoHint')}</p>
      ${this.form.sudo_rules.map((_, i) => this.#sudoRule(i))}
    </section>`;
  }

  // ---- Dateien und Verzeichnisse ----

  #editRule(index: number) {
    return html`<div class="path-row single">
      <lcm-input
        label=${t('linuxUsers.profiles.file')}
        width="full"
        mono
        placeholder=${EXAMPLES.editFile}
        .value=${this.form.edit_rules[index] ?? ''}
        maxlength=${LIMITS.path}
        .error=${this.#error(this.errors.edit[index], LIMITS.path)}
        ?disabled=${this.readOnly}
        @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#patch({ edit_rules: this.#replace(this.form.edit_rules, index, e.detail.value) })}
      ></lcm-input>
      ${this.#removeButton(t('linuxUsers.profiles.removeRule'), () => this.#patch({ edit_rules: this.#remove(this.form.edit_rules, index) }))}
    </div>`;
  }

  #edit() {
    return html`<section>
      ${this.#sectionHead(t('linuxUsers.profiles.edit'), () => this.#patch({ edit_rules: [...this.form.edit_rules, ''] }))}
      <p>${t('linuxUsers.profiles.editHint')}</p>
      ${this.form.edit_rules.map((_, i) => this.#editRule(i))}
    </section>`;
  }

  #pathRule(index: number) {
    const rule = this.form.path_rules[index]!;
    const modes: Option[] = PATH_MODES.map((mode) => ({ value: mode, label: t(MODE_LABEL[mode]) }));
    const change = (patch: Partial<typeof rule>) => this.#patch({ path_rules: this.#replace(this.form.path_rules, index, { ...rule, ...patch }) });
    return html`<div class="path-row">
      <lcm-input
        label=${t('linuxUsers.profiles.directory')}
        width="full"
        mono
        placeholder=${EXAMPLES.directory}
        .value=${rule.path}
        maxlength=${LIMITS.path}
        .error=${this.#error(this.errors.path[index], LIMITS.path)}
        ?disabled=${this.readOnly}
        @lcm-input=${(e: CustomEvent<{ value: string }>) => change({ path: e.detail.value })}
      ></lcm-input>
      <lcm-select
        label=${t('linuxUsers.profiles.mode.label')}
        width="full"
        .options=${modes}
        .value=${rule.mode}
        ?disabled=${this.readOnly}
        @lcm-change=${(e: CustomEvent<{ value: string }>) => change({ mode: PATH_MODES.find((m) => m === e.detail.value) ?? 'read' })}
      ></lcm-select>
      ${this.#removeButton(t('linuxUsers.profiles.removeRule'), () => this.#patch({ path_rules: this.#remove(this.form.path_rules, index) }))}
    </div>`;
  }

  #paths() {
    return html`<section>
      ${this.#sectionHead(t('linuxUsers.profiles.paths'), () => this.#patch({ path_rules: [...this.form.path_rules, emptyPathRule()] }))}
      <p>${t('linuxUsers.profiles.pathsHint')}</p>
      ${this.form.path_rules.map((_, i) => this.#pathRule(i))}
    </section>`;
  }

  override render() {
    return html`${this.#blocks()} ${this.#sudo()} ${this.#edit()} ${this.#paths()}`;
  }
}
customElements.define('lcm-lu-profile-rules', LcmLuProfileRules);
