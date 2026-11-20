# Offene Ports prüfen

Auf dem Reiter **Sicherheit** eines Servers zeigt die Karte **Offene Ports**,
welche Dienste der Server ins Netz anbietet - so, wie LCM ihn sieht. Damit
erkennst du Dienste, die du freigeben, abschalten oder in der Firewall sperren
solltest.

## Was LCM prüft

- **Nur TCP, nur gängige Ports:** SSH, HTTP/HTTPS, DSM (5000/5001), SMB, NFS,
  rpcbind, FTP, Telnet, Datenbanken (MySQL, PostgreSQL, Redis, MongoDB),
  Docker (2375/2376), Kubernetes, Proxmox, RouterOS, MQTT, LDAP, RDP und VNC,
  dazu der SSH-Port des Servers. UDP-Dienste wie SNMP oder DNS fehlen, ein
  Verbindungsaufbau sagt dort nichts.
- **Nur die Adresse des Servers:** IPv4 und IPv6 aus der Host-Angabe; meldet
  der Server selbst eine IPv6-Adresse, wird auch sie geprüft. Eine andere
  Adresse lässt sich nicht angeben.
- **Langsam:** höchstens vier Verbindungen zugleich, zwei Sekunden je Port. Am
  Server ändert sich nichts, LCM meldet sich dort nicht an.

Der Scan läuft aus dem Netz, in dem LCM steht. Was aus dem Internet erreichbar
ist, kann davon abweichen - etwa durch eine Portweiterleitung am Router.

## Die Bewertung

| Stufe | Bedeutung |
|---|---|
| kritisch | Ein riskanter Dienst ist erreichbar: Telnet, Docker ohne TLS (2375), eine Datenbank oder rpcbind. |
| Warnung | Der Port ist offen, steht aber nicht im Firewall-Regelsatz, den LCM für den Server führt. |
| in Ordnung | Der Port ist im Regelsatz freigegeben, oder es ist der SSH-Port. |

Die Bewertung entsteht beim Anzeigen gegen den aktuellen Regelsatz. Gibst du
einen Port in der Firewall frei, verschwindet die Warnung sofort - ein neuer
Scan ist dafür nicht nötig. Ob der Port danach wirklich gesperrt ist, zeigt
erst der nächste Scan.

Bei Synology DSM und RouterOS führt LCM keinen Firewall-Regelsatz. Dort
bewertet LCM nur die riskanten Dienste.

## Angriffserkennung

Läuft auf dem Server CrowdSec oder fail2ban mit Portscan-Erkennung, kann der
Scan dort als Angriff gelten - im schlimmsten Fall sperrt der Server dann LCM
aus. Trage die Adresse von LCM vorher in die Ausnahmeliste ein. Aus diesem
Grund läuft der Scan nur, wenn du ihn startest, nicht nach Zeitplan.
