//go:build !linux

package apt

func dpkgLocked() bool { return false }
