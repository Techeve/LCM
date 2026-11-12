// Servergruppen: Übersicht, Verwalter, Zeitpläne und Regeln.
import type { Page } from '@playwright/test';
import { test, expect } from './fixtures';
import { bestaetige, bricheAb, fenster, setzeSchalter, waehleEintrag } from './helpers';

/** Legt in „Produktion“ einen Wegwerf-Zeitplan an (die Demo-Gruppe bringt keinen mit). */
async function legeZeitplanAn(page: Page, name: string, ausdruck?: string) {
  await page.goto('/groups/2/schedules');
  await page.getByRole('button', { name: 'Zeitplan anlegen' }).click();
  const zeitplan = fenster(page, 'Zeitplan anlegen');
  await zeitplan.getByRole('textbox', { name: 'Name' }).fill(name);
  if (ausdruck) await zeitplan.getByRole('textbox', { name: 'Ausdruck', exact: true }).fill(ausdruck);
  await zeitplan.getByRole('button', { name: 'Anlegen', exact: true }).click();
  const karte = karteVon(page, name);
  await expect(karte).toBeVisible();
  return karte;
}

/** Die Karte eines Zeitplans im Reiter „Zeitpläne“. */
const karteVon = (page: Page, name: string) =>
  page.locator('lcm-card').filter({ has: page.getByRole('heading', { level: 3, name, exact: true }) });

/** Öffnet das Fenster „Regel hinzufügen“ am Zeitplan; vorher muss er auf der Seite stehen, sonst träfe der Klick die Grundsatz-Regeln. */
async function oeffneRegelFenster(page: Page, zeitplan: string) {
  await page.goto('/groups/2/rules');
  await expect(page.getByRole('heading', { level: 3, name: zeitplan, exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Regel hinzufügen' }).first().click();
  return fenster(page, 'Regel hinzufügen');
}

/** Löscht den Zeitplan samt seinen Regeln; die Tests hinterlassen ihre Ausgangslage. */
async function loescheZeitplan(page: Page, name: string) {
  await page.goto('/groups/2/schedules');
  // Die Karte listet auch ihre Regeln; der Knopf des Zeitplans steht als letzter.
  await karteVon(page, name).getByRole('button', { name: 'Löschen' }).last().click();
  await bestaetige(page, `Zeitplan „${name}“ löschen?`, 'Löschen');
  await expect(karteVon(page, name)).toHaveCount(0);
}

test.describe('Servergruppen: Übersicht', () => {
  test('zeigt Regeln je Auslöser', async ({ page }) => {
    // Produktion bringt keinen vorgefertigten Zeitplan mit, nur die Firewall-Grundsatz-Regel.
    await page.goto('/groups/2/schedules');
    await expect(page.getByRole('heading', { level: 3, name: 'Noch keine Zeitpläne' })).toBeVisible();
    await page.getByRole('tab', { name: /^Regeln/ }).click();
    await expect(page.getByRole('listitem').filter({ hasText: 'Firewall Webserver' })).toContainText('Ports: 80/tcp, 443/tcp');

    // Die Basis-Zeitpläne liegen in der System-Gruppe: Health-Check und System-Sync
    // (mit dem zentralen Docker-Check als Regel darin).
    await page.goto('/groups/1/schedules');
    await expect(karteVon(page, 'Health-Check (Ping)')).toContainText('*/15 * * * *');
    const sync = karteVon(page, 'System-Sync (Hardware & Linux-Benutzer)');
    await expect(sync).toContainText('0 4 * * *');
    await expect(sync.getByRole('listitem').filter({ hasText: 'Docker-Check (Registry & Image-CVEs)' })).toHaveCount(1);
  });

  test('Verwalter lassen sich zuweisen und entfernen', async ({ page }) => {
    // Produktion bringt den Demo-Manager mit: Ohne diese Zuordnung sähe ein
    // Benutzer der Manager-Rolle überhaupt keine Server.
    await page.goto('/groups/2');
    await expect(page.getByRole('listitem').filter({ hasText: 'ops.manager' })).toBeVisible();

    // Zuweisen in einer zweiten Gruppe und wieder entfernen.
    await page.goto('/groups/3');
    await expect(page.getByRole('button', { name: 'ops.manager entfernen' })).toHaveCount(0);
    await waehleEintrag(page.getByRole('combobox', { name: 'Verwalter hinzufügen' }), 'ops.manager (Olivia Ops)', 'ops.manager');
    await page.getByRole('button', { name: 'Hinzufügen' }).click();
    await expect(page.getByRole('button', { name: 'ops.manager entfernen' })).toBeVisible();

    // Entfernen braucht eine Bestätigung: Der Benutzer sieht die Gruppe danach nicht mehr.
    await page.getByRole('button', { name: 'ops.manager entfernen' }).click();
    await bestaetige(page, 'ops.manager als Verwalter entfernen?', 'Entfernen');
    await expect(page.getByRole('button', { name: 'ops.manager entfernen' })).toHaveCount(0);
  });

  test('Verwalter lassen sich mit der Maus aus der Liste wählen, auch am unteren Fensterrand', async ({ page }) => {
    // Niedriges Fenster: Das Feld liegt unter der Falz, der Fokus scrollt die Seite, die Liste muss trotzdem bleiben.
    await page.setViewportSize({ width: 1280, height: 500 });
    await page.goto('/groups/3');
    const feld = page.getByRole('combobox', { name: 'Verwalter hinzufügen' });
    await expect(page.getByRole('button', { name: 'ops.manager entfernen' })).toHaveCount(0);
    await feld.focus();
    const liste = page.getByRole('listbox', { name: 'Verwalter hinzufügen' });
    await expect(liste).toBeVisible();
    // Die Liste liegt vollständig im Fenster, nicht darunter.
    await expect
      .poll(async () => {
        const box = await liste.boundingBox();
        return box !== null && box.y >= 0 && box.y + box.height <= 500;
      })
      .toBe(true);

    await page.getByRole('option', { name: /ops\.manager/ }).click();
    await expect(feld).toHaveValue('ops.manager (Olivia Ops)');
    await expect(liste).toBeHidden();
    await page.getByRole('button', { name: 'Hinzufügen' }).click();
    await expect(page.getByRole('button', { name: 'ops.manager entfernen' })).toBeVisible();

    // Aufräumen: Der Test hinterlässt seine Ausgangslage.
    await page.getByRole('button', { name: 'ops.manager entfernen' }).click();
    await bestaetige(page, 'ops.manager als Verwalter entfernen?', 'Entfernen');
    await expect(page.getByRole('button', { name: 'ops.manager entfernen' })).toHaveCount(0);
  });

  test('Name, Beschreibung und Vorrang sind vorbelegt; die System-Gruppe ist geschützt', async ({ page }) => {
    await page.goto('/groups/2');
    await page.getByRole('button', { name: 'Bearbeiten' }).click();
    const bearbeiten = fenster(page, 'Gruppe bearbeiten');
    await expect(bearbeiten.getByRole('textbox', { name: 'Name' })).toHaveValue('Produktion');
    await expect(bearbeiten.getByRole('textbox', { name: 'Beschreibung' })).toHaveValue('Produktive Server (Demo)');
    // Der Vorrang entscheidet, welche Grundsatz-Regel sich durchsetzt, wenn
    // mehrere Gruppen denselben Server bespielen.
    await expect(bearbeiten.getByRole('textbox', { name: 'Vorrang' })).toHaveValue('100');
    await bearbeiten.getByRole('button', { name: 'Abbrechen' }).click();
    await expect(page.getByText('Vorrang 100', { exact: false }).first()).toBeVisible();

    // Die geschützte System-Gruppe hat weder Bearbeiten noch Auflösen.
    await page.goto('/groups/1');
    await expect(page.getByRole('heading', { level: 2, name: /System/ })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Bearbeiten' })).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'Auflösen' })).toHaveCount(0);
  });
});

test.describe('Servergruppen: Zeitpläne', () => {
  // Der Cron-Ausdruck bleibt die Wahrheit: Vorlagen und Felder schreiben ihn
  // nur und lesen ihn zurück. Beide Richtungen werden hier geprüft.
  test('der Ausdruck lässt sich über Vorlagen und Felder zusammenklicken', async ({ page }) => {
    await page.goto('/groups/2/schedules');
    await page.getByRole('button', { name: 'Zeitplan anlegen' }).click();
    const zeitplan = fenster(page, 'Zeitplan anlegen');
    const ausdruck = zeitplan.locator('code');

    // Voreinstellung 0 3 * * * liest die Maske als „täglich um 03:00“.
    await expect(ausdruck).toHaveText('0 3 * * *');
    await expect(zeitplan.getByRole('paragraph').filter({ hasText: /^Täglich um 03:00$/ })).toBeVisible();
    await expect(zeitplan.getByRole('button', { name: 'Täglich um 03:00' })).toHaveAttribute('aria-pressed', 'true');

    // Vorlage -> Ausdruck.
    await zeitplan.getByRole('button', { name: 'Werktags um 02:00' }).click();
    await expect(ausdruck).toHaveText('0 2 * * 1-5');

    // Felder -> Ausdruck: sonntags 06:30.
    await zeitplan.getByRole('textbox', { name: 'Minute', exact: true }).fill('30');
    await zeitplan.getByRole('textbox', { name: 'Stunde' }).fill('6');
    await zeitplan.getByRole('textbox', { name: 'Wochentag' }).fill('0');
    await zeitplan.getByRole('textbox', { name: 'Wochentag' }).press('Tab'); // die Felder übernehmen beim Verlassen
    await expect(ausdruck).toHaveText('30 6 * * 0');
    // Die Maske nennt die nächsten Läufe.
    await expect(zeitplan.getByText('Nächste Läufe')).toBeVisible();

    // Gespeichert wird der Ausdruck, den die Maske zuletzt geschrieben hat.
    await zeitplan.getByRole('textbox', { name: 'Name' }).fill('e2e-Baukasten');
    await zeitplan.getByRole('button', { name: 'Anlegen', exact: true }).click();
    await expect(karteVon(page, 'e2e-Baukasten')).toContainText('30 6 * * 0');
    await loescheZeitplan(page, 'e2e-Baukasten');
  });

  test('ein ungültiger Ausdruck wird am Feld abgewiesen', async ({ page }) => {
    await page.goto('/groups/2/schedules');
    await page.getByRole('button', { name: 'Zeitplan anlegen' }).click();
    const zeitplan = fenster(page, 'Zeitplan anlegen');
    await zeitplan.getByRole('textbox', { name: 'Name' }).fill('e2e-Ungueltig');
    await zeitplan.getByRole('textbox', { name: 'Stunde' }).fill('99');
    await zeitplan.getByRole('button', { name: 'Anlegen', exact: true }).click();
    // Das Fenster bleibt offen, der Zeitplan entsteht nicht.
    await expect(fenster(page, 'Zeitplan anlegen')).toBeVisible();
    await expect(page.getByRole('heading', { level: 3, name: 'e2e-Ungueltig' })).toHaveCount(0);
  });
});

test.describe('Servergruppen: Regeln', () => {
  test('eine abgebrochene Rückfrage lässt die Grundsatz-Regel aus', async ({ page }) => {
    await page.goto('/groups/2/rules');
    const schalter = page.getByRole('listitem').filter({ hasText: 'Firewall Webserver' }).getByRole('switch', { name: 'Aktiv' });
    await setzeSchalter(schalter, false);
    const rueckfrage = 'Regel „Firewall Webserver“ aktivieren?';
    await schalter.click();
    await fenster(page, rueckfrage).getByRole('button', { name: 'Abbrechen' }).click();
    await expect(fenster(page, rueckfrage)).toHaveCount(0);
    await expect(schalter).not.toBeChecked();

    await schalter.click();
    await bestaetige(page, rueckfrage, 'Aktivieren');
    await expect(schalter).toBeChecked();
  });

  test('das Regel-Fenster schränkt die Typen je Auslöser ein', async ({ page }) => {
    await legeZeitplanAn(page, 'e2e-Typen');
    // Am Zeitplan stehen die Paket-Typen zur Verfügung, dazu Rechte-Soll und Skript.
    const regel = await oeffneRegelFenster(page, 'e2e-Typen');
    const typ = regel.getByRole('combobox', { name: 'Typ' });
    await expect(typ.locator('option', { hasText: 'Bestimmte Pakete aktualisieren' })).toHaveCount(1);
    await expect(typ.locator('option', { hasText: 'Sicherheitsupdates' })).toHaveCount(1);
    await expect(typ.locator('option', { hasText: 'Rechte-Soll halten' })).toHaveCount(1);
    await expect(typ.locator('option', { hasText: 'Skript ausführen' })).toHaveCount(1);
    await typ.selectOption({ label: 'Bestimmte Pakete aktualisieren' });
    await expect(regel.getByRole('textbox', { name: 'Paketnamen' })).toBeVisible();
    await bricheAb(page, regel);

    // Als Grundsatz-Regel stehen nur Typen mit Soll-Zustand zur Verfügung:
    // Firewall, APT-Cache, ACL und Rechte-Soll. „Skript“ ist hier bewusst NICHT
    // dabei: Ein Kommando hat keinen Soll-Zustand und liefe unprotokolliert bei
    // jeder Verbindung.
    await page.getByRole('button', { name: 'Regel hinzufügen' }).last().click();
    const grundsatz = fenster(page, 'Regel hinzufügen');
    await expect(grundsatz.getByRole('combobox', { name: 'Typ' }).locator('option')).toHaveText([
      'Firewall',
      'APT-Cache anbinden',
      'ACL einrichten',
      'Rechte-Soll halten',
    ]);
    await bricheAb(page, grundsatz);
    await loescheZeitplan(page, 'e2e-Typen');
  });

  test('eine Paket-Regel ohne Paketnamen ist nicht speicherbar', async ({ page }) => {
    await legeZeitplanAn(page, 'e2e-Pakete');
    const regel = await oeffneRegelFenster(page, 'e2e-Pakete');
    await regel.getByRole('textbox', { name: 'Name' }).fill('LeerePakete');
    await regel.getByRole('combobox', { name: 'Typ' }).selectOption({ label: 'Bestimmte Pakete aktualisieren' });
    // Ohne Paketnamen weist die Maske ab, statt einen Fehler der API auszulösen.
    await regel.getByRole('button', { name: 'Anlegen', exact: true }).click();
    await expect(fenster(page, 'Regel hinzufügen')).toBeVisible();
    await expect(regel.getByRole('textbox', { name: 'Paketnamen' })).toHaveAttribute('aria-invalid', 'true');

    await regel.getByRole('textbox', { name: 'Paketnamen' }).fill('htop, unzip');
    await regel.getByRole('button', { name: 'Anlegen', exact: true }).click();
    await expect(page.getByRole('listitem').filter({ hasText: 'LeerePakete' })).toBeVisible();
    await loescheZeitplan(page, 'e2e-Pakete');
  });

  test('Regeln entstehen am Zeitplan, lassen sich umsortieren und bleiben danach so', async ({ page }) => {
    await legeZeitplanAn(page, 'e2e-Wartung');
    for (const name of ['e2e-Erste', 'e2e-Zweite']) {
      const regel = await oeffneRegelFenster(page, 'e2e-Wartung');
      await regel.getByRole('textbox', { name: 'Name' }).fill(name);
      await regel.getByRole('button', { name: 'Anlegen', exact: true }).click();
      await expect(page.getByRole('listitem').filter({ hasText: name })).toBeVisible();
    }
    // Sie reihen sich hinten an.
    const zeilen = page.getByRole('listitem').filter({ hasText: /e2e-(Erste|Zweite)/ });
    await expect(zeilen.nth(0)).toContainText('e2e-Erste');
    await expect(zeilen.nth(1)).toContainText('e2e-Zweite');

    // Die zweite nach oben: Reihenfolge wechselt.
    await zeilen.nth(1).getByRole('button', { name: 'Nach oben' }).click();
    await expect(zeilen.nth(0)).toContainText('e2e-Zweite');
    // Nach dem Neuladen bleibt es dabei, die Reihenfolge ist gespeichert.
    await page.reload();
    await expect(zeilen.nth(0)).toContainText('e2e-Zweite');
    await loescheZeitplan(page, 'e2e-Wartung');
  });

  test('der Typ „Eigene Aktion“ ist wählbar; der APT-Cache-Typ erklärt sich', async ({ page }) => {
    await legeZeitplanAn(page, 'e2e-Custom');
    const regel = await oeffneRegelFenster(page, 'e2e-Custom');
    const typ = regel.getByRole('combobox', { name: 'Typ' });
    await expect(typ.locator('option', { hasText: 'Eigene Aktion' })).toHaveCount(1);
    await typ.selectOption({ label: 'Eigene Aktion' });
    await expect(regel.getByText(/Eigene Aktion/).first()).toBeVisible();
    await typ.selectOption({ label: 'APT-Cache anbinden' });
    await expect(regel.getByText(/zentralen APT-Cache/)).toBeVisible();
    await bricheAb(page, regel);
    await loescheZeitplan(page, 'e2e-Custom');
  });
});
