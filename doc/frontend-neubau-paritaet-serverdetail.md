# Release B: Parität Serverdetail (alt → neu)

Stand: 04.11.2026 · Vergleich `frontend/src/pages/ServerDetail.svelte` samt
Komponenten mit `ui/src/pages/servers/**`.

Status: **vorhanden** (war schon im Neubau), **ergänzt** (mit diesem Stand
nachgezogen), **offen** (fehlt noch, Folgeauftrag), **anderer Bereich**
(liegt bei einem anderen Agenten bzw. einer anderen Seite). Bewusst
weggelassen wurde nichts: Das Backend liefert alle Daten der alten Seite
weiterhin.

## Kopf und Aktionen

| Alte Funktion | Im Neubau | Status |
|---|---|---|
| Name, OS-Logo, Host:Port, letzter Kontakt | Kopf | vorhanden |
| Dienstbenutzer, abweichender Hostname | Kopf, Faktenzeile | ergänzt |
| Ampel mit Befunden | Kopf (`lcm-status` mit Befund-Blase) | vorhanden |
| Offline-, Wartungs-, Eingeschränkt-, Agent-Kennzeichen | Kopf | vorhanden |
| Kennzeichen „Konsole offen“ (wer, seit wann) | Kopf, `GET /servers/consoles` | ergänzt |
| Proxmox-Produkt und Version | Kopf und System-Block | ergänzt |
| RouterOS/DSM: Kanal, Modell, Update-Stand, Advisor | System-Block und Hardware-Block | ergänzt |
| Laufender Job mit Laufzeit | Band „belegt“ | vorhanden |
| Job abbrechen | Band „belegt“, Bestätigung mit Wirkung | ergänzt |
| Konsole (Terminal) | Aktionsleiste | vorhanden |
| Hardware neu erfassen | Hardware-Block („Neu erfassen“) | ergänzt |
| Alles neu erfassen | Menü „Mehr“ | ergänzt |
| SSH härten / Härtung zurücknehmen | Menü „Mehr“ | ergänzt |
| Firewall-Knopf | Aktionsleiste, öffnet den Regeleditor (`?modal=firewall`); ohne Schreibrecht oder auf Proxmox zum Reiter Sicherheit | ergänzt |
| Firewall-Regeleditor (Regeln, Allowlists, Port-Scan) | `sec-firewall.ts`: SSH-Quellen mit LCM-Adresse und Aussperr-Warnung, Freigaben, Vorschläge aus lauschenden Diensten samt Abfrage, Docker-Hinweis, Bestätigung mit Wirkung, Deaktivieren | ergänzt |
| Einstellungen | Aktionsleiste, `srv-settings` | vorhanden |
| Benutzer abgleichen | Menü „Mehr“ | ergänzt |
| Schlüssel erneuern (SSH) / Agent-Token erneuern mit Einmal-Anzeige | Menü „Mehr“ | ergänzt |
| Neu verbinden (ReconnectWizard) | Aktionsleiste → Assistent `/servers/join?reconnect=` | vorhanden |
| DNS-Test | Hardware-Block, Zeile DNS-Test | ergänzt |
| Sicherheits-Tools installieren (fail2ban/CrowdSec, Allowlist, LAPI-Modus) | Reiter Sicherheit → „Sicherheits-Tools einrichten“ (`sec-tool-install.ts`, auch SSH-2FA) | ergänzt |
| Neustart | Menü „Mehr“ und System-Block bei „Neustart nötig“ | ergänzt |
| sudo einschränken | Aktionsleiste | vorhanden |
| Volle Rechte wiederherstellen (Anleitung) | Menü „Mehr“ | ergänzt |
| Server entfernen (mit Ziel-Bereinigung) | Menü „Mehr“ → Dialog mit getipptem Namen | vorhanden |

## Übersicht

| Alte Funktion | Im Neubau | Status |
|---|---|---|
| LCM-Host-Karte: Trivy, Version, DB-Alter/veraltet/nie geladen, Installieren | `ov-lcm-host.ts` | ergänzt |
| Sandbox-Zustand des Scanners, Sandbox nachrüsten | `ov-lcm-host.ts` | ergänzt |
| apt-cacher-ng einrichten (Statistik liegt unter Einstellungen → APT-Cache) | `ov-lcm-host.ts`, Link zur Einstellung | ergänzt |
| CrowdSec-LAPI mit Bouncer-Schalter einrichten | `ov-lcm-host.ts` | ergänzt |
| Hinweis „läuft im Container“, Hinweis „kein apt“ | `ov-lcm-host.ts` | ergänzt |
| Erkennung LCM-Host im Frontend nachgebaut | `is_lcm_host` aus dem Backend | ergänzt |
| Betriebsfähigkeit mit Reparatur/Wiederholen | Karte Betriebsfähigkeit | vorhanden |
| Hardware: Plattform, Modell, CPU, Kerne, RAM, Platte | Block Hardware und Netz | ergänzt |
| Paketverwaltung(en), IP-Adressen, DNS aktiv, DNS-Test | Block Hardware und Netz | ergänzt |
| Zeitzone, Uhr (Abweichung), Zeitdienst, Container-Hinweis | Block Hardware und Netz | ergänzt |
| OS, Support (LTS, bis wann, abgelaufen, Satz dazu) | Block System | ergänzt |
| Kernel mit „neuerer installiert“ und Container-Hinweis | Block System | ergänzt |
| Neustart nötig, SSH gehärtet, Schlüsselaustausch (quantensicher) | Block System | ergänzt |
| Firewall mit Werkzeug und Ports, Docker-Ports an der Firewall vorbei | Block System | ergänzt |
| Sicherheits-Tool (CrowdSec/fail2ban aktiv) | Block System | ergänzt |
| Host-Key-Fingerprint (DSM: Zertifikat) | Block System, Mono mit Kopieren | ergänzt |
| Installierte Kernel mit Zustand, alte Kernel entfernen | Karte Installierte Kernel | ergänzt |
| Servergruppen anzeigen, hinzufügen, entfernen | Karte Gruppen, Fenster „Gruppen zuordnen“ | ergänzt |
| API-Geräte: nur erhobene Zeilen | Zeilen ohne Erhebung entfallen | ergänzt |

## Reiter

| Alte Funktion | Im Neubau | Status |
|---|---|---|
| Pakete: aktualisieren, alle, nur Sicherheit, autoremove, Suche, Auswahl | Reiter Pakete | vorhanden |
| Pakete: Version wählen und installieren | Reiter Pakete, „Version …“ je Paket → Fenster mit Befehlsvorschau (`pkg-version.ts`) | ergänzt |
| Paket-Pins (Kernel-Pin, anwenden, hinzufügen, entfernen) | Reiter Pakete, Knopf „Pins“ → Fenster (`pkg-pins.ts`); Pin je Paket, Kennzeichen „gepinnt“ | ergänzt |
| Snaps: alle/einzelne aktualisieren, entfernen | Reiter Snaps | vorhanden |
| Docker: Container und Images | Reiter Container | vorhanden |
| Docker: Pull, Pull alle, Compose-Update, Prune, Image löschen, CVE-Relevanz | Reiter Container (auch Dienst-Update, Inventar neu lesen, Digest, Prüfstand, überholte Fassungen); Container-Seite: Image- und Projekt-Update über alle betroffenen Server | ergänzt |
| Apps: erkannte Apps mit Version | Reiter Apps | vorhanden |
| Apps: sichern, aktualisieren | Reiter Apps | ergänzt |
| Apps: Liste unbekannter Apps | Reiter Apps, Karte „Ohne Zuordnung“ (Dienst, Programm, Unit-Datei) | ergänzt |
| Benutzer: Konten mit Status, Schlüsseln, letzter Anmeldung | Reiter Benutzer | vorhanden |
| Benutzer: neu einlesen, sperren/entsperren, entfernen, Anmeldehistorie, LCM-Benutzer zuordnen, ausstehende Syncs | Reiter Benutzer (Anmeldehistorie im Fenster `?logins=`, Entfernen mit getipptem Namen, 2FA-Spalte, Anleitung) | ergänzt |
| Sicherheit: CVE-Liste, Scan, Schutzübersicht | Reiter Sicherheit | vorhanden |
| Sicherheit: ACL einrichten, Pfade härten (Vorschläge, Sammel-Härtung, zurück) | Reiter Sicherheit, Karte Verzeichnisrechte (`sec-harden.ts`) | ergänzt |
| Sicherheit: SSH-2FA, Sicherheits-Tools verwalten (Dienst, Allowlist, Sperren, Deinstallieren) | Reiter Sicherheit: Karte SSH-2FA, je Tool eine Karte (`sec-tool.ts`) | ergänzt |
| Sicherheit: Quellfilter, „ungenutzt“-Kennzeichen | Reiter Sicherheit, CVE-Karte (`?cvesrc=`), Herkunftsspalte, Zähler ungenutzter Images | ergänzt |
| Deep-Scan: Härtungsindex, Befunde, Scan starten | Reiter Deep-Scan | vorhanden |
| Deep-Scan: Werkzeuge installieren, Berichtsverlauf mit neu/behoben | Reiter Deep-Scan (Verlauf aufklappbar `?report=`, dazu Kernel-Lücken und Neustart-Hinweis) | ergänzt |
| Speicher: Laufwerke, Speicherverbünde | Reiter Speicher | vorhanden |
| Speicher: Verlaufsgrafik, Überwachung je Laufwerk mit Schwelle | Reiter Speicher (Verlauf von / mit Prognose, Fenster `?volume=` für Überwachung und Warnschwelle) | ergänzt |
| Repositories: Liste, auf HTTPS umstellen | Reiter Repositories | vorhanden |
| Repositories: HTTPS zurücknehmen, bekanntes Repo hinzufügen, APT-Cache verbinden/trennen, Konsolenausgabe | Reiter Repositories: Karte „Einrichten“, Konsolenausgabe unter der Liste | ergänzt |
| Jobs mit Health-Check-Filter | Reiter Jobs | vorhanden |
| Reiter „Protokolle“ (SSH-Sitzungen mit Befehlen) | Reiter SSH-Sitzungen | vorhanden |

## Einstellungen (Fenster)

| Alte Funktion | Im Neubau | Status |
|---|---|---|
| Name, Host, Port | `srv-settings.ts`, Abschnitt Allgemein; Systemname als Vorschlag | vorhanden |
| Konsole sperren, Root-Login, SSH-Port ändern, Benutzer-Sync, Docker-Richtlinie, Wartung (seit wann), Erreichbarkeit mit Frist, DNS-Server mit Vorgaben, Zeit prüfen, Zeitzone, Zeitserver mit Vorgaben | `srv-settings.ts` mit Abschnitten Allgemein, Betrieb, Zugang, DNS, Zeit; Aktionen auf dem Server mit Bestätigung und Wirkung (`set-*.ts`) | ergänzt |
