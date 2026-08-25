package services

import (
	"strings"
	"testing"

	"LCM/internal/core/domain"
)

// TestSSHOptionsBody prüft die Drop-in-Erzeugung: ein einzelner Port 22 ist
// die Vorgabe und braucht keine Zeile, mehrere Ports müssen VOLLSTÄNDIG
// dastehen, PermitRootLogin nur bei disableRoot.
func TestSSHOptionsBody(t *testing.T) {
	if got := sshOptionsBody(false, []int{22}); got != "" {
		t.Errorf("port 22 ohne root-sperre sollte leer sein, bekam %q", got)
	}
	if got := sshOptionsBody(true, []int{22}); got != "PermitRootLogin no\n" {
		t.Errorf("nur root-sperre: %q", got)
	}
	// Übergangsphase des Portwechsels: Debian/Ubuntu leiten die Ports der
	// Socket-Unit per sshd-socket-generator aus GENAU dieser Datei ab. Stand
	// die 22 hier nicht, öffnete der Übergang nur den neuen Port - der alte
	// fiel sofort weg, und ein Rückweg war nicht mehr möglich (live auf
	// Ubuntu 26.04 beobachtet).
	got := sshOptionsBody(true, []int{22, 2222})
	if !strings.Contains(got, "Port 22\n") || !strings.Contains(got, "Port 2222\n") || !strings.Contains(got, "PermitRootLogin no") {
		t.Errorf("übergang alt+neu muss BEIDE ports führen: %q", got)
	}
	if got := sshOptionsBody(false, []int{2222}); got != "Port 2222\n" {
		t.Errorf("nur custom port: %q", got)
	}
}

// TestApplySSHOptionsScript: leeres Drop-in => rm + reload; gesetztes => Inhalt
// + Rollback-Absicherung.
func TestApplySSHOptionsScript(t *testing.T) {
	empty := applySSHOptionsScript("")
	if !strings.Contains(empty, "rm -f "+sshOptionsDropinPath) || !strings.Contains(empty, "sshd -t") {
		t.Errorf("leeres drop-in sollte entfernen + prüfen: %q", empty)
	}
	set := applySSHOptionsScript("PermitRootLogin no\n")
	if !strings.Contains(set, "PermitRootLogin no") || !strings.Contains(set, sshOptionsDropinPath) {
		t.Errorf("gesetztes drop-in fehlt inhalt/pfad: %q", set)
	}
	if !strings.Contains(set, ".lcmbak") || !strings.Contains(set, "exit 1") {
		t.Errorf("rollback-absicherung fehlt: %q", set)
	}
}

// Socket-aktivierter sshd (Debian 13, neuere Ubuntu): Dort lauscht
// ssh.socket, und "Port" in sshd_config wird nie gelesen. Genau daran
// scheiterte der Portwechsel - das Drop-in wurde geschrieben, der neue Port
// blieb zu, die Verifikation schlug fehl. Der Port muss deshalb zusätzlich
// an der Socket-Unit gestellt werden.
func TestApplySSHSocketPortsScript(t *testing.T) {
	script := applySSHSocketPortsScript([]int{2222})
	for _, want := range []string{
		"ssh.socket", "sshd.socket", // beide Unit-Namen abdecken
		"ListenStream=\\nListenStream=2222", // geerbte Liste löschen, dann setzen
		"[Socket]",
		"systemctl daemon-reload",
		"systemctl restart",
		"10-lcm-port.conf",
	} {
		if !strings.Contains(strings.ReplaceAll(script, "\n", "\\n"), want) {
			t.Errorf("Socket-Skript enthält %q nicht:\n%s", want, script)
		}
	}

	// Übergangsphase: alter UND neuer Port müssen gleichzeitig lauschen,
	// sonst sperrt der Wechsel die laufende Verwaltung aus.
	both := applySSHSocketPortsScript([]int{22, 2222})
	if !strings.Contains(both, "ListenStream=22\nListenStream=2222") {
		t.Errorf("Übergang öffnet nicht beide Ports:\n%s", both)
	}

	// Zurück auf 22 = Vorgabe der Distribution: Drop-in verschwindet.
	back := applySSHSocketPortsScript([]int{22})
	if !strings.Contains(back, `want=''`) {
		t.Errorf("Rückkehr auf Port 22 entfernt das Drop-in nicht:\n%s", back)
	}
	// Der daemon-reload ist der entscheidende Schritt und darf nie
	// übersprungen werden: Er lässt den sshd-socket-generator neu laufen, der
	// die Ports auf Debian/Ubuntu aus sshd_config ableitet. Genau sein Fehlen
	// war die Ursache - LCM ließ bei Socket-Aktivierung jeden Reload aus.
	for _, script := range []string{script, both, back} {
		if !strings.Contains(script, "systemctl daemon-reload && systemctl restart") {
			t.Errorf("daemon-reload + Neustart fehlen:\n%s", script)
		}
	}
	if strings.Contains(back, "ListenStream=22") {
		t.Errorf("Port 22 darf kein Drop-in schreiben:\n%s", back)
	}
}

// Der Portwechsel muss BEIDE Wege stellen: sshd-Drop-in (klassisch) und
// Socket-Unit (socket-aktiviert). Welcher greift, entscheidet das Zielsystem.
func TestSSHOptionsApplyCmdCoversSocketActivation(t *testing.T) {
	srv := &domain.Server{SSHPort: 22}
	cmd := sshOptionsApplyCmd(srv, false, []int{22, 2222})
	if !strings.Contains(cmd, sshOptionsDropinPath) {
		t.Error("sshd-Drop-in fehlt")
	}
	if !strings.Contains(cmd, "10-lcm-port.conf") {
		t.Error("Socket-Drop-in fehlt - der Portwechsel bliebe auf Debian 13 wirkungslos")
	}

	// Eingeschränkter Modus läuft über den Helfer; der muss dasselbe können.
	restricted := &domain.Server{SSHPort: 22, RestrictedSudo: true}
	if got := sshOptionsApplyCmd(restricted, false, []int{2222}); !strings.Contains(got, "ssh-options") {
		t.Errorf("eingeschränkter Modus nutzt den Helfer nicht: %s", got)
	}
	if !strings.Contains(lcmHelperScript, "ssh_socket_ports") {
		t.Error("Helfer-Skript stellt die Socket-Unit nicht")
	}
}

// TestRootLoginVerificationErkenntWirkungsloseSperre: Der Fall aus der Praxis
// - LCM schreibt sein Drop-in, `sshd -t` nimmt es ab, und trotzdem bleibt der
// Root-Login offen, weil in der Hauptdatei ein `PermitRootLogin yes` VOR dem
// Include steht. sshd nimmt bei mehrfacher Definition die erste.
//
// Vorher meldete LCM in genau diesem Fall Erfolg und merkte sich „gesperrt",
// während der Deep Scan unverändert „Root-Login erlaubt" fand.
func TestRootLoginVerificationErkenntWirkungsloseSperre(t *testing.T) {
	out := `LCMEFFECTIVE=yes
LCMSOURCES
/etc/ssh/sshd_config:32:PermitRootLogin yes
/etc/ssh/sshd_config.d/10-lcm-ssh.conf:2:PermitRootLogin no`

	effective, sources := parseRootLoginVerification(out)
	if effective != "yes" {
		t.Fatalf("effektiver wert: erwartet 'yes', bekam %q", effective)
	}
	if len(sources) != 2 {
		t.Fatalf("erwartet 2 fundstellen, bekam %v", sources)
	}

	err := rootLoginMismatch(true, effective, sources)
	if err == nil {
		t.Fatal("eine wirkungslose sperre muss als fehler gemeldet werden")
	}
	// Die Meldung muss den Grund transportieren, nicht nur das Symptom -
	// sonst sucht man den Fehler in LCM statt in der Datei.
	msg := err.Error()
	if !strings.Contains(msg, "/etc/ssh/sshd_config:32") {
		t.Errorf("die fundstellen fehlen in der meldung: %s", msg)
	}
	if !strings.Contains(msg, "ERSTE") {
		t.Errorf("die meldung sollte die sshd-Regel benennen: %s", msg)
	}
}

// TestRootLoginVerificationAkzeptiertWirksameSperre: Greift die Einstellung,
// gibt es nichts zu melden - in beide Richtungen.
func TestRootLoginVerificationAkzeptiertWirksameSperre(t *testing.T) {
	effective, sources := parseRootLoginVerification("LCMEFFECTIVE=no\nLCMSOURCES\n/etc/ssh/sshd_config.d/10-lcm-ssh.conf:2:PermitRootLogin no")
	if effective != "no" {
		t.Fatalf("erwartet 'no', bekam %q", effective)
	}
	if err := rootLoginMismatch(true, effective, sources); err != nil {
		t.Errorf("wirksame sperre darf nicht meckern: %v", err)
	}
	// Wieder freigegeben: „yes" ist dann der erwartete Zustand.
	if err := rootLoginMismatch(false, "yes", nil); err != nil {
		t.Errorf("freigabe darf nicht meckern: %v", err)
	}
	// prohibit-password ist KEINE vollständige Sperre - wer „no" verlangt,
	// bekommt hier einen Widerspruch gemeldet.
	if err := rootLoginMismatch(true, "prohibit-password", nil); err == nil {
		t.Error("prohibit-password erfüllt eine geforderte vollsperre nicht")
	}
	// Freigabe mit prohibit-password ist dagegen in Ordnung: root kommt rein.
	if err := rootLoginMismatch(false, "prohibit-password", nil); err != nil {
		t.Errorf("prohibit-password ist eine erlaubte form der freigabe: %v", err)
	}
}

// TestRootLoginVerificationOhneErmittelbarenWert: Liefert `sshd -T` nichts
// (fehlende Rechte, exotisches System), ist das kein Widerspruch - es wird
// nur nicht bestätigt. Ein harter Fehlschlag wäre hier falsch.
func TestRootLoginVerificationOhneErmittelbarenWert(t *testing.T) {
	effective, _ := parseRootLoginVerification("LCMEFFECTIVE=\nLCMSOURCES")
	if effective != "" {
		t.Fatalf("erwartet leeren wert, bekam %q", effective)
	}
	if err := rootLoginMismatch(true, effective, nil); err != nil {
		t.Errorf("nicht ermittelbar darf kein fehlschlag sein: %v", err)
	}
}
