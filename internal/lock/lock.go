// Package lock provides engram's apply lock via advisory file locking (flock).
// Any writer that mutates a directory of memories (a harness render target, or
// the canonical root) takes this lock exclusively so no two applies interleave.
// A reader that must see the directory unchanged across several reads (a
// render apply from its canonical snapshot to its last write) takes it shared,
// which excludes writers but not other readers.
//
// The lock is an flock held on an open file descriptor, so the kernel releases
// it automatically when the holding process exits — including on a crash. That
// removes the whole notion of a "stale" lock: there is nothing to reclaim and no
// reclaim race. A lock file left on disk by a crashed run is inert (its mere
// presence never blocks anyone); exclusivity comes from the flock, not the file.
//
// The lock is advisory and assumes a local filesystem, which engram's canonical
// root and harness homes are.
package lock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// Name is the lock filename created inside the guarded directory.
const Name = ".engram.lock"

// ErrLocked matches (errors.Is) the error of an acquire refused because
// another holder has the lock.
var ErrLocked = errors.New("lock is held")

// heldError is a refused acquire: its own message, matching ErrLocked.
type heldError struct{ msg string }

func (e heldError) Error() string      { return e.msg }
func (heldError) Is(target error) bool { return target == ErrLocked }

// Acquire takes the exclusive lock in dir and returns a release func that frees
// it (call it, typically via defer, when the mutation completes). A second live
// holder — exclusive or shared, in this or any other process — fails it
// immediately rather than blocking or interleaving.
func Acquire(dir string) (func(), error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, Name)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, heldError{"another engram apply holds the lock at " + path}
		}
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	return unlocker(f), nil
}

// AcquireShared takes the lock in dir shared, for a reader that must see dir
// unchanged from its first read to its last: any number of readers hold it at
// once, and no exclusive holder (a writer) can while one does. Like Acquire it
// fails immediately, never waiting, when a writer holds the lock. Unlike
// Acquire it never creates dir: a missing dir fails with an error satisfying
// errors.Is(err, fs.ErrNotExist), since a reader must not turn "absent" into
// "empty".
func AcquireShared(dir string) (func(), error) {
	path := filepath.Join(dir, Name)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDONLY, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_SH|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, heldError{"an engram write holds the lock at " + path}
		}
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	return unlocker(f), nil
}

func unlocker(f *os.File) func() {
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}
}
