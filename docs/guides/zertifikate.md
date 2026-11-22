---
sidebar:
  order: 24
title: Zertifikate, Let's Encrypt & Port 443
description: Eigene Zertifikate oder Let's Encrypt für Weboberfläche, Agent und MCP, ohne Reverse-Proxy, auf eigenen Ports oder gemeinsam auf 443.
---

LCM spricht ab Werk HTTPS mit einem Zertifikat aus seiner eigenen
Zertifizierungsstelle. Das Zertifikat nennt nur `localhost` und `127.0.0.1`,
deshalb warnt jeder Browser, der LCM über einen echten Namen aufruft. Wer LCM
ohne Reverse-Proxy betreibt, hinterlegt dafür ein eigenes Zertifikat oder lässt
LCM eines bei Let's Encrypt holen und erneuern. Ein Reverse-Proxy bleibt
möglich, ist aber nicht mehr nötig
([Reverse-Proxy & Absicherung von außen](/guides/reverse-proxy/)). Welche Namen
und Ports LCM nutzt, legst du im Adressmodus fest
([Öffentliche Adressen & Adressmodus](/guides/adressen/)).

## So wählt LCM das Zertifikat

LCM hält alle Zertifikate in einem **Pool**. Bei jedem Verbindungsaufbau
schaut es in den Namen, den der Client anfragt (SNI), und liefert das
Zertifikat aus, das diesen Namen nennt. Das gilt für die Weboberfläche, den
Agent-Port und MCP gleichermaßen.

- **Passt kein Zertifikat** oder ruft der Client LCM **per IP-Adresse** auf,
  liefert LCM sein eigenes Zertifikat aus, das **LCM-Zertifikat**. Es bleibt
  immer der Rückfall. Bestehende Agenten reißen deshalb nie ab.
- **Nennen mehrere Zertifikate denselben Namen**, gilt diese Rangfolge:
  `config.json`, dann hochgeladen, dann Let's Encrypt (ACME). Ein abgelaufenes
  Zertifikat überspringt LCM, solange ein anderes passt.
- **Ein Zertifikat darf mehrere Namen tragen.** Ein Zertifikat mit drei Namen
  genügt für drei Dienste, drei Zertifikate mit je einem Namen ebenso.
- **Jedes Zertifikat wird beim Laden geprüft:** PEM-Format, der Schlüssel
  passt, es gilt gerade jetzt, es darf für Server dienen (serverAuth) und es
  nennt mindestens einen Namen. Fehlt in der Datei das Zertifikat des
  Ausstellers, lädt LCM es trotzdem und warnt, denn manche Clients lehnen eine
  unvollständige Kette ab. Besteht ein Zertifikat die Prüfung nicht, bleibt der
  bisherige Stand aktiv.

Das LCM-Zertifikat stellt LCM aus seiner eigenen Zertifizierungsstelle aus
(Laufzeit 90 Tage, monatlich erneuert). Stattdessen kannst du über
`tls_ca_cert` und `tls_ca_key` in der `config.json` eine Zertifizierungsstelle
deiner eigenen Infrastruktur angeben.

## Zertifikat und Adressmodus

Welches Zertifikat du brauchst, hängt vom Adressmodus ab
([Öffentliche Adressen & Adressmodus](/guides/adressen/)):

| Modus | Namen im Zertifikat |
|---|---|
| **A: Eigene Ports, eigene Namen** | Ein Name je Dienst. Ein Zertifikat mit allen Namen genügt, drei einzelne ebenso. Hinter einem Reverse-Proxy braucht LCM oft gar keines. |
| **B: Eigene Ports, ein Name** | Der eine Name. Ein Zertifikat gilt für Weboberfläche, Agent und MCP. |
| **C: Ein Port, eigene Namen** | Die Namen der Dienste: Oberfläche immer, Agent und MCP, wenn sie einen Namen haben. |

Im Beispiel liegt das Zertifikat als Datei vor. In Modus B sieht die
`config.json` so aus, die Ports bleiben bei den Vorgaben:

```json
{
  "host": "0.0.0.0",
  "port": 9310,
  "agent_host": "0.0.0.0",
  "agent_port": 9320,
  "tls_certificates": [
    { "cert": "/etc/lcm/tls/lcm.pem", "key": "/etc/lcm/tls/lcm.key" }
  ]
}
```

Das Zertifikat nennt `lcm.example.com`. Browser rufen
`https://lcm.example.com:9310` auf, Agenten verbinden sich mit
`https://lcm.example.com:9320`. Den Namen trägst du unter
*Einstellungen → Allgemein* im Adressmodus B ein. LCM rechnet die drei
Adressen daraus aus.

### Alles auf 443: Modus C

Weboberfläche, Agent und MCP teilen sich **einen** Port, und der angefragte
Name entscheidet, welcher Dienst die Verbindung bekommt. Du wählst den Modus in
der Oberfläche und trägst dort den gemeinsamen Port ein (Vorgabe 443). Alternativ
legst du ihn in der `config.json` fest: LCM übernimmt `shared_tls_listen` beim
Start als Modus C mit diesem Port, und die Oberfläche zeigt ihn danach wie jede
andere Einstellung ([Die `config.json` im Gleichklang](/guides/adressen/#die-configjson-im-gleichklang)):

```json
{
  "host": "127.0.0.1",
  "port": 9310,
  "shared_tls_listen": ":443",
  "tls_certificates": [
    { "cert": "/etc/lcm/tls/lcm.pem", "key": "/etc/lcm/tls/lcm.key" }
  ]
}
```

Das Zertifikat nennt `lcm.example.com`, `agent.example.com` und
`mcp.example.com`. Drei einzelne Zertifikate funktionieren genauso. Die Namen
trägst du als Adressen ein:

| Dienst | Adresse |
|---|---|
| Weboberfläche | `https://lcm.example.com` |
| Agent | `https://agent.example.com` |
| MCP | `https://mcp.example.com` |

Alle drei Namen zeigen im DNS auf den LCM-Host. Danach gilt:

- **Die Oberfläche braucht einen Namen, Agent und MCP nur, wenn sie laufen
  sollen.** Gesetzte Namen müssen verschieden sein, sonst lehnt LCM das Speichern
  ab. Ein Dienst ohne Namen ist aus.
- **Ohne Namen** (Aufruf per IP) und bei jedem unbekannten Namen landet die
  Verbindung bei der Weboberfläche.
- **Die Einzelports bleiben zusätzlich offen**, soweit du sie nicht abschaltest.
  Im Beispiel bleibt die Weboberfläche auf `127.0.0.1:9310` und ist von außen
  nicht erreichbar. Sperre die übrigen Einzelports in der Firewall, dann steht
  nach außen nur Port 443.
- **Agent und MCP** schaltest du mit ihrem Schalter unter *Einstellungen →
  Allgemein* ein (MCP auch auf der Seite *MCP*). Clients rufen
  `https://mcp.example.com/mcp` auf.
- **Port 443** darf der Dienst öffnen, ohne dass du etwas einstellst, siehe
  [Port 80 und 443 freigeben](#port-80-und-443-freigeben).

`lcm tls status` zeigt, welcher Name zu welchem Dienst führt
([Neu laden ohne Neustart](#neu-laden-ohne-neustart)).

## Das Zertifikat bereitstellen

### Aus Dateien (`config.json`)

Trage Zertifikat und Schlüssel als absolute Pfade in `tls_certificates` ein. Die
Zertifikatsdatei enthält das Zertifikat und dahinter seine Kette, bei Let's
Encrypt ist das `fullchain.pem`. Der Schlüssel liegt unverschlüsselt vor, ohne
Passwort. Der Dienst läuft als Benutzer `lcm` und muss beide Dateien lesen
können.

Ein Zertifikat, das beim Start nicht lädt, hindert LCM nicht am Start. Die
Meldung steht im Journal (`journalctl -u lcm`).

**Mit certbot** kopiert ein Deploy-Hook die erneuerten Dateien nach
`/etc/lcm/tls` und sagt LCM Bescheid. Lege das Skript an,
`/usr/local/sbin/lcm-deploy-cert`:

```sh
#!/bin/sh
# certbot setzt RENEWED_LINEAGE auf das Verzeichnis des Zertifikats.
set -eu
install -d -m 0750 -o root -g lcm /etc/lcm/tls
install -m 0640 -o root -g lcm "$RENEWED_LINEAGE/fullchain.pem" /etc/lcm/tls/lcm.pem
install -m 0640 -o root -g lcm "$RENEWED_LINEAGE/privkey.pem" /etc/lcm/tls/lcm.key
lcm tls reload
```

Mache es ausführbar und gib es certbot beim ersten Ausstellen mit. Certbot
merkt sich den Hook und ruft ihn bei jeder Erneuerung auf:

```sh
sudo chmod +x /usr/local/sbin/lcm-deploy-cert
sudo certbot certonly --standalone -d lcm.example.com \
  --deploy-hook /usr/local/sbin/lcm-deploy-cert
```

Der Hook läuft nach dem ersten Ausstellen und nach jeder Erneuerung. Die Rechte
(Gruppe `lcm`, `0640`) sorgen dafür, dass der Dienst den Schlüssel lesen kann,
sonst niemand außer root. Wer LCM selbst bei Let's Encrypt holen lässt, braucht
certbot nicht, siehe [Let's Encrypt](#lets-encrypt-acme).

### Hochladen in der Oberfläche

*Einstellungen → Allgemein*, Block **Öffentliche Adressen**: Hinter der Adresse
von Weboberfläche, Agent und MCP steht ein Zahnrad (*Zertifikat & Sicherheit*).
Der Dialog zeigt, welches
Zertifikat LCM für den Namen dieser Adresse ausliefert (Quelle, Namen,
Aussteller, gültig bis, Fingerabdruck). Dort lädst du eine PEM-Datei mit dem
Zertifikat samt Kette und die Schlüsseldatei hoch, jeweils bis 64 KiB. Unter dem
Zertifikat stehen im selben Dialog die
[erlaubten Netzbereiche](/guides/netzbereiche/) des Dienstes.

- LCM prüft beides, bevor es etwas speichert, und lädt das Zertifikat sofort,
  ohne Neustart. Der Dialog sagt danach, ob es für diese Adresse gilt, oder
  warum nicht (Name fehlt, ein Zertifikat mit höherem Rang geht vor).
- Hochgeladene Zertifikate lassen sich im Dialog wieder entfernen. Danach
  liefert LCM für den Namen ein anderes passendes Zertifikat oder das
  LCM-Zertifikat aus. Zertifikate aus der `config.json` und von Let's Encrypt
  entfernst du dort, wo sie herkommen.
- Hochgeladene Zertifikate liegen unter `/var/lib/lcm/tls/uploads/` (Rechte
  `0600`) und gehören zum System-Backup, genau wie der Schlüssel der
  LCM-Zertifikatsstelle.

Zertifikate ändern darf nur ein **Admin**. Das Recht `system:endpoints` hat
sonst niemand, auch kein Manager mit `settings:manage`.

### Let's Encrypt (ACME)

LCM kann Zertifikate selbst bei einer ACME-Zertifizierungsstelle holen und
rechtzeitig erneuern. Du stellst es unter *Einstellungen → Allgemein* im Block
**Öffentliche Adressen** ein, im eingeklappten Abschnitt **Let's Encrypt** (nur
für Admins sichtbar). Es ist in allen Adressmodi wählbar. In Modus A hinter einem
Reverse-Proxy ist meist nur DNS-01 sinnvoll, denn HTTP-01 bräuchte Port 80 auf
LCM.

| Einstellung | Bedeutung |
|---|---|
| Zertifikate automatisch beziehen | Schalter. Ausschalten entfernt die bezogenen Zertifikate, LCM liefert danach wieder die anderen oder das LCM-Zertifikat aus. |
| Namen | Nur zum Lesen. Sie kommen aus den Adressen, siehe unten. |
| Kontakt-E-Mail | Optional. Die Zertifizierungsstelle schreibt dorthin, bevor ein Zertifikat abläuft. |
| Zertifizierungsstelle | Let's Encrypt, Let's Encrypt Staging (Testzertifikate, denen Browser nicht vertrauen) oder die Adresse einer eigenen ACME-Zertifizierungsstelle. |
| Prüfung | HTTP-01 oder DNS-01 |
| Nutzungsbedingungen | Du nimmst sie einmal je Zertifizierungsstelle an. Ohne Zustimmung legt LCM kein Konto an. |

Optionale Felder des DNS-Anbieters (etwa TSIG bei RFC2136, Wartezeiten und
Nameserver) stehen unter **Weitere Einstellungen**, eingeklappt.

**Die Namen kommen aus den Adressen.** Es gibt kein eigenes Namensfeld. LCM
nimmt die verschiedenen DNS-Namen der Adressen von Weboberfläche, Agent und
MCP, in Modus B den einen Namen. Agent und MCP zählen nur, solange sie
eingeschaltet sind. IP-Adressen und Namen ohne Punkt (`localhost`) überspringt
LCM, denn dafür stellt keine öffentliche Zertifizierungsstelle ein Zertifikat
aus. Hat keine Adresse einen passenden Namen, lässt sich Let's Encrypt nicht
einschalten. Platzhalter-Namen wie `*.example.com` gibt es nicht, denn sie
stehen in keiner Adresse. Ändern sich die Adressen, gilt der neue Stand ab dem
Speichern der Seite.

**Immer ein Zertifikat für alle Namen.** In jedem Modus stellt Let's Encrypt ein
Zertifikat aus, das alle Namen trägt. Das braucht einen Lauf und eine
Erneuerung. Von Hand hochgeladene Zertifikate bleiben je Adresse möglich.

**Ausstellen ohne Extra-Klick.** Trägt kein gültiges Zertifikat von Let's Encrypt
alle Namen, stellt LCM von selbst eines aus:

- nach dem Speichern von Let's Encrypt (erstes Einrichten),
- nach dem Speichern der Adressen, wenn ein Name dazukommt,
- nach dem Einschalten von Agent oder MCP, wenn damit ein Name dazukommt,
- beim Start.

Ein Zertifikat, das mehr Namen trägt als nötig, deckt ab. Das Umschalten von Agent
oder MCP löst dann keine neue Ausstellung aus. Scheitert ein solcher Lauf, startet
LCM für dieselben Namen erst nach einer Stunde von selbst neu, es gibt keine
Schleife.

Mit **Jetzt ausstellen** startet ein Lauf sofort, auch nach einem Fehler. Sonst
läuft LCM kurz nach dem Start und danach täglich, nach einem Fehler schon nach
einer Stunde wieder. Die
Karte zeigt den Zustand, den letzten Lauf, einen Fehler und den Zeitpunkt, ab
dem das Zertifikat erneuert wird. Ändern sich Namen oder Zertifizierungsstelle,
stellt LCM ein neues Zertifikat aus und entfernt das alte, sobald alle neuen da
sind. Das Konto und die ausgestellten Zertifikate liegen unter
`/var/lib/lcm/tls/acme/` und gehören zum System-Backup.

Ein bezogenes Zertifikat hat in der Rangfolge den letzten Platz. Nennt ein
Zertifikat aus der `config.json` oder ein hochgeladenes denselben Namen, geht
es vor, auch wenn das Zertifikat von Let's Encrypt den Namen mit trägt. Eine
Erneuerung ändert daran nichts.

**Zertifikat herunterladen.** Zu jedem ausgestellten Zertifikat gehört die
Schaltfläche *Zertifikatskette herunterladen (PEM)*. Sie lädt Zertifikat und
Kette, **nie** den privaten Schlüssel. Das Recht dazu hat nur ein Admin
(`system:endpoints`), und der Abruf steht im Audit-Log
(`settings.tls.download`). Dasselbe liefert
`GET /api/v1/system/tls/certificates/<id>/download`, die ID steht in
`GET /api/v1/system/tls` (etwa `acme-<id>`).

**Umstieg von älteren Versionen.** Frühere Versionen hatten ein eigenes Feld
*Domain-Namen* und die Wahl *ein Zertifikat für alle* oder *je Name eines*.
Beim Update nimmt LCM stattdessen die Namen aus den Adressen und stellt immer
ein Zertifikat für alle aus. Decken die alten Namen die Adressen nicht ab, zeigt
die Karte einmal einen Hinweis mit den alten und den neuen Namen. Das alte
Zertifikat bleibt im Einsatz, bis der nächste Lauf ein neues geholt hat. Der
Hinweis lässt sich schließen. Die API nimmt die Felder `names` und `mode` älterer
Aufrufe noch an und liest sie über.

#### HTTP-01

Die Zertifizierungsstelle ruft eine Datei unter
`http://<Name>/.well-known/acme-challenge/` ab. Dafür muss **Port 80 aus dem
Internet auf LCM zeigen**.

- **Port 80 ist nur während der Prüfung offen.** LCM öffnet einen eigenen
  kleinen Listener, wenn ein Lauf eine Prüfung braucht, und schließt ihn danach.
  Davor und danach lehnt der Port Verbindungen ab. Seine Adresse steht in
  `acme_http_listen` (Standard `:80`).
- **Er beantwortet nur offene Prüfungen.** Auf
  `GET /.well-known/acme-challenge/<Token>` für ein gerade offenes Token liefert
  er die Antwort. Alles andere (andere Pfade, Methoden, Tokens) beendet er ohne
  Antwort, als wäre er nicht da. Eine Weiterleitung auf die Weboberfläche gibt es
  nicht mehr. Die Netzbereiche gelten dafür nicht: Die Prüfung beantwortet er
  aus jedem Netz.
- Port 80 braucht die Berechtigung für Ports unter 1024. Die Dienstdatei
  `lcm.service` bringt sie mit ([Port 80 und 443 freigeben](#port-80-und-443-freigeben)).
  Alternativ setzt du `acme_http_listen` auf einen hohen Port, etwa `":8080"`,
  und leitest Port 80 per Firewall oder Router dorthin weiter.
- **Hinter einem Reverse-Proxy** nimm lieber DNS-01. Bei HTTP-01 müsste der
  Proxy Port 80 an LCM weiterreichen, und das geht meist nicht. Hat der Proxy ein
  eigenes Zertifikat, brauchst du Let's Encrypt in LCM nicht.

Lässt sich Port 80 während einer Prüfung nicht öffnen, scheitert der Lauf. Die
Karte nennt den Grund und, wenn die Berechtigung fehlt, den Hinweis darauf.

#### DNS-01

Bei DNS-01 weist LCM den Besitz des Namens über einen TXT-Eintrag
`_acme-challenge.<Name>` nach. LCM muss dafür **nicht aus dem Internet
erreichbar sein** und braucht keinen Port 80, es braucht nur Zugriff auf die
DNS-Zone.

In der Karte **Let's Encrypt** wählst du bei *Prüfung* **DNS-01**. Darunter
erscheinen der **DNS-Anbieter** mit seinen Feldern, die **Prüf-Nameserver** und
die Schaltfläche **DNS testen**. Geheimnisse wie Tokens und Passwörter speichert
LCM verschlüsselt und zeigt sie nie wieder an. Lässt du ein Geheimfeld bei einer
späteren Änderung leer, bleibt es erhalten, mit *Löschen* entfernst du es.
Optionale Felder wie TSIG bei RFC2136, Wartezeiten und Nameserver stehen unter
*Weitere Einstellungen*.

Dasselbe geht über die API: `PUT /api/v1/system/acme` mit
`"challenge": "dns-01"`, `dns_provider` und den Werten des Anbieters in
`dns_values`. Die Namen kommen aus den Adressen, die Anfrage nennt sie nicht.
Fehlt ein Geheimfeld, bleibt es erhalten, ein leerer Wert löscht es.

```sh
curl -s -X PUT https://lcm.example.com/api/v1/system/acme \
  -H "X-API-Key: $LCM_KEY" -H "Content-Type: application/json" \
  -d '{
    "enabled": true,
    "accept_terms": true,
    "challenge": "dns-01",
    "dns_provider": "cloudflare",
    "dns_values": { "api_token": "…" }
  }'
```

Die Anbieter und ihre Felder (`*` kennzeichnet Pflichtfelder, die Namen sind die
der API, die Oberfläche zeigt sie als beschriftete Felder):

| Anbieter (`dns_provider`) | Felder |
|---|---|
| `rfc2136` | `nameserver`\* (`host` oder `host:port`), `tsig_key`, `tsig_secret` (base64), `tsig_algorithm` (`hmac-sha256` ist Standard, außerdem `hmac-sha384`, `hmac-sha512`, `hmac-sha224`, `hmac-sha1`) |
| `acmedns` | `api_base`\* (Adresse des acme-dns-Servers) |
| `httpreq` | `endpoint`\*, `mode` (`standard` oder `raw`), `username`, `password` |
| `exec` | `mode` (`standard` oder `raw`), das Programm steht in der `config.json` |
| `cloudflare` | `api_token`\* |
| `hetzner` | `api_token`\* (Token der Cloud-API) |
| `pdns` | `api_url`\*, `api_key`\*, `server_name` (Standard `localhost`) |
| `inwx` | `username`\*, `password`\*, `totp_secret`, `sandbox` (`true` oder `false`) |
| `ionos` | `api_key`\* (in der Form `<Präfix>.<Geheimnis>`) |

Bei allen Anbietern außer `acmedns` lassen sich `propagation_timeout` (so
lange wartet LCM, bis der Eintrag sichtbar ist) und `polling_interval` in
Sekunden anpassen, jeweils 1 bis 3600. `tsig_key` und `tsig_secret` gehören
zusammen.

**RFC2136 (dynamisches DNS-Update).** Für PowerDNS, BIND, Knot, Technitium und
andere Server, die TSIG-gesicherte Updates annehmen. Du legst auf dem
Nameserver einen TSIG-Schlüssel an, der TXT-Einträge unter `_acme-challenge`
ändern darf, und trägst Server, Schlüsselname, Algorithmus und das Geheimnis
ein.

**acme-dns.** Das Konto beim acme-dns-Server legt LCM beim ersten Lauf selbst
an. Der Ablauf:

1. Du stellst `acmedns` mit der Adresse deines acme-dns-Servers ein und startest
   einen Lauf.
2. Der erste Lauf scheitert mit einer Meldung, die den **CNAME** nennt, den du
   einmal setzen musst: `_acme-challenge.<Name>` zeigt auf den Namen, den der
   acme-dns-Server vergeben hat.
3. Du setzt den CNAME bei deinem DNS-Anbieter und startest den Lauf erneut. LCM
   merkt sich das Konto, ab jetzt klappt es ohne weiteres Zutun.

**httpreq.** LCM schickt `POST <endpoint>/present` und `POST <endpoint>/cleanup`
an deinen eigenen Endpunkt. Im Modus `standard` trägt der Rumpf `fqdn` und
`value` (den vollständigen Namen des TXT-Eintrags und seinen Wert), im Modus
`raw` `domain`, `token` und `keyAuth`. `username` und `password` gehen als
Basic-Anmeldung mit.

**exec.** LCM ruft ein Programm auf deinem Host auf. Das Programm legt nur die
`config.json` fest, **nicht** die Oberfläche oder die API: Ein Konto in LCM soll keinen Befehl auf dem LCM-Host festlegen
können. Trage in der `config.json` den absoluten Pfad ein:

```json
{
  "acme_dns_exec": "/usr/local/sbin/lcm-dns-challenge"
}
```

Erst wenn der Pfad dort steht, bietet LCM `exec` an. Das Programm bekommt zwei
Argumente nach dem Befehl: `present` oder `cleanup`, den Namen des TXT-Eintrags
und seinen Wert (`<Programm> present <Name> <Wert>`). Im Modus `raw` lauten sie
`<Programm> present -- <Domain> <Token> <Schlüsselautorisierung>`. Das Programm
läuft als Benutzer `lcm` und muss ausführbar sein.

**Prüf-Nameserver.** Bevor LCM die Zertifizierungsstelle fragen lässt, prüft es,
ob der TXT-Eintrag sichtbar ist. Dafür fragt es die Resolver des Systems. Mit
den Prüf-Nameservern (in der API `dns_check_nameservers`, höchstens drei, `host`
oder `host:port`) legst du andere fest, etwa die autoritativen Server deiner Zone, wenn ein Resolver im
Weg steht oder eine Split-DNS-Zone den Eintrag nicht zeigt.

**DNS testen.** Die Schaltfläche (in der API `POST /api/v1/system/acme/dns-test`)
setzt einen TXT-Eintrag `_acme-challenge.<erster Name>` (den ersten Namen aus
den Adressen) mit einem Zufallswert,
wartet bis zu 60 Sekunden darauf, dass die Prüf-Nameserver ihn zeigen, und
räumt ihn wieder auf.
So prüfst du Zugangsdaten und Zone, ohne die Zertifizierungsstelle zu belasten.
Der API-Aufruf nimmt dieselben Felder wie `PUT /system/acme`. Was fehlt, ergänzt
LCM aus den gespeicherten Einstellungen. Ein Anbieter, der länger braucht,
kann im echten Lauf trotzdem gelingen, denn dort gilt sein eigenes Zeitlimit.

**Mehrere Namen brauchen Zeit.** Bei `rfc2136` und `exec` arbeitet LCM die
Namen nacheinander ab und wartet zwischen zwei Namen 60 Sekunden. Ein
Zertifikat mit drei Namen dauert deshalb entsprechend länger. Je Anbieter gelten andere Standard-Wartezeiten bis
zur Sichtbarkeit des Eintrags: bei Cloudflare, Hetzner und PowerDNS 2 Minuten,
bei INWX 6, bei IONOS 15 Minuten.

#### Eigene ACME-Zertifizierungsstelle

Statt Let's Encrypt trägst du unter *Zertifizierungsstelle* die Adresse des
ACME-Verzeichnisses ein (muss mit `https://` beginnen), zum Beispiel die einer
eigenen Zertifizierungsstelle wie step-ca. Der Rest läuft wie oben.

Stammt das Zertifikat des ACME-Servers von einer privaten Zertifizierungsstelle,
der LCM nicht vertraut, kennt die ACME-Bibliothek von LCM die Umgebungsvariable
`LEGO_CA_CERTIFICATES`: Sie nennt eine PEM-Datei mit dem Wurzelzertifikat dieser
Stelle. Setze sie dem Dienst per Drop-in (`sudo systemctl edit lcm`):

```ini
[Service]
Environment=LEGO_CA_CERTIFICATES=/etc/lcm/tls/acme-ca.pem
```

Danach `sudo systemctl restart lcm`. Prüfe den Pfad: Die Datei muss für den
Benutzer `lcm` lesbar sein, sonst kann kein Lauf stattfinden.

#### Erneuerung und Alarm

LCM prüft bei jedem Lauf, ob ein Zertifikat fällig ist. Bietet die
Zertifizierungsstelle **ARI** an (die Empfehlung, wann erneuert werden soll),
erneuert LCM in diesem Fenster. Ohne ARI erneuert es, sobald **weniger als ein
Drittel der Laufzeit** übrig ist. Gelingt ein Lauf nicht, versucht LCM es
stündlich wieder.

Damit du es merkst, gibt es unter *Einstellungen → Alarme* den Alarmtyp
**Zertifikat-Erneuerung fehlgeschlagen**. Er löst aus, solange der letzte Lauf
einen Fehler hat, nennt den Grund und endet, sobald ein Lauf gelingt. Der Alarm
**TLS-Zertifikat läuft bald ab** meldet sich zusätzlich, wenn das
LCM-Zertifikat knapp wird.

## Neu laden ohne Neustart

Ein neues Zertifikat braucht keinen Neustart. LCM lädt Zertifikate aus der
`config.json`, hochgeladene und bezogene Zertifikate neu, wenn du es sagst:

```sh
sudo lcm tls reload       # lädt neu und berichtet, was geladen oder abgelehnt wurde
sudo lcm tls status       # zeigt den Stand
sudo systemctl reload lcm # lädt nur neu, ohne Bericht
```

`systemctl reload lcm` schickt das Signal `SIGHUP` und tut dasselbe wie
`lcm tls reload`, nur ohne Ausgabe. Das ist der richtige Aufruf für Skripte
und Hooks. Neu geladen werden nur die Zertifikate, nicht die übrigen
Einstellungen der `config.json`. Die liest LCM weiter nur beim Start. In der
Oberfläche löst der Dialog *Zertifikat & Sicherheit* das Neuladen beim
Hochladen und Entfernen selbst aus.

- **Offene Verbindungen behalten ihr Zertifikat**, Agenten bleiben verbunden.
  Neue Verbindungen bekommen das neue.
- **Ein fehlerhaftes Zertifikat ersetzt kein gutes.** Besteht eines die
  Prüfung nicht, bleibt der bisherige Stand aktiv. `lcm tls reload` nennt den
  Grund und endet mit Exit-Code 1.
- Die Steuerbuchse für diese Befehle liegt unter `/var/lib/lcm/control.sock` und
  ist nur für den Dienstbenutzer und root zugänglich. Darum `sudo`. Das Neuladen
  steht im Audit-Log (`settings.tls.reload`).

`lcm tls status` sieht ungefähr so aus:

```text
Zertifikate, in der Reihenfolge, in der sie gewählt werden:

config-0 - config.json
  Namen:        lcm.example.com, agent.example.com, mcp.example.com
  Aussteller:   E7
  Gültig bis:   2027-02-14
  Fingerprint:  3f9a1c0b7d2e5a64…

lcm-ca - LCM-Zertifikat (Rückfall)
  Namen:        localhost, 127.0.0.1, ::1
  Aussteller:   LCM Root CA
  Gültig bis:   2027-01-20
  Fingerprint:  b1c47e90a25d3f08…

Öffentliche Adressen:
  Weboberfläche  https://lcm.example.com
                 für lcm.example.com wird config-0 (config.json) ausgeliefert
  Agent          https://agent.example.com
                 für agent.example.com wird config-0 (config.json) ausgeliefert
  MCP            https://mcp.example.com
                 für mcp.example.com wird config-0 (config.json) ausgeliefert

Gemeinsamer Port :443 (aktiv): Weboberfläche, Agent und MCP nach dem angefragten Namen
  Weboberfläche  lcm.example.com
  Agent          agent.example.com
  MCP            mcp.example.com
  Weboberfläche  auch ohne Namen und für jeden anderen Namen
```

Steht bei einer Adresse „ausgeliefert wird das LCM-Zertifikat“, passt für ihren
Namen kein eigenes Zertifikat.

## Port 80 und 443 freigeben

Ports unter 1024 darf ein Dienst ohne Root-Rechte nur mit einer zusätzlichen
Berechtigung öffnen. Die mitgelieferte Dienstdatei `lcm.service` gibt LCM genau
diese eine Berechtigung (`CAP_NET_BIND_SERVICE`). Du musst nichts einstellen, und
ein Drop-in (`systemctl edit lcm`) ist nicht mehr nötig. Alle anderen Härtungen
der Dienstdatei bleiben. LCM braucht die Berechtigung für zwei Dinge:

- **Port 443**, wenn du Modus C wählst oder die Weboberfläche selbst auf 443
  legst (`LCM_PORT=443`).
- **Port 80**, nur solange eine HTTP-01-Prüfung von Let's Encrypt läuft.

Die Vorgaben 9310, 9320 und 9330 liegen über 1023 und brauchen nichts.

In der **Firewall** musst du die Ports selbst freigeben. In Modus C ist das der
gemeinsame Port, bei HTTP-01 kommt Port 80 dazu
([Öffentliche Adressen & Adressmodus](/guides/adressen/#welche-ports-du-freigibst)).

Hast du die Dienstdatei selbst gebaut oder aus einer älteren Version behalten,
fehlt die Berechtigung. Dann öffnet LCM Port 80 und den gemeinsamen Port nicht,
läuft auf den übrigen Ports weiter und nennt im Journal den Grund. Nimm in dem
Fall die Zeilen `CapabilityBoundingSet=CAP_NET_BIND_SERVICE` und
`AmbientCapabilities=CAP_NET_BIND_SERVICE` aus der aktuellen
`packaging/lcm.service` in deine Datei auf. Oder wähle hohe Ports (zum Beispiel
`":8443"` und `":8080"`) und leite 443 und 80 per Firewall dorthin weiter.

Im **Docker-Container** bleibt LCM auf hohen Ports und Docker veröffentlicht sie
auf 80 und 443: `-p 443:8443 -p 80:8080` mit `shared_tls_listen: ":8443"` und
`acme_http_listen: ":8080"`. Siehe das Image-Readme
(`techeve/lcm` auf Docker Hub).

## Agenten und Zertifikate

Agenten prüfen das Zertifikat von LCM bei jedem Verbindungsaufbau. Was für
sie gilt, hängt davon ab, woher das Zertifikat kommt.

- **Öffentliche Zertifizierungsstelle (Let's Encrypt und andere).** Der Agent
  vertraut dem Zertifikat über die Zertifikate seines Systems und prüft den
  Namen seiner Adresse. Das funktioniert mit jedem Agent. Wichtig ist, dass er
  mit dem Namen eingerichtet ist, den das Zertifikat nennt:

  ```sh
  sudo lcm-agent enroll https://agent.example.com <token>
  ```

- **Eigene (private) Zertifizierungsstelle.** Das Token des Servers trägt jetzt
  neben dem Fingerprint des Zertifikats auch die Zertifizierungsstelle, die es
  ausgestellt hat, und die LCM-eigene Stelle. Ein **neuer Agent**, den du mit
  einem solchen Token einrichtest, pinnt die private Stelle. Danach gilt jedes
  Zertifikat von ihr, **das den Namen seiner Adresse trägt**, auch nach einer
  Erneuerung mit neuem Schlüssel. Dafür muss die Zertifikatsdatei die Kette bis
  zum Aussteller enthalten, sonst kann LCM die Stelle nicht ins Token
  schreiben. Richte die Agenten also mit einem Token ein, das LCM **nach** dem
  Einsetzen des Zertifikats erzeugt hat (*Agent-Token erneuern*, dann
  `lcm-agent enroll` erneut).
- **Ältere und bereits eingerichtete Agenten** kennen die private Stelle
  nicht. Sie folgen dem Fingerprint des Zertifikats, dem Schlüssel, den sie sich
  gemerkt haben, oder dem LCM-Zertifikat. Ein Zertifikat einer privaten Stelle
  mit neuem Schlüssel nehmen sie nicht an. Ein Agent mit der neuen Version, der
  über eines seiner bisherigen Merkmale verbunden bleibt, lernt die Stelle aus
  der Kette selbst. Plane den Wechsel zu einer privaten Stelle deshalb vor dem
  Einsetzen: Agenten aktualisieren, oder das Zertifikat mit demselben Schlüssel
  ausstellen.
- **Rückfall.** Ruft ein Agent LCM per IP-Adresse oder unter einem Namen auf,
  den kein Zertifikat nennt, bekommt er das LCM-Zertifikat. Dem vertrauen alle
  Agenten ab 2.1, denn sie merken sich die LCM-eigene Zertifizierungsstelle. Auch
  nach dem Entfernen eines eigenen Zertifikats bleiben sie verbunden.
- **Der Assistent *Server hinzufügen* und der Download-Befehl** nehmen Fingerprint und
  Schlüssel aus dem Zertifikat, das LCM für den Namen der Agent-Adresse
  tatsächlich ausliefert. Trage dafür unter *Einstellungen → Allgemein* die
  Adresse für Agenten ein, wenn sie vom Namen der Weboberfläche abweicht.

Hinter einem Reverse-Proxy gelten weiter die Hinweise unter
[LCM Remote](/guides/remote/#hinter-einem-reverse-proxy).

## MCP mit TLS

Die MCP-Schnittstelle spricht **immer HTTPS**, in jedem Modus und auch auf
bestehenden Installationen. LCM wählt das Zertifikat aus dem Pool nach dem Namen,
unter dem der Client MCP aufruft.

- Clients rufen `https://<Name>:9330/mcp` auf, über den gemeinsamen Port (Modus C)
  `https://<Name>/mcp`. Ein Client oder Reverse-Proxy mit `http://` muss auf
  `https://` umstellen.
- MCP bindet an dieselbe Adresse wie die Weboberfläche. Ein eigenes Feld für die
  Bind-Adresse gibt es nicht.
- Das Zertifikat für den Namen stellst du wie bei den anderen Diensten ein, am
  Zahnrad der MCP-Adresse. Mit Let's Encrypt trägt das Zertifikat den MCP-Namen,
  sobald MCP an ist.

Alles Weitere zu Schlüsseln und Tools steht unter
[MCP-Schnittstelle](/guides/mcp/).

## Wenn etwas nicht klappt

| Beobachtung | Ursache und Abhilfe |
|---|---|
| Browser warnt weiter | `lcm tls status` zeigt, welches Zertifikat für den Namen der Adresse gilt. „LCM-Zertifikat“ heißt: Kein eigenes nennt den Namen. |
| „Kette unvollständig“ | Die Datei enthält nur das Zertifikat. Nimm die Datei mit Kette (`fullchain.pem`). |
| „Zertifikat und Schlüssel sind vertauscht“ oder „gehört nicht zu diesem Zertifikat“ | Die Dateien passen nicht zusammen. |
| „der Schlüssel lässt sich nicht lesen“ | Der Schlüssel ist mit einem Passwort geschützt. LCM braucht ihn ohne. |
| LCM öffnet Port 80 oder 443 nicht | Die Berechtigung fehlt, siehe [Port 80 und 443 freigeben](#port-80-und-443-freigeben). Das betrifft nur selbst gebaute oder alte Dienstdateien. |
| Speichern der öffentlichen Adressen scheitert | Im Modus C braucht die Oberfläche einen Namen, und gesetzte Namen müssen verschieden sein. In allen Modi müssen die Ports verschieden sein. |
| Agent oder MCP am eigenen Namen bekommt keine Antwort | Der Dienst ist aus, oder in Modus C fehlt sein Name. Schalte ihn unter *Einstellungen → Allgemein* ein. |
| Ein MCP-Client mit `http://` kommt nicht mehr durch | MCP spricht immer HTTPS. Stelle den Client auf `https://` um. |
| Let's Encrypt lässt sich nicht einschalten | Keine Adresse trägt einen DNS-Namen. IP-Adressen und `localhost` überspringt LCM. Trage Adressen mit Namen ein und speichere die Seite. |
| Let's Encrypt scheitert bei HTTP-01 | Erreicht Port 80 LCM aus dem Internet, und zeigt der Name im DNS auf diesen Host? Port 80 ist nur während der Prüfung offen. Die Karte nennt den Fehler des letzten Laufs. Hinter einem Proxy hilft DNS-01. |
| Let's Encrypt scheitert bei DNS-01 | `DNS testen` zeigt, ob Anbieter und Zone zusammenpassen. Bei langsamen Anbietern hilft eine längere Wartezeit (`propagation_timeout`). |
