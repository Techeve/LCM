//go:build linux

package agent

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// ptyShell ist eine Login-Shell von root in einem Pseudo-Terminal.
type ptyShell struct {
	master *os.File
	cmd    *exec.Cmd
	done   chan struct{}
}

// startPTYShell öffnet ein Pseudo-Terminal und startet darin eine Login-Shell
// als root (der Agent läuft als root). Bewusst ohne Zusatzpaket: /dev/ptmx und
// zwei ioctls reichen unter Linux.
func startPTYShell(term string, cols, rows int) (shellProcess, error) {
	master, slave, err := openPTY()
	if err != nil {
		return nil, err
	}
	defer slave.Close()
	if err := setWindow(master, cols, rows); err != nil {
		_ = master.Close()
		return nil, err
	}
	shell := "/bin/bash"
	if _, err := os.Stat(shell); err != nil {
		shell = "/bin/sh"
	}
	if term == "" {
		term = "xterm-256color"
	}
	cmd := exec.Command(shell, "-l")
	cmd.Dir = "/root"
	cmd.Env = []string{
		"TERM=" + term, "HOME=/root", "USER=root", "LOGNAME=root", "SHELL=" + shell,
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"LANG=" + envOr("LANG", "C.UTF-8"),
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	// Eigene Sitzung mit dem Terminal als steuerndem: Strg+C und Job-Control
	// wirken in der Shell, und Close erreicht die ganze Prozessgruppe.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		_ = master.Close()
		return nil, fmt.Errorf("shell starten: %w", err)
	}
	p := &ptyShell{master: master, cmd: cmd, done: make(chan struct{})}
	go func() {
		_ = cmd.Wait()
		close(p.done)
	}()
	return p, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// openPTY öffnet Master und Slave eines Pseudo-Terminals. Die ioctls laufen
// über SyscallConn statt Fd(): Fd() schaltete den Master auf blockierend, und
// ein laufendes Read ließe sich dann mit Close nicht mehr beenden.
func openPTY() (master, slave *os.File, err error) {
	master, err = os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("pseudo-terminal öffnen: %w", err)
	}
	var n int
	if err = control(master, func(fd int) error {
		if err := unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); err != nil {
			return err
		}
		n, err = unix.IoctlGetInt(fd, unix.TIOCGPTN)
		return err
	}); err != nil {
		_ = master.Close()
		return nil, nil, fmt.Errorf("pseudo-terminal entsperren: %w", err)
	}
	slave, err = os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|syscall.O_NOCTTY|syscall.O_CLOEXEC, 0)
	if err != nil {
		_ = master.Close()
		return nil, nil, fmt.Errorf("pseudo-terminal öffnen: %w", err)
	}
	return master, slave, nil
}

func control(f *os.File, fn func(fd int) error) error {
	raw, err := f.SyscallConn()
	if err != nil {
		return err
	}
	var inner error
	if err := raw.Control(func(fd uintptr) { inner = fn(int(fd)) }); err != nil {
		return err
	}
	return inner
}

// setWindow setzt die Fenstergröße, gekappt wie beim SSH-Terminal.
func setWindow(master *os.File, cols, rows int) error {
	cols, rows = clampWindow(cols, rows)
	return control(master, func(fd int) error {
		return unix.IoctlSetWinsize(fd, unix.TIOCSWINSZ, &unix.Winsize{Col: uint16(cols), Row: uint16(rows)})
	})
}

func (p *ptyShell) Read(b []byte) (int, error)  { return p.master.Read(b) }
func (p *ptyShell) Write(b []byte) (int, error) { return p.master.Write(b) }
func (p *ptyShell) Resize(cols, rows int) error { return setWindow(p.master, cols, rows) }

// Close beendet die Shell mit allen Kindern: erst SIGHUP wie beim Auflegen,
// nach zwei Sekunden SIGKILL. Danach schließt es den Master, das beendet das
// laufende Read.
func (p *ptyShell) Close() error {
	select {
	case <-p.done:
		// Shell schon beendet und abgeholt: kein Signal mehr an eine
		// Prozessnummer, die inzwischen ein anderer haben könnte.
		return p.master.Close()
	default:
	}
	pgid := -p.cmd.Process.Pid
	_ = syscall.Kill(pgid, syscall.SIGHUP)
	select {
	case <-p.done:
	case <-time.After(2 * time.Second):
		_ = syscall.Kill(pgid, syscall.SIGKILL)
		<-p.done
	}
	return p.master.Close()
}
