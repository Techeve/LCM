<script>
  // Gemeinsames Layout der Linux-Benutzer-Seite: Titel, Reiter-Leiste und
  // der Inhalt des jeweiligen Reiters (als Slot).
  import { link, router } from 'svelte-spa-router';
  import { i18n } from '../stores/i18n.svelte.js';
  import { visibleLinuxUsersTabs } from './linuxUsersTabs.js';

  const t = (k, p) => i18n.t(k, p);
  let { children } = $props();

  // Exakter Vergleich: „/linux-users" ist der Anfang der beiden anderen
  // Pfade - mit startsWith wäre der erste Reiter immer mit aktiv.
  function active(path) {
    return router.location === path ? 'active' : '';
  }
</script>

<div class="container">
  <h1 class="h3 mb-3">{t('linuxUsers.title')}</h1>
  <ul class="nav nav-tabs mb-4" data-testid="linux-users-tabs">
    {#each visibleLinuxUsersTabs() as tab (tab.path)}
      <li class="nav-item">
        <a class="nav-link {active(tab.path)}" href={tab.path} use:link>{t(tab.labelKey)}</a>
      </li>
    {/each}
  </ul>
  {@render children()}
</div>
