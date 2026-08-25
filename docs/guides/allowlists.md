---
sidebar:
  order: 20
title: IP-Allowlists
description: Benannte, wiederverwendbare Listen von Quell-IPs für Firewall, fail2ban und CrowdSec.
---

IP-Allowlists sind **benannte, wiederverwendbare Listen von Quell-IPs bzw.
-Netzen** (IPv4/IPv6, inkl. CIDR). Sie sind ein **gemeinsamer Pool**: nicht an
ein einzelnes Feature gebunden, sondern an mehreren Stellen auswählbar. Statt an
zehn Firewall-Regeln dieselben Büro-IPs einzutippen, pflegst du sie **einmal** in
einer Liste `Büro` und wählst diese überall aus.

## Verwalten

*Einstellungen → Allowlists*: Listen anlegen, bearbeiten, löschen. Jede Liste
hat einen Namen und beliebig viele Einträge (eine IP oder ein CIDR je Zeile,
oder komma-/leerzeichengetrennt). Beim Speichern werden die Einträge validiert,
**kanonisiert**, dedupliziert und sortiert.

Beispiel - eine gemischte v4/v6-Liste `Admins` beim Eintippen:

```
203.0.113.7
10.0.0.0/24
2001:db8:1::/48
::1
198.51.100.42/32
```

Nach dem Speichern liegen die Einträge kanonisch vor (z. B. wird `/32` bzw. eine
Einzeladresse einheitlich normalisiert, Dubletten fallen weg). Ungültige Einträge
(Tippfehler, kaputtes CIDR) werden **abgewiesen** - die ganze Liste wird dann
nicht gespeichert, mit einer klaren Fehlermeldung.

Weitere sinnvolle Listen als Vorlage:

| Listenname | Beispiel-Einträge | Zweck |
| --- | --- | --- |
| `Büro` | `203.0.113.0/24`, `2001:db8:office::/48` | feste Standort-Ranges |
| `VPN` | `10.8.0.0/24` | eingewählte Administratoren |
| `Monitoring` | `198.51.100.10`, `198.51.100.11` | Prüf-/Scraper-Hosts |
| `LCM-Host` | `203.0.113.5` | die LCM-Instanz selbst (z. B. für die zentrale CrowdSec-LAPI) |

## Verwenden

Die gleichen Listen lassen sich an drei Stellen per **Mehrfachauswahl**
zuordnen - die Auswahl ergibt die Vereinigung der IPs:

- **Firewall-Regel** (Server-Firewall-Dialog / Gruppen-Regel): Wird an einer
  Regel eine Allowlist gewählt, gibt die Regel den Port **nur für die Quell-IPs
  dieser Listen** frei - sonst niemand. Ohne Allowlist gilt die Regel wie
  bisher für alle Quellen. Umgesetzt als `ufw allow … from …`, firewalld
  `source address="…"` bzw. nftables `ip/ip6 saddr { … }`. An derselben Regel
  lassen sich zusätzlich **eigene IPs/Netze** direkt eintragen - sie werden mit
  den Allowlist-IPs vereinigt (siehe [Firewall](/guides/firewall)).
- **fail2ban**: die Allowlist-IPs landen in `ignoreip` (werden nie gesperrt).
- **CrowdSec**: die Allowlist-IPs bilden eine Parser-Whitelist (keine
  Entscheidungen gegen diese Quellen).

Die LCM-Quell-IP kommt bei fail2ban/CrowdSec zusätzlich automatisch dazu
(Aussperr-Schutz).

## Zusammenspiel mit eigenen IPs an der Firewall

An einer Firewall-Regel kannst du **benannte Listen** und **regel-eigene IPs**
frei kombinieren - das Ergebnis ist immer die Vereinigung. Ein Beispiel für eine
SSH-Regel (Port 22/TCP):

- gewählte Allowlists: `Büro` (`203.0.113.0/24`) + `VPN` (`10.8.0.0/24`)
- regel-eigene IPs im Feld: `198.51.100.9, 2001:db8:beef::1`

Wirksam freigegeben ist SSH dann für die Quellen
`203.0.113.0/24`, `10.8.0.0/24`, `198.51.100.9` und `2001:db8:beef::1` -
alle anderen Quellen werden geblockt. Die IP-Version ergibt sich je Eintrag
automatisch (v4 bzw. v6). Ändert sich später der Inhalt einer der Listen (z. B.
ein neuer VPN-Range), wendet die **Firewall-Grundsatz-Regel** den Regelsatz beim
nächsten Lauf automatisch neu an - die einzelnen Regeln müssen nicht angefasst
werden.

:::note[Gelöschte oder leere Listen]
Referenziert eine Firewall-Regel nur Allowlists, die leer sind oder gelöscht
wurden, gibt LCM den Port **nicht** frei (er bleibt zu) - nie versehentlich
„von überall". Bei fail2ban/CrowdSec tragen leere Listen schlicht keine
zusätzlichen IPs bei.
:::

## Best Practice

- **Wenige, sprechende Listen statt vieler IPs an Regeln.** Eine Liste `Büro`,
  die du überall auswählst, ist an einer Stelle pflegbar; dieselbe IP an zehn
  Regeln ist zehn Fehlerquellen.
- **CIDR statt Einzeladressen**, wo die Quelle ein ganzes Netz ist
  (`203.0.113.0/24` statt 254 Einzelzeilen).
- **v4 und v6 zusammen führen.** Hat ein Standort Dual-Stack, gehören beide
  Präfixe in dieselbe Liste - sonst greift die Regel nur für eine der beiden
  IP-Versionen.
- **Die LCM-Instanz nicht vergessen.** Wenn du SSH per Allowlist einschränkst,
  muss die IP, mit der **LCM** den Server erreicht, enthalten sein - sonst
  verlierst du den Verwaltungszugang. (An der SSH-Regel schützt LCM zwar den
  SSH-Port grundsätzlich, aber eine Quell-Einschränkung kann LCM selbst
  aussperren.)
- **Für die zentrale CrowdSec-LAPI** eine Liste mit den Quell-IPs der verwalteten
  Server pflegen und den LAPI-Port (8080) nur für diese freigeben (siehe
  [Sicherheit-Tools](/guides/security-tools)).
- **Leeren = schließen.** Willst du eine Freigabe temporär entziehen, leere die
  Liste - die referenzierenden Firewall-Regeln schließen den Port dann sicher,
  statt ihn zu öffnen.
