package cli

import "github.com/davisbuilds/engram/internal/store"

// Import result rows share one outcome vocabulary across every source (a
// native harness, Claude's shared renders) and every surface (import dry-run
// and --apply, reconcile): store.Save's own outcomes, plus the gates a
// candidate meets before any save is planned. The gates apply in one order:
// a tombstone first (forgotten), then a canonical file that claims the name
// but is withheld (a conflict carrying `withheld`), then validation of the
// candidate as scope resolution leaves it (invalid). A shared edit is judged
// against a live canonical memory when there is one; only without one do the
// gates apply (sharedEditPlan.absentState), adding an incomplete read of
// canonical, which proves no removal.
const (
	// outcomeForgotten: a tombstone holds the name; nothing is written, and
	// --force or --refresh never override it.
	outcomeForgotten store.Outcome = "forgotten"
	// outcomeInvalid: the candidate fails validation; nothing is written.
	outcomeInvalid store.Outcome = "invalid"
	// outcomeKept: shared edits only, --keep discarded the edit for canonical.
	outcomeKept store.Outcome = "kept"
)

// invalidRow is the result row for a candidate that fails validation.
func invalidRow(name string, err error) map[string]string {
	return map[string]string{"name": name, "outcome": string(outcomeInvalid), "error": err.Error()}
}
