package storage

import (
	"strings"
	"testing"

	"LCM/internal/core/domain"
)

// TestBuiltinBlocksBestehenDiePruefung ist die Abnahme des mitgelieferten
// Katalogs: Jede Variante läuft durch DIESELBE Prüfung, die auch ein von Hand
// angelegter Baustein bestehen muss.
//
// Ohne diesen Test fiele ein Tippfehler im Katalog erst auf dem Zielsystem
// auf - und zwar als fehlendes Recht, das niemand erwartet.
func TestBuiltinBlocksBestehenDiePruefung(t *testing.T) {
	for _, block := range builtinProfileBlocks() {
		if len(block.Variants) == 0 {
			t.Errorf("%s: keine Variante hinterlegt", block.Slug)
			continue
		}
		for _, variant := range block.Variants {
			if err := domain.ValidateBlockVariant(variant, block.Params); err != nil {
				t.Errorf("%s: %v", block.Slug, err)
			}
		}
	}
}

// TestBuiltinBlocksSindEindeutig: Slug und Name sind eindeutige Spalten. Zwei
// gleiche Einträge im Katalog liefen erst beim Seeding auf einen
// Datenbankfehler - und dann steht die Installation.
func TestBuiltinBlocksSindEindeutig(t *testing.T) {
	slugs, names := map[string]bool{}, map[string]bool{}
	for _, block := range builtinProfileBlocks() {
		if !domain.ValidBlockSlug(block.Slug) {
			t.Errorf("ungültiger slug %q", block.Slug)
		}
		if slugs[block.Slug] {
			t.Errorf("slug %q kommt doppelt vor", block.Slug)
		}
		if names[block.Name] {
			t.Errorf("name %q kommt doppelt vor", block.Name)
		}
		slugs[block.Slug], names[block.Name] = true, true
		if strings.TrimSpace(block.Description) == "" {
			t.Errorf("%s: ohne Beschreibung ist ein Baustein nicht auswählbar", block.Slug)
		}
	}
}

// TestBuiltinBlocksSindMitgeliefert: Alle Einträge müssen als mitgeliefert
// markiert sein - sonst wären sie änderbar und eine LCM-Aktualisierung würde
// die Änderung stillschweigend überschreiben.
func TestBuiltinBlocksSindMitgeliefert(t *testing.T) {
	for _, block := range builtinProfileBlocks() {
		if !block.Builtin {
			t.Errorf("%s ist nicht als mitgeliefert markiert", block.Slug)
		}
	}
}

// TestBuiltinBlocksDeckenDieFamilienAb: Ein Baustein, der für eine Familie
// keine Variante hat, gilt dort NICHT. Das ist erlaubt (Apache heißt auf
// pacman-Systemen anders und ist dort selten), muss aber Absicht sein: Wer
// keine „all“-Variante hat, braucht mindestens apt UND dnf - sonst ist der
// Baustein auf der Hälfte der verbreiteten Server wirkungslos.
func TestBuiltinBlocksDeckenDieFamilienAb(t *testing.T) {
	for _, block := range builtinProfileBlocks() {
		familien := map[string]bool{}
		for _, v := range block.Variants {
			familien[v.Family] = true
		}
		if familien[domain.BlockFamilyAll] {
			continue
		}
		if !familien["apt"] || !familien["dnf"] {
			t.Errorf("%s hat weder eine all-Variante noch apt+dnf - auf vielen Servern wirkungslos", block.Slug)
		}
	}
}

// TestBuiltinBlocksWerdenAktualisiert: Mitgelieferte Bausteine sind nicht
// änderbar - dafür werden sie gepflegt. Eine korrigierte Regel muss auch auf
// einer bestehenden Installation ankommen, sonst wäre die Zusage leer.
func TestBuiltinBlocksWerdenAktualisiert(t *testing.T) {
	db := newMigrationTestDB(t)

	// Stand einer älteren Installation nachstellen: derselbe Slug, aber mit
	// veralteter Beschreibung und einer überholten Variante.
	old := domain.ProfileBlock{
		Slug: "nginx-betreiben", Name: "nginx betreiben", Builtin: true,
		Description: "alter Stand",
		Variants: []domain.ProfileBlockVariant{
			{Family: domain.BlockFamilyAll, SudoCommands: "/usr/bin/systemctl --no-pager restart nginx"},
		},
	}
	if err := db.Create(&old).Error; err != nil {
		t.Fatal(err)
	}

	if err := ensureProfileBlocks(db); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	var updated domain.ProfileBlock
	if err := db.Preload("Variants").Where("slug = ?", "nginx-betreiben").First(&updated).Error; err != nil {
		t.Fatal(err)
	}
	if updated.Description == "alter Stand" {
		t.Error("die Beschreibung wurde nicht nachgezogen")
	}
	if updated.ID != old.ID {
		t.Error("der Baustein wurde ersetzt statt aktualisiert - Profile, die ihn nutzen, verlören ihn")
	}
	if len(updated.Variants) != 1 || !strings.Contains(updated.Variants[0].PathRules, "/var/log/nginx") {
		t.Errorf("die Varianten wurden nicht ersetzt: %+v", updated.Variants)
	}
}

// TestSelbstAngelegterBausteinBleibt: Wer vor uns einen Baustein unter
// demselben Slug angelegt hat, behält ihn. Ihn zu überschreiben hieße, jemandem
// seine Arbeit unter den Händen wegzunehmen.
func TestSelbstAngelegterBausteinBleibt(t *testing.T) {
	db := newMigrationTestDB(t)
	eigen := domain.ProfileBlock{
		Slug: "nginx-betreiben", Name: "Mein nginx", Builtin: false,
		Description: "selbst gebaut",
		Variants: []domain.ProfileBlockVariant{
			{Family: domain.BlockFamilyAll, SudoCommands: "/usr/bin/systemctl --no-pager restart nginx"},
		},
	}
	if err := db.Create(&eigen).Error; err != nil {
		t.Fatal(err)
	}

	if err := ensureProfileBlocks(db); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	var nachher domain.ProfileBlock
	if err := db.Where("slug = ?", "nginx-betreiben").First(&nachher).Error; err != nil {
		t.Fatal(err)
	}
	if nachher.Description != "selbst gebaut" || nachher.Builtin {
		t.Errorf("der selbst angelegte Baustein wurde überschrieben: %+v", nachher)
	}
}

// TestNextcloudLaeuftNichtAlsRoot hält den Grund fest, aus dem es RunAs an
// den Varianten überhaupt gibt: Nextclouds occ als root auszuführen verstellt
// die Dateirechte der Installation - die Anwendung warnt selbst davor. Ein
// Baustein, der das täte, wäre schlimmer als keiner.
func TestNextcloudLaeuftNichtAlsRoot(t *testing.T) {
	expected := map[string]string{"apt": "www-data", "dnf": "apache", "zypper": "wwwrun"}
	for _, slug := range []string{"nextcloud-betreiben", "nextcloud-verwalten"} {
		block := findBuiltinBlock(t, slug)
		for _, v := range block.Variants {
			if v.RunAs == "" || v.RunAs == "root" {
				t.Errorf("%s/%s läuft als root", slug, v.Family)
			}
			if want := expected[v.Family]; want != "" && v.RunAs != want {
				t.Errorf("%s/%s läuft als %q, erwartet %q", slug, v.Family, v.RunAs, want)
			}
		}
	}
}

// TestContainerBausteineBleibenEng: Docker und Podman als root sind faktisch
// root - ein Container mit eingehängtem Wurzelverzeichnis genügt. Deshalb
// darf kein Kommando ohne festen Container-Namen durchrutschen, und „run“
// gehört gar nicht erst hinein.
func TestContainerBausteineBleibenEng(t *testing.T) {
	for _, slug := range []string{"docker-container", "podman-container"} {
		block := findBuiltinBlock(t, slug)
		for _, v := range block.Variants {
			for _, cmd := range domain.BlockLines(v.SudoCommands) {
				if strings.Contains(cmd, " run") || strings.Contains(cmd, " exec") {
					t.Errorf("%s: %q startet beliebige Programme", slug, cmd)
				}
			}
		}
	}
}

// findBuiltinBlock holt einen Baustein aus dem Katalog.
func findBuiltinBlock(t *testing.T, slug string) domain.ProfileBlock {
	t.Helper()
	for _, block := range builtinProfileBlocks() {
		if block.Slug == slug {
			return block
		}
	}
	t.Fatalf("baustein %q fehlt im Katalog", slug)
	return domain.ProfileBlock{}
}

// TestGeschuetzteVerzeichnisseBleibenDraussen: An /var/lib/lcm hängen
// Master-Key und Datenbank, an /etc/ssh der Zugang. Kein mitgelieferter
// Baustein darf dorthin zeigen - LCM schützt diese Pfade auch gegen seine
// eigenen Regeln, und ein Katalog-Eintrag, der es versuchte, würde beim
// Anwenden still verworfen statt zu wirken.
func TestGeschuetzteVerzeichnisseBleibenDraussen(t *testing.T) {
	verboten := []string{"/var/lib/lcm", "/etc/ssh", "/etc/sudoers", "/root"}
	for _, block := range builtinProfileBlocks() {
		for _, v := range block.Variants {
			lines := append(domain.BlockLines(v.EditPaths), domain.BlockLines(v.PathRules)...)
			for _, line := range lines {
				for _, path := range verboten {
					if strings.Contains(line, path) {
						t.Errorf("%s zeigt auf den geschützten Pfad %s: %q", block.Slug, path, line)
					}
				}
			}
		}
	}
}

// TestMitgelieferteBausteineSindZweisprachig: Der Katalog steht in der
// Datenbank, nicht im Sprachkatalog der Oberfläche - ohne englische Fassung
// stünde in der englischen Oberfläche deutscher Text. Für die MITGELIEFERTEN
// Einträge ist das eine Zusage; selbst angelegte dürfen einsprachig bleiben.
func TestMitgelieferteBausteineSindZweisprachig(t *testing.T) {
	for _, block := range builtinProfileBlocks() {
		if strings.TrimSpace(block.NameEN) == "" {
			t.Errorf("%s: kein englischer Name", block.Slug)
		}
		if strings.TrimSpace(block.DescriptionEN) == "" {
			t.Errorf("%s: keine englische Beschreibung", block.Slug)
		}
		// Ein versehentlich kopierter deutscher Text ist schlimmer als keiner:
		// Er sieht nach Übersetzung aus und ist keine.
		if block.NameEN == block.Name {
			t.Errorf("%s: englischer Name gleicht dem deutschen", block.Slug)
		}
	}
}

// TestSprachwahlFaelltAufDeutschZurueck: Ein selbst angelegter Baustein ohne
// englische Fassung darf in der englischen Oberfläche nicht leer erscheinen.
func TestSprachwahlFaelltAufDeutschZurueck(t *testing.T) {
	eigen := domain.ProfileBlock{Name: "Eigener Baustein", Description: "Nur deutsch."}
	if got := eigen.LocalizedName("en"); got != "Eigener Baustein" {
		t.Errorf("Rückfall auf Deutsch fehlt: %q", got)
	}
	if got := eigen.LocalizedDescription("en"); got != "Nur deutsch." {
		t.Errorf("Rückfall auf Deutsch fehlt: %q", got)
	}

	zweisprachig := domain.ProfileBlock{
		Name: "Systemd-Dienst betreiben", NameEN: "Operate a systemd service",
		Description: "Deutsch.", DescriptionEN: "English.",
	}
	if got := zweisprachig.LocalizedName("en"); got != "Operate a systemd service" {
		t.Errorf("englische Fassung wurde nicht gewählt: %q", got)
	}
	if got := zweisprachig.LocalizedName("de"); got != "Systemd-Dienst betreiben" {
		t.Errorf("deutsche Fassung wurde nicht gewählt: %q", got)
	}
}
