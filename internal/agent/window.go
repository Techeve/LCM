package agent

// clampWindow hält die Fenstergröße in denselben Grenzen wie das
// SSH-Terminal (sshx): Ein Browser meldet gelegentlich 0 (Tab im
// Hintergrund) oder absurde Werte, und beides machte die Anzeige kaputt.
func clampWindow(cols, rows int) (int, int) {
	if cols < 1 {
		cols = 80
	}
	if rows < 1 {
		rows = 24
	}
	return min(cols, 500), min(rows, 300)
}
