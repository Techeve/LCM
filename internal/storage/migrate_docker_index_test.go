package storage

import "testing"

// TestMigrateDockerImageIndexNimmtImageIDAuf: Alt-Datenbank mit dem Index ohne
// Image-ID. Nach der Migration trägt der Index die ID, und zwei überholte
// Fassungen desselben Repositorys sind erlaubt.
func TestMigrateDockerImageIndexNimmtImageIDAuf(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE docker_images (id TEXT PRIMARY KEY, server_id INTEGER NOT NULL,
		repository TEXT NOT NULL, tag TEXT, image_id TEXT)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE UNIQUE INDEX idx_dockeri_server_ref ON docker_images (server_id, repository, tag)").Error; err != nil {
		t.Fatal(err)
	}
	if err := migrateDockerImageIndex(db); err != nil {
		t.Fatal(err)
	}
	var n int
	db.Raw("SELECT COUNT(*) FROM pragma_index_info('idx_dockeri_server_ref') WHERE name = 'image_id'").Scan(&n)
	if n != 0 {
		t.Fatalf("index nach dem Verwerfen darf nicht mehr existieren, Spalten-Treffer=%d", n)
	}
	// Zweiter Lauf (AutoMigrate hat den Index inzwischen neu angelegt) ist No-Op.
	if err := autoMigrate(db); err != nil {
		t.Fatal(err)
	}
	if err := migrateDockerImageIndex(db); err != nil {
		t.Fatal(err)
	}
	db.Raw("SELECT COUNT(*) FROM pragma_index_info('idx_dockeri_server_ref') WHERE name = 'image_id'").Scan(&n)
	if n != 1 {
		t.Fatalf("index nach AutoMigrate muss die image_id enthalten, Treffer=%d", n)
	}
}
