// Package sys is the only gateway between kiln and the machine.
//
// Every read, command and write goes through a System. Real changes the
// machine; Recorder (used by `kiln plan` and the review screen) performs all
// reads for real but only records changes. See docs/architecture.md.
package sys

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"
)

// ErrUnsupported is returned by Real's mutating methods on non-Linux hosts.
var ErrUnsupported = errors.New("kiln only changes Debian 13 machines; refusing on this platform")

// User is the account a command runs as (see the user-run rule in architecture.md).
type User struct {
	Name string
	UID  int
	GID  int
	Home string
}

// Cmd describes one command. Commands are never run through a shell.
type Cmd struct {
	Name    string
	Args    []string
	Env     []string // extra KEY=VALUE pairs
	Stdin   []byte   // never logged
	Dir     string
	As      *User         // nil = root
	Timeout time.Duration // 0 = DefaultTimeout; ignored (never killed) for apt/dpkg
	Redact  []string      // substrings replaced by *** in logs and plans
	Quiet   bool          // don't log output lines (e.g. password hashes)
	Output  func(line string)
}

// DefaultTimeout applies to commands without an explicit Timeout.
const DefaultTimeout = 30 * time.Minute

// String renders the command for logs and the plan, with secrets redacted.
func (c Cmd) String() string {
	parts := make([]string, 0, len(c.Args)+1)
	parts = append(parts, c.Name)
	for _, a := range c.Args {
		if a == "" || strings.ContainsAny(a, " \t\"'$`\\|&;<>()*?[]{}") {
			a = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		}
		parts = append(parts, a)
	}
	s := strings.Join(parts, " ")
	if c.As != nil {
		s = "(as " + c.As.Name + ") " + s
	}
	return Redact(s, c.Redact)
}

// Redact replaces every secret in s with ***.
func Redact(s string, secrets []string) string {
	for _, sec := range secrets {
		if sec != "" {
			s = strings.ReplaceAll(s, sec, "***")
		}
	}
	return s
}

// Result is a finished command's output.
type Result struct {
	Stdout string
	Stderr string
	Code   int
}

// ExitError reports a command that ran but exited non-zero.
type ExitError struct {
	Cmd    string
	Code   int
	Stderr string
}

func (e *ExitError) Error() string {
	msg := strings.TrimSpace(e.Stderr)
	if len(msg) > 400 {
		msg = msg[len(msg)-400:]
	}
	if msg == "" {
		return fmt.Sprintf("%s exited with status %d", e.Cmd, e.Code)
	}
	return fmt.Sprintf("%s exited with status %d: %s", e.Cmd, e.Code, msg)
}

// WriteOpts controls a managed write.
type WriteOpts struct {
	Mode     fs.FileMode // 0 = 0644
	Owner    *User       // nil = root:root
	NoBackup bool        // kiln's own state files are not backed up
}

// FetchReq downloads a file into kiln's private run directory.
type FetchReq struct {
	URL      string
	SHA256   string // hex; empty only for content verified another way (e.g. signatures)
	Name     string // file name inside the private directory
	MaxBytes int64  // 0 = 2 GiB
}

// System is everything kiln may do to a machine.
type System interface {
	// Reads: always performed for real, in both modes.
	ReadFile(path string) ([]byte, error)
	Stat(path string) (fs.FileInfo, error)
	Lstat(path string) (fs.FileInfo, error)
	ReadDir(path string) ([]fs.DirEntry, error)
	Glob(pattern string) ([]string, error)
	Readlink(path string) (string, error)
	// Query runs a read-only command (dpkg-query, systemctl is-active, ...).
	Query(ctx context.Context, c Cmd) (Result, error)
	// HTTPGet fetches a small document (key lists, JSON indexes, Release files).
	HTTPGet(ctx context.Context, url string, maxBytes int64) ([]byte, error)

	// Changes: performed by Real, recorded by Recorder.
	Run(ctx context.Context, c Cmd) (Result, error)
	WriteFile(path string, data []byte, o WriteOpts) (changed bool, err error)
	Remove(path string) error
	RemoveAll(path string) error
	Rename(oldPath, newPath string) error
	MkdirAll(path string, mode fs.FileMode, owner *User) error
	Symlink(target, path string, owner *User) error
	Fetch(ctx context.Context, r FetchReq) (localPath string, err error)

	// DryRun reports whether changes are only recorded.
	DryRun() bool
}
