package domain

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func intPtr(n int) *int { return &n }

// Echte Ausgaben, so wie die Werkzeuge sie schreiben. Sie bleiben ausführlich,
// weil der Klassifizierer genau auf deren Form angewiesen ist.
const (
	aptLockOutput = `Reading package lists... Done
E: Could not get lock /var/lib/dpkg/lock-frontend. It is held by process 1234 (unattended-upgr)
E: Unable to acquire the dpkg frontend lock (/var/lib/dpkg/lock-frontend), is another process using it?
`
	// LCMs eigene Zeile hängt die Kommandozeile an - sie darf nirgends landen.
	lcmLockOutput = `LCM: apt ist belegt (PID 1234 (unattended-upgr), laeuft seit 05:00: /usr/bin/python3 /usr/share/unattended-upgrades/unattended-upgrade-shutdown --wait-for-signal --password=geheim123) - warte
LCM: apt ist seit 10 Minuten belegt (PID 1234 (unattended-upgr), laeuft seit 10:00: /usr/bin/python3 /usr/share/unattended-upgrades/unattended-upgrade-shutdown --wait-for-signal --password=geheim123) - Lauf abgebrochen
`
	// Die Variante des echten Skripts: ohne Namen in Klammern.
	lcmLockNoName = `LCM: apt ist seit 10 Minuten belegt (PID 4321, laeuft seit 10:00: /usr/bin/apt-get install --token=geheim) - Lauf abgebrochen
`
	// lcm_apt_who ohne lesbaren Prozessnamen (/proc/<pid>/comm leer).
	lcmLockEmptyName = `LCM: apt ist seit 10 Minuten belegt (PID 77 (), laeuft seit 10:00: sleep 900) - Lauf abgebrochen
`
	scannerUnavailableOutput = `FEHLER: Trivy nicht verfügbar - CVE-Scan übersprungen. Trivy installieren, damit installierte Pakete geprüft werden.
`
	zypperLockOutput = `System management is locked by the application with pid 2210 (/usr/bin/zypper).
Close this application before trying again.
`
	dnfLockOutput = `Waiting for process with pid 3301 to finish.
`
	dpkgInterruptedOutput = `Reading package lists... Done
Building dependency tree... Done
E: dpkg was interrupted, you must manually run 'dpkg --configure -a' to correct the problem.
`
	brokenDepsOutput = `Reading package lists... Done
Building dependency tree... Done
Some packages could not be installed. This may mean that you have
requested an impossible situation or if you are using the unstable
distribution that some required packages have not yet been created
or been moved out of Incoming.
The following information may help to resolve the situation:

The following packages have unmet dependencies:
 libfoo : Depends: libbar (>= 2) but it is not going to be installed
 nginx : PreDepends: libssl3 (>= 3.0) but 2.9 is to be installed
E: Unable to correct problems, you have held broken packages.
`
	dnfBrokenOutput = `Error:
 Problem: conflicting requests
  - nothing provides libbar.so.2()(64bit) needed by foo-1.0-1.el9.x86_64
(try to add '--skip-broken' to skip uninstallable packages)
`
	zypperBrokenOutput = `Loading repository data...
Reading installed packages...
Resolving package dependencies...

Problem: nothing provides 'libbar2' needed by the to be installed foo-1.0-1.1.x86_64
 Solution 1: do not install foo-1.0-1.1.x86_64
`
	diskFullOutput = `Unpacking linux-image-6.8.0-45-generic (6.8.0-45.45) ...
dpkg: error processing archive /var/cache/apt/archives/linux-image-6.8.0-45-generic_6.8.0-45.45_amd64.deb (--unpack):
 cannot copy extracted data for './boot/vmlinuz-6.8.0-45-generic' to '/boot/vmlinuz-6.8.0-45-generic.dpkg-new': failed to write (No space left on device)
`
	aptFreeSpaceOutput = `Need to get 412 MB of archives.
After this operation, 1,204 MB of additional disk space will be used.
E: You don't have enough free space in /var/cache/apt/archives/.
`
	dnfDiskOutput = `Error: Transaction test error:
  installing package kernel-core-5.14.0-427.el9.x86_64 needs 74MB more space on the /boot filesystem
`
	aptDNSOutput = `Ign:1 http://archive.ubuntu.com/ubuntu jammy InRelease
Err:2 http://archive.ubuntu.com/ubuntu jammy-updates InRelease
  Temporary failure resolving 'archive.ubuntu.com'
Reading package lists... Done
W: Failed to fetch http://archive.ubuntu.com/ubuntu/dists/jammy-updates/InRelease  Temporary failure resolving 'archive.ubuntu.com'
W: Some index files failed to download. They have been ignored, or old ones used instead.
`
	dnfNetworkOutput = `CentOS Stream 9 - BaseOS                          0.0  B/s |   0  B     00:00
Curl error (28): Timeout was reached for https://mirrors.centos.org/metalink?repo=centos-baseos-9 [Connection timed out after 30000 milliseconds]
Error: Failed to download metadata for repo 'baseos': Cannot prepare internal mirrorlist: Curl error (28): Timeout was reached
`
	aptNoPubkeyOutput = `Get:1 http://archive.ubuntu.com/ubuntu jammy InRelease [270 kB]
Get:2 https://ppa.example.org/foo/ubuntu jammy InRelease [18.1 kB]
Err:2 https://ppa.example.org/foo/ubuntu jammy InRelease
  The following signatures couldn't be verified because the public key is not available: NO_PUBKEY 1A2B3C4D5E6F7A8B
Reading package lists... Done
W: GPG error: https://ppa.example.org/foo/ubuntu jammy InRelease: The following signatures couldn't be verified because the public key is not available: NO_PUBKEY 1A2B3C4D5E6F7A8B
E: The repository 'https://ppa.example.org/foo/ubuntu jammy InRelease' is not signed.
N: Updating from such a repository can't be done securely, and is therefore disabled by default.
`
	apt404Output = `Hit:1 http://archive.ubuntu.com/ubuntu jammy InRelease
Ign:2 http://ppa.launchpad.net/old/ppa/ubuntu jammy InRelease
Err:3 http://ppa.launchpad.net/old/ppa/ubuntu jammy Release
  404  Not Found [IP: 185.125.190.80 80]
Reading package lists... Done
E: The repository 'http://ppa.launchpad.net/old/ppa/ubuntu jammy Release' does not have a Release file.
`
	aptNotValidYetOutput = `Get:1 http://archive.ubuntu.com/ubuntu jammy InRelease [270 kB]
Reading package lists... Done
E: Release file for http://archive.ubuntu.com/ubuntu/dists/jammy/InRelease is not valid yet (invalid for another 2h 13min 5s). Updates for this repository will not be applied.
`
	aptExpiredOutput = `Reading package lists... Done
E: Release file for http://mirror.example.org/debian/dists/bookworm/InRelease is expired (invalid since 3d 2h 1min 4s). Updates for this repository will not be applied.
`
	dnfRepoOutput = `Errors during downloading metadata for repository 'baseos':
  - Status code: 404 for https://mirror.example.org/9/BaseOS/x86_64/os/repodata/repomd.xml (IP: 203.0.113.5)
Error: Failed to download metadata for repo 'baseos': Cannot download repomd.xml: Cannot download repodata/repomd.xml: All mirrors were tried
`
	zypperRepoOutput = `Retrieving repository 'repo-oss' metadata ........................[error]
Repository 'repo-oss' is invalid.
[repo-oss|http://download.example.org/distribution/leap/15.5/repo/oss/] Valid metadata not found at specified URL
History:
 - Download (curl) error for 'http://download.example.org/distribution/leap/15.5/repo/oss/repodata/repomd.xml':
Error code: Curl error 22
Error message: The requested URL returned error: 404
`
	timeoutOutput = `Reading package lists... Done
LCM: apt-get update nach 15 Minuten abgebrochen - antwortet eine Paketquelle oder der APT-Cache nicht?
`
	unknownOutput = `Setting up foo (1.0) ...
foo.postinst: line 12: /opt/foo/bin/setup: Permission denied
dpkg: error processing package foo (--configure):
 installed foo package post-installation script subprocess returned error exit status 126
Errors were encountered while processing:
 foo
`
)

func TestClassifyFailure(t *testing.T) {
	tests := []struct {
		name       string
		in         FailureInput
		class      string
		params     map[string]string
		msgContain string
	}{
		{"apt Sperre mit Halter", FailureInput{Output: aptLockOutput}, ErrClassLockBusy,
			map[string]string{"pid": "1234", "process": "unattended-upgr"}, "PID 1234 (unattended-upgr)"},
		{"LCM-Sperre über Exit-Code 75", FailureInput{ExitCode: intPtr(75), Output: lcmLockOutput}, ErrClassLockBusy,
			map[string]string{"pid": "1234", "process": "unattended-upgr"}, "PID 1234 (unattended-upgr)"},
		{"LCM-Sperre mit leerem Prozessnamen", FailureInput{ExitCode: intPtr(75), Output: lcmLockEmptyName}, ErrClassLockBusy,
			map[string]string{"pid": "77"}, "PID 77"},
		{"CVE-Scanner fehlt", FailureInput{ExitCode: intPtr(1), Output: scannerUnavailableOutput}, ErrClassScannerUnavailable,
			nil, "Trivy nicht verfügbar"},
		{"LCM-Sperre ohne Namen", FailureInput{ExitCode: intPtr(75), Output: lcmLockNoName}, ErrClassLockBusy,
			map[string]string{"pid": "4321"}, "PID 4321"},
		{"Paket-Job: Exit 75 ohne Ausgabe", FailureInput{ExitCode: intPtr(75), PackageJob: true}, ErrClassLockBusy, nil, ""},
		// Ein Skript darf 75 aus eigenem Grund liefern (EX_TEMPFAIL).
		{"Skript: Exit 75 ohne Sperrtext", FailureInput{ExitCode: intPtr(75), Output: "setup: Dienst nicht bereit"}, ErrClassUnknown, nil, "Dienst nicht bereit"},
		{"Skript: Sperrtext zählt trotzdem", FailureInput{ExitCode: intPtr(75), Output: aptLockOutput}, ErrClassLockBusy,
			map[string]string{"pid": "1234", "process": "unattended-upgr"}, "PID 1234"},
		{"zypper Sperre", FailureInput{Output: zypperLockOutput}, ErrClassLockBusy,
			map[string]string{"pid": "2210", "process": "/usr/bin/zypper"}, "PID 2210"},
		{"dnf Sperre", FailureInput{Output: dnfLockOutput}, ErrClassLockBusy,
			map[string]string{"pid": "3301"}, "PID 3301"},
		{"dpkg unterbrochen", FailureInput{ExitCode: intPtr(100), Output: dpkgInterruptedOutput}, ErrClassDpkgInterrupted,
			nil, "dpkg --configure -a"},
		{"apt unerfüllte Abhängigkeiten", FailureInput{ExitCode: intPtr(100), Output: brokenDepsOutput}, ErrClassBrokenDeps,
			map[string]string{"packages": "libfoo,nginx"}, "unmet dependencies"},
		{"dnf unerfüllte Abhängigkeiten", FailureInput{ExitCode: intPtr(1), Output: dnfBrokenOutput}, ErrClassBrokenDeps,
			map[string]string{"packages": "foo-1.0-1.el9.x86_64"}, "conflicting requests"},
		{"zypper unerfüllte Abhängigkeiten", FailureInput{ExitCode: intPtr(4), Output: zypperBrokenOutput}, ErrClassBrokenDeps,
			map[string]string{"packages": "foo-1.0-1.1.x86_64"}, "nothing provides"},
		{"Platte voll beim Entpacken", FailureInput{ExitCode: intPtr(100), Output: diskFullOutput}, ErrClassDiskFull,
			map[string]string{"path": "/boot/vmlinuz-6.8.0-45-generic.dpkg-new"}, "No space left on device"},
		{"apt zu wenig Platz", FailureInput{ExitCode: intPtr(100), Output: aptFreeSpaceOutput}, ErrClassDiskFull,
			map[string]string{"path": "/var/cache/apt/archives/"}, "enough free space"},
		{"dnf zu wenig Platz", FailureInput{ExitCode: intPtr(1), Output: dnfDiskOutput}, ErrClassDiskFull,
			map[string]string{"path": "/boot"}, "more space on the /boot filesystem"},
		{"apt DNS-Fehler ist Netz, nicht Quelle", FailureInput{ExitCode: intPtr(100), Output: aptDNSOutput}, ErrClassNetwork,
			nil, "Temporary failure resolving"},
		{"dnf Zeitüberschreitung ist Netz", FailureInput{ExitCode: intPtr(1), Output: dnfNetworkOutput}, ErrClassNetwork,
			nil, "Connection timed out"},
		{"apt NO_PUBKEY", FailureInput{ExitCode: intPtr(100), Output: aptNoPubkeyOutput}, ErrClassRepoError,
			map[string]string{"repo": "https://ppa.example.org/foo/ubuntu"}, "NO_PUBKEY"},
		{"apt 404", FailureInput{ExitCode: intPtr(100), Output: apt404Output}, ErrClassRepoError,
			map[string]string{"repo": "http://ppa.launchpad.net/old/ppa/ubuntu"}, "404  Not Found"},
		{"apt noch nicht gültig", FailureInput{ExitCode: intPtr(100), Output: aptNotValidYetOutput}, ErrClassRepoError,
			map[string]string{"repo": "http://archive.ubuntu.com/ubuntu/dists/jammy/InRelease", "not_valid_yet": "true"}, "not valid yet"},
		{"apt abgelaufen", FailureInput{ExitCode: intPtr(100), Output: aptExpiredOutput}, ErrClassRepoError,
			map[string]string{"repo": "http://mirror.example.org/debian/dists/bookworm/InRelease"}, "is expired"},
		{"dnf Metadaten", FailureInput{ExitCode: intPtr(1), Output: dnfRepoOutput}, ErrClassRepoError,
			map[string]string{"repo": "baseos"}, "Failed to download metadata"},
		{"zypper Quelle ungültig", FailureInput{ExitCode: intPtr(4), Output: zypperRepoOutput}, ErrClassRepoError,
			map[string]string{"repo": "repo-oss"}, "Repository 'repo-oss' is invalid"},
		{"LCM-Zeitüberschreitung", FailureInput{ExitCode: intPtr(124), Output: timeoutOutput}, ErrClassTimeout,
			nil, "apt-get update nach 15 Minuten abgebrochen"},
		{"Watchdog ohne Treffer", FailureInput{ByWatchdog: true, Output: "Unpacking foo ...\nSetting up foo ...\n"}, ErrClassTimeout,
			nil, "Setting up foo"},
		{"Exit 124", FailureInput{ExitCode: intPtr(124)}, ErrClassTimeout, nil, ""},
		{"unbekannt zeigt die letzten Zeilen", FailureInput{ExitCode: intPtr(100), Output: unknownOutput}, ErrClassUnknown,
			nil, "foo"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyFailure(tt.in)
			if got.Class != tt.class {
				t.Fatalf("Klasse = %q, erwartet %q (Meldung %q)", got.Class, tt.class, got.Message)
			}
			assertParams(t, got.Params, tt.params)
			if !strings.Contains(got.Message, tt.msgContain) {
				t.Errorf("Meldung %q enthält %q nicht", got.Message, tt.msgContain)
			}
			if n := utf8.RuneCountInString(got.Message); n > 500 {
				t.Errorf("Meldung hat %d Zeichen, erlaubt sind 500", n)
			}
		})
	}
}

func assertParams(t *testing.T, got, want map[string]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("Params = %v, erwartet %v", got, want)
		return
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("Params[%q] = %q, erwartet %q (alle: %v)", k, got[k], v, got)
		}
	}
}

// TestLockBusyMeldungEnthaeltKeineKommandozeile: LCMs Zeile hängt die
// Kommandozeile des Halters an, und die kann Zugangsdaten tragen.
func TestLockBusyMeldungEnthaeltKeineKommandozeile(t *testing.T) {
	for _, out := range []string{lcmLockOutput, lcmLockNoName} {
		got := ClassifyFailure(FailureInput{ExitCode: intPtr(75), Output: out})
		for _, secret := range []string{"geheim", "/usr/bin", "--", "laeuft seit"} {
			if strings.Contains(got.Message, secret) {
				t.Errorf("Meldung %q enthält %q", got.Message, secret)
			}
			for k, v := range got.Params {
				if strings.Contains(v, secret) {
					t.Errorf("Params[%s] = %q enthält %q", k, v, secret)
				}
			}
		}
	}
}

// TestSperreSchlaegtAndereMeldungen: die Reihenfolge ist Absicht - eine
// belegte Sperre ist die Ursache, die folgenden Fehler sind Folgen.
func TestSperreSchlaegtAndereMeldungen(t *testing.T) {
	out := "Err:1 http://x.example.org jammy InRelease\n  404  Not Found\n" + aptLockOutput
	if got := ClassifyFailure(FailureInput{Output: out}); got.Class != ErrClassLockBusy {
		t.Errorf("Klasse = %q, erwartet lock_busy", got.Class)
	}
}

// TestKlassifizierungLiestNurDasEnde: Eine Ausgabe von Megabytes darf die
// Prüfung nicht verlangsamen, und ein Treffer am Anfang zählt nicht mehr.
func TestKlassifizierungLiestNurDasEnde(t *testing.T) {
	filler := strings.Repeat("Unpacking libfoo (1.2.3) over (1.2.2) ...\n", 2000)
	t.Run("Treffer am Anfang liegt außerhalb", func(t *testing.T) {
		got := ClassifyFailure(FailureInput{Output: "E: dpkg was interrupted\n" + filler + "Fehler am Ende\n"})
		if got.Class != ErrClassUnknown {
			t.Errorf("Klasse = %q, erwartet unknown", got.Class)
		}
		if !strings.Contains(got.Message, "Fehler am Ende") {
			t.Errorf("Meldung %q", got.Message)
		}
	})
	t.Run("Treffer am Ende wird gefunden", func(t *testing.T) {
		got := ClassifyFailure(FailureInput{Output: filler + dpkgInterruptedOutput})
		if got.Class != ErrClassDpkgInterrupted {
			t.Errorf("Klasse = %q", got.Class)
		}
	})
	t.Run("angeschnittene erste Zeile zählt nicht", func(t *testing.T) {
		// Der Schnitt fällt mitten in eine Zeile, die den Marker nur halb enthält.
		out := "x" + strings.Repeat("y", classifyTailBytes) + "\n"
		lines := tailLines("E: dpkg was interrupted" + out)
		for _, l := range lines {
			if strings.Contains(l, "dpkg") {
				t.Errorf("Zeile %q gehört nicht in den Ausschnitt", l)
			}
		}
	})
}

func TestMeldungIstGekuerztUndOhneSteuerzeichen(t *testing.T) {
	long := "E: Unable to correct problems, you have held broken packages. " + strings.Repeat("ä", 900)
	got := ClassifyFailure(FailureInput{Output: long})
	if n := utf8.RuneCountInString(got.Message); n != 500 {
		t.Errorf("Meldung hat %d Zeichen, erwartet 500", n)
	}
	if !utf8.ValidString(got.Message) {
		t.Error("Meldung ist kein gültiges UTF-8")
	}

	got = ClassifyFailure(FailureInput{Output: "\x1b[31mE: dpkg was interrupted\x1b[0m\r\n"})
	if strings.ContainsAny(got.Message, "\x1b\r") {
		t.Errorf("Steuerzeichen in %q", got.Message)
	}
}

func TestBrokenDepsBegrenztDiePaketliste(t *testing.T) {
	var b strings.Builder
	b.WriteString("The following packages have unmet dependencies:\n")
	for i := 0; i < 25; i++ {
		b.WriteString(" pkg" + string(rune('a'+i)) + " : Depends: libx but it is not installable\n")
	}
	got := ClassifyFailure(FailureInput{Output: b.String()})
	if n := len(strings.Split(got.Params["packages"], ",")); n != 10 {
		t.Errorf("%d Pakete, erwartet 10", n)
	}
}

func TestClassifyAptUpdate(t *testing.T) {
	const allOK = `Hit:1 http://archive.ubuntu.com/ubuntu jammy InRelease
Get:2 http://archive.ubuntu.com/ubuntu jammy-updates InRelease [128 kB]
Get:3 http://security.ubuntu.com/ubuntu jammy-security InRelease [129 kB]
Get:4 http://archive.ubuntu.com/ubuntu jammy-updates/main amd64 Packages [1,500 kB]
Get:5 http://security.ubuntu.com/ubuntu jammy-security/main amd64 Packages [1,100 kB]
Fetched 2,857 kB in 2s (1,400 kB/s)
Reading package lists... Done
`
	const oneOfThree = `Hit:1 http://archive.ubuntu.com/ubuntu jammy InRelease
Get:2 http://security.ubuntu.com/ubuntu jammy-security InRelease [129 kB]
Ign:3 https://ppa.example.org/foo/ubuntu jammy InRelease
Err:3 https://ppa.example.org/foo/ubuntu jammy InRelease
  404  Not Found [IP: 203.0.113.9 443]
Reading package lists... Done
E: Failed to fetch https://ppa.example.org/foo/ubuntu/dists/jammy/InRelease  404  Not Found [IP: 203.0.113.9 443]
E: The repository 'https://ppa.example.org/foo/ubuntu jammy InRelease' is no longer signed.
`
	const allFailing = `Err:1 http://archive.ubuntu.com/ubuntu jammy InRelease
  Temporary failure resolving 'archive.ubuntu.com'
Err:2 http://security.ubuntu.com/ubuntu jammy-security InRelease
  Temporary failure resolving 'security.ubuntu.com'
Reading package lists... Done
W: Failed to fetch http://archive.ubuntu.com/ubuntu/dists/jammy/InRelease  Temporary failure resolving 'archive.ubuntu.com'
W: Some index files failed to download. They have been ignored, or old ones used instead.
`
	tests := []struct {
		name        string
		output      string
		sources     int
		failed      []string
		fatal       bool
		warnings    bool
		wantFailed  bool
		wantPartial bool
	}{
		{"alles in Ordnung", allOK, 3, nil, false, false, false, false},
		{"eine von drei Quellen scheitert", oneOfThree, 3, []string{"https://ppa.example.org/foo/ubuntu jammy"}, false, false, false, true},
		{"alle Quellen scheitern", allFailing, 2, []string{
			"http://archive.ubuntu.com/ubuntu jammy", "http://security.ubuntu.com/ubuntu jammy-security",
		}, false, true, true, false},
		{"allgemeines E: Sperre", "Reading package lists... Done\nE: Could not get lock /var/lib/apt/lists/lock. It is held by process 99 (apt)\nE: Unable to lock directory /var/lib/apt/lists/\n",
			0, nil, true, false, true, false},
		{"allgemeines E: dpkg", "Hit:1 http://archive.ubuntu.com/ubuntu jammy InRelease\nE: dpkg was interrupted, you must manually run 'dpkg --configure -a' to correct the problem.\n",
			1, nil, true, false, true, false},
		{"nur W:", "Hit:1 http://archive.ubuntu.com/ubuntu jammy InRelease\nReading package lists... Done\nW: Target Packages (main/binary-amd64/Packages) is configured multiple times in /etc/apt/sources.list:1 and /etc/apt/sources.list.d/x.list:1\n",
			1, nil, false, true, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyAptUpdate(tt.output)
			if got.Sources != tt.sources {
				t.Errorf("Sources = %d, erwartet %d", got.Sources, tt.sources)
			}
			if strings.Join(got.FailedSources, "|") != strings.Join(tt.failed, "|") {
				t.Errorf("FailedSources = %v, erwartet %v", got.FailedSources, tt.failed)
			}
			if got.FatalError != tt.fatal || got.Warnings != tt.warnings {
				t.Errorf("FatalError=%v Warnings=%v, erwartet %v/%v", got.FatalError, got.Warnings, tt.fatal, tt.warnings)
			}
			if got.Failed() != tt.wantFailed || got.PartiallyFailed() != tt.wantPartial {
				t.Errorf("Failed=%v PartiallyFailed=%v, erwartet %v/%v", got.Failed(), got.PartiallyFailed(), tt.wantFailed, tt.wantPartial)
			}
		})
	}
}

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

func TestClassifyZypperRefresh(t *testing.T) {
	const allOK = `Retrieving repository 'repo-oss (16.0)' metadata ...........[done]
Building repository 'repo-oss (16.0)' cache ...........[done]
Repository 'repo-openh264 (16.0)' is up to date.
All repositories have been refreshed.
`
	tests := []struct {
		name        string
		output      string
		sources     int
		failed      []string
		wantFailed  bool
		wantPartial bool
	}{
		{"alles in Ordnung", allOK, 2, nil, false, false},
		{"eine von drei Quellen scheitert", zypperRefreshPartialOutput, 3, []string{"kaputt"}, false, true},
		{"alle Quellen scheitern", zypperRefreshAllFailedOutput, 2, []string{"repo-openh264 (16.0)", "repo-oss (16.0)"}, true, false},
		{"Sperre ohne Quellen", "System management is locked by the application with pid 1234 (zypper).\n", 0, nil, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// ClassifyRefresh erkennt zypper an der Ausgabe, wo es welche gibt.
			got := ClassifyRefresh(tt.output)
			if got.Sources != tt.sources {
				t.Errorf("Sources = %d, erwartet %d", got.Sources, tt.sources)
			}
			if strings.Join(got.FailedSources, "|") != strings.Join(tt.failed, "|") {
				t.Errorf("FailedSources = %v, erwartet %v", got.FailedSources, tt.failed)
			}
			if got.Failed() != tt.wantFailed || got.PartiallyFailed() != tt.wantPartial {
				t.Errorf("Failed=%v PartiallyFailed=%v, erwartet %v/%v", got.Failed(), got.PartiallyFailed(), tt.wantFailed, tt.wantPartial)
			}
		})
	}
}
