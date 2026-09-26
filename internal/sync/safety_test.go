package sync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/davisbuilds/engram/internal/render"
	"github.com/davisbuilds/engram/internal/schema"
)

// caseInsensitive reports whether dir's filesystem folds case (macOS APFS by
// default), where Foo.md and foo.md are the same file.
func caseInsensitive(t *testing.T, dir string) bool {
	t.Helper()
	p := filepath.Join(dir, "CaseProbe")
	if err := os.WriteFile(p, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(p) }()
	_, err := os.Stat(filepath.Join(dir, "caseprobe"))
	return err == nil
}

// A hand-authored file whose name differs from a canonical memory only by case
// is, on a case-folding filesystem, the very file a CREATE would replace.
func TestApplyNeverOverwritesACaseVariantHandFile(t *testing.T) {
	dir := t.TempDir()
	hand := filepath.Join(dir, "User-Notes.md")
	const mine = "---\nname: User-Notes\n---\nmine, not engram's\n"
	if err := os.WriteFile(hand, []byte(mine), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := target(dir, mem("user-notes")).Apply()
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(hand)
	if err != nil {
		t.Fatalf("hand-authored file gone: %v", err)
	}
	if string(got) != mine {
		t.Fatalf("hand-authored file overwritten:\n%s", got)
	}
	if caseInsensitive(t, dir) && (len(res.Conflicts) != 1 || res.Conflicts[0].Name != "user-notes") {
		t.Errorf("want a CONFLICT for user-notes on a case-folding filesystem, got applied=%v conflicts=%v", res.Applied, res.Conflicts)
	}
}

// With an incomplete canonical view, an owned render that is not desired may
// still have a live canonical source, so it must survive.
func TestKeepStaleHoldsBackClaudeRemovals(t *testing.T) {
	dir := t.TempDir()
	orphan := mem("orphan")
	writeOwned(t, dir, orphan)

	tg := target(dir)
	tg.KeepStale = true
	acts, err := tg.Plan()
	if err != nil {
		t.Fatal(err)
	}
	if n := kinds(acts)[Stale]; n != 0 {
		t.Errorf("planned %d STALE removals with KeepStale", n)
	}
	if _, err := tg.Apply(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "orphan.md")); err != nil {
		t.Errorf("owned render removed despite KeepStale: %v", err)
	}
}

func TestKeepStaleHoldsBackCodexRemovals(t *testing.T) {
	dir := t.TempDir()
	if _, err := codexTarget(dir, mem("orphan")).Apply(); err != nil {
		t.Fatal(err)
	}
	tg := codexTarget(dir)
	tg.KeepStale = true
	if _, err := tg.Apply(); err != nil {
		t.Fatal(err)
	}
	if !codexNoteExistsFor(t, dir, "orphan") {
		t.Error("owned Codex note removed despite KeepStale")
	}
}

// Line endings are cosmetic; they must not flip an engram render to
// hand-authored and strand it in a permanent CONFLICT.
func TestCRLFRenderStaysEngramOwned(t *testing.T) {
	dir := t.TempDir()
	m := mem("crlf")
	writeOwned(t, dir, m)
	p := filepath.Join(dir, "crlf.md")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(strings.ReplaceAll(string(b), "\n", "\r\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	acts, err := target(dir, m).Plan()
	if err != nil {
		t.Fatal(err)
	}
	if n := kinds(acts)[Conflict]; n != 0 {
		t.Errorf("CRLF render planned as CONFLICT: %v", acts)
	}
	if _, err := target(dir, m).Apply(); err != nil {
		t.Fatal(err)
	}
	if acts, err := target(dir, m).Plan(); err != nil || len(acts) != 0 {
		t.Errorf("second plan after normalizing CRLF = %v, %v; want no actions", acts, err)
	}
}

// Adopting a hand file whose name differs from the canonical only by case must
// not delete the file it just wrote: on a case-folding filesystem the old and
// new paths are the same file.
func TestMigrateAdoptSurvivesCaseOnlyRename(t *testing.T) {
	dir := t.TempDir()
	m := memBody("feedback-x", "same body\n")
	writeHand(t, dir, "Feedback-X", "Feedback-X", "same body\n")

	if _, err := (ClaudeMigrateTarget{MemoryDir: dir, Desired: []*schema.CanonicalMemory{m}}).Apply(); err != nil {
		t.Fatal(err)
	}
	rr, err := render.ClaudeRenderer{}.Render(m)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, rr.FileName)); err != nil {
		t.Fatalf("adopted memory is gone after migrate: %v", err)
	}
}

// A different hand-authored file occupying the adoption's destination through a
// case variant must block the adoption. Any case variant also slug-matches the
// memory, so the two-files-one-memory ambiguity rule is what refuses it.
func TestMigrateRefusesCaseVariantOccupant(t *testing.T) {
	dir := t.TempDir()
	if !caseInsensitive(t, dir) {
		t.Skip("needs a case-folding filesystem")
	}
	m := memBody("feedback-x", "same body\n")
	writeHand(t, dir, "feedback_x", "feedback_x", "same body\n")
	writeHand(t, dir, "Feedback-X", "Feedback-X", "an unrelated note\n")

	if got := migrateActions(t, dir, m)[Adopt]; len(got) != 0 {
		t.Fatalf("adopted %v over a case-variant occupant", got)
	}
	b, err := os.ReadFile(filepath.Join(dir, "Feedback-X.md"))
	if err != nil || !strings.Contains(string(b), "an unrelated note") {
		t.Fatalf("occupant changed: %q, %v", b, err)
	}
}

// A hand-authored file that merely quotes engram's marker in its body is not
// engram's, for show as for sync.
func TestIsEngramOwnedIgnoresMarkerInBody(t *testing.T) {
	quoted := []byte("---\nname: mine\n---\nengram writes `origin: " + "engram-sync" + "` into files it owns\n")
	if IsEngramOwned(quoted) {
		t.Error("body quote treated as ownership")
	}
}
