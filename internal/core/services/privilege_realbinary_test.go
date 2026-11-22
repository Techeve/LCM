package services

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"LCM/internal/core/domain"
)

// TestRealBinaryExistsIgnoriertDasShim: im eingeschränkten Modus liegt für jedes
// Whitelist-Binary ein Shim im PATH. Der Test muss trotzdem nur das echte
// Binary finden - sonst meldet LCM „ufw" auf einem Server ohne ufw und scheitert
// erst beim `sudo -n ufw` („a password is required").
func TestRealBinaryExistsIgnoriertDasShim(t *testing.T) {
	dir := t.TempDir()
	shim := filepath.Join(dir, "sudo-bin")
	system := filepath.Join(dir, "system")
	writeSandboxFile(t, filepath.Join(shim, "ufw"), "#!/bin/sh\nexec sudo -n ufw \"$@\"\n", 0o755)
	writeSandboxFile(t, filepath.Join(system, "docker"), "#!/bin/sh\n", 0o755)

	// restrictedSecurePath durch das Test-Systemverzeichnis ersetzen; der PATH
	// der umgebenden Shell enthält das Shim wie im Restricted-Lauf.
	run := func(name string) bool {
		script := strings.ReplaceAll(realBinaryExists(name), restrictedSecurePath, system)
		cmd := exec.Command("sh", "-c", script)
		cmd.Env = []string{"PATH=" + shim + ":/usr/bin:/bin"}
		return cmd.Run() == nil
	}
	bare := exec.Command("sh", "-c", "command -v ufw >/dev/null 2>&1")
	bare.Env = []string{"PATH=" + shim + ":/usr/bin:/bin"}
	if bare.Run() != nil {
		t.Fatal("Voraussetzung: das bloße command -v findet das Shim")
	}
	if run("ufw") {
		t.Error("ufw nur als Shim vorhanden, darf nicht als installiert gelten")
	}
	if !run("docker") {
		t.Error("echtes Binary im System-PATH wird nicht gefunden")
	}
}

// TestShimNamenNutzenKeinBlossesCommandV: die Builder, die unter dem
// Restricted-PATH laufen und ein Whitelist-Binary suchen, dürfen es nicht per
// bloßem `command -v` tun (das Shim träfe immer).
func TestShimNamenNutzenKeinBlossesCommandV(t *testing.T) {
	builders := map[string]string{
		"firewallDetectCmd": firewallDetectCmd,
		"ufwEnable":         ufwEnableScript(sshRuleFor(22), nil),
		"ufwDisable":        ufwDisableScript(),
		"ufwAllowPort":      ufwAllowPortCmd(2222),
		"ufwRemovePort":     ufwRemovePortCmd(2222),
	}
	_, install := resolveFirewallBackend(&domain.Server{OSID: "ubuntu", PackageManager: "apt"}, "")
	builders["firewallInstallVerify"] = install

	for name, script := range builders {
		if !strings.Contains(script, realBinaryExists("ufw")) {
			t.Errorf("%s: prüft ufw nicht über realBinaryExists:\n%s", name, script)
		}
		bare := strings.ReplaceAll(script, "PATH="+restrictedSecurePath+" command -v", "")
		if strings.Contains(bare, "command -v ufw") {
			t.Errorf("%s: enthält ein bloßes command -v ufw:\n%s", name, script)
		}
	}
}
