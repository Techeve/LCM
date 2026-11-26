# Plan: Support-Matrix der Distributionen und Testlauf aller Varianten

Stand 24.11.2026 · Status: **alle Pakete offen**

## Ziel

LCM soll auf jeder hier genannten Distribution eindeutig eingeordnet sein. Jede Rolle hat eine von drei Stufen:

| Stufe | Bedeutung |
|---|---|
| ✅ **getestet** | Auf einem echten System mit der aktuellen Release-Linie (ab 2.2) geprüft, Ergebnis in Abschnitt 5 festgehalten. |
| 🟡 **sollte gehen** | Der Code deckt es ab, ein Test mit der aktuellen Linie fehlt. Gleicher Paketmanager reicht dafür nicht: Debian hatte trotz apt eigene Fehler (Bubblewrap 0.12, systemd 257, su-Pfad ohne sbin). |
| ❌ **nicht unterstützt** | Bewusst ausgeschlossen. Der Code bietet keinen Weg oder wir raten ab. |

Rollen:

- **Host:** LCM ist installiert. Wege: apt-Paket auf Ubuntu/Debian und Ablegern, Docker überall.
- **SSH:** Client ohne Agent.
- **Agent:** Client mit `lcm-agent`.
- **Eingeschränkt:** eingeschränkter Modus. Es gibt ihn nur bei SSH, weil der Agent als Root-Dienst läuft. Der Modus greift über die sudo-Whitelist.
- **CVE:** Abdeckung des CVE-Scans (Trivy und OSV-Bestand).

## 1. Befunde aus dem Code (develop, 24.11.2026)

1. **Host nur über .deb oder Docker.** `packaging/nfpm.yaml` baut nur `deb` und braucht `adduser`, `sudo` und systemd. Es gibt keine Distributionssperre, jeder apt-Ableger mit systemd installiert also. Ohne apt bleibt nur Docker (Image auf `scratch`). Damit gilt: **LCM-Host = Ubuntu, Debian (und Ableger) oder Docker.**
2. **Clients erkennen den Paketmanager am Programm** (`server_scan.go:269`): apt, dnf, yum, zypper, pacman und apk werden voll bedient. Der Kommentar über `detectPackageManager` behauptet noch, pacman und apk würden nicht bedient. Das ist veraltet.
3. **Agent-Pakete:** Das deb liegt im apt-Kanal. rpm, apk und Arch-Paket hängen am Release, Alpine bekommt einen OpenRC-Dienst.
4. **Support-Ende** (`platform.go`): Hinterlegt sind nur Ubuntu, Debian, RHEL, Rocky, Alma und CentOS Stream. Alle anderen IDs bekommen „keine Aussage“. `ID_LIKE` und `UBUNTU_CODENAME` werden nirgends ausgewertet.
5. **Standard-Firewall** (`firewall_backend.go:107`): Unbekannte IDs bekommen ufw. Kali und Raspbian sollten nftables bekommen. UCS bringt `univention-firewall` mit, die LCM nicht erkennt. Daneben würde ein zweites Werkzeug landen. Proxmox ist abgefangen (pve-firewall).
6. **CVE-Scan meldet bei unbekannter Distribution eine falsche Entwarnung.** `scanServerCVEs` hat keine Prüfung, ob es für die Distribution einen Bestand gibt. Für `kali`, `linuxmint`, `zorin`, `univention`, `raspbian`, `arch` und `opensuse-tumbleweed` läuft der Scan „erfolgreich“, findet 0 Lücken und setzt `last_cve_scan_at` auf jetzt. Bei pacman setzt `purlType` außerdem den Typ `deb`.
7. **Eingeschränkter Modus:** Die sudo-Whitelist kennt alle sechs Paketmanager (`privilege.go:89`). Firewall, Pins und Sicherheitsdienste laufen über den Helper für alle Familien. Die Quellen-Aktionen des Helpers (Quelle anlegen, HTTPS-Umstellung, apt-Cache) gibt es nur für apt.

## 2. Support-Matrix

Spalte „Zuletzt“: letzter belegter Test. 1.8 heißt Langzeittest Run 2 (Juli 2026, nur SSH). Das zählt nicht als „getestet“.

### apt-Distributionen (Server)

| Distribution | Host | SSH | Agent | Eingeschränkt | CVE | Zuletzt |
|---|---|---|---|---|---|---|
| Ubuntu 22.04 / 24.04 / 26.04 | ✅ | ✅ | 🟡 | ✅ | voll | 2.2.x, 21.11. Testbett |
| Debian 12 / 13 | ✅ | ✅ | 🟡 | ✅ | voll | 2.2.x, 21.11. Testbett |
| Debian 11 | ❌ | ❌ | ❌ | ❌ | – | EOL, dokumentiert |
| Proxmox VE 9 | 🟡 | 🟡 | 🟡 | 🟡 | teilweise¹ | SSH 1.8 |
| Proxmox Backup Server 4 | 🟡 | 🟡 | 🟡 | 🟡 | teilweise¹ | SSH 1.8 |
| TurnKey Linux (Debian 13) | 🟡 | 🟡 | 🟡 | 🟡 | voll | nie |
| Kali Linux (rolling) | 🟡 | 🟡 | 🟡 | 🟡 | **keiner, stumm**² | nie |
| Univention Corporate Server 5.2 | 🟡 | 🟡 | 🟡 | 🟡 | **keiner, stumm**² | nie |
| Raspberry Pi OS 64 bit (`ID=debian`) | 🟡 | 🟡 | 🟡 | 🟡 | teilweise³ | nie |
| Raspberry Pi OS 32 bit (`ID=raspbian`) | 🟡 | 🟡 | 🟡 | 🟡 | **keiner, stumm**² | nie |

### apt-Distributionen (Desktop)

| Distribution | Host | SSH | Agent | Eingeschränkt | CVE | Zuletzt |
|---|---|---|---|---|---|---|
| Ubuntu Desktop 24.04 / 26.04 | 🟡 | 🟡 | 🟡 | 🟡 | voll | nie |
| Debian 13 Desktop | 🟡 | 🟡 | 🟡 | 🟡 | voll | nie |
| Linux Mint 22 | 🟡 | 🟡 | 🟡 | 🟡 | **keiner, stumm**⁴ | nie |
| Zorin OS 18 | 🟡 | 🟡 | 🟡 | 🟡 | **keiner, stumm**⁴ | nie |

### Distributionen ohne apt (nur Client)

| Distribution | Host | SSH | Agent | Eingeschränkt | CVE | Zuletzt |
|---|---|---|---|---|---|---|
| RHEL 9 / 10 | ❌ (Docker) | 🟡 | 🟡 | 🟡⁵ | voll | nie |
| AlmaLinux 10 | ❌ (Docker) | 🟡 | 🟡 | 🟡⁵ | voll | SSH 1.8 |
| Rocky Linux 10 | ❌ (Docker) | 🟡 | 🟡 | 🟡⁵ | voll | SSH 1.8 |
| CentOS Stream 10 | ❌ (Docker) | 🟡 | 🟡 | 🟡⁵ | zu prüfen⁶ | SSH 1.8 |
| Fedora 44 Server / Workstation | ❌ (Docker) | 🟡 | 🟡 | 🟡⁵ | zu prüfen⁶ | SSH 1.8 (Server) |
| openSUSE Leap 16 | ❌ (Docker) | 🟡 | 🟡 | 🟡⁵ | voll | SSH 1.8 |
| openSUSE Tumbleweed | ❌ (Docker) | 🟡 | 🟡 | 🟡⁵ | **keiner, stumm**² | nie |
| Arch Linux | ❌ (Docker) | 🟡 | 🟡 | 🟡⁵ | **keiner, stumm**² | SSH 1.8 |
| Alpine 3.23 | ❌ (Docker) | 🟡 | 🟡 | 🟡⁵ | voll | SSH 1.8 |

¹ Debian-Pakete sind abgedeckt, die Proxmox-eigenen Pakete (`pve-*`, `proxmox-*`) nicht.
² Kein Bestand für diese ID. Der Scan meldet heute trotzdem „0 Lücken“ (Befund 6).
³ Debian-Pakete sind abgedeckt, Pakete aus `archive.raspberrypi.com` nicht.
⁴ Könnte über `UBUNTU_CODENAME` auf den Ubuntu-Bestand zeigen. Die Mint- und Zorin-eigenen Pakete blieben dann ohne Abdeckung.
⁵ Quellen-Aktionen des Helpers gibt es nur für apt (Befund 7).
⁶ Trivy kennt die Familie, für OSV gibt es keinen Bestand. Am echten System klären, ob Funde kommen.

Docker als Host: 🟡 auf jeder Distribution mit Docker. Geprüft wird auf Debian 13 und Ubuntu 26.04 (Paket T5).

## 3. Code-Pakete (vor oder parallel zu den Tests)

| Paket | Inhalt | Status |
|---|---|---|
| C1 | CVE-Scan: Distribution ohne Bestand ausdrücklich als „nicht prüfbar“ melden statt 0 Funde und neuer Prüfzeit; pacman bekommt keinen `deb`-purl | offen |
| C2 | `ID_LIKE` / `UBUNTU_CODENAME` als Rückfall für Support-Ende, Firewall und CVE-Zuordnung (Mint, Zorin, Raspbian, Kali) | offen |
| C3 | Support-Ende-Tabellen für Fedora, openSUSE Leap und Alpine; rollende Distributionen (Kali, Arch, Tumbleweed) als „rollend“ kennzeichnen | offen |
| C4 | UCS: `univention-firewall` erkennen und nicht überschreiben | offen |
| C5 | Veralteten Kommentar in `detectPackageManager` korrigieren | offen |
| C6 | Öffentliche Matrix: Abschnitt „Matrix je Distribution“ auf `docs/reference/supported-systems.md` (+ en); nach jedem Testpaket nachziehen | gebaut, wird je Testpaket gepflegt |

## 4. Test-Pakete

Umgebung: pve-test, Distro-Testbett (CT 300 ff.). Snapshots je System: `sauber`; bei Hosts dazu `leer` → `lcm-stable` → `lcm-boot`. LXC, wo es reicht. VM für Desktops, UCS, PVE, PBS und alles mit eigenem Kernel oder eigener Firewall. LCM-Host beim Testen immer im eingeschränkten Modus.

**Prüfliste Host (H):** Installation über `setup.sh` und apt, erster Start und Assistent, Trivy einrichten, Selbstverwaltung, Update auf die nächste Version, Deinstallation.

**Prüfliste SSH (S):** probe und join, Scan (Pakete, Kernel, Dienste, Support-Ende), Updates, Sicherheitsupdates, Einzelpaket und Version fixieren, Firewall an/aus mit dem passenden Werkzeug, Neustart-Erkennung, Benutzer-Sync.

**Prüfliste Eingeschränkt (E):** S wiederholen im eingeschränkten Modus, dazu Quellen-Aktionen und erwartete Ablehnungen.

**Prüfliste Agent (A):** Paket installieren, `enroll`, Scan, Update über den Agenten, Upgrade des Agenten selbst (deb/rpm/apk/pacman-Hooks), Deinstallation.

**Prüfliste CVE (V):** Scan mit bekannt verwundbarem Paket. Erwartung: Fund. Ohne Bestand muss „nicht prüfbar“ kommen.

| Paket | Systeme | Prüflisten | Status |
|---|---|---|---|
| T1 | Agent und CVE auf den vorhandenen Ubuntu- und Debian-Systemen nachholen (Agent bisher nicht belegt) | A, V | offen |
| T2 | Proxmox VE 9, PBS 4 (VM) | H, S, E, A, V | offen |
| T3 | TurnKey (Debian 13), Kali rolling (LXC) | H, S, E, A, V | offen |
| T4 | UCS 5.2 (VM) | H, S, E, A, V | offen |
| T5 | Raspberry Pi OS: x86-Ausgabe als VM, arm64 auf echtem Pi (32 und 64 bit) | H, S, E, A, V | offen |
| T6 | Desktops: Ubuntu 26.04, Debian 13, Mint 22, Zorin 18 (VM) | H, S, E, A, V | offen |
| T7 | Docker-Host auf Debian 13 und Ubuntu 26.04 | H, V | offen |
| T8 | RHEL 10 (Developer-Abo), Alma 10, Rocky 10, CentOS Stream 10 | S, E, A, V | offen |
| T9 | Fedora 44 Server und Workstation | S, E, A, V | offen |
| T10 | openSUSE Leap 16 und Tumbleweed | S, E, A, V | offen |
| T11 | Arch Linux (VM, wegen pacman-Sandbox in LXC) | S, E, A, V | offen |
| T12 | Alpine 3.23 (OpenRC) | S, E, A, V | offen |

Reihenfolge: C1 zuerst, weil die falsche Entwarnung jede CVE-Prüfung entwertet. Danach T1–T7 (Host-fähige Systeme), dann T8–T12. Funde gehen in die Fehlerliste. Die Matrix wird je Paket nachgezogen.

## 5. Ergebnisse

Noch keine.

Stand 26.11.2026: Der Agent ist inzwischen auf apt-, dnf-, zypper-, apk- und pacman-Systemen getestet (24./25.11., siehe `docs/reference/supported-systems.md`). T1 ist damit für den Agenten erledigt, der CVE-Test fehlt noch. Die Matrix oben (Abschnitt 2) ist für den Agenten veraltet, die öffentliche Seite gilt.
