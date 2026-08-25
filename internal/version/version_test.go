package version

import "testing"

// Die Commit-Kennung ist die einzige Angabe, die zweifelsfrei sagt, welcher
// Quellstand läuft - sie muss auch dann sauber funktionieren, wenn gar kein
// Commit injiziert wurde (go test, `go build` ohne -ldflags).
func TestShortCommitAndString(t *testing.T) {
	orig := Commit
	t.Cleanup(func() { Commit = orig })

	Version, Build = "1.5.1", "42"

	tests := []struct {
		name        string
		commit      string
		wantShort   string
		wantDirty   bool
		wantVersion string
	}{
		{
			name:        "ohne Commit bleibt das alte Format",
			commit:      "",
			wantShort:   "",
			wantVersion: "1.5.1 (Build 42)",
		},
		{
			name:        "voller SHA wird auf 12 Zeichen gekürzt",
			commit:      "0a5154cf8f361a7f48ac13eecacc90d8aa4c1762",
			wantShort:   "0a5154cf8f36",
			wantVersion: "1.5.1 (Build 42, 0a5154cf8f36)",
		},
		{
			name:        "dirty-Build bleibt als solcher erkennbar",
			commit:      "0a5154cf8f361a7f48ac13eecacc90d8aa4c1762-dirty",
			wantShort:   "0a5154cf8f36-dirty",
			wantDirty:   true,
			wantVersion: "1.5.1 (Build 42, 0a5154cf8f36-dirty)",
		},
		{
			name:        "kurzer SHA wird nicht aufgefüllt",
			commit:      "abc123",
			wantShort:   "abc123",
			wantVersion: "1.5.1 (Build 42, abc123)",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			Commit = tc.commit
			if got := ShortCommit(); got != tc.wantShort {
				t.Errorf("ShortCommit() = %q, erwartet %q", got, tc.wantShort)
			}
			if got := IsDirty(); got != tc.wantDirty {
				t.Errorf("IsDirty() = %v, erwartet %v", got, tc.wantDirty)
			}
			if got := String(); got != tc.wantVersion {
				t.Errorf("String() = %q, erwartet %q", got, tc.wantVersion)
			}
		})
	}
}
