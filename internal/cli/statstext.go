package cli

import (
	"fmt"
	"strings"
)

func statsText(s *inventoryStats) string {
	var out strings.Builder
	fmt.Fprintf(&out, "\nStored text statistics: complete=%t files=%d bytes=%d words=%d word_unit=%s\n", s.Complete, s.Total.Files, s.Total.Bytes, s.Total.Words, s.WordUnit)
	for _, v := range s.CanonicalScopes {
		fmt.Fprintf(&out, "  Canonical scope %s: files=%d bytes=%d words=%d body_bytes=%d body_words=%d\n", v.Scope, v.Files, v.Bytes, v.Words, v.BodyBytes, v.BodyWords)
	}
	for _, v := range s.Locations {
		fmt.Fprintf(&out, "  Volume: %s %s %q scope_cwd=%q files=%d bytes=%d words=%d\n", v.Harness, v.Kind, v.Path, v.ScopeCwd, v.Files, v.Bytes, v.Words)
	}
	for _, v := range s.Sources {
		fmt.Fprintf(&out, "  Source: %s %s %q name=%q scope=%q bytes=%d words=%d modified_at=%s declared_created=%q declared_modified=%q\n", v.Harness, v.Kind, v.Path, v.Name, v.Scope, v.Bytes, v.Words, v.ModifiedAt, v.DeclaredCreated, v.DeclaredModified)
	}
	for _, v := range s.RepeatedRenders {
		fmt.Fprintf(&out, "  Repeated render name %s: files=%d bytes=%d words=%d paths=%q\n", v.Name, v.Files, v.Bytes, v.Words, v.Paths)
	}
	for _, v := range s.ExactDuplicates {
		fmt.Fprintf(&out, "  Exact duplicate renders: %s extra_copies=%d extra_bytes=%d extra_words=%d paths=%q\n", v.SHA256, v.ExtraCopies, v.ExtraBytes, v.ExtraWords, v.Paths)
	}
	fmt.Fprintf(&out, "  References: checked=%d unresolved=%d (supported forms only)\n", s.ReferencesChecked, len(s.UnresolvedReferences))
	for _, v := range s.UnresolvedReferences {
		fmt.Fprintf(&out, "    %s %s: %q -> %q\n", v.Kind, v.State, v.Source, v.Target)
	}
	for _, d := range s.Diagnostics {
		fmt.Fprintf(&out, "  Stats diagnostic: %q: %s\n", d.Path, d.Message)
	}
	return out.String()
}
