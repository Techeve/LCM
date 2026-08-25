package services

import (
	"errors"
	"fmt"
	"strings"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

var (
	// ErrBlockBuiltin: Mitgelieferte Bausteine werden mit LCM aktualisiert.
	// Wären sie änderbar, ginge die Korrektur beim nächsten Update verloren -
	// oder überschriebe stillschweigend eine gewollte Anpassung. Wer etwas
	// anderes braucht, klont sie.
	ErrBlockBuiltin  = errors.New("mitgelieferte bausteine lassen sich nicht ändern oder löschen - zum Anpassen klonen")
	ErrBlockInUse    = errors.New("der baustein wird von mindestens einem profil verwendet und kann nicht gelöscht werden")
	ErrBlockSlugTake = errors.New("dieser slug ist bereits vergeben")
	ErrBlockNameTake = errors.New("dieser name ist bereits vergeben")
)

// ProfileBlockService verwaltet die Regelbausteine.
type ProfileBlockService struct {
	repo  *repositories.ProfileBlockRepository
	audit *AuditService
}

func NewProfileBlockService(repo *repositories.ProfileBlockRepository, audit *AuditService) *ProfileBlockService {
	return &ProfileBlockService{repo: repo, audit: audit}
}

// BlockInput ist die Eingabe für Anlegen und Ändern.
type BlockInput struct {
	Name        string
	Slug        string // nur beim Anlegen
	Description string
	// Englische Fassung - optional; leer heißt Rückfall auf die deutsche.
	NameEN        string
	DescriptionEN string
	Params        string
	Variants      []domain.ProfileBlockVariant
}

func (s *ProfileBlockService) List() ([]domain.ProfileBlock, error) { return s.repo.FindAll() }

func (s *ProfileBlockService) Get(id uint) (*domain.ProfileBlock, error) { return s.repo.FindByID(id) }

// Usage liefert die Profile, die diesen Baustein verwenden - der
// Verwendungsnachweis vor einer Änderung.
func (s *ProfileBlockService) Usage(id uint) ([]string, error) {
	return s.repo.ProfileNamesUsing(id)
}

func (s *ProfileBlockService) Create(in BlockInput, actor string) (*domain.ProfileBlock, error) {
	if !domain.ValidBlockSlug(in.Slug) {
		return nil, domain.ErrBlockSlugInvalid
	}
	if _, err := s.repo.FindBySlug(in.Slug); err == nil {
		return nil, ErrBlockSlugTake
	} else if !errors.Is(err, repositories.ErrNotFound) {
		return nil, err
	}
	block := &domain.ProfileBlock{Slug: in.Slug}
	if err := s.applyInput(block, in, 0); err != nil {
		return nil, err
	}
	if err := s.repo.Create(block); err != nil {
		return nil, err
	}
	s.audit.Log(actor, "profile-block.create", "profile-block", block.ID, block.Name)
	return s.repo.FindByID(block.ID)
}

func (s *ProfileBlockService) Update(id uint, in BlockInput, actor string) (*domain.ProfileBlock, error) {
	block, err := s.repo.FindByID(id)
	if err != nil {
		return nil, err
	}
	if block.Builtin {
		return nil, ErrBlockBuiltin
	}
	if err := s.applyInput(block, in, id); err != nil {
		return nil, err
	}
	if err := s.repo.Update(block); err != nil {
		return nil, err
	}
	// Wie viele Profile davon berührt sind, gehört in den Audit-Eintrag: Eine
	// Baustein-Änderung verändert Rechte auf allen Servern, die ihn nutzen.
	used, _ := s.repo.ProfileNamesUsing(id)
	s.audit.Log(actor, "profile-block.update", "profile-block", id,
		fmt.Sprintf("%s - verwendet von %d Profil(en): %s", block.Name, len(used), strings.Join(used, ", ")))
	return s.repo.FindByID(id)
}

func (s *ProfileBlockService) Delete(id uint, actor string) error {
	block, err := s.repo.FindByID(id)
	if err != nil {
		return err
	}
	if block.Builtin {
		return ErrBlockBuiltin
	}
	n, err := s.repo.UsageOf(id)
	if err != nil {
		return err
	}
	if n > 0 {
		return ErrBlockInUse
	}
	if err := s.repo.Delete(id); err != nil {
		return err
	}
	s.audit.Log(actor, "profile-block.delete", "profile-block", id, block.Name)
	return nil
}

// Clone legt eine änderbare Kopie eines Bausteins an - der vorgesehene Weg,
// einen mitgelieferten anzupassen.
func (s *ProfileBlockService) Clone(id uint, slug, name, actor string) (*domain.ProfileBlock, error) {
	source, err := s.repo.FindByID(id)
	if err != nil {
		return nil, err
	}
	return s.Create(BlockInput{
		Name: name, Slug: slug, Description: source.Description,
		NameEN: source.NameEN, DescriptionEN: source.DescriptionEN,
		Params: source.Params, Variants: freshVariants(source.Variants),
	}, actor)
}

// freshVariants gibt die Varianten OHNE ihre Schlüssel zurück.
//
// Ohne das schrieb gorm die vorhandenen Zeilen einfach auf den neuen Baustein
// um: Die Kopie bekam die Varianten, das Original stand mit LEEREN Regeln da.
// Ein mitgelieferter Baustein heilte sich beim nächsten Start durch das
// Seeding - ein selbst angelegter blieb kaputt, und die Profile, die ihn
// verwenden, verloren stillschweigend ihre Rechte.
func freshVariants(source []domain.ProfileBlockVariant) []domain.ProfileBlockVariant {
	out := make([]domain.ProfileBlockVariant, 0, len(source))
	for _, v := range source {
		v.ID, v.BlockID = 0, 0
		out = append(out, v)
	}
	return out
}

// applyInput prüft die Eingabe und übernimmt sie.
func (s *ProfileBlockService) applyInput(block *domain.ProfileBlock, in BlockInput, exceptID uint) error {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return domain.ErrBlockNameEmpty
	}
	existing, err := s.repo.FindAll()
	if err != nil {
		return err
	}
	for i := range existing {
		if existing[i].ID != exceptID && strings.EqualFold(existing[i].Name, name) {
			return ErrBlockNameTake
		}
	}
	for _, param := range domain.BlockParamNames(in.Params) {
		if !domain.ValidBlockParamName(param) {
			return domain.ErrBlockParamName
		}
	}
	variants, err := checkedVariants(in.Variants, in.Params)
	if err != nil {
		return err
	}
	block.Name, block.Description, block.Params = name, strings.TrimSpace(in.Description), in.Params
	block.NameEN, block.DescriptionEN = strings.TrimSpace(in.NameEN), strings.TrimSpace(in.DescriptionEN)
	block.Variants = variants
	return nil
}

// checkedVariants prüft jede Variante - mit PROBEWEISE eingesetzten
// Parametern. Anders ginge es nicht: Eine Vorlage mit Platzhaltern ist keine
// gültige Kommandozeile, und ohne Prüfung fiele ein „/usr/bin/systemctl" ohne
// Unteraktion erst auf, wenn es auf den Servern steht.
func checkedVariants(variants []domain.ProfileBlockVariant, params string) ([]domain.ProfileBlockVariant, error) {
	if len(variants) == 0 {
		return nil, domain.ErrBlockNoVariants
	}
	for _, variant := range variants {
		if err := domain.ValidateBlockVariant(variant, params); err != nil {
			return nil, err
		}
	}
	return append([]domain.ProfileBlockVariant(nil), variants...), nil
}

// RenderPreview zeigt, welche Regeln ein Baustein für eine Distributions-
// familie mit den gegebenen Werten ergibt - die Vorschau vor dem Speichern.
func (s *ProfileBlockService) RenderPreview(id uint, family, values string) ([]string, error) {
	block, err := s.repo.FindByID(id)
	if err != nil {
		return nil, err
	}
	variant := block.VariantForFamily(family)
	if variant == nil {
		return nil, fmt.Errorf("für die familie %q ist keine variante hinterlegt - der baustein gilt auf solchen servern nicht", family)
	}
	parsed := domain.ParseBlockValues(values)
	var out []string
	asUserPrefix := ""
	if variant.RunAs != "" && variant.RunAs != "root" {
		asUserPrefix = "als " + variant.RunAs + ": "
	}
	for _, cmd := range domain.BlockLines(variant.SudoCommands) {
		out = append(out, asUserPrefix+domain.NormalizeSudoCommand(domain.SubstituteBlockParams(cmd, parsed)))
	}
	for _, path := range domain.BlockLines(variant.EditPaths) {
		out = append(out, "sudoedit "+domain.SubstituteBlockParams(path, parsed))
	}
	for _, line := range domain.BlockLines(variant.PathRules) {
		out = append(out, "Verzeichnisrecht "+domain.SubstituteBlockParams(line, parsed))
	}
	return out, nil
}
