// Package conformance discovers and describes the file-based corpus under
// testdata/conformance that pins minipy's observed behavior against CPython
// 3.13.
//
// Each corpus case is a `<name>.py` source file paired with a `<name>.expected`
// golden holding CPython's captured stdout. A case whose behavior legitimately
// diverges from CPython additionally carries a `<name>.minipy` golden holding
// minipy's captured stdout, and its source declares why through header
// directives:
//
//	# minipy-divergence: <reason>
//	# minipy-divergence-doc: <path relative to the repository root>
//
// The corpus is file-based, rather than Go string literals beside the
// assertion, because every `.expected` golden must be external evidence
// produced by running a real CPython 3.13 process (see cpython_test.go),
// not a restatement of minipy's own behavior. Keeping the CPython-derived
// source and its golden as sibling files also lets `-update` regenerate
// goldens without touching Go source.
package conformance

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Case describes one corpus program and the goldens it is checked against.
type Case struct {
	Name             string // category/basename, e.g. "lang/arithmetic"
	Category         string // top-level testdata subdirectory, e.g. "lang"
	Path             string // path to the .py source
	Expected         string // CPython golden contents (sibling .expected)
	MinipyExpected   string // minipy golden contents (sibling .minipy); empty when not Divergent
	Divergent        bool   // true when the source carries a minipy-divergence directive
	DivergenceReason string // text after "# minipy-divergence:"
	DivergenceDoc    string // text after "# minipy-divergence-doc:"
}

// repoRoot is the relative path from the conformance package directory (the
// process working directory while its tests run) to the repository root. It
// resolves "# minipy-divergence-doc:" targets, which are written relative to
// the repository root for readability in source headers.
const repoRoot = ".."

const (
	divergenceReasonDirective = "minipy-divergence:"
	divergenceDocDirective    = "minipy-divergence-doc:"
)

// Load walks root for `*.py` corpus sources, pairs each with its goldens and
// header directives, and returns cases sorted by Name for deterministic test
// order. It returns an error, rather than panicking, for corpus hygiene
// violations: a source missing its `.expected` golden, an orphan golden with
// no source, a `.minipy` golden whose source lacks a divergence directive (or
// vice versa), a divergence directive without a doc pointer, or a doc pointer
// that resolves to a file that does not exist.
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
		return nil, fmt.Errorf("conformance: walk %s: %w", root, err)
	}
	return paths, nil
}

// loadCase reads one source and its goldens and validates the divergence
// contract between them.
func loadCase(root, path string) (Case, error) {
	source, err := os.ReadFile(path)
	if err != nil {
		return Case{}, fmt.Errorf("conformance: read %s: %w", path, err)
	}
	reason, doc := parseDivergenceDirectives(string(source))

	expected, err := os.ReadFile(strings.TrimSuffix(path, ".py") + ".expected")
	if err != nil {
		return Case{}, fmt.Errorf("conformance: %s: missing sibling .expected golden", path)
	}

	minipyExpected, minipyErr := os.ReadFile(strings.TrimSuffix(path, ".py") + ".minipy")
	divergent := minipyErr == nil

	if err := validateDivergenceContract(path, reason, doc, divergent); err != nil {
		return Case{}, err
	}

	name, category := caseIdentity(root, path)
	return Case{
		Name:             name,
		Category:         category,
		Path:             path,
		Expected:         string(expected),
		MinipyExpected:   string(minipyExpected),
		Divergent:        divergent,
		DivergenceReason: reason,
		DivergenceDoc:    doc,
	}, nil
}

// parseDivergenceDirectives reads the leading run of `#` comment lines at the
// top of source and extracts the divergence reason and doc pointer, if
// present. Scanning stops at the first line that is not a comment.
func parseDivergenceDirectives(source string) (reason, doc string) {
	for _, line := range strings.Split(source, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "#") {
			break
		}
		body := strings.TrimSpace(strings.TrimPrefix(trimmed, "#"))
		switch {
		case strings.HasPrefix(body, divergenceDocDirective):
			doc = strings.TrimSpace(strings.TrimPrefix(body, divergenceDocDirective))
		case strings.HasPrefix(body, divergenceReasonDirective):
			reason = strings.TrimSpace(strings.TrimPrefix(body, divergenceReasonDirective))
		}
	}
	return reason, doc
}

// validateDivergenceContract enforces the biconditional between a
// minipy-divergence directive and a sibling .minipy golden, and requires a
// resolvable doc pointer whenever a directive is present.
func validateDivergenceContract(path, reason, doc string, hasMinipyGolden bool) error {
	hasDirective := reason != ""
	if hasDirective != hasMinipyGolden {
		if hasDirective {
			return fmt.Errorf("conformance: %s: has a minipy-divergence directive but no sibling .minipy golden", path)
		}
		return fmt.Errorf("conformance: %s: has a .minipy golden but no minipy-divergence directive", path)
	}
	if !hasDirective {
		return nil
	}
	if doc == "" {
		return fmt.Errorf("conformance: %s: has a minipy-divergence directive but no minipy-divergence-doc", path)
	}
	return validateDivergenceDoc(path, doc)
}

// validateDivergenceDoc confirms doc, resolved against the repository root
// with any "#anchor" fragment stripped, names a file that exists.
func validateDivergenceDoc(path, doc string) error {
	docFile, _, _ := strings.Cut(doc, "#")
	resolved := filepath.Join(repoRoot, docFile)
	if _, err := os.Stat(resolved); err != nil {
		return fmt.Errorf("conformance: %s: minipy-divergence-doc %q does not exist (resolved %s)", path, doc, resolved)
	}
	return nil
}

// checkOrphanGoldens reports every `.expected` or `.minipy` file under root
// whose stem is not among the known source stems.
func checkOrphanGoldens(root string, stems map[string]bool) error {
	var orphans []error
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		ext := filepath.Ext(path)
		if ext != ".expected" && ext != ".minipy" {
			return nil
		}
		if stem := strings.TrimSuffix(path, ext); !stems[stem] {
			orphans = append(orphans, fmt.Errorf("conformance: %s: orphan golden has no sibling .py source", path))
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("conformance: walk %s: %w", root, err)
	}
	return errors.Join(orphans...)
}

// caseIdentity derives a case's Name (category/basename, slash-separated) and
// Category (its top-level testdata subdirectory) from its path under root.
func caseIdentity(root, path string) (name, category string) {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		rel = path
	}
	rel = filepath.ToSlash(rel)
	name = strings.TrimSuffix(rel, ".py")
	category = strings.SplitN(rel, "/", 2)[0]
	return name, category
}
