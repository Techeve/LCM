<script>
  import { link } from 'svelte-spa-router';
  import { i18n } from '../stores/i18n.svelte.js';
  const t = (k, p) => i18n.t(k, p);
</script>

<div class="container text-center py-5">
  <h1 class="display-4">404</h1>
  <p class="text-body-secondary">{t('notFound.message')}</p>
  <a class="btn btn-primary" href="/" use:link>{t('notFound.back')}</a>
</div>
