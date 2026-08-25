---
sidebar:
  order: 23
title: Synology DSM
description: Synology-NAS über die DSM-Web-API überwachen - Version, Updates, Pakete, Speicher, Zeit und Security Advisor.
---

LCM unterstützt **Synology DSM** als eigenen Gerätetyp - angebunden über die
**DSM-Web-API**, nicht über SSH.

## Warum API statt SSH

DSM ist zwar Linux-basiert, aber kein verwaltbarer Linux-Server:

- Es gibt **kein `/etc/os-release`** - LCMs Betriebssystem-Erkennung liefe
  ins Leere.
- Der Kernel ist ein **alter, von Synology gepflegter Fork** (z.&nbsp;B.
  4.4.x). Ein CVE-Scan darüber würde Falschalarme am Fließband erzeugen: die
  Sicherheitskorrekturen stecken in Synologys eigenen Fassungen, nicht in den
  Versionsnummern, gegen die eine CVE-Datenbank vergleicht.
- Pakete verwaltet **`synopkg`**, nicht apt oder dnf.
- **Benutzer, Dienste und Firewall verwaltet DSM selbst.** Ein LCM-Service-User
  mit `sudo` würde neben DSMs eigener Konfigurationsverwaltung stehen - und
  beim nächsten DSM-Update womöglich überschrieben.

Die dokumentierte Web-API liefert dagegen genau das, was für die Überwachung
zählt - und zwar so, wie DSM selbst es sieht.

## Was LCM erfasst

| Bereich | Inhalt |
| --- | --- |
| **System** | Modell, DSM-Version, Seriennummer, CPU-Kerne, RAM, Laufzeit |
| **Updates** | ob eine neuere DSM-Fassung bereitsteht (und welche) - das zentrale Ampel-Kriterium |
| **Pakete** | installierte DSM-Pakete mit Version (im Paket-Tab sichtbar) |
| **Speicher** | Volumes: Gesamtgröße, Belegung und Zustand - fließt in Festplatten-Ampel und [Speicher-Verlauf](/guides/monitoring/) ein |
| **Zeit** | Zeitzone und NTP-Zustand samt Zeitserver (siehe [Zeit & NTP](/guides/time/)) |
| **Sicherheit** | die Befunde des **DSM-eigenen Security Advisors** (Stufen *risk* und *danger*), nach Kategorien aufgeschlüsselt |

Beim Security Advisor übernimmt LCM bewusst DSMs eigene Bewertung, statt sie
ohne Shell-Zugriff nachzubauen - Synology weiß am besten, was auf einem DSM
eine Fehlkonfiguration ist.

## Was auf DSM nicht geht

Firewall-Verwaltung, CVE-Scan, Paketquellen, Paket-Updates, Benutzer-Sync,
SSH-Härtung, eingeschränkter Modus und Skript-/Custom-Aktionen sind **gesperrt**
- sie setzen eine Shell oder eine Paketverwaltung voraus. Ruft man sie über die
API auf, antwortet LCM mit einer Meldung, die den Grund nennt (HTTP&nbsp;409),
statt in einen Folgefehler zu laufen. In einer Servergruppe werden solche
Regeln auf DSM-Geräten **benannt übersprungen** - ein gemischter Zeitplan
bleibt damit grün.

**Health-Check und System-Sync** laufen dagegen: sie erheben auf einem
API-Gerät den Gerätezustand neu. Das ist dort die sachliche Entsprechung eines
Verfügbarkeits-Pings und hält Ampel, Update-Stand und Speicher-Verlauf aktuell.

## Gerät aufnehmen

1. In DSM ein **eigenes Konto für LCM** anlegen: *Systemsteuerung →
   Benutzer & Gruppe*, Mitglied der Gruppe **administrators**.

   :::caution[Zwei-Faktor-Anmeldung]
   Für dieses Konto darf **keine 2FA erzwungen** sein - ein unbeaufsichtigter
   Scan kann keinen Einmalcode eingeben. Sichere es stattdessen über
   *Systemsteuerung → Sicherheit → Konto* mit einer **IP-Beschränkung auf den
   LCM-Host** ab.
   :::

2. In LCM auf **„+ Server hinzufügen"** und oben den Modus **Synology DSM**
   wählen.

3. Name, Host, **DSM-Port** (Standard `5001`), Konto und Passwort eintragen und
   auf *Weiter* klicken.

4. LCM zeigt den **SHA-256-Fingerprint des TLS-Zertifikats**. Vergleiche ihn in
   DSM unter *Systemsteuerung → Sicherheit → Zertifikat* und bestätige.

   DSM liefert ab Werk ein selbstsigniertes Zertifikat - eine Kettenprüfung
   gibt es hier also nicht. LCM merkt sich diesen Fingerprint und **bricht die
   Verbindung künftig ab, wenn er sich ändert** (Schutz vor
   Man-in-the-Middle, genau wie beim SSH-Host-Key).

5. Nach dem Anlegen erhebt LCM den Zustand sofort und zeigt das Gerät online.

Das Passwort wird **AES-GCM-verschlüsselt** gespeichert (wie alle Zugangsdaten
in LCM) und ausschließlich für die Anmeldung an der DSM-API verwendet.

## Ampel

Ohne Paket- und CVE-Sicht tragen zwei Kriterien die Bewertung:

- **Neuere DSM-Fassung verfügbar** → Gelb, mit Versionsangabe.
- **Befunde des Security Advisors** (risk/danger) → Gelb, mit Anzahl und
  Kategorien.

Dazu kommen die allgemeinen Kriterien, die für jeden Server gelten:
Erreichbarkeit, Festplattenbelegung und Speicher-Prognose.

:::note[Zertifikat erneuert?]
Erneuerst du das DSM-Zertifikat (z.&nbsp;B. per Let's Encrypt), ändert sich der
Fingerprint und LCM meldet das Gerät als nicht erreichbar - mit genau dieser
Begründung. Nimm das Gerät dann einmal neu auf, um den neuen Fingerprint zu
bestätigen.
:::
