//go:build !linux

package agent

import "errors"

// startPTYShell gibt es nur unter Linux; der Agent läuft nur dort.
func startPTYShell(string, int, int) (shellProcess, error) {
	return nil, errors.New("terminal nur unter linux")
}
