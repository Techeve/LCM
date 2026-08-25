package domain

import (
	"strings"
	"time"
)

// PackagePin schuetzt ein Paket vor dem Aufraeumen und/oder friert seine
// Version ein. Die Pins sind der Gegenpol zum Autoremove: Ohne sie entfernt
// `apt autoremove` und Co. alles, was keine andere Abhaengigkeit mehr braucht
// - darunter regelmaessig aeltere Kernel.
//
// Zwei bewusst getrennte Wirkungen, weil sie unterschiedliche Werkzeuge
// brauchen und unterschiedliche Folgen haben:
//
//   - NoRemove: Das Paket darf nicht entfernt werden, bekommt aber weiter
//     Updates. Das ist der richtige Schutz fuer Kernel - alte Versionen
//     bleiben liegen, neue kommen trotzdem an.
//   - Hold: Die installierte Version wird eingefroren, es gibt keine Updates
//     mehr. Nuetzlich fuer Anwendungen mit heikler Versionsbindung, fuer
//     Kernel dagegen gefaehrlich (keine Sicherheitsupdates mehr).
//
// Ein Pin mit ServerID == 0 ist global und gilt fuer ALLE Server; ein Pin mit
// gesetzter ServerID gilt nur dort. Bei der Anwendung werden beide Mengen
// vereinigt - der globale Kernel-Schutz laesst sich also einmal setzen und
// je Server um Sonderfaelle ergaenzen.
type PackagePin struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// ServerID: 0 = global (alle Server), sonst der betroffene Server.
	// Der Index deckt beide Abfragen ab (global laden, je Server laden).
	ServerID uint `gorm:"index;not null;default:0" json:"server_id"`

	// Name ist der Paketname. Ein abschliessendes '*' ist erlaubt und wirkt
	// als Praefix-Muster (z.B. "linux-image-*") - nur so laesst sich eine
	// ganze Kernel-Reihe in einem Eintrag erfassen.
	Name string `gorm:"not null" json:"name"`

	// NoRemove schuetzt vor Autoremove und gezieltem Entfernen.
	NoRemove bool `gorm:"not null;default:true" json:"no_remove"`
	// Hold friert die Version ein (kein Upgrade mehr).
	Hold bool `gorm:"not null;default:false" json:"hold"`

	// Note haelt fest, warum der Pin existiert - nach einem halben Jahr
	// weiss sonst niemand mehr, ob er noch gebraucht wird.
	Note string `json:"note"`
}

// IsGlobal meldet, ob der Pin fuer alle Server gilt.
func (p PackagePin) IsGlobal() bool { return p.ServerID == 0 }

// IsPattern meldet, ob der Name ein Praefix-Muster ist (endet auf '*').
func (p PackagePin) IsPattern() bool { return strings.HasSuffix(p.Name, "*") }

// Prefix liefert bei einem Muster den Teil vor dem '*', sonst den Namen
// selbst. Fuer den Vergleich mit konkreten Paketnamen.
func (p PackagePin) Prefix() string { return strings.TrimSuffix(p.Name, "*") }

// Matches meldet, ob ein konkreter Paketname von diesem Pin erfasst wird.
// Muster greifen als Praefix, alles andere exakt.
func (p PackagePin) Matches(pkg string) bool {
	if p.IsPattern() {
		return strings.HasPrefix(pkg, p.Prefix())
	}
	return pkg == p.Name
}

// KernelPinPresets sind die Vorschlaege des Ein-Klick-Kernelschutzes, nach
// Paketverwaltung getrennt. Sie decken die Kernel-Pakete der jeweiligen
// Distributionsfamilie ab und sind bewusst NoRemove (nicht Hold): Der Zweck
// ist, alte Kernel als Rueckfallebene zu behalten, nicht neue zu verhindern.
//
// Hinweis zu dnf/yum: Dort regelt zusaetzlich `installonly_limit` in
// /etc/dnf/dnf.conf, wie viele Kernel behalten werden (Standard 3). Der Pin
// wirkt dort ergaenzend ueber die Schutzliste von LCM.
var KernelPinPresets = map[string][]string{
	"apt":    {"linux-image-*", "linux-headers-*"},
	"dnf":    {"kernel", "kernel-core", "kernel-modules"},
	"zypper": {"kernel-default", "kernel-firmware"},
	"pacman": {"linux", "linux-lts", "linux-headers"},
	// apk kennt kein Autoremove und keine Kernel-Reihen im gleichen Sinn.
	"apk": nil,
}
