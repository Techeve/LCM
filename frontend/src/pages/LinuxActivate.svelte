<script>
  // Öffentliche Self-Service-Seite: Ein Linux-Benutzer löst seinen
  // Aktivierungslink ein und setzt Passwort und/oder SSH-Public-Key.
  // Erreichbar ohne LCM-Login (Token aus der URL).
  import { api, ApiError } from '../api';
  import { i18n } from '../stores/i18n.svelte.js';
  import PasswordConfirm from '../components/PasswordConfirm.svelte';
  import SshKeyGuide from '../components/SshKeyGuide.svelte';
  import { downloadText } from '../lib/download.js';

  const t = (k, p) => i18n.t(k, p);

  // Token aus dem Hash-Query lesen (#/linux-aktivierung?token=…).
  function tokenFromHash() {
    const q = location.hash.split('?')[1] ?? '';
    return new URLSearchParams(q).get('token') ?? '';
  }

  let token = $state(tokenFromHash());
  let password = $state('');
  let passwordValid = $state(false);
  let keyName = $state(t('linuxActivate.defaultKeyName'));
  let publicKey = $state('');
  // 'none' = kein Key, 'paste' = eigenen Public Key einreichen,
  // 'generate' = LCM erzeugt das Schlüsselpaar (privater Key als Download).
  let keyMode = $state('none');
  let error = $state('');
  let done = $state(false);
  let result = $state(null); // { username, private_key? }

  async function submit(event) {
    event.preventDefault();
    error = '';
    try {
      result = await api.linuxUsers.consumeActivation({
        token,
        password,
        keyName,
        publicKey: keyMode === 'paste' ? publicKey : '',
        generateKey: keyMode === 'generate',
      });
      done = true;
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    }
  }

  let canSubmit = $derived(
    token &&
      ((keyMode === 'paste' && publicKey.trim()) ||
        keyMode === 'generate' ||
        (password && passwordValid)),
  );
</script>

<div class="container py-4" style="max-width: 560px">
  <h1 class="h3 mb-3">{t('linuxActivate.title')}</h1>

  {#if done}
    <div class="alert alert-success">
      {t('linuxActivate.doneMsg')}
    </div>
    {#if result?.private_key}
      <div class="card border-warning mb-3">
        <div class="card-body">
          <h2 class="h6">{t('linuxActivate.privKeyTitle')}</h2>
          <p class="small mb-2">
            {t('linuxActivate.privKeyHintA')}<strong>{t('linuxActivate.privKeyHintBold')}</strong>{t('linuxActivate.privKeyHintB')}
          </p>
          <div class="d-flex gap-2 mb-2">
            <button class="btn btn-primary" onclick={() => downloadText(`id_ed25519_${result.username}`, result.private_key)}>
              {t('linuxActivate.downloadPrivKey')}
            </button>
            <button class="btn btn-outline-secondary" onclick={() => navigator.clipboard?.writeText(result.private_key)}>{t('linuxActivate.copy')}</button>
          </div>
          <SshKeyGuide username={result.username} />
        </div>
      </div>
    {/if}
  {:else}
    <p class="text-body-secondary">
      {t('linuxActivate.intro')}
    </p>
    {#if error}<div class="alert alert-danger">{error}</div>{/if}
    {#if !token}<div class="alert alert-warning">{t('linuxActivate.noToken')}</div>{/if}

    <form onsubmit={submit}>
      <div class="card mb-3">
        <div class="card-body">
          <h2 class="h6">{t('linuxActivate.passwordOptional')}</h2>
          <PasswordConfirm bind:value={password} bind:valid={passwordValid} label={t('common.password')} />
        </div>
      </div>
      <div class="card mb-3">
        <div class="card-body">
          <h2 class="h6">{t('linuxActivate.sshKeyOptional')}</h2>
          <div class="form-check">
            <input class="form-check-input" type="radio" id="km-none" value="none" bind:group={keyMode} />
            <label class="form-check-label" for="km-none">{t('linuxActivate.keyNone')}</label>
          </div>
          <div class="form-check">
            <input class="form-check-input" type="radio" id="km-paste" value="paste" bind:group={keyMode} />
            <label class="form-check-label" for="km-paste">{t('linuxActivate.keyPaste')}</label>
          </div>
          <div class="form-check mb-2">
            <input class="form-check-input" type="radio" id="km-generate" value="generate" bind:group={keyMode} />
            <label class="form-check-label" for="km-generate">
              {t('linuxActivate.keyGenerate')}
            </label>
          </div>
          {#if keyMode !== 'none'}
            <div class="mb-2">
              <label class="form-label" for="act-keyname">{t('linuxActivate.keyLabel')}</label>
              <input id="act-keyname" class="form-control" bind:value={keyName} />
            </div>
          {/if}
          {#if keyMode === 'paste'}
            <div class="mb-2">
              <label class="form-label" for="act-key">{t('linuxActivate.publicKey')}</label>
              <textarea id="act-key" class="form-control" rows="3" bind:value={publicKey}
                placeholder="ssh-ed25519 AAAA…"></textarea>
            </div>
          {:else if keyMode === 'generate'}
            <div class="alert alert-info py-2 small mb-0">
              {t('linuxActivate.generateInfoA')}<strong>{t('linuxActivate.generateInfoBold')}</strong>{t('linuxActivate.generateInfoB')}
            </div>
          {/if}
        </div>
      </div>
      <button class="btn btn-primary" type="submit" disabled={!canSubmit}>{t('linuxActivate.submit')}</button>
    </form>
  {/if}
</div>
