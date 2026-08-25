// Package runtimeenv beantwortet eine einzige Frage: Läuft LCM direkt auf
// einem Host oder in einem Container?
//
// Warum das mehr ist als Kosmetik: Im Container heißt „localhost" der
// Container selbst, nicht die Maschine darunter. Alles, was LCM für den
// eigenen Rechner anbietet - Trivy per Knopfdruck installieren, die Sandbox
// nachrüsten, apt-cacher-ng einrichten, sich selbst als Server aufnehmen -
// setzt einen Host mit apt und sshd voraus. Im Container gibt es beides
// nicht. Eine Schaltfläche anzubieten, die dort scheitern MUSS, ist
// schlechter als keine: Sie behauptet eine Möglichkeit, die nicht besteht.
//
// Die Erkennung stand vorher als unexportierte Funktion in der
// Selbstregistrierung, wo sie zuerst gebraucht wurde. Sie gehört keinem
// einzelnen Dienst - deshalb hier, für alle Aufrufer und einmal geprüft.
package runtimeenv

import (
	"os"
	"strings"
	"sync"
)

// Kind ist die Betriebsart.
type Kind string

const (
	// Host: direkt installiert (.deb, systemd) - der Regelfall.
	Host Kind = "host"
	// Docker, Podman, LXC: die erkannten Container-Laufzeiten.
	Docker Kind = "docker"
	Podman Kind = "podman"
	LXC    Kind = "lxc"
	// Container: erkennbar ein Container, aber die Laufzeit gibt sich nicht
	// zu erkennen. Lieber unscharf benennen als falsch raten.
	Container Kind = "container"
)

var (
	once   sync.Once
	cached Kind
)

// Detect meldet die Betriebsart. Sie ändert sich zur Laufzeit nicht - ein
// Container wird kein Host - deshalb wird einmal geprüft und gemerkt.
func Detect() Kind {
	once.Do(func() { cached = detect(system{}) })
	return cached
}

// InContainer ist die Kurzform für den häufigsten Fall.
func InContainer() bool { return Detect() != Host }

// files kapselt die Dateizugriffe der Erkennung. Ohne diese Naht ließe sich
// nur der Fall prüfen, in dem die Tests gerade zufällig laufen: auf einem
// Entwicklerrechner immer „Host", im CI-Container immer „Container" - der
// jeweils andere Zweig nie.
type files interface {
	exists(path string) bool
	read(path string) (string, bool)
}

type system struct{}

func (system) exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func (system) read(path string) (string, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return string(b), true
}

// detect prüft mehrere Merkmale, weil keines für sich allein zuverlässig ist.
// Die eindeutigen Marker zuerst, der unscharfe zuletzt: Ein falsches Negativ
// wäre hier teurer als ein falsches Positiv - es brächte Schaltflächen
// zurück, die im Container ins Leere laufen.
func detect(f files) Kind {
	// Docker legt diese Datei im Container an.
	if f.exists("/.dockerenv") {
		return Docker
	}
	// Podman/CRI-O.
	if f.exists("/run/.containerenv") {
		return Podman
	}
	// LXC und Docker tauchen in den cgroup-Pfaden von PID 1 auf.
	if s, ok := f.read("/proc/1/cgroup"); ok {
		switch {
		case strings.Contains(s, "/docker/"):
			return Docker
		case strings.Contains(s, "/lxc/"):
			return LXC
		}
	}
	// systemd markiert die Umgebung. Der Wert benennt die Laufzeit zwar, ist
	// aber nicht verlässlich gesetzt - hier zählt nur noch das Ob.
	if s, ok := f.read("/proc/1/environ"); ok && strings.Contains(s, "container=") {
		return Container
	}
	return Host
}
