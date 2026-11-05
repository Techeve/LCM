import { LitElement, css, html } from 'lit';
import '../../components/lcm-input';
import { baseStyles, emit } from '../../components/shared';
import { toast } from '../../components/lcm-toaster';
import { groupsApi, type Group } from '../../api/groups';
import { I18nController } from '../../lib/controllers';
import { ApiError } from '../../lib/api';
import { errorText, t } from '../../lib/i18n';
import { LIMITS } from '../../lib/validate';
import './form-modal';
import '../../components/lcm-textarea';
import { emptyGroupForm, groupBody, groupFormOf, isFormValid, sameForm, validateGroupForm, type GroupForm } from './forms';

/**
 * <lcm-groups-group-modal open .group=${…}> – Gruppe anlegen (ohne `group`) oder bearbeiten.
 * Meldet `lcm-saved` mit { id } und `lcm-close`.
 */
export class LcmGroupsGroupModal extends LitElement {
  static override properties = {
    open: { type: Boolean },
    group: { attribute: false },
    _form: { state: true },
    _busy: { state: true },
    _error: { state: true },
    _nameError: { state: true },
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
  declare group: Group | null;
  declare _form: GroupForm;
  declare _busy: boolean;
  declare _error: string;
  /** Der Name ist schon vergeben: die Meldung des Servers, am Namensfeld. */
  declare _nameError: string;
  declare _attempted: boolean;
  #initial: GroupForm = emptyGroupForm();
  #wasOpen = false;

  constructor() {
    super();
    this.open = false;
    this.group = null;
    this._form = emptyGroupForm();
    this._busy = false;
    this._error = '';
    this._nameError = '';
    this._attempted = false;
    new I18nController(this);
  }

  override willUpdate(): void {
    if (this.open && !this.#wasOpen) {
      this.#initial = this.group ? groupFormOf(this.group) : emptyGroupForm();
      this._form = this.#initial;
      this._error = '';
      this._nameError = '';
      this._attempted = false;
    }
    this.#wasOpen = this.open;
  }

  #set(patch: Partial<GroupForm>): void {
    this._form = { ...this._form, ...patch };
  }

  #message(key: string | undefined, max: number): string {
    return this._attempted && key ? t(key, { max }) : '';
  }

  async #save(): Promise<void> {
    this._attempted = true;
    if (!isFormValid(validateGroupForm(this._form))) return;
    this._busy = true;
    this._error = '';
    this._nameError = '';
    try {
      const body = groupBody(this._form);
      if (this.group) {
        await groupsApi.update(this.group.id, body);
        toast({ tone: 'ok', title: t('groups.notice.saved', { name: body.name }) });
        emit(this, 'lcm-saved', { id: this.group.id });
      } else {
        const created = await groupsApi.create(body);
        toast({ tone: 'ok', title: t('groups.notice.created', { name: body.name }) });
        emit(this, 'lcm-saved', { id: created.id });
      }
      this._form = this.#initial;
    } catch (error) {
      if (error instanceof ApiError && error.code === 'group.name_taken') this._nameError = errorText(error);
      else this._error = errorText(error);
    } finally {
      this._busy = false;
    }
  }

  override render() {
    const errors = validateGroupForm(this._form);
    const editing = this.group !== null;
    return html`<lcm-groups-form-modal
      ?open=${this.open}
      heading=${editing ? t('groups.form.editTitle') : t('groups.form.newTitle')}
      subtitle=${editing ? (this.group?.name ?? '') : t('groups.form.newSubtitle')}
      save-label=${editing ? t('groups.action.save') : t('groups.action.create')}
      ?dirty=${!sameForm(this._form, this.#initial)}
      ?busy=${this._busy}
      error=${this._error}
      @lcm-submit=${this.#save}
    >
      <lcm-input
        label=${t('groups.form.name')}
        width="full"
        maxlength=${LIMITS.name}
        .value=${this._form.name}
        .error=${this.#message(errors.name, LIMITS.name) || this._nameError}
        placeholder=${t('groups.form.namePlaceholder')}
        required
        @lcm-input=${(e: CustomEvent<{ value: string }>) => {
          this._nameError = '';
          this.#set({ name: e.detail.value });
        }}
      ></lcm-input>
      <lcm-textarea
        label=${t('groups.form.description')}
        rows="3"
        maxlength=${LIMITS.description}
        .value=${this._form.description}
        .error=${this.#message(errors.description, LIMITS.description)}
        @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#set({ description: e.detail.value })}
      ></lcm-textarea>
      <lcm-input
        label=${t('groups.form.priority')}
        width="s"
        digits
        maxlength="4"
        mono
        .value=${this._form.priority}
        .error=${this.#message(errors.priority, 0)}
        hint=${t('groups.form.priorityHint')}
        @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#set({ priority: e.detail.value })}
      ></lcm-input>
    </lcm-groups-form-modal>`;
  }
}
customElements.define('lcm-groups-group-modal', LcmGroupsGroupModal);
