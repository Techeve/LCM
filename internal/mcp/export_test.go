package mcp

import "time"

// SetRebindForTest verkürzt die Zeit, in der das erneute Binden wiederholt wird.
func SetRebindForTest(d time.Duration) (restore func()) {
	old := rebindFor
	rebindFor = d
	return func() { rebindFor = old }
}
