// Anmeldeweg: Formular, Abmelden, Passwort vergessen, abgelaufene Sitzung und
// die öffentlichen Seiten, die ohne Anmeldung erreichbar sind.
import { test, expect, ADMIN_PASSWORD } from './fixtures';
import { hinweis, meldeAn, waehle } from './helpers';

test.describe('Anmeldung', () => {
  // Diese Tests starten bewusst OHNE vorbereitete Sitzung.
  test.use({ angemeldet: false });

  test('ohne Anmeldung erscheint die Anmeldemaske', async ({ page }) => {
    await page.goto('/');
    await expect(page).toHaveURL(/\/login/);
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('LCM');
    await expect(page.getByRole('form', { name: 'Anmelden' })).toBeVisible();
  });

  test('falsches Passwort zeigt einen Fehler und meldet nicht an', async ({ page }) => {
    await meldeAn(page, 'definitiv-falsch');
    await expect(page.getByRole('alert')).toBeVisible();
    await expect(page).toHaveURL(/\/login/);
  });

  test('das Zwei-Faktor-Feld steht mittig und zeigt ein Beispiel', async ({ page }) => {
    await page.route('**/api/v1/auth/login', (route) =>
      route.fulfill({ json: { twofa_required: true, challenge: 'probe' } }),
    );
    await meldeAn(page, ADMIN_PASSWORD);
    const code = page.getByRole('textbox', { name: 'Code' });
    await expect(code).toHaveAttribute('placeholder', '123456');
    await expect(code).toHaveCSS('text-align', 'center');
  });

  test('Admin kann sich anmelden und abmelden', async ({ page }) => {
    await meldeAn(page, ADMIN_PASSWORD);
    const konto = page.getByRole('button', { name: 'Konto von Administrator' });
    await expect(konto).toBeVisible();

    await konto.click();
    await page.getByRole('menuitem', { name: 'Abmelden' }).click();
    await expect(page.getByRole('form', { name: 'Anmelden' })).toBeVisible();

    // Die Sitzung ist wirklich weg: eine geschützte Seite führt zurück zur Anmeldung.
    await page.goto('/servers');
    await expect(page).toHaveURL(/\/login/);
  });

  test('Passwort vergessen antwortet generisch (keine Benutzer-Aufzählung)', async ({ page }) => {
    await page.goto('/login');
    await page.getByRole('button', { name: 'Passwort vergessen?' }).click();
    await page.getByRole('textbox', { name: 'E-Mail-Adresse' }).fill('unbekannt@example.com');
    await page.getByRole('button', { name: 'Link anfordern' }).click();
    // Auch für unbekannte Adressen dieselbe Bestätigung.
    await expect(hinweis(page, 'Wenn es ein Konto mit dieser Adresse gibt')).toBeVisible();
    await page.getByRole('button', { name: 'Zurück zur Anmeldung' }).click();
    await expect(page.getByRole('textbox', { name: 'Benutzername' })).toBeVisible();
  });
});

test.describe('Öffentliche Seiten ohne Anmeldung', () => {
  test.use({ angemeldet: false });

  test('Aktivierungsseite ohne Schlüssel zeigt einen Hinweis', async ({ page }) => {
    await page.goto('/activate');
    await expect(page.getByRole('heading', { level: 1, name: 'Passwort festlegen' })).toBeVisible();
    await expect(hinweis(page, 'fehlt der Schlüssel')).toBeVisible();
  });

  test('Linux-Aktivierung ist ohne Anmeldung erreichbar und bietet einen erzeugten Schlüssel an', async ({ page }) => {
    // Die öffentliche Self-Service-Aktivierung zeigt das Formular, nicht die Anmeldemaske.
    await page.goto('/linux-activate?token=egal');
    await expect(page.getByRole('heading', { level: 1, name: 'Zugang zu den Servern einrichten' })).toBeVisible();
    await expect(page.getByRole('heading', { level: 2, name: 'SSH-Schlüssel' })).toBeVisible();
    const absenden = page.getByRole('button', { name: 'Zugang einrichten' });
    await expect(absenden).toBeDisabled();

    // Mit der Erzeugung ist das Absenden möglich.
    await waehle(page, /LCM erzeugt ihn/);
    await expect(absenden).toBeEnabled();
  });
});

test.describe('Abgelaufene Sitzung', () => {
  test.use({ angemeldet: false });

  // Auf der Demo der Regelfall: Der nächtliche Reset macht jede Sitzung von
  // gestern ungültig. Wer damit wiederkommt, soll auf der Anmeldeseite landen,
  // ohne dass die Oberfläche vorher kurz angemeldet erscheint.
  test('alte Sitzung führt ohne weitere Abfragen zur Anmeldeseite', async ({ page, baseURL }) => {
    const host = new URL(baseURL!).hostname;
    await page.context().addCookies([
      { name: '__Host-lcm_session', value: 'abgelaufen', domain: host, path: '/', secure: true, httpOnly: true },
      { name: '__Host-lcm_csrf', value: 'abgelaufen', domain: host, path: '/', secure: true },
    ]);
    const abgewiesen: string[] = [];
    page.on('response', (antwort) => {
      if (antwort.status() === 401) abgewiesen.push(new URL(antwort.url()).pathname);
    });

    await page.goto('/servers');
    await expect(page.getByRole('form', { name: 'Anmelden' })).toBeVisible();
    // Nur die Frage „bin ich angemeldet?“ darf scheitern, keine Datenabfrage.
    expect(abgewiesen).toEqual(['/api/v1/auth/me']);
    // Die Anmeldeseite sagt, warum man wieder hier ist.
    await expect(page.getByText('Melde dich an, um fortzufahren.')).toBeVisible();
  });
});

test.describe('Sitzung endet während der Arbeit', () => {
  test('die nächste Anfrage führt zur Anmeldung, danach zurück zur Seite', async ({ page, context }) => {
    await page.goto('/servers');
    await expect(page.getByRole('grid', { name: 'Serverliste' })).toBeVisible();

    await context.clearCookies();
    await page.getByRole('link', { name: 'Gruppen' }).click();
    await expect(page.getByRole('form', { name: 'Anmelden' })).toBeVisible();
    await expect(page.getByText('Melde dich an, um fortzufahren.')).toBeVisible();

    await page.getByRole('textbox', { name: 'Benutzername' }).fill('admin');
    await page.getByRole('textbox', { name: 'Passwort' }).fill(ADMIN_PASSWORD);
    await page.getByRole('button', { name: 'Anmelden' }).click();
    await expect(page).toHaveURL(/\/groups$/);
  });
});
