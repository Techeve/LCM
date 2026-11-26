---
sidebar:
  order: 27
title: Paketkanäle (Community, Beta & Enterprise)
description: Die Paketkanäle von repo.techeve.de - Community, Beta und Enterprise - für apt, dnf, zypper, apk und pacman - Unterschiede, Einrichtung mit setup.sh, Schlüssel, Subscription und Update-Prüfung.
---

LCM wird über ein eigenes Paket-Repository ausgeliefert: `repo.techeve.de`. Es
liefert Pakete für **apt** (Debian, Ubuntu), **dnf** und **zypper** (rpm),
**apk** (Alpine) und **pacman** (Arch Linux). Es bedient **drei Kanäle**: einen
offenen Community-Kanal, einen ebenfalls offenen Beta-Kanal mit Vorabversionen
und einen Enterprise-Kanal mit Subscription.

Die Seite zeigt, wie sich die Kanäle unterscheiden, wie du die Quellen
einrichtest und prüfst, wie der Wechsel auf Enterprise und Beta in LCM läuft und
wie LCM die Update-Prüfung je Kanal macht. Wer LCM nur **installieren** möchte,
findet den Weg unter [Installation](/getting-started/installation/).

## Das Modell

Bei apt gilt eine Regel für alle Kanäle: der **URL-Pfad bestimmt den Zugang**,
die **Suite den Kanal**, die Komponente ist immer `main`. Die
anderen Paketformate haben je Kanal einen eigenen Pfad (siehe unten).

Alle Kanäle liefern **dieselben Pakete**, gebaut aus demselben Commit,
signiert mit demselben Schlüssel. Der einzige Unterschied ist der Zeitpunkt:

| | Beta | Community | Enterprise |
|---|---|---|---|
| apt-Zeile | `deb … https://repo.techeve.de beta main` | `deb … https://repo.techeve.de community main` | `deb … https://repo.techeve.de/enterprise/lcm enterprise main` |
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

Die Tabelle gilt für apt. **Enterprise gibt es nur für apt.** Für dnf, zypper,
apk und pacman gibt es die Kanäle Community und Beta.

## Paketverwaltungen und Quellen

Welche Systeme das sind und was davon getestet ist, steht unter
[Unterstützte Systeme](/reference/supported-systems/). Hier steht, wo die Quelle
liegt und was sie braucht. Die Spalte „Quelle" zeigt den Kanal Community.

| Paketverwaltung | Pakete | Quelle | Datei, die `setup.sh` anlegt | Schlüssel |
|---|---|---|---|---|
| apt | `lcm`, `lcm-agent` | `https://repo.techeve.de`, Suite `community`, Komponente `main` | `/etc/apt/sources.list.d/techeve.sources` (deb822) | `/etc/apt/keyrings/techeve-repo.gpg`, geholt von `/repo-key.gpg` |
| dnf | `lcm-agent` | `https://repo.techeve.de/rpm/community/$basearch/` | `/etc/yum.repos.d/techeve-lcm.repo` | `/keys/techeve-packages.asc`, mit `rpm --import` eingespielt |
| zypper | `lcm-agent` | wie dnf | `/etc/zypp/repos.d/techeve-lcm.repo` (zusätzlich `type=rpm-md`, `autorefresh=1`) | wie dnf |
| apk | `lcm-agent` | `https://repo.techeve.de/apk/community` | eine Zeile in `/etc/apk/repositories` | `/etc/apk/keys/techeve-apk.rsa.pub`, geholt von `/keys/techeve-apk.rsa.pub` |
| pacman | `lcm-agent` | `https://repo.techeve.de/arch/community/$arch` | Abschnitt `[techeve]` in `/etc/pacman.conf` (`SigLevel = Required`) | `/keys/techeve-packages.asc`, in den pacman-Schlüsselbund aufgenommen und lokal signiert |

Drei Punkte zu den Paketen:

- **`lcm` gibt es nur für apt.** Das ist der LCM-Server. Auf den anderen Systemen
  läuft LCM im Docker-Container, siehe [Installation](/getting-started/installation/).
  Auf diesen Systemen verwaltest du Server mit dem `lcm-agent`, den es für alle
  fünf Paketverwaltungen gibt.
- **Architekturen:** apt liefert `amd64` und `arm64`, die anderen Formate
  `x86_64` und `aarch64`. Ein Paket ohne Architektur (`noarch`, `any`) liegt in
  beiden Verzeichnissen.
- **Enterprise** liegt nur bei apt unter `https://repo.techeve.de/enterprise/lcm`,
  Suite `enterprise`, mit Zugangsdaten (siehe [Wechsel auf Enterprise](#wechsel-auf-enterprise)). Dort
  liegt `lcm`, nicht `lcm-agent`.

Die Verzeichnisse der anderen Formate unter `https://repo.techeve.de`:

| Pfad | Inhalt |
|---|---|
| `/rpm/<kanal>/<arch>/` | `repodata/` mit `repomd.xml` und `repomd.xml.asc`, dazu die `.rpm`-Dateien |
| `/apk/<kanal>/<arch>/` | `APKINDEX.tar.gz` und die `.apk`-Dateien |
| `/arch/<kanal>/<arch>/` | Datenbank `techeve.db` und `techeve.files`, die `.pkg.tar.zst`-Dateien, jeweils mit `.sig` |
| `/keys/` | die öffentlichen Schlüssel, siehe unten |

`<kanal>` ist `community` oder `beta`. Jeder Kanal hat einen gültigen, signierten
Index, auch bevor das erste Paket darin liegt. Alle Indizes und bei rpm und Arch
auch die Pakete sind signiert; `setup.sh` schaltet die Prüfung ein
(`gpgcheck=1` und `repo_gpgcheck=1` bei dnf und zypper, `SigLevel = Required`
bei pacman).

Alte Versionen bleiben abrufbar. Bei rpm und apk führt der Index alle Versionen,
du installierst eine bestimmte mit
`dnf --setopt=disable_excludes=techeve-lcm install lcm-agent-<version>`,
`zypper install lcm-agent=<version>` oder `apk add lcm-agent=<version>`. pacman führt in seiner Datenbank nur die jüngste
Version; eine ältere holst du mit `pacman -U` von ihrer festen Adresse unter
`/arch/<kanal>/<arch>/`.

## Einrichtung mit setup.sh

Ein Befehl richtet die Quelle auf allen fünf Paketverwaltungen ein. Er läuft als
`root`:

```bash
# Community-Kanal
curl -fsSL https://repo.techeve.de/setup.sh | sudo sh

# Beta-Kanal
curl -fsSL https://repo.techeve.de/setup.sh | sudo sh -s -- --beta

# Quelle und Schlüssel wieder entfernen
curl -fsSL https://repo.techeve.de/setup.sh | sudo sh -s -- --uninstall
```

`setup.sh` nimmt die erste Paketverwaltung, die es findet (Reihenfolge: apt,
dnf, zypper, apk, pacman), legt Schlüssel und Quelle an und liest den Index
einmal ein. Es lässt sich wiederholen.

| Aufruf | Wirkung |
|---|---|
| ohne Option | Kanal Community |
| `--beta` | Kanal Beta statt Community |
| `--uninstall` (auch `--remove`) | entfernt Quelle und Schlüssel, nicht die installierten Pakete |
| `--help` | zeigt die Hilfe |
| `SUITE=beta` (nur apt) | wählt wie `--beta` den Kanal Beta |
| `TECHEVE_REPO_URL=<Adresse>` | ersetzt `https://repo.techeve.de`, zum Beispiel für Tests oder einen eigenen Server |

Was du wissen solltest:

- **Ein erneuter Lauf ersetzt die Quelle.** `--beta` ersetzt die Community-Quelle
  durch die Beta-Quelle, ein Lauf ohne `--beta` stellt auf Community zurück. Bei
  apt tauscht es die Suite in `techeve.sources` aus. Willst du bei apt Beta
  **neben** Community, nimm den Wechsel in LCM (siehe
  [Beta-Kanal: ohne Subscription, ohne Konsole](#beta-kanal-ohne-subscription-ohne-konsole)).
- **Der Agent bleibt auf seiner Version.** Das Paket `lcm-agent` hält sich nach
  der Installation selbst fest, damit ein Systemupdate ihn nicht verschiebt
  (apt: Datei in `/etc/apt/preferences.d/`, dnf: `exclude=lcm-agent` in der
  Quelle, zypper: Paketsperre, pacman: `IgnorePkg`). `setup.sh` trägt das nicht
  vorab ein, damit die erste Installation ohne Umweg geht; ein erneuter Lauf
  behält ein vorhandenes `exclude`. Aktualisiert wird der Agent über LCM, siehe
  [Unterstützte Systeme](/reference/supported-systems/#lcm-agent).
- **dnf und zypper:** `--uninstall` entfernt nur die Quelle. Den importierten
  Schlüssel behält die rpm-Datenbank.
- **pacman:** `--uninstall` entfernt den Abschnitt `[techeve]` und genau den
  Techeve-Schlüssel, dessen Fingerabdruck in `/etc/pacman.d/techeve-packages.asc`
  steht. Ist der Fingerabdruck nicht zu ermitteln, bleibt der Schlüsselbund
  unberührt. Hat der Kernel kein Landlock (etwa Arch in einem LXC-Container),
  startet `setup.sh` pacman einmal mit `--disable-sandbox`, weil der Download
  sonst scheitert. `pacman.conf` bekommt diese Einstellung nie.
- **Alter Name `stable`:** Die Suite `stable` ist der alte Name von `community`.
  Bestehende Installationen laufen weiter, die Pakete `lcm` und `lcm-agent`
  stellen die Quelle beim Update selbst auf `community` um. Das betrifft nur
  Einträge für `https://repo.techeve.de` mit der Suite `stable`, nie Enterprise-,
  Beta- oder fremde Quellen.
- **Sprache:** Die Meldungen sind englisch, außer das System steht auf Deutsch.

### Beta-Kanal je Paketformat

| Paketverwaltung | Quelle im Kanal Beta |
|---|---|
| apt | `https://repo.techeve.de`, Suite `beta`, Komponente `main` |
| dnf, zypper | `https://repo.techeve.de/rpm/beta/$basearch/` |
| apk | `https://repo.techeve.de/apk/beta` |
| pacman | `https://repo.techeve.de/arch/beta/$arch` |

Jedes Format schreibt Vorabversionen anders. Aus `2.4.0-beta.1` wird bei apt und
rpm `2.4.0~beta.1`, bei apk `2.4.0_beta1` und bei Arch `2.4.0beta.1`. Jedes
Format sortiert die Vorabversion vor die fertige Version.

## Schlüssel

Alle Kanäle sind signiert. Die öffentlichen Schlüssel liegen frei abrufbar auf
dem Server:

| Datei | Verwendung |
|---|---|
| `https://repo.techeve.de/keys/techeve-packages.asc` | GPG-Schlüssel für rpm (dnf, zypper) und Arch (pacman) |
| `https://repo.techeve.de/keys/techeve-apk.rsa.pub` | RSA-Schlüssel für apk |
| `https://repo.techeve.de/repo-key.gpg` | GPG-Schlüssel für apt (auch als `/repo-key.asc`) |

Der apt-Schlüssel liegt nicht unter `/keys/`. Er ist ein eigener Schlüssel und
gilt für die Suiten `community`, `beta` und `enterprise` gemeinsam. `setup.sh`
holt immer den Schlüssel, den die erkannte Paketverwaltung braucht. Bei apk
trägt der Dateiname im Verzeichnis `/etc/apk/keys/` zugleich den Namen des
Schlüssels im Index; benenne die Datei nicht um.

## Wechsel auf Enterprise

Der Wechsel auf den Enterprise-Kanal gilt für den LCM-Host mit apt und läuft in
LCM unter *Einstellungen → Subscription* (siehe
[Subscription in LCM](#subscription-in-lcm-einstellungen--subscription)). Der
Subscription-Key selbst öffnet das Repository nicht. LCM tauscht ihn gegen einen
an die Instanz gebundenen Zugangsschlüssel. Beim Repository meldet sich LCM mit
der Instanz-ID als Login und dem Zugangsschlüssel als Passwort an.

Die Umstellung legt diese Dateien an:

| Datei | Inhalt |
|---|---|
| `/etc/apt/auth.conf.d/lcm-enterprise.conf` | Zugangsdaten, nur für `root` lesbar (`0600`) |
| `/etc/apt/sources.list.d/lcm-enterprise.list` | `deb [signed-by=…] https://repo.techeve.de/enterprise/lcm enterprise main`, mit dem Schlüsselbund der Community-Quelle |
| `/etc/apt/preferences.d/lcm-enterprise.pref` | die Vorrang-Regel unten |

Eine Beta-Quelle (`lcm-beta.list`) räumt die Umstellung ab. Danach müssen die
Kanäle getrennt werden - beide gleichzeitig als LCM-Lieferant aktiv würde den
Zweck aufheben, weil apt immer die höhere Version zöge, also über kurz oder
lang die aus dem freien Kanal.

### Kanaltrennung: Vorrang-Regel nach Suite

apt kennt keine Kanäle, es unterscheidet Quellen an dem, was im **Release-File**
steht. Dort hat jeder Kanal seine eigene Suite (`community`, `beta`,
`enterprise`; dazu der alte Name `stable`). Genau das nutzt die Umstellung: *das
Paket `lcm`* kommt aus dem Enterprise-Kanal, alles andere darf bleiben, wo es
ist. Sie schreibt `/etc/apt/preferences.d/lcm-enterprise.pref`:

```
Package: lcm
Pin: release a=community
Pin-Priority: -1

Package: lcm
Pin: release a=stable
Pin-Priority: -1

Package: lcm
Pin: release a=beta
Pin-Priority: -1
```

Priorität `-1` heißt „kommt nie in Frage". Die Community-Quelle bleibt aktiv und
liefert weiter ihre anderen Pakete - beim Stilllegen der ganzen Quelle wären die
mit weg. Die Regel greift immer, sie braucht keine Vorprüfung am Server. Eine
Community-Quelle, die ein älteres LCM beim Wechsel stilllegte, schaltet die
Umstellung wieder ein.

Gesperrt wird nur `lcm`. Der `lcm-agent` liegt nicht im Enterprise-Kanal; würde
man ihn sperren, käme er auf dem LCM-Host aus keiner Quelle mehr.

Die Community-Quelle gibt es in zwei Schreibweisen, `setup.sh` legt die
deb822-Datei `techeve.sources` an, ältere Installationen haben die klassische
`techeve.list`; beide bleiben bestehen.

:::caution[Gegenprobe gehört dazu]
Zum Schluss stellt die Umstellung die Frage, um die es wirklich geht: **woher
käme `lcm` jetzt?** (`apt-cache policy lcm`, Suite unter der Kandidaten-Version).
Steht dort nicht `enterprise`, meldet sie das als Warnung im Protokoll des Jobs.
:::

Die Zugangsdaten sind dabei auf Host **und Pfad** eingegrenzt:

```
machine https://repo.techeve.de/enterprise/lcm
login <Instanz-ID>
password <Zugangsschlüssel>
```

So wird der Zugangsschlüssel ausschließlich an den Enterprise-Pfad gesendet und
nie an den öffentlichen Teil desselben Servers.

Wird der Zugang nicht akzeptiert - Subscription abgelaufen oder Schlüssel
ungültig -, macht die Umstellung alle Änderungen rückgängig und stellt den
Community-Kanal wieder her. Eine Maschine bleibt so nie ohne Paketquelle zurück.
Der Rückweg steht auch später offen: in LCM unter *Paketkanal des LCM-Hosts* den
Community-Kanal wählen. Das entfernt Zugangsdaten, Enterprise-Quelle,
Vorrang-Regel und eine eventuelle Beta-Quelle.

## Subscription in LCM (Einstellungen → Subscription)

Die Subscription wird direkt in der LCM-Oberfläche verwaltet. Dazu fragt LCM den
**Subscription-Dienst** des Repository-Servers. Der Ablauf:

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
   die Umstellung läuft als Job auf dem LCM-Host (der apt nutzt). Beim Enterprise-Kanal:
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

Die Zeile steht in `/etc/apt/sources.list.d/lcm-beta.list`. Die Adresse kommt
aus der vorhandenen Community-Quelle des Hosts (eigener
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
Im Binary trägt jede eingebettete Datei denselben Nullzeitpunkt als
Änderungsdatum, ein `Last-Modified` wäre also über alle Versionen identisch. Die
Build-Kennung ist die einzige Angabe, die sich mit jedem Release verlässlich
ändert.
:::

### Die Prüfung folgt dem eingestellten Kanal

„Gibt es eine neuere Version?" wird gegen **den Kanal beantwortet, auf dem der
Host steht** - nicht pauschal gegen Community. LCM liest dazu denselben
Paket-Index, aus dem auch `apt update` seine Version bezieht:

| Kanal | Abgefragter Index | Zugang |
|---|---|---|
| Community | `<repo>/dists/community/…/Packages` | offen |
| Beta | `<repo>/dists/beta/…/Packages` | offen |
| Enterprise | `<subscription-repo>/dists/enterprise/…/Packages` | Instanz-ID + Zugangsschlüssel |

Das ist der Unterschied zwischen „aktuell" und „aktuell **für mich**": Eine
Vorabversion liegt über der stabilen, ein abgehangenes Enterprise-Release
darunter.

Geprüft wird alle drei Stunden und einmal beim Start. Im **Info-Fenster**
(Klick auf den Copyright-Vermerk in der Fußzeile) steht die zuletzt ermittelte
Version samt Kanal, dazu die Schaltfläche **„Aktuelle Version prüfen"** für
eine sofortige Abfrage - praktisch direkt nach einem Kanalwechsel, wo der
zwischengespeicherte Stand noch den alten Kanal betrifft.

Ist der Enterprise-Kanal eingestellt, aber kein Repository oder kein gültiger
Zugangsschlüssel hinterlegt, meldet die Prüfung das als Fehler. Sie weicht
bewusst **nicht** auf den Community-Kanal aus - eine Version aus einem fremden
Kanal als „neueste" auszugeben, wäre schlechter als eine ehrliche Fehlanzeige.

## Serverzahl

Community und Enterprise können beide unbegrenzt Server verwalten. LCM meldet die
Zahl der Server mit dem täglichen Lebenszeichen; sie wird nur angezeigt und nie
als Limit durchgesetzt.
