package storage

import (
	"testing"

	"gorm.io/gorm"

	"LCM/internal/config"
	"LCM/internal/core/domain"
)

// Eine Bestandsinstallation, deren Einstellungen vor dem Adressmodus angelegt
// wurden, bekommt beim Migrieren den Stand von vorher: Modus A und die Ports
// der config.json-Vorgaben. Ohne die Vorgaben stünde in den neuen Spalten 0.
func TestMigrateAdressmodusVorgabenFuerBestand(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := autoMigrate(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&domain.GlobalSettings{ID: 1}).Error; err != nil {
		t.Fatal(err)
	}
	// Die Spalten wieder entfernen: So sah die Tabelle vorher aus.
	for _, col := range []string{"address_mode", "public_host", "ui_port", "agent_port", "shared_tls_port"} {
		if err := db.Exec("ALTER TABLE global_settings DROP COLUMN " + col).Error; err != nil {
			t.Fatalf("Spalte %s entfernen: %v", col, err)
		}
	}

	if err := autoMigrate(db); err != nil {
		t.Fatal(err)
	}
	var st domain.GlobalSettings
	if err := db.First(&st, 1).Error; err != nil {
		t.Fatal(err)
	}
	if st.AddressMode != domain.AddressModeSeparate || st.UIPort != 9310 || st.AgentPort != 9320 ||
		st.MCPPort != 9330 || st.SharedTLSPort != 443 || st.PublicHost != "" {
		t.Errorf("Vorgaben nach der Migration: %+v", st)
	}
}

// Eine Bestandsinstallation behält ihren Agenten (an) und bekommt MCP mit TLS.
// Ein später ausgeschalteter Agent bleibt beim nächsten Start aus: Nur die
// frisch angelegte, leere Spalte wird belegt.
func TestMigrateDienstschalterFuerBestand(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := autoMigrate(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&domain.GlobalSettings{ID: 1, MCPEnabled: true}).Error; err != nil {
		t.Fatal(err)
	}
	// So sah die Tabelle vor dem Schalter aus: ohne agent_enabled.
	if err := db.Exec("ALTER TABLE global_settings DROP COLUMN agent_enabled").Error; err != nil {
		t.Fatal(err)
	}
	if err := autoMigrate(db); err != nil {
		t.Fatal(err)
	}

	if err := migrateServiceSwitches(db); err != nil {
		t.Fatal(err)
	}
	st := readSettings(t, db)
	if !st.AgentEnabled || !st.MCPTLS || !st.MCPEnabled {
		t.Errorf("nach dem Update: agent=%v mcp_tls=%v mcp=%v, erwartet an, an, an", st.AgentEnabled, st.MCPTLS, st.MCPEnabled)
	}

	if err := db.Save(&domain.GlobalSettings{ID: 1, AgentEnabled: false, MCPTLS: true}).Error; err != nil {
		t.Fatal(err)
	}
	if err := migrateServiceSwitches(db); err != nil {
		t.Fatal(err)
	}
	if st := readSettings(t, db); st.AgentEnabled {
		t.Error("ein ausgeschalteter Agent wurde beim nächsten Start wieder eingeschaltet")
	}
}

// Eine frische Installation startet mit Agent an und MCP mit TLS.
func TestSeedDienstschalterVorgaben(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := autoMigrate(db); err != nil {
		t.Fatal(err)
	}
	if err := ensureGlobalSettings(db, &config.Config{}); err != nil {
		t.Fatal(err)
	}
	if st := readSettings(t, db); !st.AgentEnabled || !st.MCPTLS || st.MCPEnabled {
		t.Errorf("Vorgaben: agent=%v mcp_tls=%v mcp=%v, erwartet an, an, aus", st.AgentEnabled, st.MCPTLS, st.MCPEnabled)
	}
}

func readSettings(t *testing.T, db *gorm.DB) domain.GlobalSettings {
	t.Helper()
	var st domain.GlobalSettings
	if err := db.First(&st, 1).Error; err != nil {
		t.Fatal(err)
	}
	return st
}

// Eine Installation, die den Adress-Schritt noch offen hatte (address_setup
// "open"), beginnt den Assistenten bei den Adressen; alle anderen gelten als
// erledigt. Die Übernahme läuft nur einmal.
func TestMigrateSetupStepAusAddressSetup(t *testing.T) {
	for old, want := range map[string]string{
		"open": domain.SetupStepAddresses, "skipped": "", "": "",
	} {
		db, err := Open(":memory:")
		if err != nil {
			t.Fatal(err)
		}
		if err := autoMigrate(db); err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&domain.GlobalSettings{ID: 1}).Error; err != nil {
			t.Fatal(err)
		}
		// So sah die Tabelle vorher aus: mit address_setup.
		if err := db.Exec("ALTER TABLE global_settings ADD COLUMN address_setup text").Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("UPDATE global_settings SET address_setup = ?", old).Error; err != nil {
			t.Fatal(err)
		}

		if err := migrateSetupStep(db); err != nil {
			t.Fatal(err)
		}
		if got := readSettings(t, db).SetupStep; got != want {
			t.Errorf("address_setup %q: setup_step = %q, erwartet %q", old, got, want)
		}

		// Der Assistent ist weiter, ein zweiter Start nimmt ihn nicht zurück.
		if err := db.Model(&domain.GlobalSettings{}).Where("id = 1").Update("setup_step", domain.SetupStepBackup).Error; err != nil {
			t.Fatal(err)
		}
		if err := migrateSetupStep(db); err != nil {
			t.Fatal(err)
		}
		if got := readSettings(t, db).SetupStep; got != domain.SetupStepBackup {
			t.Errorf("address_setup %q: zweiter Start setzte setup_step auf %q", old, got)
		}
	}
}

// Auf einer frischen Installation gibt es address_setup nicht: nichts zu tun.
func TestMigrateSetupStepOhneAlteSpalte(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := autoMigrate(db); err != nil {
		t.Fatal(err)
	}
	if err := migrateSetupStep(db); err != nil {
		t.Fatal(err)
	}
}

// Eine früher gespeicherte eigene Dienst-Adresse der Subscription wird beim
// Start geleert; ohne die Spalte (frische Installation) ist das ein Nichts.
func TestClearSubscriptionServiceURL(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := autoMigrate(db); err != nil {
		t.Fatal(err)
	}
	if err := clearSubscriptionServiceURL(db); err != nil {
		t.Fatalf("ohne Spalte: %v", err)
	}
	if err := db.Create(&domain.GlobalSettings{ID: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ALTER TABLE global_settings ADD COLUMN subscription_service_url text").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("UPDATE global_settings SET subscription_service_url = ?", "https://evil.example").Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := clearSubscriptionServiceURL(db); err != nil {
			t.Fatal(err)
		}
		var got string
		if err := db.Raw("SELECT subscription_service_url FROM global_settings WHERE id = 1").Scan(&got).Error; err != nil {
			t.Fatal(err)
		}
		if got != "" {
			t.Errorf("Lauf %d: subscription_service_url = %q, erwartet leer", i+1, got)
		}
	}
}
