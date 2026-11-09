# Fahrplan LCM: Releases A, B, C

Stand: 31.10.2026 · abgestimmt mit Tony

Drei Hauptphasen, jede endet mit einem eigenen Feature-Release (Release-Zug wie
gewohnt: zuerst beta).

| Release | Inhalt | Plan |
|---|---|---|
| **A** | Fehlgeschlagene Aktionen wirken auf den Serverstatus: Betriebsfähigkeit je Aktionsart, Fehlerklassen mit Hinweis, scheiternde Updates = rot, Alarm beim Wechsel | `plan-2026-10-31-a-aktionsfehler-status.md` |
| **B** | Frontend-Neubau: Lit + TypeScript, eigene Designsprache, Vollbild, vollständige Mehrsprachigkeit, selbsterklärende Aktionen und Einstellungen, Hauptmenü oben, neues Dashboard, Seite Server, Container statt Docker, echte URLs mit Rückkehr nach Login, Service Worker als Cache mit sofortigem Versionswechsel, WebSocket als Meldekanal je Tab, Browser-Sitzung (Arbeitsspeicher oder Cookie, einstellbar), strenge CSP | `plan-2026-10-31-b-frontend-neubau.md` |
| **C** | Zentrale Update-Erkennung, Relevanz (Kerndienste, Basis, offene Ports), Warnstufe nach Relevanz, Gruppen-Reiter „Dienste & Patchen“, Verbundgruppen, CVE-gesteuertes Patchen mit Leistungszielen | `plan-2026-10-31-c-zentrale-updates-kerndienste.md` |

Dazu im ZPO-Repo: `ZPO/doc/plan-lcm-wartungsdrain.md` (Server vor dem Patchen
aus dem Load Balancer nehmen), gebraucht für C.

## Reihenfolge

- **A zuerst:** Scheiternde Updates, die niemand sieht, machen LCM unbemerkt
  handlungsunfähig. C baut außerdem auf der Betriebsfähigkeit aus A auf.
- **B vor C:** Die Oberfläche von C entsteht gleich im neuen Frontend. C nutzt
  den Ereigniskanal aus B für Durchläufe, Freigaben und Statuswechsel.
- **C als Ganzes:** Abgleich, Relevanz, Richtlinien und Ausführung gehören
  zusammen und werden mit ihrer Oberfläche getestet.

## Querschnitt in allen drei Releases

- **Leistung:** messbare Ziele je Release, im CI bzw. in der Testumgebung
  geprüft. Keine Abfragen im Takt, wo ein Ereignis reicht.
- **Sicherheit:** Mandantentrennung bei allen neuen Daten und Ereignissen,
  sensible Inhalte verschlüsselt gespeichert, nichts Sensibles im Browser auf der
  Platte, alle externen Daten (Paketquellen, Feeds) signatur- bzw. HTTPS-geprüft.
