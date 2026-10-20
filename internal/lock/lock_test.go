package lock

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestAcquireIsExclusive(t *testing.T) {
	dir := t.TempDir()
	release, err := Acquire(dir)
	if err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}
	if _, err := Acquire(dir); err == nil {
		t.Error("second acquire should fail while the lock is held")
	}
	release()
	release2, err := Acquire(dir)
	if err != nil {
		t.Fatalf("acquire after release failed: %v", err)
	}
	release2()
}

// Readers share the lock with each other and exclude a writer: any number of
// shared holders coexist, an exclusive acquire fails while one remains, and a
// shared acquire fails while a writer holds the lock. Every refusal is
// immediate, never a wait.
func TestSharedReadersExcludeAWriter(t *testing.T) {
	dir := t.TempDir()
	r1, err := AcquireShared(dir)
	if err != nil {
		t.Fatalf("first shared acquire failed: %v", err)
	}
	r2, err := AcquireShared(dir)
	if err != nil {
		t.Fatalf("a second reader must share the lock: %v", err)
	}
	if _, err := Acquire(dir); !errors.Is(err, ErrLocked) {
		t.Fatalf("an exclusive acquire must fail with ErrLocked while readers hold the lock, got %v", err)
	}
	r1()
	if _, err := Acquire(dir); err == nil {
		t.Fatal("an exclusive acquire must fail while any reader still holds the lock")
	}
	r2()
	w, err := Acquire(dir)
	if err != nil {
		t.Fatalf("exclusive acquire after every reader released failed: %v", err)
	}
	if _, err := AcquireShared(dir); !errors.Is(err, ErrLocked) {
		t.Fatalf("a shared acquire must fail with ErrLocked while a writer holds the lock, got %v", err)
	}
	w()
	r3, err := AcquireShared(dir)
	if err != nil {
		t.Fatalf("shared acquire after the writer released failed: %v", err)
	}
	r3()
}

// A reader never creates the directory it reads: a missing canonical root is
// a signal (renders are kept, not pruned), which creating it would erase.
func TestAcquireSharedDoesNotCreateAMissingDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "absent")
	if _, err := AcquireShared(dir); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("AcquireShared on a missing dir: err = %v, want fs.ErrNotExist", err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("AcquireShared created the missing dir (stat err %v)", err)
	}
}

// A lock file left on disk by a crashed run is inert: because exclusivity is an
// flock the kernel already released, the mere presence of the file must never
// block a new acquirer (this is what replaces stale-lock reclaim).
func TestLeftoverLockFileDoesNotBlock(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, Name), []byte("from a dead run\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	release, err := Acquire(dir)
	if err != nil {
		t.Fatalf("a leftover lock file must not block acquisition: %v", err)
	}
	release()
}
