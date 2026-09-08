package main

import "testing"

// Tags wie im echten Repo: absteigend nach Version, Vorabversionen über dem
// Finale derselben Nummer (so sortiert git mit -v:refname).
var repoTags = []string{
	"v1.38.0", "v1.37.0", "v1.36.0-beta.1", "v1.36.0",
	"v1.30.8-beta.1", "v1.30.8", "v1.30.7-beta.1", "v1.30.6",
}

func TestLineTag(t *testing.T) {
	faelle := []struct {
		name    string
		version string
		tags    []string
		will    string
	}{
		{
			// Der Fall, der die Wartungslinie einmal auf 1.38.1 geschickt hat:
			// Der Hauptzweig steht bei 1.38.0, enterprise hängt bei 1.30.6.
			name: "Wartungslinie ignoriert den Hauptzweig", version: "1.30.6", tags: repoTags, will: "v1.30.6",
		},
		{
			name: "Hauptzweig nimmt sein eigenes Finale", version: "1.38.0", tags: repoTags, will: "v1.38.0",
		},
		{
			// prepare-release.sh ergänzt auf develop ein -beta.1, wenn ein
			// Aufwärtsmerge das Suffix verloren hat. Das zugehörige Tag gibt
			// es dann nicht - der Anker ist der numerische Teil.
			name: "ergänztes Suffix fällt auf das Finale zurück", version: "1.38.0-beta.1", tags: repoTags, will: "v1.38.0",
		},
		{
			name: "laufende Vorabversionsreihe", version: "1.36.0-beta.1", tags: repoTags, will: "v1.36.0-beta.1",
		},
		{
			// Version vorbereitet, aber noch nicht getaggt: das höchste Tag
			// darunter zählt.
			name: "Anker ohne eigenes Tag", version: "1.31.0", tags: repoTags, will: "v1.30.8-beta.1",
		},
		{
			name: "ohne VERSION-Datei das höchste Tag", version: "", tags: repoTags, will: "v1.38.0",
		},
		{
			name: "erstes Release", version: "0.1.0", tags: nil, will: "",
		},
		{
			name: "nur jüngere Tags als der Anker", version: "1.0.0", tags: repoTags, will: "",
		},
	}

	for _, f := range faelle {
		t.Run(f.name, func(t *testing.T) {
			if got := lineTag(f.tags, f.version); got != f.will {
				t.Errorf("lineTag(%q) = %q, erwartet %q", f.version, got, f.will)
			}
		})
	}
}
