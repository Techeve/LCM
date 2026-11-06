// Rahmen der Anwendung: Konto, Darstellung, Sprache, Info-Fenster, Update-Hinweise, Weiterleitungen.
import { test, expect } from './fixtures';
import { bestaetige, fenster } from './helpers';

// Name des Kanals, über den der Service Worker alle Tabs benachrichtigt (siehe src/lib/broadcast.ts).
const KANAL = 'lcm';

test.describe('Konto', () => {
  test('Mein Konto ist über das Kontomenü erreichbar, mit Profil und 2FA (QR-Code)', async ({ page }) => {
    await page.goto('/');
    await page.getByRole('button', { name: 'Konto von Administrator' }).click();
    await page.getByRole('menuitem', { name: /Mein Konto/ }).click();
    await expect(page).toHaveURL(/\/account$/);
    await expect(page.getByRole('heading', { level: 1, name: 'Profil' })).toBeVisible();

    // Passwort und 2FA liegen im Bereich Sicherheit des Kontos, nicht unter Einstellungen.
    await page.getByRole('navigation', { name: 'Mein Konto' }).getByRole('link', { name: 'Sicherheit' }).click();
    await expect(page.getByRole('heading', { level: 2, name: 'Passwort ändern' })).toBeVisible();
    await expect(page.getByRole('heading', { level: 2, name: 'Zwei-Faktor-Anmeldung' })).toBeVisible();
    await page.getByRole('button', { name: 'Einrichten' }).click();
    // Das QR-Bild erscheint (als Daten-Adresse, ohne dass ein fremder Server es liefert).
    await expect(page.getByRole('img', { name: /QR/i })).toBeVisible();
  });

  test('Einstellungen führen zum ersten Abschnitt; „Mein Konto“ steht dort nicht', async ({ page }) => {
    await page.goto('/settings');
    // Weiterleitung auf die erste sichtbare Unterseite.
    await expect(page).toHaveURL(/\/settings\/general$/);
    const menue = page.getByRole('navigation', { name: 'Einstellungen' });
    await expect(menue.getByRole('link', { name: 'Allgemein' })).toBeVisible();
    await expect(menue).not.toContainText('Mein Konto');
    await expect(menue).not.toContainText('Zwei-Faktor');
  });
});

test.describe('Darstellung und Sprache', () => {
  test('das Farbschema wechselt Automatisch, Dunkel, Hell und die Wahl bleibt nach dem Neuladen', async ({ page }) => {
    await page.goto('/');
    const html = page.locator('html');
    // Standard ist Automatisch (Playwright emuliert „System = hell“).
    await expect(html).toHaveAttribute('data-theme', 'auto');
    await page.getByRole('button', { name: 'Darstellung' }).click();
    await page.getByRole('menuitemradio', { name: 'Dunkel' }).click();
    await expect(html).toHaveAttribute('data-theme', 'dark');

    // Die Wahl überlebt einen Reload.
    await page.reload();
    await expect(html).toHaveAttribute('data-theme', 'dark');

    await page.getByRole('button', { name: 'Darstellung' }).click();
    await page.getByRole('menuitemradio', { name: 'Hell' }).click();
    await expect(html).toHaveAttribute('data-theme', 'light');
    await page.getByRole('button', { name: 'Darstellung' }).click();
    await page.getByRole('menuitemradio', { name: 'Automatisch' }).click();
    await expect(html).toHaveAttribute('data-theme', 'auto');
  });

  test('der Sprachwechsel schaltet die Oberfläche zwischen Deutsch und Englisch', async ({ page }) => {
    await page.goto('/');
    const html = page.locator('html');
    await expect(html).toHaveAttribute('lang', 'de');
    await expect(page.getByRole('link', { name: 'Gruppen' })).toBeVisible();

    await page.getByRole('button', { name: 'Sprache' }).click();
    await page.getByRole('menuitemradio', { name: /English/ }).click();
    await expect(html).toHaveAttribute('lang', 'en');
    await expect(page.getByRole('link', { name: 'Groups' })).toBeVisible();
    await expect(page.getByRole('link', { name: 'Settings' })).toBeVisible();

    // Zurück auf Deutsch (Aufräumen für nachfolgende Tests).
    await page.getByRole('button', { name: 'Language' }).click();
    await page.getByRole('menuitemradio', { name: /Deutsch/ }).click();
    await expect(html).toHaveAttribute('lang', 'de');
    await expect(page.getByRole('link', { name: 'Gruppen' })).toBeVisible();
  });
});

test.describe('Konsole und Version', () => {
  // Self-XSS: Wer die Konsole öffnet, soll die Warnung sehen, bevor er auf Zuruf
  // etwas einfügt: Die Sitzung ist der Zugang zu allen Servern.
  test('die Browser-Konsole warnt vor dem Einfügen fremder Befehle', async ({ page }) => {
    const konsole: string[] = [];
    page.on('console', (nachricht) => konsole.push(nachricht.text()));
    await page.goto('/');
    await expect(page.getByRole('heading', { level: 1, name: 'Dashboard' })).toBeVisible();
    expect(konsole.some((text) => text.includes('STOPP!'))).toBe(true);
    expect(konsole.some((text) => text.includes('nichts einfügen'))).toBe(true);
  });

  test('das Kontomenü zeigt Version und Build', async ({ page }) => {
    await page.goto('/');
    await page.getByRole('button', { name: 'Konto von Administrator' }).click();
    await expect(page.getByRole('menuitem', { name: /Über LCM \d+\.\d+\.\d+.*\(Build \d+\)/ })).toBeVisible();
  });

  test('nach einem Update zeigt ein Fenster einmal, was die Version mitbringt', async ({ page }) => {
    let gesehen = 0;
    await page.route('**/api/v1/system/release-notes', (route) => route.fulfill({ json: { version: '9.9.9', html: '<h3>Features</h3>\n<ul>\n<li><strong>status</strong>: neu</li>\n</ul>\n' } }));
    await page.route('**/api/v1/system/release-notes/seen', (route) => {
      gesehen += 1;
      return route.fulfill({ status: 204 });
    });
    await page.goto('/');
    const hinweise = fenster(page, 'Neu in Version 9.9.9');
    await expect(hinweise.getByRole('heading', { level: 3, name: 'Features' })).toBeVisible();
    await expect(hinweise.getByRole('link', { name: 'Alle Änderungen' })).toHaveAttribute('href', /doc\.techeve\.de\/lcm\/changelog\/\?mtm_campaign=linking/);
    await hinweise.getByRole('button', { name: 'Schließen' }).last().click();
    await expect(hinweise).toBeHidden();
    await expect.poll(() => gesehen).toBe(1);
  });

  test('das Info-Fenster nennt Version, Plattform und den Stand der Updateprüfung', async ({ page }) => {
    await page.goto('/');
    // Der Server antwortet auf die Sofort-Prüfung mit einer neueren Version aus
    // dem Beta-Kanal: So sieht die Antwort nach einem Kanalwechsel aus.
    const neuer = { current_version: '1.0.0', latest_version: '99.9.9-beta.1', update_available: true, channel: 'beta', checked_at: new Date().toISOString() };
    await page.route('**/api/v1/system/update-check', (route) => route.fulfill({ json: neuer }));
    await page.route('**/api/v1/system/update-info', (route) => route.fulfill({ json: neuer }));
    await page.getByRole('button', { name: 'Konto von Administrator' }).click();
    await page.getByRole('menuitem', { name: /Über LCM/ }).click();
    const info = fenster(page, 'Über LCM');
    await expect(info.getByRole('heading', { level: 3, name: 'Version' })).toBeVisible();
    await expect(info.getByText('Plattform')).toBeVisible();

    await info.getByRole('button', { name: 'Jetzt prüfen' }).click();
    // Version UND Kanal stehen da: Die Zahl allein wäre nicht einzuordnen.
    await expect(info).toContainText('99.9.9-beta.1');
    await expect(info).toContainText('beta');
    await expect(info.getByRole('status')).toHaveText('99.9.9-beta.1 ist verfügbar');
  });

  test('„Jetzt prüfen“ antwortet neben dem Knopf, nicht im Band dahinter', async ({ page }) => {
    await page.goto('/');
    const aktuell = { current_version: '1.0.0', latest_version: '1.0.0', update_available: false, channel: 'stable', checked_at: new Date().toISOString() };
    await page.route('**/api/v1/system/update-check', (route) => route.fulfill({ json: aktuell }));
    await page.route('**/api/v1/system/update-info', (route) => route.fulfill({ json: aktuell }));
    await page.getByRole('button', { name: 'Konto von Administrator' }).click();
    await page.getByRole('menuitem', { name: /Über LCM/ }).click();
    const info = fenster(page, 'Über LCM');
    await expect(info.getByText('Commit')).toHaveCount(0);

    await info.getByRole('button', { name: 'Jetzt prüfen' }).click();
    await expect(info.getByRole('status')).toHaveText('Aktuell');
    await expect(page.locator('lcm-update-banner lcm-notice')).toHaveCount(0);
  });
});

test.describe('Update-Hinweise', () => {
  test('„Jetzt installieren“ wartet auf laufende Jobs und nennt sie', async ({ page }) => {
    // Der Server meldet eine neuere Version und ein mögliches Selbst-Update.
    await page.route('**/api/v1/system/update-info', (route) =>
      route.fulfill({ json: { current_version: '1.0.0', latest_version: '99.9.9', update_available: true, channel: 'beta' } }),
    );
    let angefordert = false;
    await page.route('**/api/v1/system/self-update', (route) => {
      if (route.request().method() === 'POST') angefordert = true;
      return route.fulfill({
        json: angefordert
          ? { supported: true, phase: 'waiting', target_version: '99.9.9', waiting_for: ['Alle Pakete aktualisieren @ web-01'] }
          : { supported: true, phase: 'idle' },
      });
    });

    await page.goto('/');
    await expect(page.getByText('LCM 99.9.9 ist verfügbar (installiert: 1.0.0).')).toBeVisible();
    await page.getByRole('button', { name: 'Jetzt installieren' }).click();
    await bestaetige(page, 'LCM 99.9.9 installieren?', 'Jetzt installieren');
    // Die Ansage nennt den Job, auf den gewartet wird: „es dauert noch“ ohne das Wofür wäre keine Auskunft.
    await expect(page.getByText(/sobald diese Jobs fertig sind: Alle Pakete aktualisieren @ web-01/)).toBeVisible();
  });

  test('nach einem Update lädt die Oberfläche sich selbst neu', async ({ page }) => {
    await page.goto('/');
    await expect(page.getByRole('heading', { level: 1, name: 'Dashboard' })).toBeVisible();
    // Kein Hinweis, solange keine neue Version da ist.
    await expect(page.getByText('LCM wurde aktualisiert.')).toHaveCount(0);

    // Der Service Worker meldet einen neuen Build über den BroadcastChannel; ein
    // echter Wechsel des Workers lässt sich im Test nicht herstellen, die Meldung
    // ist aber genau das, was die Oberfläche von ihm bekommt.
    await page.evaluate((kanal) => new BroadcastChannel(kanal).postMessage({ type: 'new-version' }), KANAL);
    await expect(page.getByText('LCM wurde aktualisiert. Die Seite lädt gleich neu.')).toBeVisible();
    // „Jetzt neu laden“ lädt sofort neu, ohne die drei Sekunden abzuwarten.
    const geladen = page.waitForEvent('load');
    await page.getByRole('button', { name: 'Jetzt neu laden' }).click();
    await geladen;
    await expect(page.getByText('LCM wurde aktualisiert.')).toHaveCount(0);
  });
});

test.describe('Weiterleitungen alter Adressen', () => {
  test('/docker/ führt auf /containers', async ({ page }) => {
    await page.goto('/docker/');
    await expect(page).toHaveURL(/\/containers$/);
    await expect(page.getByRole('heading', { level: 1, name: 'Container' })).toBeVisible();
  });

  test('/doku ist ohne Anmeldung erreichbar und führt auf /docs', async ({ page }) => {
    await page.goto('/doku/ssh-schluessel');
    await expect(page).toHaveURL(/\/docs\/ssh-schluessel$/);
  });

  test('/servers/by-name/web01 führt zum Server', async ({ page }) => {
    await page.goto('/servers/by-name/web01');
    await expect(page).toHaveURL(/\/servers\/1$/);
  });

  test('Deep-Links: Pfade rendern ihre Seite, auch mit abschließendem Schrägstrich', async ({ page }) => {
    await page.goto('/security');
    await expect(page.getByRole('heading', { level: 1, name: 'Sicherheit' })).toBeVisible();
    await page.goto('/servers/1');
    await expect(page.getByRole('heading', { level: 2, name: 'web01', exact: true })).toBeVisible();
    await page.goto('/settings/alerts');
    await expect(page.getByRole('heading', { level: 1, name: 'Alarme' })).toBeVisible();
    await page.goto('/jobs/');
    await expect(page.getByRole('heading', { level: 1, name: 'Jobs' })).toBeVisible();
    await page.goto('/gibt-es-nicht');
    await expect(page.getByRole('heading', { level: 1, name: 'Seite nicht gefunden' })).toBeVisible();
  });
});
