// Package logx is kiln's logger: /var/log/kiln.log (0600) with secrets redacted.
package logx

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
)

// Path is the full log.
const Path = "/var/log/kiln.log"

// Redactor holds secret values to hide.
type Redactor struct {
	mu      sync.RWMutex
	secrets []string
}

// Add registers a secret.
func (r *Redactor) Add(v string) {
	if v == "" {
		return
	}
	r.mu.Lock()
	r.secrets = append(r.secrets, v)
	r.mu.Unlock()
}

// Apply redacts s.
func (r *Redactor) Apply(s string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, v := range r.secrets {
		s = strings.ReplaceAll(s, v, "***")
	}
	return s
}

type handler struct {
	inner slog.Handler
	red   *Redactor
}

func (h handler) Enabled(ctx context.Context, l slog.Level) bool { return h.inner.Enabled(ctx, l) }
func (h handler) Handle(ctx context.Context, rec slog.Record) error {
	out := slog.NewRecord(rec.Time, rec.Level, h.red.Apply(rec.Message), rec.PC)
	rec.Attrs(func(a slog.Attr) bool {
		if a.Value.Kind() == slog.KindString {
			a.Value = slog.StringValue(h.red.Apply(a.Value.String()))
		}
		out.AddAttrs(a)
		return true
	})
	return h.inner.Handle(ctx, out)
}
func (h handler) WithAttrs(as []slog.Attr) slog.Handler {
	return handler{h.inner.WithAttrs(as), h.red}
}
func (h handler) WithGroup(n string) slog.Handler { return handler{h.inner.WithGroup(n), h.red} }

// Level is the lowest level Open's loggers write. The wizard logs every query
// (debug); the boot/power-event/timer helpers set Info so the log doesn't grow
// by every read they make dozens of times a day.
var Level = slog.LevelDebug

// Open returns a logger writing to path (or io.Discard if it can't be opened).
func Open(path string, red *Redactor) (*slog.Logger, io.Closer) {
	var w io.Writer = io.Discard //nolint:staticcheck // assigned a *os.File below
	var c io.Closer = io.NopCloser(nil)
	if path != "" {
		if f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
			_ = f.Chmod(0o600)
			w, c = f, f
		}
	}
	h := slog.NewTextHandler(w, &slog.HandlerOptions{Level: Level})
	return slog.New(handler{h, red}), c
}
