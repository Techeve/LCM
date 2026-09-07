# Was LCM auf dem Server ändert

Diese Seite ist zum Nachschlagen: Zu jeder Aktion, die LCM auf einem Server
ausführt, steht hier, **welche Dateien und Kommandos betroffen sind** und **wie
du es von Hand rückgängig machst** - falls LCM einmal nicht erreichbar ist, du
den Server aus der Verwaltung nimmst oder einfach nachvollziehen willst, was
passiert ist.

Drei Grundsätze gelten überall:

- **LCM schreibt in eigene Dateien**, nicht in deine. Wo es geht, wird ein
  Drop-in angelegt (`sshd_config.d`, `sudoers.d`, `apt.conf.d`) statt eine
  bestehende Datei umzuschreiben. Löschst du das Drop-in, ist der
  Ausgangszustand zurück.
- **Muss LCM doch eine vorhandene Datei ändern**, legt es vorher eine Sicherung
  daneben - meist mit der Endung `.lcm-bak`.
- **LCM-eigene Dateien tragen `lcm` im Namen.** Was so heißt, stammt von LCM;
  alles andere hat LCM nicht angefasst.

Alle Befehle unten laufen als `root` (oder mit `sudo` davor).

## SSH-Zugang

### SSH härten

Legt `/etc/ssh/sshd_config.d/60-lcm-hardening.conf` an mit
`PasswordAuthentication no`, `ChallengeResponseAuthentication no`,
`PubkeyAuthentication yes`, `PermitRootLogin prohibit-password`. Falls die
Hauptdatei die Drop-ins noch nicht einliest, ergänzt LCM die `Include`-Zeile.
Danach wird sshd neu geladen und **nachgemessen**: Meldet sshd weiterhin
Passwort-Anmeldung, rollt LCM selbst zurück.

Rückgängig - in der Oberfläche über *SSH gehärtet ✓ - aufheben*, von Hand:

```sh
rm -f /etc/ssh/sshd_config.d/60-lcm-hardening.conf
systemctl reload sshd
```

### Root-Login sperren und eigener SSH-Port

Beides steht in einem zweiten Drop-in: `/etc/ssh/sshd_config.d/10-lcm-ssh.conf`
mit `Port …` und/oder `PermitRootLogin no`. Vor jeder Änderung legt LCM
`10-lcm-ssh.conf.lcmbak` an und rollt zurück, wenn sshd die neue
Konfiguration ablehnt.

```sh
rm -f /etc/ssh/sshd_config.d/10-lcm-ssh.conf
systemctl reload sshd
```

**Achtung bei Socket-Aktivierung:** Läuft `ssh.socket` (statt `sshd.service`
allein), entscheidet die Socket-Unit über den Port - das sshd-Drop-in bliebe
wirkungslos. LCM legt dann zusätzlich
`/etc/systemd/system/ssh.socket.d/10-lcm-port.conf` an. Löschst du nur die
erste Datei, lauscht der Server weiter auf dem alten Port:

```sh
rm -f /etc/systemd/system/ssh.socket.d/10-lcm-port.conf
systemctl daemon-reload && systemctl restart ssh.socket
```

Ist die Firewall aktiv, hat LCM den neuen Port dort mit geöffnet - siehe
Abschnitt *Firewall*.

### Eingeschränkter Modus

Statt voller Rechte bekommt der Management-Benutzer eine Positivliste:
`/etc/sudoers.d/<service-user>` mit `Cmnd_Alias LCM_ALLOWED = …` und genau
diesen Kommandos ohne Passwort. Zusätzlich liegt der LCM-Helper auf dem
Server, über den die erlaubten privilegierten Schritte laufen.

```sh
rm -f /etc/sudoers.d/<service-user>
```

Danach hat der Benutzer gar keine Sonderrechte mehr - für die volle Verwaltung
in LCM den Modus wieder aufheben, statt die Datei nur zu löschen.

## Benutzer und Rechte

### Benutzer anlegen oder zuordnen

LCM legt das Konto an (`useradd -m`, auf BusyBox `adduser`), setzt die Shell,
hebt die Passwortsperre auf (`usermod -p '*'`) und schreibt die
öffentlichen Schlüssel in `~/.ssh/authorized_keys` - **zwischen zwei Markern**:

```
# >>> LCM managed keys >>>
…
# <<< LCM managed keys <<<
```

Nur dieser Block gehört LCM. Schlüssel, die du selbst eingetragen hast, bleiben
außerhalb und werden nie angefasst.

Rückgängig: den Block zwischen den Markern löschen, oder das Konto entfernen:

```sh
sed -i '/# >>> LCM managed keys >>>/,/# <<< LCM managed keys <<</d' /home/<name>/.ssh/authorized_keys
# oder ganz:
userdel -r <name>
rm -f /etc/sudoers.d/lcm-<name>
```

### Rechteprofile

Ein Profil ist eine Gruppe `lcm-prof-<slug>` plus die Sudo-Regeln in
`/etc/sudoers.d/lcm-prof-<slug>`. Ein Konto ist Mitglied in **genau einer**
Profilgruppe. Vor dem Übernehmen prüft LCM die Regeln mit `visudo -c` und
weist uneingeschränkte Regeln (`NOPASSWD: ALL`) ab.

```sh
gpasswd -d <name> lcm-prof-<slug>
rm -f /etc/sudoers.d/lcm-prof-<slug>
groupdel lcm-prof-<slug>
```

### Konto sperren oder deaktivieren

Sperren setzt das Passwort auf gesperrt (`usermod -L` bzw. `passwd -l`),
Deaktivieren setzt zusätzlich die Shell auf `nologin`. Beides ist umkehrbar:

```sh
usermod -U <name>
usermod -s /bin/bash <name>
```

## Paketquellen

### Paketquelle hinzufügen

Zwei Dateien: der Signaturschlüssel unter `/etc/apt/keyrings/<key>.asc` und die
Quelle unter `/etc/apt/sources.list.d/lcm-<key>.list`. Beide tragen den
Namensteil `lcm`.

```sh
rm -f /etc/apt/sources.list.d/lcm-<key>.list /etc/apt/keyrings/<key>.asc
apt-get update
```

### Auf HTTPS umstellen

LCM ersetzt in allen apt-Quellen `http://` durch `https://`. Vor der Änderung
wird jede betroffene Datei als `<datei>.lcm-bak` gesichert. Danach läuft
`apt-get update`: Schlägt es fehl, spielt LCM die Sicherungen sofort selbst
zurück. Gelingt es, wandern die Sicherungen nach
`/var/backups/lcm-apt-https/` - dort liegt der Zustand von vorher.

```sh
cp -a /var/backups/lcm-apt-https/etc/apt/. /etc/apt/
apt-get update
```

In der Oberfläche gibt es dafür *Zurückstellen*, solange die Sicherung liegt.

### apt-Cache eintragen

Ein Drop-in: `/etc/apt/apt.conf.d/02lcm-apt-cache` mit `Acquire::http::Proxy`
und `Acquire::https::Proxy`. Verweigert der Cache HTTPS-Tunnel, setzt LCM
HTTPS auf `DIRECT`, damit die Quellen erreichbar bleiben.

```sh
rm -f /etc/apt/apt.conf.d/02lcm-apt-cache
apt-get update
```

## Pakete

### Updates, Entfernen, Aufräumen

Diese Aktionen sind gewöhnliche Paketverwaltung - LCM hinterlässt dabei nichts
Eigenes. `Alle Updates` entspricht `apt-get upgrade`, `Aufräumen`
`apt-get autoremove`. Rückgängig geht nur, was die Paketverwaltung selbst
hergibt: eine ältere Version gezielt installieren.

### Alte Kernel entfernen

`apt-get -y purge` auf die ausgewählten Kernel-Pakete. Der laufende Kernel,
neuere und eine Rückfallebene bleiben immer stehen. Rückgängig durch
Neuinstallation des Pakets - die Daten sind weg, das Paket ist wiederbeschaffbar.

### Paket-Pins

Zwei Wirkungen, je nach Pin:

- **Nicht entfernen** merkt LCM sich nur intern und lässt das Paket beim
  Aufräumen aus. Auf dem Server ändert sich nichts.
- **Version einfrieren** setzt `apt-mark hold`. Beim Übernehmen hebt LCM
  zuerst alle bestehenden Holds auf und setzt dann die aus den Pins.

```sh
apt-mark showhold      # was ist eingefroren?
apt-mark unhold <paket>
```

## Netz und Zeit

### DNS

Mit systemd-resolved: `/etc/systemd/resolved.conf.d/lcm-dns.conf`. Ohne
resolved schreibt LCM `/etc/resolv.conf` direkt und sichert vorher nach
`/etc/resolv.conf.lcm-bak`. Nach dem Setzen prüft LCM eine Testdomain; scheitert
die Auflösung, rollt LCM selbst zurück.

```sh
rm -f /etc/systemd/resolved.conf.d/lcm-dns.conf && systemctl restart systemd-resolved
# ohne resolved:
mv -f /etc/resolv.conf.lcm-bak /etc/resolv.conf
```

### Zeitserver

Je nach vorhandenem Dienst: `/etc/chrony/chrony.conf` (Sicherung als
`chrony.conf.lcm-bak`), `/etc/systemd/timesyncd.conf.d/lcm-ntp.conf` oder
`/etc/ntp.conf` (Sicherung `ntp.conf.lcm-bak`). LCM wartet danach auf die
tatsächliche Synchronisierung, statt nur den Dienst zu starten.

```sh
mv -f /etc/chrony/chrony.conf.lcm-bak /etc/chrony/chrony.conf && systemctl restart chronyd
# oder:
rm -f /etc/systemd/timesyncd.conf.d/lcm-ntp.conf && systemctl restart systemd-timesyncd
```

### Zeitzone

`timedatectl set-timezone`, ohne systemd der Symlink `/etc/localtime` plus
`/etc/timezone`. LCM liest den Wert danach zurück und meldet einen Fehler, wenn
das System weiterhin etwas anderes sagt.

```sh
timedatectl set-timezone Europe/Berlin
```

## Firewall

LCM benutzt das vorhandene Werkzeug - `ufw`, `firewalld` oder `nftables` - und
öffnet die freigegebenen Ports dort. Bei nftables liegt die eigene Regeldatei
unter `/etc/nftables.d/lcm.nft`.

```sh
ufw status numbered && ufw delete <nummer>          # ufw
firewall-cmd --permanent --remove-port=<port>/tcp && firewall-cmd --reload
rm -f /etc/nftables.d/lcm.nft && systemctl reload nftables
```

## Sicherheitswerkzeuge

fail2ban und CrowdSec installiert LCM über die Paketverwaltung
(`apt-get install -y`, danach `systemctl enable --now`). Es sind gewöhnliche
Pakete - Entfernen geht wie bei jedem anderen:

```sh
systemctl disable --now fail2ban
apt-get purge -y fail2ban
```

## Server aus LCM entfernen

Beim Entfernen kannst du wählen, ob LCM den Server **bereinigen** soll. Ohne
Bereinigung verschwindet nur der Eintrag in LCM; auf dem Server bleibt alles
stehen. Mit Bereinigung entfernt LCM in dieser Reihenfolge:

1. die von LCM angelegten Benutzerkonten samt `/etc/sudoers.d/lcm-<name>`
2. `~/.ssh/authorized_keys` des Management-Benutzers - **zuerst**, damit der
   Zugang sicher entzogen ist, auch wenn ein späterer Schritt scheitert
3. `/etc/sudoers.d/<service-user>` und zuletzt das Konto selbst

**Nicht entfernt** werden die Änderungen aus den Abschnitten oben: Härtung,
Paketquellen, DNS, Zeitserver und Firewall-Regeln bleiben bestehen. Das ist
Absicht - sie betreffen den Betrieb des Servers, nicht den Zugang von LCM. Wenn
du auch die weghaben willst, geh die betreffenden Abschnitte durch, **bevor**
du den Server entfernst; danach steht dir die Oberfläche dafür nicht mehr zur
Verfügung.
