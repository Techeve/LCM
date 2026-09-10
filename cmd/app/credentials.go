package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"LCM/internal/config"
	"LCM/internal/i18n"
	"LCM/internal/infrastructure/creds"
	"LCM/internal/infrastructure/crypto"
)

// Ablageorte der Umstellung auf systemd-Credentials.
const (
	// defaultCredstore ist das Verzeichnis, in dem systemd verschlüsselte
	// Credentials erwartet (root-eigen, 0700).
	defaultCredstore = "/etc/credstore.encrypted"
	// credentialsDropIn ist die Unit-Ergänzung, die das Credential lädt.
	credentialsDropIn = "credentials.conf"
)

// credentialsInit stellt den Master-Key von der Datei lcm.key auf ein
// systemd-Credential um: `lcm credentials init`.
//
// Danach liegt der Schlüssel verschlüsselt unter /etc/credstore.encrypted,
// gebunden an das TPM der Maschine und/oder an den root-eigenen
// Host-Schlüssel von systemd. Beim Start entschlüsselt systemd ihn in ein
// Verzeichnis im Arbeitsspeicher, das nur der Dienst sieht. Die Datei lcm.key
// wird nach erfolgreicher Gegenprobe vernichtet - im Datenverzeichnis liegt
// dann kein Klartext-Schlüssel mehr, und eine Kopie des Verzeichnisses ist
// ohne die Maschine wertlos.
func credentialsInit(configPath, dataDir string, args []string) error {
	fs := flag.NewFlagSet("credentials init", flag.ContinueOnError)
	withKey := fs.String("with-key", "auto", "Bindung des Credentials: auto, host, tpm2 oder host+tpm2 (auto = tpm2+host, wenn ein TPM da ist, sonst host)")
	unit := fs.String("unit", "lcm.service", "systemd-Unit, die das Credential bekommt")
	credstore := fs.String("credstore", defaultCredstore, "Verzeichnis für das verschlüsselte Credential")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if os.Geteuid() != 0 {
		return errors.New("credentials init braucht root (schreibt nach /etc)")
	}
	for _, tool := range []string{"systemd-creds", "systemctl"} {
		if _, err := exec.LookPath(tool); err != nil {
			return fmt.Errorf("%s nicht gefunden - systemd-Credentials brauchen systemd 250 oder neuer", tool)
		}
	}
	dataDir, err := resolveDataDir(dataDir)
	if err != nil {
		return err
	}
	if configPath == "" {
		configPath = filepath.Join(dataDir, config.DefaultFileName)
	}
	if _, err := config.LoadFrom(configPath); err != nil {
		return err
	}

	// Den Schlüssel nehmen, der JETZT gilt - aus der Umgebung oder der Datei.
	// Ein Credential darf hier nicht die Quelle sein (es würde nur sich selbst
	// neu schreiben), und erzeugt wird erst recht keiner: Ohne bestehenden
	// Schlüssel gibt es nichts umzustellen.
	keyPath := filepath.Join(dataDir, crypto.KeyFileName)
	key, source, err := crypto.LoadOrCreateMasterKey(dataDir)
	if err != nil {
		return err
	}
	switch source {
	case crypto.SourceGenerated:
		_ = crypto.ShredKeyFile(keyPath)
		return errors.New("kein Master-Key vorhanden - erst den Dienst einmal starten, dann umstellen")
	case crypto.SourceCredential:
		return errors.New("der Master-Key kommt bereits aus dem systemd-Credential - nichts zu tun")
	}

	// 1. Verschlüsseln - der Schlüssel geht über stdin an systemd-creds,
	//    nie über die Kommandozeile (Prozessliste).
	if err := os.MkdirAll(*credstore, 0o700); err != nil {
		return fmt.Errorf("%s anlegen: %w", *credstore, err)
	}
	target := filepath.Join(*credstore, creds.MasterKey)
	encrypt := exec.Command("systemd-creds", "encrypt", "--name="+creds.MasterKey, "--with-key="+*withKey, "-", target)
	encrypt.Stdin = bytes.NewReader(crypto.KeyFileContent(key))
	if out, err := encrypt.CombinedOutput(); err != nil {
		return fmt.Errorf("systemd-creds encrypt: %v - %s", err, bytes.TrimSpace(out))
	}
	if err := os.Chmod(target, 0o600); err != nil {
		return err
	}

	// 2. Gegenprobe: Kommt aus dem Credential exakt der Schlüssel zurück, mit
	//    dem die Datenbank verschlüsselt ist? Erst dann darf die Datei weg.
	decrypt := exec.Command("systemd-creds", "decrypt", "--name="+creds.MasterKey, target, "-")
	out, err := decrypt.Output()
	if err != nil {
		return fmt.Errorf("gegenprobe (systemd-creds decrypt) fehlgeschlagen: %w", err)
	}
	if !bytes.Equal(bytes.TrimSpace(out), bytes.TrimSpace(crypto.KeyFileContent(key))) {
		return errors.New("gegenprobe fehlgeschlagen: das Credential liefert nicht den aktuellen Schlüssel - lcm.key bleibt unverändert")
	}

	// 3. Unit-Ergänzung, damit systemd das Credential beim Start lädt.
	dropInDir := filepath.Join("/etc/systemd/system", *unit+".d")
	if err := os.MkdirAll(dropInDir, 0o755); err != nil {
		return err
	}
	dropIn := "# Von `lcm credentials init` geschrieben: Der Master-Key kommt aus dem\n" +
		"# systemd-Credential, nicht mehr aus /var/lib/lcm/lcm.key.\n" +
		"[Service]\n" +
		"LoadCredentialEncrypted=" + creds.MasterKey + ":" + target + "\n"
	if err := os.WriteFile(filepath.Join(dropInDir, credentialsDropIn), []byte(dropIn), 0o644); err != nil {
		return err
	}
	if out, err := exec.Command("systemctl", "daemon-reload").CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl daemon-reload: %v - %s", err, bytes.TrimSpace(out))
	}

	// 4. Klartext vernichten - bei der Datei. Eine Umgebungsvariable kann
	//    das Kommando nicht entfernen; das steht dann als Hinweis da.
	if source == crypto.SourceFile {
		if err := crypto.ShredKeyFile(keyPath); err != nil {
			return fmt.Errorf("credential eingerichtet, aber %s konnte nicht entfernt werden: %w", keyPath, err)
		}
	}

	fmt.Println(i18n.Tf(
		"Master key moved to the systemd credential %s (binding: %s). Unit drop-in: %s.",
		"Master-Key in das systemd-Credential %s überführt (Bindung: %s). Unit-Ergänzung: %s.",
		target, *withKey, filepath.Join(dropInDir, credentialsDropIn)))
	if source == crypto.SourceEnv {
		fmt.Println(i18n.T(
			"NOTE: the key still comes from "+crypto.EnvKeyName+" - remove that variable from the unit environment, otherwise it keeps taking precedence.",
			"HINWEIS: Der Schlüssel kommt weiterhin aus "+crypto.EnvKeyName+" - die Variable aus der Unit-Umgebung entfernen, sonst hat sie weiter Vorrang."))
	} else {
		fmt.Println(i18n.Tf(
			"%s has been destroyed. Restart the service now: systemctl restart %s",
			"%s wurde vernichtet. Jetzt den Dienst neu starten: systemctl restart %s",
			keyPath, *unit))
	}
	fmt.Println(i18n.T(
		"After the restart the journal shows: master key source=credential.",
		"Nach dem Neustart steht im Journal: master key source=credential."))
	return nil
}

// credentialHint sagt nach einer Rotation oder einem Restore, dass die Datei
// lcm.key wieder da ist, obwohl die Installation auf ein Credential umgestellt
// war - bis zur erneuten Umstellung liegt der Schlüssel im Klartext.
func credentialHint(credstore string) {
	if _, err := os.Stat(filepath.Join(credstore, creds.MasterKey)); err != nil {
		return
	}
	fmt.Println(i18n.T(
		"NOTE: this installation uses a systemd credential for the master key. Run `lcm credentials init` again to move the new key there and remove lcm.key.",
		"HINWEIS: Diese Installation nutzt ein systemd-Credential für den Master-Key. `lcm credentials init` erneut ausführen, damit der neue Schlüssel dorthin wandert und lcm.key verschwindet."))
}
