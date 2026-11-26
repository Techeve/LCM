package mcp

import "time"

// SetRebindForTest verkürzt die Zeit, in der das erneute Binden wiederholt wird.
func SetRebindForTest(d time.Duration) (restore func()) {
	old := rebindFor
	rebindFor = d
	return func() { rebindFor = old }
}

// SetAfterReleaseForTest lässt f laufen, nachdem der Listener beim Wechsel von
// TLS seinen Port freigegeben hat und bevor er ihn erneut bindet.
func SetAfterReleaseForTest(f func(addr string)) (restore func()) {
	old := afterRelease
	afterRelease = f
	return func() { afterRelease = old }
}
