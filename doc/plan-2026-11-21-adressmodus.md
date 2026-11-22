# Plan: Adressmodus, Ports, Zugriff und Let's Encrypt in „Öffentliche Adressen“

Stand 22.11.2026 · Status: **P1–P9 zu prüfen, P10–P11 in Arbeit nach Tonys Handtest**

## Ziel

In Einstellungen → Allgemein soll ein einziger Block „Öffentliche Adressen“ zeigen, wie LCM von außen erreichbar ist. Ganz oben steht die Wahl des Adressmodus. Der Rest des Blocks folgt dieser Wahl: Adressfelder, Ports, Zugriff und Let's Encrypt. Dieselbe Wahl fragt der Einrichtungsassistent ab.

Heute stehen diese Teile getrennt nebeneinander:

- die Adressen,
- der gemeinsame Port, nur in der `config.json`,
- die Netzbereiche im Zahnrad-Dialog,
- Let's Encrypt als eigene Karte mit eigenem Namensfeld.

Daraus entstand der Fehler „ein Name, keine Ports“: Der Agent-Enroll ging dabei auf 443, und dort lauschte nichts.

## 1. Die drei Adressmodi

| Modus | Ports | Namen | Felder in der Oberfläche | Typischer Einsatz |
|---|---|---|---|---|
| **A: Eigene Ports, eigene Namen** (Standard) | je Dienst einer, einstellbar (Vorgabe 9310 / 9320 / 9330) | je Dienst ein Name | drei Adressfelder (Oberfläche, Agent, MCP), frei mit oder ohne Port, dazu drei Portfelder | Reverse-Proxy davor, heutiger Stand |
| **B: Eigene Ports, ein Name** | je Dienst einer, einstellbar | ein Name für alle | ein Namensfeld, drei Portfelder. Die abgeleiteten Adressen (`https://name:9310` usw.) stehen schreibgeschützt daneben | Betrieb ohne Proxy, ein Zertifikat |
| **C: Ein Port, eigene Namen** | ein gemeinsamer Port, Vorgabe **443** | je Dienst ein eigener Name; Pflicht nur für die Oberfläche | drei Namensfelder und das Feld „Gemeinsamer Port“ | keine Ports mehr im Browser, Trennung nach Namen (SNI) |

- **Bestehende Installationen:** Sie laufen in Modus A weiter. Hat die `config.json` einen `shared_tls_listen`, startet LCM in Modus C.
- **Prüfungen beim Speichern:**
  - C: Der Name der Oberfläche ist Pflicht. Agent und MCP dürfen ohne Namen bleiben, dann sind sie aus (Abschnitt 8). Gesetzte Namen müssen verschieden sein (`checkSharedPortHosts` greift dann immer).
  - B: Es gibt genau einen Namen, ohne Port und ohne Pfad.
  - A und B: Die Ports müssen verschieden sein und dürfen keinen belegten Port treffen.
- **Abgeleitete Adressen:** Für B rechnet das Backend die drei Adressen selbst aus. So nutzen Enroll-Befehl, MCP-Adresse und Join-Assistent dieselben Werte, und keine Oberfläche baut sie nach.

## 2. Ports in der Oberfläche

- **Einstellbar:** In A und B lassen sich die Ports von Oberfläche, Agent und MCP in der Oberfläche einstellen, nicht mehr nur der MCP-Port. In C gibt es nur den gemeinsamen Port.
- **Oberfläche und `config.json` sind synchron** (entschieden von Tony, 22.11.):
  - In der `config.json` stehen Adressmodus, Namen (`public_host` bzw. die drei Adressen), alle Ports samt gemeinsamem Port, die Schalter von Agent und MCP sowie `trust_proxy_header` und `trusted_proxies`.
  - Speichern in der Oberfläche schreibt die Werte auch in die `config.json` (atomar, Rechte wie heute).
  - Beim Start liest LCM die `config.json`. Weicht ein Wert von der Datenbank ab, wurde die Datei von Hand geändert: Ihr Wert gilt und kommt in die Datenbank.
  - Kein Feld ist mehr gesperrt. Nur eine Umgebungsvariable (`LCM_PORT`, `LCM_AGENT_PORT`) sperrt ihr Feld, weil LCM sie nicht ändern kann.
  - Let's Encrypt steht nicht in der `config.json`, es wird nur in der Oberfläche eingerichtet.
- **Wechsel des Oberflächen-Ports:** Er setzt einen Neustart des Listeners voraus. Die Oberfläche
  - nennt vor dem Speichern die neue Adresse,
  - leitet danach dorthin weiter,
  - und geht bei einem Fehlstart auf den alten Port zurück (Wirkungsprobe wie beim eingeschränkten Modus).
- **Agent- und MCP-Port** schalten ohne Neustart um.
- **Firewall-Hinweis:** Die Hilfe-Spalte nennt die Ports, die freizugeben sind.

## 3. Ports 443 und 80

- **Rechte des Dienstes:** `lcm.service` bekommt `CAP_NET_BIND_SERVICE` (Bounding und Ambient), damit 443 ohne Handarbeit geht. Das hat Tony freigegeben. Der Dienst darf damit Ports unter 1024 öffnen, alle übrigen Härtungen bleiben. Kommentar und Doku sagen, warum.
- **Port 80 nur für die HTTP-01-Challenge:**
  - LCM öffnet ihn **nur, solange eine Challenge läuft**. Davor und danach ist er geschlossen, eine Verbindung wird also abgewiesen.
  - Während der Challenge beantwortet er ausschließlich `GET /.well-known/acme-challenge/<token>` für die gerade offenen Tokens.
  - Alles andere (andere Pfade, Methoden, Tokens) beendet er ohne Antwort, als wäre er nicht da. Die heutige Weiterleitung auf die Oberfläche entfällt.
- **DNS-01** braucht keinen Port 80.

## 4. Aufbau des Blocks

Reihenfolge von oben nach unten:

1. **Adressmodus:** drei anklickbare Karten mit je einem Satz Erklärung, Vorgabe A.
2. **Adressen und Ports:** je nach Modus (siehe Tabelle).
   - Unter der Adresse von Agent und MCP steht ihr Schalter an/aus (Abschnitt 8).
   - In B steht unter den Ports die Live-Vorschau der drei Adressen, je Adresse mit Kopierknopf. Hinter Agent und MCP steht „(nicht aktiv)“, solange ihr Schalter aus ist: Ein Port allein schaltet keinen Dienst ein.
3. **Zahnrad je Adresse:** „Zertifikat & Sicherheit“ mit
   - ausgeliefertem Zertifikat, Hochladen von Hand, Entfernen,
   - **Zugriff (Netzbereiche)** dieses Dienstes. Der Abschnitt kommt aus dem Block zurück ins Zahnrad. Inhalt und Schutz wie heute: Die eigene Adresse lässt sich nicht aussperren, und es gibt eine Warnung, wenn verbundene Agenten herausfielen.
4. **Let's Encrypt:** eingeklappt (siehe Abschnitt 5).
5. **Hilfe-Spalte** rechts daneben, auf schmalen Bildschirmen darunter. Ihr Inhalt folgt dem gewählten Modus. Sie erklärt
   - welcher Modus wann passt,
   - was ein Reverse-Proxy weiterreichen muss,
   - welche Ports freizugeben sind,
   - wann Let's Encrypt in LCM überflüssig ist (Proxy mit eigenem Zertifikat in A).

## 5. Let's Encrypt im Block

- **In allen Modi wählbar**, auch in A. In A hinter einem Proxy ist meist nur DNS-01 sinnvoll, weil HTTP-01 den Proxy bräuchte. Das sagt die Hilfe.
- **Was entfällt:**
  - Das Feld „Domain-Namen“: Die Namen kommen aus den Adressen, in B ein Name, in A und C bis zu drei verschiedene.
  - Die Auswahl „ein Zertifikat für alle / je Name“: Let's Encrypt stellt immer ein Zertifikat für alle Namen aus (siehe Abschnitt 7).
- **Was bleibt:**
  - Schalter an/aus
  - Kontakt-E-Mail
  - Zertifizierungsstelle (Let's Encrypt, Staging, eigene)
  - Bestätigung der Nutzungsbedingungen
  - Verfahren HTTP-01 oder DNS-01
  - Anbieter und seine Pflichtfelder
- **Optionale Anbieterfelder** (z. B. TSIG bei rfc2136, Wartezeiten, Nameserver) stehen unter „Weitere Einstellungen“ und sind eingeklappt.
- Die Schaltfläche **„Testen“** bleibt.
- **Übersicht** wie heute (Stand, letzter Lauf, Zertifikate), neu mit **„Zertifikat herunterladen“**: Zertifikatskette als PEM, **nie** der private Schlüssel.
- **Umstieg:** Aufträge mit bisherigem eigenem Namensfeld nehmen beim Update die Namen aus den Adressen. Deckten die alten Namen Adressen nicht ab, meldet die Karte das einmalig.
- **Ausstellen ohne Extra-Klick:** Deckt das aktuelle Zertifikat nicht alle Namen ab, stellt LCM von selbst aus. Das gilt
  - nach dem Speichern von Let's Encrypt (erstes Einrichten),
  - nach dem Speichern der Adressen, wenn ein Name dazukommt,
  - nach dem Einschalten von Agent oder MCP, wenn damit ein Name dazukommt.

  Ein gescheiterter Lauf wird nicht in einer Schleife wiederholt: Für dieselben Namen startet LCM von selbst erst nach einer Wartezeit neu. „Jetzt ausstellen“ bleibt für den Fall von Hand.
- **Eigenes Zertifikat geht vor:** Ein hochgeladenes Zertifikat für einen Namen wird ausgeliefert, auch wenn das Let's-Encrypt-Zertifikat den Namen mit enthält. Eine Erneuerung ändert daran nichts.

## 6. Einrichtungsassistent

Der erste Administrator wird nach Passwort und 2FA Schritt für Schritt durch die Einrichtung gefragt, in zehn Schritten. Jeder Schritt speichert für sich und nutzt dieselben Komponenten wie die Einstellungen. Der Fortschritt liegt auf dem Server (`setup_step`), damit der Assistent nach einem Wechsel der Adresse und neuer Anmeldung an derselben Stelle weitermacht.

Vor dem Assistenten (wie heute): **Passwort vergeben**, dann **2FA einrichten**. Die 2FA schickt den Code nicht von selbst ab, wenn sechs Ziffern stehen, sondern erst mit dem Knopf oder Enter.

**Für alle Schritte:**
- **Zurück:** Jeder Schritt hat „Zurück“ bis zum ersten Schritt. Gespeichertes bleibt gespeichert. Auch nach der Weiterleitung auf eine neue Adresse geht es zurück.
- **Wortwahl:** Oberfläche, Agent und MCP heißen **Schnittstellen** (Weboberfläche, Agenten-Schnittstelle, MCP-Schnittstelle), nicht Dienste. Das gilt auch in den Einstellungen.
- **Hilfe-Spalte:** Sie steht rechts neben dem Schritt wie auf den Einstellungsseiten und klappt zur Seite ein. Der Zustand gilt für alle Schritte. Auf schmalen Bildschirmen steht sie unter dem Schritt und klappt nach oben ein, bis nur noch ihre Überschrift steht; das Symbol zeigt dann diese Richtung.
- **Vorschau der Adressen** im Assistenten ohne Kopierknöpfe.

**Die Schritte:**

1. **Anmeldung im Browser:** Sitzung als Cookie oder nur für die laufende Sitzung. Das Fenster, das bisher über dem Assistenten aufging, wird dieser Schritt.
2. **Wie ist LCM erreichbar?** Modus, Namen, Ports. In allen Modi wird nur der Name eingetragen (in A optional mit `:port` für einen Proxy auf einem anderen Port), nie `https://`. Kein Überspringen.
3. **Schnittstellen:** „Welche Schnittstellen von LCM willst du verwenden?“ Je ein Schalter für Agenten- und MCP-Schnittstelle mit einem Satz Erklärung. In C nur mit Namen, sonst „keine Domain eingerichtet“.
4. **Zugriff:** aus welchen Netzen die Weboberfläche und die eingeschalteten Schnittstellen erreichbar sind (`lcm-network-group`).
5. **Zertifikat:** Der Schritt sagt, dass LCM für eigene Namen ohne Hilfe kein gültiges Zertifikat hat, und lässt wählen:
   - **Let's Encrypt:** Der Let's-Encrypt-Block erscheint darunter, ohne den Hinweis „Let's Encrypt ist aus“. Sein Knopf heißt hier **„Zertifikat anfordern“**: Er speichert, fordert an und zeigt den Fortschritt. Bis ein Zertifikat da ist, ist der Knopf unten gesperrt. Danach steht unten nur **„Weiter“**.
   - **Reverse-Proxy davor:** Felder für die Adressen oder Netzbereiche des Proxys (`trust_proxy_header`, `trusted_proxies`), dann „Speichern und weiter“.

   - **Ohne Namen** (Schritt 2 ohne Hostname, die Adressen tragen nur `localhost` oder eine IP): Let's Encrypt zeigt statt seiner Felder nur den Hinweis, dass dafür in Schritt 2 ein Name nötig ist. Vorgewählt ist dann der Reverse-Proxy.

   Ohne Let's Encrypt und ohne hochgeladenes Zertifikat liefert LCM sein selbst ausgestelltes Zertifikat aus; eine dritte Wahl gibt es nicht. Die Nutzungsbedingungen der CA sind verlinkt, hier und in den Einstellungen.

   Danach leitet der Assistent auf die neue Adresse der Oberfläche weiter, aber nur, wenn sie einen eingetragenen Namen trägt. Ohne Namen (Adresse wäre `localhost` oder die Rückfalladresse) bleibt der Browser, wo er ist. Ändert sich dabei der Name, ist eine neue Anmeldung nötig; danach geht es bei Schritt 6 weiter.
6. **Onboarding:** Vorgabe-Benutzer für neue Server und der Onboarding-Schlüssel mit Kopierknopf, Hinweise für Hetzner, Proxmox und andere. Kein Überspringen: Ein leeres Feld bleibt leer. Der Einleitungssatz sagt, dass sich das später in den Einstellungen ändern lässt.
7. **E-Mail-Versand:** Server, Port, Verschlüsselung, Zugang, Absender, Empfänger der Admin-Mails, Knopf „Test-Mail senden“. Überspringen möglich; der Einleitungssatz sagt, dass es sich später unter Einstellungen → Allgemein einrichten lässt.
8. **Aufbewahrung:** Erklärt, was Jobs sind: jede Verbindung zu einem Server, mit genauem Protokoll, was LCM getan hat und was zurückkam, verschlüsselt in der Datenbank. Dann wie viele Tage Jobs, Routineprotokolle und der Speicherverlauf aufgehoben werden (Vorgaben vorbelegt).
9. **Automatisches Backup:** an/aus, Intervall, Uhrzeit, Anzahl der Aufbewahrungen. Die Uhrzeit ist vorbelegt: zufällig zwischen 02:00 und 05:30 im 10-Minuten-Raster. Hinweis: Bei jedem Update über LCM legt LCM vorher ein Backup an. Überspringen möglich.
10. **Subscription und Kanal:** wie die Seite Subscription: Key eingeben und in den Enterprise-Kanal wechseln, oder in den Beta-Kanal wechseln, oder überspringen und im Community-Kanal bleiben. Die Kanalwahl wirkt erst mit „Weiter“ (keine Sofort-Umstellung, kein Knopf „Einrichtung erneut anwenden“). Nach erfolgreicher Aktivierung eines Keys empfiehlt der Schritt den Enterprise-Kanal.

**Danach:** die Seite des LCM-Hosts mit einer **Einführung als Fenster an der Seite**. Sie erklärt und navigiert nur, Aktionsknöpfe (Installieren, Nachrüsten) stehen ausschließlich auf den Karten der Seite. Stationen:
1. Das ist der Host, auf dem LCM läuft; er steht selbst als Server in der Liste.
2. Trivy (empfohlen): CVE-Scanner, mit Sandbox; Knopf auf der Karte.
3. apt-cacher-ng: was es zwischenspeichert, wofür es gut ist.
4. SSH härten und die Firewall, beides von dieser Seite aus.
5. Server-Einstellungen: Die Einführung öffnet das Einstellungsfenster des Servers und blättert dessen Seiten durch: Wartungs- bzw. Unbedenklich-Modus für Systeme, die nicht rund um die Uhr erreichbar sein müssen, Benutzer-Synchronisation abschalten, SSH-Zugänge (Root-Login aus, kein Konsolenzugriff über LCM), DNS und Zeit.
6. Die Reiter des Servers: Pakete (veraltete Pakete, alle oder einzeln aktualisieren, Listen aktualisieren, anpinnen), Apps (wofür), Benutzer (kurz), Sicherheit (CVE-Funde, weitere Sicherheitswerkzeuge), Deep Scan (intensiver Scanner, läuft lokal auf dem Client), Speicher-Monitoring (weitere Laufwerke anhaken), Jobs (was LCM getan hat). Repositories wird übersprungen.
7. Zurück auf der Hauptseite des Servers, unten: Gruppen. Jeder Server ist in der Systemgruppe; dort laufen Routine-Operationen und Automationen. Eigene Gruppen erlauben je Server andere Automationen (wann, wie, welche Updates).
8. Fertig: „Einrichtung abgeschlossen“.

Der Zähler zählt alle Stationen und Teile durchgehend („3 von 17“), nicht Stationen mit Teilen.

CrowdSec LAPI kommt in der Einführung nicht vor. Das Fenster lässt sich jederzeit schließen und kommt nicht wieder.

**Trivy wird immer mit Sandbox installiert.** „Sandbox nachrüsten“ bleibt nur für ältere Installationen ohne Sandbox. Damit Bubblewrap startet, leert LCM beim Start seine Ambient-Capabilities: `CAP_NET_BIND_SERVICE` aus `lcm.service` behält LCM selbst, Kindprozesse erben sie nicht (Bubblewrap bricht sonst mit „Unexpected capabilities but not setuid“ ab). Landlock fehlt im Proxmox-Kernel in der LSM-Liste; dort trägt allein Bubblewrap.

Bestehende Installationen sehen den Assistenten nicht (`setup_step` leer = erledigt).

## 7. Ein Zertifikat für alle Namen

Entschieden von Tony: Stellt Let's Encrypt das Zertifikat aus, ist es **immer eines für alle Namen**, in allen Modi. Das braucht nur einen Lauf und eine Erneuerung. Von Hand hochgeladene Zertifikate bleiben je Adresse möglich.

## 8. Agent und MCP an- und ausschalten

- **Neu: Schalter für den Agenten** (`agent_enabled`, Vorgabe an, bestehende Installationen bleiben an). Aus heißt: Der Agent-Port nimmt keine Verbindung an, in C bekommt der Agent-Name keine Antwort. Verbundene Agenten trennen sich. Vor dem Ausschalten nennt die Oberfläche, wie viele Server den Agenten nutzen.
- **MCP** behält seinen Schalter (`mcp_enabled`).
- **Wo die Schalter stehen** (jeweils dieselbe Einstellung):
  - Agent: im Block der Adressen unter seiner Adresse und in der Karte „Onboarding“ (Abschnitt 9).
  - MCP: im Block der Adressen unter seiner Adresse und auf der Seite MCP.
- **Kein Name, kein Dienst (Modus C):** Fehlt der Name von Agent oder MCP, ist der Dienst aus und sein Schalter gesperrt, überall. Darunter steht: „Für diesen Dienst ist keine Domain eingerichtet. Trage sie unter Einstellungen → Allgemein ein.“ Speichert jemand den Namen leer, schaltet LCM den Dienst dabei aus.
- **Name eingetragen:** Trägt jemand in C einen Namen ein, springt der Schalter darunter auf an. Er lässt sich vor dem Speichern wieder ausschalten.
- **Modi A und B:** Agent und MCP tragen ohne eigenen Namen den der Oberfläche, die Schalter sind frei.
- **`config.json` mit `agent_port: 0`** (ältere Installationen) heißt beim Start: Agent aus. Danach gilt der Schalter, synchron mit der Datei.
- **Seite MCP:** nur noch Schalter, Stand, wirksame Adresse mit Kopierknopf, Schlüssel und Beispiel. Es entfallen
  - die Auswahl TLS: MCP spricht immer TLS, auch bestehende Installationen nach dem Update,
  - Bind-Adresse und Port: Der Port steht in den Adressen, die Bind-Adresse ist die der Oberfläche.

  Der Release-Hinweis sagt, dass ein MCP-Client oder Proxy mit `http://` auf `https://` umstellen muss.

## 9. Karte „Onboarding“ in Einstellungen → Allgemein

Die Karten „Vorgabe für neue Server“ und „Onboarding-Schlüssel“ werden eine Karte **„Onboarding“** mit drei eingeklappten Unterkarten:

1. **Vorgabe für neue Server:** Inhalt wie bisher.
2. **Onboarding-Schlüssel:** der Schlüssel mit Kopierknopf, dazu eine Anleitung, wie er auf den Server kommt: Debian, Ubuntu und Linux Mint, dazu RHEL-Familie (Rocky, Alma, Fedora) und allgemein für andere Systeme. Benutzer, Verzeichnis und Rechte (`~/.ssh/authorized_keys`, 700/600).
3. **Mit Agent verwenden:** was der Agent ist und wann er passt, der Schalter (Abschnitt 8), die wirksame Agent-Adresse mit Port (in B mit dem Port des Agenten) und ein Verweis auf den Join-Assistenten.

## 10. Backend

- **Einstellungen:**
  - `address_mode` (`separate` | `one_name` | `shared_port`)
  - die Ports von Oberfläche, Agent und MCP
  - `shared_tls_listen`

  Sie liegen in den globalen Einstellungen (`PATCH /settings/global`) und synchron in der `config.json` (Abschnitt 2).
- **Gemeinsamer Port zur Laufzeit:** Der Router existiert schon, er wird an- und abgeschaltet, ohne Neustart.
- **Abgeleitete Adressen** in B aus Name und Ports.
- **ACME-Namen** aus den Adressen statt aus `acme_names`. Das Feld bleibt nur für den Umstieg lesbar.
- **Port 80:** Der Challenge-Listener öffnet und schließt sich mit dem Auftrag und antwortet nur auf offene Tokens.
- **Neuer Endpunkt** `GET /system/tls/certificates/:id/download` (Recht `system:endpoints`, nur Kette, Audit-Eintrag).
- **Enroll-Befehl:** Passt die Agent-Adresse ohne Port weder zum gemeinsamen Port noch zu einem Proxy, warnt der Assistent, statt einen Befehl auszugeben, der ins Leere läuft.
- **`lcm.service`:** `CAP_NET_BIND_SERVICE` mit Kommentar. Das Update bringt das mit, Handarbeit ist keine nötig.
- **Schalter:** `agent_enabled` und `mcp_enabled` speichern mit den Adressen in einem `PATCH` und schalten Listener bzw. Route zur Laufzeit um, mit derselben Wirkungsprobe wie die Ports.
- **MCP:** `mcp_tls` ist immer wahr (Migration setzt es), die Bind-Adresse folgt der Oberfläche. `POST /settings/mcp` nimmt nur noch `enabled`; übrige Felder werden ignoriert.
- **ACME:** Nach jedem Speichern, das die Namen ändert oder ACME einrichtet, prüft LCM die Abdeckung und startet bei Bedarf einen Lauf (Abschnitt 5).

## 11. API-Vertrag (Grundlage für Backend und Oberfläche)

`GET/PATCH /api/v1/settings/global` bekommt:

| Feld | Typ | Bedeutung |
|---|---|---|
| `address_mode` | `separate` \| `one_name` \| `shared_port` | Vorgabe `separate` |
| `public_host` | string | nur `one_name`: der eine Name, ohne Schema, Port, Pfad |
| `ui_port`, `agent_port`, `mcp_port` | int | Ports in A und B |
| `shared_tls_port` | int | Modus C, Vorgabe 443 |
| `agent_enabled` | bool | Agent an/aus, Vorgabe an |
| `mcp_enabled` | bool | MCP an/aus, jetzt auch hier schreibbar |

PATCH liefert zusätzlich `ui_url`, wenn sich der Oberflächen-Port geändert hat. Einschalten ohne Namen in C lehnt PATCH mit `settings.invalid` ab; ein leer gespeicherter Name schaltet den Dienst aus.

Nur in der Antwort (nicht schreibbar):

- `address_locks`: `{ui_port, agent_port, mcp_port, shared_tls_port}` als bool, wahr nur noch, wenn eine Umgebungsvariable den Wert festlegt (ab P16; bis dahin wie gebaut aus der `config.json`).
- `agent_servers`: Zahl der Server mit Transport Agent, für die Rückfrage vor dem Ausschalten.
- `effective_urls`: `{ui, agent, mcp}`, die Adressen, die LCM tatsächlich in Befehle schreibt (in B abgeleitet).

`public_base_url`, `public_agent_url`, `public_mcp_url` bleiben für A und C. Die ACME-Einstellungen behalten ihren Endpunkt, `acme_names` und die Auswahl je Name entfallen in der Oberfläche.

**Assistent (P13/P14):**

| Feld | Wo | Bedeutung |
|---|---|---|
| `setup_step` | GET/PATCH `/settings/global`, GET `/auth/me` | Schritt, an dem der Assistent steht: `session`, `addresses`, `services`, `access`, `certificate`, `onboarding`, `mail`, `retention`, `backup`, `subscription`, `intro`, leer = fertig. Ersetzt `address_setup` (Migration: `open` → `addresses`, sonst leer). Eine frische Installation beginnt mit `session`. PATCH darf zwischen `session` und `subscription` vor und zurück gehen; aus `intro` und leer gibt es kein Zurück in den Assistenten. |
| `trust_proxy_header` | GET/PATCH `/settings/global` | bool: LCM glaubt der Client-Adresse aus `X-Forwarded-For`, aber nur von `trusted_proxies`. Wirkt ohne Neustart. |
| `trusted_proxies` | GET/PATCH `/settings/global` | Liste von IPs oder Netzbereichen (CIDR). Abgelehnt: ungültige Einträge, `0.0.0.0/0`, `::/0`, leere Liste bei `trust_proxy_header: true`. |

**`config.json` synchron (P16):** Gespiegelt werden `address_mode`, `public_host`, die drei Adressen, `port`, `agent_port`, `mcp_port`, `shared_tls_listen` (aus Modus und gemeinsamem Port), `agent_enabled`, `mcp_enabled`, `trust_proxy_header`, `trusted_proxies` sowie die Netzbereiche `allowed_ips`, `agent_allowed_ips`, `mcp_allowed_ips`. `address_locks` meldet nur noch Felder, die eine Umgebungsvariable festlegt.

## 12. Pakete

| Paket | Inhalt | Status |
|---|---|---|
| P1 | Backend: `address_mode`, Ports in der DB, abgeleitete Adressen, Prüfungen, Tests | zu prüfen |
| P2 | Backend: gemeinsamer Port aus DB, Laufzeit-Umschaltung, Port-Wechsel mit Wirkungsprobe, `CAP_NET_BIND_SERVICE` | zu prüfen |
| P3 | Backend: Port-80-Listener nur während der Challenge, ACME-Namen aus Adressen, Umstieg, Download-Endpunkt | zu prüfen |
| P4 | Oberfläche: Block mit Modus, Adressen, Ports, Zugriff, Hilfe-Spalte | zu prüfen |
| P5 | Oberfläche: Let's Encrypt im Block, „Weitere Einstellungen“, Download | zu prüfen |
| P6 | Einrichtungsassistent: Schritt „Wie ist LCM erreichbar?“ nach der 2FA | zu prüfen |
| P7 | Doku (`docs/guides/zertifikate.md` + EN), Release-Hinweis, E2E-Test der drei Modi auf pve-test (Ubuntu und Debian) | zu prüfen |
| P8 | Backend: `agent_enabled`, Schalter im PATCH mit Laufzeitwirkung, C ohne Namen = aus, MCP immer TLS, Bind wie Oberfläche, `agent_servers`, Sperre bei `agent_port: 0` | zu prüfen |
| P9 | Backend: Ausstellen bei fehlender Abdeckung nach dem Speichern, Wartezeit nach Fehlschlag, Test „Upload geht vor“ | zu prüfen |
| P10 | Oberfläche: Schalter im Block, C ohne Namen, Vorschau mit Kopierknopf und „(nicht aktiv)“, Zugriff zurück ins Zahnrad | zu prüfen |
| P11 | Oberfläche: Seite MCP verschlankt, Karte „Onboarding“ mit Anleitung und Agent-Schalter, Hinweis im Join-Assistenten bei ausgeschaltetem Agenten | zu prüfen |
| P12 | Doku, Release-Hinweis (MCP nur noch TLS, Agent-Schalter), E2E | zu prüfen |
| P13 | Backend für den Assistenten: Schrittstand auf dem Server, `trust_proxy_header`/`trusted_proxies` in den Einstellungen | zu prüfen |
| P16 | Backend: `config.json` synchron (Schreiben beim Speichern, Übernahme von Handänderungen beim Start, Sperren nur noch durch Umgebungsvariablen), Oberfläche ohne Sperrhinweis aus der Datei | zu prüfen |
| P14 | Oberfläche: Assistent Schritte 3–8 (Abschnitt 6), Weiterleitung nach Schritt 6 | zu prüfen |
| P15 | Oberfläche: Einführung auf der Seite des LCM-Hosts mit Trivy, apt-cacher-ng, CrowdSec LAPI | zu prüfen |
| P17 | Oberfläche: Assistent nach Handtest 2 (Sitzungsschritt, Zurück, Hilfe-Spalte, Wortwahl „Schnittstelle“, nur Namen in allen Modi, Vorschau ohne Kopierknöpfe, Zertifikat anfordern, Link Nutzungsbedingungen, Onboarding ohne Überspringen, Backup-Uhrzeit; dazu Agent-Unterkarte ohne Schalter mit Stand, Kopierfeld mittig, Karte Reverse-Proxy im Block) | zu prüfen |
| P18 | Backend: Schritt `session`, Zurückgehen im Assistenten, Trivy immer mit Sandbox | zu prüfen |
| P19 | Oberfläche: Einführung als Seitenfenster, Trivy mit Sandbox installieren, 2FA ohne Auto-Absenden | zu prüfen |
| P20 | Einführung nach Handtest 3: nur erklären und navigieren, Stationen 1–8 (Abschnitt 6), „Sandbox nachrüsten“ auf der Trivy-Karte wieder sichtbar, Erfolgsmeldung von Let's Encrypt im Assistenten in voller Breite | zu prüfen |
| P21 | Backend: Schritte `mail`, `retention`, `subscription` im `setup_step`; eigene Dienst-Adresse für Subscriptions entfernt (feste Adresse); Firewall des LCM-Hosts nennt den MCP-Port; Ambient-Capabilities leeren | zu prüfen |
| P22 | Oberfläche: Assistent-Schritte E-Mail, Aufbewahrung, Subscription/Kanal; Let's-Encrypt-Schritt behält die Eingaben und sperrt „Anfordern“ nach Erfolg; Zähler der Einführung durchgehend; Feld „Eigene Dienst-Adresse“ entfernt | zu prüfen |
| P23 | Oberfläche: Zertifikat-Schritt ohne Namen (Hinweis statt Let's Encrypt, Proxy vorgewählt), keine Weiterleitung ohne Namen, Kanalwahl im Assistenten erst mit „Weiter“, Empfehlung Enterprise nach Aktivierung | zu prüfen |
| P24 | Firewall des LCM-Hosts: eigene Ports mit Schnittstelle beschriftet („LCM GUI“, „LCM Agent“, „LCM MCP“, im Modus C „LCM GUI · Agent · MCP“), Port 80 als „LCM Let's Encrypt (HTTP-01)“ mit Warnhinweis, solange keine Regel ihn freigibt | zu prüfen |
| P25 | Eingeschränkter Modus: Werkzeug-Erkennung sucht ufw ohne Shim-Verzeichnis (Shim täuschte ufw vor, Firewall scheiterte mit „sudo: a password is required“) | zu prüfen |

## 13. Teststand (22.11.2026, Testbau 2.3.0~alpha.1)

| System | Ergebnis |
|---|---|
| CT 333 Debian 13, eingeschränkt | Update aus 2.2.2~alpha.4 ohne Änderung (Modus A, Ports 9310/9320), Capability gesetzt, Port 80 zu. B mit Portwechsel, UI-Port 9410 im laufenden Betrieb, alter Port nach 10 s zu, C auf 443: Oberfläche, Agent-WebSocket (101) und MCP per Name erreichbar, Neustart behält C, zurück auf A |
| CT 330 Ubuntu 24.04, eingeschränkt | derselbe Ablauf, gleiches Ergebnis |
| CT 340 Ubuntu, Let's Encrypt | Update ohne Änderung: Zertifikat, Namen und Ports bleiben, kein Umstiegshinweis nötig |

Abgelehnt wie vorgesehen: Port doppelt, Modus B mit Schema und Port, Modus C mit gleichen Namen, MCP auf belegtem Port.

Nicht live geprüft: die HTTP-01-Prüfung gegen eine echte CA (Port 80 nur während der Prüfung, nur Unit-Tests), der Download-Knopf mit einem ACME-Zertifikat, der Assistenten-Schritt auf einer frischen Installation, das Bild im Browser.

CT 331 (2.3.0~alpha.2, Modus C auf 443, drei Namen, DNS-01 über IONOS), 22.11.:

- Oberfläche und API per Name mit strenger Zertifikatsprüfung erreichbar.
- Agent-Enroll von CT 341 über `https://agent.example.techeve.de` gelungen, Server online.
- MCP per Name: Das erste Zertifikat entstand, bevor MCP an war, und trug nur zwei Namen. Nach dem Einschalten stellte LCM nicht neu aus, MCP bekam das Zertifikat der eigenen CA (behoben mit P9). Nach einem Lauf von Hand: alle drei Namen, MCP streng geprüft erreichbar.
- Eigenes Zertifikat für MCP hochgeladen, danach Let's Encrypt neu ausgestellt: MCP liefert weiter das eigene aus.

CT 331 mit 2.3.0~alpha.4 (P8–P11), 22.11.:

- Agent aus: der verbundene Agent trennt sich, der Agent-Name bekommt keine Antwort; Agent an: der Agent verbindet sich wieder.
- MCP aus: der MCP-Name wird abgewiesen, nicht zur Oberfläche weitergeleitet.
- MCP ohne Namen einschalten: abgelehnt (400). MCP-Namen leer speichern: MCP aus. Namen zurück und an: MCP erreichbar.
- MCP eingeschaltet, nachdem das Zertifikat nur Oberfläche und Agent trug: LCM stellt von selbst für alle drei aus.
- In alpha.3 löste jeder Schalterwechsel eine Ausstellung aus, weil nur ein Zertifikat mit genau denselben Namen als Abdeckung galt. Behoben in alpha.4: Ein Zertifikat mit mehr Namen deckt ab. Vier Wechsel danach: keine Ausstellung.
- Let's-Encrypt-Grenze beachten: Für die drei Namen sind diese Woche 4 von 5 Ausstellungen verbraucht.

2.3.0~alpha.5 (P13–P16), 22.11.:

- CT 332 Debian 12, frisch installiert: Die `config.json` trägt alle gespiegelten Schlüssel. Port 9410 und `trusted_proxies` von Hand eingetragen, Neustart: LCM übernimmt beide (Audit `actor=config.json`), lauscht auf 9410. Der Assistent steht auf `addresses`. Danach zurück auf 9310 für Tonys Handtest.
- CT 331 Update aus alpha.4 in Modus C: Modus, Namen und Schalter stehen danach in der `config.json`, Oberfläche, API und MCP auf 443 weiter streng geprüft erreichbar.
- E2E: Adressen, Einstellungen, Assistent, Server aufnehmen, Sicherheit, Schutz, Barrierefreiheit grün. Gefunden und behoben: Im Entwicklungsmodus lief kein Abgleich, die Ports der Test-`config.json` wurden nicht übernommen.
- Assistent im Browser: Handtest 2 von Tony, Ergebnis in Abschnitt 6 und P17–P19 eingearbeitet.

2.3.0~alpha.6 (P17–P19), 22.11.: vitest 1167 grün, E2E (Assistent, Einstellungen, Anmeldung, Server-Übersicht, Rahmen, Aufnehmen, Barrierefreiheit) 147 grün. CT 332 frisch installiert, der Assistent beginnt bei `session`. Handtest im Browser steht aus.

2.3.0~alpha.7, 22.11.: `fix/repo-kanaele` hereingeholt (Kanäle nach Suite, apt-Quelle von `stable` auf `community`). CT 333 Update aus alpha.1: Postinstall stellt `techeve.sources` auf `community` um, Debian-Quellen bleiben, `apt-get update` ohne Fehler. Go-Tests core, cmd/app, api grün.

2.3.0~alpha.8 (P20), 22.11.: vitest 1202 grün, E2E Assistent, Server-Übersicht, Rahmen, Endpunkte, Server-Betrieb grün. Neu aufgesetzt: CT 334, leeres Debian 13 (Snapshot `nackt`), alpha.8 aus dem .deb installiert. Zum fehlenden „Sandbox nachrüsten“ auf CT 332: bubblewrap war installiert; der Knopf stand nur im Einführungsfenster, weil die Karte ihre Knöpfe während der Einführung versteckte (behoben).

Sandbox auf CT 334 (Debian 13 LXC, Kernel 7.0 pve), 22.11.: bubblewrap 0.12 installiert und als `lcm` von Hand lauffähig; aus LCM heraus Abbruch „Unexpected capabilities but not setuid“, weil `lcm.service` `CAP_NET_BIND_SERVICE` als Ambient-Capability setzt und bubblewrap sie erbte (mit `setpriv` nachgestellt). Landlock fehlt im Proxmox-Kernel in `/sys/kernel/security/lsm`. Fix 8ae1eac1: LCM leert beim Start die Ambient-Capabilities. Testbinary auf CT 334: `CapAmb 0`, `CapEff 0x400`, Ports 9310/9320/9330 offen.

2.3.0~alpha.9 (P21, P22), 22.11.: vitest 1215 grün, Go-Tests router, services, cmd/app, storage grün. CT 334 als Update aus alpha.8 eingespielt, `CapAmb 0`. Der Assistent ist dort schon durchlaufen (`setup_step` leer); die neuen Schritte zeigt erst eine frische Installation.

Sandbox erneut, CT 334 frisch mit alpha.9: wieder „Unexpected capabilities“. Ursache: Der erste Fix leerte die Ambient-Capabilities nur im Haupt-Thread; 7 von 8 Threads trugen sie weiter, und Go startet Kindprozesse aus beliebigen Threads. Fix efcd1f25: `syscall.AllThreadsSyscall` (Pakete mit `CGO_ENABLED=0`). Testbinary auf CT 334: alle Threads `CapAmb 0`, `CapEff 0x400`. Tony bestätigt: „Sandbox nachrüsten“ läuft durch, Trivy ist in der Sandbox aktiv.

2.3.0~alpha.10 (P23 + Sandbox-Fix), 22.11.: vitest 1233 grün, Go router/cmd grün. CT 334 aus `nackt` frisch installiert: alle 8 Threads `CapAmb 0`, Assistent bei `session`.

2.3.0~alpha.11 (P24), 22.11.: Sandbox mit alpha.10 von Tony bestätigt (Trivy in einem Zug). vitest 1236 grün, Go services/cmd grün. CT 334 auf alpha.11 aktualisiert (ohne Rücksetzen). Tony: Beschriftung der LCM-Ports in der Firewall bestätigt (nach Neuladen der Seite); Hinweis auf Port 80 bei HTTP-01 noch ungetestet. Hinweis auf Port 80 per E2E-Test geprüft (server-sicherheit.spec.ts, Rollen gemockt): Bezeichnungen, Hinweis, Knopf übernimmt die Regel mit Bemerkung, Hinweis verschwindet.

2.3.0~alpha.12 (P25), 22.11.: Go services grün. CT 334 aktualisiert; als lcm-svc findet die Erkennung kein ufw mehr, nft vorhanden. Tony: Firewall lässt sich im eingeschränkten Modus auf Debian einschalten.

Unbekannte Namen auf dem gemeinsamen Port landen bei der Oberfläche. Ein ausgeschaltetes MCP tat das zum Teststand ebenso; seit P8 bekommen ein ausgeschalteter Agent und ein ausgeschaltetes MCP am eigenen Namen keine Antwort (die Verbindung wird geschlossen).

Fehler beim ersten Handtest (22.11., Tony, CT 331 frisch): Nach der Erst-Anmeldung stand „Seite nicht gefunden“, der Passwortwechsel war nicht erreichbar. Ursache: Der Adress-Schritt leitete die Kontoseite weiter, die Pflichtschritte leiteten zurück. Behoben in 2.3.0~alpha.2: Der Adress-Schritt wartet, bis Passwort und 2FA erledigt sind (Test `routeRedirect`).
