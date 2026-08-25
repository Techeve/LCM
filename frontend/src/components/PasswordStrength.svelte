<script>
  // Live-Stärkeanzeige für Passwortfelder: Balken + konkrete Begründung,
  // was noch fehlt. Rein zur Benutzerführung - die verbindliche Prüfung
  // macht der Server (siehe lib/passwordPolicy.js).
  import { i18n } from '../stores/i18n.svelte.js';
  import { checkPassword } from '../lib/passwordPolicy.js';

  const t = (k, p) => i18n.t(k, p);

  let {
    password = '',
    identity = {},
    // showWhenEmpty: Regelhinweis auch bei leerem Feld anzeigen (z.B. im
    // Anlege-Formular, damit die Anforderungen vorab sichtbar sind).
    showWhenEmpty = false,
  } = $props();

  let result = $derived(checkPassword(password ?? '', identity));
  let empty = $derived(!(password ?? '').length);

  const LABELS = ['veryWeak', 'weak', 'fair', 'strong', 'veryStrong'];
  const CLASSES = ['bg-danger', 'bg-danger', 'bg-warning', 'bg-success', 'bg-success'];
</script>

{#if empty}
  {#if showWhenEmpty}
    <div class="form-text" data-testid="pw-hint">{t('password.requirements')}</div>
  {/if}
{:else}
  <div class="mt-1" data-testid="pw-strength">
    <div class="progress" style="height: 6px" role="progressbar"
      aria-label={t('password.strengthLabel')}
      aria-valuenow={result.score} aria-valuemin="0" aria-valuemax="4">
      <div class="progress-bar {CLASSES[result.score]}"
        style="width: {((result.score + 1) / 5) * 100}%"></div>
    </div>
    <div class="small mt-1" class:text-danger={!result.ok} class:text-success={result.ok}>
      {t('password.strength.' + LABELS[result.score])}
    </div>
    {#if !result.ok}
      <ul class="small text-body-secondary mb-0 ps-3" data-testid="pw-problems">
        {#each result.codes as code (code)}
          <li>{t('password.problem.' + code)}</li>
        {/each}
      </ul>
    {/if}
  </div>
{/if}
