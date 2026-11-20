# Mehr Funktionen für Agent-Server

Stand 19.11.2026. Tonys Auftrag: Server, die per Agent verbunden sind, sollen mehr können als
heute, vor allem ein Terminal über die Agent-Verbindung und die SSH-Härtung.

Der Agent läuft als root und führt beliebige Befehle aus (`internal/agent/runner.go`). Es fehlt
also kein Recht, sondern der Weg: Der Agent kennt nur „Befehl hin, ganze Ausgabe zurück“, ohne
Datenstrom und ohne PTY. Die SSH-Funktionen sind bisher grundsätzlich gesperrt
(`ensureSSHTransport`), meist ohne technischen Grund.

Entscheidungen von Tony (19.11.2026):

- Reihenfolge: erst die SSH-Funktionen (Teil A, eigener MR), dann das Terminal (Teil B).
- Terminal als root-Shell.
- Der Betreiber kann das Terminal am Agent lokal abschalten; standardmäßig ist es an.
- Freigegeben werden SSH-Härtung, Root-Login, SSH-Port, offene Ports und SSH-2FA.

## Teil A: SSH-Funktionen für Agent-Server

Läuft mit jedem vorhandenen Agent, kein neues Agent-Paket nötig.

| Paket | Inhalt | Stand |
|---|---|---|
| A1 SSH-Härtung | `planHardenSSH`/`planUnhardenSSH` ohne Sperre; beim Agent bricht die Härtung ab, wenn auf dem Server kein SSH-Schlüssel liegt; Knopf und Fakt in der Oberfläche auch für Agent-Server | zu prüfen |
| A2 Root-Login | ohne Sperre; der gespeicherte Port eines Agent-Servers ist die Spaltenvorgabe 22, die keine Port-Zeile ergibt | zu prüfen |
| A3 SSH-Port | Agent: aktueller Port aus `sshd -T`, neuer Port muss frei sein, danach lauscht er und `sshd -T` nennt ihn (statt `DialKey`) | zu prüfen |
| A4 Offene Ports | Liste der lauschenden Ports auch über den Agent | zu prüfen |
| A5 SSH-2FA | Bei Agent-Servern ohne `Match User`-Ausnahme; statt der Aussperr-Probe prüft `sshd -T`, dass der Einmalcode verlangt wird, sonst Rollback | zu prüfen |
| A6 Ungereimtheiten | „sudo einschränken“ für Agent-Server ausblenden (Agent ist root; Backend lehnt ab) | zu prüfen |

Bleibt gesperrt, weil es an der SSH-Verbindung von LCM hängt: Schlüsselrotation, Neu-Verbinden,
Aufräumen beim Löschen über SSH, Portscan von außen.

## Teil B: Terminal über den Agent

Braucht ein neues `lcm-agent`-Paket.

| Paket | Inhalt | Stand |
|---|---|---|
| B1 Fähigkeiten | `Inventory.Capabilities` (`tty` bzw. `tty-off`), gespeichert in `agent_capabilities`; `terminalPossible` und die Oberfläche fragen sie ab, alte Agents zeigen „Agent aktualisieren“ | zu prüfen |
| B2 Protokoll | Topics `tty`/`ttyo` mit `TTYFrame` (Sitzung, Nummer, Art), `Sequencer` setzt in Reihe und verwirft Doppel; ACL erweitert; Öffnen wird wiederholt, bis der Agent antwortet | zu prüfen |
| B3 Agent-PTY | Login-Shell als root im PTY über `/dev/ptmx` (`x/sys`, keine neue Abhängigkeit), außerhalb der Job-Sperre, höchstens zwei Sitzungen, Ende nach 30 min ohne Eingabe und bei Verbindungsabbruch | zu prüfen |
| B4 Lokaler Schalter | Datei `/etc/lcm-agent/terminal-off` (eigene Datei, damit das Speichern gelernter Pins sie nicht überschreibt; im Zweifel aus), `lcm-agent terminal on|off|status`; gilt sofort, auch für offene Konsolen; LCM sieht es nach dem nächsten Verbindungsaufbau | zu prüfen |
| B5 Server-Seite | `agentConn` erfüllt `sshx.TerminalConn`; Ticket, `servers:console`, Schalter, Konsolen-Limit, Audit und Mitschnitt unverändert | zu prüfen |
| B6 Doku | `docs/guides/remote.md` (DE/EN): Konsole auf Agent-Servern, lokaler Schalter | zu prüfen |
