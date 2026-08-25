# MikroTik-Router einbinden

LCM kann MikroTik-Geräte mit **RouterOS** überwachen: Modell, RouterOS-Version
und ob eine neuere Fassung verfügbar ist. Der Zugriff ist **rein lesend** -
LCM verändert an einem Router nichts.

> RouterOS ist kein Linux mit Paketverwaltung. Pakete, Docker, Benutzer-Sync,
> Härtung und CVE-Bewertung gibt es hier deshalb nicht - sie sind für diese
> Geräte ausgeblendet, statt ins Leere zu laufen.

## Schritt 1 - Einen Benutzer für LCM anlegen

LCM braucht auf dem Router **nur Leserechte**. Lege dafür einen eigenen
Benutzer der eingebauten Gruppe `read` an, statt einen Admin-Zugang zu teilen:

```
/user add name=lcm group=read password=<starkes-passwort>
```

Damit kann LCM ausschließlich Werte abfragen - Konfigurationsänderungen sind
diesem Benutzer auf dem Router selbst verwehrt.

## Schritt 2 - Gerät in LCM aufnehmen

In LCM auf **„+ Server hinzufügen"** und oben den Modus **MikroTik RouterOS**
wählen. Nötig sind:

- **Name** für die Übersicht
- **Host/IP** und **SSH-Port** (in aller Regel 22)
- der eben angelegte **Benutzer**

Danach zeigt LCM den **Fingerprint des SSH-Host-Keys**. Vergleiche ihn mit dem
Router und bestätige - LCM merkt sich diesen Wert und bricht künftig ab, falls
er sich ändert. Das schützt davor, dass sich jemand unbemerkt dazwischenschaltet.

## Schritt 3 - Anmeldung wählen

**Mit Passwort:** LCM verbindet sich sofort, liest Version und Gerätedaten und
nimmt den Router **online** auf. Das Passwort wird verschlüsselt gespeichert.

**Mit Schlüssel (empfohlen):** LCM erzeugt ein Schlüsselpaar und zeigt den
öffentlichen Teil an. Das Gerät bleibt zunächst **offline**, bis du den
Schlüssel auf dem Router hinterlegst:

1. Den angezeigten Public Key als Datei auf den Router laden - etwa über
   *Files* in WinBox oder per `scp` als `lcm.pub`.
2. Auf dem Router importieren:

   ```
   /user ssh-keys import public-key-file=lcm.pub user=lcm
   ```

Beim nächsten Aktualisieren verbindet sich LCM und das Gerät geht online.

## Wenn es klemmt

| Problem | Ursache und Abhilfe |
|---|---|
| „Kein RouterOS erkannt" | LCM prüft beim Aufnehmen, ob wirklich RouterOS antwortet. Meist zeigt der Host auf ein anderes Gerät, oder der SSH-Dienst am Router ist abgeschaltet (*IP → Services → ssh*). |
| Gerät bleibt nach dem Schlüssel-Import offline | Der Import muss auf **denselben Benutzer** zeigen, mit dem LCM sich anmeldet (`user=` im Import-Befehl). |
| Anmeldung wird abgelehnt | Prüfe, ob der Benutzer auf dem Router aktiv ist und der SSH-Dienst nicht auf bestimmte Adressen eingeschränkt ist (*IP → Services → ssh*, Feld *Available From*). |

## Was LCM hier zeigt

Modell, RouterOS-Version, verfügbare Aktualisierung und die Erreichbarkeit. Die
Ampel bewertet entsprechend: Ein Gerät mit ausstehendem RouterOS-Update oder
ohne Kontakt fällt auf. Aktualisieren tust du den Router weiterhin selbst -
LCM sagt dir nur, dass es ansteht.
