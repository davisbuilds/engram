// Package discover walks a canonical root and parses every memory file, keeping
// per-file parse failures separate so one malformed file never hides the rest.
package discover

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/davisbuilds/engram/internal/schema"
)

// ParseError records a single file that could not be read or parsed.
type ParseError struct {
	Path string
	Err  error
}

// Discover recursively parses every *.md under root into a CanonicalMemory. It
// returns the parsed memories and a separate slice of per-file parse errors; a
// malformed file is reported, not fatal. A missing root yields empty results.
func Discover(root string) ([]*schema.CanonicalMemory, []ParseError, error) {
	var (
		mems  []*schema.CanonicalMemory
		perrs []ParseError
	)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			perrs = append(perrs, ParseError{Path: path, Err: rerr})
			return nil
		}
		m, perr := schema.Parse(data)
		if perr != nil {
			perrs = append(perrs, ParseError{Path: path, Err: perr})
			return nil
		}
		mems = append(mems, m)
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("walk %s: %w", root, err)
	}
	return mems, perrs, nil
}
