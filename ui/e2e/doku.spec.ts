// Doku: ohne Anmeldung erreichbar, gerendertes Markdown, Seitenliste und Querverweise.
import { test, expect } from './fixtures';

test.describe('Doku ohne Anmeldung', () => {
  // Bewusst OHNE Anmeldung: Die Schlüssel-Anleitung wird gebraucht, bevor man
  // einen Zugang hat, zum Beispiel aus der Aktivierungs-Mail heraus.
  test.use({ angemeldet: false });

  test('die Doku ist erreichbar und rendert Markdown', async ({ page }) => {
    await page.goto('/docs');
    const artikel = page.getByRole('article');
    await expect(artikel.getByRole('heading', { level: 1, name: 'SSH-Schlüssel einrichten' })).toBeVisible();
    // Die drei Systeme und der PuTTY-Weg müssen drin sein.
    for (const abschnitt of ['Linux und macOS', 'Windows mit OpenSSH', 'Windows mit PuTTY']) {
      await expect(artikel.getByRole('heading', { level: 2, name: new RegExp(`^${abschnitt}`) })).toBeVisible();
    }
    // Markdown wurde wirklich gerendert, nicht als Rohtext ausgegeben.
    await expect(artikel.locator('pre code, code').filter({ hasText: 'ssh-keygen' }).first()).toBeVisible();
    await expect(artikel.getByRole('table').first()).toContainText('id_ed25519');
    await expect(artikel.locator('blockquote').first()).toBeVisible();
    // Kein Roh-HTML aus der Quelle.
    await expect(artikel.locator('script')).toHaveCount(0);
    // Überschriften tragen Anker, zu denen verlinkt werden kann.
    await expect(artikel.getByRole('heading', { level: 2, name: /^Windows mit OpenSSH/ }).getByRole('link')).toHaveAttribute('href', '#windows-mit-openssh');
  });

  test('zweite Seite (SSH-2FA) mit Seitenliste und Querverweis', async ({ page }) => {
    await page.goto('/docs');
    const liste = page.getByRole('navigation', { name: 'Doku-Seiten' });
    await liste.getByRole('link', { name: /Zwei-Faktor/ }).click();
    await expect(page).toHaveURL(/\/docs\/ssh-2fa$/);
    const artikel = page.getByRole('article');
    await expect(artikel.getByRole('heading', { level: 1, name: /Zwei-Faktor-Anmeldung/ })).toBeVisible();
    // Die Anleitung muss den Weg vollständig zeigen, inklusive der Probe in einer
    // zweiten Sitzung und dem Rückweg, sonst sperrt man sich aus.
    await expect(artikel).toContainText('google-authenticator');
    await expect(artikel).toContainText('Notfallcodes');
    await expect(artikel.getByRole('heading', { level: 2, name: /Vorher prüfen/ })).toBeVisible();
    await expect(artikel).toContainText('rm ~/.google_authenticator');
    // Querverweis auf die Schlüssel-Seite.
    await expect(artikel.locator('a[href="/docs/ssh-schluessel"]').first()).toBeVisible();
  });

  test('Geräte-Anleitungen: Agent, RouterOS, Synology', async ({ page }) => {
    await page.goto('/docs');
    const liste = page.getByRole('navigation', { name: 'Doku-Seiten' });
    const artikel = page.getByRole('article');

    // Agent: Der Port ist die Stelle, an der es sonst hakt.
    await liste.getByRole('link', { name: /Agent/ }).click();
    await expect(artikel.getByRole('heading', { level: 1, name: /LCM-Agent/ })).toBeVisible();
    await expect(artikel).toContainText('lcm-agent enroll');
    await expect(artikel).toContainText('9320');

    // RouterOS: Nur-Lese-Benutzer und Schlüssel-Import.
    await liste.getByRole('link', { name: /MikroTik/ }).click();
    await expect(artikel.getByRole('heading', { level: 1, name: /MikroTik/ })).toBeVisible();
    await expect(artikel).toContainText('group=read');
    await expect(artikel).toContainText('ssh-keys import');

    // Synology: Die erzwungene 2FA ist der klassische Stolperstein.
    await liste.getByRole('link', { name: /Synology/ }).click();
    await expect(artikel.getByRole('heading', { level: 1, name: /Synology/ })).toBeVisible();
    await expect(artikel).toContainText('administrators');
    await expect(artikel).toContainText('5001');
    await expect(artikel).toContainText('Zwei-Faktor');
  });
});

test.describe('Doku angemeldet', () => {
  test('die Doku ist über die Navigation erreichbar', async ({ page }) => {
    await page.goto('/');
    await page.getByRole('link', { name: 'Doku' }).click();
    await expect(page).toHaveURL(/\/docs\/[a-z0-9-]+$/);
    await expect(page.getByRole('article')).toBeVisible();
  });
});
