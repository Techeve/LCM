---
sidebar:
  order: 24
title: Öffentliche Adressen & Adressmodus
description: Wie LCM von außen erreichbar ist - drei Adressmodi, Ports, Schalter für Agent und MCP, Reverse-Proxy, die config.json im Gleichklang mit der Oberfläche und der Einrichtungsassistent.
---

LCM hat drei Dienste: die Weboberfläche mit der REST-API, den Agent-Port für
LCM Remote und die MCP-Schnittstelle. Unter *Einstellungen → Allgemein* legst
du im Block **Öffentliche Adressen** fest, unter welchen Namen und auf welchen
Ports Browser, Agenten und MCP-Clients sie erreichen. Ganz oben wählst du den
**Adressmodus**. Der Rest des Blocks folgt dieser Wahl: Adressfelder, Ports,
Schalter für Agent und MCP, Let's Encrypt. Rechts daneben (auf schmalen Bildschirmen darunter)
steht eine Hilfe, die zum gewählten Modus passt.

Aus diesen Adressen baut LCM alles, was nach außen zeigt: den Befehl für
`lcm-agent enroll`, die MCP-Adresse, den Assistenten *Server hinzufügen* und die
Links in E-Mails. Dazu nimmt LCM die Namen für das Zertifikat von Let's Encrypt
([Zertifikate, Let's Encrypt & Port 443](/guides/zertifikate/)).

## Die drei Adressmodi

| Modus | Ports | Namen | Felder | Wann passt er |
|---|---|---|---|---|
| **A: Eigene Ports, eigene Namen** (Standard) | je Dienst einer, einstellbar (Vorgabe 9310, 9320, 9330) | je Dienst eine Adresse, mit oder ohne Port | drei Adressfelder und drei Portfelder | Ein Reverse-Proxy steht davor, oder jeder Dienst soll unter eigenem Namen laufen. Das ist der Stand früherer Versionen. |
| **B: Eigene Ports, ein Name** | je Dienst einer, einstellbar | ein Name für alle Dienste | ein Namensfeld und drei Portfelder | Betrieb ohne Proxy mit einem Zertifikat. |
| **C: Ein Port, eigene Namen** | ein gemeinsamer Port, Vorgabe 443 | je Dienst ein eigener Name; Pflicht nur für die Oberfläche | drei Namensfelder und *Gemeinsamer Port* | Im Browser steht kein Port. LCM trennt die Dienste nach dem angefragten Namen (SNI). |

**Modus A.** Du trägst die Adressen frei ein, etwa `https://lcm.example.com`.
Lässt du die Adresse für Agenten oder MCP leer, nimmt LCM den Namen der
Weboberfläche mit dem Port des Dienstes. Ohne Adresse der Weboberfläche zeigt
LCM eine Warnung: Links in E-Mails entstehen dann aus der lokalen Konfiguration
und sind von außen womöglich nicht erreichbar.

**Modus B.** Du trägst einen Namen ein, ohne `https://`, Port und Pfad. LCM
rechnet die drei Adressen daraus aus und zeigt sie schreibgeschützt darunter,
zum Beispiel `https://lcm.example.com:9310`, `https://lcm.example.com:9320` und
`https://lcm.example.com:9330`.

**Modus C.** Die Weboberfläche braucht einen Namen. Agent und MCP bekommen je
einen eigenen, müssen aber keinen haben. Alle Namen zeigen im DNS auf den
LCM-Host. Die Adressen lauten `https://<Name>:<Port>`, beim Port 443 ohne Port.
Ohne Namen (Aufruf per IP) und bei jedem unbekannten Namen landet die
Verbindung bei der Weboberfläche. Ein Dienst ohne Namen ist aus, siehe
[Agent und MCP an- und ausschalten](#agent-und-mcp-an--und-ausschalten).

Beim Wechsel des Modus bleibt, was schon im Formular steht. Leere Ports
bekommen die Vorgabe, und beim Wechsel in Modus C kürzt die Seite die Adressen
auf ihre Namen. Gespeichert wird erst mit *Speichern*.

### Was beim Speichern geprüft wird

- **Ports:** Jeder Port liegt zwischen 1 und 65535, und kein Port gilt für zwei
  Dienste. Auch der Port der Let's-Encrypt-Prüfung (`acme_http_listen`, Standard
  80) ist belegt. Der gemeinsame Port zählt nur in Modus C.
- **Tausch:** Einen Port, den ein anderer Dienst noch hält, lehnt LCM ab, auch
  bei einem Tausch von Oberfläche und Agent. Wechsle erst auf einen freien Port.
- **Modus B:** Es gibt genau einen Namen.
- **Modus C:** Der Name der Weboberfläche ist Pflicht. Die gesetzten Namen sind
  verschieden. Einen Dienst ohne Namen schaltest du nicht ein.

Bestehende Installationen laufen in Modus A weiter. Steht in der `config.json`
ein `shared_tls_listen`, startet LCM beim ersten Start nach dem Update in Modus C
mit diesem Port. Danach lässt sich der Modus in der Oberfläche wie jeder andere
ändern.

## Welche Ports du freigibst

| Modus | Auf dem LCM-Host öffnen | Zusätzlich |
|---|---|---|
| **A, mit Reverse-Proxy** | Oberfläche, Agent und MCP nur für den Proxy, der Agent-Port auch für Agenten, die ihn direkt erreichen | Nach außen braucht nur der Proxy Port 443. |
| **A, ohne Proxy** | Oberfläche, Agent und MCP mit ihren Ports (Vorgabe 9310, 9320, 9330) | |
| **B** | Oberfläche, Agent und MCP mit ihren Ports | Kurz Port 80, wenn Let's Encrypt per HTTP-01 prüft. |
| **C** | nur der gemeinsame Port (Vorgabe 443) | Kurz Port 80 bei HTTP-01. |

Die Weboberfläche bindet an `host` aus der `config.json`. MCP bindet an dieselbe
Adresse, es hat kein eigenes Feld dafür. Soll jemand von außen darauf zugreifen,
ändere `host`. Der Agent-Port bindet an `agent_host`, ab Werk an alle
Schnittstellen.

In Modus C lauschen die Einzelports weiter. Sperre sie in der Firewall, wenn
nur der gemeinsame Port offen sein soll. Den Agent-Port schaltest du mit dem
Schalter für den Agenten ab, MCP mit seinem Schalter.

Port 80 braucht LCM nur für HTTP-01, und auch nur, solange die Prüfung läuft.
Mit DNS-01 brauchst du ihn nie.

Ports unter 1024 darf der Dienst der Paketinstallation öffnen: `lcm.service`
gibt ihm dafür genau eine Berechtigung (`CAP_NET_BIND_SERVICE`). Du musst dafür
nichts einstellen. Alle anderen Härtungen der Dienstdatei bleiben.

## Modus A hinter einem Reverse-Proxy

Der Proxy bekommt je Name einen Weg zum passenden Port von LCM. Er muss drei
Dinge weiterreichen:

| Dienst | Port | Was der Proxy weiterreicht |
|---|---|---|
| **Weboberfläche** | `ui_port` (9310) | Alle Pfade über HTTPS. WebSocket für `/api/v1/events` (Live-Anzeige) und `/api/v1/servers/<id>/terminal` (Web-Konsole), Server-Sent Events für `/api/v1/system/logs/stream` ohne Puffer. |
| **Agent** | `agent_port` (9320) | `/mqtt` als WebSocket (Subprotokoll `mqtt`) mit Upgrade-Weiterleitung. Die Verbindung bleibt dauerhaft offen: Das Lesetimeout muss deutlich über 30 Sekunden liegen, besser eine Stunde. |
| **MCP** | `mcp_port` (9330) | `/mcp` über HTTPS. MCP spricht immer TLS, der Proxy muss also auch zu diesem Port HTTPS sprechen. |

Für alle drei gilt:

- Der Proxy lässt den Host-Namen unverändert und setzt `X-Forwarded-Proto` und
  `X-Forwarded-For`. In LCM gehören `trust_proxy_header` und
  `trusted_proxies` (die Adresse des Proxys) in die Einstellungen, im
  Einrichtungsassistenten beim Schritt „Zertifikat“ unter „Reverse-Proxy davor“.
  Sie wirken sofort, ohne Neustart.
- Den Agent-Port reichst du am besten per TCP-Passthrough durch. Terminiert der
  Proxy TLS, sieht er den Verwaltungskanal der Agenten.
- Trage die Namen, unter denen der Proxy die Dienste veröffentlicht, in die
  Adressfelder ein.

Fertige Konfigurationen stehen unter
[Reverse-Proxy & Absicherung von außen](/guides/reverse-proxy/) für die
Weboberfläche und unter [LCM Remote](/guides/remote/#hinter-einem-reverse-proxy)
für den Agent.

Hat der Proxy ein eigenes Zertifikat, brauchst du Let's Encrypt in LCM nicht.
Willst du es trotzdem, nimm DNS-01. Bei HTTP-01 müsste Port 80 auf LCM zeigen,
und das geht hinter einem Proxy meist nicht.

## Agent und MCP an- und ausschalten

Unter der Adresse von Agent und MCP steht je ein Schalter. Beide wirken ohne
Neustart.

- **Agent:** Ab Werk an, bestehende Installationen bleiben an. Ist er aus, nimmt
  der Agent-Port keine Verbindung an, und verbundene Agenten trennen sich. In
  Modus C bekommt der Agent-Name keine Antwort. Vor dem Ausschalten nennt die
  Seite, wie viele Server den Agenten nutzen: Sie gehen beim Speichern offline.
  Der Assistent *Server hinzufügen* erzeugt dann keinen Enroll-Befehl, sondern
  verweist auf die Einstellungen.
- **MCP:** Ist es aus, nimmt LCM keine MCP-Anfragen an. Bestehende MCP-Schlüssel
  bleiben, sind aber wirkungslos.
- **Ein Port allein schaltet nichts ein.** In Modus B steht in der Vorschau der
  Adressen hinter Agent und MCP „(nicht aktiv)“, solange ihr Schalter aus ist.
- **Modus A und B:** Agent und MCP tragen ohne eigene Adresse den Namen der
  Weboberfläche. Die Schalter sind frei.
- **Modus C ohne Namen:** Fehlt der Name von Agent oder MCP, ist der Dienst aus
  und sein Schalter gesperrt, mit dem Hinweis „Für diesen Dienst ist keine Domain
  eingerichtet.“ Trägst du einen Namen ein, springt der Schalter auf an. Du kannst
  ihn vor dem Speichern wieder ausschalten. Speicherst du den Namen leer, schaltet
  LCM den Dienst dabei aus. Über die API lehnt `PATCH /api/v1/settings/global`
  das Einschalten ohne Namen mit `settings.invalid` ab.

Die Schalter stehen an zwei Orten, es ist jeweils dieselbe Einstellung: der
Agent-Schalter in der Karte **Onboarding** ([Onboarding](#onboarding)), der
MCP-Schalter auf der Seite *MCP* ([MCP-Schnittstelle](/guides/mcp/)).

## Ports ändern

In Modus A und B stellst du die Ports von Oberfläche, Agent und MCP in der
Oberfläche ein, in Modus C den gemeinsamen Port. Die Felder haben die Vorgaben
aus der Tabelle oben.

- **Port der Oberfläche:** LCM bindet den neuen Port zuerst. Klappt das nicht
  (belegt, keine Berechtigung), bleibt der alte Port in Betrieb, und LCM
  speichert nichts. Vor dem Speichern nennt die Seite die neue Adresse. Nach dem
  Speichern leitet sie dorthin weiter. Der alte Port schließt nach 10 Sekunden,
  offene Verbindungen bleiben davon unberührt.
- **Agent-Port, MCP-Port und gemeinsamer Port:** LCM schaltet sie ohne Neustart
  um. Das gilt auch für die Schalter von Agent und MCP.
- **Bereits eingerichtete Agenten** tragen die Adresse aus ihrer Einrichtung.
  LCM schickt ihnen keine neue. Nach einem Wechsel des Agent-Ports oder des
  Namens richtest du sie mit einem neuen Token erneut ein
  (`lcm-agent enroll`).
- Die Firewall musst du selbst anpassen. Die Hilfe neben dem Block nennt die Ports
  des gewählten Modus.

### Die `config.json` im Gleichklang

Die Oberfläche und die `config.json` führen dieselben Werte. Welche, steht in der
Tabelle:

| Schlüssel in der `config.json` | Einstellung |
|---|---|
| `address_mode`, `public_host` | Adressmodus und der eine Name (Modus B) |
| `public_base_url`, `public_agent_url`, `public_mcp_url` | die Adressen |
| `port`, `agent_port`, `mcp_port` | die Ports von Oberfläche, Agent und MCP |
| `shared_tls_listen` | der gemeinsame Port, nur in Modus C (sonst leer). Ein Host davor (`127.0.0.1:443`) bestimmt die Bind-Adresse und bleibt erhalten. |
| `agent_enabled`, `mcp_enabled` | die Schalter von Agent und MCP |
| `trust_proxy_header`, `trusted_proxies` | woher LCM die Client-Adresse nimmt, und welchen Proxys es glaubt |
| `allowed_ips`, `agent_allowed_ips`, `mcp_allowed_ips` | die [erlaubten Netzbereiche](/guides/netzbereiche/) |

- **Speichern in der Oberfläche** schreibt die Werte in die Datei, atomar und mit
  den Rechten, die sie hat. Alle anderen Schlüssel (Zertifikate, Pfade, Geheimnisse)
  bleiben, wie sie sind. Klappt das Schreiben nicht, gilt die Änderung trotzdem, und
  die Antwort enthält `config_sync_error`.
- **Beim Start** liest LCM die Datei. Hast du einen Wert von Hand geändert, gilt er
  und kommt in die Einstellungen; das Log nennt jeden übernommenen Schlüssel. LCM
  prüft ihn dabei wie eine Eingabe in der Oberfläche. Ist er ungültig, gilt der Wert
  der Einstellungen weiter, und das Log warnt bei jedem Start, bis du die Datei
  korrigierst. Fehlt ein Schlüssel, trägt LCM ihn mit dem Wert der Einstellungen ein.
- **Gesperrt** ist nur, was eine Umgebungsvariable festlegt, denn LCM kann sie nicht
  ändern: `LCM_PORT` sperrt den Port der Oberfläche, `LCM_AGENT_PORT` den des
  Agenten (`0` schaltet den Agenten ab). Die Oberfläche zeigt das Feld dann
  gesperrt, mit dem Hinweis „festgelegt durch eine Umgebungsvariable“. Ein Wert
  in der `config.json` sperrt nichts.
- **Let's Encrypt** steht nicht in der `config.json`, nur in der Oberfläche.
- **Alte Dateien:** Ein `agent_port` von `0` heißt „Agent aus“. LCM übernimmt es
  als `agent_enabled: false` und behält den bisherigen Agent-Port. In Modus C gilt
  das nicht, dort heißt `0` nur: kein eigener Port neben dem gemeinsamen.

## Zertifikat, Zugriff und Let's Encrypt im Block

- **Zahnrad hinter einer Adresse:** *Zertifikat & Sicherheit*. Der Dialog hat zwei
  Teile. Oben steht das Zertifikat, das LCM für den Namen ausliefert. Du kannst
  eines von Hand hochladen oder wieder entfernen. Darunter stehen die **erlaubten
  Netzbereiche** dieses Dienstes ([Erlaubte Netzbereiche](/guides/netzbereiche/)).
  Einen eigenen Abschnitt *Zugriff* im Block gibt es nicht.
- **Let's Encrypt:** eingeklappt, in allen Modi wählbar. Die Namen kommen aus
  den Adressen, es gibt ein Feld weniger als früher. Nach dem Speichern stellt LCM
  das Zertifikat von selbst aus
  ([Zertifikate, Let's Encrypt & Port 443](/guides/zertifikate/#lets-encrypt-acme)).

Diese Teile sehen nur Admins (Recht `system:endpoints`).

## Onboarding

*Einstellungen → Allgemein*, Karte **Onboarding**: Hier steht, wie neue Server zu
LCM kommen. Die Karte hat drei eingeklappte Unterkarten.

1. **Vorgabe für neue Server:** Benutzer und Port, mit denen LCM neue Server
   vorbelegt.
2. **Onboarding-Schlüssel:** der öffentliche Schlüssel von LCM mit Kopierknopf. Die
   Anleitung darunter zeigt, wie er auf den Server kommt: für Debian, Ubuntu und
   Linux Mint, für die RHEL-Familie (Rocky, Alma, Fedora) und für andere Systeme.
   Sie nennt Benutzer, Verzeichnis und Rechte (`~/.ssh/authorized_keys`, Verzeichnis
   `700`, Datei `600`).
3. **Mit Agent verwenden:** was der Agent ist und wann er passt, der Schalter für
   den Agenten, die Adresse, unter der Agenten sich verbinden (in Modus B mit dem
   Port des Agenten), und ein Knopf zum Assistenten *Agent-Server hinzufügen*.
   Ohne Domain in Modus C weist die Karte auf den Block der Adressen hin.

## Einrichtungsassistent

Auf einer frischen Installation führt der Assistent den ersten Administrator
nach dem Passwortwechsel und der Zwei-Faktor-Anmeldung Schritt für Schritt durch
die Einrichtung. Jeder Schritt speichert für sich und nutzt dieselben Bausteine wie
die Einstellungen. Der Server merkt sich den Stand (`setup_step`), damit der
Assistent nach einem Wechsel der Adresse und neuer Anmeldung an derselben Stelle
weitermacht. Er geht nur vorwärts.

| Schritt | Inhalt |
|---|---|
| 1. **Wie ist LCM erreichbar?** | Adressmodus, Adressen und Ports wie im Block der Einstellungen. Dieser Schritt lässt sich nicht überspringen. |
| 2. **Welche Dienste sollen laufen?** | Je ein Schalter für Agent und MCP mit einem Satz Erklärung. In Modus C nur für Dienste mit Namen, die anderen stehen als „keine Domain eingerichtet“ da. |
| 3. **Wer darf LCM erreichen?** | Je Dienst „alle Netze“ oder eigene Netzbereiche, für die Oberfläche und die eingeschalteten Dienste. Jede Gruppe speicherst du für sich. |
| 4. **Zertifikat** | Der Schritt sagt, dass LCM für eigene Namen ohne Hilfe kein gültiges Zertifikat hat. Du wählst *Let's Encrypt* (darunter der Block wie in den Einstellungen, Speichern stellt aus) oder *Reverse-Proxy davor* (darunter die Adressen des Proxys, denen LCM die Client-Adresse glaubt: `trust_proxy_header` und `trusted_proxies`). Ohne beides und ohne hochgeladenes Zertifikat liefert LCM sein eigenes aus. Danach leitet der Assistent auf die neue Adresse der Oberfläche weiter. Ändert sich dabei der Name, meldest du dich neu an und machst beim nächsten Schritt weiter. |
| 5. **Neue Server aufnehmen** | Der Vorgabe-Benutzer für neue Server und der Onboarding-Schlüssel mit Kopierknopf, dazu der Hinweis, ihn gleich beim Anbieter zu hinterlegen (Hetzner: SSH-Key im Projekt, Proxmox: Cloud-Init, sonst `authorized_keys`). Lässt sich überspringen. |
| 6. **Automatisches Backup** | An oder aus, Intervall, Uhrzeit, Aufbewahrung und die Passphrase, denn geplante Backups sind verschlüsselt. Lässt sich überspringen. Bei jedem Update, das über LCM startet, legt LCM vorher ohnehin ein Backup an. |

Überspringen heißt: „Das kannst du später in den Einstellungen einrichten.“ Nach
dem sechsten Schritt öffnet LCM die Seite des **LCM-Hosts** in der Serverliste. Eine
Einführung erklärt, dass der Host, auf dem LCM läuft, selbst als Server in der Liste
steht, und bietet weitere Dienste zum Installieren an: **Trivy** (der CVE-Scanner,
empfohlen, läuft in einer Sandbox), **apt-cacher-ng** (Zwischenspeicher für
Pakete) und **CrowdSec LAPI** (gemeinsame Sperrliste). Die Einführung lässt sich
schließen und kommt nicht wieder.

Später änderst du alles unter *Einstellungen → Allgemein*. Im Demo- und
Entwicklungsmodus und auf bestehenden Installationen erscheint der Assistent nicht.

## Über die API

`GET` und `PATCH /api/v1/settings/global` kennen diese Felder:

| Feld | Bedeutung |
|---|---|
| `address_mode` | `separate` (Modus A, Standard), `one_name` (B) oder `shared_port` (C) |
| `public_host` | Modus B: der eine Name, ohne Schema, Port und Pfad |
| `public_base_url`, `public_agent_url`, `public_mcp_url` | die Adressen in Modus A und C |
| `ui_port`, `agent_port`, `mcp_port` | die Ports in Modus A und B |
| `shared_tls_port` | der gemeinsame Port in Modus C, Vorgabe 443 |
| `agent_enabled`, `mcp_enabled` | die Schalter von Agent (Vorgabe an) und MCP |
| `trust_proxy_header` | LCM glaubt der Client-Adresse aus `X-Forwarded-For`, aber nur von `trusted_proxies` |
| `trusted_proxies` | Liste von IP-Adressen oder Netzen (CIDR); `0.0.0.0/0` und `::/0` lehnt LCM ab, ebenso eine leere Liste bei eingeschaltetem `trust_proxy_header` |
| `setup_step` | der Schritt des Einrichtungsassistenten (`addresses`, `services`, `access`, `certificate`, `onboarding`, `mail`, `retention`, `backup`, `subscription`, `intro`), leer = erledigt. Der `PATCH` darf zwischen `session` und `subscription` vor und zurück, von dort auch auf `intro` oder leer; aus `intro` nur auf leer. Er steht auch in `GET /api/v1/auth/me`. |

Nur in der Antwort stehen `address_locks` (je Feld wahr, wenn eine
Umgebungsvariable es festlegt), `agent_servers` (die Zahl der Server, die den
Agenten nutzen, für die Rückfrage vor dem Ausschalten) und `effective_urls` (die
Adressen, die LCM tatsächlich in Befehle schreibt, in Modus B abgeleitet). Ändert ein `PATCH` den Port der Oberfläche,
nennt die Antwort zusätzlich `ui_url`: die Adresse, zu der der Browser wechseln
soll. Konnte LCM die `config.json` nach dem Speichern nicht schreiben, steht dort
`config_sync_error` mit dem Grund. Die vollständigen Anfragen und Antworten stehen in der
OpenAPI-Spezifikation ([REST-API](/reference/rest-api/)).
