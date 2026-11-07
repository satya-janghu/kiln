//go:build linux

package runner

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// startInhibitor holds sleep, idle, lid, power-key and shutdown blocks for the
// run (D41). The child is `systemd-inhibit … cat` reading a pipe only kiln
// holds open, so if kiln dies for any reason (panic, OOM, SIGKILL) the pipe
// closes, cat exits and the block is released. The returned function releases it.
func startInhibitor(log *slog.Logger) func() {
	rd, wr, err := os.Pipe()
	if err != nil {
		log.Warn("inhibitor", "err", err.Error())
		return func() {}
	}
	cmd := exec.CommandContext(context.Background(), "systemd-inhibit",
		"--what=sleep:idle:handle-lid-switch:handle-power-key:shutdown",
		"--mode=block", "--who=kiln",
		"--why=installing packages; rebooting now could break the system",
		"cat")
	cmd.Stdin = rd
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		log.Warn("inhibitor", "err", err.Error())
		_ = rd.Close()
		_ = wr.Close()
		return func() {}
	}
	_ = rd.Close() // only the child keeps the read end
	return func() {
		_ = wr.Close() // EOF → cat exits → systemd-inhibit releases the block
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
	}
}
