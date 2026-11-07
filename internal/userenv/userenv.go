// Package userenv sets up the target user's ~/.bashrc.d: ~/.bashrc sources
// every ~/.bashrc.d/*.sh, so the dev steps (python, node, golang) can each drop
// in their own file. Each of those steps calls it, so each works on its own.
package userenv

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/satya-janghu/kiln/internal/action"
	"github.com/satya-janghu/kiln/internal/sys"
)

const (
	// Marker identifies kiln's loader in ~/.bashrc.
	Marker = "# kiln: load ~/.bashrc.d"
	loader = Marker + "\n" +
		`if [ -d "$HOME/.bashrc.d" ]; then for f in "$HOME"/.bashrc.d/*.sh; do [ -r "$f" ] && . "$f"; done; unset f; fi` + "\n"
	skelBashrc = "/etc/skel/.bashrc"
)

// Bashrc is the user's ~/.bashrc.
func Bashrc(u *sys.User) string { return filepath.Join(u.Home, ".bashrc") }

// BashrcD is the user's ~/.bashrc.d.
func BashrcD(u *sys.User) string { return filepath.Join(u.Home, ".bashrc.d") }

// want returns the ~/.bashrc content with the loader, and whether that differs
// from what's there. A missing ~/.bashrc starts from /etc/skel/.bashrc; the
// loader is appended once (the marker makes it idempotent).
func want(s sys.System, u *sys.User) ([]byte, bool) {
	cur, err := s.ReadFile(Bashrc(u))
	if err != nil {
		skel, serr := s.ReadFile(skelBashrc)
		if serr != nil {
			skel = nil
		}
		cur = skel
	} else if strings.Contains(string(cur), Marker) {
		return cur, false
	}
	out := string(cur)
	if out != "" && !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return []byte(out + "\n" + loader), true
}

// Check reports whether ~/.bashrc already loads ~/.bashrc.d (read-only). When
// it doesn't, detail says so.
func Check(s sys.System, u *sys.User) (ok bool, detail string) {
	if _, change := want(s, u); change {
		return false, Bashrc(u) + " doesn't load ~/.bashrc.d yet"
	}
	return true, ""
}

// Actions makes ~/.bashrc load ~/.bashrc.d and creates ~/.bashrc.d, as the
// user. The ~/.bashrc content is computed when the action runs, so an earlier
// step that already added the loader makes it a no-op.
func Actions(s sys.System, u *sys.User) []action.Action {
	var acts []action.Action
	if _, change := want(s, u); change {
		acts = append(acts, action.Custom{
			Desc: "add the ~/.bashrc.d loader to " + Bashrc(u) + " (0644, owned by " + u.Name + "; started from " + skelBashrc + " if it's missing)",
			K:    action.Files,
			Fn: func(_ context.Context, sy sys.System) error {
				content, change := want(sy, u)
				if !change {
					return nil
				}
				_, err := sy.WriteFile(Bashrc(u), content, sys.WriteOpts{Mode: 0o644, Owner: u})
				return err
			},
		})
	}
	return append(acts, action.Run{Desc: "create " + BashrcD(u) + " as " + u.Name, K: action.Files,
		Cmd: sys.Cmd{Name: "mkdir", Args: []string{"-p", BashrcD(u)}, As: u}})
}
