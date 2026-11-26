# Qodana-Durchsicht internal/core und cmd: offene Punkte

Stand: 24.11.2026, Branch `fix/qodana-core`. Grundlage ist der Qodana-Bericht
(268 Befunde im Bereich `internal/core/**` und `cmd/**`), erstellt auf einer
älteren develop-Fassung. Reine Stilregeln (Kommentaranfang, Empfängernamen,
nil-Slices) waren schon herausgefiltert.

## Entscheidung des Besitzers nötig

| Regel | Ort | Befund | Warum unsicher | Vorschlag |
|---|---|---|---|---|
| GoUnusedConst | `internal/core/domain/advisory_cache.go` `AdvisoryCacheTTLDefault` | Exportierte Konstante (20 Minuten) wird nirgends benutzt. | Sie dokumentiert den Standard, den das gorm-Tag `default:20` in `settings.go` nur als Literal wiederholt. Entfernen verliert die Doku, Behalten lässt zwei Stellen auseinanderlaufen. | Behalten und im Tag-Kommentar darauf verweisen, oder löschen und die 20 im Kommentar nennen. |
| GoDfaConstantCondition | `internal/core/domain/settings.go` `ClampJobIdleTimeout` | `minutes < JobIdleTimeoutMin` ist nach `minutes <= 0` immer falsch, solange `JobIdleTimeoutMin = 1`. | Der Zweig ist Absicherung für den Fall, dass die Untergrenze später steigt. | Behalten (bewusst), oder Zweig streichen und den Test anpassen. |
| GoDfaConstantCondition | `internal/core/domain/server.go` `ClampUnreachableGraceDays` | Gleiches Muster mit `UnreachableGraceDaysMin = 1`. | Wie oben. | Wie oben. |
| GoErrorStringFormat | `services/agent_service.go` `ErrAgentOffline`, `services/cvescan.go` `ErrCVEScanUnreachable`, `errCVEScannerUnavailable`, `services/firewall.go` (drei Fehlertexte mit „Regel Port ...") | Fehlertext beginnt groß. | Deutsche Substantive und ein Marker (`domain.ScannerUnavailableMarker`), den der Klassifizierer im Text sucht; Texte erscheinen in der Oberfläche und in Tests. Umschreiben kann Übereinstimmungen brechen. | Nicht ändern; Regel für das Projekt abschalten (Deutsch). |
| GoUnhandledErrorResult | `cmd/probe/run.go` `defer w.Close()` (journal.Writer) | Schließfehler des Journals geht beim Beenden verloren. | Betrifft nur das Ende des Prozesses, ein Fehler hätte keine Folge außer einer Logzeile. | Optional `slog.Warn` im Defer. |
| GoMaybeNil | `services/capability_service_test.go` (`storedCapability`) | Hilfsfunktion liefert `nil`, wenn keine Zeile existiert; mehrere Tests dereferenzieren das Ergebnis direkt. | `requireState` nutzt das `nil` bewusst als „keine Zeile". Bei Fehlschlag gäbe es eine Panik statt einer Meldung, nur in Tests. | Neue Hilfsfunktion `mustStoredCapability` mit `t.Fatalf` für die direkten Zugriffe. |

## Bewusst nicht geändert

| Regel | Anzahl | Grund |
|---|---|---|
| GoUnhandledErrorResult: `fmt.Fprint*` auf Terminal (`cmd/**`) | 46 | Ausgabe an `os.Stdout`/`os.Stderr`; ein Fehler ist nicht behandelbar. |
| GoUnhandledErrorResult: `fmt.Fprintf` auf `*strings.Builder` | 47 | `strings.Builder` liefert nie einen Fehler. |
| GoUnhandledErrorResult: `Close()`/`Flush()` auf SSH-Verbindungen, Antwortkörpern, lesend geöffneten Dateien, im `defer` | 93 | Aufräumen; ein Fehler ändert nichts. Schreibend geöffnete Dateien (Backup, Staging, `moveFile`) prüfen `Close` bereits. |
| GoUnhandledErrorResult: `os.Remove` von Temp-Dateien | 5 | Aufräumen, meist im Fehlerpfad. |
| GoUnhandledErrorResult: Testcode (Fuzz-Aufrufe, `os.Unsetenv`, `t.Cleanup`, Puffer) | 9 | Testhilfen, nichts zu verlieren. |
| GoMaybeNil / GoDfaErrorMayBeNotNil | 19 | Testcode; nach der Fehlerprüfung folgt ein `t.Fatal`, oder der nil-Pfad wird bewusst geübt (`dockerRelevantRefs(nil, ...)`, `ruleScript(nil, nil, ...)` greifen nicht auf das nil-Argument zu). |
| GoDfaConstantCondition | 3 | Zwei Clamp-Funktionen (Tabelle oben) und `dsm_checks_test.go`: `out != nil` nach `Scan("")` ist ein Fehlalarm. |
| GoBoolExpressions | 2 | `ssh_2fa_test.go` vergleicht Pfadkonstanten absichtlich, um die Sortierreihenfolge der Drop-ins festzuschreiben. |
| GoResourceLeak | 2 | `backup_archive.go`: `chunkWriter` und age-Writer halten keine Betriebssystem-Ressource; die Datei schließt der Aufrufer. |
| GoErrorStringFormat | 6 | Siehe Tabelle oben. |
| GoUnusedConst | 1 | `AdvisoryCacheTTLDefault`, siehe Tabelle oben. |

## Geändert

| Regel | Anzahl | Was |
|---|---|---|
| GoPrintFunctions | 6 (zwei Zeilen, je drei Meldungen) | Echter Fehler: das Skript der Welt-Rechte-Suche (`path_hardening.go`) lief durch `Sprintf`, `%a` und `%G` von `stat` waren nicht geschützt. Auf dem Server stand `%!a(string=daten)`. Test ergänzt. |
| GoUnhandledErrorResult | 3 | `trivyd` prüft `Close` der SBOM-Datei; zwei Test-Setups prüfen `ReplacePackages` und `TriggerSystem`. |
| GoDirectComparisonOfErrors | 7 | `errors.Is` in Tests. |
| GoTypeAssertionOnErrors | 6 | `errors.As` in Tests. |
| GoUnusedConst / GoUnusedFunction / GoUnusedGlobalVariable / GoUnusedParameter | 1 + 1 + 1 + 1 | `whenAlways`, `domain.splitCSVList`, `backupMagic`, Parameter von `cpuSince` (und `self`). |
| GoReservedWordUsedAsName | 6 | `min`/`max` in `alert_service.go`, `allowlist_service.go`, `custom_action_service.go`, `settings_service.go` umbenannt. |
| GoImportUsedAsName | 1 | `path` in `profile_apply.go` heißt `sudoersPath`. |
| GoRedundantConversion | 1 | `docker_update_rule_test.go`. |
| GoMaybeNil | 1 | `dsm_checks_test.go`: `t.Fatalf` statt `t.Errorf`. |
