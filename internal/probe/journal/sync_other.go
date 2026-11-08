//go:build !linux

package journal

import "os"

// datasync: Außerhalb von Linux gibt es kein fdatasync, Sync tut dasselbe
// mit Metadaten. Geschrieben wird dort nur in Tests.
func datasync(f *os.File) error { return f.Sync() }
