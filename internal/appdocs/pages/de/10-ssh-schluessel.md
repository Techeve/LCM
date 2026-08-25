# SSH-Schlüssel einrichten

Damit du dich an den von LCM verwalteten Servern anmelden kannst, brauchst du
ein **Schlüsselpaar**: einen privaten Schlüssel, der bei dir bleibt, und einen
öffentlichen, den LCM auf die Server verteilt.

Es gibt zwei Wege - beide führen zum Ziel:

- **LCM erzeugt den Schlüssel für dich.** Du lädst den privaten Schlüssel
  einmalig herunter. Bequem, aber der Schlüssel war kurz unterwegs.
- **Du erzeugst ihn selbst** und gibst LCM nur den öffentlichen Teil. Der
  private Schlüssel verlässt deinen Rechner nie. Das ist der sicherere Weg.

> Der private Schlüssel ist wie ein Hausschlüssel: Er gehört nur dir. Gib ihn
> niemals weiter - auch nicht an Kollegen oder den Support.

## Linux und macOS

### Eigenen Schlüssel erzeugen

```bash
ssh-keygen -t ed25519 -C "vorname.nachname@firma.de"
```

Die Nachfrage nach dem Speicherort kannst du mit Enter bestätigen. Vergib eine
**Passphrase** - sie schützt den Schlüssel, falls dein Rechner abhandenkommt.

Danach liegen zwei Dateien in `~/.ssh/`:

| Datei | Inhalt | Weitergeben? |
|---|---|---|
| `id_ed25519` | privater Schlüssel | **niemals** |
| `id_ed25519.pub` | öffentlicher Schlüssel | ja, in LCM eintragen |

Den Inhalt der `.pub`-Datei anzeigen und vollständig in LCM einfügen:

```bash
cat ~/.ssh/id_ed25519.pub
```

### Von LCM erzeugten Schlüssel ablegen

Hat LCM den Schlüssel für dich erzeugt, speichere die heruntergeladene Datei
unter `~/.ssh/` und setze die Rechte - ohne das verweigert OpenSSH den Dienst:

```bash
mv ~/Downloads/id_ed25519_BENUTZER ~/.ssh/
chmod 600 ~/.ssh/id_ed25519_BENUTZER
```

### Verbinden

```bash
ssh -i ~/.ssh/id_ed25519_BENUTZER benutzer@server
```

Damit du den Schlüssel nicht jedes Mal angeben musst, trag den Server in
`~/.ssh/config` ein:

```
Host meinserver
  HostName 10.0.0.5
  User benutzer
  IdentityFile ~/.ssh/id_ed25519_BENUTZER
```

Danach genügt `ssh meinserver`.

## Windows mit OpenSSH

Windows 10 und 11 bringen OpenSSH bereits mit - du brauchst kein Zusatzprogramm.
Öffne die **PowerShell**.

### Eigenen Schlüssel erzeugen

```powershell
ssh-keygen -t ed25519 -C "vorname.nachname@firma.de"
```

Die Dateien landen in `C:\Users\<name>\.ssh\`. Den öffentlichen Schlüssel
anzeigen und in LCM eintragen:

```powershell
type $env:USERPROFILE\.ssh\id_ed25519.pub
```

### Von LCM erzeugten Schlüssel ablegen

Verschiebe die heruntergeladene Datei nach `C:\Users\<name>\.ssh\`. Anders als
unter Linux zählen hier nicht die Dateirechte, sondern der **Besitzer**: Die
Datei muss dir gehören und sonst niemandem. Falls sich OpenSSH über zu offene
Rechte beschwert, hilft in der PowerShell:

```powershell
icacls $env:USERPROFILE\.ssh\id_ed25519_BENUTZER /inheritance:r /grant:r "$($env:USERNAME):(R)"
```

### Als Standard einrichten

Lege die Datei `C:\Users\<name>\.ssh\config` an (ohne Dateiendung):

```
Host meinserver
  HostName 10.0.0.5
  User benutzer
  IdentityFile ~/.ssh/id_ed25519_BENUTZER
```

Soll ein Schlüssel für **alle** Verbindungen gelten, nimm `Host *`:

```
Host *
  IdentityFile ~/.ssh/id_ed25519_BENUTZER
```

Damit du die Passphrase nur einmal je Sitzung eingeben musst, starte den
Agenten und lade den Schlüssel hinein:

```powershell
Set-Service ssh-agent -StartupType Automatic
Start-Service ssh-agent
ssh-add $env:USERPROFILE\.ssh\id_ed25519_BENUTZER
```

## Windows mit PuTTY

PuTTY nutzt ein **eigenes Schlüsselformat** (`.ppk`), das OpenSSH und damit
auch LCM nicht direkt versteht. Das ist kein Hindernis - du musst nur wissen,
welchen Teil du wohin gibst.

### Vorhandenen PPK-Schlüssel mit LCM nutzen

LCM braucht ausschließlich den **öffentlichen** Teil. Den holst du aus deinem
vorhandenen Schlüssel heraus, ohne etwas neu zu erzeugen:

1. **PuTTYgen** starten (kommt mit PuTTY mit).
2. *Load* anklicken, deine `.ppk`-Datei auswählen, Passphrase eingeben.
3. Oben im Feld **„Public key for pasting into OpenSSH authorized_keys file"**
   steht dein öffentlicher Schlüssel im richtigen Format. Diesen Text
   **vollständig** markieren und kopieren - er beginnt mit `ssh-ed25519` oder
   `ssh-rsa` und ist eine einzige lange Zeile.
4. In LCM unter deinem Benutzer als SSH-Schlüssel einfügen.

> Nimm **nicht** den Inhalt der `.ppk`-Datei und auch nicht das, was *Save
> public key* schreibt: Das ist das PuTTY-Format mit mehreren Zeilen und
> `---- BEGIN SSH2 PUBLIC KEY ----` am Anfang. Damit kann LCM nichts anfangen.

### Als Standard einrichten

Damit PuTTY den Schlüssel von selbst verwendet, gibt es zwei Wege:

**Weg 1 - Pageant (der Schlüsselagent von PuTTY).** Er hält den Schlüssel
entsperrt vor, sodass du die Passphrase nur einmal eingibst:

1. `pageant.exe` starten, dann *Add Key* und deine `.ppk` auswählen.
2. Für den Autostart eine Verknüpfung zu `pageant.exe` in den Autostart-Ordner
   legen (`Win`+`R`, dann `shell:startup`) und im Ziel den Schlüsselpfad
   anhängen:

```
"C:\Program Files\PuTTY\pageant.exe" "C:\Users\<name>\.ssh\meinschluessel.ppk"
```

**Weg 2 - fest in der PuTTY-Sitzung.** In PuTTY unter
*Connection → SSH → Auth → Credentials* die `.ppk` als *Private key file*
eintragen, dann zurück auf *Session*, einen Namen vergeben und **Save**
klicken. Beim nächsten Start die gespeicherte Sitzung laden.

### PPK für OpenSSH umwandeln

Willst du denselben Schlüssel auch in PowerShell, VS Code oder Git benutzen,
wandle ihn einmalig um:

1. In **PuTTYgen** die `.ppk` per *Load* öffnen.
2. Menü *Conversions → Export OpenSSH key*.
3. Als `C:\Users\<name>\.ssh\id_ed25519` speichern (ohne Dateiendung).

Danach gilt für diesen Schlüssel alles aus dem Abschnitt
[Windows mit OpenSSH](#windows-mit-openssh).

Ist auf einem Server zusätzlich die Zwei-Faktor-Anmeldung aktiv, brauchst du
neben dem Schlüssel einen Einmalcode - wie du den einrichtest, steht unter
[Zwei-Faktor-Anmeldung für SSH einrichten](/#/doku/ssh-2fa).

## Wenn es klemmt

| Meldung | Ursache |
|---|---|
| `Permission denied (publickey)` | Der öffentliche Schlüssel ist noch nicht auf dem Server - ist er in LCM eingetragen und der Benutzer dem Server zugeordnet? |
| `UNPROTECTED PRIVATE KEY FILE` | Die Rechte sind zu offen: `chmod 600` (Linux/macOS) bzw. Besitzer richtigstellen (Windows). |
| `Invalid format` / `error in libcrypto` | Vermutlich wurde eine `.ppk` oder ein PuTTY-Public-Key eingetragen. Nimm den Text aus dem OpenSSH-Feld in PuTTYgen. |
| PuTTY fragt nach einem Passwort statt nach der Passphrase | PuTTY kennt den Schlüssel nicht - Sitzung ohne *Private key file* gespeichert oder Pageant läuft nicht. |
