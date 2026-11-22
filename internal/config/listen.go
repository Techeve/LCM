package config

import (
	"fmt"
	"net"
	"strconv"
)

// validateListenAddress prüft eine Listen-Adresse der Form host:port oder
// :port. Der Host darf fehlen (alle Schnittstellen), der Port nicht.
func validateListenAddress(key, addr string) error {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("%s: %q ist keine Adresse der Form host:port oder :port", key, addr)
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("%s: ungültiger Port in %q", key, addr)
	}
	return nil
}
