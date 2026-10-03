package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadDefaultsWhenNoFile(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.CanonicalRoot == "" {
		t.Error("default canonical root should be non-empty")
	}
	for _, h := range []string{HarnessClaude, HarnessCodex} {
		if !cfg.Harnesses[h].Enabled() {
			t.Errorf("%s should be enabled by default", h)
		}
		if cfg.Harnesses[h].Home == "" {
			t.Errorf("%s should have a default home", h)
		}
	}
}

func TestLoadOverridesAndMerges(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	body := "" +
		"canonical_root: /tmp/canon\n" +
		"hosts:\n  laptop-hostname: host-a\n" +
		"harnesses:\n  codex:\n    disabled: true\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.CanonicalRoot != "/tmp/canon" {
		t.Errorf("canonical_root = %q, want /tmp/canon", cfg.CanonicalRoot)
	}
	if cfg.Harnesses[HarnessCodex].Enabled() {
		t.Error("codex should be disabled per config")
	}
	// Once harnesses: is present, an unlisted harness is disabled rather than
	// enabled at the real default home; it keeps that home for reporting.
	if cfg.Harnesses[HarnessClaude].Enabled() {
		t.Error("claude is not listed under harnesses:, so it should be disabled")
	}
	if cfg.Harnesses[HarnessClaude].Home == "" {
		t.Error("claude home should keep its default for reporting")
	}
	if label, ok := cfg.HostLabel("laptop-hostname"); !ok || label != "host-a" {
		t.Errorf("HostLabel(laptop-hostname) = %q,%v; want host-a,true", label, ok)
	}
}

func TestHostLabelUnknownIsNotOK(t *testing.T) {
	cfg, _ := Load(filepath.Join(t.TempDir(), "none.yaml"))
	if _, ok := cfg.HostLabel("some-unmapped-host"); ok {
		t.Error("unmapped hostname should report ok=false (fail-closed)")
	}
}

func TestCurateModelDefaults(t *testing.T) {
	cfg, err := Load("") // no file: built-in defaults
	if err != nil {
		t.Fatal(err)
	}
	claude := cfg.CurateModel(HarnessClaude)
	if claude.Model != "claude-sonnet-5" || claude.Effort != "high" {
		t.Errorf("claude default = %+v, want claude-sonnet-5/high", claude)
	}
	codex := cfg.CurateModel(HarnessCodex)
	if codex.Model != "gpt-5.6-terra" || codex.Effort != "high" {
		t.Errorf("codex default = %+v, want gpt-5.6-terra/high", codex)
	}
}

func TestCurateModelPartialOverrideFallsBack(t *testing.T) {
	// A config that sets only the model must inherit the default effort.
	cfg := &Config{Curate: CurateConfig{Models: map[string]ModelChoice{
		HarnessClaude: {Model: "claude-opus-5"},
	}}}
	got := cfg.CurateModel(HarnessClaude)
	if got.Model != "claude-opus-5" || got.Effort != "high" {
		t.Errorf("partial override = %+v, want claude-opus-5/high", got)
	}
}

// A config naming one harness (e.g. a scratch config pointing claude-code at a
// temp dir) must not leave the other enabled at the user's real home: that is
// how a test once wrote into, and deleted, a real ~/.codex.
func TestUnlistedHarnessIsDisabled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("harnesses:\n  claude-code:\n    home: /tmp/scratch-claude\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Harnesses[HarnessClaude].Enabled() || cfg.Harnesses[HarnessClaude].Home != "/tmp/scratch-claude" {
		t.Errorf("listed claude-code = %+v; want enabled at /tmp/scratch-claude", cfg.Harnesses[HarnessClaude])
	}
	codex := cfg.Harnesses[HarnessCodex]
	if codex.Enabled() {
		t.Error("codex is not listed under harnesses:, so it must be disabled")
	}
	if !codex.Unlisted {
		t.Error("codex should be marked unlisted so its warning can say why it is off")
	}
}

// A harnesses: key that lists nothing is still a harnesses: section, so it
// disables every harness; only a config without the key keeps the defaults.
func TestEmptyHarnessesSectionDisablesEveryHarness(t *testing.T) {
	for _, body := range []string{"harnesses:\n", "harnesses: {}\n", "harnesses: ~\n"} {
		path := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(path, []byte("canonical_root: /tmp/scratch\n"+body), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("%q: Load: %v", body, err)
		}
		for _, h := range []string{HarnessClaude, HarnessCodex} {
			if got := cfg.Harnesses[h]; got.Enabled() || !got.Unlisted {
				t.Errorf("%q: %s = %+v; want disabled and unlisted", body, h, got)
			}
		}
	}

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("canonical_root: /tmp/scratch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, h := range []string{HarnessClaude, HarnessCodex} {
		if !cfg.Harnesses[h].Enabled() {
			t.Errorf("no harnesses: key: %s should keep its enabled default", h)
		}
	}
}

// A listed harness with no home still gets the default home, and listing a
// harness with no settings enables it.
func TestListedHarnessWithoutHomeUsesDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("harnesses:\n  claude-code: {}\n  codex: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, h := range []string{HarnessClaude, HarnessCodex} {
		if got := cfg.Harnesses[h]; !got.Enabled() || got.Home == "" || got.Unlisted {
			t.Errorf("%s = %+v; want enabled, listed, at its default home", h, got)
		}
	}
}

func TestCurateTimeout(t *testing.T) {
	cases := []struct {
		yaml    string
		want    time.Duration
		wantErr bool
	}{
		{"", DefaultCurateTimeout, false},
		{"curate:\n  timeout: 5m\n", 5 * time.Minute, false},
		{"curate:\n  timeout: \"0\"\n", 0, false},
		{"curate:\n  timeout: soon\n", 0, true},
		{"curate:\n  timeout: -1m\n", 0, true},
	}
	for _, c := range cases {
		path := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(path, []byte(c.yaml), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load(%q): %v", c.yaml, err)
		}
		got, err := cfg.CurateTimeout()
		if (err != nil) != c.wantErr || (!c.wantErr && got != c.want) {
			t.Errorf("CurateTimeout for %q = %v, %v; want %v, error %v", c.yaml, got, err, c.want, c.wantErr)
		}
	}
}

// TestSharedIndexDefaultsOnAndTurnsOff pins the claude-code shared_index key:
// absent means on, false turns it off.
func TestSharedIndexDefaultsOnAndTurnsOff(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("harnesses:\n  claude-code:\n    home: /tmp/c\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Harnesses[HarnessClaude].SharesIndex() {
		t.Error("the shared index should default to on")
	}
	if err := os.WriteFile(path, []byte("harnesses:\n  claude-code:\n    home: /tmp/c\n    shared_index: false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if cfg, err = Load(path); err != nil {
		t.Fatal(err)
	}
	if cfg.Harnesses[HarnessClaude].SharesIndex() {
		t.Error("shared_index: false should turn the shared index off")
	}
}
