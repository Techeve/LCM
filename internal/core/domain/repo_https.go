package domain

import (
	"net/url"
	"strings"
)

// Rueckstellung der https-Umstellung: welche Paketquellen kommen dafuer
// ueberhaupt in Frage?
//
// Die Umstellung auf https (SecureRepositories) dreht pauschal jedes
// http:// in den apt-Quellen auf https://. Zurueck darf aber nur, was
// vorher WIRKLICH http war - eine Fremdquelle, die von Haus aus https
// spricht, auf http zu zwingen waere eine Verschlechterung.
//
// Dafuer gibt es zwei Erkenntnisquellen, in dieser Reihenfolge:
//
//  1. Das Protokoll der Umstellung. Seit dieser Version hebt LCM die
//     Sicherungskopien der geaenderten Dateien auf; daraus laesst sich
//     exakt ablesen, welche URLs LCM selbst umgestellt hat.
//  2. Die Standardquellen der Distribution. Auf Servern, die vor dieser
//     Version umgestellt wurden, gibt es kein Protokoll mehr. Dort bleiben
//     die Spiegel der Distribution als sichere Annahme: Sie sprechen beide
//     Protokolle, und sie sind der Fall, den die Umstellung praktisch immer
//     betrifft. Fremdquellen bleiben in diesem Fall aussen vor.
//
// In beiden Faellen bekommt der Anwender die Liste vor dem Ausfuehren zu
// sehen - geraten wird hier nichts hinter seinem Ruecken.

// distroMirrorSuffixes sind die Host-Endungen der Distributions-Spiegel von
// Debian, Ubuntu und Raspberry Pi OS. Bewusst als Endung, damit auch die
// Landesspiegel (ftp.de.debian.org, de.archive.ubuntu.com) erfasst sind.
//
// Die Liste ist eine Vorauswahl, keine Sicherheitsgrenze: Sie entscheidet
// nur, was LCM ohne eigenes Protokoll zur Rueckstellung vorschlaegt.
var distroMirrorSuffixes = []string{
	".debian.org",
	".ubuntu.com",
	".raspbian.org",
	".raspberrypi.org",
	".raspberrypi.com",
}

// RepoURI zieht die erste http(s)-URL aus einer Paketquellen-Zeile.
// Erfasst beide apt-Formate - die einzeilige ("deb [signed-by=…] https://…
// bookworm main") und deb822 ("URIs: https://…") - sowie die schon beim
// Scan auf die reine URL reduzierten rpm-/zypper-Eintraege.
// Ohne URL: leerer String.
func RepoURI(line string) string {
	for _, field := range strings.Fields(line) {
		if strings.HasPrefix(field, "http://") || strings.HasPrefix(field, "https://") {
			return strings.TrimRight(field, ",;")
		}
	}
	return ""
}

// unsafeURIChars sind die Zeichen, die eine URL fuer die Rueckstellung
// unbrauchbar machen: Sie landet dort in einer sed-Ersetzung und in einer
// Kommandozeile, die als root laeuft. In echten Paketquellen kommt keines von
// ihnen vor - eine URL mit solchen Zeichen wird deshalb gar nicht erst
// vorgeschlagen, statt sie irgendwie zu maskieren.
const unsafeURIChars = "&|$`'\"\\ \t\n<>;(){}*?[]!#"

// SafeRepoURI sagt, ob die URL gefahrlos in ein Kommando eingesetzt werden kann.
func SafeRepoURI(uri string) bool {
	return uri != "" && !strings.ContainsAny(uri, unsafeURIChars)
}

// IsDistroMirror sagt, ob die URL auf einen Spiegel der Distribution zeigt.
func IsDistroMirror(uri string) bool {
	parsed, err := url.Parse(uri)
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return false
	}
	for _, suffix := range distroMirrorSuffixes {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return false
}

// HTTPSRevertCandidates bestimmt, welche Quellen des Servers sich auf http
// zurueckstellen lassen: das Protokoll der Umstellung, sonst die
// Distributions-Spiegel, die aktuell auf https stehen.
//
// recorded sind die von LCM umgestellten http-URLs aus dem Protokoll auf dem
// Server, repos der aktuelle Bestand. Das Ergebnis enthaelt https-URLs in der
// Reihenfolge des Bestands, jede genau einmal.
func HTTPSRevertCandidates(recorded []string, repos []AptRepository) []string {
	rec := make(map[string]bool, len(recorded))
	for _, raw := range recorded {
		if uri := strings.TrimSpace(raw); uri != "" {
			rec["https://"+strings.TrimPrefix(uri, "http://")] = true
		}
	}
	seen := map[string]bool{}
	var out []string
	for _, repo := range repos {
		uri := RepoURI(repo.Line)
		if !strings.HasPrefix(uri, "https://") || seen[uri] || !SafeRepoURI(uri) {
			continue
		}
		// Mit Protokoll zaehlt allein das Protokoll: Was LCM nicht
		// umgestellt hat, war vorher schon https.
		if len(rec) > 0 && !rec[uri] {
			continue
		}
		if len(rec) == 0 && !IsDistroMirror(uri) {
			continue
		}
		seen[uri] = true
		out = append(out, uri)
	}
	return out
}

// Pruefung vor der https-Umstellung: Nicht jeder Spiegel spricht HTTPS. Die
// Landesspiegel von Ubuntu (de.archive.ubuntu.com & Co.) etwa nehmen auf
// Port 443 gar nichts an, ftp.de.debian.org hat kein passendes Zertifikat.
// Wer pauschal umstellt, bekommt deshalb auf vielen Servern nur den Rollback.
// LCM prueft die Quellen deshalb vorher einzeln, und zwar vom Server aus.

// InsecureRepoURIs sind die http-URLs der unverschluesselten Quellen, jede
// genau einmal, in der Reihenfolge des Bestands. Was SafeRepoURI nicht
// besteht, fehlt: Die URL landet in einer Kommandozeile, die als root laeuft.
func InsecureRepoURIs(repos []AptRepository) []string {
	seen := map[string]bool{}
	var out []string
	for _, repo := range repos {
		uri := RepoURI(repo.Line)
		if !repo.Insecure || !strings.HasPrefix(uri, "http://") || seen[uri] || !SafeRepoURI(uri) {
			continue
		}
		seen[uri] = true
		out = append(out, uri)
	}
	return out
}

// HTTPSProbe ist eine zu pruefende Quelle: ihre http-URL und die Datei, die
// ueber https abgerufen wird. URL ist leer, wenn sich aus der Zeile keine
// pruefbare Adresse ableiten laesst.
type HTTPSProbe struct {
	URI string
	URL string
}

// HTTPSProbes bestimmt fuer jede unverschluesselte Quelle die Probe. Massgeblich
// ist die erste Zeile mit dieser URL - die Release-Datei einer Suite genuegt,
// um zu sehen, ob der Spiegel HTTPS spricht.
func HTTPSProbes(repos []AptRepository) []HTTPSProbe {
	uris := InsecureRepoURIs(repos)
	probes := make([]HTTPSProbe, 0, len(uris))
	for _, uri := range uris {
		probes = append(probes, HTTPSProbe{URI: uri, URL: firstProbeURL(uri, repos)})
	}
	return probes
}

func firstProbeURL(uri string, repos []AptRepository) string {
	for _, repo := range repos {
		if RepoURI(repo.Line) == uri {
			if probe := HTTPSProbeURL(repo.Line); probe != "" {
				return probe
			}
		}
	}
	return ""
}

// HTTPSProbeURL leitet aus einer Quellen-Zeile die https-Adresse ihrer
// Release-Datei ab. Die Suite ist das Feld hinter der URL; endet sie auf „/",
// ist es ein flaches Repository ohne dists/. Ohne Suite oder mit Zeichen, die
// nicht in eine Kommandozeile gehoeren: leerer String.
func HTTPSProbeURL(line string) string {
	uri := RepoURI(line)
	if !strings.HasPrefix(uri, "http://") {
		return ""
	}
	suite := fieldAfter(line, uri)
	if suite == "" {
		return ""
	}
	base := "https://" + strings.TrimRight(strings.TrimPrefix(uri, "http://"), "/")
	probe := base + "/dists/" + suite + "/Release"
	if strings.HasSuffix(suite, "/") {
		probe = base + "/" + strings.TrimLeft(strings.TrimPrefix(suite, "./"), "/") + "Release"
	}
	if !SafeRepoURI(probe) {
		return ""
	}
	return probe
}

// fieldAfter liefert das Feld der Zeile, das auf want folgt.
func fieldAfter(line, want string) string {
	fields := strings.Fields(line)
	for i, field := range fields {
		if strings.TrimRight(field, ",;") == want && i+1 < len(fields) {
			return fields[i+1]
		}
	}
	return ""
}

// HTTPSMirrorAlternative schlaegt fuer eine Quelle ohne HTTPS einen Spiegel
// derselben Distribution vor, der HTTPS spricht: die zentralen Adressen von
// Ubuntu und Debian. Fuer alles andere kennt LCM keinen Ersatz - leerer
// String. Der Vorschlag ist nur ein Hinweis, LCM tauscht nichts selbst.
func HTTPSMirrorAlternative(uri string) string {
	parsed, err := url.Parse(uri)
	if err != nil {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	path := strings.TrimRight(parsed.Path, "/")
	switch {
	case host == "archive.ubuntu.com" || strings.HasSuffix(host, ".archive.ubuntu.com"):
		return "https://archive.ubuntu.com" + path
	case host == "ports.ubuntu.com" || strings.HasSuffix(host, ".ports.ubuntu.com"):
		return "https://ports.ubuntu.com" + path
	case host == "security.ubuntu.com":
		return "https://security.ubuntu.com" + path
	// archive.debian.org trägt nur abgelaufene Releases, die es auf
	// deb.debian.org nicht mehr gibt; debian-ports und Co. liegen dort auch nicht.
	case !strings.HasSuffix(host, ".debian.org") || host == "archive.debian.org":
		return ""
	case path == "/debian-security" || strings.HasPrefix(path, "/debian-security/"):
		return "https://security.debian.org/debian-security"
	case path == "/debian" || strings.HasPrefix(path, "/debian/"):
		return "https://deb.debian.org/debian"
	}
	return ""
}
