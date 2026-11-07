//go:build linux

package apt

import (
	"os"
	"syscall"
)

// dpkgLocked reports whether a process holds dpkg's frontend or database lock
// (fcntl locks, as dpkg and apt take them).
func dpkgLocked() bool {
	for _, p := range []string{"/var/lib/dpkg/lock-frontend", "/var/lib/dpkg/lock"} {
		f, err := os.OpenFile(p, os.O_RDWR, 0)
		if err != nil {
			continue
		}
		lk := syscall.Flock_t{Type: syscall.F_WRLCK, Whence: 0, Start: 0, Len: 0}
		err = syscall.FcntlFlock(f.Fd(), syscall.F_GETLK, &lk)
		_ = f.Close()
		if err == nil && lk.Type != syscall.F_UNLCK {
			return true
		}
	}
	return false
}
