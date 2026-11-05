# Linux-Benutzer einrichten

Diese Seite beschreibt, was ein Linux-Benutzer auf den von LCM verwalteten
Servern braucht und wie er es selbst einstellt. Sie richtet sich an zwei
Personen: an die Verwaltung, die den Benutzer in LCM anlegt, und an den
Benutzer selbst.

> LCM verteilt den Benutzer auf die Server. Das **Passwort legt der Benutzer
> selbst fest**, über einen Aktivierungslink. Ohne Passwort kommt er per
> SSH-Schlüssel herein, aber **nicht** über eine Anmeldung mit Passwort, etwa
> in der Proxmox-Web-Oberfläche oder am Konsolen-Login des Servers.

## Wann ein Passwort gebraucht wird

- **SSH mit Schlüssel**: kein Passwort nötig.
- **Anmeldung mit Passwort**, zum Beispiel in der Web-Oberfläche von Proxmox
  oder am Konsolen-Login, sowie `sudo`, wenn das Berechtigungsprofil ein
  Passwort verlangt.

Ein Konto ohne Passwort ist gesperrt. Das ist Absicht: Ein Konto ohne Passwort
soll nicht offen stehen.

## Passwort festlegen

1. Die Verwaltung schickt einen **Aktivierungslink** per E-Mail oder gibt ihn
   weiter. Der Link ist standardmäßig 48 Stunden gültig.
2. Den Link öffnen. Dafür ist keine Anmeldung bei LCM nötig.
3. Passwort eingeben und wiederholen. Die Passwort-Richtlinie gilt auch hier;
   die Stärke-Anzeige zeigt, wann das Passwort reicht.
4. Absenden. LCM verteilt das Passwort **sofort** auf alle Server, auf denen
   der Benutzer eingerichtet ist.

Wer sein Passwort ändern will, fordert dafür einen neuen Aktivierungslink an.
Ein Passwort, das man direkt am Server mit `passwd` setzt, bleibt erhalten,
solange in LCM **kein** Passwort hinterlegt ist. Hat LCM eines, überschreibt
der nächste Abgleich das Server-Passwort wieder mit dem aus LCM.

## Angaben zum Benutzer

Die Verwaltung pflegt die Angaben in LCM unter **Linux-Benutzer**. Sie werden
bei jedem Abgleich auf die Server übertragen:

| Angabe | Wirkung auf dem Server | Wo in LCM |
|---|---|---|
| **Benutzername** | Name des Kontos. Nach dem Anlegen nicht mehr änderbar. | Anlegen |
| **Voller Name** | Kommentarfeld des Kontos (`chfn`), sichtbar in `getent passwd` | Bearbeiten |
| **E-Mail** | Nur für den Aktivierungslink. Sie wird **nicht** auf den Server geschrieben. | Bearbeiten |
| **Shell** | Anmelde-Shell, zum Beispiel `/bin/bash` | Bearbeiten |
| **SSH-Schlüssel** | Öffentliche Schlüssel, je Konto beliebig viele | Schlüssel |
| **Berechtigungsprofil** | `sudo`-Rechte und Verzeichnisrechte | Zuweisen |

Braucht jemand auf dem Server eine E-Mail-Adresse, etwa für
`git config user.email` oder für eine Weiterleitung, trägt er sie dort selbst
ein. LCM schreibt sie nicht auf den Server.

### Was der Abgleich überschreibt

LCM ist die Quelle der Wahrheit für diese Angaben. Ändert jemand sie direkt am
Server, stellt der nächste Abgleich den Stand aus LCM wieder her:

- Passwort, wenn in LCM eines hinterlegt ist
- Shell
- voller Name, wenn in LCM einer hinterlegt ist; ist keiner eingetragen,
  bleibt der Wert am Server unberührt
- SSH-Schlüssel, die LCM verwaltet
- Sperre und Ablauf des Kontos

Soll eine Änderung bleiben, pflegt man sie in LCM.

## Sperren und Entfernen

Im Reiter **Benutzer** eines Servers sperrt **Sperren** das Konto auf diesem
Server; **Entsperren** hebt die Sperre wieder auf. **Entfernen** löscht das
Konto samt Home-Verzeichnis. Die Sperre bleibt über den nächsten Abgleich
erhalten. Mehr dazu auf der Seite [SSH-Schlüssel](/docs/ssh-schluessel) und in
der Anleitung zu den Servern.

## Selbst einrichten: Passwort und Schlüssel

Der Benutzer kann auf der Aktivierungsseite beides auf einmal setzen: das
Passwort und einen SSH-Schlüssel. Den Schlüssel kann LCM auch für ihn erzeugen;
den privaten Teil lädt er dann einmalig herunter. Die Anleitung zum eigenen
Schlüssel steht auf der Seite [SSH-Schlüssel einrichten](/docs/ssh-schluessel).
