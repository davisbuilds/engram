package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/davisbuilds/engram/internal/agentexec"
	"github.com/davisbuilds/engram/internal/config"
	"github.com/davisbuilds/engram/internal/curate"
	"github.com/davisbuilds/engram/internal/discover"
	"github.com/davisbuilds/engram/internal/schema"
)

// cmdCuratePlan checks a saved plan against the store as it is now and, under
// --apply, applies it. No agent runs. The plan is refused unless it parses
// strictly, names this store, finds every file as it was when the plan was
// made, and validates exactly as saved; curate.ApplyPlan checks the binding and
// validates again under the canonical lock, immediately before writing.
func cmdCuratePlan(e *env, name, path string) int {
	cfg, err := config.Load(e.config)
	if err != nil {
		e.emit(name, false, nil, nil, &RespError{Code: "config_load", Message: err.Error()}, nil)
		return exitError
	}
	root := cfg.CanonicalRoot
	raw, err := os.ReadFile(path)
	if err != nil {
		e.emit(name, false, nil, nil, &RespError{Code: "plan_read", Message: err.Error()}, nil)
		return exitError
	}
	data := map[string]any{"apply": e.apply, "plan": planInfo(path, raw)}
	p, err := curate.ParsePlan(raw)
	if err != nil {
		e.emit(name, false, data, nil, &RespError{Code: "invalid_plan", Message: err.Error()}, nil)
		return exitUsage
	}
	if p.Proposer != nil {
		data["proposer"] = p.Proposer
	}
	if err := p.Check(root); err != nil {
		var stale *curate.StaleError
		switch {
		case errors.As(err, &stale):
			return e.emitStale(name, data, nil, stale)
		case errors.Is(err, curate.ErrRootMismatch):
			e.emit(name, false, data, nil, &RespError{Code: "plan_root_mismatch", Message: err.Error()}, nil)
			return exitUsage
		}
		e.emit(name, false, data, nil, fingerprintError(err), nil)
		return exitError
	}
	mems, perrs, derr := discover.Discover(root)
	warns := warnParseErrors(perrs)
	if derr != nil {
		e.emit(name, false, data, warns, &RespError{Code: "discover", Message: derr.Error()}, nil)
		return exitError
	}
	if len(perrs) > 0 {
		e.emit(name, false, data, warns, corpusParseError(len(perrs)), nil)
		return exitError
	}
	results := curate.ValidatePlan(p.Operations, mems)
	data["operations"] = opResultItems(results)
	data["count"] = len(results)
	if !curate.AllValid(results) {
		e.emit(name, false, data, warns,
			&RespError{Code: "invalid_proposal", Message: "refusing the plan: it contains operations that do not validate against current canonical"},
			curateNextSteps(results))
		return exitConflicts
	}
	if !e.apply {
		var next []NextStep
		if len(results) > 0 {
			next = []NextStep{{
				Reason:  "the saved plan still applies to canonical as it is now",
				Command: "engram curate --plan " + agentexec.ShellQuote(path) + " --apply",
			}}
		}
		e.emit(name, true, data, warns, nil, next)
		return exitOK
	}
	if e.beforeApplyLock != nil {
		e.beforeApplyLock()
	}
	applied, aerr := curate.ApplyPlan(root, p)
	return e.emitApplied(name, data, warns, applied, aerr)
}

// savePlan writes the dry run's validated batch to out as a plan bound to
// binding, the store the agent was shown. A store that changed while the agent
// ran gets no plan: it would be stale on arrival.
func (e *env) savePlan(name, out, root string, binding curate.Binding, proposer *curate.Proposer,
	results []curate.OpResult, data map[string]any, warns []string,
) int {
	now, err := curate.Fingerprint(root)
	if err != nil {
		e.emit(name, false, data, warns, fingerprintError(err), nil)
		return exitError
	}
	if drift := binding.Drift(now); len(drift) > 0 {
		return e.emitStale(name, data, append(warns, "no plan was saved"), &curate.StaleError{Drift: drift})
	}
	ops := make([]curate.Operation, len(results))
	for i, r := range results {
		ops[i] = r.Op
	}
	p, err := curate.NewPlan(root, binding, proposer, ops)
	var raw []byte
	if err == nil {
		raw, err = p.Marshal()
	}
	if err != nil {
		e.emit(name, false, data, warns, &RespError{Code: "plan_write", Message: err.Error()}, nil)
		return exitError
	}
	// The path was checked before the agent ran; check it again, since it had
	// all that time to change.
	path, rerr := checkPlanOut(out, root)
	if rerr != nil {
		e.emit(name, false, data, warns, rerr, nil)
		return exitUsage
	}
	if err := writePlanFile(path, raw); err != nil {
		if errors.Is(err, fs.ErrExist) {
			e.emit(name, false, data, warns, planExistsError(out), nil)
			return exitUsage
		}
		e.emit(name, false, data, warns, &RespError{Code: "plan_write", Message: err.Error()}, nil)
		return exitError
	}
	data["plan"] = planInfo(out, raw)
	var next []NextStep
	if len(ops) > 0 {
		next = []NextStep{{
			Reason:  "review the saved plan (data.operations, or the file); applying it runs no agent and is refused if canonical changes first",
			Command: "engram curate --plan " + agentexec.ShellQuote(out) + " --apply",
		}}
	}
	e.emit(name, true, data, warns, nil, next)
	return exitOK
}

// emitStale reports a plan whose store changed since it was made (exit 3):
// data.drift lists each changed path, relative to the canonical root.
func (e *env) emitStale(name string, data map[string]any, warns []string, stale *curate.StaleError) int {
	data["drift"] = stale.Drift
	e.emit(name, false, data, warns, &RespError{Code: "stale_plan", Message: stale.Error()}, []NextStep{{
		Reason:  "canonical changed since the plan was made, so it no longer describes what applying it would do",
		Command: "engram curate --save-plan <new plan file>, review it, then engram curate --plan <new plan file> --apply",
	}})
	return exitConflicts
}

// checkPlanFlags rejects flag combinations that cannot mean what they say:
// --plan runs no agent, so the agent's flags would be silently ignored, and
// --save-plan is a dry run's output.
func checkPlanFlags(e *env, pa parsedArgs) *RespError {
	for _, f := range []string{"--plan", "--save-plan", "--proposal"} {
		if len(pa.vals[f]) > 1 {
			return usageError("%s given more than once", f)
		}
	}
	planIn, planOut := pa.last("--plan"), pa.last("--save-plan")
	if pa.last("--proposal") != "" {
		if planIn != "" || e.apply {
			return usageError("--proposal validates or saves a new plan; review and apply it separately with --plan <file> --apply")
		}
		for _, f := range []string{"--harness", "--model", "--effort", "--timeout"} {
			if len(pa.vals[f]) > 0 {
				return usageError("--proposal runs no agent, so %s does not apply", f)
			}
		}
	}
	switch {
	case planIn != "" && planOut != "":
		return usageError("--plan applies a saved plan and --save-plan saves a new one; give one of them")
	case planOut != "" && e.apply:
		return usageError("--save-plan saves a dry run's plan for review; apply it afterwards with --plan <file> --apply")
	case planIn != "":
		for _, f := range []string{"--harness", "--model", "--effort", "--timeout"} {
			if len(pa.vals[f]) > 0 {
				return usageError("--plan applies a saved plan and runs no agent, so %s does not apply", f)
			}
		}
	}
	return nil
}

// checkPlanOut refuses a --save-plan path that already exists (a plan never
// replaces a file), whose directory is missing, or that is the canonical root
// or lies inside it (the plan would change the store it is bound to). It
// returns the path to write: out with its directory resolved, so the plan is
// created where it was checked even when out climbs out of a symlink with "..",
// which the kernel resolves from the link's target, not lexically.
func checkPlanOut(out, root string) (string, *RespError) {
	bad := func(format string, a ...any) *RespError {
		return &RespError{Code: "plan_path", Message: fmt.Sprintf(format, a...)}
	}
	dir, base := splitPath(out)
	if base == "" || base == "." || base == ".." {
		return "", bad("%s does not name a file", out)
	}
	real, err := filepath.EvalSymlinks(dir)
	if err == nil {
		real, err = filepath.Abs(real)
	}
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", bad("the directory for %s does not exist", out)
		}
		return "", bad("%s: %v", out, err)
	}
	if info, err := os.Stat(real); err != nil || !info.IsDir() {
		return "", bad("the directory for %s does not exist", out)
	}
	path := filepath.Join(real, base)
	if _, err := os.Lstat(path); err == nil {
		return "", planExistsError(out)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", bad("%s: %v", out, err)
	}
	inside, err := insideRoot(path, root)
	if err != nil {
		return "", bad("cannot tell whether %s is inside the canonical root: %v", out, err)
	}
	if inside {
		return "", bad("%s is the canonical root or inside it; a plan there would change the store it is bound to", out)
	}
	return path, nil
}

// splitPath splits p after its final separator without cleaning it, since
// filepath.Dir resolves ".." lexically. A bare name's directory is ".".
func splitPath(p string) (dir, base string) {
	dir, base = filepath.Split(p)
	if dir == "" {
		dir = "."
	}
	return dir, base
}

func planExistsError(out string) *RespError {
	return &RespError{Code: "plan_exists", Message: out + " already exists; a plan never replaces a file"}
}

// corpusCapture is what curate shows its agent: the corpus and, for
// --save-plan, the binding of the store it was read from.
type corpusCapture struct {
	mems        []*schema.CanonicalMemory
	binding     curate.Binding
	parseErrors int
}

// captureCorpus reads the corpus curate shows its agent and, with bind, the
// binding a saved plan carries. Under --apply or --save-plan the whole read
// holds the canonical lock shared, so no engram writer changes the store
// between the binding and the corpus (or changes it and changes it back); a
// writer holding the lock, or a store whose tombstone directory is a symlink
// (which apply would refuse), refuses the capture before any agent runs. The lock
// is released before it returns, so the agent runs with no lock held, and
// apply checks the store again under the exclusive lock before it writes. A
// root the lock found missing is captured as the empty store it was, not read:
// nothing is locked, so a root created since would be read unlocked. A plain
// dry run takes no lock, since it writes nothing.
func (e *env) captureCorpus(root string, bind bool) (corpusCapture, []string, *RespError) {
	var c corpusCapture
	if e.apply || bind {
		release, missing, rerr := canonReadLock(root)
		if rerr != nil {
			return c, nil, rerr
		}
		defer release()
		e.at(hookCaptureLock)
		if missing {
			c.binding = curate.Binding{Files: map[string]string{}}
			return c, nil, nil
		}
		if err := curate.CheckHistory(root); err != nil {
			return c, nil, fingerprintError(err)
		}
	}
	if bind {
		var err error
		if c.binding, err = curate.Fingerprint(root); err != nil {
			return c, nil, fingerprintError(err)
		}
	}
	e.at(hookFingerprint)
	mems, perrs, err := discover.Discover(root)
	warns := warnParseErrors(perrs)
	if err != nil {
		return c, warns, &RespError{Code: "discover", Message: err.Error()}
	}
	c.mems = mems
	c.parseErrors = len(perrs)
	e.at(hookSnapshot)
	if bind {
		if rerr := checkPlannable(root, c.binding, perrs); rerr != nil {
			return c, warns, rerr
		}
	}
	return c, warns, nil
}

// checkPlannable refuses to plan over a store that does not fully parse (apply
// would refuse it) or that changed while it was read (the corpus the agent is
// shown would not be the store the plan is bound to). The capture's shared
// lock keeps engram's writers out, so such a change is an edit made outside
// engram; one that leaves the bytes as they were cannot be told apart.
func checkPlannable(root string, binding curate.Binding, perrs []discover.ParseError) *RespError {
	if len(perrs) > 0 {
		return corpusParseError(len(perrs))
	}
	now, err := curate.Fingerprint(root)
	if err != nil {
		return fingerprintError(err)
	}
	if drift := binding.Drift(now); len(drift) > 0 {
		return &RespError{Code: "corpus_changed", Message: fmt.Sprintf("canonical changed while it was read (%d path(s)); retry", len(drift))}
	}
	return nil
}

// fingerprintError reports a store curate could not read or bind: a symlinked
// tombstone directory, which curate refuses, as linked_history, and any other
// failure as fingerprint.
func fingerprintError(err error) *RespError {
	if errors.Is(err, curate.ErrLinkedHistory) {
		return &RespError{Code: "linked_history", Message: err.Error()}
	}
	return &RespError{Code: "fingerprint", Message: err.Error()}
}

func corpusParseError(n int) *RespError {
	return &RespError{Code: "corpus_parse", Message: fmt.Sprintf("refusing: %d canonical file(s) do not parse; fix them before curating", n)}
}

// insideRoot reports whether path, a name in a resolved directory, is the
// canonical root or lies under it. It compares file identity, not spelling, so
// a symlink or a case variant of the root is caught: path's directory is walked
// up to the filesystem root. A root that does not exist yet has nothing under
// it, but path may be where it would be created (see wouldBeRoot).
func insideRoot(path, root string) (bool, error) {
	rootInfo, err := os.Stat(root)
	if errors.Is(err, fs.ErrNotExist) {
		return wouldBeRoot(path, root)
	}
	if err != nil {
		return false, err
	}
	for d := filepath.Dir(path); ; {
		if info, err := os.Stat(d); err == nil && os.SameFile(info, rootInfo) {
			return true, nil
		}
		parent := filepath.Dir(d)
		if parent == d {
			return false, nil
		}
		d = parent
	}
}

// wouldBeRoot reports whether creating path would create the absent canonical
// root: the root itself or, if the root is a dangling symlink, any target along
// its chain. Each candidate's directory is matched by identity against path's,
// so its spelling (relative, "..", symlinked parents) does not matter, and the
// final names are compared without regard to case, which fails closed on a
// case-insensitive filesystem.
func wouldBeRoot(path, root string) (bool, error) {
	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		return false, err
	}
	entry := root
	for range 40 {
		for len(entry) > 1 && os.IsPathSeparator(entry[len(entry)-1]) {
			entry = entry[:len(entry)-1]
		}
		dir, base := splitPath(entry)
		if strings.EqualFold(base, filepath.Base(path)) {
			info, err := os.Stat(dir)
			switch {
			case err == nil && os.SameFile(info, dirInfo):
				return true, nil
			case err != nil && !errors.Is(err, fs.ErrNotExist):
				return false, err
			}
		}
		info, err := os.Lstat(entry)
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if info.Mode()&fs.ModeSymlink == 0 {
			return false, nil
		}
		target, err := os.Readlink(entry)
		if err != nil {
			return false, err
		}
		if !filepath.IsAbs(target) && dir != "." {
			target = dir + target
		}
		entry = target
	}
	return false, errors.New("too many levels of symbolic links")
}

// writePlanFile writes data to path, which must not exist. A temporary file in
// the same directory is linked into place, so no reader sees a partial plan and
// a file created at path meanwhile is never replaced (the link fails with
// fs.ErrExist). The plan keeps the temporary file's owner-only mode, since it
// carries memory bodies.
func writePlanFile(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".engram-plan-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Link(name, path)
}

// planInfo identifies a plan file in output: its path and the digest of its
// bytes, so the plan applied can be matched to the plan reviewed.
func planInfo(path string, raw []byte) map[string]any {
	sum := sha256.Sum256(raw)
	return map[string]any{"path": path, "sha256": "sha256:" + hex.EncodeToString(sum[:])}
}
