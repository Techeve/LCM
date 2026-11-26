# Qodana-Bericht (Rest): offene Punkte

Umfang: alles außer `internal/core/**` und `cmd/**` (227 Befunde). Der Bericht
stammt von einem älteren develop-Stand; Befunde wurden nach Inhalt gesucht,
nicht nach Zeilennummer. In `internal/agent` und `internal/remote` ist viel
Code seither umgebaut worden (`enroll.go`, `ws.go`, `hub.go`, `switch.go`).

Bilanz: 21 geändert, 206 bewusst nicht geändert, davon 2 zur Entscheidung.

## Braucht eine Entscheidung

| Regel | Ort | Befund | Warum unsicher | Vorschlag |
|---|---|---|---|---|
| GoUnusedParameter | `internal/storage/repositories/custom_action_repository.go` `CountRulesUsing(id uint, actionIDStr string)` | `id` wird nicht benutzt, die Funktion nimmt nur `actionIDStr`. | Einziger Aufrufer ist `internal/core/services/custom_action_service.go:131`, also außerhalb dieses Umfangs. Eine Signaturänderung betrifft Core und möglicherweise Mocks. | Parameter `id` streichen und den Aufruf in Core anpassen (kleiner Folge-MR im Core-Umfang). |
| GoUnusedExportedFunction | `internal/infrastructure/sandbox/toolspec.go` `TempDir()` | Exportierte Funktion ohne Aufrufer; `sandbox_linux.go` ruft `os.TempDir()` direkt. | Exportierte API, die Entscheidung liegt beim Besitzer des Pakets. | Entweder löschen oder in `sandbox_linux.go` verwenden, damit der Kommentar zu `PrivateTmp` an einer Stelle lebt. |

Sicherheitsrelevante Befunde (Anmeldung, Rechte, Token, Zertifikate, Pinning,
Krypto) gab es in diesem Umfang nicht; entsprechend wurde daran nichts geändert.

## Bewusst nicht geändert

| Regel | Anzahl | Grund |
|---|---|---|
| GoUnhandledErrorResult | 139 | Siehe unten. |
| GoUnusedParameter | 23 | Schnittstellen- und Hook-Signaturen (MQTT-Hooks in `hub.go`, `sshx/fake.go`, `encfield.go` gorm-Serializer, Nicht-Linux-Stubs `tty_other.go`, `sandbox_other.go`); 1 Fall zur Entscheidung (oben). |
| GoErrorStringFormat | 8 | Deutsche Fehlertexte für Anwender: Substantive stehen groß, Satzzeichen gehören zum Text. Die Go-Regel gilt für englische Meldungen. |
| GoDfaErrorMayBeNotNil | 10 | Falsch positiv: In Tests folgt auf die Fehlerprüfung ein `t.Fatal`/`continue`; `trivy.go` gibt einen Struktur-Wert zurück, nie nil. |
| GoMaybeNil | 3 | Falsch positiv: `newBinaryWatch` liefert bei `err == nil` immer einen Zeiger (`switch.go`); `validate_test.go` siehe oben. |
| GoDirectComparisonOfErrors | 7 | Absichtliche Identitätsvergleiche: `mapProvError`/`mapServerError`/`mapNotificationError` geben unbekannte Fehler unverändert zurück und der Vergleich fragt genau das ab (mit `nolint:errorlint` vermerkt); `withDetails` vergleicht `*apierr.Error`-Zeiger. |
| GoBoolExpressions | 4 | Test-Code: Prüfungen von Konstanten (Grenzwerte als Sicherung gegen spätere Änderungen). |
| GoResourceLeak | 3 | Falsch positiv: `mirror.go` schließt in `fillTemp`, `sniroute.go` in allen Zweigen, `logging.go` hält die Rotationsdatei bewusst bis zum Prozessende. |
| GoSwitchMissingCasesForIotaConsts | 2 | Absichtlich mit Auffangfall am Ende (`validate.go`, `hub.go`). |
| GoDeferInLoop | 2 | Test-Code; Verbindungen sollen bis Testende offen bleiben. |
| GoExportedFuncWithUnexportedType | 1 | Absichtlich: `NewLoginGuard` reicht den Zähler nur weiter, Aufrufer brauchen den Typ nicht. |
| GoRedundantConversion | 1 | `diskfree_unix.go`: Feldtypen unterscheiden sich je System, mit `nolint:unconvert` begründet. |
| GoUnusedConst | 1 | `codeInternal` gehört zur vollständigen Tabelle der JSON-RPC-Fehlercodes. |
| GoDfaConstantCondition | 1 | Test-Code (`synology/fixture_test.go`). |
| GoUnusedExportedFunction | 1 | Zur Entscheidung (oben). |

GoUnhandledErrorResult im Einzelnen (nicht geändert, 139):

- 68 in Test-Code (Close, Write in Handlern, Hilfsfunktionen).
- 21 `fmt.Fprintf`/`Fprintln` in `strings.Builder`, `bytes.Buffer` oder `tabwriter` (`appdocs/markdown.go`, `notify/email.go`, `synology/apply.go`, `probe/analysis/text.go`); diese Schreibvorgänge scheitern nie, `tabwriter` meldet Fehler beim `Flush`, der geprüft wird.
- 50 `Close`/`Remove`/`RemoveAll` auf nur gelesenen Dateien, Antwort-Körpern, Verbindungen (meist `defer`) und in Fehlerzweigen, in denen bereits ein Fehler zurückgegeben wird. Dort sind Schreib-, `Sync`- und `Close`-Ergebnisse der fertigen Datei schon geprüft (`probe/journal/writer.go`, `advisories/local.go`, `agent/enroll.go` `copyFile`, `config/mirror.go`).
- Einige Meldungen treffen heute anderen Code (`enroll.go`, `logs_controller.go`, `ws.go`, `encfield.go`): `tx.AddError` ist die Fehlerbehandlung selbst, die übrigen sind bereits mit `_ =` versehen oder Stream-Callbacks ohne Rückgabewert.

## Geändert

| Regel | Anzahl | Was |
|---|---|---|
| GoUnhandledErrorResult | 6 | `seed_demo.go`: fünf `Association(...).Append(...)` geben Fehler jetzt zurück statt sie zu verschlucken; `trivy/local.go`: Fehler beim Schließen der SBOM-Datei vor dem Scan wird geprüft. |
| GoDirectComparisonOfErrors | 7 | `err == gorm.ErrRecordNotFound` zu `errors.Is` in sechs Repositories (`audit`, `job`, `package_pin`, `role`, `server` je 1 bzw. 2); `tty_test.go` auf `errors.Is(err, ErrTTYGap)` (beide Stellen). |
| GoTypeAssertionOnErrors | 1 | `sniroute_test.go`: `errors.As` statt Typzusicherung. |
| RegExpRepeatedSpace | 2 | `openapi_test.go`: `^ {2}` und `^ {4}`. |
| GoReservedWordUsedAsName | 4 | `min` (`logging/read.go`, Funktion im `appdocs`-Test entfernt, eingebautes `min` genügt), `max` (`totp/qr_test.go`), `new` (`trivy/scan_cache_test.go`). |
| GoImportUsedAsName | 1 | `probe/analysis/analysis_test.go`: `host` zu `hostLine`. |
