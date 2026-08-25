<script>
  // Öffentliche Self-Service-Seite für LCM-Benutzer: löst einen
  // Aktivierungs- bzw. Passwort-Reset-Link ein und setzt das (neue)
  // Passwort. Erreichbar ohne Login (Token aus der URL) - die Links kommen
  // per Einladungs- oder Passwort-Reset-Mail (#/aktivierung?token=…).
  import { link } from 'svelte-spa-router';
  import { api, ApiError } from '../api';
  import { i18n } from '../stores/i18n.svelte.js';
  import PasswordConfirm from '../components/PasswordConfirm.svelte';

  const t = (k, p) => i18n.t(k, p);

  // Token aus dem Hash-Query lesen (#/aktivierung?token=…).
  function tokenFromHash() {
    const q = location.hash.split('?')[1] ?? '';
    return new URLSearchParams(q).get('token') ?? '';
  }

  let token = $state(tokenFromHash());
  let password = $state('');
  let passwordValid = $state(false);
  let error = $state('');
  let busy = $state(false);
  let done = $state(false);
  let username = $state('');

  async function submit(event) {
    event.preventDefault();
    error = '';
    busy = true;
    try {
      const res = await api.users.consumeActivationLink(token, password);
      username = res.username ?? '';
      done = true;
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    } finally {
      busy = false;
    }
  }
</script>

<div class="container" style="max-width: 460px">
  <div class="card shadow-sm">
    <div class="card-body p-4">
      <h1 class="h4 mb-3 text-center">{t('activate.title')}</h1>

      {#if done}
        <div class="alert alert-success">
          {t('activate.saved')}{#if username}&nbsp;{t('activate.savedAsA')}<strong>{username}</strong>{t('activate.savedAsB')}{/if}.
        </div>
        <a class="btn btn-primary w-100" href="/login" use:link>{t('activate.toLogin')}</a>
      {:else if !token}
        <div class="alert alert-warning small">
          {t('activate.noToken')}
        </div>
      {:else}
        <p class="text-body-secondary small text-center">
          {t('activate.intro')}
        </p>
        <form onsubmit={submit}>
          <PasswordConfirm bind:value={password} bind:valid={passwordValid} />
          {#if error}<div class="alert alert-danger py-2 mt-2">{error}</div>{/if}
          <button class="btn btn-primary w-100 mt-2" type="submit" disabled={busy || !passwordValid}>
            {busy ? t('activate.saving') : t('activate.savePassword')}
          </button>
        </form>
      {/if}
    </div>
  </div>
</div>
