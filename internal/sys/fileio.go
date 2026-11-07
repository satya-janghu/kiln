package sys

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"syscall"
)

// ManagedHeader returns the "Managed by kiln" header for a comment prefix.
func ManagedHeader(prefix string) string {
	return prefix + " Managed by kiln — edits may be overwritten on the next run.\n"
}

// writeManaged implements rule 4: atomic write, first-write backup, final
// mode from creation (D41), no-op when unchanged.
func writeManaged(log *slog.Logger, path string, data []byte, o WriteOpts) (bool, error) {
	mode := o.Mode
	if mode == 0 {
		mode = 0o644
	}
	uid, gid := 0, 0
	if o.Owner != nil {
		uid, gid = o.Owner.UID, o.Owner.GID
	}
	if fi, err := os.Stat(path); err == nil {
		old, rerr := os.ReadFile(path)
		if rerr == nil && bytes.Equal(old, data) && fi.Mode().Perm() == mode.Perm() && sameOwner(fi, uid, gid) {
			return false, nil
		}
		if !o.NoBackup {
			if err := backupOnce(path); err != nil {
				return false, fmt.Errorf("could not back up %s before changing it: %w", path, err)
			}
		}
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, err
	}
	var rnd [6]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return false, err
	}
	// Leading dot and a '.' in the name: sudoers.d, apt and run-parts ignore it.
	tmp := filepath.Join(dir, "."+filepath.Base(path)+".kiln-tmp-"+hex.EncodeToString(rnd[:]))
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode.Perm())
	if err != nil {
		return false, err
	}
	cleanup := func() { _ = os.Remove(tmp) }
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		cleanup()
		return false, err
	}
	if err := f.Chmod(mode.Perm()); err != nil { // umask may have narrowed it
		_ = f.Close()
		cleanup()
		return false, err
	}
	if err := f.Chown(uid, gid); err != nil {
		_ = f.Close()
		cleanup()
		return false, err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		cleanup()
		return false, err
	}
	if err := f.Close(); err != nil {
		cleanup()
		return false, err
	}
	if err := os.Rename(tmp, path); err != nil {
		cleanup()
		return false, err
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	log.Info("write", "path", path, "mode", fmt.Sprintf("%#o", mode.Perm()), "bytes", len(data))
	return true, nil
}

func sameOwner(fi fs.FileInfo, uid, gid int) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return true
	}
	return int(st.Uid) == uid && int(st.Gid) == gid
}

// BackupPath is where the first-write backup of path lives.
func BackupPath(path string) string { return filepath.Join(BackupRoot, path) }

// backupOnce copies path to the backup tree unless a backup already exists.
func backupOnce(path string) error {
	dst := BackupPath(path)
	if _, err := os.Lstat(dst); err == nil {
		return nil
	}
	fi, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	if fi.Mode()&fs.ModeSymlink != 0 {
		t, err := os.Readlink(path)
		if err != nil {
			return err
		}
		return os.Symlink(t, dst)
	}
	if !fi.Mode().IsRegular() {
		return nil
	}
	return CopyFile(path, dst)
}

// CopyFile copies a regular file preserving mode, owner and times. The copy is
// written to a temp file and renamed into place, so a power loss never leaves
// a truncated backup that would later be trusted (D41).
func CopyFile(src, dst string) error {
	fi, err := os.Stat(src)
	if err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	var rnd [6]byte
	if _, rerr := rand.Read(rnd[:]); rerr != nil {
		return rerr
	}
	tmp := dst + ".kiln-tmp-" + hex.EncodeToString(rnd[:])
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, fi.Mode().Perm())
	if err != nil {
		return err
	}
	fail := func(e error) error { _ = out.Close(); _ = os.Remove(tmp); return e }
	if _, err := io.Copy(out, in); err != nil {
		return fail(err)
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		_ = out.Chown(int(st.Uid), int(st.Gid))
	}
	_ = out.Chmod(fi.Mode().Perm())
	if err := out.Sync(); err != nil {
		return fail(err)
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	_ = os.Chtimes(tmp, fi.ModTime(), fi.ModTime())
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if d, err := os.Open(filepath.Dir(dst)); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}
