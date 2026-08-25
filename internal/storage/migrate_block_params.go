package storage

import (
	"strings"

	"gorm.io/gorm"

	"LCM/internal/core/domain"
)

// englishBlockParams ist die Umbenennung der Baustein-Platzhalter auf
// englische Namen. Die übrigen Namen des mitgelieferten Katalogs (container,
// ip, addons, portal) waren es schon.
var englishBlockParams = map[string]string{
	"dienst":        "service",
	"pfad":          "path",
	"schnittstelle": "interface",
	"instanz":       "instance",
}

// migrateBlockParamsToEnglish zieht den Bestand nach. Der Name eines
// Parameters steht an DREI Stellen, und alle drei müssen zusammen wandern:
// in der Parameterliste des Bausteins, als {name} in seinen Regeln und als
// „name=wert" in jeder Verwendung, die ihn in ein Profil einhängt.
//
// Bliebe die dritte Stelle stehen, wäre der Schaden lautlos: Der Platzhalter
// bekäme keinen Wert, die Regel fiele bei der Prüfung durch, und das Profil
// verlöre genau diese Rechte - ohne dass irgendwo etwas fehlt. Deshalb läuft
// alles über die Bausteine statt über ein pauschales UPDATE.
//
// Die mitgelieferten Bausteine würden ihre Parameter zwar auch beim Seeden
// bekommen, ihre Verwendungen aber nicht. Und Klone eines mitgelieferten
// Bausteins sind selbst angelegt - die erreicht das Seeden gar nicht.
//
// Idempotent: Nach dem Lauf trägt kein Baustein mehr einen deutschen
// Parameternamen, der zweite Lauf findet nichts mehr.
func migrateBlockParamsToEnglish(db *gorm.DB) error {
	var blocks []domain.ProfileBlock
	if err := db.Preload("Variants").Find(&blocks).Error; err != nil {
		return err
	}
	for i := range blocks {
		if err := renameBlockParams(db, &blocks[i]); err != nil {
			return err
		}
	}
	return db.Exec(
		`UPDATE app_catalog_entries SET version_command = REPLACE(version_command, '{pfad}', '{path}')
		 WHERE version_command LIKE '%{pfad}%'`).Error
}

// renameBlockParams benennt die deutschen Parameter eines Bausteins um. Ist
// der englische Name in demselben Baustein schon vergeben, bleibt dieser
// Parameter unangetastet: Zwei Platzhalter zu einem zu verschmelzen würde
// stillschweigend andere Regeln erzeugen, und ein unveränderter Baustein
// funktioniert weiter wie bisher.
func renameBlockParams(db *gorm.DB, block *domain.ProfileBlock) error {
	names := domain.BlockParamNames(block.Params)
	taken := map[string]bool{}
	for _, name := range names {
		taken[name] = true
	}

	renames := map[string]string{}
	for _, name := range names {
		if english, ok := englishBlockParams[name]; ok && !taken[english] {
			renames[name] = english
		}
	}
	if len(renames) == 0 {
		return nil
	}

	for i, name := range names {
		if english, ok := renames[name]; ok {
			names[i] = english
		}
	}
	if err := db.Model(&domain.ProfileBlock{}).Where("id = ?", block.ID).
		Update("params", strings.Join(names, ",")).Error; err != nil {
		return err
	}

	for _, variant := range block.Variants {
		if err := db.Model(&domain.ProfileBlockVariant{}).Where("id = ?", variant.ID).
			Updates(map[string]any{
				"sudo_commands": renamePlaceholders(variant.SudoCommands, renames),
				"edit_paths":    renamePlaceholders(variant.EditPaths, renames),
				"path_rules":    renamePlaceholders(variant.PathRules, renames),
			}).Error; err != nil {
			return err
		}
	}

	var uses []domain.ProfileBlockUse
	if err := db.Where("block_id = ?", block.ID).Find(&uses).Error; err != nil {
		return err
	}
	for _, use := range uses {
		if err := db.Model(&domain.ProfileBlockUse{}).Where("id = ?", use.ID).
			Update("values", renameValueKeys(use.Values, renames)).Error; err != nil {
			return err
		}
	}
	return nil
}

// renamePlaceholders tauscht {alt} gegen {neu} im Regeltext.
func renamePlaceholders(text string, renames map[string]string) string {
	for old, english := range renames {
		text = strings.ReplaceAll(text, "{"+old+"}", "{"+english+"}")
	}
	return text
}

// renameValueKeys tauscht den Namen links vom Gleichheitszeichen. Nur dort -
// ein Wert, der zufällig „pfad" heißt, geht die Umbenennung nichts an.
func renameValueKeys(values string, renames map[string]string) string {
	lines := strings.Split(values, "\n")
	for i, line := range lines {
		name, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		if english, ok := renames[strings.TrimSpace(name)]; ok {
			lines[i] = english + "=" + value
		}
	}
	return strings.Join(lines, "\n")
}
