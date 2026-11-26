---
sidebar:
  order: 21
title: SSH-Anbindung
description: Server klassisch per SSH an LCM anbinden - die drei Zugangsvarianten (root, sudo, su), was LCM dabei einrichtet, der eingeschränkte Modus und die Fallstricke je Distribution.
---

Bei der klassischen Anbindung verbindet sich **LCM selbst per SSH** zum Server.
Auf dem Server läuft dafür kein eigenes Programm, nur der ohnehin vorhandene
SSH-Dienst. Diese Seite erklärt, wie du einen Server so aufnimmst, welche
Zugangsvarianten es gibt, was LCM dabei auf dem Server einrichtet und worauf du
je Distribution achten musst.

Ob SSH oder Agent der richtige Weg ist, steht unter
[Agent oder SSH?](/guides/remote/#agent-oder-ssh). Kurz: SSH passt, wenn LCM den
Server über das Netz erreicht. Liegt er hinter NAT oder soll kein SSH-Port offen
sein, nimm den Agent.

## So läuft die Aufnahme ab

Auf dem Dashboard wählst du **„+ Server hinzufügen“** und als Verbindungsart
**SSH**. Der Dialog hat drei Schritte:

1. **Verbindung.** Du trägst ein:
   - **Name** (Anzeigename in LCM),
   - **Hostname oder IP** (im Docker-Container von LCM ist `localhost` nicht
     erlaubt, es wäre der Container selbst - nimm die Netzadresse des Hosts),
   - **SSH-Port** (Vorgabe 22),
   - **Login-Benutzer** (Vorgabe `root`),
   - **Anmeldung:** **Passwort** oder **System-SSH-Schlüssel**,
   - den Schalter **Eingeschränkte Rechte (empfohlen)**, siehe
     [Eingeschränkter Modus](#eingeschränkter-modus).

   Benutzer und Port, die der Dialog vorbelegt, stellst du unter *Einstellungen →
   Allgemein* ein (Unterkarte **Vorgabe für neue Server**).

   Mit **„Weiter: Host-Schlüssel prüfen“** geht es weiter.
2. **Host-Schlüssel.** LCM liest den Schlüssel des Servers, noch ohne
   Zugangsdaten, und zeigt den **Fingerprint**. Vergleiche ihn auf dem Server (an
   der Konsole) und bestätige mit **„Stimmt überein – weiter“**:

   ```sh
   ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub
   ```

   Je nach Schlüsseltyp heißt die Datei `…_ecdsa_key.pub` oder `…_rsa_key.pub`;
   der Dialog nennt die passende.

   LCM merkt sich diesen Schlüssel und bricht jede spätere Verbindung ab, wenn er
   sich ändert (Schutz gegen Mitlesen). Wurde der Server neu aufgesetzt, erwartest
   du einen neuen Schlüssel - dafür gibt es **Neu verbinden**
   ([unten](#danach-neu-verbinden-schlüssel-erneuern-entfernen)).
3. **Aufnehmen.** LCM zeigt, was es auf dem Server tun wird (*Das macht LCM jetzt
   auf …*), und startet mit **„Server jetzt aufnehmen“**. Die vollständige Ausgabe
   steht danach im Job des Servers. Schlägt die Aufnahme fehl, steht das
   Protokoll in der Fehlermeldung.

Das **Login-Passwort** nutzt LCM nur für diese eine Einrichtung. Es wird
nicht gespeichert und läuft nie über die Kommandozeile, sondern über stdin, steht
also auch nicht im Protokoll. Danach meldet sich LCM nur noch mit seinem eigenen
Schlüssel an.

## Die drei Zugangsvarianten

Der **Login-Benutzer** ist der Zugang, mit dem LCM **einmal** hereinkommt. Er
braucht Root-Rechte, denn LCM legt auf dem Server ein eigenes Konto an (siehe
[Was LCM einrichtet](#was-lcm-auf-dem-server-einrichtet)). Es gibt drei
Wege dorthin. Du wählst keinen davon im Dialog aus: LCM probiert sie selbst
durch.

| Variante | Login-Benutzer | Wie wird er root? |
| --- | --- | --- |
| **1. Direkt als root** | `root` | Er ist es schon. |
| **2. Benutzer mit sudo** | z. B. `admin` | per `sudo`, mit seinem Passwort oder ohne (`NOPASSWD`) |
| **3. Benutzer, der zu root wechselt** | z. B. `admin` | per `su` oder `login`; geht nur in engen Fällen, siehe unten |

### Wie LCM root wird

Ist der Login-Benutzer nicht `root`, führt LCM der Reihe nach harmlose Proben aus
(sie starten nur `true`). **Die erste, die klappt, gilt.** Je Versuch wird das
Ergebnis im Aufnahme-Protokoll festgehalten.

| Nr. | Versuch | Probe | Gedacht für |
| --- | --- | --- | --- |
| 1 | Login-Benutzer ist `root` | keine Probe nötig | Variante 1 |
| 2 | sudo mit Login-Passwort | `sudo -S -p '' sh -c true`, Passwort über stdin (nur, wenn du eines eingetragen hast) | Variante 2 |
| 3 | sudo ohne Passwort | `sudo -n sh -c true` | Variante 2 mit `NOPASSWD` |
| 4 | su ohne Passwort | `su root -c true` mit leerer Eingabe | Variante 3: root ohne Passwort oder `pam_wheel` mit Vertrauen |
| 5 | su mit **Login-Passwort** | `su root -c true`, Login-Passwort über stdin | Variante 3: root hat **dasselbe** Passwort wie der Login-Benutzer |
| 6 | `login` ohne Passwort | `login root` | root hat kein Passwort, `su` ist aber gesperrt (z. B. per Gruppe) |
| 7 | `login` mit **Login-Passwort** | `login root`, Login-Passwort über stdin (nur, wenn du eines eingetragen hast) | Variante 3: root hat **dasselbe** Passwort wie der Login-Benutzer, `su` ist aber gesperrt (z. B. per Gruppe) |

Findet LCM keinen Weg, bricht die Aufnahme ab, bevor auf dem Server etwas
angelegt wurde. Die Meldung beginnt mit *keine Root-Rechte für die
Provisionierung* und nennt, was du ändern kannst.

### Variante 1: direkt als root

LCM meldet sich als `root` an und richtet alles ohne Umweg ein. Das ist der
einfachste Weg. Zwei Möglichkeiten, hereinzukommen:

- **Per Schlüssel (empfohlen).** Wähle **System-SSH-Schlüssel**. LCM hat beim
  ersten Start ein eigenes Schlüsselpaar erzeugt. Den **öffentlichen Teil** findest
  du unter *Einstellungen → Allgemein* (Karte **Onboarding**, Unterkarte
  **Onboarding-Schlüssel**, mit fertigen Befehlen für Debian/Ubuntu, die
  RHEL-Familie und andere Systeme). Trage ihn in `/root/.ssh/authorized_keys` des
  neuen Servers ein. Das geht mit den Vorgaben der meisten Distributionen
  (`PermitRootLogin prohibit-password`: root darf mit Schlüssel herein, nur nicht
  mit Passwort), ohne die SSH-Konfiguration zu ändern.
- **Per Passwort.** Wähle **Passwort** und trage das root-Passwort ein. Das setzt
  voraus, dass root sich per SSH mit Passwort anmelden darf. Die meisten
  Distributionen verbieten das ab Werk (siehe [Distributionen](#distributionen)).
  Wie du den Root-Login nur für die Aufnahme öffnest, steht im
  [Schnellstart](/getting-started/quickstart/) (Schritt 2, ausklappbarer Abschnitt).

Fehlt auf dem Server `sudo`, installiert LCM es nach (siehe unten).

### Variante 2: Benutzer mit sudo

Der Login-Benutzer darf per `sudo` root werden. Das ist auf Ubuntu und den
Cloud-Images der meisten Distributionen der Normalfall.

- **Mit Passwort:** Trage Benutzer und Passwort ein. LCM übergibt das Passwort
  an `sudo -S` über stdin.
- **Ohne Passwort (`NOPASSWD`):** Meist wählst du **System-SSH-Schlüssel**, mit
  dem Schlüssel in der `~/.ssh/authorized_keys` des Benutzers. Kommt der Benutzer
  ohne Passwort herein, kannst du das Passwortfeld auch leer lassen. LCM versucht
  dann `sudo -n`. Cloud-Images
  (Benutzer wie `ubuntu`, `debian`, `fedora`) bringen genau diese Kombination
  meist mit.

Bei **System-SSH-Schlüssel** gibt es kein Passwort, das LCM an `sudo` weitergeben
könnte. Der Benutzer braucht dann `NOPASSWD`, sonst scheitert die Aufnahme.

`sudo` muss für den Benutzer auch ohne Terminal laufen. Steht in der
sudoers-Konfiguration `Defaults requiretty` (ältere RHEL-Systeme), lehnt `sudo` den
Aufruf von LCM ab. Entferne die Zeile oder nimm sie für den Login-Benutzer aus.

### Variante 3: Benutzer, der per su oder login root wird

Hier kommt LCM mit einem normalen Benutzer herein und wechselt per `su` oder
`login` zu root. **LCM unterstützt das nur in engen Fällen:**

- **su ohne Passwort** (Versuch 4): root hat kein Passwort oder `pam_wheel`
  lässt den Benutzer ohne Passwort durch.
- **su mit dem Login-Passwort** (Versuch 5): LCM gibt `su` das Passwort, das du im
  Dialog für den **Login-Benutzer** eingetragen hast. `su` fragt aber nach dem
  Passwort von **root**. Es klappt also nur, wenn root **dasselbe Passwort** hat
  wie der Login-Benutzer.
- **login ohne Passwort** (Versuch 6): `su` ist gesperrt (z. B. per Gruppe), aber
  das Programm `login` hat eine eigene PAM-Richtlinie und lässt root herein, weil
  root kein Passwort hat.
- **login mit dem Login-Passwort** (Versuch 7): wie Versuch 5, nur über `login`.
  Das hilft, wenn `su` gesperrt ist, root aber **dasselbe Passwort** wie der
  Login-Benutzer hat.

**Ein eigenes Feld für ein abweichendes root-Passwort gibt es nicht.** Hat dein
Benutzer ein anderes Passwort als root (der übliche Fall, etwa auf einem Debian,
das mit gesetztem root-Passwort installiert wurde), scheitert Variante 3. So gehst
du dann vor:

1. **Besser: sudo einrichten** und Variante 2 nehmen. An der Konsole, als root:

   ```sh
   su -
   apt install sudo               # Debian; sonst der Paketmanager des Systems
   usermod -aG sudo benutzer      # Gruppe sudo (Debian, Ubuntu), auf RHEL: wheel
   ```

   Der Benutzer meldet sich danach neu an, damit die Gruppe gilt.
2. **Oder per Schlüssel als root herein** (Variante 1): System-SSH-Schlüssel in
   `/root/.ssh/authorized_keys` eintragen.
3. **Oder** root das Passwort des Login-Benutzers geben, nur für die Aufnahme, und
   danach wieder ändern. Das ist der unschönste Weg.

Weil die Probe `su` als Programm ohne Terminal aufruft und das Passwort über stdin
einspeist, hängt Versuch 5 davon ab, dass dein `su` das so annimmt (bei
util-linux der Fall). Versuch 7 speist das Passwort ebenso über stdin ein; es
steht nie in der Kommandozeile und nicht im Aufnahme-Protokoll. Von BusyBox-`su` (Alpine) ist das nicht belegt - nimm dort
root oder sudo.

### Anmeldung: Passwort oder System-SSH-Schlüssel

| Login-Benutzer | Passwort | System-SSH-Schlüssel |
| --- | --- | --- |
| `root` | geht, wenn root sich per SSH mit Passwort anmelden darf | geht; Schlüssel in `/root/.ssh/authorized_keys` |
| Benutzer mit `sudo` | geht, `sudo` bekommt das Passwort | geht nur mit `NOPASSWD` |
| Benutzer mit `su` oder `login` | nur, wenn root-Passwort = Login-Passwort | nur ohne Passwort (su oder login) |

Hat der Server die Passwort-Anmeldung abgeschaltet (zum Beispiel nach **SSH
härten**), meldet LCM *der Server bietet keine Passwort-Anmeldung an*. Dann
**System-SSH-Schlüssel** wählen.

## Was LCM auf dem Server einrichtet

Mit den Rechten des Login-Benutzers legt LCM einen **eigenen Dienstbenutzer**
an. Der Login-Benutzer wird danach nicht mehr gebraucht.

| Was | Wo | Inhalt |
| --- | --- | --- |
| **Konto** `lcm-svc` | `/home/lcm-svc`, Rechte 700 | Shell `/bin/bash`, ohne Bash `/bin/sh`. Legt das Konto mit `useradd` an, auf BusyBox-Systemen mit `adduser`. |
| **Schlüssel** | `/home/lcm-svc/.ssh/authorized_keys` | genau **ein** Schlüssel: ein **neues ed25519-Paar nur für diesen Server**. Der private Teil bleibt verschlüsselt in LCM. |
| **sudo-Regel** | `/etc/sudoers.d/lcm-svc`, Rechte 0440 | Voll-Modus: `lcm-svc ALL=(ALL) NOPASSWD:ALL`. Eingeschränkter Modus: nur eine Whitelist. |
| **sudo** selbst | Paket | wird nur nachinstalliert, wenn es fehlt (siehe unten) |

Zu den Einzelheiten:

- **Gesperrtes Passwortfeld.** Ein frisches Konto ist per Passwort gesperrt
  (`!`). Manche sshd-Konfigurationen (`UsePAM no`, etwa auf openSUSE) lehnen dann
  auch den Schlüssel ab. LCM setzt darum ein unbrauchbares Passwort (`*`): Der
  Schlüssel geht, eine Passwort-Anmeldung bleibt unmöglich. Ein schon gesetztes
  Passwort lässt LCM unverändert.
- **sudo nachinstallieren.** Fehlt `sudo`, installiert LCM es mit der
  Paketverwaltung des Systems (apt, dnf, yum, zypper, pacman oder apk). Das ist
  vor allem Debian, Proxmox (VE, PBS, PDM), Arch und Alpine. Das gilt für
  Variante 1 und 3. Bei Variante 2 ist `sudo` ja schon da. Auf Proxmox liegt
  `/etc/sudoers.d/` schon, ohne dass das Programm installiert ist; LCM prüft
  deshalb das Programm, nicht das Verzeichnis. **LCM entfernt `sudo` nie wieder**,
  auch nicht beim Rückbau einer gescheiterten Aufnahme: andere Anmeldungen
  könnten es inzwischen brauchen. Was installiert wurde, steht im Protokoll der
  Aufnahme.
- **Funktionstest.** Im Voll-Modus meldet sich LCM mit dem neuen Schlüssel an
  und führt `sudo -n id -u` aus. Kommt nicht `0` zurück, ist die Aufnahme
  gescheitert. Typische Ursache: ein späterer Eintrag in `/etc/sudoers.d/`, der
  `lcm-svc` wieder ausschließt, oder eine `sudoers`-Datei, die das Verzeichnis nicht
  einbindet (fehlendes `#includedir /etc/sudoers.d`).
- **Paketverwaltung.** LCM bedient apt, dnf/yum, zypper, pacman und apk. Auf
  Systemen mit etwas anderem nimmt es den Server nicht auf, statt einen Server
  ohne Paketbestand als gesund anzuzeigen.
- **Rücknahme bei Fehlern.** Scheitert die Aufnahme nach dem Anlegen, entfernt
  LCM Konto, sudoers-Datei und Helper wieder. Es bleibt nichts liegen.

Danach liest LCM das System ein (Pakete, Quellen, Benutzer, Container, Speicher)
und nimmt den Server in die **System-Gruppe** auf, damit Zustandsprüfung und
Abgleich sofort laufen. Im Betrieb führt LCM Aufträge als `lcm-svc` aus, im
Voll-Modus als `sudo sh -c '…'`.

### Danach: neu verbinden, Schlüssel erneuern, entfernen

- **Schlüssel erneuern** (Serveraktion): LCM erzeugt ein neues Paar, hinterlegt es,
  prüft die Anmeldung damit und entzieht dann den alten.
- **Neu verbinden:** für einen Server, der neu aufgesetzt wurde oder dessen
  Zugangsdaten sich geändert haben. Der Dialog ist derselbe wie bei der
  Aufnahme. Gruppen, Benutzer-Zuordnungen und Job-Historie bleiben. Auf einem
  Server, den LCM gerade normal erreicht, lehnt LCM den Neu-Verbinden-Dialog ab;
  die Ausnahme ist der eingeschränkte Modus (siehe unten).
- **Entfernen:** Mit dem Schalter **LCM-Zugang auf dem Server auch löschen** räumt
  LCM beim Entfernen `authorized_keys`, sudoers-Datei und Konto auf dem Server weg.
  Ohne den Schalter verschwindet der Server nur aus LCM, der Zugang bleibt.
- **Root-Login sperren** (Serveraktion): Hast du root nur für die Aufnahme
  geöffnet, schließt du ihn damit wieder. LCM selbst ist nicht betroffen, es
  meldet sich als `lcm-svc` an.

Weitere Hinweise zur SSH-Härtung stehen unter
[Sicherheits-Tools](/guides/security-tools/) und im
[Sicherheitsmodell](/reference/security-model/).

## Eingeschränkter Modus

Im Voll-Modus bekommt `lcm-svc` `sudo` für alles. Im **eingeschränkten Modus**
bekommt er stattdessen eine feste **Whitelist** und keine Root-Shell.

### Wann er sinnvoll ist und wann nicht

Für die meisten Server ist der eingeschränkte Modus die richtige Wahl, deshalb
empfehlen wir ihn. Ein Server, den LCM aktuell hält, überwacht und absichert,
braucht für LCM keine Root-Shell. Der Voll-Modus lohnt sich dort, wo LCM mehr
tun soll als pflegen, oder wo eine Einschränkung kaum etwas gewinnt.

| Server | Modus | Warum |
| --- | --- | --- |
| Web-, Datei- und Datenbankserver mit Updates, Neustart bei Bedarf und Firewall per Regel | eingeschränkt | Mehr als Paketverwaltung, Firewall und Neustart braucht LCM dort nicht. |
| Server mit sensiblen Daten oder Prüfpflichten | eingeschränkt | Was LCM darf, ist festgelegt und für Prüfer nachvollziehbar. |
| Server mit eigenen Wartungsskripten: Datenbank-Dump vor dem Update, Deployment, Dienst neu laden | voll | Skripte und eigene Aktionen brauchen eine Root-Shell. |
| Docker-Host mit Compose-Projekten, die LCM per Regel aktualisiert | eingeschränkt | Compose-Updates laufen über `docker` auf der Whitelist. |
| Docker-Host mit einzelnen Containern ohne Compose, die LCM per Regel aktualisieren soll | voll | Die Regel *Container aktualisieren* braucht den Docker-Socket direkt. |
| Test-, Entwicklungs- und Laborserver, auf denen ohnehin viele Personen root haben | voll | Die Einschränkung bringt dort wenig Sicherheit, kostet aber Funktionen. |
| Server, auf denen LCM die SSH-2FA einrichten soll | voll | Die Einrichtung braucht eine Root-Shell. |
| Distribution, auf der der Modus noch nicht getestet ist | voll, bis der Test vorliegt | Siehe [Unterstützte Systeme](/reference/supported-systems/). |

Eine Gruppe darf beide Arten mischen. Eine Regel, die ein eingeschränkter Server
nicht ausführen darf, überspringt LCM bei ihm mit Hinweis im Job, statt sie
scheitern zu lassen. Der Zeitplan bleibt dadurch grün.

**Was geht weiter:** Paketverwaltung (apt, dnf, yum, zypper, pacman, apk, snap),
Docker (Scans, Compose-Updates, Pull, Aufräumen und ungenutzte Images), ufw, die Prüfwerkzeuge des Deep Scan und der **LCM-Helper**. Der Helper ist
ein von LCM installiertes, streng prüfendes Programm für alles, was root braucht und
nicht auf der Whitelist steht: Repositories, APT-Cache, SSH-Konfiguration,
Benutzer-Sync, firewalld und nftables, fail2ban, CrowdSec, Neustart.

**Was gesperrt ist:** eigene Skripte, eigene Aktionen und SSH-2FA. Sie brauchen eine
Root-Shell. Dazu die Docker-Regel *Container aktualisieren*: Sie erstellt Container
direkt über den Docker-Socket neu, den der eingeschränkte Benutzer nicht erreicht.

**Was er nicht leistet:** Er schützt nicht vor jemandem, der den Schlüssel von
`lcm-svc` stiehlt. Paketverwaltung und Docker führen von Haus aus Code als root aus
und `sudo` kann ihre Argumente nicht zuverlässig filtern. Der Modus verkleinert
die Angriffsfläche gegen Bedienfehler und macht nachvollziehbar, was LCM darf.
Einzelheiten: [Sicherheitsmodell](/reference/security-model/#eingeschränkter-modus-des-management-benutzers).

**So schaltest du ihn ein:** im Dialog der Schalter **Eingeschränkte Rechte
(empfohlen)** - er steht beim Öffnen auf *Aus* -, oder später über die
Serveraktion **Rechte einschränken**. Nach dem Einrichten prüft LCM als
`lcm-svc`, ob Helper und Paketverwaltung wirklich erreichbar sind. Fällt die Probe
durch, stellt LCM den Voll-Modus wieder her und meldet den Fehler. Du bekommst
keinen Server, dessen Kernfunktionen tot sind.

**Der Weg zurück:** Die Umschaltung per Klick ist ein Einweg-Schritt, denn
`lcm-svc` hat selbst nicht mehr die Rechte, sich Root zu holen. **Volle Rechte
wiederherstellen** zeigt dir die Befehle, die du auf dem Server selbst ausführst;
danach machst du **Neu verbinden** ohne den Schalter. Auch **Neu verbinden** mit
dem Login-Passwort allein führt zurück.

Welche Distributionen den Modus tragen, steht in
[Unterstützte Systeme](/reference/supported-systems/) (Spalte **SSHE**). Die
Tabelle hält dort fest, was getestet ist. Paketquellen verwaltet LCM im
eingeschränkten Modus nur auf apt-Systemen. LCM gleicht distributionsspezifische
Abweichungen selbst aus: den `secure_path` von sudo (RHEL 10 und Klone),
`targetpw` (openSUSE), `pacman-key` (Arch), den Pfad von `apk` und das fehlende
`/usr/local/sbin` (Alpine).

## SSH auf dem Zielserver vorbereiten

Vor der Aufnahme muss ein SSH-Dienst laufen und LCM erreichen können.

**1. Ist ein SSH-Server installiert und gestartet?**

| System | Dienstname | Prüfen |
| --- | --- | --- |
| Debian, Ubuntu | `ssh` | `systemctl status ssh` |
| RHEL-Familie, Fedora, openSUSE, Arch | `sshd` | `systemctl status sshd` |
| Alpine (OpenRC) | `sshd` | `rc-service sshd status` |

Ist kein SSH-Server installiert (etwa auf Linux Mint, Arch oder Alpine), holst du
ihn mit der Paketverwaltung nach (`openssh-server` auf Debian-Systemen, sonst
`openssh`) und startest den Dienst.

**2. Erreicht LCM den Port?** Die Firewall des Servers muss den SSH-Port für die
Adresse des LCM-Hosts offen haben.

**3. Was gilt wirklich?** `sshd` nimmt je Option den **zuerst** gefundenen Wert.
Zuerst kommen die Dateien aus `/etc/ssh/sshd_config.d/` (alphabetisch), wenn die
Hauptdatei sie oben einbindet, dann der Rest der Hauptdatei. Ein Drop-in mit
dem Namen `00-…` gewinnt gegen `50-…`. Prüfe nicht die Dateien, sondern die
**wirksame** Konfiguration:

```sh
sudo mkdir -p /run/sshd
sudo sshd -T | grep -iE 'permitrootlogin|passwordauthentication|pubkeyauthentication'
```

`mkdir -p /run/sshd` ist nötig, wenn `sshd` per Socket gestartet wird und das
Verzeichnis nur während einer Verbindung existiert. Ob die Hauptdatei die
Drop-ins überhaupt einbindet, siehst du hier:

```sh
grep -i '^Include' /etc/ssh/sshd_config
```

**4. Nach jeder Änderung** erst prüfen, dann neu starten:

```sh
sudo sshd -t
sudo systemctl restart ssh       # Debian, Ubuntu
sudo systemctl restart sshd      # RHEL-Familie, Fedora, openSUSE, Arch
sudo rc-service sshd restart     # Alpine
```

Auf neueren Debian- und Ubuntu-Versionen nimmt `ssh.socket` die Verbindungen an.
Ein **Neustart** des Dienstes (`restart`, nicht `reload`) stellt sicher, dass die neue
Konfiguration gilt.


## Distributionen

Die Spalten zeigen die **Ausgangslage mit den Vorgaben der Distribution**. Sie sind
das, was du typischerweise antriffst. Dein Server kann abweichen
(Installer-Auswahl, Cloud-Image, Hostinganbieter).

| Distribution | 1: root | 2: Benutzer mit sudo | 3: Benutzer mit su |
| --- | --- | --- | --- |
| **Ubuntu** | nur per Schlüssel (root gesperrt, kein Passwort) | Regelfall (Gruppe `sudo`) | geht nicht (root ohne Passwort) |
| **Debian** | nur per Schlüssel (Passwort für root per SSH gesperrt) | `sudo` oft nicht installiert, erst einrichten | nur wenn root- und Login-Passwort gleich sind |
| **RHEL, Alma, Rocky, CentOS Stream, Fedora** | per Schlüssel; Passwort in aktuellen Versionen gesperrt | Regelfall (Gruppe `wheel`) | nur bei gleichem Passwort |
| **openSUSE** | sshd prüfen (Drop-ins, Leap 16) | `sudo` verlangt das Passwort von root (`targetpw`) | nur bei gleichem Passwort |
| **Arch Linux** | erst `openssh` einrichten, dann per Schlüssel | `sudo` fehlt zunächst | nur bei gleichem Passwort |
| **Alpine** | erst `openssh` einrichten, dann per Schlüssel | `sudo` fehlt (`doas` kennt LCM nicht) | unsicher (BusyBox-`su`) |
| **Proxmox VE, PBS** | der normale Weg (Konto root) | `sudo` fehlt, LCM installiert es | nur bei gleichem Passwort |

**Getestet** ist die SSH-Anbindung laut
[Unterstützte Systeme](/reference/supported-systems/) auf **Debian 12 und 13 sowie
Ubuntu 22.04, 24.04 und 26.04** (Aufnahme, Service-Benutzer, eingeschränkter
Modus). Für alle anderen Distributionen ist der Code vorbereitet, ein Testlauf
steht aber aus. Die Hinweise unten für sie stammen aus der Vorgabe der jeweiligen
Distribution und aus dem Verhalten von LCM. Sie sind **nicht** durch Testläufe mit
LCM belegt.

### Ubuntu (22.04, 24.04, 26.04) - getestet

**Ausgangslage:** root hat kein Passwort. Der erste Benutzer ist in der Gruppe
`sudo` und braucht sein Passwort für `sudo`. `sshd` läuft mit
`PermitRootLogin prohibit-password`.

**Empfehlung:** Variante 2. Benutzer und Passwort eintragen, fertig. Auf
Cloud-Images (Benutzer `ubuntu`, `sudo` ohne Passwort, Anmeldung nur per
Schlüssel) wählst du **System-SSH-Schlüssel** mit dem Schlüssel im Benutzerkonto.

**Fallstricke:**

- Wer Variante 1 mit Passwort will, muss erst ein root-Passwort vergeben
  (`sudo passwd root`) **und** `PermitRootLogin yes` setzen. Besser: root per
  Schlüssel.
- Auf Cloud-Images steht vor dem Schlüssel von root oft ein `command=`-Eintrag,
  der zur Anmeldung als `ubuntu` auffordert. Melde dich als dieser Benutzer an.
- `50-cloud-init.conf` in `sshd_config.d` kann die Passwort-Anmeldung
  ein- oder ausschalten. Ein eigenes Drop-in mit `00-` davor gewinnt.
- Der Dienst heißt `ssh`. Neuere Versionen nutzen `ssh.socket`; nach einer Änderung
  `systemctl restart ssh`.
- Ubuntu 26.04 bringt `sudo-rs` statt `sudo` mit. LCM berücksichtigt das (es nimmt
  für diesen Fall `!requiretty` aus der Whitelist-Datei).

### Debian (12, 13) - getestet

**Ausgangslage:** Der Installer fragt nach einem root-Passwort. Setzt du eines,
bekommt der erste Benutzer **kein** `sudo`, und `sudo` ist nicht einmal installiert.
Lässt du das Feld leer, wird der Benutzer sudo-fähig und root gesperrt. `sshd`
erlaubt root nur per Schlüssel.

**Empfehlung:**

- Mit `sudo` eingerichtet: Variante 2.
- Ohne `sudo`: root per Schlüssel (Variante 1) **oder** `sudo` an der Konsole
  nachrüsten (Befehle oben bei [Variante 3](#variante-3-benutzer-der-per-su-oder-login-root-wird)).
  Variante 3 funktioniert nur bei gleichem Passwort.

**Fallstricke:**

- Fehlt `sudo`, installiert LCM es selbst, **sobald es root hat** (Variante 1 oder 3).
  Als Benutzer ohne `sudo` kommst du nicht in Variante 2.
- Damit `useradd` gefunden wird, setzt LCM beim Weg über `su` den Suchpfad selbst
  (ein `su` ohne `-` hat auf Debian kein `/usr/sbin` im Pfad).
- Debian 13 nutzt `ssh.socket`. Nach einer Änderung `systemctl restart ssh`.
- Der Dienst heißt `ssh`.

### RHEL, AlmaLinux, Rocky Linux, CentOS Stream, Fedora - nicht getestet

**Ausgangslage:** `sudo` ist installiert. Der Administrator liegt in der Gruppe
`wheel` und braucht sein Passwort. In aktuellen Versionen steht
`PermitRootLogin prohibit-password`. Der Dienst heißt `sshd`, Drop-ins liegen in
`/etc/ssh/sshd_config.d/`. Cloud-Images haben meist einen Benutzer mit `sudo` ohne
Passwort (z. B. `ec2-user`, `almalinux`, `rocky`, `fedora`).

**Empfehlung:** Variante 2, oder root per Schlüssel.

**Fallstricke:**

- **SELinux.** Hast du den Schlüssel für den Login-Benutzer von Hand eingetragen,
  setze danach den Kontext: `restorecon -R ~/.ssh`. Sonst lehnt `sshd` die Datei
  ab. Die Anleitung unter *Onboarding-Schlüssel* enthält den Befehl.
- Ältere Systeme mit `Defaults requiretty` verweigern `sudo` ohne Terminal.
- Im eingeschränkten Modus setzt LCM `secure_path` selbst. RHEL 10 und Klone lassen
  `/usr/local/sbin` weg, wo der Helper liegt.
- Ob LCM `lcm-svc` unter erzwungenem SELinux ohne weiteres anmelden kann, ist
  **nicht getestet**. Meldet die Aufnahme einen fehlgeschlagenen Schlüssel-Login,
  prüfe `/var/log/audit/audit.log`.

### openSUSE (Leap 16, Tumbleweed) - nicht getestet

**Ausgangslage:** Der Installer vergibt ein root-Passwort. `sudo` ist meist
vorhanden, verlangt aber standardmäßig das **Passwort von root**
(`Defaults targetpw`), nicht das des Benutzers.

**Empfehlung:** root (Variante 1). Bei Variante 2 und 3 passt das Passwort, das
LCM weitergibt, nur dann, wenn Benutzer- und root-Passwort gleich sind. Das folgt aus
dem Verhalten von `targetpw` und dem Ablauf der Proben, ist aber nicht mit einem
Testlauf belegt.

**Fallstricke:**

- **Leap 16 liefert die Hauptdatei unter `/usr/etc/ssh/sshd_config`** und bindet
  `sshd_config.d` nicht ein. Ein Drop-in wie
  `00-root-onboarding.conf` bleibt dann wirkungslos. Prüfe mit `sshd -T`. Fehlt
  `/etc/ssh/sshd_config`, kopiere die Datei aus `/usr/etc/ssh/` dorthin und setze die
  Zeile in der Kopie. Für sein eigenes Härten ergänzt LCM die `Include`-Zeile selbst
  ganz oben in der Datei.
- `UsePAM no`: Ein Konto mit gesperrtem Passwort wird auch mit Schlüssel abgewiesen.
  LCM entsperrt `lcm-svc` darum selbst.
- Im eingeschränkten Modus schaltet LCM `targetpw` für `lcm-svc` ab (die Whitelist
  begrenzt, nicht eine Passwortabfrage).
- Dienst: `sshd`.

### Arch Linux - nicht getestet

**Ausgangslage:** Eine Minimalinstallation. `openssh` ist nicht installiert, der
Dienst nicht aktiv, `sudo` fehlt, die `wheel`-Regel in `sudoers` ist meist
auskommentiert.

**Empfehlung:** `pacman -S openssh`, `systemctl enable --now sshd` und root per
Schlüssel (Variante 1). LCM installiert `sudo` dann selbst.

**Fallstricke:**

- LCM installiert `sudo` mit `pacman -Sy --noconfirm sudo`. Das ist **kein
  vollständiges Update**, und Arch unterstützt solche Teil-Updates nicht. Installiere
  `sudo` lieber vorher selbst: `pacman -Syu sudo`.
- Für Variante 2 braucht der Benutzer einen Eintrag: `visudo`, dann die
  `%wheel`-Zeile einkommentieren.
- Im eingeschränkten Modus nimmt LCM `pacman` **und** `pacman-key` in die
  Whitelist, weil die Einrichtung von Repositories Signaturschlüssel importiert.

### Alpine (3.22 bis 3.24) - nicht getestet

**Ausgangslage:** BusyBox und OpenRC. Der SSH-Dienst gehört nicht zwingend zur
Installation (`apk add openssh`, `rc-update add sshd`, `rc-service sshd start`).
`sudo` fehlt. Je nach Einrichtung gibt es `doas`.

**Empfehlung:** root per Schlüssel (Variante 1). LCM installiert `sudo` per
`apk add --no-cache sudo` und nutzt es danach.

**Fallstricke:**

- **`doas` kennt LCM nicht.** Weder die Proben noch der Betrieb rufen es auf. Auf
  einem Benutzer mit `doas` und ohne `sudo` scheitert Variante 2.
- **`su` ist von BusyBox**, nicht von util-linux. Ob Versuch 5 hier das Passwort über
  stdin annimmt, ist nicht belegt. Nimm root oder `sudo`.
- Das Anlegen von `lcm-svc` erledigt LCM mit `adduser` und nimmt `/bin/sh`, wenn
  `bash` fehlt. Ohne diesen Rückfall würde `sshd` den Schlüssel-Login ablehnen.
- Den Dienst lädt LCM mit `rc-service sshd reload`. `systemctl` gibt es nicht.
- `/usr/local/sbin` fehlt ab Werk. Im eingeschränkten Modus legt LCM das
  Verzeichnis für den Helper an.

### Proxmox VE, Backup Server, Datacenter Manager - nicht getestet

**Ausgangslage:** Debian-Basis, mit root als normalem Konto. `sudo` fehlt, obwohl
`/etc/sudoers.d/` existiert.

**Empfehlung:** root (Variante 1). LCM installiert `sudo` nach. Bei einem System
ohne Subscription meldet `apt-get update` für das Enterprise-Repository einen
Fehler (401). LCM wertet das nicht als Abbruch, installiert aber aus den vorhandenen
Paketlisten.

**Fallstricke:** LCM sperrt dort Aktionen, die mit Proxmox kollidieren:
Repositories hinzufügen, die ufw-Firewall und den Linux-Benutzer-Sync.
Proxmox verwaltet das selbst. Der CVE-Scan deckt die Debian-Pakete ab, nicht
`pve-*` und `proxmox-*`.

## Wenn die Aufnahme scheitert

Das Protokoll aller Schritte steht in der Fehlermeldung (*Provisionierungs-Protokoll*).
Die häufigsten Meldungen:

| Meldung beginnt mit | Ursache | Abhilfe |
| --- | --- | --- |
| *Anmeldung abgelehnt - Benutzername oder Passwort stimmt nicht* | Der Server bietet die Passwort-Anmeldung an, hat die Eingaben aber abgelehnt. | Benutzer und Passwort prüfen. Bei `root` darf der Server sich nicht per Passwort anmelden lassen (`PermitRootLogin`), siehe oben. |
| *der Server bietet keine Passwort-Anmeldung an* | `PasswordAuthentication no`, meist nach SSH-Härtung. | **System-SSH-Schlüssel** wählen und den öffentlichen Teil beim Login-Benutzer hinterlegen. |
| *keine Root-Rechte für die Provisionierung* | Keiner der sieben Versuche hat root ergeben. | Variante 2 einrichten (sudo), root per Schlüssel nehmen, oder bei Variante 3 gleiche Passwörter. Siehe [Variante 3](#variante-3-benutzer-der-per-su-oder-login-root-wird). |
| *sudo ist auf … nicht installiert und ließ sich nicht nachinstallieren* | Die Paketverwaltung konnte `sudo` nicht holen (Quellen, Netz). | Von Hand installieren (`apt-get install sudo`, entsprechend je System) und erneut aufnehmen. |
| *der Service-User „lcm-svc“ erreicht … keine Root-Rechte* | Die sudoers-Datei von LCM greift nicht. | `visudo` prüfen: schließt ein späterer Eintrag in `/etc/sudoers.d/` `lcm-svc` aus, fehlt `#includedir /etc/sudoers.d`? |
| *SSH-Host-Key stimmt nicht mit dem gespeicherten Fingerprint überein* | Der Server hat einen anderen Schlüssel als bei der Aufnahme. | Wurde der Server neu aufgesetzt: **Neu verbinden**. Sonst nicht weitermachen, es könnte jemand die Verbindung abfangen. |
| *die Paketverwaltung dieses Systems wird nicht unterstützt* | Keine der Paketverwaltungen apt, dnf/yum, zypper, pacman, apk gefunden. | Der Server lässt sich per SSH nicht aufnehmen. |
| *kein System-Onboarding-SSH-Key verfügbar* | **System-SSH-Schlüssel** gewählt, LCM hat keinen. | Unter *Einstellungen → Allgemein* nachsehen. |

## Siehe auch

- [Schnellstart](/getting-started/quickstart/): erste Aufnahme Schritt für Schritt
- [LCM Remote (lcm-agent)](/guides/remote/): Server ohne eingehenden SSH-Zugang
- [Unterstützte Systeme](/reference/supported-systems/): was getestet ist
- [Linux-Benutzer](/guides/linux-users/): Konten und Schlüssel auf den Servern verwalten
- [Sicherheitsmodell](/reference/security-model/): was der eingeschränkte Modus leistet
