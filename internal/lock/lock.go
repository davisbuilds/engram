// Package lock provides engram's exclusive apply lock. Any writer that mutates a
// directory of memories (a harness render target, or the canonical root) takes
// this lock so no two applies interleave and half-write a batch. The lock is a
// single file created with O_EXCL; a lock left behind by a crashed run is
// reclaimed once it ages past a staleness threshold, so one crash does not wedge
// every future apply.
package lock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// Name is the lock filename created inside the guarded directory.
const Name = ".engram.lock"

// DefaultStaleAfter is how long an existing lock must be untouched before a new
// acquirer treats it as orphaned by a crashed run and reclaims it. It is long
// enough that a legitimately in-progress apply is never stolen from.
const DefaultStaleAfter = 10 * time.Minute

// Acquire takes the exclusive lock in dir. It returns a release func that removes
// the lock; call it (typically via defer) when the mutation completes. A second
// live holder fails rather than interleaving. If an existing lock has not been
// modified for longer than staleAfter, it is assumed orphaned and reclaimed;
// pass staleAfter <= 0 to disable reclaim. Reclaim is best-effort: two processes
// racing to reclaim the same stale lock is possible but harmless for a
// single-user tool — one wins the re-create, the other fails cleanly.
func Acquire(dir string, staleAfter time.Duration) (func(), error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, Name)
	for attempt := 0; attempt < 2; attempt++ {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			// The pid is informational — it aids a human debugging a stuck lock;
			// exclusivity comes from O_EXCL, not from the content.
			_, _ = f.WriteString(strconv.Itoa(os.Getpid()) + "\n")
			_ = f.Close()
			return func() { _ = os.Remove(path) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		// The lock exists. On the first attempt only, reclaim it if it is stale.
		if attempt == 0 && staleAfter > 0 {
			if info, serr := os.Stat(path); serr == nil && time.Since(info.ModTime()) > staleAfter {
				reclaimStale(path, info.ModTime())
				continue
			}
		}
		return nil, fmt.Errorf("another engram apply holds the lock at %s", path)
	}
	return nil, fmt.Errorf("could not acquire lock at %s", path)
}

// reclaimStale removes an orphaned lock, but only if it is still the exact stale
// file first observed (unchanged mtime) — never a fresh lock a racing acquirer
// created in the meantime. Together with the O_EXCL re-create on the retry, this
// makes reclaim safe under contention: at most one acquirer wins the create, and
// no acquirer can unlink another's valid replacement. (A stale lock is always
// older than staleAfter, so a just-created replacement never matches its mtime.)
func reclaimStale(path string, observed time.Time) {
	if cur, err := os.Stat(path); err == nil && cur.ModTime().Equal(observed) {
		_ = os.Remove(path)
	}
}
