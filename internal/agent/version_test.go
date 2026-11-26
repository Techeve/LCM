package agent

import (
	"os"
	"path/filepath"
	"testing"
)

// TestReadPin: Der Pin wird für jeden Paketmanager an seiner Stelle erkannt.
func TestReadPin(t *testing.T) {
	cases := []struct {
		name, file, content string
		wantPinned          bool
		wantVersion, source string
	}{
		{"nichts", "", "", false, "", ""},
		{"apt", aptPinFile,
			"# Kommentar\nPackage: lcm-agent\nPin: version 2.5.0~beta.1\nPin-Priority: 1001\n",
			true, "2.5.0~beta.1", "apt-preferences"},
		{"apt, anderes Paket", aptPinFile, "Package: foo\nPin: version 1\nPin-Priority: 1001\n", false, "", ""},
		{"apk", apkWorldFile, "alpine-base\nlcm-agent=2.4.0-r0\n", true, "2.4.0-r0", "apk-world"},
		{"apk ohne Version", apkWorldFile, "alpine-base\nlcm-agent\n", false, "", ""},
		{"dnf", rpmVersionLock, "lcm-agent-0:2.4.0-1.*\n", true, "", "versionlock"},
		{"dnf5", dnf5VersionLock, "version = \"1.0\"\n\n[[packages]]\nname = \"lcm-agent\"\ncomment = \"Added by 'versionlock add' command on 2026-11-25 07:04:19\"\n\n[[packages.conditions]]\nkey = \"evr\"\ncomparator = \"=\"\nvalue = \"2.3.0-1\"\n",
			true, "2.3.0", "versionlock"},
		{"dnf5, anderes Paket vor dem Agenten", dnf5VersionLock, "version = \"1.0\"\n\n[[packages]]\nname = \"foo\"\n\n[[packages.conditions]]\nvalue = \"9-1\"\n\n[[packages]]\nname = \"lcm-agent\"\n",
			true, "", "versionlock"},
		{"dnf5, nur anderes Paket", dnf5VersionLock, "[[packages]]\nname = \"foo\"\n\n[[packages.conditions]]\nvalue = \"9-1\"\n", false, "", ""},
		{"dnf exclude", dnfRepoFile, "[techeve-lcm]\nname=Techeve\nexclude=lcm-agent\n", true, "", "dnf-exclude"},
		{"dnf excludepkgs mit Komma", dnfRepoFile, "[techeve-lcm]\nexcludepkgs = foo,lcm-agent\n", true, "", "dnf-exclude"},
		{"dnf exclude, anderes Paket", dnfRepoFile, "[techeve-lcm]\nexclude=lcm-agent-extra\n", false, "", ""},
		{"zypper", zypperLocksFile, "\ntype: package\nmatch_type: glob\ncase_sensitive: on\nsolvable_name: lcm-agent\n", true, "", "zypper-lock"},
		{"zypper, anderes Paket", zypperLocksFile, "\ntype: package\nsolvable_name: lcm-agent-extra\n", false, "", ""},
		{"pacman", pacmanConfFile, "[options]\nIgnorePkg = foo lcm-agent bar\n", true, "", "pacman-ignorepkg"},
		{"pacman, anderes Paket", pacmanConfFile, "[options]\nIgnorePkg = lcm-agent-extra\n", false, "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			if c.file != "" {
				writeFile(t, filepath.Join(root, c.file), c.content)
			}
			pinned, version, source := readPin(root)
			if pinned != c.wantPinned || version != c.wantVersion || source != c.source {
				t.Errorf("readPin = %v %q %q, erwartet %v %q %q", pinned, version, source, c.wantPinned, c.wantVersion, c.source)
			}
		})
	}
}

// TestCollectVersionInfo: Protokoll, Fähigkeiten, Pin und Rückfall-Stand in einem Aufruf.
func TestCollectVersionInfo(t *testing.T) {
	root := t.TempDir()
	old := UpdateDir
	UpdateDir = filepath.Join(root, "update")
	t.Cleanup(func() { UpdateDir = old })

	info := CollectVersionInfo(root, "2.4.0", "2.4.0 (Build 7)")
	if info.Version != "2.4.0" || info.FullVersion != "2.4.0 (Build 7)" || info.Protocol == 0 {
		t.Errorf("info = %+v", info)
	}
	if info.Pinned || info.Rollback.PrevBinary || info.Rollback.Mode != "normal" || info.Rollback.RolledBack != nil {
		t.Errorf("leeres System: %+v", info)
	}

	writeFile(t, filepath.Join(root, aptPinFile), "Package: lcm-agent\nPin: version 2.4.0\nPin-Priority: 1001\n")
	prev := filepath.Join(UpdateDir, prevBinaryName)
	writeFile(t, prev, "#!/bin/sh\necho 'lcm-agent 2.3.0 (Build 3)'\n")
	if err := os.Chmod(prev, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(UpdateDir, "starts"), "2\n")
	writeFile(t, filepath.Join(UpdateDir, "mode"), "prev\n")
	writeFile(t, filepath.Join(UpdateDir, "rolled-back"), "from=2.4.0\nto=2.3.0\nreason=crash-loop\nat=9\n")

	info = CollectVersionInfo(root, "2.4.0", "2.4.0")
	if !info.Pinned || info.PinnedVersion != "2.4.0" || info.PinSource != "apt-preferences" {
		t.Errorf("pin: %+v", info)
	}
	rb := info.Rollback
	if !rb.PrevBinary || rb.PrevVersion != "2.3.0" || rb.Mode != "prev" || rb.Starts != 2 ||
		rb.RolledBack == nil || rb.RolledBack.From != "2.4.0" {
		t.Errorf("rollback: %+v", rb)
	}
}
