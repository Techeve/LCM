---
sidebar:
  order: 23
title: MCP-Schnittstelle (KI-Agenten)
description: KI-Agenten read-only Server-Eigenschaften bereitstellen - über einen separaten, verschlüsselten und authentifizierten Port.
---

Die **MCP-Schnittstelle** (Model Context Protocol) stellt KI-Agenten **read-only**
Eigenschaften der von LCM verwalteten Server bereit. Sie ist bewusst eng
gefasst:

- **Keine Geheimnisse.** Passwörter, Login-Benutzer, private/öffentliche
  Schlüssel, Host-Key-Fingerprints und Token werden **nie** ausgegeben. Die
  Daten laufen durch ein kuratiertes Whitelist-DTO.
- **Nur lesen.** Es gibt keine schreibenden oder konfigurierenden Tools.
- **Eigener Port + Pflicht-Authentifizierung** per MCP-API-Key (Bearer-Token).
- **Immer verschlüsselt.** MCP spricht nur HTTPS.
- **Vollständig an-/abschaltbar** mit einem Schalter.

## Aktivieren

Die Seite *Einstellungen → MCP* zeigt den Schalter **MCP-Schnittstelle**, den
Stand, die wirksame Adresse mit Kopierknopf, die MCP-Schlüssel und ein fertiges
Beispiel.

1. **MCP-Schnittstelle** einschalten. Der separate Listener startet sofort (kein
   Neustart nötig); Ausschalten stoppt ihn ebenso sofort. Dieselbe Einstellung
   steht in *Einstellungen → Allgemein* unter der MCP-Adresse
   ([Öffentliche Adressen & Adressmodus](/guides/adressen/#agent-und-mcp-an--und-ausschalten)).
2. **Adresse und Port** stellst du in *Einstellungen → Allgemein* im Block
   **Öffentliche Adressen** ein, nicht auf dieser Seite (Vorgabe Port `9330`). Die
   Seite zeigt nur, was daraus folgt.
3. Einen **MCP-API-Key** erzeugen (eigene, getrennte Liste auf derselben Seite).
   Der Klartext wird **nur einmal** angezeigt.

Im Adressmodus C hat MCP einen eigenen Namen am gemeinsamen Port. Fehlt der Name,
ist MCP aus, und der Schalter bleibt gesperrt, bis du unter *Einstellungen →
Allgemein* einen Namen einträgst.

## Verschlüsselung und Adresse

MCP spricht **immer HTTPS**, einen Schalter dafür gibt es nicht mehr. LCM wählt
das Zertifikat aus dem Pool nach dem Namen, unter dem der Client MCP aufruft.
Der Listener bindet an dieselbe Adresse wie die Weboberfläche (`host` in der
`config.json`), ein eigenes Feld dafür gibt es nicht. Clients rufen
`https://<Name>:9330/mcp` auf, über den gemeinsamen Port (Modus C) ohne Port.

:::caution[Aus `http://` wird `https://`]
Bis 2.2 sprach MCP ohne TLS einfaches HTTP und lauschte nur lokal. Ab 2.3 gibt es
das nicht mehr, auch auf bestehenden Installationen. Ein MCP-Client oder ein
Reverse-Proxy, der noch `http://` aufruft, muss auf `https://` umstellen. Der
Proxy muss dann auch zu MCP HTTPS sprechen.
:::

Welches Zertifikat für den Namen gilt, stellst du am Zahnrad der MCP-Adresse
unter *Einstellungen → Allgemein* ein. Mit Let's Encrypt trägt das Zertifikat auch
den MCP-Namen, sobald MCP an ist. Alles dazu steht unter
[Zertifikate, Let's Encrypt & Port 443](/guides/zertifikate/#mcp-mit-tls).
Wer sich mit MCP verbinden darf, begrenzen die
[Erlaubten Netzbereiche](/guides/netzbereiche/).

## Sicherheit: isolierter Scope

Ein MCP-API-Key hat den **eigenen Scope `mcp`** und ist strikt vom Rest der
Anwendung getrennt:

- Er funktioniert **ausschließlich** auf dem MCP-Listener (`POST /mcp`).
- An der **REST-API/UI** wird ein MCP-Key mit **403 Forbidden** abgewiesen - er
  kann dort weder lesen noch schreiben.
- Umgekehrt gelten normale API-Keys (`read`/`readwrite`) **nicht** auf dem
  MCP-Endpunkt.
- Fehlt der Bearer-Token oder ist er ungültig, antwortet der Endpunkt mit
  **401 Unauthorized** (Header `WWW-Authenticate: Bearer`).

So kann ein für read-only-Serverdaten gedachter Agent-Key niemals versehentlich
schreibend wirken.

## Tools

| Tool | Zweck |
| --- | --- |
| `list_servers` | Alle Server mit ihren read-only Eigenschaften (OS, Version, Status, Erreichbarkeit, Hardware, Update-Stand, Sicherheitslage). |
| `get_server` | Ein Server per ID oder exaktem Namen. |
| `fleet_summary` | Aggregat: Anzahl gesamt, erreichbar, Ampel-Verteilung, Server mit Updates/kritischen CVEs. |

### Rückgabefelder (`list_servers` / `get_server`)

Beide Tools liefern dieselbe Server-Sicht. Die Whitelist umfasst **ausschließlich**
diese Felder - bewusst keine Zugangsdaten:

| Feld | Bedeutung |
| --- | --- |
| `id`, `name`, `host`, `ip_addresses` | Kennung & Adressen |
| `os_name`, `os_version`, `os_id` | Betriebssystem |
| `transport` | `ssh`, `agent` oder `routeros` |
| `reachable`, `last_seen_at` | Erreichbarkeit |
| `status` | Ampel: `excellent` / `green` / `yellow` / `red` |
| `insights` | Klartext-Befunde zur Ampel (z. B. „Firewall nicht aktiv") |
| `cpu_model`, `cpu_cores` | CPU |
| `mem_total_mb`, `mem_used_mb` | Arbeitsspeicher |
| `disk_total_mb`, `disk_used_mb`, `disk_usage_percent` | Speicher |
| `kernel_version`, `virtualization` | Kernel & Virtualisierung |
| `outdated_packages`, `reboot_required` | Paket-/Update-Stand |
| `routeros_channel`, `routeros_latest_version`, `routeros_update_available` | RouterOS-spezifisch |
| `cve_critical`, `cve_high` | Schwachstellen-Zähler |
| `ssh_hardened`, `firewall_active`, `firewall_tool` | Sicherheitslage |
| `hardening_index`, `proxmox_type` | Härtungs-Index & Proxmox-Rolle |

### `fleet_summary`

Aggregiert alle sichtbaren Server. Beispiel-Ergebnis:

```json
{
  "total": 12,
  "reachable": 11,
  "unreachable": 1,
  "by_status": { "excellent": 5, "green": 3, "yellow": 3, "red": 1 },
  "updates_available": 4,
  "servers_with_critical_cve": 1
}
```

`updates_available` zählt Server mit veralteten Paketen **oder** einem verfügbaren
RouterOS-Update; `servers_with_critical_cve` zählt Server mit mindestens einer
kritischen CVE.

## Anbinden

Die Einstellungsseite zeigt ein fertiges Beispiel mit deiner wirksamen Adresse.
Für einen MCP-Client mit HTTP-Transport (z. B. Claude Desktop /
VS-Code-MCP-Clients):

```json
{
  "mcpServers": {
    "lcm": {
      "type": "http",
      "url": "https://lcm.example.com:9330/mcp",
      "headers": { "Authorization": "Bearer <DEIN_MCP_KEY>" }
    }
  }
}
```

Mehrere Instanzen lassen sich parallel eintragen - jede mit eigenem Key:

```json
{
  "mcpServers": {
    "lcm-prod": {
      "type": "http",
      "url": "https://mcp.example.com/mcp",
      "headers": { "Authorization": "Bearer <PROD_KEY>" }
    },
    "lcm-lab": {
      "type": "http",
      "url": "https://lab.example.com:9330/mcp",
      "headers": { "Authorization": "Bearer <LAB_KEY>" }
    }
  }
}
```

(Die `lcm-prod`-URL steht hier für Modus C auf dem gemeinsamen Port 443, `lcm-lab`
für eigene Ports.)

Direkt testen (JSON-RPC 2.0 über curl). Trägt der Name kein Zertifikat einer
öffentlichen Stelle, vertraut curl dem LCM-Zertifikat erst mit `--cacert`. Die
Datei lädst du als Admin von `GET /api/v1/system/tls/ca.pem`:

```bash
# 1) verfügbare Tools auflisten
curl -s --cacert lcm-ca.pem https://lcm.example.com:9330/mcp \
  -H "Authorization: Bearer <DEIN_MCP_KEY>" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'

# 2) einen Server per Name abrufen
curl -s --cacert lcm-ca.pem https://lcm.example.com:9330/mcp \
  -H "Authorization: Bearer <DEIN_MCP_KEY>" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call",
       "params":{"name":"get_server","arguments":{"server":"web01"}}}'

# 3) Flotten-Aggregat
curl -s --cacert lcm-ca.pem https://lcm.example.com:9330/mcp \
  -H "Authorization: Bearer <DEIN_MCP_KEY>" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":3,"method":"tools/call",
       "params":{"name":"fleet_summary","arguments":{}}}'
```

## Protokoll & Betrieb

- Der Endpunkt spricht **JSON-RPC 2.0** über `POST /mcp` (MCP „Streamable HTTP",
  zustandslos). Methoden: `initialize`, `tools/list`, `tools/call`, `ping` sowie
  die Notifications `notifications/initialized` / `notifications/cancelled`
  (quittiert mit HTTP 202).
- Angebotene Protokollversion: `2025-06-18`; fordert der Client eine andere an,
  spiegelt der Server sie beim `initialize`.
- `GET /mcp` wird nicht genutzt (kein server-initiiertes SSE) und antwortet mit
  **405 Method Not Allowed**.
- Schalter und Port sind jederzeit zur Laufzeit umschaltbar, ohne Neustart der
  Anwendung. Der Port steht in *Einstellungen → Allgemein*.
- Über die API (`PUT /api/v1/settings/mcp`) nimmt LCM nur noch `enabled` an.
  `bind_host`, `port` und `tls` älterer Clients überliest es.
