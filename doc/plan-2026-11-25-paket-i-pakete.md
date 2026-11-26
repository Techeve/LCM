# Plan: Paket I – Agent als rpm, apk und Arch-Paket aus repo.techeve.de

Teil von `doc/plan-2026-11-23-agent-robustheit.md` (Paket I). Betrifft zwei Repos: LCM
(`techeve/lcm-intern`) und RSM (`~/dev/rsm`, liefert repo.techeve.de aus).

## Ziel

Der Agent lässt sich auf der RHEL-Familie 9/10 (dnf4), Fedora (dnf5), openSUSE Leap und
Tumbleweed (zypper), Alpine 3.2x (apk, OpenRC) und Arch Linux (pacman) genauso einrichten,
halten und per Knopf oder Gruppenregel aktualisieren wie auf Debian und Ubuntu. Die Pakete
kommen signiert aus repo.techeve.de.

## Entscheidungen (Tony, 25.11.2026)

1. rpm und Arch bekommen einen **eigenen GPG-Schlüssel** (nicht den apt-Schlüssel). apk bekommt
   einen eigenen RSA-Schlüssel. Beide erzeugt und hält RSM.
2. **RSM signiert und baut die Metadaten** (repodata, APKINDEX, Arch-Datenbank). Die CI lädt nur
   rohe Pakete hoch.
3. Unterstützt: RHEL-Familie 9/10, Fedora aktuell, openSUSE Leap/Tumbleweed, Alpine 3.2x, Arch.
4. dnf hält den Agenten über `exclude=lcm-agent` in der Techeve-Repo-Datei; LCM installiert
   gezielt mit `--disableexcludes=techeve-lcm`.
5. Kanäle nur **community** und **beta**, kein Enterprise-Pfad (Agenten beziehen sich immer aus
   dem öffentlichen Kanal).
6. **Arch-Repo** gehört dazu.
7. Nach einem Rollback darf LCM das Paket auf die laufende ältere Version zurücksetzen (wie bei
   apt), sonst nie abwärts.
8. **Ein `setup.sh`** erkennt apt, dnf, zypper, apk und pacman. Es kommt unter
   Versionskontrolle ins RSM-Repo.

## Schnittstelle zwischen LCM und RSM

| Was | Festlegung |
|---|---|
| Upload | `POST <REPO_URL>/pkg/v1/<format>/<kanal>` auf dem Upload-Listener, Rumpf = Paketdatei, Basic Auth mit einem RSM-Upload-Zugang. `format` ∈ `rpm`, `apk`, `arch`; `kanal` ∈ `community`, `beta`. Recht des Zugangs: Eintrag `<format>/<kanal>` in `Publish`. |
| Antworten | `201` mit JSON `{"name","version","arch","path"}`; `409` wenn genau diese Version schon liegt (nichts wird überschrieben); `400` bei falschem Format; `403` ohne Recht. |
| Ablage rpm | `/rpm/<kanal>/<arch>/` mit `arch` = `x86_64`, `aarch64`; Metadaten in `repodata/`, `repomd.xml.asc`. dnf: `baseurl=https://repo.techeve.de/rpm/<kanal>/$basearch/`. |
| Ablage apk | `/apk/<kanal>/<arch>/<name>-<version>.apk` und `APKINDEX.tar.gz`; Zeile in `/etc/apk/repositories`: `https://repo.techeve.de/apk/<kanal>`. |
| Ablage Arch | `/arch/<kanal>/<arch>/` mit Repo-Name `techeve` (`techeve.db`, `techeve.files`, je mit `.sig`), Pakete mit `.sig`. pacman: `Server = https://repo.techeve.de/arch/<kanal>/$arch`. |
| Schlüssel | `/keys/techeve-packages.asc` (GPG für rpm und Arch), `/keys/techeve-apk.rsa.pub` (apk, Dateiname = Name in `/etc/apk/keys/`). |
| Einrichtung | `curl -fsSL https://repo.techeve.de/setup.sh \| sudo sh` (Kanal über `--beta`), wie heute für apt. |

## Pakete

### RSM

**R-1 Datenmodell und Reservierung** (offen): Namen `rpm`, `apk`, `arch`, `keys` reserviert
(Dateiablage, Upload-Präfixe, aptly-Prefixe). Kanalliste führt je Format und Kanal einen
Eintrag mit `format`. Abnahme: Tests; aptly-Publish auf `rpm` wird abgewiesen.

**R-2 Image** (offen): Werkzeuge für rpm (`createrepo-c`, `rpm`/`rpmsign`) und Arch (`repo-add`
oder eigener Go-Bau der Datenbank) im Image; APKINDEX baut RSM in Go. Abnahme: Image baut,
Smoketest in der CI.

**R-3 Schlüssel** (offen): Neuer GPG-Schlüssel für Pakete und RSA-Schlüssel für apk, erzeugt
beim ersten Start, Ablage 0600 unter `/data`, Anzeige in der Oberfläche, öffentliche Teile
unter `/keys/`. Abnahme: Neustart behält die Schlüssel; Export stimmt mit der Signatur überein.

**R-4 Upload-Weg** (offen): Endpunkt laut Schnittstelle, Rechte über `UploadAccount.Publish`,
Größenlimit, Formatprüfung über die Datei selbst (nicht den Namen), Name/Version/Arch aus dem
Paket gelesen. Abnahme: Tests für Rechte, Format, 409.

**R-5 rpm-Kanal** (offen): signieren (`rpmsign`), ablegen, `createrepo_c --update`,
`repomd.xml.asc`; serialisiert und atomar. Abnahme: `dnf install` mit `gpgcheck=1` und
`repo_gpgcheck=1` auf AlmaLinux und Fedora, `zypper in` auf openSUSE.

**R-6 apk-Kanal** (offen): Ablage unter dem von apk erwarteten Namen, APKINDEX neu bauen und
mit RSA signieren. Indexformat vorher auf Alpine 3.23/3.24 prüfen (apk-tools 2 oder 3).
Abnahme: `apk update` ohne Warnung und `apk add lcm-agent=<ver>` auf Alpine; manipulierte
Signatur wird abgewiesen.

**R-7 Arch-Kanal** (offen): Paket und Datenbank mit dem Paketschlüssel signieren. Abnahme:
`pacman -Sy lcm-agent` mit `SigLevel = Required` auf Arch.

**R-8 setup.sh** (offen): ein Skript für apt, dnf, zypper, apk und pacman, unter
Versionskontrolle im RSM-Repo, ausgeliefert über die Dateiablage. dnf-Repo mit
`exclude=lcm-agent`. Abnahme: Skript läuft auf allen Zielsystemen im Container durch und
richtet eine Quelle ein, die danach `lcm-agent` findet.

**R-9 Oberfläche, Landing, Zähler, Pflege** (offen): Kanalansicht und Landing mit
Einrichtungszeilen je Format, Downloadzähler für `.rpm`, `.apk`, `.pkg.tar.zst`, nächtliche
Prüfung der Dateien, keine automatische Löschung. Abnahme: Tests; Zähler steigt nach Abruf.

### LCM

**L-1 CI-Veröffentlichung** (zu prüfen): Auf community und beta lädt ein Job die rpm-, apk- und
Arch-Pakete (amd64, arm64) hoch und prüft danach die Version im veröffentlichten Index.
Abnahme: Fehlertest mit falscher Version scheitert.
Umsetzung: `packaging/publish-pkgs.sh` und CI-Job `deploy:pkgs` (nur community und beta,
`RELEASE_NEEDED=true`, gleiche `needs` wie `deploy:apt`). 201 und 409 gelten als Erfolg. Die
Gegenprobe liest `repodata/primary.xml` (gz, zst, xz), `APKINDEX.tar.gz` und `techeve.db` je
Architektur; die Version nimmt sie aus dem Dateinamen des hochgeladenen Pakets. Test:
`packaging/publish-pkgs-test.sh` gegen eine Attrappe (Job `packaging:publish`); die Prüfungen
laufen zusätzlich gegen echte Ausgaben von createrepo_c, `apk index` und `repo-add`.

**L-2 Agent-Update für dnf, zypper, apk, pacman** (zu prüfen): Versionen auflisten, exakte Version
installieren (nie abwärts, außer beim Nachziehen nach einem Rollback), Version zurücklesen;
Riegel „nur apt“ für unterstützte Familien öffnen, UI-Texte anpassen. Abnahme: Parser-Tests mit
echten Ausgaben von dnf4, dnf5, zypper, apk und pacman.
Umsetzung: `internal/core/services/agent_pkg.go` (Skripte, Auswertung, Versions-Umrechnung),
Einbindung in `agent_update.go`; `ErrAgentUpdatePackageManager` und der apt-Riegel entfallen,
Grund `not_apt` heißt jetzt `no_manager`. Der Job prüft nach der Installation die gemeldete
Version und scheitert sofort, statt 45 Minuten zu warten. Fixtures in
`internal/core/services/testdata/agent-pkg/` stammen aus Containern (AlmaLinux 9 und 10,
Fedora, Leap, Tumbleweed, Alpine 3.22 und 3.24, Arch); `testing/agent-pkg-e2e.sh` fährt alle
Skripte dort durch.

**L-3 Halten** (zu prüfen): dnf über `exclude` (Install mit `--disableexcludes`), zypper über
`addlock` (um den Install herum gelöst), apk über `lcm-agent=<ver>` in `/etc/apk/world`,
pacman über `IgnorePkg`. `readPin` des Agenten kennt dnf5 und die neuen Wege. Abnahme:
`dnf upgrade`, `zypper update`, `apk upgrade`, `pacman -Syu` lassen den Agenten stehen.
Umsetzung: Das Paketskript `agent-postinstall.sh` setzt `exclude=lcm-agent` in
`techeve-lcm.repo` (falls `setup.sh` es nicht schon tat), das zypper-Schloss in
`/etc/zypp/locks` und `IgnorePkg` in `pacman.conf`; unter apk gibt es nur einen Hinweis auf
`apk add lcm-agent=<ver>` (apk schreibt `world` nach den Skripten neu), LCM installiert immer so.
LCM nutzt `--setopt=disable_excludes=techeve-lcm` (dnf4 und dnf5; `--disableexcludes` kennt dnf5
nicht), löst das zypper-Schloss um die Installation und setzt es wieder. `readPin` kennt
`exclude`/`excludepkgs`, `versionlock.toml` (dnf5), `/etc/zypp/locks`. Im Container geprüft:
Systemupdate, gezielter Wechsel hoch, Beta, Nachziehen nach unten.

**L-4 Paketskripte** (zu prüfen): fehlende Zweige für rpm, apk und pacman, Fälle in
`agent-scripts-test.sh`. Abnahme: `packaging:shells` grün.
Umsetzung: `pkg_kind`, `pin_dnf`, `pin_zypper`, `pin_pacman`, `hint_apk` im postinstall; Fälle
in `agent-scripts-test.sh`, grün unter dash, busybox sh und bash --posix. Nichts startet oder
startet neu (unverändert).

**L-5 Einrichtung in UI und Doku** (zu prüfen): Onboarding und Agent-Doku nennen die Befehle je
System (gleicher `setup.sh`-Aufruf, dann `dnf`/`zypper`/`apk`/`pacman`). Abnahme: UI-Test, Doku de/en.
Umsetzung: Auswahl „Betriebssystem des Servers“ im Onboarding (`join-agent.ts`, `join-model.ts`
mit Vitest), Tabelle in `internal/appdocs/pages/{de,en}/30-agent.md` und im Leitfaden
`docs/**/guides/remote.md`. Für apk gilt `apk add lcm-agent` (die Oberfläche kennt die
Version nicht; das Paket nennt den Pin-Befehl, LCM pinnt bei jedem Update). Für dnf steht
die Ausnahme vom `exclude` im Befehl, sie schadet auch ohne `exclude` nicht.

### Tests

**T-1 Echte Paketverwaltungen im Container** (LCM-Seite zu prüfen, RSM-Teststand offen): almalinux:9, almalinux:10, fedora,
opensuse/leap, opensuse/tumbleweed, alpine:3.23/3.24, archlinux gegen einen lokalen
RSM-Teststand: einrichten über `setup.sh`, installieren, halten, gezielt aktualisieren,
Rollback-Nachziehen, Signaturfehler.
Stand LCM: `testing/agent-pkg-e2e.sh` prüft die Agent-Skripte gegen echte Paketverwaltungen mit
einem selbst gebauten lokalen Repository (kein RSM, keine `setup.sh`; pacman mit signiertem
Repo und `SigLevel = Required`). Ausstehend sind der RSM-Teststand und Signaturfehler.

**T-2 pve-test** (offen): CT 103, 104, 105, 106, 108, 109, 111 gegen einen RSM-Teststand und
eine Test-LCM: einrichten, enrollen, Update per Knopf, Systemupdate lässt den Agenten stehen.
Nach Freigabe durch Tony: Ausrollen von RSM auf repo.techeve.de.
