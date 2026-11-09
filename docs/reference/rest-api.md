---
sidebar:
  order: 22
title: REST-API
description: Authentifizierung, Berechtigungen, Beispiel-Requests und die maschinenlesbare OpenAPI-Spezifikation.
---

Das Web-Frontend nutzt **exakt dieselbe** REST-API, die auch für eigene
Skripte, Automatisierung oder Integrationen zur Verfügung steht - es gibt
keine separate interne Schnittstelle. Alle Endpunkte liegen unter `/api/v1`.

## Authentifizierung

Skripte, Automatisierung und Integrationen nutzen den **API-Key**; die
Weboberfläche meldet sich über eine Browser-Sitzung an:

| Methode | Header | Für |
|---|---|---|
| API-Key | `X-API-Key: <key>` | Skripte, Automatisierung, Service-zu-Service |
| Browser-Sitzung | `X-LCM-Session: <wert>` oder Cookie | die Weboberfläche |

`Authorization: Bearer` gilt an der REST-API **nicht** als Anmeldung. Einen
Login mit Benutzername und Passwort für Skripte gibt es nicht - dafür ist der
API-Key da. (Der MCP-Port ist ein eigener Listener und nimmt den API-Key vom
Scope `mcp` als Bearer-Token, siehe [MCP-Schnittstelle](/guides/mcp/).)

**Browser-Sitzung:** Wie die Weboberfläche die Sitzung trägt, stellt der Betreiber
unter *Einstellungen → Allgemein → Anmeldung im Browser* ein. Mehr dazu im
[Sicherheitsmodell](/reference/security-model/#anmeldung-im-browser).
`POST /auth/login` liefert in der Standard-Variante den Sitzungswert im
Antwortrumpf (`session_token`), den die Oberfläche als `X-LCM-Session` sendet; in
der Cookie-Variante setzt es stattdessen das Cookie `__Host-lcm_session`. Ist für
den Account 2FA aktiv, liefert `/auth/login` eine Challenge; `POST /auth/login/2fa`
mit dem TOTP-Code schließt den Login ab.

**API-Key erzeugen** (unter *Einstellungen → API-Schlüssel*, benötigt
`apikeys:manage`): Der Klartext-Key wird **nur einmal** in der Antwort
angezeigt - sicher aufbewahren, ein verlorener Key lässt sich nicht erneut
anzeigen, nur widerrufen und neu erzeugen.

```sh
curl -s https://lcm.example.com/api/v1/servers \
  -H 'X-API-Key: lcm_xxxxxxxxxxxxxxxx'
```

## Berechtigungen (RBAC)

Jeder Endpunkt prüft einen festen Berechtigungs-Scope (z. B. `servers:write`,
`settings:manage`) - bei fehlender Berechtigung antwortet die API mit `403`.
Bei Servern wird die **sichtbare Datenmenge** zusätzlich auf die
Servergruppen des jeweiligen Benutzers eingeschränkt (Manager sehen nur ihre
zugewiesenen Gruppen, Admins alles). Details zu Rollen und Scopes:
[Sicherheitsmodell](/reference/security-model/).

## Beispiel-Workflow

```sh
# Der Key kommt aus Einstellungen → API-Schlüssel (siehe oben)
export LCM_KEY='lcm_xxxxxxxxxxxxxxxx'

# Alle Server auflisten
curl -s https://lcm.example.com/api/v1/servers \
  -H "X-API-Key: $LCM_KEY" | jq '.items[].name'

# Sicherheits-Updates auf einem Server anstoßen (asynchron, liefert eine Job-ID)
curl -s -X POST https://lcm.example.com/api/v1/servers/3/packages/upgrade-security \
  -H "X-API-Key: $LCM_KEY"
# → {"status":"started", "job_id":"…", "job_name":"…"}

# Job-Fortschritt/-Ergebnis abfragen
curl -s https://lcm.example.com/api/v1/jobs?server_id=3 \
  -H "X-API-Key: $LCM_KEY"
```

Server-verändernde Aktionen (Updates, Härtung, Docker-Aktionen, …) laufen
**asynchron als Job**: der Endpunkt liefert sofort eine `job_id`, der
tatsächliche SSH-Kontakt läuft im Hintergrund. Fortschritt und
Konsolen-Ausgabe stehen unter `GET /jobs/:id/ssh-output` bzw. in der
Job-Historie.

## Vollständige Referenz (OpenAPI)

Alle **290 Endpunkte** (Kombinationen aus Methode und Pfad, verteilt auf 258 Pfade) mit benötigter Berechtigung und (soweit
im Quellcode ersichtlich) Request-Body stehen maschinenlesbar als
OpenAPI-3.0-Spezifikation zum Download bereit:

**[openapi.yaml](/static/openapi.yaml)**

Die Spezifikation wird von Hand gepflegt - deshalb stehen dort Erklärungen,
die kein Generator liefern kann. Damit sie nicht hinterherhinkt, gleicht ein
Test bei jedem Lauf die Routen des Servers mit den beschriebenen Pfaden ab:
Ein neuer Endpunkt ohne Eintrag lässt den Testlauf scheitern.

In einem beliebigen OpenAPI-Werkzeug öffnen (z. B.
[Swagger Editor](https://editor.swagger.io/), Postman-Import, Insomnia,
Bruno) für eine durchsuchbare, interaktive Ansicht mit allen Endpunkten nach
Bereich gruppiert (Server, Servergruppen, Docker, Sicherheit, Jobs,
Benutzer, Einstellungen, …).

:::note[Browsable API-Referenz]
Eine direkt in diese Doku eingebettete, browsbare API-Referenz ist
vorbereitet (der Doku-Builder unterstützt das über `starlight-openapi`),
wartet aber noch auf eine kleine Korrektur im gemeinsam genutzten
Doku-Baustein - bis dahin die Datei oben lokal öffnen.
:::
