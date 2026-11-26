package main

import "testing"

func TestCompareVersions(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{
		{"2.3.3", "2.3.2", 1},
		{"2.3.2", "2.3.2", 0},
		{"2.4.0-beta.3", "2.3.3", 1},
		{"2.4.0-beta.9", "2.4.0", -1},
		{"2.4.0-beta.10", "2.4.0-beta.9", 1},
		{"v1.10.0", "v1.9.9", 1},
		{"v0.0.0-20230101120000-abcdef", "v0.0.0-20240101120000-fedcba", -1},
		{"v1.2.3+incompatible", "v1.2.3", 0},
	} {
		got, err := compareVersions(c.a, c.b)
		if err != nil || got != c.want {
			t.Errorf("compareVersions(%q, %q) = %d, %v; erwartet %d", c.a, c.b, got, err, c.want)
		}
	}
	if _, err := compareVersions("kaputt", "1.0.0"); err == nil {
		t.Error("ungültige Version wurde akzeptiert")
	}
}

func TestNextFreePatch(t *testing.T) {
	tags := []string{"v2.2.3", "v2.3.0", "v2.3.1", "v2.3.2", "v2.3.4-beta.1", "v2.2.5"}
	for _, c := range []struct{ line, want string }{
		{"2.3.2", "2.3.3"},
		{"2.3.3", "2.3.5"}, // 2.3.4 ist als Beta vergeben
		{"2.2.3", "2.2.4"},
		{"2.2.4", "2.2.6"}, // 2.2.5 gehört schon einer anderen Linie
	} {
		got, err := nextFreePatch(c.line, tags)
		if err != nil || got != c.want {
			t.Errorf("nextFreePatch(%q) = %q, %v; erwartet %q", c.line, got, err, c.want)
		}
	}
}
