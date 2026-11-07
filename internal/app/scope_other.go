//go:build !linux

package app

// EnsureScope is a no-op off Linux.
func EnsureScope() {}
