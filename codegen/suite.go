// Package codegen pins the bytecode minipy emits. Each corpus case is a
// `<name>.py` source paired with a `<name>.masm` golden holding the program
// minipy compiles it to, rendered by Format.
//
// The corpus is file-based, deviating from the coding standard's default of
// keeping source and expected output visible beside the assertion (§11.2). The
// invariant that requires it: a golden is a whole program listing, tens to
// hundreds of lines long, and it is regenerated mechanically with `-update`
// rather than written by hand. Go string literals would make both the diff and
// the regeneration unreadable. Inline fixtures remain the default for all unit
// tests.
//
// A golden is only ever a record of what the compiler does, never of what it
// should do, so nothing here reads one as a specification. Every case is also
// executed and its stdout compared against `conformance/`-style behavior by
// the test that owns it, which is what stops a golden from being regenerated
// into a program that does not work.
package codegen

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Case describes one corpus program and the golden it is checked against.
type Case struct {
	Name     string // category/basename, e.g. "loops/while_sum"
	Category string // top-level testdata subdirectory, e.g. "loops"
	Path     string // path to the .py source
	Golden   string // path to the sibling .masm golden
	Expected string // the golden's contents, empty when it does not exist yet
}

// Load walks root for `*.py` corpus sources, pairs each with its golden, and
// returns cases sorted by Name for deterministic test order. It returns an
// error, rather than panicking, for corpus hygiene violations: an orphan golden
// with no source, or a source whose golden is unreadable for a reason other
// than not existing yet. A missing golden is not a violation — it is how a new
// case is added, and `-update` writes it.
func Load(root string) ([]Case, error) {
	sources, err := discoverSources(root)
	if err != nil {
		return nil, err
	}

	var cases []Case
	var problems []error

	stems := make(map[string]bool, len(sources))
	for _, path := range sources {
		stems[strings.TrimSuffix(path, ".py")] = true

		c, err := loadCase(root, path)
		if err != nil {
			problems = append(problems, err)
			continue
		}
		cases = append(cases, c)
	}

	if err := checkOrphanGoldens(root, stems); err != nil {
		problems = append(problems, err)
	}

	if len(problems) > 0 {
		return nil, errors.Join(problems...)
	}

	sort.Slice(cases, func(i, j int) bool { return cases[i].Name < cases[j].Name })
	return cases, nil
}

// discoverSources returns every `*.py` path under root.
func discoverSources(root string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && filepath.Ext(path) == ".py" {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("codegen: walk %s: %w", root, err)
	}
	return paths, nil
}

// loadCase reads one source's golden.
func loadCase(root, path string) (Case, error) {
	golden := strings.TrimSuffix(path, ".py") + ".masm"
	expected, err := os.ReadFile(golden)
	if err != nil && !os.IsNotExist(err) {
		return Case{}, fmt.Errorf("codegen: read %s: %w", golden, err)
	}

	name, category := caseIdentity(root, path)
	return Case{
		Name:     name,
		Category: category,
		Path:     path,
		Golden:   golden,
		Expected: string(expected),
	}, nil
}

// checkOrphanGoldens reports goldens under root whose `.py` source is gone, so
// a renamed or deleted case cannot leave a stale listing behind.
func checkOrphanGoldens(root string, stems map[string]bool) error {
	var problems []error
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".masm" {
			return nil
		}
		if !stems[strings.TrimSuffix(path, ".masm")] {
			problems = append(problems, fmt.Errorf("codegen: %s: golden has no sibling .py source", path))
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("codegen: walk %s: %w", root, err)
	}
	return errors.Join(problems...)
}

// caseIdentity derives a case's name and category from its path relative to
// root: the first path element is the category and the rest, without the
// extension, completes the name.
func caseIdentity(root, path string) (name, category string) {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		relative = path
	}
	relative = filepath.ToSlash(strings.TrimSuffix(relative, ".py"))
	category, _, _ = strings.Cut(relative, "/")
	return relative, category
}
