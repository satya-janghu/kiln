package sys

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// BackupRoot holds first-write backups, mirroring absolute paths (D36).
const BackupRoot = "/var/lib/kiln/backup"

// WatchdogInterval is how long a command may be silent before a heartbeat is logged.
const WatchdogInterval = 10 * time.Minute

// Real changes the machine. Its mutating methods refuse unless GOOS is linux.
type Real struct {
	Log *slog.Logger

	mu     sync.Mutex
	tmpDir string
	client *http.Client
}

// NewReal returns a System that changes this machine.
func NewReal(log *slog.Logger) *Real {
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Real{Log: log, client: &http.Client{Timeout: 10 * time.Minute}}
}

func (r *Real) DryRun() bool { return false }

func (r *Real) canMutate() error {
	if runtime.GOOS != "linux" {
		return ErrUnsupported
	}
	return nil
}

func (r *Real) ReadFile(p string) ([]byte, error)                { return os.ReadFile(p) }
func (r *Real) Stat(p string) (fs.FileInfo, error)               { return os.Stat(p) }
func (r *Real) Lstat(p string) (fs.FileInfo, error)              { return os.Lstat(p) }
func (r *Real) ReadDir(p string) ([]fs.DirEntry, error)          { return os.ReadDir(p) }
func (r *Real) Glob(pattern string) ([]string, error)            { return filepath.Glob(pattern) }
func (r *Real) Readlink(p string) (string, error)                { return os.Readlink(p) }
func (r *Real) Query(ctx context.Context, c Cmd) (Result, error) { return r.exec(ctx, c, false) }

func (r *Real) Run(ctx context.Context, c Cmd) (Result, error) {
	if err := r.canMutate(); err != nil {
		return Result{}, err
	}
	return r.exec(ctx, c, true)
}

// neverKill lists programs kiln must never kill on a deadline (D41).
var neverKill = map[string]bool{"apt-get": true, "apt": true, "dpkg": true, "dpkg-reconfigure": true, "unattended-upgrade": true}

func (r *Real) exec(ctx context.Context, c Cmd, mutating bool) (Result, error) {
	name, args, env, dir := c.Name, c.Args, c.Env, c.Dir
	if c.As != nil {
		// User-run rule: clean environment, the user's HOME, absolute PATH.
		base := []string{
			"-u", c.As.Name, "--", "env", "-i",
			"HOME=" + c.As.Home, "USER=" + c.As.Name, "LOGNAME=" + c.As.Name,
			"SHELL=/bin/bash", "LANG=C.UTF-8",
			"PATH=" + c.As.Home + "/.local/bin:/usr/local/go/bin:" + c.As.Home + "/go/bin:/usr/local/bin:/usr/bin:/bin",
		}
		base = append(base, env...)
		args = append(append(base, name), args...)
		name, env = "runuser", nil
		if dir == "" {
			dir = c.As.Home
		}
	}
	timeout := c.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	runCtx := ctx
	cancel := func() {}
	if !neverKill[c.Name] {
		runCtx, cancel = context.WithTimeout(ctx, timeout)
	} else {
		// apt/dpkg are never killed: detach from cancellation entirely.
		runCtx = context.WithoutCancel(ctx)
	}
	defer cancel()

	cmd := exec.CommandContext(runCtx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	if c.Stdin != nil {
		cmd.Stdin = bytes.NewReader(c.Stdin)
	} // nil Stdin = /dev/null
	detach(cmd)

	shown := c.String()
	if mutating {
		r.Log.Info("run", "cmd", shown)
	} else {
		r.Log.Debug("query", "cmd", shown)
	}

	var mu sync.Mutex
	last := time.Now()
	sink := func(stream string) *lineWriter {
		return &lineWriter{fn: func(line string) {
			mu.Lock()
			last = time.Now()
			mu.Unlock()
			red := Redact(line, c.Redact)
			if mutating && !c.Quiet {
				r.Log.Debug(stream, "line", red)
			}
			if c.Output != nil {
				c.Output(red)
			}
		}}
	}
	outW, errW := sink("out"), sink("err")
	cmd.Stdout, cmd.Stderr = outW, errW
	// After the process exits, don't wait forever for grandchildren that kept
	// our pipes open (D41: nothing may hang past its deadline).
	cmd.WaitDelay = 15 * time.Second
	if err := cmd.Start(); err != nil {
		return Result{}, fmt.Errorf("could not start %s: %w (is the package that provides it installed?)", c.Name, err)
	}

	done := make(chan struct{})
	go func() {
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				mu.Lock()
				silent := time.Since(last)
				mu.Unlock()
				if silent >= WatchdogInterval {
					msg := fmt.Sprintf("still running after %s without output: %s", silent.Round(time.Minute), shown)
					r.Log.Warn("watchdog", "msg", msg)
					if c.Output != nil {
						c.Output(msg)
					}
				}
			}
		}
	}()
	werr := cmd.Wait()
	close(done)
	outW.flush()
	errW.flush()

	res := Result{Stdout: outW.String(), Stderr: errW.String()}
	if werr != nil && !errors.Is(werr, exec.ErrWaitDelay) {
		var ee *exec.ExitError
		if errors.As(werr, &ee) {
			res.Code = ee.ExitCode()
			if runCtx.Err() == context.DeadlineExceeded {
				return res, fmt.Errorf("%s did not finish within %s and was stopped", shown, timeout)
			}
			if mutating {
				r.Log.Info("exit", "cmd", shown, "code", res.Code)
			}
			return res, &ExitError{Cmd: shown, Code: res.Code, Stderr: Redact(res.Stderr, c.Redact)}
		}
		return res, werr
	}
	return res, nil
}

func (r *Real) HTTPGet(ctx context.Context, url string, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		maxBytes = 64 << 20
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "kiln")
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not download %s: %w (check the network)", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, &HTTPError{URL: url, Status: resp.StatusCode}
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > maxBytes {
		return nil, fmt.Errorf("%s is larger than the expected %d bytes", url, maxBytes)
	}
	return b, nil
}

// HTTPError is a non-200 HTTP response.
type HTTPError struct {
	URL    string
	Status int
}

func (e *HTTPError) Error() string { return fmt.Sprintf("%s returned HTTP %d", e.URL, e.Status) }

// privateDir returns this process's 0700 download directory (D41).
func (r *Real) privateDir() (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.tmpDir != "" {
		return r.tmpDir, nil
	}
	d, err := os.MkdirTemp("/var/tmp", "kiln.")
	if err != nil {
		return "", err
	}
	if err := os.Chmod(d, 0o700); err != nil {
		return "", err
	}
	r.tmpDir = d
	return d, nil
}

// Cleanup removes the private download directory.
func (r *Real) Cleanup() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.tmpDir != "" {
		_ = os.RemoveAll(r.tmpDir)
		r.tmpDir = ""
	}
}

func (r *Real) Fetch(ctx context.Context, q FetchReq) (string, error) {
	if err := r.canMutate(); err != nil {
		return "", err
	}
	dir, err := r.privateDir()
	if err != nil {
		return "", err
	}
	max := q.MaxBytes
	if max <= 0 {
		max = 2 << 30
	}
	name := q.Name
	if name == "" {
		name = filepath.Base(q.URL)
	}
	dst := filepath.Join(dir, filepath.Base(name))
	r.Log.Info("fetch", "url", q.URL, "dest", dst)
	ctx, cancel := context.WithTimeout(ctx, 60*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, q.URL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "kiln")
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return "", fmt.Errorf("could not download %s: %w (check the network)", q.URL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", &HTTPError{URL: q.URL, Status: resp.StatusCode}
	}
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, max+1))
	if cerr := f.Close(); err == nil { //nolint:gosec // closing the download file; error is checked below
		err = cerr
	}
	if err != nil {
		_ = os.Remove(dst)
		return "", fmt.Errorf("download of %s was interrupted: %w", q.URL, err)
	}
	if n > max {
		_ = os.Remove(dst)
		return "", fmt.Errorf("%s is larger than expected", q.URL)
	}
	sum := hex.EncodeToString(h.Sum(nil))
	r.Log.Info("fetched", "url", q.URL, "sha256", sum, "bytes", n)
	if q.SHA256 != "" && !strings.EqualFold(sum, q.SHA256) {
		_ = os.Remove(dst)
		return "", fmt.Errorf("checksum mismatch for %s: got %s, expected %s; the download may be corrupt or tampered with", q.URL, sum, q.SHA256)
	}
	return dst, nil
}

func (r *Real) WriteFile(path string, data []byte, o WriteOpts) (bool, error) {
	if err := r.canMutate(); err != nil {
		return false, err
	}
	return writeManaged(r.Log, path, data, o)
}

func (r *Real) Remove(path string) error {
	if err := r.canMutate(); err != nil {
		return err
	}
	if err := backupOnce(path); err != nil {
		return err
	}
	r.Log.Info("remove", "path", path)
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func (r *Real) RemoveAll(path string) error {
	if err := r.canMutate(); err != nil {
		return err
	}
	if path == "/" || path == "" || !filepath.IsAbs(path) {
		return fmt.Errorf("refusing to remove %q", path)
	}
	r.Log.Info("remove-all", "path", path)
	return os.RemoveAll(path)
}

func (r *Real) Rename(oldPath, newPath string) error {
	if err := r.canMutate(); err != nil {
		return err
	}
	r.Log.Info("rename", "from", oldPath, "to", newPath)
	return os.Rename(oldPath, newPath)
}

func (r *Real) MkdirAll(path string, mode fs.FileMode, owner *User) error {
	if err := r.canMutate(); err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if err := os.MkdirAll(path, mode); err != nil {
		return err
	}
	if err := os.Chmod(path, mode); err != nil {
		return err
	}
	if owner != nil {
		return os.Chown(path, owner.UID, owner.GID)
	}
	return nil
}

func (r *Real) Symlink(target, path string, owner *User) error {
	if err := r.canMutate(); err != nil {
		return err
	}
	if cur, err := os.Readlink(path); err == nil && cur == target {
		return nil
	}
	if err := backupOnce(path); err != nil {
		return err
	}
	tmp := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".kiln-tmp-"+strconv.FormatInt(time.Now().UnixNano(), 36))
	if err := os.Symlink(target, tmp); err != nil {
		return err
	}
	if owner != nil {
		if err := os.Lchown(tmp, owner.UID, owner.GID); err != nil {
			_ = os.Remove(tmp)
			return err
		}
	}
	r.Log.Info("symlink", "path", path, "target", target)
	return os.Rename(tmp, path)
}

// lineWriter splits a stream into lines for live output, keeping everything.
// Lines are capped at 64 KiB for display; the full text is kept in buf.
type lineWriter struct {
	mu      sync.Mutex
	buf     bytes.Buffer
	partial []byte
	fn      func(string)
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf.Write(p)
	w.partial = append(w.partial, p...)
	for {
		i := bytes.IndexByte(w.partial, '\n')
		if i < 0 {
			break
		}
		w.emit(w.partial[:i])
		w.partial = w.partial[i+1:]
	}
	if len(w.partial) > 64<<10 {
		w.emit(w.partial)
		w.partial = nil
	}
	return len(p), nil
}

func (w *lineWriter) emit(b []byte) {
	if len(b) > 64<<10 {
		b = b[:64<<10]
	}
	w.fn(strings.TrimRight(string(b), "\r"))
}

func (w *lineWriter) flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.partial) > 0 {
		w.emit(w.partial)
		w.partial = nil
	}
}

func (w *lineWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}
