package compiler

import (
	"io/fs"
	"path"
	"strings"
)

// distribution is an installed package's metadata parsed from its
// "<name>-<version>.dist-info" directory (pip / wheel layout). A distribution
// may expose one or more top-level import names distinct from its own name
// (e.g. the "Pillow" distribution provides the "PIL" import package).
type distribution struct {
	name    string
	version string
	imports []string
}

// distIndex maps top-level import names to their installed distributions across
// the search roots (a site-packages analog).
type distIndex struct {
	all      []*distribution
	byImport map[string]*distribution
}

// newDistIndex scans each search root for "*.dist-info" directories and records
// the distributions they describe.
func newDistIndex(paths []searchEntry) *distIndex {
	idx := &distIndex{byImport: map[string]*distribution{}}
	for _, entry := range paths {
		dir := cleanDir(entry.dir)
		items, err := fs.ReadDir(entry.fsys, dir)
		if err != nil {
			continue
		}
		for _, it := range items {
			if !it.IsDir() || !strings.HasSuffix(it.Name(), ".dist-info") {
				continue
			}
			idx.add(entry.fsys, path.Join(dir, it.Name()))
		}
	}
	return idx
}

// add records a distribution from one dist-info directory. A dist-info directory
// must contain at least METADATA and RECORD (per the wheel spec); top_level.txt
// lists the import names it provides.
func (idx *distIndex) add(fsys fs.FS, dir string) {
	if !readable(fsys, path.Join(dir, "RECORD")) {
		return
	}
	meta, err := fs.ReadFile(fsys, path.Join(dir, "METADATA"))
	if err != nil {
		return
	}
	name, version := parseMetadata(string(meta))
	if name == "" {
		return
	}
	d := &distribution{name: name, version: version}
	if tl, err := fs.ReadFile(fsys, path.Join(dir, "top_level.txt")); err == nil {
		d.imports = strings.Fields(string(tl))
	}
	if len(d.imports) == 0 {
		d.imports = []string{importNameFromDist(name)}
	}
	idx.all = append(idx.all, d)
	for _, imp := range d.imports {
		if _, seen := idx.byImport[imp]; !seen {
			idx.byImport[imp] = d
		}
	}
}

// distribution returns the installed distribution providing a top-level import
// name, honoring distribution-name-vs-import-name differences.
func (idx *distIndex) distribution(importName string) (*distribution, bool) {
	d, ok := idx.byImport[importName]
	return d, ok
}

// parseMetadata reads the Name and Version headers from a core-metadata METADATA
// file (RFC 822-style headers terminated by a blank line).
func parseMetadata(content string) (name, version string) {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			break
		}
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "name":
			name = strings.TrimSpace(val)
		case "version":
			version = strings.TrimSpace(val)
		}
	}
	return name, version
}

// importNameFromDist approximates the import name of a distribution when it does
// not ship a top_level.txt, normalizing per PEP 503 (lowercase, dashes to
// underscores).
func importNameFromDist(name string) string {
	return strings.ReplaceAll(strings.ToLower(name), "-", "_")
}
