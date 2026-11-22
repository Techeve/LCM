//go:build linux

package main

import (
	"log/slog"
	"syscall"

	"golang.org/x/sys/unix"
)

// dropAmbientCaps leert die Ambient-Capabilities des Prozesses. lcm.service
// gibt LCM CAP_NET_BIND_SERVICE als Ambient-Capability, damit es Ports unter
// 1024 (443, 80) öffnen darf. LCM selbst behält sie danach in Permitted und
// Effective. Ambient-Capabilities erbt aber jedes Programm, das LCM startet,
// und Bubblewrap bricht mit Capabilities ab („Unexpected capabilities but not
// setuid, old file caps config?"): Trivy liefe dann nie in der Sandbox. Leer
// erben Kindprozesse keine Capability mehr.
//
// Capabilities gelten je Thread, und Go startet Kindprozesse aus beliebigen
// Threads der Laufzeit. Darum in ALLEN Threads (AllThreadsSyscall); ein
// prctl nur im aufrufenden Thread ließ die übrigen Threads die Capability
// weiterreichen. AllThreadsSyscall geht nur ohne cgo - die Pakete werden mit
// CGO_ENABLED=0 gebaut.
func dropAmbientCaps() {
	_, _, errno := syscall.AllThreadsSyscall(unix.SYS_PRCTL, unix.PR_CAP_AMBIENT, unix.PR_CAP_AMBIENT_CLEAR_ALL, 0)
	if errno != 0 {
		slog.Warn("ambient capabilities not cleared - sandboxed tools may fail to start", "error", errno)
	}
}
