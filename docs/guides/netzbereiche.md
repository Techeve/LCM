---
sidebar:
  order: 25
title: Erlaubte Netzbereiche
description: Legt fest, aus welchen Netzen sich Browser, Agenten und MCP-Clients mit LCM verbinden dürfen, je Dienst, mit Schutz vor Selbstaussperrung.
---

Standardmäßig darf jede Adresse, die einen Port von LCM erreicht, dort anklopfen.
Wer ankommt, entscheidet danach die Anmeldung. Mit **erlaubten Netzbereichen**
legst du zusätzlich fest, aus welchen Netzen LCM überhaupt antwortet. Das gilt
je Dienst getrennt:

| Dienst | Wer sich verbindet |
|---|---|
| **Weboberfläche** | Browser und API-Clients (Weboberfläche und REST-API) |
| **Agent** | die `lcm-agent` auf deinen Servern |
| **MCP** | KI-Assistenten über die MCP-Schnittstelle |

Eine Anfrage von einer Adresse außerhalb der Liste beantwortet LCM mit `403`,
noch bevor die Anmeldung oder ein Controller läuft.

:::note[Nicht dasselbe wie die IP-Allowlists]
Die [IP-Allowlists](/guides/allowlists/) unter *Einstellungen → Allowlists* sind
benannte Listen für Firewall, fail2ban und CrowdSec auf den **verwalteten
Servern**. Die erlaubten Netzbereiche dieser Seite schützen **LCM selbst**.
:::

## Einträge

Eine leere Liste erlaubt **alle** Netze. Das ist der Standard. Jeder Eintrag ist:

- eine IP-Adresse (`203.0.113.5`, `2001:db8::1`),
- ein Netz in CIDR-Schreibweise (`10.0.0.0/8`, `fd00::/8`),
- oder eines der Wörter `localhost` (die lokale Maschine) und `private`
  (private Netze nach RFC 1918, IPv6-ULA, Link-Local, dazu die lokale
  Maschine).

Eine Liste hat höchstens 64 Einträge. LCM schreibt sie beim Speichern einheitlich
(einzelne Adressen werden zu `/32` bzw. `/128`) und entfernt Doppelte.

## In der Oberfläche einstellen

*Einstellungen → Allgemein*, Block **Öffentliche Adressen**: Hinter der Adresse
von Weboberfläche, Agent und MCP steht ein Zahnrad (*Zertifikat & Sicherheit*).
Der Dialog zeigt unter dem Zertifikat die **erlaubten Netzbereiche** dieses
Dienstes, mit eigenem Feld und eigener Schaltfläche *Netzbereiche speichern*. Ein
Eintrag je Zeile, Speichern wirkt **sofort**, ohne Neustart. Die Änderung steht im
Audit-Log (`settings.network.update`).

Im Einrichtungsassistenten stellst du dieselben Listen im Schritt *Wer darf LCM
erreichen?* ein ([Öffentliche Adressen & Adressmodus](/guides/adressen/#einrichtungsassistent)).

Das Recht dazu, `system:endpoints`, hat nur ein **Admin**. Weder ein Manager mit
`settings:manage` noch ein anderes Konto kann Netzbereiche ändern.

### Schutz vor Selbstaussperrung

- **Weboberfläche:** LCM lehnt eine Liste ab, in der deine eigene Adresse fehlt.
  Auch die lokale Adresse gilt nur, wenn sie in der Liste steht. Nimm
  `localhost` mit auf, sonst schlägt zum Beispiel der Docker-Healthcheck fehl,
  der LCM über `localhost` abfragt.
- **Agent:** Engst du den Bereich ein, zeigt LCM die Agenten, die gerade von
  außerhalb verbunden sind. Erst nach deiner Bestätigung speichert LCM und
  **trennt** sie. Sie können sich erst wieder verbinden, wenn ihre Adresse
  erlaubt ist.
- **MCP:** Der Bereich gilt ab der nächsten Anfrage.

## In der `config.json`

Die Listen stehen auch in der `config.json`, im Gleichklang mit der Oberfläche
([Öffentliche Adressen & Adressmodus](/guides/adressen/#die-configjson-im-gleichklang)):

```json
{
  "allowed_ips": ["private"],
  "agent_allowed_ips": ["203.0.113.0/24", "198.51.100.17"],
  "mcp_allowed_ips": ["10.0.0.0/8"]
}
```

| Schlüssel | Dienst |
|---|---|
| `allowed_ips` | Weboberfläche |
| `agent_allowed_ips` | Agent |
| `mcp_allowed_ips` | MCP |

Speicherst du eine Liste in der Oberfläche, schreibt LCM sie in die Datei. Änderst
du sie in der Datei von Hand, übernimmt LCM sie beim Start, wenn sie gültig ist.
Die Oberfläche zeigt die Bereiche immer bearbeitbar. So kommst du nach einer
Aussperrung über die Konsole wieder hinein: Liste in der Datei leeren und
`sudo systemctl restart lcm`.

## Wenn du dich ausgesperrt hast

Dieser Befehl setzt die Liste zurück, in den Einstellungen und in der
`config.json`. Er läuft auf dem LCM-Host und braucht kein Konto in LCM:

```sh
sudo lcm network status            # zeigt je Dienst die Liste
sudo lcm network reset ui          # Weboberfläche: wieder alle Netze erlaubt
sudo lcm network reset agent
sudo lcm network reset mcp
```

`lcm network status` sieht so aus:

```text
Erlaubte Netzbereiche je Dienst:
  Weboberfläche  10.0.0.0/8, 192.168.1.0/24
  Agent          alle Netze erlaubt
  MCP            10.0.0.0/8
```

Die Befehle sprechen über die Steuerbuchse `/var/lib/lcm/control.sock`, die nur
der Dienstbenutzer und root öffnen dürfen. Darum `sudo`. Das Zurücksetzen steht im
Audit-Log (`settings.network.reset`).

## Hinter einem Reverse-Proxy

LCM prüft die Adresse, die es als Client erkennt. Steht ein Proxy davor, ist
das ohne weitere Einstellung die Adresse des Proxys, und die Liste würde alle
Clients gleich behandeln. Schalte deshalb `trust_proxy_header` ein **und** trage
die Adresse des Proxys in `trusted_proxies` ein (Einstellungen, oder beides in der
`config.json`). Dann zählt der Eintrag aus
`X-Forwarded-For`, aber nur von diesem Proxy. Details und Beispiele:
[Reverse-Proxy & Absicherung von außen](/guides/reverse-proxy/#trusted_proxies-wem-x-forwarded-for-geglaubt-wird).
Das gilt für alle drei Dienste.

## Was immer erreichbar bleibt

- **Der öffentliche Agent-Download** (`/api/v1/agent/download/`) auf der
  Weboberfläche. Wer einen Server aufnimmt, lädt den Agent von einem Netz aus,
  das du nicht kennen musst.
- **Die Prüfung von Let's Encrypt** unter `/.well-known/acme-challenge/` auf dem
  HTTP-01-Listener (Port 80). Die Zertifizierungsstelle kommt aus dem Internet.
  Der Port ist nur während der Prüfung offen und beantwortet sonst nichts
  ([Zertifikate](/guides/zertifikate/#http-01)).
