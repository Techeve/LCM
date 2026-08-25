<script>
  // /settings ohne Unterseite: leitet auf die erste per RBAC sichtbare
  // Einstellungs-Seite weiter. „Mein Konto" liegt nicht mehr hier -
  // es ist über den Usernamen in der Navbar erreichbar.
  import { replace } from 'svelte-spa-router';
  import { i18n } from '../../stores/i18n.svelte.js';
  import { visibleSettingsItems } from '../../components/settingsNavItems.js';

  const t = (k, p) => i18n.t(k, p);
  const items = visibleSettingsItems();
  $effect(() => {
    if (items.length) replace(items[0].path);
  });
</script>

{#if !items.length}
  <div class="container">
    <p class="text-body-secondary">{t('settings.noPages')}</p>
  </div>
{/if}
