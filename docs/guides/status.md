---
sidebar:
  order: 11
title: Status-Berechnung
description: Wie LCM den Ampel-Status eines Servers ermittelt - alle Faktoren, Schwellwerte und Sonderfälle im Detail.
---

Jeder Server trägt einen von vier Status: 🟢 **Sehr gut** (sattes Grün),
🟢 **OK** (helles Grün), 🟡 **Warnung** und 🔴 **Kritisch**. Diese Seite
erklärt exakt, wie sich der Status ergibt. Fährst du in der Serverliste mit der Maus
über den Status (in der Server-Detailansicht klickst du darauf), erscheinen
die konkreten Gründe („Insights") - bei Gelb/Rot die Probleme, bei **OK** die
Punkte, die noch zu „Sehr gut" fehlen.

## Die Prüf-Reihenfolge

Der Status wird bei jedem Aufruf frisch aus den zuletzt erfassten Daten
berechnet, in dieser Reihenfolge:

### 1. Erreichbarkeit → 🔴

Ist der Server **nicht erreichbar** (offline, Auth-/Host-Key-Fehler), ist er
sofort **kritisch** - außer für ihn ist *„Nichterreichbarkeit unkritisch"*
gesetzt (Server-Einstellungen). Dann behält er seinen zuletzt berechneten
Status und wird nur ausgegraut; erst nach Ablauf der **Kulanzfrist**
(Standard 28 Tage, einstellbar 1-365) wird er doch kritisch.

### 2. Rot-Kriterien → 🔴

- **Mindestens eine kritische CVE** (nach Gewichtung, siehe unten).
- **Betriebssystem außerhalb des Herstellersupports (EOL)** - oder weniger
  als **einen Monat** vor dem Support-Ende.
- **LCM ist handlungsunfähig:** Die Paketverwaltung oder die Verbindung
  schlägt fehl (siehe [Betriebsfähigkeit](#betriebsfähigkeit)) - eine belegte
  dpkg-Sperre erst ab dem zweiten Fehlschlag in Folge.
- **Paketstand veraltet (rot):** Die letzte erfolgreiche
  Paketaktualisierung ist älter als 7 Tage (Einstellungen → Allgemein).

### 3. Gelb-Kriterien → 🟡

Jeder der folgenden Punkte macht den Server zur **Warnung** (und erscheint
als eigener Insight):

| Kriterium | Schwelle |
| --- | --- |
| Hohe CVEs (gewichtet) | ≥ 1 |
| Überfällige Paket-Updates | ≥ 1 |
| Genutzte Docker-Images mit verfügbarem Update | ≥ 1 |
| Belegung des Root-Volumes `/` | ≥ 85 % |
| System fordert Neustart an (z. B. nach Kernel-Update) | ja |
| Regeln/Aktionen oder CVE-Scan schlagen fehl | ja |
| Belegte dpkg-Sperre beim ersten Fehlschlag | ja |
| Einzelne Paketquelle fehlerhaft | ja |
| Paketstand veraltet (letzte erfolgreiche Paketaktualisierung älter als 48 h, einstellbar) | ja |

### 4. „Sehr gut" oder „OK"

Liegt keines der obigen Signale vor, ist der Server grün. **Sehr gut**
erreicht er nur makellos, wenn **alle drei** Kriterien erfüllt sind:

1. **Null zählende CVEs** - keine einzige bekannte Lücke, auch keine
   niedrigen (Docker-CVEs nicht relevanter Container zählen nicht mit,
   siehe unten).
2. **SSH-Härtung aktiv** (nur Key-Login, kein Root-Passwort-Login).
3. **Firewall (ufw) aktiv** - Proxmox-Systeme bringen ihre eigene Firewall
   mit und gelten als abgedeckt.

Fehlt eines davon, bleibt der Server bei **OK** - und die Gründe nennen
genau, was noch fehlt.

## Betriebsfähigkeit

LCM merkt sich je Server für vier Bereiche, ob es dort noch handeln kann:
**Verbindung**, **Paketverwaltung** (Paketliste, Updates, Security-Updates,
Einzelpakete), **Regeln und Aktionen** und **CVE-Scan**. Die Karte
„Betriebsfähigkeit" in der Server-Übersicht zeigt je Bereich, seit wann er
fehlschlägt, wie oft in Folge, die Ursache und was zu tun ist.

- Ein Fehlschlag betrifft nur **seinen** Bereich. Scheitert ein Lauf schon an
  der Verbindung, zählt er zur Verbindung, nicht zur Paketverwaltung.
- **Nur ein Erfolg derselben Aktion heilt** einen Bereich. Ein erfolgreicher
  Gesundheitstest behebt also kein fehlgeschlagenes Update; ein volles Update
  heilt aber auch fehlgeschlagene Security-Updates.
- Ein vom Job-Watchdog abgebrochener Lauf zählt als Fehlschlag, ein von Hand
  abgebrochener nicht. Abgewiesene Läufe (Server gerade belegt) zählen nicht.
- „Paketliste aktualisieren" wertet die Ausgabe von `apt-get update` aus:
  Scheitern alle Quellen oder meldet apt einen allgemeinen Fehler, schlägt
  der Lauf fehl; scheitert nur eine von mehreren Quellen, gilt er als Erfolg
  mit gelbem Hinweis „Paketquelle fehlerhaft".
- Für ein unterbrochenes dpkg und kaputte Abhängigkeiten bietet die Karte
  einen **Reparatur-Knopf** (`dpkg --configure -a` bzw. `apt -f install`,
  bei zypper `zypper verify`). Danach führt **„Erneut versuchen"** die
  gescheiterte Aktion noch einmal aus - erst deren Erfolg setzt den Bereich
  zurück.
- Die Kachel **„LCM handlungsunfähig"** auf dem Dashboard führt zur
  Serverliste mit allen Servern, bei denen Paketverwaltung oder Verbindung
  ausgefallen sind oder der Paketstand kritisch veraltet ist. Der Alarm **„Aktion fehlgeschlagen"**
  meldet solche Wechsel sofort (siehe [Alarme](/guides/alerts/)).

## CVE-Gewichtung

Für Ampel und Alarme wird die rohe Trivy-Schwere kontextabhängig gewichtet;
die Sicherheits-Ansichten zeigen weiterhin die Roh-Bewertung:

- **Docker-CVEs zählen standardmäßig gar nicht.** Container sind isoliert,
  ihre Pakete von außen nicht direkt erreichbar, und für Image-Inhalte ist
  der Image-Anbieter verantwortlich. Nur Container, die im Reiter *Container*
  eines Servers in der Spalte *In der Ampel* **ausdrücklich eingeschaltet**
  sind, fließen ein - dann mit **voller Schwere**. Die Markierung hängt am Container-Namen und übersteht
  Image-Updates und Inventar-Scans.
- **CVEs exponierter Pakete eine Stufe höher** - Webserver, Reverse-Proxies,
  Mail-/DNS-Server, Datenbanken (Liste unter *Einstellungen → Sicherheit*)
  sowie automatisch erkannte Pakete, die auf von außen erreichbaren Ports
  lauschen.

:::tip[Wenn die Ampel hohe CVEs meldet, die unter Sicherheit fehlen]
Genau das ist die sichtbare Folge der Gewichtung: Ein *mittlerer* Fund an
einem exponierten Dienst zählt hier als **hoch**, steht in der
Sicherheitsübersicht aber weiterhin als **mittel** - dort ist die Roh-Bewertung
bewusst unverändert. Wer daraufhin nach hohen Funden sucht, findet keine und
hält die Bewertung für hängengeblieben; ein erneuter Scan ändert daran nichts,
weil beide Angaben stimmen.

Deshalb benennt die Statusbegründung die Pakete, deren Funde hochgestuft
wurden:

> Höher gewichtet, weil exponiert oder hoch eingestuft: nginx, openssh-server.
> Unter Sicherheit stehen diese Funde mit ihrer ursprünglichen, niedrigeren
> Schwere.

Damit ist die Differenz in einem Satz aufgelöst - und man sieht sofort, an
welchem Dienst die Ampel tatsächlich hängt.
:::

## Sonderfälle

- **Offline-tolerierte Server** (*„Nichterreichbarkeit unkritisch"*) behalten
  ihren letzten Status und erscheinen ausgegraut, bis die Kulanzfrist abläuft.
- **Demo-Server** werden nie kontaktiert; ihr Status stammt aus den
  Demo-Daten.
- **Proxmox-Systeme** erfüllen das Firewall-Kriterium automatisch
  (pve-firewall); die ufw-Verwaltung ist dort gesperrt.

Verwandte Seiten: [Server & Monitoring](/guides/monitoring/),
[Sicherheit & CVE-Scans](/guides/security-cve/),
[Docker-Monitoring](/guides/docker/).
