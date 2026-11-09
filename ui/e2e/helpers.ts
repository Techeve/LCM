// Kleine Hilfen, die mehrere Spezifikationen brauchen.
import { expect, type Locator, type Page } from '@playwright/test';

/** Meldet über das Anmeldeformular an (nur für Tests, die den Anmeldeweg selbst prüfen). */
export async function meldeAn(page: Page, passwort: string): Promise<void> {
  await page.goto('/login');
  await page.getByRole('textbox', { name: 'Benutzername' }).fill('admin');
  await page.getByRole('textbox', { name: 'Passwort' }).fill(passwort);
  await page.getByRole('button', { name: 'Anmelden' }).click();
}

/**
 * Wählt eine Option einer Auswahlgruppe (`lcm-choice`). Deren Radioknopf ist
 * unsichtbar und nimmt keine Mausklicks an; der Klick landet auf der Karte
 * darüber, deshalb `force`.
 */
export async function waehle(page: Page, name: string | RegExp): Promise<void> {
  await page.getByRole('radio', { name }).check({ force: true });
}

/**
 * Feste Meldung (`lcm-notice`) mit dem Text. Der Text steht im Light-DOM der
 * Komponente, ein Rollen-Locator (`status`, `alert`) kennt ihn deshalb nicht.
 */
export function hinweis(bereich: Page | Locator, text?: string | RegExp): Locator {
  return bereich.locator('lcm-notice', { hasText: text });
}

type ServerEintrag = Record<string, unknown> & { id: number };

/**
 * Verändert in jeder Antwort der Serverliste (auch `?ids=` für die Detailseite)
 * den Eintrag mit der ID. Die Seiten laufen sonst gegen das echte Binary; nur
 * Zustände, die sich in den Demo-Daten nicht auf Zuruf herstellen lassen
 * (handlungsunfähig, Befunde), kommen so von hier.
 */
export async function veraendereServer(
  page: Page,
  id: number,
  aenderung: (server: ServerEintrag) => ServerEintrag,
): Promise<void> {
  await page.route(/\/api\/v1\/servers\?/, async (route) => {
    const antwort = await route.fetch();
    const koerper = await antwort.json();
    const items = (koerper.items as ServerEintrag[]).map((server) => (server.id === id ? aenderung(server) : server));
    await route.fulfill({ response: antwort, json: { ...koerper, items } });
  });
}

/**
 * Offenes Fenster mit dieser Überschrift (ohne Namen: das erste offene).
 * Gemeint ist das äußerste Element mit `open` und `heading`: ein
 * Bestätigungsdialog (`lcm-dialog`), ein Fenster (`lcm-modal`) oder der Rahmen
 * einer Seite um ein solches Fenster (`lcm-groups-form-modal` …). Nur dort
 * hängen die Felder und Knöpfe, die ein Aufrufer ins Fenster steckt; der
 * Rollen-Locator `dialog` kennt sie nicht, weil sie als Light-DOM neben dem
 * `<dialog>` liegen, nicht darin.
 */
export function fenster(page: Page, name?: string): Locator {
  const ueberschrift = name === undefined ? '' : `[heading=${JSON.stringify(name)}]`;
  return page.locator(`[open][heading]${ueberschrift}`).first();
}

/**
 * Bestätigt einen Dialog (`lcm-dialog`): bei zerstörenden Aktionen tippt man
 * erst den Namen ein, dann ist der Knopf frei.
 */
export async function bestaetige(page: Page, ueberschrift: string, knopf: string, eintippen?: string): Promise<void> {
  const dialog = fenster(page, ueberschrift);
  if (eintippen !== undefined) await dialog.getByRole('textbox').fill(eintippen);
  await dialog.getByRole('button', { name: knopf, exact: true }).click();
}

/** Die Speicherleiste einer Einstellungsseite (erscheint, sobald etwas geändert ist). */
export const speicherleiste = (page: Page): Locator => page.getByRole('region', { name: 'Änderungen speichern' });

/** Speichert über die Speicherleiste und wartet auf die Bestätigung. */
export async function speichere(page: Page): Promise<void> {
  await speicherleiste(page).getByRole('button', { name: 'Speichern' }).click();
  // `last`: Speichert ein Test zweimal kurz hintereinander, steht die erste Meldung noch da.
  await expect(page.getByText('Gespeichert', { exact: true }).last()).toBeVisible();
}

/** Stellt einen Schalter auf den gewünschten Zustand, statt ihn vorauszusetzen. Liefert, ob er umgelegt wurde. */
export async function setzeSchalter(schalter: Locator, an: boolean): Promise<boolean> {
  if ((await schalter.isChecked()) === an) return false;
  await schalter.click();
  await expect(schalter).toBeChecked({ checked: an });
  return true;
}

/**
 * Wählt einen Eintrag einer Auswahl mit Tippfilter (`lcm-combobox`): tippen,
 * Enter. Das ist der Weg für Tastatur und Screenreader; die Auswahl mit der
 * Maus prüft ein eigener Test (gruppen.spec.ts).
 */
export async function waehleEintrag(feld: Locator, beschriftung: string, tippen = beschriftung): Promise<void> {
  // Die Liste wird nachgeladen: Enter vor ihrer Ankunft wählt nichts, also wiederholen.
  await expect(async () => {
    await feld.fill(tippen);
    await feld.press('Enter');
    expect(await feld.inputValue()).toBe(beschriftung);
  }).toPass();
}

/**
 * Bricht ein Fenster ab. Hat man etwas eingegeben, fragt es erst, ob die
 * Eingaben verworfen werden sollen (siehe `lib/leave-guard.ts`); das wird bestätigt.
 */
export async function bricheAb(page: Page, offenes: Locator): Promise<void> {
  await offenes.getByRole('button', { name: 'Abbrechen' }).click();
  // Je nach Fenster heißt die Rückfrage „Änderungen verwerfen?“ oder „Eingaben verwerfen?“.
  const rueckfrage = page.getByRole('heading', { level: 2, name: /^(Änderungen|Eingaben) verwerfen\?$/ });
  if (await rueckfrage.waitFor({ state: 'visible', timeout: 1000 }).then(() => true, () => false)) {
    await page.getByRole('button', { name: 'Verwerfen', exact: true }).click();
  }
  await expect(offenes).toBeHidden();
}
