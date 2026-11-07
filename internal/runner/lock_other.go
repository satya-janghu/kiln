//go:build !linux

package runner

// LockDir holds kiln's locks.
const LockDir = "/run/kiln"

// Lock is a no-op off Linux.
func Lock(string) (func(), int, error) { return func() {}, 0, nil }
