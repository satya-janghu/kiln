// Package run is how kiln changes the machine: it runs commands, writes files,
// downloads and drives apt, sending all output to the screen and the log
// (docs/architecture.md). Nothing here saves state between runs.
package run

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// LogPath is the only file kiln leaves behind (D55).
const LogPath = "/var/log/kiln.log"

// User is the person the machine is set up for.
type User struct {
	Name string
	UID  int
	GID  int
	Home string
}

// S is one kiln run. It is safe for use by one step at a time.
type S struct {
	ctx context.Context

	mu      sync.Mutex
	logf    io.Writer
	out     func(line string) // live output for the progress screen
	secrets []string

	notes   []string
	reboots []string
	secret  string // the MOK one-time password, shown once on the summary

	tmpDir string
}

// New starts a run that logs to w and shows command output through out.
func New(ctx context.Context, w io.Writer, out func(string)) *S {
	if out == nil {
		out = func(string) {}
	}
	return &S{ctx: ctx, logf: w, out: out}
}

// OpenLog opens /var/log/kiln.log for appending (0600).
func OpenLog() (*os.File, error) {
	return os.OpenFile(LogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
}

// Context returns the run's context.
func (s *S) Context() context.Context { return s.ctx }

// Log writes a line to the log only.
func (s *S) Log(format string, a ...any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	msg := s.redact(fmt.Sprintf(format, a...))
	if s.logf != nil {
		_, _ = fmt.Fprintf(s.logf, "%s %s\n", time.Now().Format("2006-01-02 15:04:05"), msg)
	}
}

// Say writes a line to the log and the progress screen.
func (s *S) Say(format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	s.Log("%s", msg)
	s.out(s.redactLocked(msg))
}

// Note adds a line to the summary.
func (s *S) Note(format string, a ...any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	msg := fmt.Sprintf(format, a...)
	for _, n := range s.notes {
		if n == msg {
			return
		}
	}
	s.notes = append(s.notes, msg)
}

// Reboot records why a reboot is needed.
func (s *S) Reboot(format string, a ...any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	msg := fmt.Sprintf(format, a...)
	for _, r := range s.reboots {
		if r == msg {
			return
		}
	}
	s.reboots = append(s.reboots, msg)
}

// SetSecret keeps the MOK one-time password for the summary and keeps it out of the log.
func (s *S) SetSecret(v string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.secret = v
	s.secrets = append(s.secrets, v)
}

// Results returns the summary notes, reboot reasons and the one-time password.
func (s *S) Results() (notes, reboots []string, secret string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.notes...), append([]string(nil), s.reboots...), s.secret
}

func (s *S) redact(v string) string {
	for _, x := range s.secrets {
		if x != "" {
			v = strings.ReplaceAll(v, x, "********")
		}
	}
	return v
}

func (s *S) redactLocked(v string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.redact(v)
}

// Cmd describes one command.
type Cmd struct {
	Name  string
	Args  []string
	Env   []string // added to kiln's environment
	Stdin string
	Dir   string
	Quiet bool // log the output but don't show it on the progress screen
	// Private keeps the output out of the log and the screen (secrets).
	Private bool
}

func (c Cmd) String() string {
	return strings.TrimSpace(c.Name + " " + strings.Join(c.Args, " "))
}

// ExitError is a command that ran and failed.
type ExitError struct {
	Cmd    string
	Code   int
	Output string // the last lines of output
}

func (e *ExitError) Error() string {
	if e.Output == "" {
		return fmt.Sprintf("%q failed (exit code %d)", e.Cmd, e.Code)
	}
	return fmt.Sprintf("%q failed (exit code %d): %s", e.Cmd, e.Code, e.Output)
}

// Exec runs a command, streams its output, and returns its stdout.
// stdin is /dev/null unless Stdin is set. The command never shares kiln's
// terminal (its own session), so keys and terminal signals never reach it.
func (s *S) Exec(c Cmd) (string, error) {
	// #nosec G204 -- arguments are passed as argv, never through a shell.
	cmd := exec.CommandContext(s.ctx, c.Name, c.Args...)
	cmd.Env = append(os.Environ(), c.Env...)
	cmd.Dir = c.Dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if c.Stdin != "" {
		cmd.Stdin = strings.NewReader(c.Stdin)
	}
	var stdout bytes.Buffer
	tail := &tailWriter{max: 6}
	lw := &lineWriter{emit: func(l string) {
		if c.Private {
			return
		}
		l = s.redactLocked(l)
		tail.add(l)
		s.Log("  %s", l)
		if !c.Quiet {
			s.out(l)
		}
	}}
	cmd.Stdout = io.MultiWriter(&stdout, lw)
	cmd.Stderr = lw
	s.Log("$ %s", c.String())
	err := cmd.Run()
	lw.flush()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return stdout.String(), &ExitError{Cmd: c.String(), Code: ee.ExitCode(), Output: tail.String()}
		}
		return stdout.String(), fmt.Errorf("could not run %q: %w", c.String(), err)
	}
	return stdout.String(), nil
}

// Run runs a command and shows its output.
func (s *S) Run(name string, args ...string) error {
	_, err := s.Exec(Cmd{Name: name, Args: args})
	return err
}

// Out runs a command quietly and returns its stdout.
func (s *S) Out(name string, args ...string) (string, error) {
	return s.Exec(Cmd{Name: name, Args: args, Quiet: true, Env: []string{"LC_ALL=C"}})
}

// OK reports whether a command exits 0 (quietly).
func (s *S) OK(name string, args ...string) bool {
	_, err := s.Out(name, args...)
	return err == nil
}

// UserCmd builds a command that runs as u with a clean environment
// (architecture: user commands). It never relies on ~/.bashrc.
func UserCmd(u *User, args ...string) Cmd {
	path := u.Home + "/.local/bin:/usr/local/go/bin:" + u.Home + "/go/bin:/usr/local/bin:/usr/bin:/bin"
	full := []string{"-u", u.Name, "--", "env", "-i",
		"HOME=" + u.Home, "USER=" + u.Name, "LOGNAME=" + u.Name,
		"SHELL=/bin/bash", "LANG=C.UTF-8", "PATH=" + path}
	return Cmd{Name: "runuser", Args: append(full, args...), Dir: u.Home}
}

// AsUser runs a command as u and shows its output.
func (s *S) AsUser(u *User, args ...string) error {
	_, err := s.Exec(UserCmd(u, args...))
	return err
}

// Read returns a file's contents, or "" if it can't be read.
func Read(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}

// Exists reports whether a path exists.
func Exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// Write writes a file atomically (temp file in the same directory, fsync,
// rename) with the given mode, owned by root. Identical content is left alone.
func (s *S) Write(path string, mode os.FileMode, content string) error {
	return s.WriteAs(path, mode, -1, -1, content)
}

// WriteAs is Write with an owner (-1 keeps root).
func (s *S) WriteAs(path string, mode os.FileMode, uid, gid int, content string) error {
	if cur, err := os.ReadFile(path); err == nil && string(cur) == content {
		if fi, err := os.Stat(path); err == nil && fi.Mode().Perm() == mode.Perm() {
			return nil
		}
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("could not create %s: %w", dir, err)
	}
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".kiln-*")
	if err != nil {
		return fmt.Errorf("could not write %s: %w", path, err)
	}
	tmp := f.Name()
	defer func() { _ = os.Remove(tmp) }()
	if _, err := f.WriteString(content); err != nil {
		_ = f.Close()
		return fmt.Errorf("could not write %s: %w", path, err)
	}
	if err := f.Chmod(mode); err != nil {
		_ = f.Close()
		return fmt.Errorf("could not set the mode of %s: %w", path, err)
	}
	if uid >= 0 {
		if err := f.Chown(uid, gid); err != nil {
			_ = f.Close()
			return fmt.Errorf("could not set the owner of %s: %w", path, err)
		}
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("could not write %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("could not write %s: %w", path, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("could not put %s in place: %w", path, err)
	}
	s.Log("wrote %s", path)
	return nil
}

// Remove deletes a file if it exists.
func (s *S) Remove(path string) error {
	err := os.Remove(path)
	if err == nil {
		s.Log("removed %s", path)
		return nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return fmt.Errorf("could not remove %s: %w", path, err)
}

// TempDir returns this run's private download directory under /var/tmp
// (never /tmp, a tmpfs on trixie), created on first use.
func (s *S) TempDir() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tmpDir != "" {
		return s.tmpDir, nil
	}
	d, err := os.MkdirTemp("/var/tmp", "kiln-")
	if err != nil {
		return "", fmt.Errorf("could not create a download directory in /var/tmp: %w", err)
	}
	s.tmpDir = d
	return d, nil
}

// Cleanup removes the download directory.
func (s *S) Cleanup() {
	s.mu.Lock()
	d := s.tmpDir
	s.tmpDir = ""
	s.mu.Unlock()
	if d != "" {
		_ = os.RemoveAll(d)
	}
}

// lineWriter splits output into lines.
type lineWriter struct {
	buf  []byte
	emit func(string)
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexAny(w.buf, "\r\n")
		if i < 0 {
			break
		}
		line := strings.TrimRight(string(w.buf[:i]), " \t")
		w.buf = w.buf[i+1:]
		if line != "" {
			w.emit(line)
		}
	}
	return len(p), nil
}

func (w *lineWriter) flush() {
	if len(w.buf) > 0 {
		w.emit(string(w.buf))
		w.buf = nil
	}
}

// tailWriter keeps the last lines of output for error messages.
type tailWriter struct {
	max   int
	lines []string
}

func (t *tailWriter) add(l string) {
	t.lines = append(t.lines, l)
	if len(t.lines) > t.max {
		t.lines = t.lines[1:]
	}
}

func (t *tailWriter) String() string { return strings.Join(t.lines, " / ") }

// Scan splits text into trimmed, non-empty lines.
func Scan(text string) []string {
	var out []string
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		if l := strings.TrimSpace(sc.Text()); l != "" {
			out = append(out, l)
		}
	}
	return out
}
