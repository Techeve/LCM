package services

import (
	"errors"
	"testing"

	"LCM/internal/core/domain"
)

// Echte Ausgaben von `zypper refresh` auf openSUSE Leap 16 (Exit-Code jeweils 4).
const (
	zypperRefreshPartialOutput = `Retrieving repository 'kaputt' metadata [.error]
Repository 'kaputt' is invalid.
[kaputt|http://download.opensuse.org/gibtsnicht/] Failed to retrieve new repository metadata.
History:
 - [kaputt|http://download.opensuse.org/gibtsnicht/] Repository type can't be determined.

Please check if the URIs defined for this repository are pointing to a valid repository.
Skipping repository 'kaputt' because of the above error.
Repository 'repo-openh264 (16.0)' is up to date.
Repository 'repo-oss (16.0)' is up to date.
Some of the repositories have not been refreshed because of an error.
`
	zypperRefreshAllFailedOutput = `Retrieving repository 'repo-openh264 (16.0)' metadata [.error]
Repository 'repo-openh264 (16.0)' is invalid.
[openSUSE:repo-openh264|http://codecs.opensuse.org/openh264/openSUSE_Leap_16] Failed to retrieve new repository metadata.
 - Download (curl) error for 'http://codecs.opensuse.org/openh264/openSUSE_Leap_16/content':
   Error code: Connection failed Curl error (6)
   Error message: Could not resolve host: codecs.opensuse.org
Please check if the URIs defined for this repository are pointing to a valid repository.
Skipping repository 'repo-openh264 (16.0)' because of the above error.
Retrieving repository 'repo-oss (16.0)' metadata [.error]
Repository 'repo-oss (16.0)' is invalid.
[openSUSE:repo-oss|http://cdn.opensuse.org/distribution/leap/16.0/repo/oss/x86_64] Failed to retrieve new repository metadata.
 - Download (curl) error for 'http://cdn.opensuse.org/distribution/leap/16.0/repo/oss/x86_64/content':
   Error code: Connection failed Curl error (6)
   Error message: Could not resolve host: cdn.opensuse.org
Please check if the URIs defined for this repository are pointing to a valid repository.
Skipping repository 'repo-oss (16.0)' because of the above error.
Could not refresh the repositories because of errors.
`
)

func TestPackageRunResult(t *testing.T) {
	const (
		partial = "Hit:1 http://a.example/debian bookworm InRelease\n" +
			"Err:2 https://b.example/debian bookworm InRelease\n  404  Not Found\n" +
			"E: The repository 'https://b.example/debian bookworm InRelease' is not signed.\n"
		allBroken = "Err:1 https://b.example/debian bookworm InRelease\n  404  Not Found\n" +
			"E: The repository 'https://b.example/debian bookworm InRelease' is not signed.\n"
		fatal = "Hit:1 http://a.example/debian bookworm InRelease\n" +
			"Err:2 https://b.example/debian bookworm InRelease\n  404  Not Found\n" +
			"E: Could not get lock /var/lib/apt/lists/lock. It is held by process 1234 (apt)\n"
	)
	// apt-get update endet bei DNS-Ausfall mit 0 und meldet nur Err:/W:.
	const (
		dnsAllDown = "Err:1 http://deb.debian.org/debian bookworm InRelease\n" +
			"  Temporary failure resolving 'deb.debian.org'\n" +
			"Err:2 http://deb.debian.org/debian bookworm-updates InRelease\n" +
			"  Temporary failure resolving 'deb.debian.org'\n" +
			"Err:3 http://security.debian.org/debian-security bookworm-security InRelease\n" +
			"  Temporary failure resolving 'security.debian.org'\n" +
			"Reading package lists...\n" +
			"W: Failed to fetch http://deb.debian.org/debian/dists/bookworm/InRelease  Temporary failure resolving 'deb.debian.org'\n" +
			"W: Some index files failed to download. They have been ignored, or old ones used instead.\n"
		oneOfThreeDown = "Hit:1 http://deb.debian.org/debian bookworm InRelease\n" +
			"Hit:2 http://security.debian.org/debian-security bookworm-security InRelease\n" +
			"Err:3 https://b.example/debian bookworm InRelease\n  404  Not Found\n" +
			"Reading package lists...\n" +
			"W: Failed to fetch https://b.example/debian/dists/bookworm/InRelease  404  Not Found\n" +
			"W: Some index files failed to download. They have been ignored, or old ones used instead.\n"
	)
	transport := errors.New("ssh-kommando: EOF")

	tests := []struct {
		name     string
		jobType  string
		mgr      string
		output   string
		code     int
		runErr   error
		wantCode int
		wantErr  bool
	}{
		{"Erfolg", domain.RuleTypeUpdate, "apt", "", 0, nil, 0, false},
		{"Exit-Code ohne Ausnahme", domain.RuleTypeUpdate, "apt", partial, 100, nil, 100, true},
		{"Paketliste: eine Quelle kaputt", domain.RuleTypePackageScan, "apt", partial, 100, nil, 0, false},
		{"Paketliste: alle Quellen kaputt", domain.RuleTypePackageScan, "apt", allBroken, 100, nil, 100, true},
		{"Paketliste: allgemeiner Fehler", domain.RuleTypePackageScan, "apt", fatal, 100, nil, 100, true},
		{"Paketliste: Exit 75 (Sperre)", domain.RuleTypePackageScan, "apt", "LCM: apt ist seit 10 Minuten belegt (PID 1 (x), laeuft seit 1)", 75, nil, 75, true},
		{"Paketliste: dnf ohne Ausnahme", domain.RuleTypePackageScan, "dnf", partial, 1, nil, 1, true},
		{"Paketliste: Exit 0, alle Quellen Err", domain.RuleTypePackageScan, "apt", dnsAllDown, 0, nil, 0, true},
		{"Paketliste: Exit 0, eine von drei Quellen Err", domain.RuleTypePackageScan, "apt", oneOfThreeDown, 0, nil, 0, false},
		{"Exit 0, alle Quellen Err, aber kein Paketlisten-Lauf", domain.RuleTypeUpdate, "apt", dnsAllDown, 0, nil, 0, false},
		{"Paketliste: zypper, eine Quelle kaputt", domain.RuleTypePackageScan, "zypper", zypperRefreshPartialOutput, 4, nil, 0, false},
		{"Paketliste: zypper, alle Quellen kaputt", domain.RuleTypePackageScan, "zypper", zypperRefreshAllFailedOutput, 4, nil, 4, true},
		{"Paketliste: zypper, Exit 0", domain.RuleTypePackageScan, "zypper", "Repository 'repo-oss (16.0)' is up to date.\n", 0, nil, 0, false},
		{"zypper, Exit 4 ohne Paketlisten-Lauf", domain.RuleTypeUpdate, "zypper", zypperRefreshPartialOutput, 4, nil, 4, true},
		{"Paketliste: Transportfehler", domain.RuleTypePackageScan, "apt", partial, -1, transport, -1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, err := packageRunResult(tt.jobType, tt.mgr, tt.output, tt.code, tt.runErr)
			if code != tt.wantCode || (err != nil) != tt.wantErr {
				t.Errorf("= (%d, %v), erwartet (%d, Fehler %v)", code, err, tt.wantCode, tt.wantErr)
			}
		})
	}
}

// TestFehlgeschlagenerPaketlistenLaufWirdKlassifiziert: Der Fehler, den
// packageRunResult bei Exit 0 meldet, landet mit der apt-Ausgabe am Job
// (JobService.Complete hängt „FEHLER: …" an). Daraus muss die richtige Klasse
// werden: DNS-Ausfall ist ein Netzproblem, alles andere ein Quellenfehler.
func TestFehlgeschlagenerPaketlistenLaufWirdKlassifiziert(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   string
	}{
		{"DNS", "Err:1 http://deb.debian.org/debian bookworm InRelease\n" +
			"  Temporary failure resolving 'deb.debian.org'\n" +
			"W: Some index files failed to download. They have been ignored, or old ones used instead.\n", domain.ErrClassNetwork},
		{"Quelle nicht gefunden", "Err:1 https://b.example/debian bookworm InRelease\n  404  Not Found\n" +
			"W: Failed to fetch https://b.example/debian/dists/bookworm/InRelease  404  Not Found\n" +
			"W: Some index files failed to download. They have been ignored, or old ones used instead.\n", domain.ErrClassRepoError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := packageRunResult(domain.RuleTypePackageScan, "apt", tt.output, 0, nil)
			if err == nil {
				t.Fatal("Fehlschlag erwartet")
			}
			jobOutput := tt.output + "\nFEHLER: " + err.Error()
			f := domain.ClassifyFailure(domain.FailureInput{Output: jobOutput, PackageJob: true})
			if f.Class != tt.want {
				t.Errorf("Klasse = %q, erwartet %q (%s)", f.Class, tt.want, f.Message)
			}
		})
	}
}
