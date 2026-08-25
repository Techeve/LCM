---
sidebar:
  order: 13
title: Deep Scan
description: Tiefergehende Sicherheitsprüfung je Server - Kernel-Reboot-Lücke, Kernel-CVEs und Fehlkonfiguration.
---

Der **Deep Scan** ergänzt den routinemäßigen CVE-Scan um eine tiefere Prüfung, die
**auf dem Server** läuft. Er beantwortet drei Fragen, die der zentrale Trivy-Scan
allein nicht abdecken kann.

## Was der Deep Scan prüft

- **Kernel-/Neustart-Lücke** (via `needrestart`): Ist der **laufende** Kernel älter
  als der **installierte**? Dann sind bereits eingespielte Kernel-Sicherheitsfixes
  erst nach einem Neustart wirksam - der Server ist bis dahin weiter exponiert.
  Ebenso: welche **Dienste** laufen noch mit veralteten Bibliotheken und müssen neu
  gestartet werden. Trivy sieht nur die installierte Version, nicht die Laufzeit.
- **Kernel-CVEs**: die kernel-bezogenen Funde aus dem zentralen Trivy-Scan,
  hervorgehoben und mit der Reboot-Lücke verknüpft.
- **Fehlkonfiguration / Härtung** (via **Lynis**): ein CIS-orientiertes
  Härtungs-Audit mit **Härtungs-Index (0-100)** und Warnungen/Empfehlungen. Ist
  Lynis nicht installiert, greifen die **kuratierten LCM-Eigenprüfungen** (SSH-Config,
  sysctl-Härtung, Konten ohne Passwort, automatische Sicherheitsupdates …) mit
  geringerer Abdeckung.

:::note[Trivy ist das richtige Werkzeug für CVEs - aber nicht für alles]
Trivy prüft installierte Pakete (inkl. Kernel-Paket) backport-bewusst auf CVEs und
läuft zentral auf dem LCM-Host. Für die **Laufzeit-Lücke** (laufender vs.
installierter Kernel) und die **Host-Härtung** braucht es Werkzeuge auf dem Ziel -
`needrestart` und Lynis. Der Deep Scan ergänzt Trivy, er ersetzt ihn nicht.
:::

## Werkzeuge installieren

`needrestart` und `lynis` sind auf vielen Servern nicht vorinstalliert. LCM
installiert sie **nicht automatisch**: fehlt ein Werkzeug, läuft der Deep Scan
trotzdem und meldet den fehlenden Teil ehrlich als „nicht geprüft". Über den Knopf
**„Tools installieren"** (Server-Detail → Tab **Deep Scan**) lassen sich beide
Werkzeuge je Paketverwaltung nachrüsten (apt/dnf/zypper/pacman/apk; wo eine
Distribution ein Werkzeug nicht anbietet - z. B. needrestart auf Alpine - wird das
gemeldet).

## Ausführen

- **Einzelner Server**: Server-Detail → Tab **Deep Scan** → **„Deep Scan starten"**.
  Der Lauf kann durch Lynis ~30-60 s dauern und erscheint als Job in der Historie.
- **Ganze Gruppe**: Gruppen-Regel vom Typ **„Deep Scan"** (geplant oder manuell
  ausgelöst).

Der Deep Scan ist rein lesend und läuft daher auch im eingeschränkten Sudo-Modus.

## Ergebnis

Der Tab **Deep Scan** zeigt den Härtungs-Index, die kernel-bezogenen CVEs und die
**Berichte**.

### Berichte statt einer endlosen Liste

Jeder Lauf wird als eigener, **datierter Bericht** abgelegt und bleibt erhalten.
Die Liste zeigt je Lauf Datum, die Befundzahlen nach Schwere und - das ist der
Punkt - den Unterschied zum vorhergehenden Lauf:

| Kennzeichen | Bedeutung |
|---|---|
| **+n neu** | so viele Befunde standen im Lauf davor noch nicht |
| **−n behoben** | so viele sind seither verschwunden; beim Aufklappen stehen sie namentlich da |
| **aktuell** | der jüngste Lauf - er speist Ampel, Insights und Alarme |
| **Verwendete Werkzeuge** | ein Lauf ohne Lynis deckt deutlich weniger ab; das erklärt Sprünge in der Befundzahl |

Ein Klick auf einen Lauf öffnet seine Befunde, nach Kategorie gruppiert; neue
Befunde sind einzeln als **neu** markiert. Damit lässt sich beantworten, was
eine flache Liste nie beantworten konnte: *Habe ich seit dem letzten Mal etwas
erreicht?* Beim allerersten Lauf bleiben beide Kennzeichen leer - dort gibt es
nichts zu vergleichen.

Aufbewahrt werden die **jüngsten 40 Läufe je Server**; die Log-Bereinigung
räumt ältere samt ihrer Befunde ab.

Zusätzlich fließt das Ergebnis in Ampel und Alarme ein:

- eine **Kernel-Reboot-Lücke** setzt „Neustart erforderlich" (gelb),
- **Härtungs-/Konfigurationswarnungen** färben den Server gelb - reine **Lynis-
  Empfehlungen** bleiben bewusst informativ (kein „alles gelb"),
- die Alarmregel **„Deep Scan: Befunde"** meldet Warnungen/Kernel-Reboot aktiv.

## Wenn der Befund „Root-Login erlaubt" nicht verschwindet

Der Deep Scan liest den Root-Login mit `sshd -T` aus - also die **effektive**
Konfiguration, nicht den Inhalt einer bestimmten Datei. Meldet er den Zugang
als offen, obwohl in LCM „Root-Login deaktivieren" gesetzt ist, hat der Scan
recht und die Einstellung greift tatsächlich nicht.

Der Grund liegt in einer Eigenheit von sshd: Bei **mehrfacher Definition
gewinnt die ERSTE** Fundstelle - anders als bei fast jeder anderen
Konfiguration. Steht in `/etc/ssh/sshd_config` ein `PermitRootLogin yes`
*vor* dem `Include` der Drop-ins, bleibt LCMs Drop-in
(`10-lcm-ssh.conf`) wirkungslos, obwohl es fehlerfrei geschrieben und von
`sshd -t` abgenommen wurde. Viele Cloud- und Hoster-Images liefern genau diese
Zeile mit.

**LCM löst das inzwischen selbst.** Beim Sperren des Root-Logins prüft es die
Wirkung; greift sie nicht, legt es die vorrangigen Zeilen der Hauptdatei mit
einer Markierung still:

```
#LCM-STILLGELEGT# PermitRootLogin yes
```

Danach wird erneut geprüft - erst dieser zweite Anlauf entscheidet über Erfolg
oder Fehlermeldung. Beim **Freigeben** des Root-Logins nimmt LCM die
Stilllegung wieder zurück und stellt die Datei zeichengenau im Ursprungszustand
her; die Markierung ist genau dafür da.

Angefasst wird die Hauptdatei nur, wenn sie nachweislich im Weg steht: Eine
`PermitRootLogin`-Zeile *hinter* dem Include ist ohnehin wirkungslos und bleibt
unberührt. Ebenso bleiben bereits auskommentierte Zeilen und die Drop-ins
selbst unangetastet. Jede Änderung läuft mit Sicherung, `sshd -t` und Rollback
- lehnt sshd die geänderte Datei ab, ist der alte Stand sofort wieder da. Was
stillgelegt wurde, steht mit Zeilennummer im Job-Protokoll.
