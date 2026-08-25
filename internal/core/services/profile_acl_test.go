package services

import (
	"strings"
	"testing"

	"LCM/internal/core/domain"
)

func aclProfile(rules ...domain.ProfilePathRule) *domain.PrivilegeProfile {
	return &domain.PrivilegeProfile{ID: 3, Slug: "web", Name: "Web", PathRules: rules}
}

// TestACLFolgtKeinenSymlinks: Ohne -P folgt setfacl Symlinks. Ein Benutzer mit
// Schreibrecht im freigegebenen Baum könnte vor dem nächsten Abgleich einen
// Link auf /etc legen und bekäme dessen Ziel mitfreigegeben.
func TestACLFolgtKeinenSymlinks(t *testing.T) {
	script := profilePathScript(aclProfile(
		domain.ProfilePathRule{Path: "/srv/www", Mode: domain.PathModeReadWrite},
	))
	for _, line := range strings.Split(script, "\n") {
		if strings.Contains(line, "setfacl") && !strings.Contains(line, "-RP") {
			t.Errorf("setfacl ohne -P (physical): %s", line)
		}
	}
	// Ein Pfad, der selbst ein Symlink ist, wird gar nicht erst angefasst.
	if !strings.Contains(script, "-L /srv/www") {
		t.Errorf("symlink-prüfung des pfades fehlt: %s", script)
	}
}

// TestACLSetztVererbung: Die Default-ACL ist die Vererbung - was neu entsteht,
// erbt die Rechte. Ohne sie wäre eine Freigabe nach der ersten neuen Datei
// wieder löchrig.
func TestACLSetztVererbung(t *testing.T) {
	script := profilePathScript(aclProfile(
		domain.ProfilePathRule{Path: "/srv/www", Mode: domain.PathModeReadWrite},
	))
	if !strings.Contains(script, "setfacl -RP -m g:lcm-prof-web:rwX /srv/www") {
		t.Errorf("bestand wird nicht nachgezogen: %s", script)
	}
	if !strings.Contains(script, "setfacl -RP -d -m g:lcm-prof-web:rwX /srv/www") {
		t.Errorf("vererbung für neue dateien fehlt: %s", script)
	}
}

// TestACLVerweigerungOhneVererbung: Für eine Verweigerung ergibt eine
// Vererbung keinen Sinn - sie würde jede neue Datei mit einem Deny-Eintrag
// versehen, ohne dass es etwas nützt.
func TestACLVerweigerungOhneVererbung(t *testing.T) {
	script := profilePathScript(aclProfile(
		domain.ProfilePathRule{Path: "/opt/kundendaten", Mode: domain.PathModeDeny},
	))
	if !strings.Contains(script, "-m g:lcm-prof-web:--- /opt/kundendaten") {
		t.Errorf("verweigerung fehlt: %s", script)
	}
	if strings.Contains(script, "-d -m g:lcm-prof-web:---") {
		t.Errorf("verweigerung darf nicht vererbt werden: %s", script)
	}
}

// TestACLRechteSpezifikation: Großes X setzt das Durchgangs-/Ausführrecht nur
// auf Verzeichnisse - kleines x machte jede Textdatei im Baum ausführbar.
func TestACLRechteSpezifikation(t *testing.T) {
	cases := map[string]string{
		domain.PathModeRead:      "rX",
		domain.PathModeReadWrite: "rwX",
		domain.PathModeDeny:      "---",
	}
	for mode, want := range cases {
		if got := aclSpecFor(mode); got != want {
			t.Errorf("modus %q: erwartet %q, bekam %q", mode, want, got)
		}
	}
}

// TestACLRuecknahme nimmt Zugriff UND Vererbung zurück - sonst erbten neue
// Dateien weiterhin ein Recht, das aus dem Profil längst entfernt ist.
func TestACLRuecknahme(t *testing.T) {
	script := removeProfilePathScript("web", "/srv/www")
	if !strings.Contains(script, "setfacl -RP -x g:lcm-prof-web /srv/www") {
		t.Errorf("zugriff wird nicht zurückgenommen: %s", script)
	}
	if !strings.Contains(script, "setfacl -RP -d -x g:lcm-prof-web /srv/www") {
		t.Errorf("vererbung wird nicht zurückgenommen: %s", script)
	}
}

// TestACLProbePruefotDasDateisystem: Das Werkzeug allein genügt nicht - auf
// ZFS ohne acltype=posixacl ist setfacl vorhanden und bewirkt nichts.
func TestACLProbePruefotDasDateisystem(t *testing.T) {
	for _, marker := range []string{"setfacl -m", "getfacl", "acl-ok", "rm -rf"} {
		if !strings.Contains(aclProbeScript, marker) {
			t.Errorf("probe enthält %q nicht: %s", marker, aclProbeScript)
		}
	}
}

// TestGeaenderterPfadWirdErkannt: Nur was unverändert gefordert bleibt, darf
// stehen bleiben - ein geänderter Modus muss neu gesetzt und der alte Eintrag
// zurückgenommen werden.
func TestGeaenderterPfadWirdErkannt(t *testing.T) {
	old := domain.AppliedProfilePath{ProfileID: 3, Path: "/srv/www", Mode: domain.PathModeReadWrite}
	soll := []domain.AppliedProfilePath{{ProfileID: 3, Path: "/srv/www", Mode: domain.PathModeRead}}
	if pathStillWanted(old, soll) {
		t.Error("ein geänderter modus darf nicht als unverändert gelten")
	}
	soll[0].Mode = domain.PathModeReadWrite
	if !pathStillWanted(old, soll) {
		t.Error("unveränderter pfad wurde als weggefallen erkannt")
	}
	if pathStillWanted(old, nil) {
		t.Error("ohne soll-eintrag muss der pfad als weggefallen gelten")
	}
}
