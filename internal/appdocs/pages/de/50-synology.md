# Synology-NAS einbinden

LCM überwacht Synology-Geräte über die **DSM-Web-API** - nicht über SSH. Das ist
Absicht: SSH ist auf einem NAS ab Werk abgeschaltet und sollte es auch bleiben.
Die API liefert alles Nötige, ohne einen zusätzlichen Zugangsweg zu öffnen.

> Erfasst werden DSM-Version, verfügbare Aktualisierungen, installierte Pakete,
> Speicherpools, Volumes und Laufwerke, Firewall und Sicherheitseinstellungen.
> Paketverwaltung, Docker-Inventar, Benutzer-Sync und CVE-Bewertung gibt es hier
> nicht - DSM ist kein gewöhnliches Linux.

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

LCM liest die Werte und vergleicht sie mit den Sollwerten. Jeder Befund nennt
den Weg in der DSM-Oberfläche, wo du ihn behebst. Die Sicherheitseinstellungen
setzt LCM auf Wunsch auch selbst auf den Sollwert, aber nur auf deinen Klick.

| Bereich | Woher | Was daraus wird |
|---|---|---|
| System | `SYNO.Core.System` | Modell, DSM-Version, CPU, Arbeitsspeicher, Zeitzone und Zeitdienst |
| Updates | `SYNO.Core.Upgrade.Server` | Befund, wenn eine neuere DSM-Version bereitsteht |
| Pakete | `SYNO.Core.Package` | Reiter **Pakete**; einen eigenen Reiter Apps gibt es bei DSM nicht |
| Speicher | `SYNO.Storage.CGI.Storage` | Reiter **Speicher**: jedes Volume einzeln, Speicherpools und Laufwerke mit Zustand |
| Security Advisor | `SYNO.Core.SecurityScan.Status` | Befunde der Stufen Risiko und Gefahr |
| Firewall und Sicherheit | `SYNO.Core.Security.*` und weitere | Abgleich mit den Sollwerten unten |

### Speicher

Die Volumes stehen mit ihrem Pfad (`/volume1`) unter **Speicher**. Dort lässt
sich jedes einzeln überwachen, mit eigener Warnschwelle - wie ein Laufwerk
eines Linux-Servers. Die Summe aller Volumes geht in den Speicherverlauf.

Speicherpools, Volumes und Laufwerke werden immer bewertet:

| Zustand in DSM | Ampel |
|---|---|
| Laufwerk ausgefallen, SMART meldet Ausfall, Pool oder Volume abgestürzt, Volume nur lesbar | rot |
| Pool ohne volle Redundanz (degradiert), laufende Reparatur, SMART-Warnung, zu viele defekte Sektoren, Restlebensdauer unter der Schwelle | gelb |

Ein virtualisiertes DSM (VirtualDSM) meldet weder SMART noch Temperatur. Was
fehlt, ist kein Befund. Die CPU heißt dort „virtuell", mit der Zahl der vCPUs.

### Sicherheitseinstellungen

| Einstellung | Soll | In DSM |
|---|---|---|
| Firewall | an, und das aktive Profil verweigert, was keine Regel erlaubt: am Ende eine Regel „alles verweigern“ oder „Wenn keine Regel zutrifft: Zugriff verweigern“ | Systemsteuerung → Sicherheit → Firewall |
| Browser-Kompatibilität durch Verzicht auf IP-Prüfung | aus | Systemsteuerung → Sicherheit → Sicherheit |
| Schutz vor Cross-Site-Request-Forgery (CSRF) | an | Systemsteuerung → Sicherheit → Sicherheit |
| HTTP Content Security Policy | an | Systemsteuerung → Sicherheit → Sicherheit |
| Einbetten in einen iFrame verhindern | an | Systemsteuerung → Sicherheit → Sicherheit |
| Sitzungen beim Neustart löschen | an | Systemsteuerung → Sicherheit → Sicherheit |
| Automatisch blockieren | an, höchstens 10 Versuche in 5 Minuten | Systemsteuerung → Sicherheit → Schutz |
| DoS-Schutz | an, wenn das Gerät aus dem Internet erreichbar ist | Systemsteuerung → Sicherheit → Schutz |
| TLS/SSL-Profilstufe | moderne Kompatibilität | Systemsteuerung → Sicherheit → Erweitert |
| Schutz vor Spectre und Meltdown | an | Systemsteuerung → Sicherheit → Erweitert |

Jede Abweichung ist ein gelber Befund. Nur der DoS-Schutz ist ein Hinweis ohne
Ampel: LCM kann nicht prüfen, ob dein NAS aus dem Internet erreichbar ist. Steht
es dort, schalte ihn ein.

Die TLS/SSL-Profilstufe meldet DSM 7.3 nur als Nummer. Am Gerät abgelesen
sind Stufe 1 für „moderne Kompatibilität“ und Stufe 3 für „alte
Kompatibilität“. Stufe 2 gilt als „mittlere Kompatibilität“, weil DSM nur drei
Stufen kennt. Eine andere Nummer zeigt LCM an, wertet sie aber nicht.

Synology dokumentiert diese Schnittstellen nicht, und die Feldnamen wechseln
mit der DSM-Version. Kann LCM einen Wert nicht deuten, gilt er als
**unbekannt** und färbt die Ampel nicht. Im Job-Protokoll der Erfassung
(Reiter **Jobs**) steht dann der Grund und die Antwort von DSM, ohne Passwörter
und Sitzungsdaten. Mit dieser Antwort lässt sich die Schreibweise deiner
DSM-Version in LCM nachtragen.

### Sollwerte setzen

Im Reiter **Sicherheit** steht an jeder Abweichung der Knopf **Auf Sollwert
setzen**. **Alle Abweichungen beheben** nimmt alle auf einmal. Vorher zeigt ein
Dialog, was sich ändert. Das Setzen läuft als Job, danach liest LCM das Gerät
neu ein. Dafür braucht es das Recht `servers:write`.

LCM liest dazu die Einstellung, ändert nur den Zielwert und schickt alles
Gelesene an DSM zurück. Alle anderen Werte bleiben, wie sie sind. Das
Job-Protokoll nennt je Einstellung den Stand vorher, was gesendet wurde und
den Stand danach.

Bei drei Einstellungen warnt der Dialog vorher:

| Einstellung | Hinweis |
|---|---|
| DoS-Schutz | Nur sinnvoll, wenn das Gerät aus dem Internet erreichbar ist. |
| TLS/SSL-Profilstufe | Ältere Geräte und Apps können die Verbindung verlieren. |
| Schutz vor Spectre und Meltdown | Wirkt erst nach einem Neustart des Geräts. |

Die **Firewall** schaltet LCM nicht. Mit Regeln eingeschaltet, die nicht
passen, sperrt sie LCM und dich aus. Die Regeln bleiben Sache von DSM. Prüfe
sie unter *Systemsteuerung → Sicherheit → Firewall* und schalte sie dort ein.

Im Reiter **Sicherheit** listet LCM die Regeln des aktiven Profils, je
Schnittstelle mit Aktion, Protokoll, Ports und Quelle. Darunter steht, was gilt,
wenn keine Regel zutrifft. Abgeschaltete Regeln sind als „aus“ markiert und
zählen nicht. Maßgeblich sind die Regeln für alle Schnittstellen. Regeln an
einem einzelnen Adapter schützen die übrigen nicht.

### DSM-Oberfläche

Der Knopf **DSM öffnen** in der Kopfleiste des Geräts öffnet die
Anmeldeseite von DSM in einem neuen Tab, unter Adresse und Port aus LCM.

### Updates

Das Einspielen von DSM-Updates bleibt bei dir - LCM macht darauf aufmerksam.
