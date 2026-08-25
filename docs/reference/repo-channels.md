---
sidebar:
  order: 27
title: Paketkanäle (Community, Beta & Enterprise)
description: Drei apt-Kanäle aus einer aptly-Instanz - Community offen und rollend, Beta offen mit Vorabversionen, Enterprise abgehangen und per Subscription-Key geschützt.
---

LCM wird über ein eigenes apt-Repository ausgeliefert. Dieses Repository
bedient **drei Kanäle**: einen offenen Community-Kanal, einen ebenfalls offenen
Beta-Kanal mit Vorabversionen und einen Enterprise-Kanal mit Subscription.
Diese Seite beschreibt den Aufbau auf dem Repository-Server.

:::note[Für wen ist diese Seite?]
Für den Betrieb des Repository-Servers. Wer LCM nur **installieren** möchte,
findet den Weg unter [Installation](/getting-started/installation/).
:::

## Das Modell

Alle Kanäle liefern **dieselben Pakete**, gebaut aus demselben Commit,
signiert mit demselben Schlüssel. Der einzige Unterschied ist der Zeitpunkt:

| | Beta | Community | Enterprise |
|---|---|---|---|
| apt-Zeile | `deb … repo.techeve.de beta main` | `deb … repo.techeve.de stable main` | `deb … repo.techeve.de/enterprise stable main` |
| Zugang | offen | offen | Subscription-Key |
| Funktions-Updates | Vorabversionen vor dem Release | sofort mit jedem Release | erst nach Bewährung im freien Kanal |
| Sicherheitsupdates | sofort | sofort | **ebenfalls sofort** |
| Support | Community | Community | vertraglich |

Das ist bewusst kein Feature-Split: Es gibt keine Funktion, die im freien Kanal
fehlt. Verkauft wird **Stabilität und Ansprechbarkeit**, nicht ein beschnittenes
Produkt. Der freie Kanal ist dadurch gleichzeitig die Erprobungsstufe für den
Enterprise-Kanal - und der Beta-Kanal die Stufe davor.

:::caution[Sicherheitsupdates nie verzögern]
Ein Sicherheitsrelease wird sofort in alle Kanäle gegeben. Zahlende Kunden
länger verwundbar zu lassen als freie Nutzer wäre weder vertretbar noch
vermittelbar.
:::

## Aufbau auf dem Server

Es ist **kein zweiter Server nötig**. Eine aptly-Instanz kann mehrere
Publish-Punkte parallel bedienen:

```
repo.techeve.de/
├── dists/stable/…          Community - von der CI bei jedem Release publiziert
├── dists/beta/…            Beta - von der CI aus dem beta-Branch publiziert
├── pool/…                  Pakete beider offener Kanäle (gemeinsamer Pool)
├── enterprise/
│   ├── dists/stable/…      Enterprise - ein freigegebener Snapshot
│   └── pool/…              eigener Paket-Pool
├── repo-key.gpg            Signaturschlüssel (öffentlich)
└── setup.sh                Einrichtungsskript für den Community-Kanal
```

Der Community-Kanal bleibt dabei **exakt dort, wo er ist**. Bestehende
Installationen tragen `deb … repo.techeve.de stable main` in ihren Quellen;
jede Änderung an diesem Pfad würde sie beim nächsten `apt update` brechen.

### Warum ein Pfad-Prefix und keine zweite Suite

Naheliegend wäre, beide Kanäle über dieselbe URL-Wurzel mit unterschiedlicher
Suite zu fahren (`stable` und `enterprise`). Das ist eine **Sicherheitsfalle**:
aptly legt für jeden Publish-Prefix einen eigenen `pool/`-Baum an, aber bei
gleichem Prefix teilen sich beide Suites den Pool. Die Metadaten unter
`dists/enterprise/` wären geschützt - die `.deb`-Dateien darunter läge aber im
gemeinsamen, offenen `pool/` und wären mit geratenem Pfad frei herunterladbar.

Mit eigenem Prefix schützt eine einzige `location`-Regel Metadaten **und**
Pakete.

### Weboberfläche (nginx)

Vollständige Konfiguration: `packaging/repo-server/nginx-lcm-repo.conf`. Die
drei relevanten Blöcke:

```nginx
# Enterprise-Kanal - Subscription-Key erforderlich.
# "^~" ohne Schrägstrich am Ende, damit auch /enterprise (ohne Slash) greift.
location ^~ /enterprise {
    auth_basic           "LCM Enterprise Repository";
    auth_basic_user_file /etc/lcm-repo/enterprise.htpasswd;
    try_files $uri $uri/ =404;
}

# aptly-API - ausschließlich CI-Zugangsdaten, eigene Datei.
location ^~ /api {
    auth_basic           "aptly API";
    auth_basic_user_file /etc/lcm-repo/api.htpasswd;
    proxy_pass           http://127.0.0.1:8080;
}

# Alles Übrige ist der offene Community-Kanal.
location / {
    try_files $uri $uri/ =404;
}
```

Zwei Punkte sind nicht verhandelbar:

- **Getrennte `htpasswd`-Dateien.** Die aptly-API kann publizieren *und
  löschen*. Ein Kunden-Key darf sie unter keinen Umständen erreichen.
- **Nur HTTPS.** Basic Auth überträgt den Key lediglich base64-kodiert. Port 80
  leitet ausschließlich um.

## Subscription-Keys verwalten

Die Keys liegen in einer einfachen Datei, aus der die `htpasswd` erzeugt wird -
kein zusätzlicher Dienst, der laufen und überwacht werden muss. Werkzeug:
`packaging/repo-server/lcm-subscriptions`.

```bash
# Neuen Kunden anlegen (Key wird EINMALIG angezeigt)
lcm-subscriptions add "Musterfirma GmbH" 2027-08-01

# Überblick mit Status (aktiv / läuft bald ab / abgelaufen)
lcm-subscriptions list

# Verlängern nach Rechnungseingang
lcm-subscriptions renew LCM-E-XXXX-XXXX-XXXX 2028-08-01

# Kündigung - wirkt sofort
lcm-subscriptions revoke LCM-E-XXXX-XXXX-XXXX
```

Gespeichert wird nur der **Hash** des Keys; im Klartext existiert er nur in dem
Moment, in dem er angelegt und dem Kunden übergeben wird. Geht er verloren,
wird er ersetzt, nicht wiederhergestellt.

Das Ablaufdatum wird durch den täglichen `sync` durchgesetzt, der abgelaufene
Keys aus der `htpasswd` entfernt:

```
0 4 * * * root /usr/local/sbin/lcm-subscriptions sync >/dev/null
```

nginx liest die `htpasswd` bei **jeder Anfrage** neu - Sperren und Verlängern
wirken deshalb sofort, ohne Reload.

## Welche Versionsnummer bekommt ein Zug?

**Die Vorabversion trägt bereits die Nummer, die final werden soll.** Aus
`1.16.0-beta.1` wird `1.16.0` - dieselbe Zahl, nur ohne Suffix. Die Beta ist
der Kandidat für genau diese Version, nicht für „irgendeine spätere".

Welche Nummer das ist, ergibt sich aus den Commits seit dem letzten Release
(Conventional Commits) und wird **vor** dem ersten Beta-Zug bestimmt:

```sh
make next-version     # zeigt die Nummer, die aus den Commits folgt
```

| Enthaltene Commits | Nummer | Beta heißt dann |
|---|---|---|
| nur `docs:`, `chore:`, `test:`, `ci:` | **kein Release** | - |
| `fix:`, `perf:`, `refactor:` | Patch (1.16.**1**) | `1.16.1-beta.1` |
| mindestens ein `feat:` | Minor (1.**17**.0) | `1.17.0-beta.1` |
| `feat!:` / `BREAKING CHANGE` | Major (**2**.0.0) | `2.0.0-beta.1` |

Release-relevant sind also nur `feat`, `fix`, `perf` und `refactor`. Bestehen
die Commits seit dem letzten Tag ausschließlich aus Doku- oder Wartungsarbeit,
meldet `make next-version` „kein neues Release" und `prepare-release.sh` tut
ohne Argument nichts. Soll so ein Stand trotzdem hinaus - etwa weil eine
Korrektur an der Dokumentation die Nutzer erreichen soll -, gibt man die
Version ausdrücklich an; sie folgt dann derselben Regel wie oben (in dem Fall
ein Patch).

Kommt während der Stabilisierung ein weiterer Commit dazu, der die Stufe
anhebt - etwa ein `feat:` in einer als Patch geplanten Reihe -, gehört die
Beta-Nummer mitgezogen: die nächste Vorabversion heißt dann `1.17.0-beta.1`
statt `1.16.1-beta.2`.

:::caution[Eine Vorabversion ist KLEINER als ihre finale Version]
`1.16.0-beta.1 < 1.16.0` - so schreiben es SemVer und (mit `~`) auch die
Debian-Versionsordnung vor. Eine Vorabversion für eine bereits
veröffentlichte Version anzulegen, ergibt deshalb keinen Sinn: Sie wäre ein
Rückschritt und würde von `apt` nie installiert.

Beta-Hosts fallen dadurch nicht zurück: Bei ihnen liegt die Beta-Quelle
**neben** der Community-Quelle, und `apt` nimmt von beiden die höhere Version.
Sobald der Community-Kanal die finale Version führt, wechseln sie automatisch
darauf - bis die nächste, höhere Vorabversion erscheint.
:::

Weicht die finale Nummer doch einmal von der Beta ab (z. B. weil erst beim
Überführen auffällt, dass ein `feat:` enthalten war), ist das technisch
folgenlos, solange sie **höher** liegt: Der Versionsvergleich stimmt, das
Upgrade greift. Zurück bleibt eine Lücke in der Nummernfolge - die
Beta-Nummer existiert dann nie als finale Version.

## Release-Ablauf

Der Community-Kanal wird unverändert von der CI bedient
(`packaging/publish-deb.sh`, siehe [CI & Release](/reference/ci-release/)).
Der Enterprise-Kanal wird von Hand nachgezogen - mit
`packaging/repo-server/lcm-channel`:

```bash
# 1. Direkt nach dem Release: den Stand einfrieren.
#    Ab jetzt ist er unveränderlich, egal was im Community-Kanal folgt.
lcm-channel freeze 1.10.0

# 2. Nach der Bewährungszeit (Richtwert 2-4 Wochen ohne kritische Meldungen):
lcm-channel promote 1.10.0
```

`freeze` erzeugt einen aptly-Snapshot, `promote` schaltet den Publish-Punkt des
Enterprise-Kanals darauf um. Kunden sehen die neue Version beim nächsten
`apt update`.

:::tip[Warum über die HTTP-API und nicht per aptly-CLI]
aptly sperrt seine Datenbank für einen einzigen Prozess. Läuft `aptly api serve`
als Dienst - und das tut es, weil die CI darüber publiziert -, scheitern
gleichzeitige CLI-Aufrufe mit einem Datenbankfehler. `lcm-channel` spricht
deshalb dieselbe HTTP-API wie die CI.
:::

## Kundenseite

Der Wechsel auf den Enterprise-Kanal läuft über ein Skript, das neben `setup.sh`
auf dem Repository-Server liegt:

```bash
curl -fsSL https://repo.techeve.de/setup-enterprise.sh | sudo sh -s -- \
  LCM-E-XXXX-XXXX-XXXX <key>
```

Ohne Argumente aufgerufen fragt es den Key interaktiv ab - dann taucht er weder
in der Shell-Historie noch in der Prozessliste auf.

Das Skript hinterlegt die Zugangsdaten in `/etc/apt/auth.conf.d/` (nur für
`root` lesbar, `0600`) und trägt die Enterprise-Quelle ein. Danach müssen die
Kanäle getrennt werden - beide gleichzeitig als LCM-Lieferant aktiv würde den
Zweck aufheben, weil apt immer die höhere Version zöge, also über kurz oder
lang die aus dem freien Kanal.

### Kanaltrennung: Vorrang-Regel statt Kahlschlag

apt kennt keine Kanäle. Es unterscheidet Quellen an dem, was im **Release-File**
steht - und dort tragen alle drei Kanäle dieselbe Suite auf demselben Host.
Deshalb bekommt jeder Publish-Punkt eine eigene Kennzeichnung (aptly: `Origin`
und `Label`, einmalig gesetzt mit
`packaging/repo-server/set-channel-metadata.sh`):

| Kanal | Origin | Label |
|---|---|---|
| Community | `TechEve` | `techeve-community` |
| Beta | `TechEve` | `techeve-beta` |
| Enterprise | `TechEve` | `techeve-enterprise` |

Damit lässt sich genau das ausdrücken, worum es geht - *die LCM-Pakete* kommen
aus dem Enterprise-Kanal, alles andere darf bleiben, wo es ist. Die Umstellung
schreibt dafür `/etc/apt/preferences.d/lcm-enterprise.pref`:

```
Package: lcm lcm-agent
Pin: release l=techeve-community
Pin-Priority: -1
```

Priorität `-1` heißt „kommt nie in Frage". Die Community-Quelle bleibt aktiv und
liefert weiter ihre anderen Pakete - beim Stilllegen der ganzen Quelle wären die
mit weg.

**Rückfall für alte Publish-Punkte:** trägt der Repository-Server noch keine
Kennzeichnung, wäre die Regel wirkungslos. Dann legt die Umstellung die
Community-Quelle still wie bisher und schreibt in das Job-Protokoll, warum.
Die Prüfung läuft gegen die *wieder eingeschaltete* Quelle, damit ein früherer
Lauf den besseren Weg nicht dauerhaft verstellt: wer erst ohne und später mit
Kennzeichnung umstellt, bekommt die Quelle zurück und die Regel an ihre Stelle.

Die Community-Quelle gibt es dabei in zwei Schreibweisen, und beide werden
behandelt: `setup.sh` legt sie heute als deb822-Datei `techeve.sources` an - die
bekommt `Enabled: no`, den Schalter, den apt selbst kennt; Name und Inhalt
bleiben, der Rückweg ist dadurch verlustfrei. Ältere Installationen haben die
klassische `techeve.list`, sie wird nach `techeve.list.disabled` umbenannt (apt
liest in `sources.list.d` nur `*.list` und `*.sources`).

:::caution[Gegenprobe gehört dazu]
Zum Schluss stellt das Skript die Frage, um die es wirklich geht: **woher käme
`lcm` jetzt?** (`apt-cache policy lcm`, Quelle unter der Kandidaten-Version).
Steht dort nicht der Enterprise-Kanal, sagt es das laut.

Genau daran hing ein Fehler bis LCM 1.12.5: die Umstellung suchte nur
`techeve.list`, `setup.sh` schrieb aber längst `techeve.sources` - das
Stilllegen griff ins Leere, die Umstellung meldete trotzdem Erfolg, und beide
Kanäle liefen nebeneinander.

Betroffene Hosts zieht man mit *Einstellungen → Subscription → Einrichtung
erneut anwenden* gerade (oder mit einem erneuten Lauf von
`setup-enterprise.sh`).
:::

Die Zugangsdaten sind dabei auf Host **und Pfad** eingegrenzt:

```
machine repo.techeve.de/enterprise
login LCM-E-XXXX-XXXX-XXXX
password <key>
```

So wird der Key ausschließlich an den Enterprise-Pfad gesendet und nie an den
öffentlichen Teil desselben Servers.

Wird der Key nicht akzeptiert - falsch eingegeben oder Subscription abgelaufen -,
macht das Skript alle Änderungen rückgängig und stellt den Community-Kanal
wieder her. Eine Maschine bleibt so nie ohne Paketquelle zurück. Der Rückweg
steht auch später offen:

```bash
curl -fsSL https://repo.techeve.de/setup-enterprise.sh | sudo sh -s -- --revert
```

## Subscription in LCM (Einstellungen → Subscription)

Statt des Skript-Wegs oben kann die Subscription direkt in der LCM-Oberfläche
verwaltet werden - vorausgesetzt, auf dem Repository-Server läuft der
**Subscription-Dienst** (eigenes Repository:
[techeve/lcm-subscription-service](https://gitlab.techeve.de/techeve/lcm-subscription-service)).
Der Ablauf:

1. Beim ersten Start erzeugt LCM eine dauerhafte **Instanz-Kennung** (UUID).
   Sie liegt in der Datenbank und wandert im Backup mit - nach einem Umzug aus
   dem Backup bleibt es dieselbe Instanz.
2. Der Betreiber trägt den Subscription-Key unter *Einstellungen →
   Subscription* ein. LCM meldet Key + Instanz-Kennung an den Dienst und
   erhält einen **an diese Instanz gebundenen Zugangsschlüssel** - der Key
   selbst öffnet das Repository nicht. Die erste Instanz gewinnt; ein bereits
   anderweitig gebundener Key wird mit Klartext abgelehnt.
3. Ein **tägliches Lebenszeichen** (verify) hält den Vertragsstand aktuell -
   die Seite zeigt „läuft in X Tagen ab", den letzten Prüfzeitpunkt und
   Fehler. Ist der Dienst nicht erreichbar, heißt der Status ehrlich
   „nicht erreichbar" (unbekannt ≠ schlecht).
4. Unter **„Paketkanal des LCM-Hosts"** stehen alle drei Kanäle zur Auswahl;
   die Umstellung läuft als Job auf dem LCM-Host. Beim Enterprise-Kanal:
   Zugangsdaten nach `/etc/apt/auth.conf.d/` (0600), eigene Quelle,
   Kanaltrennung, Prüfung nur gegen die neue Quelle - bei Fehlschlag
   vollständiger Rückbau. Der Rückweg („Community-Kanal") bricht ab, statt
   eine Maschine ohne Paketquelle zurückzulassen.

Key und Zugangsschlüssel liegen AES-GCM-verschlüsselt in der Datenbank; im
SSH-Protokoll des Umstell-Jobs wird der Zugangsschlüssel redigiert.

### Beta-Kanal: ohne Subscription, ohne Konsole

Der Beta-Kanal ist offen - für ihn braucht es **keine Subscription**. Die
Kanalauswahl steht deshalb auch auf Installationen ohne hinterlegten Key zur
Verfügung; nur der Enterprise-Eintrag ist dort gesperrt.

Anders als beim Enterprise-Wechsel tritt die Beta-Quelle **neben** die
Community-Quelle statt an ihre Stelle:

```
deb [signed-by=<keyring der Community-Quelle>] https://repo.techeve.de beta main
```

Die Adresse kommt aus der vorhandenen Community-Quelle des Hosts (eigener
Repository-Server inklusive), der Signaturschlüssel ebenso - alle Kanäle sind
mit demselben Schlüssel signiert. Es gibt hier bewusst **keine Vorrang-Regel**:
apt nimmt von beiden Quellen die neuere Version. Ein Beta-Tester bekommt damit
die Vorabversion, solange sie vorne liegt, und das fertige Release, sobald es
erscheint - Sicherheitsupdates eingeschlossen. Eine Vorrang-Regel auf den
Beta-Kanal würde genau das verhindern, sobald zwischen zwei Vorabversionen ein
fertiges Release liegt.

Geprüft wird nur gegen die neue Quelle: kennt der Server die Suite `beta`
nicht, scheitert der Job und die Quelle wird wieder entfernt - ein Host bleibt
nie mit einer toten Paketquelle zurück. Der Wechsel auf einen anderen Kanal
räumt die Beta-Quelle wieder ab.

:::note[Downgrades macht apt nicht]
Wer zurück auf Community wechselt, bleibt auf der installierten Vorabversion
stehen, bis das fertige Release sie überholt. Das ist kein Fehler, sondern
apt-Verhalten - ein Zurückstufen müsste von Hand kommen (`apt install
lcm=<version>`) und ist bei Datenbank-Migrationen ohnehin nichts, was man
beiläufig tut.
:::

## LCM aktualisiert sich selbst

Der LCM-Host steht mit in der Verwaltung, und das Paket `lcm` liegt in
demselben Kanal wie alles andere. Wer dort Pakete aktualisiert - über die
Server-Detailansicht, eine Regel oder den Beta-Wechsel oben - aktualisiert
damit früher oder später LCM selbst.

Dabei passiert zwangsläufig etwas Ungewöhnliches: Das Paket startet den Dienst
neu, und der Dienst führt in diesem Moment den Job aus, der ihn ersetzt. Die
laufende Aktion verliert ihren eigenen Ausführenden, noch bevor sie ein
Ergebnis schreiben kann.

LCM erkennt das beim nächsten Start am Zusammentreffen dreier Umstände: Es
läuft in einer anderen Version als zuvor (`version.json`), der offene Job
spielte Pakete ein, und er lief auf dem LCM-Host. Dann gilt der Job als
**erfolgreich** - das Update ist ja nachweislich eingespielt, sonst liefe die
neue Version nicht. Im Protokoll steht der Versionswechsel als Begründung.

Anschließend erfasst LCM den eigenen Host neu, damit die Übersicht die
tatsächlich installierte Version zeigt und nicht den Stand von vor dem Update.

Zwei Abgrenzungen, damit daraus keine Schönfärberei wird:

- Nur **Paket-Jobs auf dem LCM-Host** fallen darunter. Ein unterbrochener Lauf
  auf einem anderen Server bleibt ein Fehler - über einen fremden Rechner darf
  LCM nichts behaupten, was es nicht geprüft hat.
- Ohne Versionswechsel gibt es den Sonderfall nicht. Ein Neustart aus anderem
  Grund (Absturz, manueller Restart) lässt unterbrochene Jobs weiterhin als
  Fehler stehen.

Die **Oberfläche** zieht ebenfalls nach: Eine geöffnete Seite hält ihr
JavaScript im Speicher und bekäme vom Austausch der Dateien nichts mit. Sie
prüft deshalb regelmäßig die Kennung des laufenden Builds und lädt sich bei
einem Wechsel selbst neu - mit kurzer Ansage, damit der Bildschirminhalt nicht
kommentarlos verschwindet.

Damit ein Neuladen überhaupt etwas bringt, muss der Server die neue
`index.html` auch herausrücken. Die Asset-Dateien tragen einen Hash im Namen
und sind deshalb dauerhaft cachebar; die `index.html`, die auf sie verweist,
darf es nicht sein. Sie wird mit `Cache-Control: no-cache` und einem **ETag aus
der Build-Kennung** ausgeliefert: Bei gleichem Build antwortet der Server mit
`304 Not Modified` (nichts wird übertragen), bei neuem Build mit `200` und der
neuen Datei.

:::note[Warum nicht Last-Modified]
Das Frontend liegt als eingebetteter Dateibaum im Binary, und dort trägt jede
Datei den Nullzeitpunkt als Änderungsdatum. Ein daraus gebildetes
`Last-Modified` ist über alle Versionen hinweg identisch - der Browser
revalidierte also brav, bekam immer ein `304` und behielt die alte
`index.html` samt der alten Asset-Verweise. Nach einem Update lief die alte
Oberfläche weiter, und auch „Jetzt neu laden" endete wieder dort. Die
Build-Kennung ist die einzige Angabe, die sich mit jedem Release verlässlich
ändert.
:::

### Die Prüfung folgt dem eingestellten Kanal

„Gibt es eine neuere Version?" wird gegen **den Kanal beantwortet, auf dem der
Host steht** - nicht pauschal gegen Community. LCM liest dazu denselben
Paket-Index, aus dem auch `apt update` seine Version bezieht:

| Kanal | Abgefragter Index | Zugang |
|---|---|---|
| Community | `<repo>/dists/stable/…/Packages` | offen |
| Beta | `<repo>/dists/beta/…/Packages` | offen |
| Enterprise | `<subscription-repo>/dists/stable/…/Packages` | Instanz-ID + Zugangsschlüssel |

Das ist der Unterschied zwischen „aktuell" und „aktuell **für mich**": Eine
Vorabversion liegt über der stabilen, ein abgehangenes Enterprise-Release
darunter. Wer auf Beta stand, bekam vorher die stabile Version gemeldet und
sah deshalb dauerhaft kein Update, obwohl in seinem Kanal längst eins bereitlag.

Geprüft wird alle drei Stunden und einmal beim Start. Im **Info-Fenster**
(Klick auf den Copyright-Vermerk in der Fußzeile) steht die zuletzt ermittelte
Version samt Kanal, dazu die Schaltfläche **„Aktuelle Version prüfen"** für
eine sofortige Abfrage - praktisch direkt nach einem Kanalwechsel, wo der
zwischengespeicherte Stand noch den alten Kanal betrifft.

Ist der Enterprise-Kanal eingestellt, aber kein Repository oder kein gültiger
Zugangsschlüssel hinterlegt, meldet die Prüfung das als Fehler. Sie weicht
bewusst **nicht** auf den Community-Kanal aus - eine Version aus einem fremden
Kanal als „neueste" auszugeben, wäre schlechter als eine ehrliche Fehlanzeige.

## Grenzen dieses Aufbaus

- **Kein Kopierschutz.** In beiden Kanälen liegt dasselbe Binary; wer den Key
  weitergibt, gibt Zugang weiter. Der Schutz ist vertraglich, nicht technisch -
  bei identischen Paketen ist alles andere Selbstbetrug. Der Subscription-Dienst
  verhindert die Mehrfach-*Aktivierung* eines Keys, nicht die Weitergabe von
  Paketen.
- **Serverzahl wird gemeldet, nie durchgesetzt.** Community und Enterprise
  können beide unbegrenzt Server verwalten; die beim Lebenszeichen gemeldete
  Zahl ist Anzeige beim Anbieter, kein Limit.
