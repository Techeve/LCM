<script>
  // Zwei Passwortfelder, deren Eingabe übereinstimmen muss, samt
  // Live-Stärkeanzeige. Exponiert das Passwort (value) und ein
  // Gültigkeits-Flag (valid = erfüllt die Policy UND beide Felder gleich).
  //
  // Die Policy-Prüfung hier ist reine Benutzerführung - verbindlich ist
  // ausschließlich der Server (services.EnforcePasswordPolicy). Ein
  // manipulierter Client kommt an ihr nicht vorbei.
  import { i18n } from '../stores/i18n.svelte.js';
  import { checkPassword, PASSWORD_MIN_LENGTH } from '../lib/passwordPolicy.js';
  import PasswordStrength from './PasswordStrength.svelte';

  const t = (k, p) => i18n.t(k, p);
  let {
    value = $bindable(''),
    valid = $bindable(false),
    label = null,
    // identity: Benutzername/Name/E-Mail des betroffenen Kontos - damit die
    // Anzeige (wie der Server) ein Passwort ablehnt, das den eigenen Namen
    // enthält.
    identity = {},
  } = $props();

  let confirm = $state('');
  let lbl = $derived(label ?? t('passwordConfirm.newPassword'));
  let check = $derived(checkPassword(value ?? '', identity));

  $effect(() => {
    valid = check.ok && value === confirm;
  });

  let mismatch = $derived(confirm.length > 0 && value !== confirm);
</script>

<label class="form-label d-block mb-2">
  {lbl} {t('passwordConfirm.minChars', { min: PASSWORD_MIN_LENGTH })}
  <input type="password" class="form-control" bind:value autocomplete="new-password" />
</label>
<PasswordStrength password={value} {identity} showWhenEmpty />
<label class="form-label d-block mb-2 mt-2">
  {t('passwordConfirm.confirmLabel', { label: lbl })}
  <input
    type="password"
    class="form-control {mismatch ? 'is-invalid' : ''}"
    bind:value={confirm}
    autocomplete="new-password"
  />
  {#if mismatch}<div class="invalid-feedback">{t('passwordConfirm.mismatch')}</div>{/if}
</label>
