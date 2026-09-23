package services

import (
	"testing"

	"LCM/internal/core/domain"
)

// Fuzz-Ziele für die Parser der Server-Ausgaben. Ein verwalteter Server ist
// eine fremde Maschine: Was er zurückschickt, bestimmt im Zweifel ein
// Angreifer. Kein Parser darf darauf mit einem Panic antworten - der Executor
// fängt Panics zwar ab, aber ein Job, der bei einer präparierten Ausgabe
// stirbt, ist ein Angriffsweg auf die Verfügbarkeit. Die Ziele prüfen genau
// das: beliebige Bytes hinein, kein Panic heraus.
//
// Lauf: go test ./internal/core/services -run '^$' -fuzz FuzzParseDpkgList -fuzztime 20s

var fuzzSeeds = []string{
	"", "\n", "\x00", "a\tb\tc\n", "\xff\xfe", "  \n\n\t", "0 0", "-1 -1",
	"nginx 1.22.1\nopenssl 3.0.11\n",
	"{\"broken\": [}", "9999999999999999999999 99999999999999999999",
}

func seed(f *testing.F) {
	for _, s := range fuzzSeeds {
		f.Add(s)
	}
}

func FuzzParseDpkgList(f *testing.F) {
	seed(f)
	f.Fuzz(func(t *testing.T, s string) { parseDpkgList(s) })
}
func FuzzParseRPMList(f *testing.F) {
	seed(f)
	f.Fuzz(func(t *testing.T, s string) { parseRPMList(s) })
}
func FuzzParseApkList(f *testing.F) {
	seed(f)
	f.Fuzz(func(t *testing.T, s string) { parseApkList(s) })
}
func FuzzParseSnapList(f *testing.F) {
	seed(f)
	f.Fuzz(func(t *testing.T, s string) { parseSnapList(s) })
}
func FuzzParseAptRepos(f *testing.F) {
	seed(f)
	f.Fuzz(func(t *testing.T, s string) { parseAptRepos(s) })
}
func FuzzParseRepoURIs(f *testing.F) {
	seed(f)
	f.Fuzz(func(t *testing.T, s string) { parseRepoURIs(s) })
}
func FuzzParseApkRepos(f *testing.F) {
	seed(f)
	f.Fuzz(func(t *testing.T, s string) { parseApkRepos(s) })
}
func FuzzParseDockerPS(f *testing.F) {
	seed(f)
	f.Fuzz(func(t *testing.T, s string) { parseDockerPS(s) })
}
func FuzzParseDockerImages(f *testing.F) {
	seed(f)
	f.Fuzz(func(t *testing.T, s string) { parseDockerImages(s) })
}
func FuzzParseListeningPorts(f *testing.F) {
	seed(f)
	f.Fuzz(func(t *testing.T, s string) { parseListeningPorts(s); parseListeningPortsJSON(s) })
}
func FuzzParseOSRelease(f *testing.F) {
	seed(f)
	f.Fuzz(func(t *testing.T, s string) { parseOSRelease(s); parseTwoInts(s) })
}
func FuzzParseDiskVolumes(f *testing.F) {
	seed(f)
	f.Fuzz(func(t *testing.T, s string) { parseDiskVolumes(s) })
}
func FuzzParseStorageHealth(f *testing.F) {
	seed(f)
	f.Fuzz(func(t *testing.T, s string) { parseStorageHealth(s) })
}
func FuzzParseServerUsers(f *testing.F) {
	seed(f)
	f.Fuzz(func(t *testing.T, s string) { parseServerUsers(s); parseLastLogins(s) })
}
func FuzzParseKernelList(f *testing.F) {
	seed(f)
	f.Fuzz(func(t *testing.T, s string) {
		for _, m := range []string{"apt", "dnf", "zypper", "pacman", "apk"} {
			parseKernelList(m, s)
		}
		domain.ParseKernelPackages(s)
	})
}
func FuzzParseDeepScan(f *testing.F) {
	seed(f)
	f.Fuzz(func(t *testing.T, s string) {
		parseDeepScanTools(s)
		parseNeedrestart(s)
		parseLynisReport(s)
		parseCuratedChecks(s)
	})
}
func FuzzParseHarden(f *testing.F) {
	seed(f)
	f.Fuzz(func(t *testing.T, s string) { parseHardenSuggestions(s); parseHardenState(s, "LCM") })
}
func FuzzParseTimeState(f *testing.F) {
	seed(f)
	f.Fuzz(func(t *testing.T, s string) { parseTimeState(s) })
}
func FuzzParseDNS(f *testing.F) {
	seed(f)
	f.Fuzz(func(t *testing.T, s string) { parseDNSTest(s); parseDNSList(s) })
}
func FuzzParseBans(f *testing.F) {
	seed(f)
	f.Fuzz(func(t *testing.T, s string) { parseBans("fail2ban", s); parseBans("crowdsec", s) })
}
func FuzzParseRouterOS(f *testing.F) {
	seed(f)
	f.Fuzz(func(t *testing.T, s string) { parseRouterOSKV(s); parseRouterOSVersion(s); parseRouterOSMemMB(s) })
}
func FuzzParseAcngStats(f *testing.F) {
	seed(f)
	f.Fuzz(func(t *testing.T, s string) { parseAcngStats(s) })
}
func FuzzParseFirewallRuleSpec(f *testing.F) {
	seed(f)
	f.Fuzz(func(t *testing.T, s string) {
		parseFirewallRuleSpec(s)
		parseFirewallPorts(s)
		parseSSHSources(s)
		parseFirewallDetect(s)
	})
}
func FuzzParsePkgVersions(f *testing.F) {
	seed(f)
	f.Fuzz(func(t *testing.T, s string) {
		for _, m := range []string{"apt", "dnf", "zypper", "pacman", "apk"} {
			parsePkgVersions(m, "nginx", s)
		}
		parseMadison(s)
	})
}
func FuzzParseAppScan(f *testing.F) {
	seed(f)
	f.Fuzz(func(t *testing.T, s string) { parseAppScan(s, nil); domain.ParseAppMarkers(s) })
}
func FuzzParseMisc(f *testing.F) {
	seed(f)
	f.Fuzz(func(t *testing.T, s string) {
		parseKeyValueLines(s)
		parseRHSMStatus(s)
		parseRootLoginVerification(s)
		parseHelperVersion(s)
		parseActionCommands(s)
		parsePackageNames(s)
		parseSnapNames(s)
		domain.ParseBlockPathRule(s)
		domain.ParseBlockValues(s)
		ParseAgentToken(s)
	})
}
