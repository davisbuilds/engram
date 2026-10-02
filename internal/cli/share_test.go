package cli

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/davisbuilds/engram/internal/schema"
)

// shareFixture writes one imported canonical memory carrying a merge base and
// returns the config args and the memory's path.
func shareFixture(t *testing.T) ([]string, string) {
	t.Helper()
	dir := t.TempDir()
	canon := filepath.Join(dir, "canonical")
	memFile := filepath.Join(canon, "a-mem.md")
	writeFile(t, memFile, "---\nname: a-mem\ndescription: d\ntype: lesson\nscope: global\nprovenance:\n    origin: import:claude-code\n    source: a-mem.md\n    import_hash: sha256:abc\n    import_source: -work-x/a-mem.md\n---\nbody\n")
	cfg := filepath.Join(dir, "c.yaml")
	writeFile(t, cfg, "canonical_root: "+canon+"\n")
	return []string{"--config", cfg, "--json"}, memFile
}

func loadShared(t *testing.T, path string) *schema.CanonicalMemory {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	m, err := schema.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// TestShareAppliesCwd pins narrowing by cwd: the globs replace applies_to.cwd,
// a leading ~ expands to the home directory, and the scope tier and the import
// lineage (origin, merge base) survive untouched.
func TestShareAppliesCwd(t *testing.T) {
	c, memFile := shareFixture(t)
	defer silenceStdout(t)()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}

	if code := Run(append([]string{"share", "a-mem", "--applies-cwd", "/work/**", "--applies-cwd", "~/dev"}, c...)); code != exitOK {
		t.Fatalf("share exit = %d, want %d", code, exitOK)
	}
	m := loadShared(t, memFile)
	want := []string{"/work/**", filepath.Join(home, "dev")}
	if !slices.Equal(m.AppliesTo.Cwd, want) {
		t.Errorf("applies_to.cwd = %v, want %v", m.AppliesTo.Cwd, want)
	}
	if m.Scope != "global" {
		t.Errorf("scope = %q, want it unchanged (global)", m.Scope)
	}
	p := m.Provenance
	if p.Origin != "import:claude-code" || p.ImportHash != "sha256:abc" || p.ImportSource != "-work-x/a-mem.md" {
		t.Errorf("share must keep the import lineage, got %+v", p)
	}
}

// TestShareCleansCwdGlobs pins that share stores the clean spelling of a glob,
// since a session cwd is always cleaned before it is matched: "/work/" and
// "/work/../other/**" would otherwise never match anything.
func TestShareCleansCwdGlobs(t *testing.T) {
	c, memFile := shareFixture(t)
	defer silenceStdout(t)()
	if code := Run(append([]string{"share", "a-mem", "--applies-cwd", "/work/", "--applies-cwd", "/work/../other/**"}, c...)); code != exitOK {
		t.Fatalf("share exit = %d, want %d", code, exitOK)
	}
	if got, want := loadShared(t, memFile).AppliesTo.Cwd, []string{"/work", "/other/**"}; !slices.Equal(got, want) {
		t.Errorf("applies_to.cwd = %v, want %v", got, want)
	}
}

// TestShareAnyCwd pins the reverse: --any-cwd clears the cwd axis.
func TestShareAnyCwd(t *testing.T) {
	c, memFile := shareFixture(t)
	defer silenceStdout(t)()
	if code := Run(append([]string{"share", "a-mem", "--applies-cwd", "/work/**"}, c...)); code != exitOK {
		t.Fatalf("narrow exit = %d, want %d", code, exitOK)
	}
	if code := Run(append([]string{"share", "a-mem", "--any-cwd"}, c...)); code != exitOK {
		t.Fatalf("--any-cwd exit = %d, want %d", code, exitOK)
	}
	if got := loadShared(t, memFile).AppliesTo.Cwd; len(got) != 0 {
		t.Errorf("applies_to.cwd = %v, want it cleared", got)
	}
}

// TestShareUsage pins the argument rules: share needs something to change, the
// cwd flags exclude each other, and a relative glob (which could never match an
// absolute cwd, silently hiding the memory everywhere) is refused. None writes.
func TestShareUsage(t *testing.T) {
	cases := map[string][]string{
		"nothing to change": {"share", "a-mem"},
		"cwd and any-cwd":   {"share", "a-mem", "--applies-cwd", "/work/**", "--any-cwd"},
		"relative glob":     {"share", "a-mem", "--applies-cwd", "work/**"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			c, memFile := shareFixture(t)
			before, err := os.ReadFile(memFile)
			if err != nil {
				t.Fatal(err)
			}
			defer silenceStdout(t)()
			if code := Run(append(args, c...)); code != exitUsage {
				t.Errorf("exit = %d, want %d", code, exitUsage)
			}
			after, err := os.ReadFile(memFile)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(before) {
				t.Errorf("a refused share must not write:\n%s", after)
			}
		})
	}
}

// TestRememberAppliesCwdTilde pins that remember spells a ~ glob the same way
// share does, so the two authoring paths store one form.
func TestRememberAppliesCwdTilde(t *testing.T) {
	c, _ := shareFixture(t)
	defer silenceStdout(t)()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"remember", "--name", "b-mem", "--description", "d", "--type", "lesson", "--applies-cwd", "~/dev/**"}
	if code := Run(append(args, c...)); code != exitOK {
		t.Fatalf("remember exit = %d, want %d", code, exitOK)
	}
	canon := filepath.Dir(c[1])
	m := loadShared(t, filepath.Join(canon, "canonical", "b-mem.md"))
	if want := []string{filepath.Join(home, "dev") + "/**"}; !slices.Equal(m.AppliesTo.Cwd, want) {
		t.Errorf("applies_to.cwd = %v, want %v", m.AppliesTo.Cwd, want)
	}
}
