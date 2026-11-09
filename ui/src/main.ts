import './styles/index.css';
import './shell/lcm-app';
import { auth } from './lib/auth';
import { i18n } from './lib/i18n';
import { printConsoleWarning } from './lib/console-warning';
import { connectLiveData } from './lib/live';
import { remediationRedirect } from './lib/remediation';
import { router } from './lib/router';
import { registerServiceWorker } from './lib/sw-register';
import { initTheme } from './lib/theme';
import { versionWatcher } from './lib/version';
import { routes } from './routes';

async function boot(): Promise<void> {
  initTheme();
  await i18n.init();
  if (import.meta.env.PROD) printConsoleWarning();
  connectLiveData();
  versionWatcher.start();
  await auth.start();
  router.configure({
    routes,
    isAuthenticated: () => auth.isAuthenticated,
    guard: (path) => remediationRedirect(auth.user, path),
  });
  await router.start();
  void registerServiceWorker();
}

// Fällt vor dem Laden der Sprache etwas aus (etwa ein Sprachpaket nach einem Deploy), steht sonst eine leere Seite da.
function showBootError(): void {
  const box = document.createElement('div');
  box.setAttribute('role', 'alert');
  box.style.cssText = 'font-family:system-ui,sans-serif;max-width:32rem;margin:4rem auto;padding:0 1rem;line-height:1.5';
  const heading = document.createElement('h1');
  heading.textContent = 'LCM konnte nicht starten / LCM could not start';
  const text = document.createElement('p');
  text.textContent = 'Bitte die Seite neu laden. / Please reload the page.';
  const reload = document.createElement('button');
  reload.type = 'button';
  reload.textContent = 'Neu laden / Reload';
  reload.addEventListener('click', () => location.reload());
  box.append(heading, text, reload);
  document.body.replaceChildren(box);
}

boot().catch((error: unknown) => {
  console.error('LCM boot failed', error);
  showBootError();
});
