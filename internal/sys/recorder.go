package sys

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"sync"
)

// OpKind classifies a recorded change for the review screen.
type OpKind string

const (
	OpRun     OpKind = "run"
	OpWrite   OpKind = "write"
	OpRemove  OpKind = "remove"
	OpRename  OpKind = "rename"
	OpMkdir   OpKind = "mkdir"
	OpSymlink OpKind = "symlink"
	OpFetch   OpKind = "fetch"
)

// Op is one recorded change.
type Op struct {
	Kind    OpKind
	Step    string
	Summary string // one line, secrets redacted
	Cmd     *Cmd   // for OpRun
	Path    string
	Content []byte // for OpWrite; nil when the file holds a secret
	Mode    fs.FileMode
}

// Recorder performs reads for real and records changes (rule 7).
type Recorder struct {
	Inner System

	mu   sync.Mutex
	step string
	ops  []Op
}

// NewRecorder wraps a real System.
func NewRecorder(inner System) *Recorder { return &Recorder{Inner: inner} }

// SetStep tags subsequent ops with a step ID.
func (r *Recorder) SetStep(id string) { r.mu.Lock(); r.step = id; r.mu.Unlock() }

// Ops returns everything recorded so far.
func (r *Recorder) Ops() []Op {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Op(nil), r.ops...)
}

func (r *Recorder) add(op Op) {
	r.mu.Lock()
	op.Step = r.step
	r.ops = append(r.ops, op)
	r.mu.Unlock()
}

func (r *Recorder) DryRun() bool                            { return true }
func (r *Recorder) ReadFile(p string) ([]byte, error)       { return r.Inner.ReadFile(p) }
func (r *Recorder) Stat(p string) (fs.FileInfo, error)      { return r.Inner.Stat(p) }
func (r *Recorder) Lstat(p string) (fs.FileInfo, error)     { return r.Inner.Lstat(p) }
func (r *Recorder) ReadDir(p string) ([]fs.DirEntry, error) { return r.Inner.ReadDir(p) }
func (r *Recorder) Glob(p string) ([]string, error)         { return r.Inner.Glob(p) }
func (r *Recorder) Readlink(p string) (string, error)       { return r.Inner.Readlink(p) }
func (r *Recorder) Query(ctx context.Context, c Cmd) (Result, error) {
	return r.Inner.Query(ctx, c)
}
func (r *Recorder) HTTPGet(ctx context.Context, url string, max int64) ([]byte, error) {
	return r.Inner.HTTPGet(ctx, url, max)
}

func (r *Recorder) Run(_ context.Context, c Cmd) (Result, error) {
	cc := c
	cc.Stdin = nil
	r.add(Op{Kind: OpRun, Summary: c.String(), Cmd: &cc})
	return Result{}, nil
}

func (r *Recorder) WriteFile(path string, data []byte, o WriteOpts) (bool, error) {
	if old, err := r.Inner.ReadFile(path); err == nil && string(old) == string(data) {
		return false, nil
	}
	mode := o.Mode
	if mode == 0 {
		mode = 0o644
	}
	content := data
	if mode.Perm()&0o077 == 0 {
		content = nil // never show files that only root may read (secrets)
	}
	r.add(Op{Kind: OpWrite, Path: path, Content: content, Mode: mode,
		Summary: fmt.Sprintf("write %s (%#o, %d bytes)", path, mode.Perm(), len(data))})
	return true, nil
}

func (r *Recorder) Remove(p string) error {
	r.add(Op{Kind: OpRemove, Path: p, Summary: "remove " + p})
	return nil
}
func (r *Recorder) RemoveAll(p string) error {
	r.add(Op{Kind: OpRemove, Path: p, Summary: "remove " + p + " (recursively)"})
	return nil
}
func (r *Recorder) Rename(a, b string) error {
	r.add(Op{Kind: OpRename, Path: b, Summary: "rename " + a + " → " + b})
	return nil
}
func (r *Recorder) MkdirAll(p string, m fs.FileMode, _ *User) error {
	if _, err := r.Inner.Stat(p); err == nil {
		return nil
	}
	r.add(Op{Kind: OpMkdir, Path: p, Mode: m, Summary: fmt.Sprintf("create directory %s (%#o)", p, m.Perm())})
	return nil
}
func (r *Recorder) Symlink(t, p string, _ *User) error {
	if cur, err := r.Inner.Readlink(p); err == nil && cur == t {
		return nil
	}
	r.add(Op{Kind: OpSymlink, Path: p, Summary: "link " + p + " → " + t})
	return nil
}
func (r *Recorder) Fetch(_ context.Context, q FetchReq) (string, error) {
	r.add(Op{Kind: OpFetch, Summary: "download " + q.URL})
	name := q.Name
	if name == "" {
		name = filepath.Base(q.URL)
	}
	return filepath.Join("/var/tmp/kiln.(preview)", name), nil
}
