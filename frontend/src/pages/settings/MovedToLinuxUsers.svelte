<script>
  // Berechtigungsprofile und Regelbausteine liegen seit 1.27 nicht mehr in den
  // Einstellungen, sondern als Reiter auf der Linux-Benutzer-Seite. Alte
  // Lesezeichen und Verweise landen hier und werden weitergereicht, statt auf
  // einer „Seite nicht gefunden" zu enden.
  import { replace, router } from 'svelte-spa-router';

  const ziel = router.location.endsWith('/profile-blocks')
    ? '/linux-users/profile-blocks'
    : '/linux-users/profiles';

  $effect(() => {
    replace(ziel);
  });
</script>
