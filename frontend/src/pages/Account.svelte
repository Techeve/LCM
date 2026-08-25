<script>
  // "Mein Konto": eigene Seite außerhalb der Einstellungen, erreichbar über
  // den Usernamen in der Navbar. Der eingeloggte User pflegt hier sein
  // Profil (E-Mail, Name), ändert sein Passwort und richtet die
  // Zwei-Faktor-Authentifizierung ein.
  import { api, ApiError } from '../api';
  import { auth } from '../stores/auth.svelte.js';
  import { i18n } from '../stores/i18n.svelte.js';
  import PasswordConfirm from '../components/PasswordConfirm.svelte';

  const t = (k, p) => i18n.t(k, p);

  let profile = $state({ email: '', firstName: '', lastName: '' });
  let password = $state('');
  let passwordValid = $state(false);
  let currentPassword = $state('');
  let code = $state('');
  let error = $state('');
  let notice = $state('');

  // 2FA-Einrichtung: { secret, provisioning_uri, qr_code } während des Setups.
  let setup = $state(null);
  let setupCode = $state('');

  // Bei aktivem TOTP ist zusätzlich ein 2FA-Code für die Passwortänderung nötig.
  let totpEnabled = $derived(!!auth.user?.totp_enabled);
  let canSubmitPw = $derived(passwordValid && currentPassword.length > 0 && (!totpEnabled || code.length >= 6));

  $effect(() => {
    const u = auth.user;
    if (u) {
      profile = { email: u.email ?? '', firstName: u.first_name ?? '', lastName: u.last_name ?? '' };
    }
  });

  async function saveProfile(event) {
    event.preventDefault();
    error = '';
    notice = '';
    try {
      const updated = await api.users.updateProfile(auth.user.id, profile);
      // Session-Profil aktualisieren, damit die Navbar den neuen Namen zeigt.
      api.client.startSession(localStorage.getItem('lcm.token'), { ...auth.user, ...updated });
      notice = t('account.profileSaved');
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    }
  }

  async function changePassword(event) {
    event.preventDefault();
    error = '';
    notice = '';
    try {
      await api.users.resetPassword(auth.user.id, password, currentPassword, code);
      password = '';
      currentPassword = '';
      code = '';
      // Die Passwortänderung entwertet serverseitig das aktuelle Token: der
      // nächste Request läuft ins 401 und die App meldet automatisch ab.
      notice = t('account.passwordChanged');
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    }
  }

  async function startSetup() {
    error = '';
    try {
      setup = await api.auth.setup2fa();
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    }
  }

  async function enable2fa() {
    error = '';
    try {
      await api.auth.enable2fa(setupCode);
      // Session-Flag aktualisieren.
      api.client.startSession(localStorage.getItem('lcm.token'), { ...auth.user, totp_enabled: true });
      setup = null;
      setupCode = '';
      notice = t('account.twofaEnabled');
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    }
  }

  async function disable2fa() {
    error = '';
    const c = prompt(t('account.disablePrompt'));
    if (!c) return;
    try {
      await api.auth.disable2fa(c);
      api.client.startSession(localStorage.getItem('lcm.token'), { ...auth.user, totp_enabled: false });
      notice = t('account.twofaDisabled');
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    }
  }
</script>

<div class="container">
  <h1 class="h3 mb-4">{t('account.title')}</h1>
  {#if error}<div class="alert alert-danger">{error}</div>{/if}
  {#if notice}<div class="alert alert-success">{notice}</div>{/if}

  <div class="row g-4">
    <div class="col-lg-6">
      <div class="card mb-4">
        <div class="card-body">
          <h3 class="h6">{t('account.profile')}</h3>
          <form onsubmit={saveProfile}>
            <div class="mb-2">
              <label class="form-label" for="acc-email">{t('account.email')}</label>
              <input id="acc-email" type="email" class="form-control" bind:value={profile.email} />
            </div>
            <div class="row g-2 mb-3">
              <div class="col">
                <label class="form-label" for="acc-first">{t('account.firstName')}</label>
                <input id="acc-first" class="form-control" bind:value={profile.firstName} />
              </div>
              <div class="col">
                <label class="form-label" for="acc-last">{t('account.lastName')}</label>
                <input id="acc-last" class="form-control" bind:value={profile.lastName} />
              </div>
            </div>
            <button class="btn btn-primary">{t('account.saveProfile')}</button>
          </form>
        </div>
      </div>

      <div class="card">
        <div class="card-body">
          <h3 class="h6">{t('account.changePassword')}</h3>
          <form onsubmit={changePassword}>
            <div style="max-width: 360px">
              <div class="mb-2">
                <label class="form-label" for="acc-cur">{t('account.currentPassword')}</label>
                <input id="acc-cur" type="password" autocomplete="current-password"
                  class="form-control" bind:value={currentPassword} />
              </div>
              {#if totpEnabled}
                <div class="mb-2">
                  <label class="form-label" for="acc-code">{t('account.twofaCode')}</label>
                  <input id="acc-code" inputmode="numeric" autocomplete="one-time-code"
                    class="form-control" bind:value={code} placeholder={t('account.sixDigit')} />
                </div>
              {/if}
              <PasswordConfirm bind:value={password} bind:valid={passwordValid}
                identity={{ username: auth.user?.username, email: auth.user?.email, firstName: auth.user?.first_name, lastName: auth.user?.last_name }} />
            </div>
            <button class="btn btn-primary" disabled={!canSubmitPw}>{t('account.changePassword')}</button>
          </form>
        </div>
      </div>
    </div>

    <div class="col-lg-6">
      <div class="card">
        <div class="card-body">
          <h3 class="h6">{t('account.twofaTitle')}</h3>
          {#if auth.user?.totp_enabled}
            <p class="text-success">{t('account.twofaActive')}</p>
            <button class="btn btn-outline-danger" onclick={disable2fa}>{t('account.twofaDisable')}</button>
          {:else if setup}
            <p class="small">
              {t('account.twofaScanHint')}
            </p>
            <div class="text-center mb-3">
              <img src={setup.qr_code} alt={t('account.qrAlt')} width="220" height="220" class="border rounded" />
            </div>
            <details class="mb-3">
              <summary class="small text-body-secondary">{t('account.noScanner')}</summary>
              <code class="user-select-all d-block mt-2">{setup.secret}</code>
            </details>
            <div class="input-group" style="max-width: 320px">
              <input class="form-control" placeholder={t('account.sixDigitCode')} bind:value={setupCode} inputmode="numeric" maxlength="6" />
              <button class="btn btn-primary" onclick={enable2fa} disabled={setupCode.length !== 6}>{t('account.activate')}</button>
            </div>
          {:else}
            <p class="text-body-secondary small">
              {t('account.twofaIntro')}
            </p>
            <button class="btn btn-outline-primary" onclick={startSetup}>{t('account.twofaSetup')}</button>
          {/if}
        </div>
      </div>
    </div>
  </div>
</div>
