package repositories

import (
	"strings"

	"gorm.io/gorm"

	"LCM/internal/core/domain"
)

// splitList zerlegt eine Kommaliste in bereinigte, nicht-leere Einträge.
func splitList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// likeContains macht aus einem Suchtext ein LIKE-Muster „enthält", in dem % und
// _ des Suchtexts wörtlich gelten. Die Abfrage braucht dazu ESCAPE '\'.
func likeContains(s string) string {
	escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
	return "%" + escaped + "%"
}

// nameTaken sagt, ob eine andere Zeile von `model` schon diesen Namen trägt
// (ohne Unterschied zwischen Groß- und Kleinschreibung). `exceptID` nimmt beim
// Umbenennen die Zeile selbst aus; 0 beim Anlegen. Der Unique-Index fängt den
// Rest ab, aber erst als rohen Datenbankfehler - die Dienste prüfen vorher, um
// dem Formular eine Fehlermeldung am Feld zu geben.
func nameTaken(db *gorm.DB, model any, name string, exceptID uint) (bool, error) {
	var n int64
	q := db.Model(model).Where("LOWER(name) = LOWER(?)", name)
	if exceptID != 0 {
		q = q.Where("id <> ?", exceptID)
	}
	if err := q.Count(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}

// NameTaken: trägt schon eine andere Gruppe diesen Namen?
func (r *GroupRepository) NameTaken(name string, exceptID uint) (bool, error) {
	return nameTaken(r.db, &domain.ServerGroup{}, name, exceptID)
}

// NameTaken: trägt schon eine andere Eigene Aktion diesen Namen?
func (r *CustomActionRepository) NameTaken(name string, exceptID uint) (bool, error) {
	return nameTaken(r.db, &domain.CustomAction{}, name, exceptID)
}

// NameTaken: trägt schon ein anderer Benachrichtigungskanal diesen Namen?
func (r *NotificationRepository) NameTaken(name string, exceptID uint) (bool, error) {
	return nameTaken(r.db, &domain.NotificationChannel{}, name, exceptID)
}
