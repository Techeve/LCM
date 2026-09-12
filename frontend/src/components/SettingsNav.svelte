<script>
  // Seitliche Unter-Navigation der Einstellungen. Punkte werden per RBAC
  // ein-/ausgeblendet (gemeinsame Definition in settingsNavItems.js).
  import { link, router } from 'svelte-spa-router';
  import { i18n } from '../stores/i18n.svelte.js';
  import { visibleSettingsItems } from './settingsNavItems.js';

  const t = (k, p) => i18n.t(k, p);

  function active(path) {
    return router.location === path ? 'active' : '';
  }
</script>

<nav class="list-group" aria-label={t('settings.navLabel')} data-arrow-nav="vertical">
  {#each visibleSettingsItems() as item (item.path)}
    <a class="list-group-item list-group-item-action {active(item.path)}" href={item.path} use:link>
      {t(item.labelKey)}
    </a>
  {/each}
</nav>
