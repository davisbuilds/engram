package importer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/davisbuilds/engram/internal/schema"
)

func byName(mems []*schema.CanonicalMemory) map[string]*schema.CanonicalMemory {
	m := map[string]*schema.CanonicalMemory{}
	for _, x := range mems {
		m[x.Name] = x
	}
	return m
}

func TestImportClaudeMapsNativeFrontmatter(t *testing.T) {
	dir := t.TempDir()
	// A hand-authored native Claude memory (type under metadata, no origin).
	native := "---\nname: rg-gotcha\ndescription: never rg -r\nmetadata:\n  type: lesson\n---\nBody stays.\n"
	if err := os.WriteFile(filepath.Join(dir, "rg-gotcha.md"), []byte(native), 0o644); err != nil {
		t.Fatal(err)
	}
	// MEMORY.md index must be ignored.
	if err := os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte("- [x](x.md)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// cwd is a bare temp dir with no .git above it → global scope.
	res, err := ImportClaude(dir, dir)
	if err != nil {
		t.Fatalf("ImportClaude: %v", err)
	}
	if len(res.Memories) != 1 {
		t.Fatalf("got %d memories, want 1", len(res.Memories))
	}
	m := res.Memories[0]
	if m.Name != "rg-gotcha" || string(m.Type) != "lesson" || m.Description != "never rg -r" {
		t.Errorf("mapping wrong: %+v", m)
	}
	if m.Scope != "global" {
		t.Errorf("imported scope = %q, want global (no repo above cwd)", m.Scope)
	}
	if m.Body != "Body stays.\n" {
		t.Errorf("body not preserved: %q", m.Body)
	}
}

// Real Claude native names are free text — sentences and snake_case — but
// canonical requires kebab-case. Import must normalize them (as the Codex path
// already does) so the memory is valid, instead of passing the raw name through
// to fail at apply time. Names taken from the real corpus dogfood.
func TestImportClaudeNormalizesNamesToKebab(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"obsidian_vault.md": "---\nname: Obsidian vault location\ndescription: where the vault is\nmetadata:\n  type: reference\n---\nBody.\n",
		"feedback.md":       "---\nname: feedback_pr_review_monitor\ndescription: monitor endpoint\nmetadata:\n  type: feedback\n---\nBody.\n",
	}
	for fn, content := range files {
		if err := os.WriteFile(filepath.Join(dir, fn), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	res, err := ImportClaude(dir, dir)
	if err != nil {
		t.Fatal(err)
	}
	names := byName(res.Memories)
	for _, want := range []string{"obsidian-vault-location", "feedback-pr-review-monitor"} {
		m, ok := names[want]
		if !ok {
			t.Fatalf("expected normalized name %q; got %v", want, keys(names))
		}
		if err := m.Validate(); err != nil {
			t.Errorf("normalized memory %q must pass canonical validation: %v", want, err)
		}
	}
}

// A native file with no YAML frontmatter (rust-gotchas.md in the real corpus) was
// silently dropped. It must be recovered: the filename supplies the name, the
// first heading the description, and the whole file the body.
func TestImportClaudeRecoversFrontmatterlessFile(t *testing.T) {
	dir := t.TempDir()
	content := "# Rust Backend Gotchas\n\nAvoid the thing.\n"
	if err := os.WriteFile(filepath.Join(dir, "rust-gotchas.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := ImportClaude(dir, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Memories) != 1 {
		t.Fatalf("frontmatter-less file should be recovered; got %d memories, %d dropped", len(res.Memories), len(res.Dropped))
	}
	m := res.Memories[0]
	if m.Name != "rust-gotchas" {
		t.Errorf("recovered name = %q, want rust-gotchas (from filename)", m.Name)
	}
	if !strings.Contains(m.Body, "Avoid the thing.") {
		t.Errorf("recovered body must keep the file content: %q", m.Body)
	}
	if err := m.Validate(); err != nil {
		t.Errorf("recovered memory must be valid: %v", err)
	}
}

// A file that opens a frontmatter fence but has no parseable closing fence —
// truncated, or CRLF line endings frontmatterAndBody can't split — is malformed,
// not frontmatter-less. It must be reported in Dropped, never force-imported as a
// filename-named memory whose body is the raw document (which would also bypass
// the metadata.origin loop guard for CRLF engram output). Codex PR #3 P2.
func TestImportClaudeDropsMalformedFrontmatter(t *testing.T) {
	dir := t.TempDir()
	// Opening fence, no closing fence.
	if err := os.WriteFile(filepath.Join(dir, "truncated.md"),
		[]byte("---\nname: foo\ndescription: bar\nno closing fence here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// CRLF frontmatter: the fences are there but not in the LF form we split on.
	if err := os.WriteFile(filepath.Join(dir, "crlf.md"),
		[]byte("---\r\nname: baz\r\n---\r\nbody\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := ImportClaude(dir, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Memories) != 0 {
		t.Errorf("malformed frontmatter must not be imported; got %d memories: %+v", len(res.Memories), res.Memories)
	}
	if len(res.Dropped) != 2 {
		t.Fatalf("both malformed files must be reported in Dropped; got %+v", res.Dropped)
	}
}

// A file whose frontmatter fences are present but whose YAML does not parse must
// be reported in Dropped, never silently discarded.
func TestImportClaudeReportsUnparseableFrontmatter(t *testing.T) {
	dir := t.TempDir()
	bad := "---\nname: [unclosed\n---\nbody\n"
	if err := os.WriteFile(filepath.Join(dir, "broken.md"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := ImportClaude(dir, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Memories) != 0 {
		t.Errorf("a file with broken frontmatter must not be imported; got %d", len(res.Memories))
	}
	if len(res.Dropped) != 1 || res.Dropped[0].Source != "broken.md" {
		t.Fatalf("broken file must be reported in Dropped; got %+v", res.Dropped)
	}
}

// The no-silent-drops invariant: every candidate file (excluding the MEMORY.md
// index) is accounted for in exactly one of Memories, Skipped, or Dropped.
func TestImportClaudeAccountsForEveryFile(t *testing.T) {
	dir := t.TempDir()
	write := func(n, c string) {
		if err := os.WriteFile(filepath.Join(dir, n), []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("good.md", "---\nname: Good One\ndescription: d\nmetadata:\n  type: lesson\n---\nb\n")
	write("ours.md", "---\nname: ours\ndescription: d\nmetadata:\n  type: lesson\n  origin: engram-sync\n---\nx\n")
	write("no-frontmatter.md", "# Heading\n\ntext\n")
	write("broken.md", "---\nname: [unclosed\n---\nb\n")
	write("MEMORY.md", "- index\n") // ignored, not a candidate
	res, err := ImportClaude(dir, dir)
	if err != nil {
		t.Fatal(err)
	}
	if total := len(res.Memories) + len(res.Skipped) + len(res.Dropped); total != 4 {
		t.Errorf("every candidate accounted for once; memories=%d skipped=%d dropped=%d (sum %d, want 4)",
			len(res.Memories), len(res.Skipped), len(res.Dropped), total)
	}
	if len(res.Memories) != 2 || len(res.Skipped) != 1 || len(res.Dropped) != 1 {
		t.Errorf("split: memories=%d (good+recovered) skipped=%d (engram) dropped=%d (broken)",
			len(res.Memories), len(res.Skipped), len(res.Dropped))
	}
}

// The resolver must match real directories whose names contain '-' (which the
// slug can't distinguish from a path separator).
func TestResolveSlugMatchesHyphenatedComponents(t *testing.T) {
	base := t.TempDir()
	full := filepath.Join(base, "my-user", "code", "web-app-ios")
	if err := os.MkdirAll(full, 0o755); err != nil {
		t.Fatal(err)
	}
	// Slug for base/my-user/code/web-app-ios, relative to base: every separator and
	// every literal '-' becomes '-'.
	got := resolveSlugMatches(base, "-my-user-code-web-app-ios")
	if len(got) != 1 || got[0] != full {
		t.Errorf("resolveSlugMatches = %v, want [%q]", got, full)
	}
}

// P1: a component whose real name contains a non-hyphen non-alphanumeric char
// (underscore, dot, space) still encodes to '-' in the slug and must resolve —
// the resolver re-encodes real names rather than rebuilding only hyphenated ones.
func TestResolveSlugMatchesNonHyphenChars(t *testing.T) {
	base := t.TempDir()
	for _, real := range []string{"my_project", "app.v2", "two words"} {
		if err := os.MkdirAll(filepath.Join(base, real), 0o755); err != nil {
			t.Fatal(err)
		}
		// All three encode to the same shape "<sep>my-project" etc.
		enc := "-" + strings.Map(func(r rune) rune {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
				return r
			}
			return '-'
		}, real)
		got := resolveSlugMatches(base, enc)
		want := filepath.Join(base, real)
		if len(got) != 1 || got[0] != want {
			t.Errorf("resolveSlugMatches(%q) = %v, want [%q]", enc, got, want)
		}
	}
}

// P2: when two real paths encode to the same slug ("-a-b" ← both "/a-b" and
// "/a/b"), the resolver reports both, so pathFromClaudeSlug falls back to global
// rather than silently narrowing to one repo.
func TestResolveSlugAmbiguousYieldsNoUniquePath(t *testing.T) {
	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "foo-bar"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(base, "foo", "bar"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := resolveSlugMatches(base, "-foo-bar")
	if len(got) != 2 {
		t.Fatalf("both encodings should match; got %v", got)
	}
}

func TestResolveSlugNoMatchIsEmpty(t *testing.T) {
	base := t.TempDir()
	if got := resolveSlugMatches(base, "-nonexistent-project"); len(got) != 0 {
		t.Errorf("a slug with no matching directory must not resolve; got %v", got)
	}
}

// ImportClaudeAll sweeps every project slug and merges the results; a slug that
// resolves to a real repo carries its project scope, and MEMORY.md is ignored.
func TestImportClaudeAllSweepsAllSlugs(t *testing.T) {
	home := t.TempDir()
	// Two project slugs, each with one native memory. Scope resolution walks from
	// the real root and won't find these temp paths, so both resolve to global —
	// the sweep/merge behavior is what this pins (scope resolution is covered by
	// the resolveTokens tests).
	mk := func(slug, mem string) {
		d := filepath.Join(home, "projects", slug, "memory")
		writeMem(t, filepath.Join(d, mem+".md"),
			"---\nname: "+mem+"\ndescription: d\nmetadata:\n  type: lesson\n---\nbody\n")
		writeMem(t, filepath.Join(d, "MEMORY.md"), "- index\n")
	}
	mk("-work-alpha", "alpha-lesson")
	mk("-work-beta", "beta-lesson")

	res, err := ImportClaudeAll(home)
	if err != nil {
		t.Fatal(err)
	}
	names := byName(res.Memories)
	if len(res.Memories) != 2 || names["alpha-lesson"] == nil || names["beta-lesson"] == nil {
		t.Errorf("sweep should import one memory per slug; got %v", keys(names))
	}
}

func TestImportClaudeAllMissingProjectsDirIsEmpty(t *testing.T) {
	res, err := ImportClaudeAll(t.TempDir()) // no projects/ subdir
	if err != nil {
		t.Fatalf("a missing projects dir must not error: %v", err)
	}
	if len(res.Memories) != 0 {
		t.Errorf("expected empty result, got %d", len(res.Memories))
	}
}

func writeMem(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestImportClaudeSkipsEngramOrigin(t *testing.T) {
	dir := t.TempDir()
	ours := "---\nname: ours\ndescription: d\nmetadata:\n  type: lesson\n  origin: engram-sync\n---\nx\n"
	if err := os.WriteFile(filepath.Join(dir, "ours.md"), []byte(ours), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := ImportClaude(dir, dir)
	if err != nil {
		t.Fatalf("ImportClaude: %v", err)
	}
	if len(res.Memories) != 0 {
		t.Errorf("engram-origin file must not be imported; got %d", len(res.Memories))
	}
	if len(res.Skipped) != 1 {
		t.Errorf("expected 1 loop-guard skip, got %d", len(res.Skipped))
	}
}

func TestImportCodexSplitsTaskGroups(t *testing.T) {
	dir := t.TempDir()
	mem := "# Task Group: Dotfiles / Tokyo Night theme\n\nscope: apply a theme\n\n## Task 1\n\n" +
		"# Task Group: Supabase migration replay CI\n\ndetails here\n"
	path := filepath.Join(dir, "MEMORY.md")
	if err := os.WriteFile(path, []byte(mem), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := ImportCodex(path)
	if err != nil {
		t.Fatalf("ImportCodex: %v", err)
	}
	if len(res.Memories) != 2 {
		t.Fatalf("got %d memories, want 2 (one per Task Group)", len(res.Memories))
	}
	names := byName(res.Memories)
	if _, ok := names["dotfiles-tokyo-night-theme"]; !ok {
		t.Errorf("expected slugified name; got %v", keys(names))
	}
}

func TestImportCodexSkipsEngramGroups(t *testing.T) {
	dir := t.TempDir()
	// A consolidated Task Group carrying engram's own marker must be skipped.
	mem := "# Task Group: real user lesson\n\ndetails\n\n" +
		"# Task Group: engram note fold-in\n\n<!-- engram-sync canonical=x scope=global extension=engram -->\nfolded\n"
	path := filepath.Join(dir, "MEMORY.md")
	if err := os.WriteFile(path, []byte(mem), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := ImportCodex(path)
	if err != nil {
		t.Fatalf("ImportCodex: %v", err)
	}
	if len(res.Memories) != 1 {
		t.Errorf("engram-origin Task Group must be skipped; imported %d", len(res.Memories))
	}
	if len(res.Skipped) != 1 {
		t.Errorf("expected 1 loop-guard skip, got %d", len(res.Skipped))
	}
}

func TestImportCodexStaleWarning(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "MEMORY.md")
	if err := os.WriteFile(path, []byte("# Task Group: a lesson\n\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if res, _ := ImportCodex(path); res.StaleWarning {
		t.Error("a fresh MEMORY.md must not be flagged stale")
	}
	old := time.Now().Add(-40 * 24 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	res, err := ImportCodex(path)
	if err != nil {
		t.Fatal(err)
	}
	if !res.StaleWarning {
		t.Error("a 40-day-old MEMORY.md should set StaleWarning")
	}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Dotfiles / Tokyo Night theme": "dotfiles-tokyo-night-theme",
		"  Trailing/leading  ":         "trailing-leading",
		"Already-kebab-2":              "already-kebab-2",
	}
	for in, want := range cases {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func keys(m map[string]*schema.CanonicalMemory) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}

func TestDeriveClaudeScopeRepoRootBecomesProject(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	mdir := filepath.Join(repo, ".claude", "memory")
	if err := os.MkdirAll(mdir, 0o755); err != nil {
		t.Fatal(err)
	}
	native := "---\nname: repo-lesson\ndescription: d\nmetadata:\n  type: lesson\n---\nb\n"
	if err := os.WriteFile(filepath.Join(mdir, "repo-lesson.md"), []byte(native), 0o644); err != nil {
		t.Fatal(err)
	}
	// cwd is a subdirectory inside the repo: scope resolves to the repo's base.
	res, err := ImportClaude(mdir, filepath.Join(repo, "src", "pkg"))
	if err != nil {
		t.Fatal(err)
	}
	// The subdir does not exist on disk, but the walk finds .git at repo root.
	want := "project:" + filepath.Base(repo)
	if len(res.Memories) != 1 || res.Memories[0].Scope != want {
		t.Errorf("scope = %q, want %q", res.Memories[0].Scope, want)
	}
}

// mkRepo makes a temp dir named <name> containing a .git marker and returns its
// path, so scope derivation (which requires a real repo) has something to find.
func mkRepo(t *testing.T, name string) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	return repo
}

func TestDeriveCodexScopeFromRealRepo(t *testing.T) {
	repo := mkRepo(t, "dotfiles")
	got := deriveCodexScope("applies_to: cwd=" + repo + "; reuse_rule=x\nmore body")
	if got != "project:dotfiles" {
		t.Errorf("got %q, want project:dotfiles", got)
	}
	// Only the base name enters the scope; the full path never does.
	if strings.Contains(got, string(filepath.Separator)) {
		t.Errorf("scope %q leaked a path separator", got)
	}
}

func TestDeriveCodexScopeNonRepoPathStaysGlobal(t *testing.T) {
	// A container directory (no .git) must not become a project scope, and a
	// group with no applies_to line stays global.
	container := t.TempDir() // exists but has no .git
	if got := deriveCodexScope("applies_to: cwd=" + container + "\n"); got != "global" {
		t.Errorf("non-repo cwd: got %q, want global", got)
	}
	if got := deriveCodexScope("scope: apply a theme\n## Task 1\n"); got != "global" {
		t.Errorf("no applies_to: got %q, want global", got)
	}
}

func TestProjectScopeFromRepoGuardsDegenerate(t *testing.T) {
	// "." is not degenerate — it resolves to the real cwd (covered separately);
	// these are the paths that must always yield global.
	for _, p := range []string{"", "/", "  "} {
		if got := projectScopeFromRepo(p); got != "global" {
			t.Errorf("projectScopeFromRepo(%q) = %q, want global", p, got)
		}
	}
}

func TestImportCodexDerivesProjectScope(t *testing.T) {
	repo := mkRepo(t, "dotfiles")
	dir := t.TempDir()
	mem := "# Task Group: Dotfiles theme\n\n" +
		"applies_to: cwd=" + repo + "; reuse_rule=x\n\n## Task 1\nbody\n"
	path := filepath.Join(dir, "MEMORY.md")
	if err := os.WriteFile(path, []byte(mem), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := ImportCodex(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Memories) != 1 || res.Memories[0].Scope != "project:dotfiles" {
		t.Errorf("scope = %q, want project:dotfiles", res.Memories[0].Scope)
	}
}

func TestProjectScopeFromRepoResolvesRelativeCwd(t *testing.T) {
	repo := mkRepo(t, "myrepo")
	t.Chdir(repo) // run as if invoked from inside the repo
	// A relative "." must resolve to the absolute repo root, not "project:.".
	if got := projectScopeFromRepo("."); got != "project:myrepo" {
		t.Errorf("projectScopeFromRepo(\".\") = %q, want project:myrepo", got)
	}
}
