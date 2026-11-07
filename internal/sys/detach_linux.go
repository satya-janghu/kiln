//go:build linux

package sys

import (
	"os/exec"
	"syscall"
)

// detach starts children in their own session: no controlling terminal, so the
// terminal's SIGINT/SIGHUP never reach apt or dpkg (D41). On a deadline the
// whole process group is killed, not just the direct child.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		// With Setsid the child's process group id equals its pid.
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
