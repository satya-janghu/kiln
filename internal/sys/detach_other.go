//go:build !linux

package sys

import "os/exec"

func detach(*exec.Cmd) {}
