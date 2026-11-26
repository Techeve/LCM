//go:build !linux

package agent

import "errors"

// Ohne /proc (der Agent läuft nur unter Linux, dies hält Entwicklungsrechner
// übersetzbar) gibt es keine Beobachtung des Prozessbaums und keinen Platz-
// gewinn bei der Ausgabedatei.

func procStartTime(int) uint64 { return 0 }

func sameProcess(int, uint64) bool { return true }

func readBootID() string { return "" }

func groupIsOurs(int, uint64) bool { return true }

func reapWrapper(int, uint64) {}

func sampleSession(int) (sample, bool) { return sample{}, false }

func punchHole(string, int64, int64) error { return errors.New("nur unter linux") }
