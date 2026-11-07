//go:build linux

package app

import (
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

// EnsureScope re-executes kiln inside its own transient scope in the system
// slice, so closing the terminal or ending the desktop session can't kill it
// mid-dpkg (D41). It returns only if re-exec isn't possible or already done.
func EnsureScope() {
	signal.Ignore(syscall.SIGHUP)
	if os.Getenv("KILN_IN_SCOPE") != "" || os.Geteuid() != 0 {
		return
	}
	run, err := exec.LookPath("systemd-run")
	if err != nil {
		return
	}
	self, err := os.Executable()
	if err != nil {
		return
	}
	if _, err := os.Stat("/run/systemd/system"); err != nil {
		return // not booted with systemd
	}
	if err := os.Setenv("KILN_IN_SCOPE", "1"); err != nil {
		return
	}
	args := append([]string{run, "--scope", "--quiet", "--collect", "--unit=kiln-run-" + RunID(), "--", self}, os.Args[1:]...)
	// Re-executes kiln itself with its own arguments; nothing is shell-interpreted.
	_ = syscall.Exec(run, args, os.Environ()) //nolint:gosec // G702: argv is kiln's own path and arguments
}
