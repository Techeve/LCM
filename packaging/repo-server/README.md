# Konfiguration des Paket-Repository-Servers

Dieses Verzeichnis enthält die Konfiguration für **repo.techeve.de** - den
Server, der die LCM-Pakete ausliefert. Die Dateien hier werden **nicht** mit
einem Paket installiert, sondern von Hand auf dem Repository-Server abgelegt.

Hintergrund und vollständige Anleitung: **docs/reference/repo-channels.md**
(englisch: `docs/en/reference/repo-channels.md`).

## Das Modell in einem Satz

Drei Kanäle aus einer aptly-Instanz, jeder wird von der CI aus **seinem
Branch** befüllt (`beta`/`community`/`enterprise`): Beta liefert Vorabversionen,
Community jedes stabile Release, Enterprise den konservativen Wartungszweig
(voriges Feature-Release + schnelle Fixes) hinter dem Subscription-Key.

| Kanal | Branch | aptly-Repo | URL / apt-Zeile | Zugang |
|---|---|---|---|---|
| Beta | `beta` | `lcm-beta` | `deb https://repo.techeve.de beta main` | offen |
| Community | `community` | `techeve` | `deb https://repo.techeve.de stable main` | offen |
| Enterprise | `enterprise` | `lcm-enterprise` | `deb https://repo.techeve.de/enterprise stable main` | Subscription-Key |

Die Kanal-Zuordnung trifft die CI (`.gitlab-ci.yml`, Job `deploy:apt` →
`packaging/publish-deb.sh` mit `REPO_NAME`/`DISTRO`/`PUBLISH_PREFIX`).

## Dateien

| Datei | Ziel auf dem Server | Zweck |
|---|---|---|
| `nginx-lcm-repo.conf` | `/etc/nginx/sites-available/repo.techeve.de` | Community + Beta offen, `/enterprise` per Basic Auth, aptly-API getrennt abgesichert |
| `lcm-subscriptions` | `/usr/local/sbin/lcm-subscriptions` | Keys anlegen/verlängern/sperren, Ablauf durchsetzen |
| `lcm-channel` | `/usr/local/sbin/lcm-channel` | **veraltet** - Snapshot-Modell; nur noch bis zur Migration (s. u.) |
| `lcm-channel-verify` | `/usr/local/sbin/lcm-channel-verify` | prüft je Kanal, ob der veröffentlichte Index zum Repository passt |
| `set-channel-metadata.sh` | (einmalig ausführen) | gibt jedem Kanal ein eigenes `Origin`/`Label` - Voraussetzung für die Kanaltrennung per apt-Vorrang-Regel |
| `setup-enterprise.sh` | `<aptly-public>/setup-enterprise.sh` | läuft beim **Kunden**: Key eintragen und auf den Enterprise-Kanal wechseln |

## Einrichtung der neuen Kanäle (einmalig)

Voraussetzung: die bisherige Einrichtung (nginx, htpasswd-Dateien,
`lcm-subscriptions`) besteht bereits - für Beta und den CI-gespeisten
Enterprise-Kanal kommen nur zwei aptly-Repos dazu. nginx braucht für den
Beta-Kanal KEINE Änderung (gleiche Wurzel, nur eine weitere Suite).

```bash
export REPO_URL=https://repo.techeve.de REPO_USER=ci REPO_PASS=...
API="curl -fsS -u $REPO_USER:$REPO_PASS $REPO_URL/api"

# 1. aptly-Repo für den Beta-Kanal + Erstveröffentlichung der Suite "beta"
#    an der Wurzel (öffentlich, teilt sich den public-Baum mit stable).
$API/repos -X POST -H 'Content-Type: application/json' \
  --data '{"Name":"lcm-beta","DefaultDistribution":"beta"}'
$API/publish/:. -X POST -H 'Content-Type: application/json' \
  --data '{"SourceKind":"local","Sources":[{"Name":"lcm-beta","Component":"main"}],"Distribution":"beta","Signing":{"Batch":true,"GpgKey":"repo@techeve.de"}}'

# 2. aptly-Repo für den Enterprise-Kanal (Veröffentlichung erst bei der
#    Migration, s. u. - bis dahin liefert der Kanal den bestehenden Snapshot).
$API/repos -X POST -H 'Content-Type: application/json' \
  --data '{"Name":"lcm-enterprise","DefaultDistribution":"stable"}'
```

### Migration des Enterprise-Kanals (Snapshot → CI-gespeist)

Sobald das erste Release aus dem `enterprise`-Branch ansteht, den Kanal
einmalig vom alten Snapshot auf das CI-Repo umstellen. **Vorher** das
`lcm-enterprise`-Repo mit dem aktuell ausgelieferten Stand befüllen, damit
Bestandskunden beim Wechsel keine leere Paketliste sehen:

```bash
# Aktuell im Enterprise-Kanal ausgelieferte Pakete in das neue Repo kopieren
# (Paketliste des alten Snapshots anzeigen, dann per Paket-Referenz übernehmen):
$API/publish                       # nachsehen, welcher Snapshot publiziert ist
$API/snapshots/lcm-<version>/packages
$API/repos/lcm-enterprise/packages -X POST -H 'Content-Type: application/json' \
  --data '{"PackageRefs":[ ... Ausgabe von oben ... ]}'

# Publish unter dem Prefix "enterprise" vom Snapshot auf das lokale Repo umstellen:
$API/publish/enterprise/stable -X DELETE
$API/publish/enterprise -X POST -H 'Content-Type: application/json' \
  --data '{"SourceKind":"local","Sources":[{"Name":"lcm-enterprise","Component":"main"}],"Distribution":"stable","Signing":{"Batch":true,"GpgKey":"repo@techeve.de"}}'
```

Ab dann publiziert die CI des `enterprise`-Branches direkt in den Kanal;
`lcm-channel freeze/promote` wird nicht mehr benutzt.

### Nachprüfen, ob ein Kanal wirklich ausliefert

Ein Publish-Aufruf kann gelingen, ohne etwas auszurichten: Hängt der
Publish-Punkt an einem **Snapshot** statt am lokalen Repo, veröffentlicht aptly
gehorsam wieder den Snapshot - HTTP 200, alte Pakete. Genau so lieferte der
Enterprise-Kanal über vier Releases hinweg eine alte Version aus, während jeder
CI-Job Erfolg meldete.

```bash
lcm-channel-verify              # alle drei Kanäle
lcm-channel-verify enterprise   # nur einen
```

Gemeldet werden beide Richtungen: Pakete, die **im Index stehen, aber nicht im
Repo** (verschwänden beim nächsten Publish), und solche, die **im Repo liegen,
aber nicht im Index** (der Kanal braucht ein Publish). Exit-Code 1 bei
Abweichung, also auch für einen nächtlichen Cron-Lauf brauchbar.

Seit dem Release-Fix prüft `publish-deb.sh` dasselbe nach jedem Deploy für die
soeben gelieferte Version; `lcm-channel-verify` schaut zusätzlich auf den
gesamten Bestand.

### Kennzeichnung der Kanäle (einmalig, danach trägt die CI sie weiter)

apt unterscheidet Quellen an dem, was im Release-File steht - und dort tragen
alle drei Kanäle dieselbe Suite auf demselben Host. Ohne eigene Kennzeichnung
bleibt einem Enterprise-Host nur, die ganze Community-Quelle stillzulegen; damit
sind auch alle **anderen** Pakete von dort weg. Mit `Origin`/`Label` je Kanal
genügt eine Vorrang-Regel für die LCM-Pakete allein.

```bash
export REPO_URL=https://repo.techeve.de REPO_USER=ci REPO_PASS=...
./set-channel-metadata.sh          # zeigt nur an, was sich ändern würde
./set-channel-metadata.sh apply
```

Vergeben wird `Origin: TechEve` und `Label: techeve-community` /
`techeve-beta` / `techeve-enterprise`.

:::caution
aptly hält `Origin`/`Label` am **Publish-Punkt** fest, und sie lassen sich nur
beim Anlegen setzen. Das Skript löscht die Veröffentlichung deshalb und legt sie
aus denselben Quellen neu an. Die Pakete liegen im aptly-Repo, nicht in der
Veröffentlichung - verloren geht nichts, aber der Kanal ist für die ein, zwei
Sekunden dazwischen nicht erreichbar. Wer in genau diesem Moment `apt update`
laufen lässt, sieht einen 404 und versucht es später wieder.
:::

## Laufender Betrieb

```bash
# Neuer Kunde
lcm-subscriptions add "Musterfirma GmbH" 2027-08-01

# Subscription gekündigt - wirkt sofort, ohne nginx-Reload
lcm-subscriptions revoke LCM-E-XXXX-XXXX-XXXX
```

Releases in die Kanäle macht die CI; auf dem Server ist dafür nichts zu tun.

## Wichtig

- Der **Community-Pfad bleibt unverändert** auf der Wurzel. Bestandsinstallationen
  haben `deb … repo.techeve.de stable main` in ihren Quellen - jede Änderung
  daran bricht sie beim nächsten `apt update`. Gleiches gilt für Enterprise-
  Kunden mit `deb … repo.techeve.de/enterprise stable main`.
- Der Enterprise-Kanal liegt bewusst unter einem eigenen **aptly-Prefix**, nicht
  unter einer eigenen Suite: Nur so bekommt er einen eigenen `pool/`-Baum und die
  `.deb`-Dateien sind mitgeschützt. Beta dagegen liegt als Suite an der Wurzel -
  Beta ist öffentlich, ein gemeinsamer Pool mit stable ist dort in Ordnung.
- **apt macht keine Downgrades**: Wer vom Community- auf den Enterprise-Kanal
  wechselt, bleibt auf seiner installierten Version stehen, bis die
  Enterprise-Linie ihn überholt (Enterprise hinkt genau ein Feature-Release
  hinterher, deshalb passiert das spätestens beim nächsten Release-Zug).
- Ein Subscription-Key darf die **aptly-API niemals** erreichen - sie kann
  publizieren und löschen. Deshalb zwei getrennte `htpasswd`-Dateien.
- Basic Auth überträgt den Key nur base64-kodiert. Der Enterprise-Pfad muss
  daher **ausschließlich über HTTPS** erreichbar sein.
