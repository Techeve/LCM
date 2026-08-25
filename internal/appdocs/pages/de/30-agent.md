# LCM-Agent installieren

Normalerweise verbindet sich LCM per **SSH** zu einem Server. Für Maschinen, die
von außen nicht erreichbar sind - hinter NAT, in fremden Netzen, im Homeoffice -
gibt es den **Agent**: ein kleiner Dienst, der auf dem Server läuft und die
Verbindung **von sich aus nach außen** aufbaut. Am Server muss dafür kein Port
geöffnet werden.

> Für welchen Weg du dich entscheidest, hängt an der Erreichbarkeit, nicht am
> Funktionsumfang: Scans, Paket-Updates und Aktionen gibt es über beide Wege.

## Bevor du anfängst

Der Agent meldet sich an einem **eigenen Port** des LCM-Servers an - nicht an
dem der Oberfläche. Beides ist bewusst getrennt: Der Oberflächen-Port bietet
keine Agent-Schnittstelle und umgekehrt. Wie die Adresse lautet, zeigt dir LCM
im nächsten Schritt an; sie sieht etwa so aus:

```
https://lcm.example.de:9320
```

Nach außen erreichbar sein muss also **der LCM-Server**, nicht der zu
verwaltende Server. Steht der LCM-Server hinter einer Firewall, muss dieser
eine Port dorthin durchgereicht werden.

## Schritt 1 - Server in LCM anlegen

In LCM auf **„+ Server hinzufügen"**, oben den Modus **Agent** wählen und einen
**Namen** vergeben. Mehr braucht es nicht.

LCM legt den Server zunächst **offline** an und erzeugt ein
**Enrollment-Token**. Auf der folgenden Seite stehen die fertigen Befehle für
den Zielserver - mit Adresse und Token bereits eingesetzt.

> Das Token wird **nur ein einziges Mal** angezeigt. Danach ist es nicht mehr
> abrufbar, weil LCM nur seinen Hashwert speichert. Kopiere es gleich; geht es
> verloren, erzeugst du über **„Token neu erzeugen"** einfach ein neues.

## Schritt 2 - Agent auf dem Server installieren

Melde dich auf dem zu verwaltenden Server an. Es gibt zwei Wege - den
angezeigten Befehlen kannst du folgen, hier stehen sie zur Einordnung:

**Mit Paketquelle (empfohlen)** - der Agent bekommt damit Updates wie jedes
andere Paket:

```bash
# einmalig die TechEve-Paketquelle einrichten (entfällt, wenn schon vorhanden)
curl -fsSL https://repo.techeve.de/setup.sh | sudo sh
sudo apt install lcm-agent
```

**Ohne Paketquelle** - das Binary direkt vom LCM-Server laden. Praktisch für
einen schnellen Test, aber Updates musst du dann selbst einspielen.

## Schritt 3 - Agent anmelden

```bash
sudo lcm-agent enroll https://lcm.example.de:9320 <token>
```

Das braucht **root**, weil der Agent als Systemdienst eingerichtet wird. Der
Befehl prüft zuerst die Verbindung und legt erst danach die Konfiguration an -
ein falsches Token oder eine unerreichbare Adresse fällt also sofort auf und
nicht erst später im Dienstprotokoll.

Danach verbindet sich der Agent von selbst, der Server geht in LCM **online**,
und der erste Systemscan startet automatisch.

## Prüfen, ob es läuft

Auf dem Server:

```bash
systemctl status lcm-agent
```

In LCM: Der Server steht auf **online** und zeigt nach dem ersten Scan
Betriebssystem, Pakete und Hardware.

## Wenn es klemmt

| Problem | Ursache und Abhilfe |
|---|---|
| `enroll` meldet, die Verbindung schlage fehl | Adresse oder Port stimmen nicht - es ist der **Agent-Port**, nicht der der Oberfläche. Prüfe von diesem Server aus, ob der LCM-Server erreichbar ist. |
| `enroll benötigt root` | Mit `sudo` ausführen. |
| Server bleibt offline | `systemctl status lcm-agent` und `journalctl -u lcm-agent -n 50` zeigen die Ursache. Häufig: Token abgetippt statt kopiert. |
| Token verloren | In LCM beim Server **„Token neu erzeugen"**. Das alte wird sofort ungültig, der Agent muss neu angemeldet werden. |

## Was am Agent-Server anders ist

Der Agent läuft als Root-Dienst - einen Service-Benutzer wie beim SSH-Weg gibt
es nicht. Entsprechend sind die SSH-spezifischen Funktionen (SSH-Härtung,
Schlüsselwechsel, Neu verbinden) bei diesen Servern ausgeblendet. Alles andere
- Scans, Updates, Aktionen, Zeitpläne - funktioniert wie gewohnt.

Zum Entfernen: In LCM den Server löschen und auf dem Zielsystem

```bash
sudo lcm-agent uninstall
```
