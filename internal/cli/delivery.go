package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/davisbuilds/engram/internal/harness"
	"github.com/davisbuilds/engram/internal/render"
	syncer "github.com/davisbuilds/engram/internal/sync"
)

type evidenceState struct {
	State  string `json:"state"`
	Reason string `json:"reason"`
}

type deliveryEvidence struct {
	Readiness   harness.Report `json:"readiness"`
	LoadPaths   []loadPath     `json:"load_paths"`
	Consumption evidenceState  `json:"consumption"`
}

type loadPath struct {
	Kind        string         `json:"kind"`
	Artifacts   []loadArtifact `json:"artifacts"`
	Eligibility string         `json:"eligibility"`
	Reason      string         `json:"reason"`
}

type loadArtifact struct {
	Role  string `json:"role"`
	Path  string `json:"path"`
	State string `json:"state"`
}

func newDelivery(r harness.Report) deliveryEvidence {
	return deliveryEvidence{
		Readiness: r, LoadPaths: []loadPath{},
		Consumption: evidenceState{"unknown", "no_session_consumption_evidence"},
	}
}

// This observes prerequisites of engram's documented loading routes. Even byte
// agreement with our writer says nothing about effective settings, permissions,
// index truncation, harness versions, or an individual session's context.
func (h *harnessInventory) loadRoute(kind string, artifacts ...loadArtifact) {
	route := loadPath{Kind: kind, Artifacts: artifacts, Eligibility: "unknown", Reason: "runtime_loading_unverified"}
	missing, different, failed := false, false, false
	for _, a := range artifacts {
		switch a.State {
		case "missing", "empty":
			missing = true
		case "different":
			different = true
		case "error":
			failed = true
		}
	}
	switch {
	case failed:
		route.Reason = "artifact_inspection_failed"
	case missing:
		route.Eligibility, route.Reason = "prerequisites_missing", "loading_artifact_missing_or_empty"
	case different:
		route.Reason = "wiring_differs_from_engram"
	}
	h.Delivery.LoadPaths = append(h.Delivery.LoadPaths, route)
}

// expected is only supplied for wiring that engram itself generates. A custom
// file can still work; differing bytes are not evidence of ineligibility.
func (h *harnessInventory) loadFile(role, path string, expected []byte) loadArtifact {
	a := loadArtifact{role, path, "present"}
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		a.State = "missing"
	case err != nil:
		a.State = "error"
		h.issue(path, err.Error())
	case len(strings.TrimSpace(string(data))) == 0:
		a.State = "empty"
	case expected != nil && !bytes.Equal(data, expected):
		a.State = "different"
	}
	return a
}

func (h *harnessInventory) claudeSharedRoute(home string) {
	dir := filepath.Join(home, "engram", "memory")
	h.loadRoute("claude_shared",
		h.loadFile("index", filepath.Join(dir, "MEMORY.md"), nil),
		h.loadFile("rules_import", filepath.Join(home, "rules", "engram-memory.md"), syncer.SharedRulesContent(dir)))
}

func (h *harnessInventory) codexRoute(home string) {
	dir := codexExtDir(home)
	// The notes directory is already scanned by inventory; reuse that evidence
	// rather than creating a second observation with a potentially different view.
	for _, loc := range h.Locations {
		if loc.Kind == "codex_notes" {
			h.loadRoute("codex_extension",
				h.loadFile("instructions", filepath.Join(dir, "instructions.md"), []byte(render.CodexInstructions)),
				loadArtifact{"notes", loc.Path, loc.State})
			return
		}
	}
}
