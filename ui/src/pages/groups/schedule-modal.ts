import { LitElement, css, html } from 'lit';
import '../../components/lcm-cron';
import '../../components/lcm-input';
import type { CronLabels, CronPreset } from '../../components/lcm-cron';
import { baseStyles, emit } from '../../components/shared';
import { toast } from '../../components/lcm-toaster';
import { groupsApi, type Schedule } from '../../api/groups';
import { I18nController } from '../../lib/controllers';
import { errorText, i18n, t } from '../../lib/i18n';
import { LIMITS } from '../../lib/validate';
import { cronText } from './cron-describe';
import './form-modal';
import { emptyScheduleForm, isFormValid, sameForm, scheduleBody, scheduleFormOf, validateScheduleForm, type ScheduleForm } from './forms';

function cronLabels(): CronLabels {
  return {
    presets: t('cron.presets'),
    fields: [t('cron.field.minute'), t('cron.field.hour'), t('cron.field.day'), t('cron.field.month'), t('cron.field.weekday')],
    expression: t('cron.expression'),
    next: t('cron.next'),
    invalid: t('cron.invalid'),
  };
}

function cronPresets(): CronPreset[] {
  return [
    { label: t('cron.preset.quarter'), value: '*/15 * * * *' },
    { label: t('cron.preset.hourly'), value: '0 * * * *' },
    { label: t('cron.preset.nightly'), value: '0 3 * * *' },
    { label: t('cron.preset.workdays'), value: '0 2 * * 1-5' },
    { label: t('cron.preset.weekly'), value: '30 3 * * 0' },
    { label: t('cron.preset.monthly'), value: '0 4 1 * *' },
  ];
}

/**
 * <lcm-groups-schedule-modal open group-id="3" .schedule=${…}> – Zeitplan anlegen (ohne `schedule`) oder bearbeiten.
 * Meldet `lcm-saved` und `lcm-close`.
 */
export class LcmGroupsScheduleModal extends LitElement {
  static override properties = {
    open: { type: Boolean },
    groupId: { type: Number, attribute: 'group-id' },
    schedule: { attribute: false },
    _form: { state: true },
    _busy: { state: true },
    _error: { state: true },
    _attempted: { state: true },
  };
  static override styles = [
    baseStyles,
    css`
      :host {
        display: contents;
      }
    `,
  ];

  declare open: boolean;
  declare groupId: number;
  declare schedule: Schedule | null;
  declare _form: ScheduleForm;
  declare _busy: boolean;
  declare _error: string;
  declare _attempted: boolean;
  #initial: ScheduleForm = emptyScheduleForm();
  #wasOpen = false;

  constructor() {
    super();
    this.open = false;
    this.groupId = 0;
    this.schedule = null;
    this._form = emptyScheduleForm();
    this._busy = false;
    this._error = '';
    this._attempted = false;
    new I18nController(this);
  }

  override willUpdate(): void {
    if (this.open && !this.#wasOpen) {
      this.#initial = this.schedule ? scheduleFormOf(this.schedule) : emptyScheduleForm();
      this._form = this.#initial;
      this._error = '';
      this._attempted = false;
    }
    this.#wasOpen = this.open;
  }

  #set(patch: Partial<ScheduleForm>): void {
    this._form = { ...this._form, ...patch };
  }

  #message(key: string | undefined): string {
    return this._attempted && key ? t(key, { max: LIMITS.name }) : '';
  }

  async #save(): Promise<void> {
    this._attempted = true;
    if (!isFormValid(validateScheduleForm(this._form))) return;
    this._busy = true;
    this._error = '';
    try {
      const body = scheduleBody(this._form);
      if (this.schedule) await groupsApi.updateSchedule(this.schedule.id, body);
      else await groupsApi.defineSchedule(this.groupId, body);
      toast({ tone: 'ok', title: t(this.schedule ? 'schedules.notice.saved' : 'schedules.notice.created', { name: body.name }) });
      this._form = this.#initial;
      emit(this, 'lcm-saved');
    } catch (error) {
      this._error = errorText(error);
    } finally {
      this._busy = false;
    }
  }

  override render() {
    const errors = validateScheduleForm(this._form);
    const editing = this.schedule !== null;
    return html`<lcm-groups-form-modal
      ?open=${this.open}
      heading=${editing ? t('schedules.form.editTitle') : t('schedules.form.newTitle')}
      subtitle=${editing ? (this.schedule?.name ?? '') : ''}
      save-label=${editing ? t('groups.action.save') : t('groups.action.create')}
      ?dirty=${!sameForm(this._form, this.#initial)}
      ?busy=${this._busy}
      error=${this._error}
      @lcm-submit=${this.#save}
    >
      <lcm-input
        label=${t('schedules.form.name')}
        maxlength=${LIMITS.name}
        .value=${this._form.name}
        .error=${this.#message(errors.name)}
        placeholder=${t('schedules.form.namePlaceholder')}
        required
        @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#set({ name: e.detail.value })}
      ></lcm-input>
      <lcm-cron
        .value=${this._form.cron_expr}
        .presets=${cronPresets()}
        .labels=${cronLabels()}
        .description=${cronText(this._form.cron_expr)}
        locale=${i18n.language}
        @lcm-change=${(e: CustomEvent<{ value: string }>) => this.#set({ cron_expr: e.detail.value })}
      ></lcm-cron>
      <lcm-input
        label=${t('schedules.form.spread')}
        width="s"
        digits
        maxlength="4"
        mono
        .value=${this._form.spread_minutes}
        .error=${this.#message(errors.spread)}
        hint=${t('schedules.form.spreadHint')}
        @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#set({ spread_minutes: e.detail.value })}
      ></lcm-input>
    </lcm-groups-form-modal>`;
  }
}
customElements.define('lcm-groups-schedule-modal', LcmGroupsScheduleModal);
