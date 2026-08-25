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

Zwei gleichwertige Wege:

| Methode | Header | Für |
|---|---|---|
| Session-Token | `Authorization: Bearer <jwt>` | interaktive Nutzung, Skripte mit Login |
| API-Key | `X-API-Key: <key>` | Service-zu-Service, Automatisierung |

**Session-Login:**

```sh
curl -s -X POST https://lcm.example.com/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"…"}'
# → {"token":"eyJ…", "user": {...}}
```

Ist für den Account 2FA aktiv, liefert `/auth/login` statt eines Tokens eine
Challenge; `POST /auth/login/2fa` mit dem TOTP-Code schließt den Login ab.

**API-Key erzeugen** (unter *Einstellungen → API-Keys*, benötigt
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
TOKEN=$(curl -s -X POST https://lcm.example.com/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"…"}' | jq -r .token)

# Alle Server auflisten
curl -s https://lcm.example.com/api/v1/servers \
  -H "Authorization: Bearer $TOKEN" | jq '.[].name'

# Sicherheits-Updates auf einem Server anstoßen (asynchron, liefert eine Job-ID)
curl -s -X POST https://lcm.example.com/api/v1/servers/3/packages/upgrade-security \
  -H "Authorization: Bearer $TOKEN"
# → {"status":"started", "job_id":"…", "job_name":"…"}

# Job-Fortschritt/-Ergebnis abfragen
curl -s https://lcm.example.com/api/v1/jobs/history?server_id=3 \
  -H "Authorization: Bearer $TOKEN"
```

Server-verändernde Aktionen (Updates, Härtung, Docker-Aktionen, …) laufen
**asynchron als Job**: der Endpunkt liefert sofort eine `job_id`, der
tatsächliche SSH-Kontakt läuft im Hintergrund. Fortschritt und
Konsolen-Ausgabe stehen unter `GET /jobs/:id/ssh-output` bzw. in der
Job-Historie.

## Vollständige Referenz (OpenAPI)

Alle **258 Endpunkte** mit Methode, Pfad, benötigter Berechtigung und (soweit
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
