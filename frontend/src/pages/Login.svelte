<script>
  // Login mit optionalem zweiten Faktor (TOTP). Ist für den User 2FA
  // aktiv, liefert Schritt 1 eine Challenge; Schritt 2 verifiziert den Code.
  import { push } from 'svelte-spa-router';
  import { api, ApiError } from '../api';
  import { i18n } from '../stores/i18n.svelte.js';
  const t = (k, p) => i18n.t(k, p);

  let username = $state('');
  let password = $state('');
  let error = $state('');
  let busy = $state(false);

  // 2FA-Zustand
  let challenge = $state('');
  let code = $state('');

  // Passwort-vergessen-Zustand: die Antwort ist bewusst immer gleich -
  // ob die Adresse existiert, verrät der Server nicht.
  let forgot = $state(false);
  let resetEmail = $state('');
  let resetDone = $state(false);

  async function submitReset(event) {
    event.preventDefault();
    busy = true;
    error = '';
    try {
      await api.auth.requestPasswordReset(resetEmail);
      resetDone = true;
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    } finally {
      busy = false;
    }
  }

  function toggleForgot(on) {
    forgot = on;
    error = '';
    resetDone = false;
  }

  async function submit(event) {
    event.preventDefault();
    busy = true;
    error = '';
    try {
      const res = await api.auth.login(username, password);
      if (res.twofa_required) {
        challenge = res.challenge;
      } else {
        push('/');
      }
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    } finally {
      busy = false;
    }
  }

  async function submitCode(event) {
    event.preventDefault();
    busy = true;
    error = '';
    try {
      await api.auth.loginTotp(challenge, code);
      push('/');
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    } finally {
      busy = false;
    }
  }
</script>

<div class="container" style="max-width: 420px">
  <div class="card shadow-sm">
    <div class="card-body p-4">
      <h1 class="h4 mb-3 text-center">{t('login.title')}</h1>

      {#if forgot}
        {#if resetDone}
          <div class="alert alert-success py-2 small">{t('login.resetDone')}</div>
          <button class="btn btn-outline-secondary w-100" type="button" onclick={() => toggleForgot(false)}>{t('login.backToLogin')}</button>
        {:else}
          <p class="text-body-secondary small text-center">{t('login.resetIntro')}</p>
          <form onsubmit={submitReset}>
            <div class="mb-3">
              <label class="form-label" for="reset-email">{t('login.email')}</label>
              <input id="reset-email" type="email" class="form-control" bind:value={resetEmail} autocomplete="email" required />
            </div>
            {#if error}<div class="alert alert-danger py-2">{error}</div>{/if}
            <button class="btn btn-primary w-100" type="submit" disabled={busy}>{busy ? t('login.sending') : t('login.requestLink')}</button>
            <button class="btn btn-link w-100 btn-sm mt-1" type="button" onclick={() => toggleForgot(false)}>{t('login.backToLogin')}</button>
          </form>
        {/if}
      {:else if !challenge}
        <p class="text-body-secondary small text-center">{t('login.firstStartHint')}</p>
        <form onsubmit={submit}>
          <div class="mb-3">
            <label class="form-label" for="username">{t('login.username')}</label>
            <input id="username" class="form-control" bind:value={username} autocomplete="username" required />
          </div>
          <div class="mb-3">
            <label class="form-label" for="password">{t('login.password')}</label>
            <input id="password" type="password" class="form-control" bind:value={password} autocomplete="current-password" required />
          </div>
          {#if error}<div class="alert alert-danger py-2" data-testid="login-error">{error}</div>{/if}
          <button class="btn btn-primary w-100" type="submit" disabled={busy}>{busy ? t('login.submitting') : t('login.submit')}</button>
          <button class="btn btn-link w-100 btn-sm mt-1" type="button" data-testid="forgot-password"
            onclick={() => toggleForgot(true)}>{t('login.forgot')}</button>
        </form>
      {:else}
        <p class="text-body-secondary small text-center">{t('login.totpHint')}</p>
        <form onsubmit={submitCode}>
          <div class="mb-3">
            <label class="form-label" for="code">{t('login.totpCode')}</label>
            <input id="code" class="form-control text-center" bind:value={code} inputmode="numeric" maxlength="6" autocomplete="one-time-code" required />
          </div>
          {#if error}<div class="alert alert-danger py-2">{error}</div>{/if}
          <button class="btn btn-primary w-100" type="submit" disabled={busy}>{busy ? t('login.verifying') : t('login.confirm')}</button>
        </form>
      {/if}
    </div>
  </div>
</div>
