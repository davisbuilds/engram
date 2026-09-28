package cli

import (
	"fmt"
	"slices"
	"strings"
)

// argSpec declares the arguments one command accepts after the global flags are
// removed: at most positionals bare arguments (anyPositionals for no limit), the flags in values (each takes a
// value, as `--flag v` or `--flag=v`, and may repeat), and the boolean flags in
// bools. A command whose spec is nil parses its own arguments.
// anyPositionals is the argSpec.positionals value for a command that takes any
// number of bare arguments (a list of memory names).
const anyPositionals = -1

type argSpec struct {
	positionals int
	values      []string
	bools       []string
}

// parsedArgs is the result of parseArgs: positionals in order, every value given
// for each value flag, and the boolean flags that were present.
type parsedArgs struct {
	pos   []string
	vals  map[string][]string
	bools map[string]bool
}

// last returns the final value given for a value flag, or "" when absent.
func (p parsedArgs) last(flag string) string {
	v := p.vals[flag]
	if len(v) == 0 {
		return ""
	}
	return v[len(v)-1]
}

// parseArgs checks args against spec. An unknown flag, a value flag with no
// value, or a positional beyond the spec's limit is a usage error: a typo must
// never be ignored, since the command would then run as if it were absent.
func parseArgs(args []string, spec argSpec) (parsedArgs, *RespError) {
	p := parsedArgs{vals: map[string][]string{}, bools: map[string]bool{}}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			if spec.positionals != anyPositionals && len(p.pos) == spec.positionals {
				return p, usageError("unexpected argument %q", a)
			}
			p.pos = append(p.pos, a)
			continue
		}
		name, _, _ := strings.Cut(a, "=")
		switch {
		case slices.Contains(spec.values, name):
			v, next, rerr := requireValue(args, i)
			if rerr != nil {
				return p, rerr
			}
			p.vals[name] = append(p.vals[name], v)
			i = next
		case slices.Contains(spec.bools, a):
			p.bools[a] = true
		default:
			return p, usageError("unknown flag %q", a)
		}
	}
	return p, nil
}

// requireValue returns the value of the value flag at args[i], given as
// `--flag=v` or as the next argument, and the index of the last argument it
// consumed. A missing or empty value, or a next argument that is itself a flag,
// is a usage error rather than an empty value the command would then act on.
func requireValue(args []string, i int) (string, int, *RespError) {
	name, v, inline := strings.Cut(args[i], "=")
	if !inline {
		if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			v, i = args[i+1], i+1
		}
	}
	if v == "" {
		return "", i, usageError("flag %s needs a value", name)
	}
	return v, i, nil
}

func usageError(format string, a ...any) *RespError {
	return &RespError{Code: "usage", Message: fmt.Sprintf(format, a...)}
}
