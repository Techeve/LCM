package main

import "testing"

// Der MR community -> beta kommt direkt aus community: Der Quell-Branch darf
// nach dem Merge nicht gelöscht werden.
func TestNurEigeneBranchesWerdenNachDemMergeGeloescht(t *testing.T) {
	for branch, want := range map[string]bool{
		"hotfix/auto-2.3.3":                    true,
		"merge/beta-2.4.0-beta.2-nach-develop": true,
		"community":                            false,
		"beta":                                 false,
		"hotfix/handarbeit":                    false,
	} {
		if got := removableBranch(branch); got != want {
			t.Errorf("removableBranch(%q) = %v, erwartet %v", branch, got, want)
		}
	}
}
