<script>
  // Gemeinsames Layout aller Einstellungs-Unterseiten: Titel + Unter-Nav
  // (links) und der jeweilige Seiteninhalt (rechts, als Slot).
  import SettingsNav from './SettingsNav.svelte';
  import { i18n } from '../stores/i18n.svelte.js';
  const t = (k, p) => i18n.t(k, p);
  let { title, children } = $props();
</script>

<div class="container">
  <h1 class="h3 mb-4">{t('settings.title')}</h1>
  <div class="row g-4">
    <div class="col-lg-3">
      <SettingsNav />
    </div>
    <div class="col-lg-9">
      <h2 class="h5 mb-3">{title}</h2>
      {@render children()}
    </div>
  </div>
</div>
