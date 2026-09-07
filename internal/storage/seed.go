package storage

import (
	"errors"
	"fmt"

	"gorm.io/gorm"

	"LCM/internal/config"
	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/i18n"
	"LCM/internal/storage/repositories"
)

// Seed legt beim ersten Start (leere User-Tabelle) die Basisdaten an:
// die Standard-User "system" und "admin", die globalen Einstellungen sowie
// die geschützte System-Gruppe mit den serverbezogenen Basis-Schedules
// (Health-Check, System-Sync). Permissions und die Standard-Rollen
// (admin/manager/system) werden dagegen bei JEDEM Start abgeglichen -
// so bekommen bestehende Datenbanken neue Permission-Codes automatisch
// nachgezogen (Upgrade-Pfad). Im Demo-Modus werden zusätzlich Testdaten
// (Server, Pakete, Job-Historien, LCM-Manager + Linux-Benutzer) angelegt.
// Der Aufruf ist idempotent.
func Seed(db *gorm.DB, cfg *config.Config) error {
	userRepo := repositories.NewUserRepository(db)
	roleRepo := repositories.NewRoleRepository(db)

	// Die Settings-Zeile (ID 1) muss immer existieren - auch nach Updates.
	if err := ensureGlobalSettings(db, cfg); err != nil {
		return err
	}

	// Permissions + Rollen-Zuordnung immer synchronisieren (Migration für
	// bestehende Datenbanken, z.B. nachträglich eingeführte linuxusers:*).
	adminRole, _, systemRole, err := syncRolesAndPermissions(roleRepo)
	if err != nil {
		return err
	}

	// Neue System-Rules in bestehende DBs nachziehen (z.B. die tägliche
	// Paketlisten-Aktualisierung). Auf einer frischen DB ein No-op.
	if err := ensureSystemSyncRules(db); err != nil {
		return err
	}

	// Katalog bekannter Paketquellen: mitgelieferte Einträge nachziehen,
	// ohne Nutzer-Änderungen zu überschreiben.
	if err := ensureKnownRepos(db); err != nil {
		return err
	}

	// Mitgelieferte Berechtigungsprofile - auch in bestehenden Datenbanken,
	// damit sich jeder vorhandene Linux-Benutzer einem Profil zuordnen lässt.
	if err := ensurePrivilegeProfiles(db); err != nil {
		return err
	}
	// Mitgelieferte Regelbausteine.
	if err := ensureProfileBlocks(db); err != nil {
		return err
	}
	if err := ensureAppEntries(db); err != nil {
		return err
	}

	count, err := userRepo.Count()
	if err != nil {
		return err
	}
	if count > 0 {
		// Bereits initialisiert - nur noch Bestandsbenutzer ohne Profil
		// nachziehen (siehe Kommentar am Ende der Funktion).
		return migrateLinuxUserProfiles(db)
	}

	// 3. Standard-User
	// System-User: kann sich nie einloggen (IsSystem), Zufalls-"Passwort"
	// als Platzhalter, da das Feld not-null ist.
	systemHash, err := services.HashPassword(config.RandomSecret(32))
	if err != nil {
		return err
	}
	systemUser := domain.User{
		Username:     domain.SystemUsername,
		Email:        "system@localhost",
		PasswordHash: systemHash,
		FirstName:    "System",
		IsSystem:     true,
		Active:       true,
		Roles:        []domain.Role{*systemRole},
	}
	if err := userRepo.Create(&systemUser); err != nil {
		return fmt.Errorf("system-user: %w", err)
	}

	adminPassword := cfg.AdminInitialPassword
	generated := false
	if adminPassword == "" {
		adminPassword = config.RandomSecret(12)
		generated = true
	}
	adminHash, err := services.HashPassword(adminPassword)
	if err != nil {
		return err
	}
	adminUser := domain.User{
		Username:     domain.AdminUsername,
		Email:        "admin@localhost",
		PasswordHash: adminHash,
		FirstName:    "Administrator",
		Active:       true,
		// Sicherheitsvorgabe: Das initiale Passwort MUSS beim ersten Login
		// geändert werden (serverseitig erzwungen). Im Demo-Modus bewusst
		// AUS, damit der Admin die UI ohne Reibung erkunden kann.
		MustChangePassword: !cfg.DemoMode,
		Roles:              []domain.Role{*adminRole},
	}
	if err := userRepo.Create(&adminUser); err != nil {
		return fmt.Errorf("admin-user: %w", err)
	}
	if generated {
		// Einmalige Ausgabe - danach nirgends mehr einsehbar.
		//
		// Die feste Kennzeile "Admin account" bleibt in JEDER Sprache stehen:
		// Die Installationsanleitung verweist zum Wiederfinden des Passworts
		// auf `journalctl -u lcm | grep -A3 'Admin account'` - ein übersetzter
		// Suchbegriff würde diese Anleitung auf deutschen Systemen brechen.
		fmt.Printf("\n========================================================\n")
		fmt.Printf("  Admin account created (first start)\n")
		fmt.Printf("  %s: %s\n", i18n.T("User", "Benutzer"), domain.AdminUsername)
		fmt.Printf("  %s: %s\n", i18n.T("Password", "Passwort"), adminPassword)
		fmt.Printf("  (%s)\n", i18n.T(
			"Note it down - it must be changed at first login.",
			"Bitte notieren - muss beim ersten Login geändert werden."))
		fmt.Printf("========================================================\n\n")
	}

	// 4. System-Gruppe mit den nicht löschbaren Basis-Schedules.
	if err := seedSystemGroup(db); err != nil {
		return err
	}

	// 5. Demo-Daten (nur im Demo-Modus): Test-Server, Pakete, Hardware,
	//    Job-Historien und Fake-User zum sofortigen Ausprobieren der UI.
	if cfg.DemoMode {
		if err := seedDemo(db, roleRepo); err != nil {
			return fmt.Errorf("demo-daten seeden: %w", err)
		}
	}

	// 6. Linux-Benutzer ohne Berechtigungsprofil den eingebauten zuordnen
	//    („sudo" → Voll-Administrator, sonst Standardbenutzer).
	//
	//    Bewusst ganz am Ende: Die Demo-Daten schreiben ihre Benutzer direkt
	//    in die Tabelle, am Service vorbei. Lief die Zuordnung davor, standen
	//    genau sie ohne Profil da - und die Übersicht zeigte bei „Rechte"
	//    einen Strich, obwohl der Benutzer sehr wohl sudo hatte.
	return migrateLinuxUserProfiles(db)
}

// syncRolesAndPermissions gleicht Permissions und die Standard-Rollen bei
// jedem Start mit dem Code-Stand ab: Neue Permission-Codes werden angelegt
// und die Permission-Sets von admin (alles), manager (ManagerPermissions)
// und system (keine) auf den kanonischen Stand gesetzt. Dadurch bekommen
// bestehende Installationen nachträglich eingeführte Rechte (z.B.
// linuxusers:*) automatisch - ohne manuelles Datenbank-Update.
func syncRolesAndPermissions(roleRepo *repositories.RoleRepository) (admin, manager, system *domain.Role, err error) {
	permDefs := map[string]string{
		domain.PermUsersRead:       "User auflisten und lesen",
		domain.PermUsersWrite:      "User anlegen, bearbeiten und löschen",
		domain.PermRolesRead:       "Rollen auflisten",
		domain.PermRolesWrite:      "Rollenzuordnungen ändern",
		domain.PermServersRead:     "Server, Status, Hardware und Pakete lesen",
		domain.PermServersWrite:    "Server joinen, konfigurieren, härten, entfernen",
		domain.PermServersConsole:  "Interaktive Konsole auf einem Server öffnen (Root-Shell im Browser)",
		domain.PermGroupsRead:      "Servergruppen lesen",
		domain.PermGroupsWrite:     "Servergruppen anlegen, ändern, auflösen",
		domain.PermRulesManage:     "Rules und Schedules verwalten und triggern",
		domain.PermJobsRead:        "Job-Historie und SSH-Konsolen-Output lesen",
		domain.PermPackagesRead:    "Globale Paket- und Vulnerability-Übersicht",
		domain.PermLinuxUsersRead:  "Linux-Benutzer lesen",
		domain.PermLinuxUsersWrite: "Linux-Benutzer anlegen, ändern, zuordnen",
		domain.PermProfilesRead:    "Berechtigungsprofile lesen",
		domain.PermProfilesWrite:   "Berechtigungsprofile anlegen und ändern",
		domain.PermSettingsManage:  "Globale Einstellungen und SSL verwalten",
		domain.PermBackupsManage:   "System-Backups verwalten und auslösen",
		domain.PermAuditRead:       "Audit-Log lesen",
		domain.PermAPIKeysManage:   "API-Keys / Service-Accounts verwalten",

		domain.PermNotificationsManage: "Benachrichtigungskanäle verwalten",
		domain.PermAlertsManage:        "Alarmregeln und -historie verwalten",
	}
	perms := map[string]domain.Permission{}
	allCodes := make([]string, 0, len(permDefs))
	for code, desc := range permDefs {
		p, err := roleRepo.EnsurePermission(code, desc)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("permission %s: %w", code, err)
		}
		perms[code] = *p
		allCodes = append(allCodes, code)
	}
	pick := func(codes ...string) []domain.Permission {
		out := make([]domain.Permission, 0, len(codes))
		for _, c := range codes {
			out = append(out, perms[c])
		}
		return out
	}

	// ensureRole legt die Rolle an, falls sie fehlt, und setzt ihr
	// Permission-Set immer auf den kanonischen Stand.
	ensureRole := func(name, description string, permSet []domain.Permission) (*domain.Role, error) {
		role, err := roleRepo.FindByName(name)
		if err != nil {
			if !errors.Is(err, repositories.ErrNotFound) {
				return nil, fmt.Errorf("rolle %s laden: %w", name, err)
			}
			role = &domain.Role{Name: name, Description: description, Permissions: permSet}
			if err := roleRepo.Create(role); err != nil {
				return nil, fmt.Errorf("rolle %s: %w", name, err)
			}
			return role, nil
		}
		if err := roleRepo.SetPermissions(role, permSet); err != nil {
			return nil, fmt.Errorf("rolle %s aktualisieren: %w", name, err)
		}
		return role, nil
	}

	if admin, err = ensureRole(domain.RoleAdmin,
		"Administrative User - voller Zugriff auf alle Funktionen", pick(allCodes...)); err != nil {
		return nil, nil, nil, err
	}
	if manager, err = ensureRole(domain.RoleManager,
		"Verwaltungs-User - verwalten nur ihre zugewiesenen Servergruppen", pick(domain.ManagerPermissions()...)); err != nil {
		return nil, nil, nil, err
	}
	if system, err = ensureRole(domain.RoleSystem,
		"Interne Prozesse ohne User-Kontext", nil); err != nil {
		return nil, nil, nil, err
	}
	return admin, manager, system, nil
}

// ensureKnownRepos befüllt einen noch leeren Katalog mit den mitgelieferten
// Einträgen (Docker, PostgreSQL, …). Sobald der Katalog Einträge hat, gehört
// er dem Nutzer - Änderungen und Löschungen überleben jeden Neustart.
func ensureKnownRepos(db *gorm.DB) error {
	var n int64
	if err := db.Model(&domain.KnownRepo{}).Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	for _, def := range domain.DefaultKnownRepos() {
		if err := db.Create(&def).Error; err != nil {
			return fmt.Errorf("katalog-eintrag %s: %w", def.Key, err)
		}
	}
	return nil
}

// ensureGlobalSettings legt die Settings-Zeile (ID 1) an, falls sie fehlt.
func ensureGlobalSettings(db *gorm.DB, cfg *config.Config) error {
	var n int64
	if err := db.Model(&domain.GlobalSettings{}).Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	settings := domain.GlobalSettings{
		ID:                  1,
		DefaultSSHPort:      22,
		LogRetentionDays:    90,
		BackupEnabled:       true,
		BackupIntervalHours: 24,
		BackupRetention:     14,
		// DNS-Vorgaben sichtbar hinterlegen, damit die Auswahl-Presets und die
		// Test-Domains von Anfang an in der UI erscheinen und editierbar sind.
		DNSServerPresets: domain.DefaultDNSServerPresets,
		DNSTestDomains:   domain.DefaultDNSTestDomains,
		// 2FA-PFLICHT FÜR ADMINISTRATOREN - Standard im regulären Betrieb.
		// Das Admin-Konto verwaltet SSH-Zugänge und Root-Rechte der gesamten
		// Serverflotte; ein alleiniges Passwort ist dafür zu wenig.
		//
		// Im Entwicklungs- und Demo-Modus bewusst AUS, damit Entwicklung und
		// Ausprobieren ohne Authenticator-App möglich bleiben. Umschaltbar
		// unter Einstellungen → Allgemein (require_2fa_roles).
		Require2FARoles: defaultRequire2FARoles(cfg),
	}
	return db.Create(&settings).Error
}

// seedSystemGroup verankert die serverbezogenen Basis-Jobs in einer
// geschützten Gruppe: der Health-Check-Schedule (Ping alle 15 Minuten -
// bei jedem Ping werden zusätzlich alle Grundsatz-Regeln geprüft) und der
// tägliche System-Sync (Hardware-Daten + Linux-Benutzer). Schedules der
// System-Gruppe laufen auf ALLEN Servern. Backup und Log-Bereinigung sind
// KEINE Gruppen-Schedules - sie sind system-globale Schedules, die der
// Scheduler aus den globalen Einstellungen ableitet (siehe scheduler.go).
func seedSystemGroup(db *gorm.DB) error {
	group := domain.ServerGroup{
		Name:        domain.SystemGroupName,
		Description: "Geschützte System-Gruppe - Basis-Schedules für alle Server",
		IsSystem:    true,
		// Schwächster Vorrang: Die Regeln hier gelten für ALLE Server und
		// bilden die Grundlinie, die eine spezifischere Gruppe überstimmt.
		Priority: domain.SystemGroupPriority,
	}
	if err := db.Create(&group).Error; err != nil {
		return fmt.Errorf("system-gruppe: %w", err)
	}
	// Jeder Basis-Schedule bündelt eine oder mehrere Rules. Der tägliche
	// System-Sync frischt zusätzlich die Paketliste auf (apt-get update +
	// Bestandsaufnahme) und stößt danach den zentralen Docker-Check an
	// (Registry-Update-Check + Image-CVE-Scan auf dem LCM-Host) - so ist
	// das Docker-Inventar frisch, bevor es geprüft wird.
	schedules := []struct {
		cron  string
		name  string
		rules []domain.Rule
	}{
		{"*/15 * * * *", "Health-Check (Ping)", []domain.Rule{
			{Name: "Health-Check (Ping)", Type: domain.RuleTypeHealth},
		}},
		{"0 4 * * *", "System-Sync (Hardware & Linux-Benutzer)", []domain.Rule{
			{Name: "System-Sync (Hardware & Linux-Benutzer)", Type: domain.RuleTypeSync},
			{Name: "Paketliste aktualisieren", Type: domain.RuleTypePackageScan},
			{Name: "Docker-Check (Registry & Image-CVEs)", Type: domain.RuleTypeDockerCheck},
			{Name: "Anwendungs-Check (neueste Versionen)", Type: domain.RuleTypeAppCheck},
		}},
	}
	for _, s := range schedules {
		sched := domain.Schedule{
			GroupID: group.ID, Name: s.name, CronExpr: s.cron,
			Enabled: true, IsSystem: true,
		}
		if err := db.Create(&sched).Error; err != nil {
			return fmt.Errorf("system-schedule %s: %w", sched.Name, err)
		}
		for _, r := range s.rules {
			r.GroupID = group.ID
			r.ScheduleID = &sched.ID
			r.Enabled = true
			r.IsSystem = true
			if err := db.Create(&r).Error; err != nil {
				return fmt.Errorf("system-rule %s: %w", r.Name, err)
			}
		}
	}
	return nil
}

// ensureSystemSyncRules zieht neue System-Rules in bestehende Datenbanken
// nach (Upgrade-Pfad, läuft bei jedem Start): fügt dem täglichen System-
// Sync-Schedule die Paketlisten-Aktualisierung und den zentralen
// Docker-Check hinzu, falls sie noch fehlen. Idempotent; auf einer
// frischen DB (noch keine System-Gruppe) ein No-op, weil dann
// seedSystemGroup die Rules direkt anlegt.
func ensureSystemSyncRules(db *gorm.DB) error {
	var group domain.ServerGroup
	if err := db.Where("is_system = ?", true).First(&group).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	// Den Sync-Schedule über seine sync-Rule finden.
	var syncRule domain.Rule
	if err := db.Where("group_id = ? AND type = ?", group.ID, domain.RuleTypeSync).
		First(&syncRule).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	if syncRule.ScheduleID == nil {
		return nil
	}
	wanted := []domain.Rule{
		{Name: "Paketliste aktualisieren", Type: domain.RuleTypePackageScan},
		{Name: "Docker-Check (Registry & Image-CVEs)", Type: domain.RuleTypeDockerCheck},
		{Name: "Anwendungs-Check (neueste Versionen)", Type: domain.RuleTypeAppCheck},
	}
	for _, w := range wanted {
		var n int64
		if err := db.Model(&domain.Rule{}).
			Where("schedule_id = ? AND type = ?", *syncRule.ScheduleID, w.Type).
			Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		rule := domain.Rule{
			GroupID: group.ID, ScheduleID: syncRule.ScheduleID,
			Name: w.Name, Type: w.Type,
			Enabled: true, IsSystem: true,
		}
		if err := db.Create(&rule).Error; err != nil {
			return err
		}
	}
	return nil
}

// defaultRequire2FARoles liefert die beim Erststart voreingestellte
// 2FA-Pflicht. Regulärer Betrieb: Administratoren müssen einen zweiten
// Faktor einrichten. Entwicklungs-/Demo-Modus: keine Pflicht - dort wäre sie
// nur ein Hindernis, und beide Modi sind ohnehin nicht für den Produktivbetrieb
// gedacht.
func defaultRequire2FARoles(cfg *config.Config) string {
	if cfg != nil && (cfg.DemoMode || cfg.DevMode) {
		return ""
	}
	return domain.RoleAdmin
}

// ensurePrivilegeProfiles legt die mitgelieferten Berechtigungsprofile an,
// falls sie fehlen - je Profil einzeln geprüft, damit auch ein nachträglich
// hinzugekommenes in bestehende Datenbanken wandert.
//
// Die beiden Profile bilden GENAU den Zustand ab, den es vor den Profilen
// schon gab: „sudo" und „kein sudo". Dadurch lässt sich jeder bestehende
// Linux-Benutzer eindeutig einem Profil zuordnen, ohne dass sich auf einem
// Server ein einziges Recht ändert.
func ensurePrivilegeProfiles(db *gorm.DB) error {
	builtins := []domain.PrivilegeProfile{
		{
			Slug:           domain.ProfileSlugFullAdmin,
			Name:           "Voll-Administrator",
			Description:    "Uneingeschränkte Root-Rechte über sudo (NOPASSWD:ALL) - das bisherige Häkchen „sudo\".",
			Builtin:        true,
			GrantsFullRoot: true,
		},
		{
			Slug:        domain.ProfileSlugStandard,
			Name:        "Standardbenutzer",
			Description: "Keine Root-Rechte. Anmeldung per SSH-Schlüssel, Zugriff nach den Rechten des Dateisystems.",
			Builtin:     true,
		},
	}
	for i := range builtins {
		var n int64
		if err := db.Model(&domain.PrivilegeProfile{}).
			Where("slug = ?", builtins[i].Slug).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		if err := db.Create(&builtins[i]).Error; err != nil {
			return fmt.Errorf("berechtigungsprofil %s: %w", builtins[i].Slug, err)
		}
	}
	return nil
}

// ensureProfileBlocks bringt die mitgelieferten Regelbausteine auf den Stand
// dieser LCM-Fassung - je Slug einzeln, damit auch nachträglich hinzugekommene
// in bestehende Datenbanken wandern.
//
// Vorhandene mitgelieferte Bausteine werden ÜBERSCHRIEBEN. Das ist die Zusage,
// die an ihnen hängt: Sie sind nicht änderbar, dafür werden sie gepflegt -
// eine korrigierte Regel oder ein neuer Pfad muss auch auf einer bestehenden
// Installation ankommen. Ein Baustein, den jemand SELBST unter demselben Slug
// angelegt hat, bleibt unangetastet; er gehört ihm.
//
// Der Katalog selbst steht in seed_blocks.go. Wer einen Baustein anpassen
// will, klont ihn.
func ensureProfileBlocks(db *gorm.DB) error {
	for _, block := range builtinProfileBlocks() {
		var existing domain.ProfileBlock
		err := db.Where("slug = ?", block.Slug).First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if err := db.Create(&block).Error; err != nil {
				return fmt.Errorf("regelbaustein %s: %w", block.Slug, err)
			}
			continue
		}
		if err != nil {
			return err
		}
		if !existing.Builtin {
			continue // selbst angelegt - nicht anfassen
		}
		if err := updateBuiltinBlock(db, &existing, block); err != nil {
			return fmt.Errorf("regelbaustein %s: %w", block.Slug, err)
		}
	}
	return nil
}

// updateBuiltinBlock schreibt Beschreibung, Parameter und Varianten neu. Die
// Varianten werden ersetzt statt abgeglichen: Sie haben keinen fachlichen
// Schlüssel außer der Familie, und ein Abgleich wäre mehr Code als Nutzen.
func updateBuiltinBlock(db *gorm.DB, existing *domain.ProfileBlock, wanted domain.ProfileBlock) error {
	if err := db.Model(existing).Updates(map[string]any{
		"name": wanted.Name, "description": wanted.Description, "params": wanted.Params, "builtin": true,
		"name_en": wanted.NameEN, "description_en": wanted.DescriptionEN,
	}).Error; err != nil {
		return err
	}
	if err := db.Where("block_id = ?", existing.ID).Delete(&domain.ProfileBlockVariant{}).Error; err != nil {
		return err
	}
	for i := range wanted.Variants {
		wanted.Variants[i].ID = 0
		wanted.Variants[i].BlockID = existing.ID
		if err := db.Create(&wanted.Variants[i]).Error; err != nil {
			return err
		}
	}
	return nil
}

// ensureAppEntries pflegt die mitgelieferten Einträge des Anwendungskatalogs.
//
// Dieselbe Regel wie bei den Regelbausteinen: Mitgelieferte Einträge werden
// bei jedem Start auf den aktuellen Stand gebracht - sonst bliebe ein einmal
// ausgeliefertes falsches Merkmal für immer stehen. Ein Eintrag, den jemand
// selbst angelegt hat, wird nicht angefasst, auch wenn er denselben
// technischen Namen trägt.
//
// Enabled und die Verweise auf Eigene Aktionen bleiben ebenfalls unberührt:
// Das sind Entscheidungen des Betreibers, keine Stammdaten.
func ensureAppEntries(db *gorm.DB) error {
	for _, entry := range builtinAppEntries() {
		entry.Builtin, entry.Enabled = true, true
		var existing domain.AppCatalogEntry
		err := db.Where("slug = ?", entry.Slug).First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if err := db.Create(&entry).Error; err != nil {
				return fmt.Errorf("anwendung %s: %w", entry.Slug, err)
			}
			continue
		}
		if err != nil {
			return err
		}
		if !existing.Builtin {
			continue // selbst angelegt - nicht anfassen
		}
		if err := db.Model(&existing).Updates(map[string]any{
			"name": entry.Name, "description": entry.Description,
			"name_en": entry.NameEN, "description_en": entry.DescriptionEN,
			"markers": entry.Markers, "version_command": entry.VersionCommand,
			"version_pattern": entry.VersionPattern, "compare": entry.Compare,
			"latest_source": entry.LatestSource, "latest_pattern": entry.LatestPattern,
			"builtin": true,
		}).Error; err != nil {
			return fmt.Errorf("anwendung %s: %w", entry.Slug, err)
		}
	}
	return nil
}
