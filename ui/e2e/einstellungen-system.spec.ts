// Einstellungen: Allgemein, Zeit und Subscription.
import { test, expect } from './fixtures';
import type { Page } from '@playwright/test';
import { speichere, speicherleiste } from './helpers';

test.describe('Einstellungen: Allgemein', () => {
  test('zeigt den Onboarding-Schlüssel und gliedert die Karten', async ({ page }) => {
    await page.goto('/settings/general');
    // Der beim ersten Start erzeugte öffentliche Schlüssel ist sichtbar (ssh-ed25519 …).
    await expect(page.getByRole('heading', { level: 2, name: 'Onboarding-Schlüssel' })).toBeVisible();
    await expect(page.getByText(/^ssh-ed25519 /)).toBeVisible();
    await expect(page.getByRole('button', { name: 'Kopieren' })).toBeVisible();

    // Zuerst die Onboarding-Themen, dann Adresse und Anmeldung im Browser, danach Überwachung, E-Mail und Aufbewahrung.
    await expect(page.locator('main').getByRole('heading', { level: 2 })).toHaveText([
      'Vorgabe für neue Server',
      'Onboarding-Schlüssel',
      'Öffentliche Adresse',
      'Anmeldung im Browser',
      'Überwachung',
      'E-Mail-Versand',
      'Aufbewahrung',
    ]);
    // Der Docker-Check hat keine eigenen Einstellungen mehr: Er läuft als Regel des System-Zeitplans.
    await expect(page.getByText('Docker-Check', { exact: false })).toHaveCount(0);
  });
});

test.describe('Einstellungen: Zeit', () => {
  test('Zeitserver und Zeitzone speichern und bleiben nach dem Neuladen stehen', async ({ page }) => {
    await page.goto('/settings/time');
    // Die Felder des PATCH-Rumpfs fehlten früher im Backend: Speichern antwortete mit 200,
    // die Werte kamen nie an, und das Formular stand danach wieder leer.
    const server = page.getByRole('textbox', { name: 'Zeitserver' });
    const zone = page.getByRole('textbox', { name: 'Zeitzone' });
    // Ein Wiederholungslauf findet die Werte des ersten schon vor: Dann nimmt der Test die andere Variante.
    await expect(server).toBeVisible();
    const neu = (await zone.inputValue()) === 'Europe/Berlin' ? 'Europe/Vienna' : 'Europe/Berlin';
    await server.fill(`NTP-Pool = ${neu === 'Europe/Berlin' ? '0' : '1'}.pool.ntp.org`);
    await zone.fill(neu);
    await speichere(page);
    // Direkt nach dem Speichern stehen die Werte noch im Formular ...
    await expect(zone).toHaveValue(neu);
    // ... und sie überleben das Neuladen, kommen also wirklich aus der Datenbank.
    await page.reload();
    await expect(page.getByRole('textbox', { name: 'Zeitserver' })).toHaveValue(`NTP-Pool = ${neu === 'Europe/Berlin' ? '0' : '1'}.pool.ntp.org`);
    await expect(page.getByRole('textbox', { name: 'Zeitzone' })).toHaveValue(neu);
  });
});

test.describe('Meldungen und Speicherleiste', () => {
  test('„Gespeichert“ verdeckt den Speichern-Knopf der nächsten Änderung nicht', async ({ page }) => {
    // Schmales Fenster: Die Leiste reicht bis unter den Stapel der Meldungen.
    await page.setViewportSize({ width: 900, height: 700 });
    await page.goto('/settings/time');
    const zone = page.getByRole('textbox', { name: 'Zeitzone' });
    await expect(zone).toBeVisible();
    await zone.fill('Europe/Paris');
    await speichere(page);

    // Die Meldung steht noch, und schon gibt es die nächste Änderung.
    const meldung = page.locator('lcm-toaster .toast').filter({ hasText: 'Gespeichert' });
    await expect(meldung).toBeVisible();
    await zone.fill('Europe/Rome');
    const knopf = speicherleiste(page).getByRole('button', { name: 'Speichern' });
    await expect(knopf).toBeVisible();
    await expect
      .poll(async () => {
        const [m, k] = await Promise.all([meldung.boundingBox(), knopf.boundingBox()]);
        return m!.y + m!.height <= k!.y;
      })
      .toBe(true);

    // Ein Klick ohne die Meldung vorher zu schließen trifft den Knopf.
    await knopf.click();
    await expect(speicherleiste(page)).toHaveCount(0);
  });
});

test.describe('Einstellungen: Subscription', () => {
  test('Key-Eingabe, Instanz-Kennung und eine ehrliche Fehlermeldung', async ({ page }) => {
    await page.goto('/settings/subscription');
    // Ohne Subscription: Aktivierungskarte mit Key-Feld.
    const schluessel = page.getByRole('textbox', { name: 'Subscription-Key' });
    await expect(schluessel).toBeVisible();
    // Die Instanz-Kennung existiert unabhängig vom Vertrag (UUID, beim ersten Start erzeugt).
    await expect(page.getByText(/[0-9a-f]{8}-[0-9a-f]{4}-/)).toBeVisible();
    // Aktivieren erst mit Eingabe aktiv.
    await expect(page.getByRole('button', { name: 'Aktivieren' })).toBeDisabled();
    await schluessel.fill('LCM-E-AAAAA-BBBBB-CCCCC-DDDDD');
    await expect(page.getByRole('button', { name: 'Aktivieren' })).toBeEnabled();

    // Eigene Dienst-Adresse auf einen toten Port: Die Aktivierung muss EHRLICH scheitern.
    await page.getByRole('button', { name: 'Eigene Dienst-Adresse' }).click();
    await page.getByRole('textbox', { name: /Dienst-Adresse|Adresse/ }).last().fill('http://127.0.0.1:1');
    await page.getByRole('button', { name: 'Aktivieren' }).click();
    await expect(page.getByText(/nicht erreichbar/i).first()).toBeVisible();
    // Weiterhin keine Subscription: Die Aktivierung bleibt angeboten.
    await expect(page.getByRole('button', { name: 'Aktivieren' })).toBeVisible();
  });

  test('die Kanalauswahl ist ohne Vertrag sichtbar, Enterprise gesperrt', async ({ page }) => {
    await kanalStaende(page, { community: { latest: '2.0.0' }, beta: { latest: '2.0.1~beta.1' }, enterprise: { lock: 'subscription' } });
    await page.goto('/settings/subscription');
    await expect(page.getByRole('heading', { level: 2, name: 'Paketkanal des LCM-Hosts' })).toBeVisible();
    // Die Kanalauswahl hängt NICHT am Vertrag: Community und Beta sind frei.
    await expect(page.getByRole('radio', { name: /^Community-Kanal/ })).toBeEnabled();
    await expect(page.getByRole('radio', { name: /^Beta-Kanal/ })).toBeEnabled();
    // Ohne hinterlegte Subscription bleibt allein der Enterprise-Kanal zu.
    await expect(page.getByRole('radio', { name: /^Enterprise-Kanal/ })).toBeDisabled();
    // Jeder Kanal nennt seine neueste Version.
    await expect(page.getByText('2.0.1-beta.1')).toBeVisible();
  });

  test('ein Kanal, der eine Feature-Version zurückliegt, ist gesperrt', async ({ page }) => {
    // Die Demo steht auf Community; der eigene Kanal bleibt immer wählbar.
    await kanalStaende(page, { community: { latest: '2.1.0' }, beta: { latest: '2.0.0~beta.12', lock: 'behind' }, enterprise: { lock: 'subscription' } }, '2.1.0');
    await page.goto('/settings/subscription');
    await expect(page.getByRole('radio', { name: /^Community-Kanal/ })).toBeEnabled();
    await expect(page.getByRole('radio', { name: /^Beta-Kanal/ })).toBeDisabled();
    await expect(page.getByText(/liegt hinter der installierten Version 2\.1\.0\. Wechseln geht erst, wenn er 2\.1 erreicht hat/)).toBeVisible();
  });
});

type KanalStand = { latest?: string; lock?: string };

/** Die Stände der Paketkanäle kommen sonst live aus dem Repository; der Test legt sie fest. */
async function kanalStaende(page: Page, staende: Record<'community' | 'beta' | 'enterprise', KanalStand>, installed = '2.0.0'): Promise<void> {
  await page.route('**/api/v1/subscription/apt/channels', (route) =>
    route.fulfill({
      json: { installed, channels: Object.entries(staende).map(([channel, stand]) => ({ channel, latest: stand.latest ?? '', lock: stand.lock ?? '' })) },
    }),
  );
}
