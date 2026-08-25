package services

import (
	"errors"
	"strconv"
	"strings"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

var (
	ErrCustomActionNameRequired = errors.New("name der custom-aktion ist erforderlich")
	ErrCustomActionEmpty        = errors.New("die custom-aktion enthält kein einziges kommando")
	ErrCustomActionInUse        = errors.New("die custom-aktion wird von mindestens einer rule verwendet und kann nicht gelöscht werden")
)

// CustomActionService verwaltet die benutzerdefinierten Aktionen
// (wiederverwendbare Command-Listen für Gruppen-Rules).
type CustomActionService struct {
	repo  *repositories.CustomActionRepository
	audit *AuditService
}

func NewCustomActionService(repo *repositories.CustomActionRepository, audit *AuditService) *CustomActionService {
	return &CustomActionService{repo: repo, audit: audit}
}

// auditCommandSummary fasst die Kommandos einer Custom Action fürs Audit-Log
// zusammen - bei mehreren als „cmd1 | cmd2 | …", insgesamt gedeckelt, damit
// der Eintrag lesbar bleibt. Das Audit-Log ist die manipulationssichere
// Kette; die Kommandos gehören dort hinein, nicht nur in jobs.output, das
// der Aufbewahrungsfrist unterliegt (R2-064).
func auditCommandSummary(commands string) string {
	cmds := parseActionCommands(commands)
	joined := strings.Join(cmds, " | ")
	const max = 300
	if len(joined) > max {
		joined = joined[:max] + " …"
	}
	return joined
}

// parseActionCommands zerlegt den Command-Text in einzelne Kommandos:
// ein Kommando pro Zeile, ohne Leerzeilen und ohne '#'-Kommentarzeilen.
func parseActionCommands(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		cmd := strings.TrimSpace(line)
		if cmd == "" || strings.HasPrefix(cmd, "#") {
			continue
		}
		out = append(out, cmd)
	}
	return out
}

func (s *CustomActionService) List() ([]domain.CustomAction, error) {
	return s.repo.FindAll()
}

func (s *CustomActionService) Create(name, description, commands, actor string) (*domain.CustomAction, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrCustomActionNameRequired
	}
	if len(parseActionCommands(commands)) == 0 {
		return nil, ErrCustomActionEmpty
	}
	action := &domain.CustomAction{Name: name, Description: description, Commands: commands}
	if err := s.repo.Create(action); err != nil {
		return nil, err
	}
	s.audit.Log(actor, "custom-action.create", "custom-action", action.ID,
		name+" - Kommandos: "+auditCommandSummary(commands))
	return action, nil
}

func (s *CustomActionService) Update(id uint, name, description, commands, actor string) (*domain.CustomAction, error) {
	action, err := s.repo.FindByID(id)
	if err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrCustomActionNameRequired
	}
	if len(parseActionCommands(commands)) == 0 {
		return nil, ErrCustomActionEmpty
	}
	oldCommands := action.Commands
	action.Name = name
	action.Description = description
	action.Commands = commands
	if err := s.repo.Update(action); err != nil {
		return nil, err
	}
	details := name
	if oldCommands != commands {
		details += " - Kommandos vorher: [" + auditCommandSummary(oldCommands) +
			"] nachher: [" + auditCommandSummary(commands) + "]"
	}
	s.audit.Log(actor, "custom-action.update", "custom-action", id, details)
	return action, nil
}

func (s *CustomActionService) Delete(id uint, actor string) error {
	action, err := s.repo.FindByID(id)
	if err != nil {
		return err
	}
	// Nicht löschbar, solange eine Rule sie referenziert.
	n, err := s.repo.CountRulesUsing(id, strconv.FormatUint(uint64(id), 10))
	if err != nil {
		return err
	}
	if n > 0 {
		return ErrCustomActionInUse
	}
	if err := s.repo.Delete(id); err != nil {
		return err
	}
	s.audit.Log(actor, "custom-action.delete", "custom-action", id, action.Name)
	return nil
}
