package storage

import (
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/crypto"
	"LCM/internal/storage/repositories"
)

// TestRotationErfasstDenBenutzerBestandDerServer ist der Beweis zum
// Nebenbefund: Fünf verschlüsselte Spalten des Benutzer-Bestands waren für die
// Schlüsselrotation nicht registriert. Sie wären mit dem ALTEN Schlüssel
// liegengeblieben - und weil das Lesen nicht entschlüsselbare Werte
// durchreicht, hätten danach sämtliche Benutzernamen aller Server als
// Geheimtext in der Oberfläche gestanden.
//
// Drei davon tragen zusätzlich einen Blindindex, über den gesucht wird
// (Sperrprüfung, Anmeldungen, ausstehende Abgleiche). Reines Umschlüsseln
// reicht dort nicht: Der Index leitet sich aus dem Schlüssel ab und muss neu
// berechnet werden, sonst findet die Sperrprüfung keinen gesperrten Benutzer
// mehr - der Sperrschutz wäre nach der Rotation still außer Kraft.
func TestRotationErfasstDenBenutzerBestandDerServer(t *testing.T) {
	db, alt := setupEncDB(t)

	server := &domain.Server{Name: "web01", Host: "10.0.0.1", HardwareModel: "Raspberry Pi 4 Model B"}
	if err := db.Create(server).Error; err != nil {
		t.Fatal(err)
	}
	zeilen := []any{
		&domain.ServerUser{ServerID: server.ID, Username: "alice", UID: 1000},
		&domain.ServerUserBlock{ServerID: server.ID, Username: "Mallory"},
		&domain.ServerUserLogin{ServerID: server.ID, Username: "alice", FromHost: "10.0.0.99"},
		&domain.PendingUserSync{ServerID: server.ID, Username: "bob"},
		&domain.StorageHealth{ServerID: server.ID, Kind: domain.StorageKindZFS, Name: "tank",
			RawState: "DEGRADED", Message: "Pool-Zustand DEGRADED"},
	}
	for _, z := range zeilen {
		if err := db.Create(z).Error; err != nil {
			t.Fatalf("%T anlegen: %v", z, err)
		}
	}

	neu, _ := crypto.NewCipher(crypto.GenerateKey())
	if err := RotateEncryptedFields(db, alt, neu); err != nil {
		t.Fatalf("Rotation: %v", err)
	}
	SetFieldCipher(neu)

	// Jeder Wert muss unter dem neuen Schlüssel wieder im Klartext ankommen.
	// Käme der Rohwert durch (Fallback), wäre es Geheimtext - und ungleich.
	var su domain.ServerUser
	var sb domain.ServerUserBlock
	var sl domain.ServerUserLogin
	var ps domain.PendingUserSync
	var sh domain.StorageHealth
	var sv domain.Server
	for name, dst := range map[string]any{
		"server_users": &su, "server_user_blocks": &sb, "server_user_logins": &sl,
		"pending_user_syncs": &ps, "storage_healths": &sh, "servers": &sv,
	} {
		if err := db.First(dst).Error; err != nil {
			t.Fatalf("%s lesen: %v", name, err)
		}
	}
	erwartet := map[string][2]string{
		"server_users.username":        {su.Username, "alice"},
		"server_user_blocks.username":  {sb.Username, "Mallory"},
		"server_user_logins.username":  {sl.Username, "alice"},
		"server_user_logins.from_host": {sl.FromHost, "10.0.0.99"},
		"pending_user_syncs.username":  {ps.Username, "bob"},
		"storage_healths.name":         {sh.Name, "tank"},
		"storage_healths.raw_state":    {sh.RawState, "DEGRADED"},
		"storage_healths.message":      {sh.Message, "Pool-Zustand DEGRADED"},
		"servers.hardware_model":       {sv.HardwareModel, "Raspberry Pi 4 Model B"},
	}
	for spalte, w := range erwartet {
		if w[0] != w[1] {
			t.Errorf("%s nach der Rotation nicht lesbar: %q statt %q", spalte, w[0], w[1])
		}
	}

	// Die Blindindizes müssen zum NEUEN Schlüssel passen - sonst findet die
	// Suche nach dem Namen nichts mehr, obwohl der Wert lesbar ist.
	for name, roh := range map[string]string{
		"server_user_blocks": rawCol(t, db, "server_user_blocks", "username_bidx", sb.ID),
		"server_user_logins": rawCol(t, db, "server_user_logins", "username_bidx", sl.ID),
		"pending_user_syncs": rawCol(t, db, "pending_user_syncs", "username_bidx", ps.ID),
	} {
		var klartext string
		switch name {
		case "server_user_blocks":
			klartext = "Mallory"
		case "pending_user_syncs":
			klartext = "bob"
		default:
			klartext = "alice"
		}
		if roh != repositories.BlindIndex(klartext) {
			t.Errorf("%s: Blindindex passt nach der Rotation nicht zum neuen Schlüssel - die Suche nach %q liefe ins Leere", name, klartext)
		}
	}
}
