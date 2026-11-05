import { LitElement, css, html, nothing } from 'lit';
import '../../components/lcm-input';
import '../../components/lcm-select';
import type { Option } from '../../components/lcm-select';
import { baseStyles, emit } from '../../components/shared';
import { toast } from '../../components/lcm-toaster';
import {
  APT_CACHES_URL,
  CUSTOM_ACTIONS_URL,
  IP_ALLOWLISTS_URL,
  groupsApi,
  normalizeAllowlists,
  normalizeAptCaches,
  normalizeCustomActions,
  type AptCache,
  type CustomAction,
  type Group,
  type IpAllowlist,
  type Rule,
  type Schedule,
} from '../../api/groups';
import { auth } from '../../lib/auth';
import { I18nController } from '../../lib/controllers';
import { errorText, t } from '../../lib/i18n';
import { store } from '../../lib/store';
import { LIMITS } from '../../lib/validate';
import { cronText } from './cron-describe';
import './firewall-editor';
import './form-modal';
import '../../components/lcm-textarea';
import {
  commandParams,
  emptyRuleForm,
  formFromRule,
  ruleBody,
  ruleFormChanged,
  ruleUpdateBody,
  typeInfo,
  typesFor,
  validateRuleForm,
  withTrigger,
  withType,
  effectsOf,
  type RuleForm,
  type Trigger,
} from './rule-model';

const TRIGGER_NONE = '';

function triggerValue(trigger: Trigger | null): string {
  return trigger === null ? TRIGGER_NONE : String(trigger);
}

function triggerFrom(value: string): Trigger | null {
  if (value === 'enforce') return 'enforce';
  return /^\d+$/.test(value) ? Number(value) : null;
}

/**
 * <lcm-groups-rule-modal open .group=${…} .schedules=${…} .rule=${…} .trigger=${…}> – Regel anlegen (ohne `rule`)
 * oder bearbeiten. Beim Bearbeiten ändern sich Name und Einstellungen; Typ und Auslöser stehen fest.
 * Meldet `lcm-saved` und `lcm-close`.
 */
export class LcmGroupsRuleModal extends LitElement {
  static override properties = {
    open: { type: Boolean },
    group: { attribute: false },
    schedules: { attribute: false },
    rule: { attribute: false },
    trigger: { attribute: false },
    _form: { state: true },
    _busy: { state: true },
    _error: { state: true },
    _attempted: { state: true },
    _actions: { state: true },
    _caches: { state: true },
    _allowlists: { state: true },
  };
  static override styles = [
    baseStyles,
    css`
      :host {
        display: contents;
      }
      .help {
        margin: 0;
        color: var(--text-2);
        font-size: var(--fs-s);
      }
      .fact {
        display: grid;
        grid-template-columns: max-content 1fr;
        gap: var(--space-1) var(--space-4);
        margin: 0;
        font-size: var(--fs-s);
      }
      .fact dt {
        color: var(--text-2);
      }
      .fact dd {
        margin: 0;
        font-weight: var(--fw-medium);
        overflow-wrap: anywhere;
      }
      .impact {
        padding: var(--space-4);
        border-radius: var(--radius-m);
        background: var(--surface-2);
      }
      .impact h3 {
        margin: 0 0 var(--space-2);
        font-size: var(--fs-m);
      }
      .impact ul {
        display: flex;
        flex-direction: column;
        gap: var(--space-1);
        margin: 0;
        padding-inline-start: 1.1rem;
        color: var(--text-2);
        font-size: var(--fs-s);
      }
      .impact li.warn {
        color: var(--warn-text);
        font-weight: var(--fw-medium);
      }
      .note {
        margin: 0;
        padding: var(--space-3) var(--space-4);
        border-radius: var(--radius-m);
        background: var(--info-soft);
        color: var(--info-text);
        font-size: var(--fs-s);
      }
    `,
  ];

  declare open: boolean;
  declare group: Group | null;
  declare schedules: Schedule[];
  declare rule: Rule | null;
  declare trigger: Trigger | null;
  declare _form: RuleForm;
  declare _busy: boolean;
  declare _error: string;
  declare _attempted: boolean;
  declare _actions: CustomAction[];
  declare _caches: AptCache[];
  declare _allowlists: IpAllowlist[];
  #initial: RuleForm = emptyRuleForm(null);
  #wasOpen = false;

  constructor() {
    super();
    this.open = false;
    this.group = null;
    this.schedules = [];
    this.rule = null;
    this.trigger = null;
    this._form = emptyRuleForm(null);
    this._busy = false;
    this._error = '';
    this._attempted = false;
    this._actions = [];
    this._caches = [];
    this._allowlists = [];
    new I18nController(this);
  }

  override willUpdate(): void {
    if (this.open && !this.#wasOpen) {
      this.#initial = this.rule ? formFromRule(this.rule) : emptyRuleForm(this.trigger);
      this._form = this.#initial;
      this._error = '';
      this._attempted = false;
      void this.#loadChoices();
    }
    this.#wasOpen = this.open;
  }

  /** Auswahllisten für Custom-Aktion, APT-Cache und Allowlists. Fehlt ein Recht, bleibt die Liste leer. */
  async #loadChoices(): Promise<void> {
    const read = async <T>(url: string, normalize: (raw: unknown) => T[], allowed: boolean): Promise<T[]> => {
      if (!allowed) return [];
      try {
        return normalize(await store.get(url));
      } catch {
        return [];
      }
    };
    const servers = auth.can('servers:read');
    [this._actions, this._caches, this._allowlists] = await Promise.all([
      read(CUSTOM_ACTIONS_URL, normalizeCustomActions, true),
      read(APT_CACHES_URL, normalizeAptCaches, servers),
      read(IP_ALLOWLISTS_URL, normalizeAllowlists, servers),
    ]);
  }

  #set(form: RuleForm): void {
    this._form = form;
  }

  #command(value: string): void {
    this._form = { ...this._form, command: value };
  }

  async #save(): Promise<void> {
    this._attempted = true;
    if (Object.keys(validateRuleForm(this._form)).length > 0) return;
    this._busy = true;
    this._error = '';
    try {
      const name = this._form.name.trim();
      if (this.rule) await groupsApi.updateRule(this.rule.id, ruleUpdateBody(this._form));
      else if (this.group) await groupsApi.defineRule(this.group.id, ruleBody(this._form));
      toast({ tone: 'ok', title: t(this.rule ? 'rules.notice.saved' : 'rules.notice.created', { name }) });
      this._form = this.#initial;
      emit(this, 'lcm-saved');
    } catch (error) {
      this._error = errorText(error);
    } finally {
      this._busy = false;
    }
  }

  #triggerOptions(): Option[] {
    return [
      ...(this._form.trigger === null ? [{ value: TRIGGER_NONE, label: t('rules.form.triggerChoose') }] : []),
      ...this.schedules.map((s) => ({ value: String(s.id), label: `${s.name} (${s.cron_expr})` })),
      { value: 'enforce', label: t('rules.form.triggerEnforce') },
    ];
  }

  #triggerLabel(): string {
    const trigger = this._form.trigger;
    if (trigger === 'enforce') return t('rules.form.triggerEnforce');
    const schedule = this.schedules.find((s) => s.id === trigger);
    return schedule ? `${schedule.name} (${schedule.cron_expr})` : '';
  }

  #typeOptions(): Option[] {
    return typesFor(this._form.trigger).map((type) => ({ value: type.id, label: t(`${type.key}.label`) }));
  }

  #renderTypeField(error: string | undefined) {
    const message = this._attempted && error ? t(error, commandParams(this._form)) : '';
    switch (typeInfo(this._form.type)?.field) {
      case 'script':
        return this.#scriptField(message);
      case 'packages':
        return this.#packagesField(message);
      case 'custom':
        return this.#customActionField(message);
      case 'apt-proxy':
        return this.#aptCacheField();
      case 'docker-scope':
        return this.#dockerScopeField(message);
      case 'dns-domains':
        return this.#dnsDomainsField(message);
      case 'firewall':
        return this.#firewallField();
      default:
        return nothing;
    }
  }

  #scriptField(message: string) {
    return html`<lcm-textarea
      label=${t('rules.form.script')}
      rows="6"
      maxlength=${LIMITS.script}
      mono
      .value=${this._form.command}
      .error=${message}
      hint=${t('rules.form.scriptHint')}
      @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#command(e.detail.value)}
    ></lcm-textarea>`;
  }

  #packagesField(message: string) {
    return html`<lcm-input
      label=${t('rules.form.packages')}
      width="full"
      maxlength=${LIMITS.script}
      mono
      placeholder=${'htop, unzip, openssh-server'}
      .value=${this._form.command}
      .error=${message}
      hint=${t('rules.form.packagesHint')}
      @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#command(e.detail.value)}
    ></lcm-input>`;
  }

  #customActionField(message: string) {
    if (this._actions.length === 0) return html`<p class="note">${t('rules.form.noCustomActions')}</p>`;
    return html`<lcm-select
      label=${t('rules.form.customAction')}
      width="l"
      .options=${[{ value: '', label: t('rules.form.choose') }, ...this._actions.map((a) => ({ value: String(a.id), label: a.name }))]}
      .value=${this._form.command}
      hint=${message || t('rules.form.customActionHint')}
      @lcm-change=${(e: CustomEvent<{ value: string }>) => this.#command(e.detail.value)}
    ></lcm-select>`;
  }

  #aptCacheField() {
    return html`<lcm-select
      label=${t('rules.form.aptCache')}
      width="l"
      .options=${[{ value: '', label: t('rules.form.aptCacheOnly') }, ...this._caches.map((c) => ({ value: String(c.id), label: `${c.name} (${c.url})` }))]}
      .value=${this._form.command}
      hint=${t('rules.form.aptCacheHint')}
      @lcm-change=${(e: CustomEvent<{ value: string }>) => this.#command(e.detail.value)}
    ></lcm-select>`;
  }

  #dockerScopeField(message: string) {
    return html`<lcm-input
      label=${t('rules.form.dockerScope')}
      maxlength=${LIMITS.name}
      placeholder=${t('rules.form.dockerScopePlaceholder')}
      .value=${this._form.command}
      .error=${message}
      hint=${t('rules.form.dockerScopeHint')}
      @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#command(e.detail.value)}
    ></lcm-input>`;
  }

  #dnsDomainsField(message: string) {
    return html`<lcm-textarea
      label=${t('rules.form.dnsDomains')}
      rows="3"
      maxlength=${LIMITS.script}
      mono
      placeholder=${'intranet.example\ngit.example.org'}
      .value=${this._form.command}
      .error=${message}
      hint=${t('rules.form.dnsDomainsHint')}
      @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#command(e.detail.value)}
    ></lcm-textarea>`;
  }

  #firewallField() {
    return html`<lcm-groups-firewall-editor
      .rows=${this._form.firewall}
      .allowlists=${this._allowlists}
      ?show-errors=${this._attempted}
      @lcm-change=${(e: CustomEvent<{ rows: RuleForm['firewall'] }>) => (this._form = { ...this._form, firewall: e.detail.rows })}
    ></lcm-groups-firewall-editor>`;
  }

  /** Was diese Regel bewirken wird, damit niemand raten muss. */
  #renderImpact() {
    const form = this._form;
    const info = typeInfo(form.type);
    if (!info || !this.group) return nothing;
    const effects = effectsOf([{ type: form.type }]);
    const names = this.group.servers.map((s) => s.name);
    const shown = names.slice(0, 4).join(', ') + (names.length > 4 ? ` +${names.length - 4}` : '');
    const when =
      form.trigger === 'enforce'
        ? t('rules.impact.enforce')
        : t('rules.impact.scheduled', { schedule: this.#triggerLabel(), when: cronText(this.schedules.find((s) => s.id === form.trigger)?.cron_expr ?? '') });
    return html`<div class="impact">
      <h3>${t('rules.impact.title')}</h3>
      <ul>
        <li>${when}</li>
        <li>${names.length > 0 ? t('rules.impact.servers', { count: names.length, names: shown }) : t('rules.impact.noServers')}</li>
        ${effects.reboot ? html`<li class="warn">${t('rules.impact.reboot')}</li>` : nothing}
        ${effects.maybeReboot ? html`<li class="warn">${t('rules.impact.maybeReboot')}</li>` : nothing}
        ${effects.readOnly ? html`<li>${t('rules.impact.readOnly')}</li>` : nothing}
        ${form.type === 'firewall' && form.firewall.length === 0 ? html`<li class="warn">${t('rules.impact.firewallClosed')}</li>` : nothing}
        ${form.type === 'script' ? html`<li class="warn">${t('rules.impact.script')}</li>` : nothing}
      </ul>
    </div>`;
  }

  override render() {
    const errors = validateRuleForm(this._form);
    const editing = this.rule !== null;
    const info = typeInfo(this._form.type);
    const showName = this._attempted && errors.name ? t(errors.name, { max: LIMITS.name }) : '';
    const showTrigger = this._attempted && errors.trigger ? t(errors.trigger) : '';
    return html`<lcm-groups-form-modal
      ?open=${this.open}
      heading=${editing ? t('rules.form.editTitle') : t('rules.form.newTitle')}
      subtitle=${this.group?.name ?? ''}
      size=${info?.field === 'firewall' ? 'xl' : 'm'}
      save-label=${editing ? t('groups.action.save') : t('groups.action.create')}
      ?dirty=${ruleFormChanged(this.#initial, this._form)}
      ?busy=${this._busy}
      error=${this._error}
      @lcm-submit=${this.#save}
    >
      <lcm-input
        label=${t('rules.form.name')}
        maxlength=${LIMITS.name}
        .value=${this._form.name}
        .error=${showName}
        placeholder=${t('rules.form.namePlaceholder')}
        required
        @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#set({ ...this._form, name: e.detail.value })}
      ></lcm-input>
      ${editing
        ? html`<dl class="fact">
            <dt>${t('rules.form.type')}</dt>
            <dd>${info ? t(`${info.key}.label`) : this._form.type}</dd>
            <dt>${t('rules.form.trigger')}</dt>
            <dd>${this.#triggerLabel()}</dd>
          </dl>`
        : html`<lcm-select
              label=${t('rules.form.trigger')}
              width="l"
              .options=${this.#triggerOptions()}
              .value=${triggerValue(this._form.trigger)}
              hint=${showTrigger || t('rules.form.triggerHint')}
              @lcm-change=${(e: CustomEvent<{ value: string }>) => this.#set(withTrigger(this._form, triggerFrom(e.detail.value)))}
            ></lcm-select>
            <lcm-select
              label=${t('rules.form.type')}
              width="l"
              .options=${this.#typeOptions()}
              .value=${this._form.type}
              @lcm-change=${(e: CustomEvent<{ value: string }>) => this.#set(withType(this._form, e.detail.value))}
            ></lcm-select>`}
      ${info ? html`<p class="help">${t(`${info.key}.help`)}</p>` : nothing} ${this.#renderTypeField(errors.command)}
      ${this._form.trigger === 'enforce' ? html`<p class="note">${t('rules.form.enforceNote')}</p>` : nothing} ${this.#renderImpact()}
    </lcm-groups-form-modal>`;
  }
}
customElements.define('lcm-groups-rule-modal', LcmGroupsRuleModal);
