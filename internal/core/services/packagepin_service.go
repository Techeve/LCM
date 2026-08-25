package services

import (
	"errors"
	"fmt"
	"strings"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

// Paket-Pins: Schutz vor dem Aufraeumen (Autoremove) und optionale
// Versions-Fixierung - je Server und global.
//
// Der Anlass ist der Kernel: `apt autoremove` raeumt aeltere Kernel weg,
// sobald sie keine Abhaengigkeit mehr haben. Wer wie Proxmox mehrere Kernel
// als Rueckfallebene behalten will, braucht dafuer eine ausdrueckliche
// Schutzliste. Die Pins sind genau das - und wirken zusaetzlich als
// allgemeiner „nie entfernen"-Schalter fuer beliebige Pakete.

var (
	// ErrPackagePinsUnavailable: Auf Proxmox-Systemen ist das Feature
	// ausgenommen. Proxmox pflegt seine Kernel-Aufbewahrung selbst
	// (proxmox-boot-tool / eigene Meta-Pakete); eine zweite Schutzliste
	// daneben wuerde die beiden Mechanismen gegeneinander laufen lassen.
	ErrPackagePinsUnavailable = errors.New("paket-pins sind auf proxmox-systemen ausgenommen - proxmox verwaltet die kernel-aufbewahrung selbst")
	// ErrPackagePinName: leerer oder unzulaessiger Pin-Name.
	ErrPackagePinName = errors.New("ungueltiger paketname fuer den pin (erlaubt: a-z, 0-9, . _ + - und ein abschliessendes *)")
	// ErrPackagePinsNotWired: der Pin-Speicher ist nicht verdrahtet (schlanke
	// Tests) - die Aktion ist dann nicht verfuegbar.
	ErrPackagePinsNotWired = errors.New("paket-pins sind in dieser instanz nicht verfuegbar")
	// ErrPackagePinEffect: ein Pin ohne Wirkung waere ein stiller No-op.
	ErrPackagePinEffect = errors.New("ein pin braucht mindestens eine wirkung (nicht entfernen und/oder version einfrieren)")
	// ErrPinnedPackage: das Paket ist per Pin vor dem Entfernen geschuetzt.
	// Bewusst getrennt von ErrProtectedPackage (fest eingebauter Schutz) -
	// diesen hier kann der Anwender selbst aufheben.
	ErrPinnedPackage = errors.New("dieses paket ist per pin vor dem entfernen geschuetzt - zuerst den pin loesen")
)

// WithPackagePins verdrahtet den Pin-Speicher. Optional - ohne ihn melden die
// Pin-Aktionen ErrPackagePinsNotWired, statt zu paniken.
func (s *ServerService) WithPackagePins(repo *repositories.PackagePinRepository) *ServerService {
	s.pins = repo
	return s
}

// ensurePinsAvailable prueft die Voraussetzungen fuer alle Pin-Aktionen.
func (s *ServerService) ensurePinsAvailable(server *domain.Server) error {
	if s.pins == nil {
		return ErrPackagePinsNotWired
	}
	if err := ensureNotRouterOS(server); err != nil {
		return err
	}
	if server.IsProxmox() {
		return ErrPackagePinsUnavailable
	}
	return nil
}

// validPinName prueft den Paketnamen eines Pins. Der Name landet in
// Konfigurationsdateien und in Shell-Kommandos auf dem Ziel - hier wird er
// deshalb streng gefiltert, nicht nur oberflaechlich getrimmt. Ein
// abschliessendes '*' ist als Praefix-Muster erlaubt (linux-image-*).
func validPinName(name string) (string, error) {
	n := strings.TrimSpace(strings.ToLower(name))
	if n == "" || len(n) > 128 {
		return "", ErrPackagePinName
	}
	base := strings.TrimSuffix(n, "*")
	if base == "" {
		return "", ErrPackagePinName
	}
	for _, r := range base {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case r == '.', r == '_', r == '+', r == '-':
		default:
			return "", ErrPackagePinName
		}
	}
	return n, nil
}

// PackagePinView buendelt die Pins fuer die Oberflaeche: die globalen, die
// serverspezifischen und den Hinweis, ob das Feature hier ueberhaupt greift.
type PackagePinView struct {
	Global     []domain.PackagePin `json:"global"`
	Server     []domain.PackagePin `json:"server"`
	Available  bool                `json:"available"`
	Reason     string              `json:"reason,omitempty"`
	PkgManager string              `json:"package_manager"`
	// KernelPreset sind die Vorschlaege des Ein-Klick-Kernelschutzes fuer die
	// Paketverwaltung DIESES Servers (leer = fuer diese Verwaltung sinnlos).
	KernelPreset []string `json:"kernel_preset"`
}

// ListPackagePins liefert die Pin-Sicht eines Servers.
func (s *ServerService) ListPackagePins(scope repositories.AccessScope, id uint) (*PackagePinView, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return nil, err
	}
	view := &PackagePinView{
		Available:    true,
		PkgManager:   server.PackageManager,
		KernelPreset: domain.KernelPinPresets[pkgFamily(server.PackageManager)],
		Global:       []domain.PackagePin{},
		Server:       []domain.PackagePin{},
	}
	if err := s.ensurePinsAvailable(server); err != nil {
		view.Available = false
		view.Reason = err.Error()
		// Die gespeicherten Pins trotzdem zeigen, wenn es sie gibt - sonst
		// waere unklar, warum sie auf anderen Servern wirken.
		if s.pins == nil {
			return view, nil
		}
	}
	all, err := s.pins.ListForServer(id)
	if err != nil {
		return nil, err
	}
	for _, p := range all {
		if p.IsGlobal() {
			view.Global = append(view.Global, p)
		} else {
			view.Server = append(view.Server, p)
		}
	}
	return view, nil
}

// PackagePinInput sind die Eingaben zum Anlegen eines Pins.
type PackagePinInput struct {
	Name     string
	NoRemove bool
	Hold     bool
	Note     string
	// Global: true legt einen Pin fuer ALLE Server an (ServerID 0).
	Global bool
}

// CreatePackagePin legt einen Pin an (global oder fuer diesen Server).
// Der Server dient auch im globalen Fall als Berechtigungs- und
// Machbarkeitsanker: Wer den Server nicht sehen darf, legt hier nichts an.
func (s *ServerService) CreatePackagePin(scope repositories.AccessScope, id uint, in PackagePinInput, actor string) (*domain.PackagePin, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return nil, err
	}
	if err := s.ensurePinsAvailable(server); err != nil {
		return nil, err
	}
	name, err := validPinName(in.Name)
	if err != nil {
		return nil, err
	}
	if !in.NoRemove && !in.Hold {
		return nil, ErrPackagePinEffect
	}
	pin := &domain.PackagePin{
		Name: name, NoRemove: in.NoRemove, Hold: in.Hold,
		Note: strings.TrimSpace(in.Note),
	}
	if !in.Global {
		pin.ServerID = server.ID
	}
	if err := s.pins.Create(pin); err != nil {
		return nil, err
	}
	kind := "global"
	if !in.Global {
		kind = server.Name
	}
	s.audit.Log(actor, "server.package-pin-create", "server", id,
		fmt.Sprintf("%s: %s (nicht entfernen=%v, version einfrieren=%v)", kind, name, in.NoRemove, in.Hold))
	return pin, nil
}

// DeletePackagePin entfernt einen Pin. Der Server ist auch hier der
// Berechtigungsanker; ein serverspezifischer Pin muss zu ihm gehoeren.
func (s *ServerService) DeletePackagePin(scope repositories.AccessScope, id, pinID uint, actor string) error {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return err
	}
	if s.pins == nil {
		return ErrPackagePinsNotWired
	}
	pin, err := s.pins.FindByID(pinID)
	if err != nil {
		return err
	}
	if !pin.IsGlobal() && pin.ServerID != server.ID {
		return repositories.ErrNotFound
	}
	if err := s.pins.Delete(pinID); err != nil {
		return err
	}
	s.audit.Log(actor, "server.package-pin-delete", "server", id, pin.Name)
	return nil
}

// PinKernelPreset legt den Ein-Klick-Kernelschutz an: die Kernel-Muster der
// Paketverwaltung dieses Servers, bewusst als NoRemove (nicht Hold). Der Zweck
// ist, aeltere Kernel als Rueckfallebene zu BEHALTEN - nicht, neue zu
// verhindern. Ein Hold auf dem Kernel wuerde Sicherheitsupdates blockieren.
func (s *ServerService) PinKernelPreset(scope repositories.AccessScope, id uint, global bool, actor string) ([]domain.PackagePin, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return nil, err
	}
	if err := s.ensurePinsAvailable(server); err != nil {
		return nil, err
	}
	presets := domain.KernelPinPresets[pkgFamily(server.PackageManager)]
	if len(presets) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrPackagePinName, PackageManagerLabel(server.PackageManager))
	}
	var created []domain.PackagePin
	for _, name := range presets {
		pin := &domain.PackagePin{
			Name: name, NoRemove: true, Hold: false,
			Note: "Kernel-Rückfallebene (LCM-Vorgabe)",
		}
		if !global {
			pin.ServerID = server.ID
		}
		if err := s.pins.Create(pin); err != nil {
			return nil, err
		}
		created = append(created, *pin)
	}
	scopeName := "global"
	if !global {
		scopeName = server.Name
	}
	s.audit.Log(actor, "server.package-pin-kernel", "server", id,
		scopeName+": "+strings.Join(presets, ", "))
	return created, nil
}

// effectivePins liefert die auf DIESEM Server wirksamen Pins (global +
// serverspezifisch). Ohne verdrahteten Speicher: leer, damit alle
// paketbezogenen Jobs auch in schlanken Tests laufen.
func (s *ServerService) effectivePins(server *domain.Server) []domain.PackagePin {
	if s.pins == nil || server.IsProxmox() {
		return nil
	}
	pins, err := s.pins.ListForServer(server.ID)
	if err != nil {
		return nil
	}
	return pins
}

// ApplyPackagePins schreibt die wirksamen Pins auf dem Server fest
// (Schutzdateien und Holds je Paketverwaltung). Asynchroner Job.
func (s *ServerService) ApplyPackagePins(scope repositories.AccessScope, id uint, actor string) (*domain.Job, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return nil, err
	}
	if err := s.ensurePinsAvailable(server); err != nil {
		return nil, err
	}
	// Schutzdateien liegen unter /etc - das geht ueber die sudo-Whitelist des
	// eingeschraenkten Modus hinaus.
	if err := ensureFullSudo(server); err != nil {
		return nil, err
	}
	pins := s.effectivePins(server)
	build := func(mgr string) string { return pkgPinScript(mgr, pins) }
	return s.startPackageJob(scope, id, domain.RuleTypePackageScan,
		"Paket-Pins anwenden", build, actor)
}
