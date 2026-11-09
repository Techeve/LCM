// Service Worker: nur für statische Dateien des aktuellen Builds (siehe sw.ts).
// Registriert wird nur im Produktionsbuild; der Entwicklungsserver liefert
// ungehashte Module, die nie zwischengespeichert werden dürfen.

let registration: ServiceWorkerRegistration | undefined;

interface ScriptUrlPolicies {
  createPolicy(name: string, rules: { createScriptURL(url: string): string }): { createScriptURL(url: string): string };
}

const SW_URL = '/sw.js';

/** Die CSP verlangt Trusted Types: `register` nimmt die Adresse nur aus der Richtlinie `lcm-ui`, und die kennt nur sw.js. */
function swUrl(): string {
  const policies = (window as unknown as { trustedTypes?: ScriptUrlPolicies }).trustedTypes;
  if (!policies) return SW_URL;
  const policy = policies.createPolicy('lcm-ui', {
    createScriptURL(url) {
      if (url !== SW_URL) throw new TypeError(`unerwartete Skriptadresse: ${url}`);
      return url;
    },
  });
  return policy.createScriptURL(SW_URL);
}

function loaded(): Promise<void> {
  if (document.readyState === 'complete') return Promise.resolve();
  return new Promise((resolve) => addEventListener('load', () => resolve(), { once: true }));
}

export async function registerServiceWorker(): Promise<void> {
  if (!import.meta.env.PROD || !('serviceWorker' in navigator)) return;
  await loaded(); // das Vorladen aller Dateien soll den Start nicht bremsen
  try {
    registration = await navigator.serviceWorker.register(swUrl(), { scope: '/', updateViaCache: 'none' });
  } catch (error) {
    console.error('service worker registration failed', error);
  }
}

/** Fragt nach einem neuen Service-Worker-Skript (Ereignis `system.build`). */
export function checkForNewVersion(): void {
  registration?.update().catch(() => {});
}
