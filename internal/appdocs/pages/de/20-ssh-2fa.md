# Zwei-Faktor-Anmeldung für SSH einrichten

Ist auf einem Server die **SSH-2FA** aktiviert, verlangt die Anmeldung zwei
Dinge: deinen **SSH-Schlüssel** und einen **Einmalcode** aus einer
Authenticator-App. Der Code wechselt alle 30 Sekunden und steht nur auf deinem
Gerät - wer deinen Schlüssel kopiert, kommt damit trotzdem nicht herein.

Diesen zweiten Faktor richtest du **selbst auf dem Server** ein. Er ist an dein
Konto gebunden, nicht an den Rechner, von dem aus du dich verbindest.

> Solange du noch nichts eingerichtet hast, kommst du wie bisher allein mit
> deinem Schlüssel herein. Die Einrichtung sperrt dich also nicht aus - sie
> schaltet den zweiten Faktor für dich frei.

## Was du brauchst

- Eine **Authenticator-App** auf dem Handy. Es funktioniert jede App, die
  zeitbasierte Einmalcodes (TOTP) beherrscht - etwa Google Authenticator, Aegis,
  2FAS, Microsoft Authenticator oder ein Passwortmanager mit TOTP-Funktion.
- Einen funktionierenden **SSH-Zugang** zum Server (siehe
  [SSH-Schlüssel einrichten](/#/doku/ssh-schluessel)).

## Schritt für Schritt

1. **Auf dem Server anmelden** - wie gewohnt:

   ```bash
   ssh benutzer@server
   ```

2. **Einrichtung starten:**

   ```bash
   google-authenticator
   ```

3. Die erste Frage lautet, ob die Codes **zeitbasiert** sein sollen. Antworte
   mit **y** - das ist das übliche TOTP-Verfahren, das jede App versteht.

4. Es erscheint ein **QR-Code** im Terminal. Scanne ihn mit deiner App. Ist das
   Fenster zu klein und der Code zerfranst, hilft der darunter angezeigte
   `secret key`: Den kannst du in der App von Hand eintippen.

5. **Notfallcodes sichern.** Direkt darunter stehen fünf `emergency scratch
   codes`. Jeder funktioniert genau einmal und rettet dich, wenn das Handy weg
   ist. Schreib sie an einen sicheren Ort - nicht auf denselben Server und
   nicht in dieselbe App.

6. Die restlichen vier Fragen beantwortest du am besten so:

   | Frage (sinngemäß) | Antwort | Warum |
   |---|:-:|---|
   | Einstellungen in `~/.google_authenticator` speichern? | **y** | Ohne Speichern war alles umsonst. |
   | Denselben Code mehrfach zulassen? | **n** | Ein abgefangener Code wäre sonst wiederverwendbar. |
   | Zeitfenster vergrößern (bei ungenauer Uhr)? | **n** | Nur nötig, wenn Codes ständig abgelehnt werden. |
   | Anmeldeversuche begrenzen? | **y** | Bremst automatisiertes Durchprobieren aus. |

Wer es lieber in einem Rutsch hat, bekommt dieselbe Einrichtung ohne Rückfragen:

```bash
google-authenticator -t -d -f -r 3 -R 30 -w 3
```

## Vorher prüfen, nicht nachher

**Schließe die aktuelle Sitzung noch nicht.** Öffne stattdessen ein **zweites**
Terminalfenster und melde dich dort neu an. Erst wenn das klappt, ist die
Einrichtung wirklich in Ordnung. Sperrst du dich aus, hast du in der noch
offenen ersten Sitzung die Möglichkeit, es zurückzunehmen:

```bash
rm ~/.google_authenticator
```

So sieht die Anmeldung danach aus:

```
Verification code:
```

Dort gibst du den sechsstelligen Code aus der App ein - dein Schlüssel wird
weiterhin zusätzlich geprüft.

## Wenn es klemmt

| Problem | Ursache und Abhilfe |
|---|---|
| Es wird gar kein Code verlangt | Auf diesem Server ist 2FA nicht aktiviert, oder du hast noch nichts eingerichtet. Beides ist kein Fehler. |
| Der Code wird abgelehnt | Meist geht die Uhr auseinander. Prüfe die Uhrzeit auf dem Handy (automatische Zeit einschalten). Hilft das nicht, richte neu ein und lass beim Zeitfenster **y** zu. |
| Handy verloren | Melde dich mit einem **Notfallcode** an und richte anschließend neu ein. Ohne Notfallcode hilft nur die Administration weiter. |
| „Permission denied" direkt nach dem Schlüssel | Der zweite Faktor scheiterte. Achte auf die Zeile `Verification code:` - bleibt sie aus, stimmt etwas an der Server-Konfiguration nicht. |

> Dein Einmalcode-Geheimnis liegt in `~/.google_authenticator` auf dem Server.
> Es gilt nur für dieses Konto auf diesem Server - auf einem weiteren Server
> richtest du 2FA erneut ein.
