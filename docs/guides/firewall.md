---
sidebar:
  order: 14
title: Firewall (ufw / firewalld / nftables)
description: Multi-Distro-Firewall mit detaillierten Regeln und Port-Vorschlägen.
---

LCM verwaltet die Host-Firewall jedes Servers - mit dem Werkzeug, das zur
Distribution passt, und detaillierten Freigabe-Regeln (Port, TCP/UDP,
IP-Version, erlaubte Quellen, Bemerkung).

## Backend je Distribution

| Distribution | Firewall-Werkzeug |
| --- | --- |
| Ubuntu | **ufw** |
| RHEL, Rocky, AlmaLinux, Fedora, CentOS Stream, openSUSE/SLES | **firewalld** |
| Debian, Arch, Alpine | **nftables** (direkt, LCM-eigene Tabelle) |
| andere Distributionen | ufw (Standard) |

**Konfliktprüfung:** Vor einer Installation prüft LCM immer, ob bereits ein
anderes Firewall-Werkzeug installiert ist. Ist eines vorhanden, wird **dieses**
verwendet - LCM installiert nie eine zweite Firewall neben einer bestehenden
(zwei Paketfilter bekämpfen sich). Ist keines vorhanden, installiert LCM das
vorgesehene Werkzeug automatisch.

## Regeln

Server-Detail → **Firewall**. Jede Regel besteht aus:

- **Port** (1-65535),
- **Protokoll** - TCP oder UDP,
- **IP-Version** - IPv4, IPv6 oder beide,
- **Erlaubte Quellen** (optional) - benannte Allowlists und/oder eigene
  IPs/Netze (CIDR); ohne Auswahl gilt die Freigabe für alle Absender,
- **Bemerkung** (optional) - wofür ist diese Freigabe da. Wo das Werkzeug
  Kommentare kennt (ufw, nftables), steht sie auch im Regelsatz auf dem
  Zielsystem.

:::note[Keine Bind-/Zieladresse mehr]
Bis LCM 1.13.0 gab es zusätzlich eine Bind-/Zieladresse: „auf welcher lokalen
Adresse ist der Port offen". Neben den erlaubten Quellen („von wo darf jemand
zugreifen") stiftete das mehr Verwirrung als Nutzen - beide Felder klangen
gleich und meinten Verschiedenes. Das Feld ist entfallen; die Frage, die man
in der Praxis beantworten will, beantworten die Quellen. Ein „bind" aus einem
älteren gespeicherten Regelsatz wird beim Lesen ignoriert: der Port ist danach
auf allen lokalen Adressen offen, für die Quellen gilt weiter, was dort steht.
:::

Der **SSH-Port ist immer freigegeben** (Aussperr-Schutz); alles andere wird
beim Aktivieren geblockt. Die Anwendung läuft als Job - inklusive
Installation des Werkzeugs, falls nötig, und Verifikation, dass die Firewall
danach wirklich aktiv ist.

### SSH-Regel (nicht löschbar, Quellen einstellbar)

Die SSH-Freigabe wird im Editor als **oberste, gesperrte Zeile** angezeigt -
mit dem tatsächlichen SSH-Port des Servers (auch wenn er von 22 abweicht). Sie
lässt sich **nicht löschen** (sonst würdest du dich aussperren); Port und
Protokoll sind schreibgeschützt. Einstellen lassen sich aber die **erlaubten
Quellen**: damit beschränkst du SSH auf bestimmte Absender-Adressen oder
-Netze. Leer = von überall erreichbar. Die Adresse, über die LCM den Server
erreicht, steht als Vorlage bereit - fehlt sie in der Liste, warnt der Editor,
denn damit sperrst du LCM selbst aus.

### Vorschläge aus dem Port-Scan

Der Hardware-Scan inventarisiert die **lauschenden Dienste** des Servers
(`ss -tulnp`, TCP und UDP, ohne Loopback). Im Firewall-Dialog erscheinen sie
als Vorschlags-Chips - ein Klick übernimmt Port, Protokoll, IP-Version und den
Dienstnamen als Bemerkung in eine Regel.

Von Docker veröffentlichte Ports erscheinen **nicht** als Vorschlag, sondern
nur als Hinweis: Docker trägt seine Weiterleitungen selbst in den Paketfilter
ein und umgeht die Host-Firewall vollständig. Eine Regel darauf ändert an der
Erreichbarkeit nichts - sie würde nur vortäuschen, der Port sei durch LCM
geregelt. Soll ein solcher Port von außen zu sein, gehört die Bindung in die
Container-Veröffentlichung (`127.0.0.1:8080:80` statt `8080:80`).

## Eigenheiten je Backend

- **ufw** - deklarativer Neuaufbau (`ufw --force reset` → Regeln → enable).
  Eine reine IPv4-/IPv6-Freigabe wird über die Zieladresse `0.0.0.0/0` bzw.
  `::/0` ausgedrückt (ufw-Eigenheit); ohne Angabe gilt die Regel für beide.
- **firewalld** - LCM verwaltet die **Default-Zone deklarativ**: alle Ports
  und Rich-Rules der Zone werden neu gesetzt (Freigaben mit Familie oder
  Quell-Einschränkung als Rich-Rules). Zonen-*Services* (z. B. der vorkonfigurierte ssh-Service)
  bleiben unangetastet. Ein einziges `--reload` macht alles zugleich wirksam.
- **nftables** - LCM besitzt eine **eigene Tabelle `inet lcm`** und fasst
  fremde Tabellen (Docker, fail2ban, …) nie an. Der Regelsatz wird atomar
  ersetzt (eine Transaktion, kein regelloses Zeitfenster), vorab per `nft -c`
  geprüft und über `/etc/nftables.d/lcm.nft` + Include persistiert (systemd
  oder OpenRC/Alpine). Deaktivieren entfernt nur die LCM-Tabelle.

:::note[Eingeschränkter Sudo-Modus]
Im eingeschränkten Modus ist nur **ufw** verwaltbar (es steht auf der
sudo-Whitelist). firewalld und nftables brauchen vollen Root-Zugriff
(Dienststeuerung, Dateien unter `/etc`) - LCM meldet das ehrlich statt halb
zu konfigurieren.
:::

## Gruppen-Regel

Unter **Gruppen** lässt sich die Firewall als **Grundsatz-Regel** (Enforce)
definieren - mit demselben Regel-Editor. Bei jeder Verbindung prüft LCM den
Ist-Zustand (billiger Hash-/Set-Vergleich je Backend) und wendet den Regelsatz
nur bei Abweichung neu an; ein fehlendes Werkzeug wird dabei installiert.
Bestehende Regeln im alten Format (Portliste `80,443`) laufen unverändert
weiter.

:::note[Docker & Proxmox]
Von Docker **veröffentlichte Container-Ports** laufen an der Host-Firewall
vorbei (Docker filtert vor der INPUT-Kette - bei allen drei Backends). LCM
zeigt diese Ports am Server ehrlich an, greift aber bewusst nicht ein.
**Proxmox**-Systeme sind ausgenommen: dort verwaltet pve-firewall die Regeln.
:::

## Quell-Einschränkung: Allowlists und eigene IPs

Je Regel lassen sich die **erlaubten Quellen** einschränken - auf zwei Wegen,
die sich frei kombinieren lassen:

- **[IP-Allowlists](/guides/allowlists)** (gemeinsamer Pool): eine oder
  mehrere benannte Listen per Mehrfachauswahl wählen.
- **Eigene IPs/Netze**: einzelne IPv4-/IPv6-Adressen oder CIDR-Netze direkt
  in die Regel eintragen (komma- oder leerzeichengetrennt), z. B.
  `203.0.113.7, 10.0.0.0/24` - ohne dafür eine benannte Liste anzulegen.

Ist mindestens eine Quelle gesetzt, gibt die Regel den Port **nur für die
Vereinigung dieser Quell-IPs** frei - alle anderen Quellen werden geblockt.
Ohne Quellen gilt die Regel wie bisher für alle. Umgesetzt als
`ufw allow … from …`, firewalld `source address="…"` bzw. nftables
`ip/ip6 saddr { … }`. Ändern sich die Listeninhalte, wendet die Firewall-
Grundsatz-Regel den Regelsatz beim nächsten Lauf automatisch neu an.

:::caution[Leere Quellen öffnen nie]
Löst die Quell-Einschränkung einer Regel zu **null IPs** auf (etwa weil die
referenzierte Allowlist geleert oder gelöscht wurde), wird die Regel beim
Anwenden **ausgelassen** - der Port bleibt zu. LCM macht aus einer
eingeschränkten Freigabe niemals stillschweigend eine offene.
:::

## Gerenderte Regel-Beispiele

Damit greifbar wird, was LCM je Backend tatsächlich auf den Server schreibt,
hier dieselbe Regel-Absicht in allen drei Werkzeugen. Jedes Beispiel zeigt
die Zeile(n), die aus **einer** Regel im Editor entstehen.

### Ein einfacher Port (HTTP 80/tcp, alle Adressen, alle Quellen)

Regel: Port `80`, TCP, IP-Version *beide*, keine Quell-Einschränkung.

| Backend | Gerenderte Regel |
| --- | --- |
| ufw | `ufw allow proto tcp to any port 80 comment 'lcm'` |
| firewalld | `firewall-cmd --permanent --zone=<zone> --add-port=80/tcp` (einfache Freigabe, keine Rich-Rule) |
| nftables | `tcp dport 80 accept` |

### Nur IPv4 bzw. nur IPv6 (HTTPS 443/tcp)

Regel: Port `443`, TCP, IP-Version *IPv4* (bzw. *IPv6*).

| Backend | nur IPv4 | nur IPv6 |
| --- | --- | --- |
| ufw | `ufw allow proto tcp to 0.0.0.0/0 port 443 comment 'lcm'` | `ufw allow proto tcp to ::/0 port 443 comment 'lcm'` |
| firewalld | `rule family="ipv4" port port="443" protocol="tcp" accept` | `rule family="ipv6" port port="443" protocol="tcp" accept` |
| nftables | `meta nfproto ipv4 tcp dport 443 accept` | `meta nfproto ipv6 tcp dport 443 accept` |

ufw hat keine explizite Familien-Option und drückt eine reine v4-/v6-Freigabe
deshalb über die Wildcard-Zieladresse `0.0.0.0/0` bzw. `::/0` aus - ohne
Zieladresse gilt die Regel für beide Familien.

### Quell-Einschränkung (nur bestimmte Absender)

Regel: Port `443`, TCP, Quelle `203.0.113.7` (aus einer Allowlist oder als
eigene IP eingetragen).

| Backend | Gerenderte Regel |
| --- | --- |
| ufw | `ufw allow proto tcp from 203.0.113.7 to any port 443 comment 'lcm'` |
| firewalld | `rule family="ipv4" source address="203.0.113.7" port port="443" protocol="tcp" accept` |
| nftables | `ip saddr { 203.0.113.7 } tcp dport 443 accept` |

Mehrere Quellen ergeben bei **ufw** je Quelle eine eigene Zeile, bei
**firewalld** je Quelle (nach Familie getrennt) eine eigene Rich-Rule und bei
**nftables** ein zusammengefasstes Set (`ip saddr { a, b, c }`).

### Gemischt IPv4 + IPv6 in einer Regel

Regel: Port `443`, TCP, IP-Version *beide*, Quellen `203.0.113.7` **und**
`2001:db8:acab::1` (etwa eine Allowlist mit v4- und v6-Einträgen). LCM trennt
die Quellen sauber nach Adressfamilie - jede Zeile passt zu ihrer Familie:

| Backend | Gerenderte Regel(n) |
| --- | --- |
| ufw | `... from 203.0.113.7 to any port 443 ...` **und** `... from 2001:db8:acab::1 to any port 443 ...` |
| firewalld | `rule family="ipv4" source address="203.0.113.7" ...` **und** `rule family="ipv6" source address="2001:db8:acab::1" ...` |
| nftables | `ip saddr { 203.0.113.7 } tcp dport 443 accept` **und** `ip6 saddr { 2001:db8:acab::1 } tcp dport 443 accept` |

## Warum es keine Zieladresse gibt

Ein Paketfilter kennt zwei unabhängige Achsen, und sie werden leicht
verwechselt:

- **Zieladresse** (`to …` / `destination address` / `daddr`): auf **welcher
  lokalen Adresse** des Servers der Port überhaupt geöffnet wird.
- **Quelle** (`from …` / `source address` / `saddr`): **wer** (welche
  Absender-IP, welches Absender-Netz) auf den Port zugreifen darf.

LCM bietet nur noch die Quelle an. Der Grund ist die Praxis: Wer einen Dienst
„nur intern" erreichbar machen will, meint fast immer „nur aus dem internen
Netz" - also die Quelle. Zwei ähnlich klingende Felder nebeneinander führten
regelmäßig dazu, dass das falsche gefüllt wurde, mit dem Ergebnis, dass ein
Port für alle offen stand, obwohl er eingeschränkt aussah.

Wirklich verloren geht dabei nur ein Sonderfall: derselbe Absender darf einen
Dienst auf der einen lokalen Adresse erreichen, auf der anderen nicht. Wer das
braucht, bindet den Dienst selbst an die gewünschte Adresse (in seiner
Konfiguration) - das ist ohnehin die robustere Stelle dafür, weil sie auch
ohne Firewall gilt.
