# Offen für Tony (Stand 24.11.2026)

Alles, was nach Qodana-Bericht, Review und Paket G noch eine Entscheidung braucht.
Erledigtes steht in `doc/qodana-offen-core.md`, `doc/qodana-offen-rest.md` und
`doc/sicherheit-agent-2026-11.md`.

## Qodana

Nichts offen. 495 inhaltliche Befunde durchgesehen, etwa 55 geändert, darunter ein echter Fehler:
Die Vorschlagssuche für Verzeichnisse mit Welt-Rechten (Abschotten) lief mit kaputtem Skript.
`Sprintf` hatte `%a` und `%G` von `stat -c` verschluckt. Die übrigen Befunde sind Fehlalarme,
Testcode oder bewusst so.

Zwölf kleine Punkte, die die Agenten offen gelassen hatten, habe ich selbst entschieden:
- **Geändert:**
  - `CountRulesUsing` ohne den doppelten Parameter.
  - `sandbox.TempDir()` wird jetzt genutzt.
  - Ein Fehler beim Schließen des Probe-Journals wird geloggt.
  - Neuer Test-Helfer `mustStoredCapability`.
- **Bewusst behalten:**
  - Die Clamp-Absicherungen in `ClampJobIdleTimeout` und `ClampUnreachableGraceDays`.
  - `AdvisoryCacheTTLDefault` als Doku des Standards.
  - Die deutschen Fehlertexte mit großem Anfang, darunter einer mit einem Marker, den der
    Klassifizierer sucht.

Stilregeln, die zu deutschen Kommentaren nicht passen, sind nicht bearbeitet (328 Befunde):
Kommentaranfang mit Funktionsnamen, Receiver-Namen, nil-Slices. Läuft Qodana künftig
regelmäßig, sollten diese Regeln in einer `qodana.yaml` abgeschaltet werden.

## Sicherheit: braucht deine Freigabe

Diese Punkte ändern Anmeldung, Pins, Token oder die Datei mit dem Secret. Einzelheiten mit
Szenario stehen im Sicherheitsbericht unter derselben ID.

| ID | Schwere | Worum es geht | Vorschlag |
|---|---|---|---|
| S-05 | mittel | Das Einrichtungstoken ist das Dauer-Secret: kein Ablauf, nicht einmalig, steht auf der Kommandozeile (ps, Shell-Verlauf). Wer es hat, übernimmt die Sitzung und sieht alle Kommandos samt `stdin`. | Echtes Einmal-Token (bis zur ersten Anmeldung, höchstens 24 h), das gegen ein Betriebs-Secret getauscht wird; Token auch per Datei oder stdin annehmen. Größerer Umbau, eigenes Paket: `doc/plan-2026-11-24-einmal-token.md`. |

## Entschieden 24.11.

| ID | Entscheidung | Stand |
|---|---|---|
| S-01 | Systemroot-Fallback bleibt auch mit Pins, wie er ist. | bleibt so |
| S-15 | Wer LCM übernimmt, ist root auf allen Agenten; keine signierten Kommandos. | bleibt so |
| S-02 | Nach dem Lernen von CA und Schlüssel entfernt der Agent den Zertifikats-Pin aus `agent.json` (im selben atomaren Schreiben). Ein Agent, dessen LCM keine CA mitschickt, behält ihn. | gebaut |
| S-04 | Server-Adresse ohne Hostnamen (`https://:8443`) wird bei der Einrichtung abgewiesen; ohne Namen kein Rückfall auf die Systemroots. | gebaut |
| S-07 | Vor der Anmeldung: CONNECT binnen 10 s, erstes Paket höchstens 8 KiB, höchstens 64 solche Verbindungen gleichzeitig (darüber 503 vor dem Upgrade). Getestet mit dem echten Broker und den echten Agenten 2.0.0 bis 2.3.0. | gebaut |
| S-08 | `agent.json` wird über Zwischendatei, `fsync` und `rename` geschrieben, die Rechte der bestehenden Datei bleiben; beim Start warnt der Agent im Journal bei Rechten für Gruppe oder andere. | gebaut |
| S-05 | Einmal-Token als eigenes Paket geplant: `doc/plan-2026-11-24-einmal-token.md`. | geplant |

## Noch nicht auf echtem System geprüft

- Paket H: `RuntimeDirectory` und `lcm-agent status` auf Debian 11–13.
- Rückfall-Regel bei Abweisung durch LCM (D-4): nur mit Unit-Tests geprüft.
- `tools/distro-matrix.sh` ist noch nie gegen pve-test gelaufen. Vorher muss auf jedem CT der
  Snapshot `vor-agent-robust` angelegt sein.
- Die neuen CI-Jobs (`compat:agents`, `packaging:shells`, `packaging:units`) laufen erst mit
  diesem MR zum ersten Mal auf dem Runner.
