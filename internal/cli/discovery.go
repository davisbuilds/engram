package cli

import (
	"flag"
	"sort"
)

// Argument discovery describes accepted spelling/arity, not a complete grammar:
// required values, mutual exclusions and semantic validation remain command
// contracts. Empty slices are arrays rather than null for machine consumers.
type argumentInfo struct {
	PositionalsMax int      `json:"positionals_max"`
	ValueFlags     []string `json:"value_flags"`
	BooleanFlags   []string `json:"boolean_flags"`
}

type commandInfo struct {
	Name      string       `json:"name"`
	Summary   string       `json:"summary"`
	Arguments argumentInfo `json:"arguments"`
}

var globalArgs = argSpec{
	values: []string{"--config", "--cwd", "--agent", "--host"},
	bools:  []string{"--json", "--plain", "--quiet", "-q", "--apply", "--version", "--help", "-h"},
}

func argumentMetadata(spec argSpec) argumentInfo {
	a := argumentInfo{spec.positionals, append([]string{}, spec.values...), append([]string{}, spec.bools...)}
	sort.Strings(a.ValueFlags)
	sort.Strings(a.BooleanFlags)
	return a
}

func commandMetadata() []commandInfo {
	out := []commandInfo{}
	for _, c := range commands() {
		spec := c.args
		if spec == nil {
			// remember uses FlagSet; inspect its actual registrations. Do not
			// maintain an independent list that can drift from authoring.
			fs, _ := rememberFlags()
			spec = &argSpec{}
			fs.VisitAll(func(f *flag.Flag) {
				if b, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && b.IsBoolFlag() {
					spec.bools = append(spec.bools, "--"+f.Name)
				} else {
					spec.values = append(spec.values, "--"+f.Name)
				}
			})
		}
		out = append(out, commandInfo{c.name, c.summary, argumentMetadata(*spec)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Versions describe the named capability contract, independent of build tags
// and the unchanged response-envelope version. Absence means not advertised.
func capabilities() map[string]int {
	return map[string]int{
		"argument_discovery": 1, "memory_inventory": 1, "native_comparison": 1,
		"delivery_evidence": 1, "curate_saved_plans": 1, "curate_supplied_proposals": 1,
		"memory_statistics": 1,
	}
}
