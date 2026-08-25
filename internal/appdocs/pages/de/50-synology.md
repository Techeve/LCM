# Synology-NAS einbinden

LCM überwacht Synology-Geräte über die **DSM-Web-API** - nicht über SSH. Das ist
Absicht: SSH ist auf einem NAS ab Werk abgeschaltet und sollte es auch bleiben.
Die API liefert alles Nötige, ohne einen zusätzlichen Zugangsweg zu öffnen.

> Erfasst werden DSM-Version, verfügbare Aktualisierungen, Speicherbelegung und
> der Zustand der Volumes. Paketverwaltung, Docker-Inventar, Benutzer-Sync und
> CVE-Bewertung gibt es hier nicht - DSM ist kein gewöhnliches Linux.

## Schritt 1 - Ein eigenes Konto in DSM anlegen

Lege in DSM unter *Systemsteuerung → Benutzer & Gruppe* ein **eigenes Konto für
LCM** an und mach es zum Mitglied der Gruppe **administrators**. Ein geteiltes
Admin-Konto zu verwenden ist keine gute Idee - mit einem eigenen Konto siehst du
im DSM-Protokoll, was LCM war und was ein Mensch.

> **Wichtig:** Für dieses Konto darf **keine Zwei-Faktor-Anmeldung erzwungen**
> sein. Ein Scan läuft unbeaufsichtigt und kann keinen Einmalcode eingeben.
>
> Damit das Konto trotzdem gut geschützt ist, schränke es stattdessen auf die
> Adresse des LCM-Servers ein: *Systemsteuerung → Sicherheit → Konto*. Dann
> nützt das Passwort von anderswo nichts.

## Schritt 2 - Gerät in LCM aufnehmen

In LCM auf **„+ Server hinzufügen"** und oben den Modus **Synology DSM** wählen.
Einzutragen sind:

- **Name** für die Übersicht
- **Host/IP** des NAS
- **DSM-Port** - standardmäßig `5001` (HTTPS)
- **Konto** und **Passwort** des eben angelegten Benutzers

## Schritt 3 - Zertifikat bestätigen

LCM zeigt den **Fingerprint des TLS-Zertifikats** deines NAS. Vergleiche ihn in
DSM unter *Systemsteuerung → Sicherheit → Zertifikat* und bestätige.

Dieser Schritt ist kein Formalismus: Synology liefert ab Werk ein
selbstsigniertes Zertifikat, das sich nicht gegen eine offizielle Stelle prüfen
lässt. LCM merkt sich deshalb genau diesen Fingerprint und **bricht die
Verbindung künftig ab, wenn er sich ändert** - dieselbe Absicherung wie beim
SSH-Host-Key.

Anschließend erhebt LCM den Zustand sofort; das Gerät erscheint online.

## Wenn es klemmt

| Problem | Ursache und Abhilfe |
|---|---|
| Anmeldung schlägt fehl, Zugangsdaten stimmen aber | Für das Konto ist 2FA erzwungen. Nimm die Erzwingung für dieses eine Konto zurück und sichere es über die IP-Beschränkung ab. |
| Keine Verbindung zum Port | Der DSM-Port weicht ab (`5001` für HTTPS, `5000` unverschlüsselt). Bei geändertem Port den tatsächlichen eintragen. |
| Verbindung bricht mit Zertifikatsfehler ab | Das Zertifikat des NAS hat sich geändert - etwa weil ein Let's-Encrypt-Zertifikat erneuert wurde. Nimm das Gerät erneut auf und bestätige den neuen Fingerprint. |
| „Zugriff verweigert" bei Abfragen | Das Konto ist nicht in der Gruppe **administrators**. Die Zustandsabfragen von DSM setzen das voraus. |

## Was LCM hier zeigt

DSM-Version und ob eine Aktualisierung bereitsteht, dazu Speicherbelegung und
Volume-Zustand. Die Ampel stützt sich auf diese beiden Punkte: ein ausstehendes
DSM-Update oder eine volllaufende Platte fällt auf. Das Einspielen von
DSM-Updates bleibt bei dir - LCM macht darauf aufmerksam.
