package journal

import (
	"os"

	"golang.org/x/sys/unix"
)

// datasync bringt die Daten auf die Platte, ohne die Metadaten (Zeitstempel)
// mitzuschreiben - das spart bei 43.200 Zeilen am Tag den halben Aufwand.
func datasync(f *os.File) error { return unix.Fdatasync(int(f.Fd())) }
