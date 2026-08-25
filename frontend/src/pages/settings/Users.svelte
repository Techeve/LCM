<script>
  // LCM-Benutzerverwaltung (Login-User: admin/manager). Anlegen und
  // Bearbeiten laufen über Modal-Overlays; der Erstellen-Button steht oben.
  import { api, ApiError } from '../../api';
  import { auth } from '../../stores/auth.svelte.js';
  import { i18n } from '../../stores/i18n.svelte.js';
  import SettingsLayout from '../../components/SettingsLayout.svelte';
  import Modal from '../../components/Modal.svelte';
  import PasswordConfirm from '../../components/PasswordConfirm.svelte';

  const t = (k, p) => i18n.t(k, p);

  let users = $state([]);
  let roles = $state([]);
  let error = $state('');
  let notice = $state('');

  let showCreate = $state(false);
  let editing = $state(null); // User im Bearbeiten-Modal

  let form = $state(newForm());
  let createPw = $state('');
  let createPwValid = $state(false);
  let editPw = $state('');
  let editPwValid = $state(false);

  function newForm() {
    return { username: '', email: '', firstName: '', lastName: '', roles: ['manager'] };
  }

  async function load() {
    try {
      [users, roles] = await Promise.all([api.users.getAll(), api.users.getRoles()]);
      error = '';
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    }
  }

  function openCreate() {
    form = newForm();
    createPw = '';
    showCreate = true;
  }

  async function createUser() {
    error = '';
    notice = '';
    try {
      await api.users.create({ ...form, password: createPw });
      notice = t('settings.users.notices.created', { name: form.username });
      showCreate = false;
      await load();
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    }
  }

  function startEdit(user) {
    editing = {
      id: user.id,
      username: user.username,
      email: user.email ?? '',
      firstName: user.first_name ?? '',
      lastName: user.last_name ?? '',
      roles: user.roles.map((r) => r.name),
    };
    editPw = '';
  }

  async function saveEdit() {
    error = '';
    notice = '';
    try {
      await api.users.updateProfile(editing.id, editing);
      if (auth.can('roles:write')) await api.users.updateRoles(editing.id, editing.roles);
      if (editPw) await api.users.resetPassword(editing.id, editPw);
      notice = t('settings.users.notices.saved', { name: editing.username });
      editing = null;
      await load();
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    }
  }

  async function removeUser(user) {
    if (!confirm(t('settings.users.deleteConfirm', { name: user.username }))) return;
    error = '';
    try {
      await api.users.remove(user.id);
      await load();
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    }
  }

  // R2-036: Konto sperren/entsperren, ohne es zu löschen - der Normalfall
  // beim Offboarding.
  async function toggleActive(user) {
    const next = !user.active;
    if (!next && !confirm(t('settings.users.blockConfirm', { name: user.username }))) return;
    error = '';
    try {
      await api.users.setActive(user.id, next);
      await load();
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    }
  }

  // Einladung: Aktivierungslink erzeugen (48h gültig) und - wenn der User
  // eine E-Mail-Adresse hat - direkt über den Standard-E-Mail-Versand
  // verschicken. Der Link wird zusätzlich zum Kopieren angezeigt (das
  // Token ist nur in diesem Moment sichtbar).
  let invite = $state(null); // { username, url, mailed, mailError }
  let inviteCopied = $state(false);

  async function inviteUser(user) {
    error = '';
    notice = '';
    try {
      const res = await api.users.generateActivationLink(user.id, { sendEmail: !!user.email });
      invite = {
        username: user.username,
        url: `${location.origin}${location.pathname}#/aktivierung?token=${res.token}`,
        mailed: !!res.mailed,
        mailError: res.mail_error || '',
      };
      inviteCopied = false;
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    }
  }

  async function copyInvite() {
    try {
      await navigator.clipboard.writeText(invite.url);
      inviteCopied = true;
      setTimeout(() => (inviteCopied = false), 2000);
    } catch {
      // Clipboard-API nicht verfügbar (z.B. ohne HTTPS) - still ignorieren.
    }
  }

  function toggleRole(list, role) {
    return list.includes(role) ? list.filter((r) => r !== role) : [...list, role];
  }

  $effect(() => {
    load();
  });
</script>

<SettingsLayout title={t('settings.users.title')}>
  {#if error}<div class="alert alert-danger" data-testid="users-error">{error}</div>{/if}
  {#if notice}<div class="alert alert-success py-2">{notice}</div>{/if}

  {#if auth.can('users:write')}
    <div class="mb-3">
      <button class="btn btn-primary" onclick={openCreate}>{t('settings.users.addUser')}</button>
    </div>
  {/if}

  <div class="table-responsive">
    <table class="table table-hover align-middle">
      <thead><tr><th>{t('settings.users.colUser')}</th><th>{t('settings.users.colRoles')}</th><th></th></tr></thead>
      <tbody>
        {#each users as user (user.id)}
          <tr data-testid="user-row">
            <td>
              {user.username}
              {#if user.is_system}<span class="badge text-bg-secondary ms-1">{t('settings.users.system')}</span>{/if}
              {#if user.totp_enabled}<span class="badge text-bg-success ms-1" title={t('settings.users.twofaTitle')}>2FA</span>{/if}
              {#if !user.active}<span class="badge text-bg-danger ms-1" data-testid="user-blocked">{t('settings.users.blocked')}</span>{/if}
              <div class="small text-body-secondary">
                {[user.first_name, user.last_name].filter(Boolean).join(' ') || '-'}
                {#if user.email}· {user.email}{/if}
              </div>
            </td>
            <td>
              {#each user.roles as role (role.id)}
                <span class="badge border text-body-secondary me-1">{role.name}</span>
              {/each}
            </td>
            <td class="text-end">
              {#if !user.is_system && (auth.can('users:write') || user.id === auth.user.id)}
                <button class="btn btn-outline-primary btn-sm" onclick={() => startEdit(user)}>{t('common.edit')}</button>
              {/if}
              {#if auth.can('users:write') && !user.is_system}
                <button class="btn btn-outline-secondary btn-sm" title={t('settings.users.inviteBtnTitle')}
                  onclick={() => inviteUser(user)}>{t('settings.users.inviteBtn')}</button>
              {/if}
              {#if auth.can('users:write') && !user.is_system && user.id !== auth.user.id}
                <button class="btn btn-outline-warning btn-sm" data-testid="user-toggle-active" onclick={() => toggleActive(user)}>
                  {user.active ? t('settings.users.block') : t('settings.users.unblock')}
                </button>
              {/if}
              {#if auth.can('users:write') && !user.is_system && user.username !== 'admin'}
                <button class="btn btn-outline-danger btn-sm" onclick={() => removeUser(user)}>{t('common.delete')}</button>
              {/if}
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
</SettingsLayout>

<!-- Erstellen-Modal -->
<Modal title={t('settings.users.createTitle')} bind:open={showCreate}>
  <div class="row g-2 mb-2">
    <div class="col-md-6">
      <label class="form-label" for="new-username">{t('settings.users.username')}</label>
      <input id="new-username" class="form-control" bind:value={form.username} required />
    </div>
    <div class="col-md-6">
      <label class="form-label" for="new-email">{t('settings.users.email')}</label>
      <input id="new-email" type="email" class="form-control" bind:value={form.email} />
    </div>
  </div>
  <div class="row g-2 mb-2">
    <div class="col"><input class="form-control" placeholder={t('settings.users.firstName')} bind:value={form.firstName} /></div>
    <div class="col"><input class="form-control" placeholder={t('settings.users.lastName')} bind:value={form.lastName} /></div>
  </div>
  <PasswordConfirm bind:value={createPw} bind:valid={createPwValid} label={t('common.password')}
    identity={{ username: form.username, email: form.email, firstName: form.firstName, lastName: form.lastName }} />
  <fieldset class="mb-3">
    <legend class="form-label fs-6">{t('settings.users.roles')}</legend>
    {#each roles as role (role.id)}
      <div class="form-check form-check-inline">
        <input class="form-check-input" type="checkbox" id="crole-{role.name}"
          checked={form.roles.includes(role.name)}
          onchange={() => (form.roles = toggleRole(form.roles, role.name))} />
        <label class="form-check-label" for="crole-{role.name}">{role.name}</label>
      </div>
    {/each}
  </fieldset>
  <div class="text-end">
    <button class="btn btn-secondary" onclick={() => (showCreate = false)}>{t('common.cancel')}</button>
    <button class="btn btn-primary" onclick={createUser} disabled={!form.username || !createPwValid}>{t('settings.users.createBtn')}</button>
  </div>
</Modal>

<!-- Bearbeiten-Modal -->
<Modal title={editing ? t('settings.users.editTitle', { name: editing.username }) : ''} bind:open={() => editing !== null, (v) => { if (!v) editing = null; }}>
  {#if editing}
    <div class="row g-2 mb-2">
      <div class="col-md-6">
        <label class="form-label" for="edit-email">{t('settings.users.email')}</label>
        <input id="edit-email" type="email" class="form-control" bind:value={editing.email} />
      </div>
      <div class="col-md-3">
        <label class="form-label" for="edit-first">{t('settings.users.firstName')}</label>
        <input id="edit-first" class="form-control" bind:value={editing.firstName} />
      </div>
      <div class="col-md-3">
        <label class="form-label" for="edit-last">{t('settings.users.lastName')}</label>
        <input id="edit-last" class="form-control" bind:value={editing.lastName} />
      </div>
    </div>
    {#if auth.can('roles:write')}
      <fieldset class="mb-3">
        <legend class="form-label fs-6">{t('settings.users.roles')}</legend>
        {#each roles as role (role.id)}
          <div class="form-check form-check-inline">
            <input class="form-check-input" type="checkbox" id="erole-{role.name}"
              checked={editing.roles.includes(role.name)}
              onchange={() => (editing.roles = toggleRole(editing.roles, role.name))} />
            <label class="form-check-label" for="erole-{role.name}">{role.name}</label>
          </div>
        {/each}
      </fieldset>
    {/if}
    <hr />
    <p class="small text-body-secondary mb-2">{t('settings.users.changePwHint')}</p>
    <PasswordConfirm bind:value={editPw} bind:valid={editPwValid}
      identity={{ username: editing.username, email: editing.email, firstName: editing.firstName, lastName: editing.lastName }} />
    <div class="text-end">
      <button class="btn btn-secondary" onclick={() => (editing = null)}>{t('common.cancel')}</button>
      <button class="btn btn-primary" onclick={saveEdit} disabled={editPw.length > 0 && !editPwValid}>{t('common.save')}</button>
    </div>
  {/if}
</Modal>

<!-- Einladungs-Modal: zeigt den frisch erzeugten Aktivierungslink an -->
<Modal title={invite ? t('settings.users.inviteTitle', { name: invite.username }) : ''} bind:open={() => invite !== null, (v) => { if (!v) invite = null; }}>
  {#if invite}
    {#if invite.mailed}
      <div class="alert alert-success py-2 small">
        {t('settings.users.inviteMailed')}
      </div>
    {:else if invite.mailError}
      <div class="alert alert-warning py-2 small">
        {t('settings.users.inviteMailErr', { err: invite.mailError })}
      </div>
    {/if}
    <p class="small text-body-secondary mb-2">
      {t('settings.users.inviteHint')}
    </p>
    <div class="input-group input-group-sm">
      <input class="form-control font-monospace" style="font-size: .8rem" readonly value={invite.url} />
      <button type="button" class="btn btn-outline-secondary" onclick={copyInvite}>{inviteCopied ? t('settings.users.copied') : t('common.copy')}</button>
    </div>
    <div class="text-end mt-3">
      <button class="btn btn-secondary" onclick={() => (invite = null)}>{t('modal.close')}</button>
    </div>
  {/if}
</Modal>
