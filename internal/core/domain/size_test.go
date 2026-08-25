package domain

import "testing"

// TestFormatMiB prüft die automatische Skalierung binärer Speichereinheiten.
func TestFormatMiB(t *testing.T) {
	cases := []struct {
		mb   int64
		want string
	}{
		{0, "0 MiB"},
		{512, "512 MiB"},
		{1023, "1023 MiB"},
		{1024, "1.0 GiB"},
		{8192, "8.0 GiB"},
		{40960, "40.0 GiB"},
		{102400, "100.0 GiB"},
		{1048576, "1.00 TiB"},   // 1024 GiB
		{2621440, "2.50 TiB"},   // 2,5 TiB
		{10485760, "10.00 TiB"}, // 10 TiB
	}
	for _, c := range cases {
		if got := FormatMiB(c.mb); got != c.want {
			t.Errorf("FormatMiB(%d) = %q, erwartet %q", c.mb, got, c.want)
		}
	}
}
