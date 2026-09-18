// Abgelaufene Sitzung: Wer mit einem alten Token wiederkommt, soll auf der
// Anmeldeseite landen - ohne dass die Oberfläche vorher kurz angemeldet
// erscheint und drei Abfragen in 401 laufen. Auf der Demo ist das der
// Regelfall: Der nächtliche Reset macht jedes Token von gestern ungültig.
import { test, expect } from './fixtures.js';

// Ein JWT, dessen exp in der Vergangenheit liegt. Signatur egal - der Client
// schaut nur auf das Ablaufdatum, der Server bekommt es gar nicht erst.
function abgelaufenesToken() {
  const teil = (o) => Buffer.from(JSON.stringify(o)).toString('base64url');
  return `${teil({ alg: 'HS256', typ: 'JWT' })}.${teil({ username: 'demo', exp: 1000000000 })}.egal`;
}

test.describe('Abgelaufene Sitzung', () => {
  test.use({ angemeldet: false });

  test('landet ohne 401-Abfragen auf der Anmeldeseite', async ({ page }) => {
    await page.addInitScript(
      ([token, user]) => {
        localStorage.setItem('lcm.token', token);
        localStorage.setItem('lcm.user', user);
      },
      [abgelaufenesToken(), JSON.stringify({ id: 1, username: 'demo', permissions: ['servers:read'] })],
    );

    const abgewiesen = [];
    page.on('response', (r) => {
      if (r.status() === 401) abgewiesen.push(new URL(r.url()).pathname);
    });

    await page.goto('/#/');
    await expect(page.locator('#username')).toBeVisible();
    expect(abgewiesen, 'kein Aufruf darf mit dem alten Token losgeschickt werden').toEqual([]);

    // Und das Token ist weg, nicht bloß ignoriert.
    expect(await page.evaluate(() => localStorage.getItem('lcm.token'))).toBeNull();
  });
});
