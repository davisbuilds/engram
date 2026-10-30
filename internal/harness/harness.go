// Package harness reports configuration heuristics for harness memory support.
// These checks do not establish effective runtime settings or consumption.
package harness

import (
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// NativeMemory is the state of a harness's own memory feature, independent of
// engram's toggle.
type NativeMemory string

const (
	MemoryOn      NativeMemory = "on"
	MemoryOff     NativeMemory = "off"
	MemoryUnknown NativeMemory = "unknown"
)

// Report is a per-harness readiness assessment.
type Report struct {
	Harness        string       `json:"harness"`
	EngramEnabled  bool         `json:"engram_enabled"`
	HomeExists     bool         `json:"home_exists"`
	NativeMemory   NativeMemory `json:"native_memory"`
	Ready          bool         `json:"ready"`
	ReadinessBasis string       `json:"readiness_basis"`
	Warnings       []string     `json:"warnings,omitempty"`
}

// CheckClaude preserves the legacy readiness heuristic: engram's toggle and
// existence of the configured home. It does not inspect effective Claude settings.
func CheckClaude(home string, engramEnabled bool) Report {
	r := Report{
		Harness:        "claude-code",
		ReadinessBasis: "configuration_heuristic",
		EngramEnabled:  engramEnabled,
		HomeExists:     dirExists(home),
		NativeMemory:   MemoryUnknown,
	}
	if r.HomeExists {
		r.NativeMemory = MemoryOn
	}
	r.Warnings = commonWarnings(engramEnabled, home, r.HomeExists)
	r.Ready = engramEnabled && r.HomeExists
	return r
}

// CheckCodex preserves the legacy home/toggle/config-file readiness heuristic.
// The lightweight setting scan is not effective runtime configuration evidence.
func CheckCodex(home string, engramEnabled bool) Report {
	r := Report{
		Harness:        "codex",
		ReadinessBasis: "configuration_heuristic",
		EngramEnabled:  engramEnabled,
		HomeExists:     dirExists(home),
		NativeMemory:   codexMemories(home),
	}
	r.Warnings = commonWarnings(engramEnabled, home, r.HomeExists)
	switch r.NativeMemory {
	case MemoryOff:
		r.Warnings = append(r.Warnings,
			"Codex memory is off (set `memories = true` in "+filepath.Join(home, "config.toml")+
				"); consolidation may be disabled")
	case MemoryUnknown:
		r.Warnings = append(r.Warnings,
			"could not determine Codex `memories` setting in "+filepath.Join(home, "config.toml"))
	}
	if CodexMemoryStale(home) {
		r.Warnings = append(r.Warnings,
			"Codex MEMORY.md is older than 30 days; the consolidator may be stalled")
	}
	r.Ready = engramEnabled && r.HomeExists && r.NativeMemory != MemoryOff
	return r
}

// CodexMemoryStale reports whether Codex's consolidated MEMORY.md is older than
// 30 days (a sign the consolidator has not run recently). A missing file is not
// stale.
func CodexMemoryStale(home string) bool {
	fi, err := os.Stat(filepath.Join(home, "memories", "MEMORY.md"))
	if err != nil {
		return false
	}
	return time.Since(fi.ModTime()) > 30*24*time.Hour
}

func commonWarnings(engramEnabled bool, home string, homeExists bool) []string {
	var w []string
	if !engramEnabled {
		w = append(w, "engram is disabled for this harness in config")
	}
	if !homeExists {
		w = append(w, "harness home "+home+" does not exist; engram would create it, but the harness may not read from there")
	}
	return w
}

var codexMemoriesRe = regexp.MustCompile(`(?m)^\s*memories\s*=\s*(true|false)`)

func codexMemories(home string) NativeMemory {
	data, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil {
		return MemoryUnknown
	}
	m := codexMemoriesRe.FindStringSubmatch(string(data))
	if m == nil {
		return MemoryUnknown
	}
	if m[1] == "true" {
		return MemoryOn
	}
	return MemoryOff
}

func dirExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}
