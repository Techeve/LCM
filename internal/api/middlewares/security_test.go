package middlewares

import (
	"strings"
	"testing"
)

// TestContentSecurityPolicyNextUI sichert die strenge Richtlinie der neuen
// Oberfläche: Wortlaut, und dass sie nirgends Inline oder eval erlaubt.
func TestContentSecurityPolicyNextUI(t *testing.T) {
	const want = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; " +
		"font-src 'self'; connect-src 'self'; worker-src 'self'; manifest-src 'self'; " +
		"object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'; " +
		"require-trusted-types-for 'script'; trusted-types lit-html lcm-ui"
	if contentSecurityPolicy != want {
		t.Errorf("contentSecurityPolicy weicht ab:\n got: %s\nwant: %s", contentSecurityPolicy, want)
	}
	for _, bad := range []string{"unsafe-inline", "unsafe-eval", "*", "http:", "https:"} {
		if strings.Contains(contentSecurityPolicy, bad) {
			t.Errorf("Richtlinie enthält %q", bad)
		}
	}
	seen := map[string]bool{}
	for _, directive := range strings.Split(contentSecurityPolicy, "; ") {
		name, _, _ := strings.Cut(directive, " ")
		if seen[name] {
			t.Errorf("Direktive %q doppelt", name)
		}
		seen[name] = true
	}
}
