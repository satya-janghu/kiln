//go:build !linux

package runner

import "log/slog"

func startInhibitor(*slog.Logger) func() { return func() {} }
