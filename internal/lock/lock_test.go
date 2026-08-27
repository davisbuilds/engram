package lock

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAcquireIsExclusive(t *testing.T) {
	dir := t.TempDir()
	release, err := Acquire(dir, DefaultStaleAfter)
	if err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}
	if _, err := Acquire(dir, DefaultStaleAfter); err == nil {
		t.Error("second acquire should fail while the lock is held")
	}
	release()
	release2, err := Acquire(dir, DefaultStaleAfter)
	if err != nil {
		t.Fatalf("acquire after release failed: %v", err)
	}
	release2()
}

func TestAcquireReclaimsStaleLock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, Name)
	if err := os.WriteFile(path, []byte("99999\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Backdate the lock well past the staleness threshold.
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	release, err := Acquire(dir, 10*time.Minute)
	if err != nil {
		t.Fatalf("a stale lock should be reclaimable: %v", err)
	}
	release()
}

func TestAcquireDoesNotReclaimFreshLock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, Name)
	if err := os.WriteFile(path, []byte("held\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A fresh lock (mtime = now) must be respected, not stolen.
	if _, err := Acquire(dir, 10*time.Minute); err == nil {
		t.Error("a fresh lock must not be reclaimed")
	}
}

func TestAcquireReclaimDisabled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, Name)
	if err := os.WriteFile(path, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	_ = os.Chtimes(path, old, old)
	// staleAfter <= 0 disables reclaim even for an ancient lock.
	if _, err := Acquire(dir, 0); err == nil {
		t.Error("reclaim disabled: even a stale lock must block")
	}
}

func TestReclaimStaleOnlyRemovesUnchangedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, Name)
	if err := os.WriteFile(path, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// A different observed mtime means the file was replaced by a racing
	// acquirer; reclaim must NOT delete that replacement.
	reclaimStale(path, info.ModTime().Add(-time.Hour))
	if _, err := os.Stat(path); err != nil {
		t.Error("reclaimStale deleted a file whose mtime did not match the observed stale one")
	}
	// The exact observed file is removed.
	reclaimStale(path, info.ModTime())
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("reclaimStale should have removed the observed stale file")
	}
}
