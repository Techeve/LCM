//go:build !unix

package storage

// FreeDiskBytes ist auf Systemen ohne statfs nicht ermittelbar - LCM läuft
// produktiv nur unter Linux.
func FreeDiskBytes(string) (uint64, bool) { return 0, false }
