//go:build linux

package runner

import (
	"os"
	"strconv"
	"strings"
	"syscall"
)

// LockDir holds kiln's locks (D41).
const LockDir = "/run/kiln"

// Lock takes an exclusive, non-blocking flock on /run/kiln/<name>.lock.
// held reports the holder's pid when the lock is busy.
func Lock(name string) (release func(), held int, err error) {
	if merr := os.MkdirAll(LockDir, 0o755); merr != nil {
		return nil, 0, merr
	}
	path := LockDir + "/" + name + ".lock"
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, 0, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		b, _ := os.ReadFile(path)
		pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
		_ = f.Close()
		return nil, pid, ErrBusy
	}
	_ = f.Truncate(0)
	_, _ = f.WriteAt([]byte(strconv.Itoa(os.Getpid())+"\n"), 0)
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }, 0, nil
}
