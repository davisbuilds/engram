package store

import (
	"os"
	"path/filepath"
	"testing"
)

// Discovery withholds a canonical file that fails to parse or validate, or that
// shares its name with another file. Such a file still exists, so a writer must
// refuse rather than read "not found" as "safe to create".
func TestSaveNeverOverwritesAWithheldCanonical(t *testing.T) {
	cases := map[string]map[string]string{
		"unparseable": {"a-mem.md": "---\nname: a-mem\n"},
		"invalid":     {"a-mem.md": "---\nname: a-mem\ndescription: d\ntype: lesson\nscope: \"project:\"\n---\nmine\n"},
		"duplicate": {
			"a-mem.md":     "---\nname: a-mem\ndescription: d\ntype: lesson\nscope: global\n---\none\n",
			"sub/other.md": "---\nname: a-mem\ndescription: d\ntype: lesson\nscope: global\n---\ntwo\n",
		},
	}
	for label, files := range cases {
		t.Run(label, func(t *testing.T) {
			root := t.TempDir()
			for rel, body := range files {
				p := filepath.Join(root, rel)
				if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			out, _, err := Save(root, newMem("d"), false)
			if err == nil && out != Conflict {
				t.Errorf("Save outcome = %q, want %q or an error", out, Conflict)
			}
			for rel, body := range files {
				got, rerr := os.ReadFile(filepath.Join(root, rel))
				if rerr != nil || string(got) != body {
					t.Errorf("%s changed: %q, %v", rel, got, rerr)
				}
			}
			if _, _, found, lerr := Load(root, "a-mem"); lerr == nil && !found {
				t.Error("Load reported a withheld canonical as absent, without error")
			}
		})
	}
}
