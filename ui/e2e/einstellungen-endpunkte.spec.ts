// Einstellungen → Allgemein, Block „Öffentliche Adressen“: Adressmodus und Ports, Schalter für Agent und MCP,
// Zertifikat und Zugriff (Netzbereiche) am Zahnrad der Adresse, Let's Encrypt und der Schalter auf der Seite MCP.
//
// Zertifikate gibt es nur, wenn der Testserver wie im Betrieb TLS spricht (LCM_E2E_TLS=1, siehe run-server.sh);
// mit --dev bleibt der Zertifikatsteil beim Hinweis. Die Netzbereiche laufen in beiden Fällen.
//
// Der Testserver legt Oberflächen- und Agent-Port in der config.json fest und weicht damit von den Vorgaben ab:
// Beide Felder sind gesperrt. Ein anderer Modus wird deshalb nur eingestellt, nie auf dem gemeinsamen Port geöffnet.
import { X509Certificate } from 'node:crypto';
import { execFileSync } from 'node:child_process';
import { mkdtempSync, readFileSync, writeFileSync } from 'node:fs';
import { connect } from 'node:tls';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { test, expect } from './fixtures';
import { fenster, hinweis, speichere, speicherleiste, waehle } from './helpers';
import type { Locator, Page } from '@playwright/test';

const TLS = process.env.LCM_E2E_TLS === '1';

/** Ein Programm, das ein selbstsigniertes Zertifikat für einen Namen erzeugt; es liegt nur in einem Temp-Verzeichnis. */
const ZERTIFIKAT_GO = `package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"time"
)

func main() {
	dir, name := os.Args[1], os.Args[2]
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: name},
		DNSNames:     []string{name},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(60 * 24 * time.Hour),
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, _ := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	keyDER, _ := x509.MarshalECPrivateKey(key)
	os.WriteFile(filepath.Join(dir, "cert.pem"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600)
	os.WriteFile(filepath.Join(dir, "key.pem"), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600)
}
`;

interface Paar {
  cert: string;
  key: string;
}

/** Erzeugt Zertifikat und Schlüssel für den Namen mit der Go-Toolchain, die der Testserver ohnehin braucht. */
function erzeugeZertifikat(name: string): Paar {
  const dir = mkdtempSync(join(tmpdir(), 'lcm-e2e-cert-'));
  const programm = join(dir, 'main.go');
  writeFileSync(programm, ZERTIFIKAT_GO);
  execFileSync('go', ['run', programm, dir, name], { cwd: dir, stdio: 'pipe' });
  return { cert: join(dir, 'cert.pem'), key: join(dir, 'key.pem') };
}

/** Der Fingerabdruck des Zertifikats, das der Server dem Aufrufer mit diesem Namen (SNI) liefert; ohne Namen der Rückfall. */
function ausgeliefert(port: number, servername?: string): Promise<string> {
  return new Promise((resolve, reject) => {
    const socket = connect({ host: '127.0.0.1', port, servername, rejectUnauthorized: false }, () => {
      const fingerprint = socket.getPeerCertificate().fingerprint256;
      socket.end();
      resolve(fingerprint);
    });
    socket.on('error', reject);
  });
}

type Dienst = 'ui' | 'agent' | 'mcp';

const DIENSTE: Record<Dienst, string> = { ui: 'Weboberfläche', agent: 'Agenten-Schnittstelle', mcp: 'MCP-Schnittstelle' };

const block = (page: Page): Locator => page.locator('lcm-address-block');
const zahnrad = (page: Page, dienst: string): Locator => page.getByRole('button', { name: `Zertifikat & Sicherheit für ${dienst}` });
const dialogDe = (page: Page, dienst: string): Locator => fenster(page, `${dienst}: Zertifikat & Sicherheit`);
const gruppe = (page: Page, dienst: Dienst): Locator => page.locator(`lcm-network-group[endpoint="${dienst}"]`);
const adresseFeld = (page: Page): Locator => page.getByRole('textbox', { name: 'Name der Weboberfläche' });

/** Klappt eine eingeklappte Karte auf (Let's Encrypt); eine offene lässt es in Ruhe. */
async function klappeAuf(bereich: Locator, name: string): Promise<void> {
  const kopf = bereich.getByRole('button', { name, exact: true });
  if ((await kopf.getAttribute('aria-expanded')) === 'false') await kopf.click();
}

/** Öffnet das Fenster am Zahnrad des Dienstes und gibt seine Gruppe der Netzbereiche zurück. */
async function oeffneGruppe(page: Page, dienst: Dienst): Promise<Locator> {
  await page.goto('/settings/general');
  return oeffneGruppeHier(page, dienst);
}

async function oeffneGruppeHier(page: Page, dienst: Dienst): Promise<Locator> {
  await zahnrad(page, DIENSTE[dienst]).click();
  const gruppeDesDienstes = gruppe(page, dienst);
  await expect(gruppeDesDienstes.getByRole('heading', { level: 4, name: DIENSTE[dienst] })).toBeVisible();
  return gruppeDesDienstes;
}

/** Öffnet die Karte Let's Encrypt im Block. */
async function oeffneLetsEncrypt(page: Page): Promise<Locator> {
  await page.goto('/settings/general');
  const karte = page.locator('lcm-settings-acme');
  await klappeAuf(karte, "Let's Encrypt");
  return karte;
}

/** Die Felder der Anbieter ohne Pflicht stehen unter „Weitere Einstellungen“, eingeklappt. */
async function oeffneWeitere(karte: Locator): Promise<void> {
  await karte.locator('[data-field="dns-more"] summary').click();
}

/** Trägt die Adresse der Oberfläche ein und speichert die Seite: Let's Encrypt nimmt die Namen aus den Adressen. */
async function speichereAdresse(page: Page, adresse: string): Promise<void> {
  await adresseFeld(page).fill(adresse);
  await speichere(page);
}

test.describe('Adressmodus und Ports', () => {
  test('Modus A ist vorgewählt, die Ports aus der config.json stehen in den Feldern und sind änderbar', async ({ page }) => {
    await page.goto('/settings/general');
    await expect(waehleModus(page, 'Eigene Ports, eigene Namen')).toBeChecked();
    for (const dienst of ['Weboberfläche', 'Agenten-Schnittstelle', 'MCP-Schnittstelle']) await expect(page.getByRole('group', { name: dienst })).toBeVisible();

    // Der Testserver schreibt Oberflächen- und Agent-Port in die config.json. LCM übernimmt sie in die
    // Einstellungen und hält beide synchron: Kein Feld ist mehr gesperrt.
    const oberflaeche = block(page).locator('lcm-input[data-key="ui_port"]').getByRole('textbox');
    const agent = block(page).locator('lcm-input[data-key="agent_port"]').getByRole('textbox');
    const mcp = block(page).locator('lcm-input[data-key="mcp_port"]').getByRole('textbox');
    await expect(oberflaeche).toBeEnabled();
    await expect(oberflaeche).toHaveValue(new URL(page.url()).port);
    await expect(agent).toBeEnabled();
    await expect(mcp).toHaveValue('9330');
    await expect(block(page).getByText(/festgelegt/)).toHaveCount(0);
  });

  test('Modus B: ein Name, drei Ports, die Adressen daraus stehen darunter und bleiben nach dem Speichern', async ({ page }) => {
    await page.goto('/settings/general');
    await waehle(page, /^Eigene Ports, ein Name/);
    const name = block(page).getByRole('textbox', { name: 'Name', exact: true });
    await expect(name).toBeVisible();
    for (const port of ['Port der Weboberfläche', 'Port der Agenten-Schnittstelle', 'Port der MCP-Schnittstelle']) await expect(block(page).getByRole('textbox', { name: port })).toBeVisible();
    await expect(block(page).getByText('Trage einen Namen ein.')).toHaveCount(3);

    await name.fill('lcm.example.test');
    // Jede Adresse steht in einem Feld mit Kopierknopf.
    await expect(block(page).locator('[data-field="url-ui"]')).toHaveAttribute('value', /^https:\/\/lcm\.example\.test:\d+$/);
    await expect(block(page).locator('[data-field="url-agent"]')).toHaveAttribute('value', /^https:\/\/lcm\.example\.test:\d+$/);
    await expect(block(page).locator('[data-field="url-ui"]').getByRole('button', { name: 'Kopieren' })).toBeVisible();
    // MCP spricht immer TLS.
    await expect(block(page).locator('[data-field="url-mcp"]')).toHaveAttribute('value', 'https://lcm.example.test:9330');
    // Ein Port allein schaltet keinen Dienst ein: Ist MCP aus, steht es dabei.
    await expect(block(page).locator('[data-field="inactive-mcp"]')).toHaveText('(nicht aktiv)');
    await expect(block(page).locator('[data-field="inactive-ui"]')).toHaveCount(0);
    await speichere(page);

    await page.reload();
    await expect(waehleModus(page, 'Eigene Ports, ein Name')).toBeChecked();
    await expect(block(page).getByRole('textbox', { name: 'Name', exact: true })).toHaveValue('lcm.example.test');

    // Aufräumen: zurück auf Modus A, damit die übrigen Tests den Ausgangszustand finden.
    await waehle(page, /^Eigene Ports, eigene Namen/);
    await speichere(page);
    await page.reload();
    await expect(waehleModus(page, 'Eigene Ports, eigene Namen')).toBeChecked();
  });

  test('Modus C: drei Namen und der gemeinsame Port 443, die Hilfe nennt den Port, gleiche Namen werden abgelehnt', async ({ page }) => {
    const gesendet: unknown[] = [];
    await page.route('**/api/v1/settings/global', (route) => {
      if (route.request().method() === 'PATCH') gesendet.push(route.request().postDataJSON());
      return route.fallback();
    });
    await page.goto('/settings/general');
    await waehle(page, /^Ein Port, eigene Namen/);
    await expect(block(page).getByRole('textbox', { name: 'Gemeinsamer Port' })).toHaveValue('443');
    for (const feld of ['Name der Weboberfläche', 'Name der Agenten-Schnittstelle', 'Name der MCP-Schnittstelle']) await expect(block(page).getByRole('textbox', { name: feld })).toBeVisible();
    // Ohne Namen sind Agent und MCP aus, ihre Schalter gesperrt. Ein Name schaltet den Dienst ein.
    for (const dienst of ['agent', 'mcp']) {
      await expect(block(page).locator(`[data-switch="${dienst}"]`).getByRole('switch')).toBeDisabled();
      await expect(block(page).locator(`[data-switch-hint="${dienst}"]`)).toHaveText('Für diese Schnittstelle ist keine Domain eingerichtet.');
    }
    // Die Hilfe folgt dem Modus. Auf breiten Bildschirmen steht sie in der Spalte der Seite, nicht im Block.
    await expect(page.locator('lcm-explainer').getByText('Gib den Port 443 frei', { exact: false }).locator('visible=true')).toBeVisible();

    for (const feld of ['Name der Weboberfläche', 'Name der Agenten-Schnittstelle', 'Name der MCP-Schnittstelle']) await block(page).getByRole('textbox', { name: feld }).fill('lcm.example.test');
    await speicherleiste(page).getByRole('button', { name: 'Speichern' }).click();
    await expect(block(page).getByText('Jede Schnittstelle braucht einen eigenen Namen.').first()).toBeVisible();
    expect(gesendet).toHaveLength(0);
  });

  test('ein anderer Oberflächen-Port nennt die neue Adresse schon vor dem Speichern', async ({ page }) => {
    // Der Testserver sperrt den Port über die config.json; hier gilt er als frei, damit sich das Feld ändern lässt.
    await page.route('**/api/v1/settings/global', async (route) => {
      if (route.request().method() !== 'GET') return route.fallback();
      // Ohne If-None-Match: Eine 304-Antwort hätte keinen Rumpf, den sich ändern ließe.
      const { 'if-none-match': _ohne, ...kopf } = route.request().headers();
      const antwort = await route.fetch({ headers: kopf });
      const koerper = await antwort.json();
      return route.fulfill({ response: antwort, json: { ...koerper, address_locks: { ui_port: false, agent_port: false, mcp_port: false, shared_tls_port: false } } });
    });
    await page.goto('/settings/general');
    const feld = block(page).locator('lcm-input[data-key="ui_port"]').getByRole('textbox');
    await expect(feld).toBeEnabled();
    // Die Überschrift der Meldung ist ein Attribut, kein Text.
    const meldung = block(page).locator('lcm-notice[heading="Neue Adresse der Oberfläche"]');
    await expect(meldung).toHaveCount(0);
    await feld.fill('8999');
    await expect(meldung).toContainText(':8999');
  });
});

/** Die Karte des Adressmodus mit diesem Titel (die Beschreibung gehört zum Namen des Radioknopfs, daher der Anfang). */
function waehleModus(page: Page, titel: string): Locator {
  return block(page).getByRole('radio', { name: new RegExp(`^${titel}`) });
}

test.describe('Zertifikat am Zahnrad der Adresse', () => {
  test('das Zahnrad steht an jeder Adresse und öffnet das Fenster des Dienstes mit Zertifikat und Netzbereichen', async ({ page }) => {
    await page.goto('/settings/general');
    for (const dienst of Object.values(DIENSTE)) await expect(zahnrad(page, dienst)).toBeVisible();

    await zahnrad(page, 'Agenten-Schnittstelle').click();
    const dialog = dialogDe(page, 'Agenten-Schnittstelle');
    await expect(dialog.getByRole('heading', { level: 3, name: 'Zertifikat' })).toBeVisible();
    // Darunter stehen die Netzbereiche dieses Dienstes, und nur dieses.
    await expect(dialog.getByRole('heading', { level: 3, name: 'Zugriff (Netzbereiche)' })).toBeVisible();
    await expect(dialog.locator('lcm-network-group')).toHaveCount(1);
    // Ohne eingetragene Adresse gilt das LCM-Zertifikat; im Entwicklungsmodus gibt es gar keines.
    await expect(dialog.getByText(TLS ? 'Ohne Namen liefert LCM sein eigenes Zertifikat aus' : 'Entwicklungsmodus ohne TLS')).toBeVisible();
  });

  test.describe('Zertifikate (TLS-Testserver)', () => {
    test.skip(!TLS, 'Zertifikate gibt es nur mit LCM_E2E_TLS=1.');

    test('ein hochgeladenes Zertifikat wird für den Namen ausgeliefert und lässt sich entfernen', async ({ page, baseURL }) => {
      const port = Number(new URL(baseURL!).port);
      const name = 'lcm.example.test';
      const paar = erzeugeZertifikat(name);
      const fingerprint = new X509Certificate(readFileSync(paar.cert)).fingerprint256;

      // Erst eine Adresse eintragen: Ohne Namen gibt es nichts, wofür das Zertifikat gelten könnte.
      await page.goto('/settings/general');
      await speichereAdresse(page, `https://${name}:${port}`);

      await zahnrad(page, 'Weboberfläche').click();
      const dialog = dialogDe(page, 'Weboberfläche');
      await expect(dialog.getByText(`Für ${name} gibt es kein eigenes Zertifikat`)).toBeVisible();
      await expect(dialog.getByText('LCM-Zertifikat', { exact: true }).first()).toBeVisible();

      // Ein Schlüssel, der nicht passt, wird mit Grund abgelehnt, und es bleibt nichts liegen.
      const fremd = erzeugeZertifikat('anders.example.test');
      await dialog.getByRole('button', { name: 'Zertifikat hochladen' }).click();
      await dialog.locator('input[data-field="certificate"]').setInputFiles(paar.cert);
      await dialog.locator('input[data-field="key"]').setInputFiles(fremd.key);
      await dialog.getByRole('button', { name: 'Hochladen', exact: true }).click();
      await expect(dialog.getByText('der Schlüssel gehört nicht zu diesem Zertifikat')).toBeVisible();

      await dialog.locator('input[data-field="key"]').setInputFiles(paar.key);
      await dialog.getByRole('button', { name: 'Hochladen', exact: true }).click();
      await expect(dialog.getByText(`LCM liefert es jetzt für ${name} aus.`)).toBeVisible();
      await expect(dialog.getByText('Hochgeladen', { exact: true })).toBeVisible();
      await expect(dialog.getByText(name, { exact: true }).first()).toBeVisible();

      // Der Handshake liefert es für den Namen, per IP weiter das LCM-Zertifikat.
      expect(await ausgeliefert(port, name)).toBe(fingerprint);
      expect(await ausgeliefert(port)).not.toBe(fingerprint);

      await dialog.getByRole('button', { name: 'Hochgeladenes entfernen' }).click();
      const rueckfrage = fenster(page, 'Zertifikat entfernen?');
      await rueckfrage.getByRole('button', { name: 'Entfernen', exact: true }).click();
      await expect(dialog.getByText(`Für ${name} gibt es kein eigenes Zertifikat`)).toBeVisible();
      expect(await ausgeliefert(port, name)).not.toBe(fingerprint);

      // Aufräumen: Die Adresse wieder leeren.
      await dialog.getByRole('button', { name: 'Schließen' }).last().click();
      await speichereAdresse(page, '');
    });
  });
});

test.describe('Zugriff: Netzbereiche je Dienst', () => {
  test('das Fenster jedes Dienstes zeigt seine Gruppe', async ({ page }) => {
    await page.goto('/settings/general');
    for (const dienst of Object.keys(DIENSTE) as Dienst[]) {
      await oeffneGruppeHier(page, dienst);
      await expect(gruppe(page, dienst).getByText('Alle Netze erlaubt')).toBeVisible();
      await dialogDe(page, DIENSTE[dienst]).getByRole('button', { name: 'Schließen' }).last().click();
    }
  });

  test('ein Bereich ohne die eigene Adresse wird abgelehnt, mit ihr gilt er sofort', async ({ page }) => {
    const ui = await oeffneGruppe(page, 'ui');
    const feld = ui.getByRole('textbox', { name: 'Erlaubte Netze' });
    const speichern = ui.getByRole('button', { name: 'Netzbereiche speichern: Weboberfläche' });
    await expect(speichern).toBeDisabled();
    const eigene = (await ui.locator('[data-field="client-address"]').textContent())!.trim();

    await feld.fill('10.0.0.0/8');
    await speichern.click();
    await expect(ui.getByText(/würdest dich aussperren/)).toBeVisible();
    await expect(ui.getByText(`Deine eigene Adresse ${eigene}`, { exact: false })).toBeVisible();

    await feld.fill(`${eigene}\n10.0.0.0/8`);
    await speichern.click();
    await expect(ui.getByText('Netzbereiche gespeichert')).toBeVisible();
    await expect(ui.getByText('2 Einträge')).toBeVisible();

    // Nach dem Neuladen steht der Bereich in der Datenbank, so wie der Server ihn schreibt (eine Adresse als /32); zurück auf alle Netze.
    await page.reload();
    await oeffneGruppeHier(page, 'ui');
    await expect(ui.getByRole('textbox', { name: 'Erlaubte Netze' })).toHaveValue(`${eigene}/32\n10.0.0.0/8`);
    await ui.getByRole('textbox', { name: 'Erlaubte Netze' }).fill('');
    await ui.getByRole('button', { name: 'Netzbereiche speichern: Weboberfläche' }).click();
    await expect(ui.getByText('Alle Netze erlaubt')).toBeVisible();
  });

  test('ungültige Einträge nennt die Gruppe, bevor etwas gesendet wird', async ({ page }) => {
    const mcp = await oeffneGruppe(page, 'mcp');
    let gesendet = false;
    await page.route('**/api/v1/system/network/mcp', (route) => {
      gesendet = true;
      return route.abort();
    });
    await mcp.getByRole('textbox', { name: 'Erlaubte Netze' }).fill('10.0.0.0/8\nexample.com');
    await mcp.getByRole('button', { name: 'Netzbereiche speichern: MCP' }).click();
    await expect(mcp.getByText('Kein gültiger Eintrag: example.com')).toBeVisible();
    expect(gesendet).toBe(false);
  });

  test('ein engerer Agent-Bereich fragt vor dem Trennen verbundener Agenten und bestätigt mit confirm', async ({ page }) => {
    const bodies: Array<{ allowed: string[]; confirm: boolean }> = [];
    await page.route('**/api/v1/system/network/agent', async (route) => {
      const body = route.request().postDataJSON();
      bodies.push(body);
      if (!body.confirm) {
        return route.fulfill({
          status: 409,
          json: { error: 'x', code: 'endpoint.agents_outside', params: { count: 1, agents: [{ id: 'a1', name: 'web01', address: '203.0.113.9' }] } },
        });
      }
      return route.fulfill({ json: { endpoint: 'agent', allowed: body.allowed, source: 'settings', locked: false, disconnected: [{ id: 'a1', name: 'web01', address: '203.0.113.9' }] } });
    });
    const agent = await oeffneGruppe(page, 'agent');
    await agent.getByRole('textbox', { name: 'Erlaubte Netze' }).fill('10.0.0.0/8');
    await agent.getByRole('button', { name: 'Netzbereiche speichern: Agent' }).click();

    const rueckfrage = fenster(page, 'Verbundene Agenten werden getrennt');
    await expect(rueckfrage.getByText('web01')).toBeVisible();
    await expect(rueckfrage.getByText('203.0.113.9')).toBeVisible();
    await rueckfrage.getByRole('button', { name: 'Speichern und trennen' }).click();
    await expect(agent.getByText('Netzbereiche gespeichert, 1 Agent getrennt')).toBeVisible();
    expect(bodies.map((b) => b.confirm)).toEqual([false, true]);
  });
});

test.describe('Let\'s Encrypt', () => {
  test('die Namen kommen aus den Adressen, und die Karte verlangt die Nutzungsbedingungen, bevor sich speichern lässt', async ({ page }) => {
    const karte = await oeffneLetsEncrypt(page);
    await expect(karte.getByText('Aus', { exact: true })).toBeVisible();
    // HTTP-01 ist vorgewählt und als Text erklärt.
    await expect(karte.getByText('Port 80 muss dafür aus dem Internet auf LCM zeigen.', { exact: false })).toBeVisible();
    // Es gibt kein Feld für Namen mehr.
    await expect(karte.getByRole('textbox', { name: 'Namen' })).toHaveCount(0);
    const speichern = karte.getByRole('button', { name: 'Speichern', exact: true });
    await expect(speichern).toBeDisabled();
    await expect(karte.getByRole('button', { name: 'Jetzt ausstellen' })).toBeDisabled();

    // Ohne Adresse mit einem Namen gibt es nichts auszustellen.
    await karte.getByRole('switch', { name: 'Zertifikate automatisch beziehen' }).click();
    await expect(speichern).toBeDisabled();
    await expect(karte.locator('[data-field="names"]')).toContainText('Trage oben mindestens eine Adresse mit einem Namen ein.');

    try {
      await speichereAdresse(page, 'https://lcm.example.com');
      await expect(karte.locator('[data-field="names"]')).toContainText('lcm.example.com');
      await expect(karte.getByText('Nimm die Nutzungsbedingungen an, um ACME einzuschalten.')).toBeVisible();
      // Die Nutzungsbedingungen sind verlinkt; die API nennt keine Adresse aus dem Verzeichnis der CA, darum der feste Link.
      const link = karte.getByRole('link', { name: 'Nutzungsbedingungen von acme-v02.api.letsencrypt.org lesen' });
      await expect(link).toHaveAttribute('href', 'https://letsencrypt.org/repository/');
      await expect(link).toHaveAttribute('target', '_blank');
      await expect(link).toHaveAttribute('rel', /noopener/);
      await karte.getByRole('switch', { name: 'Ich nehme die Nutzungsbedingungen von acme-v02.api.letsencrypt.org an' }).click();
      await expect(speichern).toBeEnabled();

      await karte.getByRole('button', { name: 'Verwerfen' }).click();
      await expect(speichern).toBeDisabled();
    } finally {
      await speichereAdresse(page, '');
    }
  });

  test('ein Hinweis zum Umstieg nennt alte und neue Namen und lässt sich bestätigen', async ({ page }) => {
    let bestaetigt = false;
    await page.route('**/api/v1/system/acme', async (route) => {
      if (route.request().method() !== 'GET') return route.fallback();
      const antwort = await route.fetch();
      const koerper = await antwort.json();
      return route.fulfill({ response: antwort, json: { ...koerper, names_changed_notice: { old: ['alt.example.com'], new: ['lcm.example.com'], was_per_name: true } } });
    });
    await page.route('**/api/v1/system/acme/names-notice/ack', (route) => {
      bestaetigt = true;
      return route.fulfill({ json: {} });
    });
    const karte = await oeffneLetsEncrypt(page);
    const meldung = hinweis(karte, 'Die Namen im Zertifikat kommen jetzt aus den Adressen');
    await expect(meldung).toContainText('alt.example.com');
    await expect(meldung).toContainText('lcm.example.com');
    await expect(meldung).toContainText('nicht mehr je Name ein Zertifikat, sondern eines für alle Namen');
    await meldung.getByRole('button', { name: 'Hinweis schließen' }).click();
    await expect.poll(() => bestaetigt).toBe(true);
  });
});

test.describe('Let\'s Encrypt per DNS-01', () => {
  test('Anbieter und Token werden gespeichert, der Token bleibt verborgen', async ({ page }) => {
    const karte = await oeffneLetsEncrypt(page);
    await expect(karte.getByRole('button', { name: 'DNS testen' })).toHaveCount(0);

    await karte.getByRole('radio', { name: /^DNS-01/ }).check({ force: true });
    await karte.getByRole('combobox', { name: 'DNS-Anbieter' }).selectOption({ label: 'Cloudflare' });
    const token = karte.getByLabel('API-Token', { exact: true });
    await expect(token).toBeVisible();
    await expect(karte.getByRole('button', { name: 'DNS testen' })).toBeDisabled();
    await token.fill('cf-test-token');
    await oeffneWeitere(karte);
    await karte.getByLabel('Wartezeit auf den Eintrag in Sekunden (optional)').fill('90');

    const speichern = karte.getByRole('button', { name: 'Speichern', exact: true });
    await expect(speichern).toBeEnabled();
    await speichern.click();
    await expect(speichern).toBeDisabled();

    // Nach dem Laden zeigt die Karte den Token nur als gespeichert, das Feld bleibt leer.
    await page.reload();
    await klappeAuf(karte, "Let's Encrypt");
    await expect(karte.getByRole('radio', { name: /^DNS-01/ })).toBeChecked();
    await expect(karte.getByRole('combobox', { name: 'DNS-Anbieter' })).toHaveValue('cloudflare');
    await expect(karte.getByText('Gespeichert. Leer lassen, um es zu behalten.')).toBeVisible();
    await expect(karte.getByLabel('API-Token', { exact: true })).toHaveValue('');
    await expect(karte.getByLabel('API-Token', { exact: true })).toHaveAttribute('placeholder', 'gespeichert');
    await oeffneWeitere(karte);
    await expect(karte.getByLabel('Wartezeit auf den Eintrag in Sekunden (optional)')).toHaveValue('90');
    await expect(karte.getByRole('button', { name: 'Speichern', exact: true })).toBeDisabled();

    // Löschen merkt das Feld zum Löschen vor, Behalten nimmt es zurück.
    await karte.getByRole('button', { name: 'Löschen' }).click();
    await expect(karte.getByText('Wird beim Speichern gelöscht.')).toBeVisible();
    await karte.getByRole('button', { name: 'Doch behalten' }).click();
    await expect(karte.getByRole('button', { name: 'Speichern', exact: true })).toBeDisabled();

    // Aufräumen: Der Server der Datei bleibt für die übrigen Tests ohne Anbieter und bei HTTP-01.
    await karte.getByRole('combobox', { name: 'DNS-Anbieter' }).selectOption({ label: 'Anbieter wählen' });
    await karte.getByRole('button', { name: 'Speichern', exact: true }).click();
    await expect(karte.getByRole('button', { name: 'Speichern', exact: true })).toBeDisabled();
    await karte.getByRole('radio', { name: /^HTTP-01/ }).check({ force: true });
    await karte.getByRole('button', { name: 'Speichern', exact: true }).click();
    await expect(karte.getByRole('button', { name: 'Speichern', exact: true })).toBeDisabled();
  });

  test('DNS testen schickt den Entwurf und zeigt Erfolg oder die Meldung des Anbieters in der Karte', async ({ page }) => {
    const gesendet: unknown[] = [];
    let antwort: { status: number; json: unknown } = { status: 200, json: { ok: true } };
    await page.route('**/api/v1/system/acme/dns-test', async (route) => {
      gesendet.push(route.request().postDataJSON());
      await new Promise((fertig) => setTimeout(fertig, 300));
      return route.fulfill(antwort);
    });
    await page.goto('/settings/general');
    // Ohne Adresse mit einem Namen gibt es nichts zu prüfen: Die Namen kommen aus den gespeicherten Adressen.
    await speichereAdresse(page, 'https://lcm.example.com');
    try {
      const karte = await oeffneLetsEncrypt(page);
      await karte.getByRole('radio', { name: /^DNS-01/ }).check({ force: true });
      const testen = karte.getByRole('button', { name: 'DNS testen' });
      await expect(testen).toBeDisabled();
      await karte.getByRole('combobox', { name: 'DNS-Anbieter' }).selectOption({ label: 'Cloudflare' });
      await karte.getByLabel('API-Token', { exact: true }).fill('cf-test-token');
      await oeffneWeitere(karte);
      await karte.getByRole('textbox', { name: 'Prüf-Nameserver (optional)' }).fill('1.1.1.1');

      await testen.click();
      await expect(karte.getByText('Wird geprüft … (bis zu einer Minute)')).toBeVisible();
      await expect(hinweis(karte, 'Der DNS-Anbieter hat den Eintrag gesetzt')).toBeVisible();
      expect(gesendet).toEqual([
        { names: ['lcm.example.com'], dns_provider: 'cloudflare', dns_values: { api_token: 'cf-test-token' }, dns_check_nameservers: ['1.1.1.1'] },
      ]);

      // Eine Änderung macht das Ergebnis ungültig.
      await karte.getByRole('textbox', { name: 'Prüf-Nameserver (optional)' }).fill('9.9.9.9');
      await expect(hinweis(karte, 'Der DNS-Anbieter hat den Eintrag gesetzt')).toHaveCount(0);

      antwort = { status: 422, json: { error: 'x', code: 'acme.dns_test_failed', params: { detail: 'cloudflare, example.com: 403 Forbidden' } } };
      await testen.click();
      await expect(hinweis(karte, 'Die Probe beim DNS-Anbieter ist gescheitert: cloudflare, example.com: 403 Forbidden')).toBeVisible();
    } finally {
      await page.goto('/settings/general');
      await speichereAdresse(page, '');
    }
  });

  test('ein fehlendes Pflichtfeld der API steht am Feld', async ({ page }) => {
    await page.route('**/api/v1/system/acme', (route) =>
      route.request().method() === 'PUT'
        ? route.fulfill({ status: 422, json: { error: 'x', code: 'acme.field_missing', params: { field: 'dns_values.api_token' } } })
        : route.fallback(),
    );
    const karte = await oeffneLetsEncrypt(page);
    await karte.getByRole('radio', { name: /^DNS-01/ }).check({ force: true });
    await karte.getByRole('combobox', { name: 'DNS-Anbieter' }).selectOption({ label: 'Cloudflare' });
    await karte.getByLabel('API-Token', { exact: true }).fill('x');
    await karte.getByRole('button', { name: 'Speichern', exact: true }).click();
    await expect(karte.getByText('Diese Angabe fehlt.')).toBeVisible();
  });

  test('DNS-01 verlangt einen Anbieter, sobald Let\'s Encrypt an ist', async ({ page }) => {
    await page.goto('/settings/general');
    await speichereAdresse(page, 'https://lcm.example.com');
    try {
      const karte = await oeffneLetsEncrypt(page);
      await karte.getByRole('switch', { name: 'Zertifikate automatisch beziehen' }).click();
      await karte.getByRole('radio', { name: /^DNS-01/ }).check({ force: true });
      await expect(karte.getByText('Wähle einen DNS-Anbieter.')).toBeVisible();
      await expect(karte.getByRole('button', { name: 'Speichern', exact: true })).toBeDisabled();
    } finally {
      await page.goto('/settings/general');
      await speichereAdresse(page, '');
    }
  });
});

test.describe('MCP: Schalter', () => {
  test('die Seite hat nur den Schalter, und er geht allein als enabled an den Server', async ({ page }) => {
    const gesendet: unknown[] = [];
    await page.route('**/api/v1/settings/mcp', (route) => {
      gesendet.push(route.request().postDataJSON());
      return route.fulfill({ json: { mcp_enabled: true } });
    });
    await page.goto('/settings/mcp');
    // TLS, Bind-Adresse und Port gibt es hier nicht mehr: MCP spricht immer TLS, die Adressen stehen unter Allgemein.
    await expect(page.getByRole('switch', { name: /^TLS/ })).toHaveCount(0);
    await expect(page.getByRole('textbox', { name: 'Bind-Adresse' })).toHaveCount(0);
    await page.getByRole('switch', { name: 'MCP-Schnittstelle' }).click();
    await speichere(page);
    expect(gesendet).toEqual([{ enabled: true }]);
  });
});

test.describe('Onboarding: Mit Agent verwenden', () => {
  test('die Unterkarte zeigt den Stand schon eingeklappt, hat keinen Schalter, aber die Adresse und den Weg zum Assistenten', async ({ page }) => {
    await page.goto('/settings/general');
    const karte = page.locator('lcm-card[data-card="agent"]');
    const kopf = karte.getByRole('button', { name: 'Mit Agent verwenden', exact: true });
    await expect(kopf).toHaveAttribute('aria-expanded', 'false');
    const imBlock = block(page).locator('[data-switch="agent"]').getByRole('switch');
    const an = (await imBlock.getAttribute('aria-checked')) === 'true';
    const stand = karte.locator('[data-field="agent-status"]');
    await expect(stand).toBeVisible();
    await expect(stand).toHaveAttribute('level', an ? 'ok' : 'unknown');
    await expect(stand).toContainText(an ? 'An' : 'Aus');

    await kopf.click();
    await expect(karte.getByRole('switch')).toHaveCount(0);
    await expect(karte.getByRole('link', { name: 'Agent-Server hinzufügen' })).toHaveAttribute('href', '/servers/join?mode=agent');

    // Der Stand folgt dem Schalter im Block, auch vor dem Speichern; ausgeschaltet führt ein Knopf dorthin.
    await imBlock.click();
    await expect(stand).toHaveAttribute('level', an ? 'unknown' : 'ok');
    if (an) await expect(karte.locator('[data-field="agent-off"]')).toContainText('Die Agenten-Schnittstelle ist ausgeschaltet.');
    await speicherleiste(page).getByRole('button', { name: 'Verwerfen' }).click();
  });
});

test.describe('Reverse-Proxy im Block', () => {
  test('die eingeklappte Karte unter Let\'s Encrypt speichert Schalter und Liste über die Einstellungen', async ({ page }) => {
    const gesendet: Record<string, unknown>[] = [];
    await page.route('**/api/v1/settings/global', async (route) => {
      if (route.request().method() === 'PATCH') gesendet.push(route.request().postDataJSON() as Record<string, unknown>);
      await route.continue();
    });
    await page.goto('/settings/general');
    const karte = page.locator('lcm-settings-proxy');
    await klappeAuf(karte, 'Reverse-Proxy');
    const schalter = karte.getByRole('switch', { name: 'Reverse-Proxy davor' });
    const liste = karte.getByRole('textbox', { name: 'Adressen des Proxys' });
    const war = (await schalter.getAttribute('aria-checked')) === 'true';
    const eintraege = await liste.inputValue();
    try {
      if (!war) await schalter.click();
      await liste.fill('0.0.0.0/0');
      await karte.getByRole('button', { name: 'Speichern', exact: true }).click();
      await expect(karte.getByText('Ein Netzbereich über alle Adressen ist nicht erlaubt: 0.0.0.0/0')).toBeVisible();
      expect(gesendet).toEqual([]);

      await liste.fill('10.0.0.1\n192.168.0.0/24');
      await karte.getByRole('button', { name: 'Speichern', exact: true }).click();
      await expect.poll(() => gesendet).toEqual([{ trust_proxy_header: true, trusted_proxies: ['10.0.0.1', '192.168.0.0/24'] }]);
    } finally {
      await page.unroute('**/api/v1/settings/global');
      await page.request.patch('/api/v1/settings/global', { data: { trust_proxy_header: war, ...(war ? { trusted_proxies: eintraege.split('\n').filter(Boolean) } : {}) } });
    }
  });
});

test.describe('Server aufnehmen: Agent ausgeschaltet', () => {
  test('der Assistent zeigt statt des Formulars den Weg zu den Einstellungen', async ({ page }) => {
    await page.route('**/api/v1/system/info', async (route) => {
      const antwort = await route.fetch();
      return route.fulfill({ response: antwort, json: { ...(await antwort.json()), agent_enabled: false } });
    });
    await page.goto('/servers/join?mode=agent');
    const hinweis = page.locator('lcm-notice[data-field="agent-off"]');
    await expect(hinweis).toContainText('Der Agent ist ausgeschaltet');
    await expect(hinweis.getByRole('link', { name: 'Zu Einstellungen → Allgemein' })).toHaveAttribute('href', '/settings/general');
    await expect(page.getByRole('button', { name: 'Anlegen und Token erzeugen' })).toHaveCount(0);
    await expect(page.getByRole('textbox', { name: 'Name' })).toHaveCount(0);
  });
});
