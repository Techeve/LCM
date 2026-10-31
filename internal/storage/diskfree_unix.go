//go:build unix

package storage

import "golang.org/x/sys/unix"

// FreeDiskBytes liefert den für LCM nutzbaren freien Platz im Verzeichnis dir.
func FreeDiskBytes(dir string) (uint64, bool) {
	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		return 0, false
	}
	return uint64(st.Bavail) * uint64(st.Bsize), true //nolint:unconvert // Feldtypen je System verschieden
}
