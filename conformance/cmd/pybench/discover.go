package main

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// Implementation is one runnable Python-like interpreter under test:
// resolved on PATH (or, for minipy, built from this checkout) and ready to
// invoke as `Command Args... sourcePath`.
type Implementation struct {
	Info    ImplInfo
	Command string
	Args    []string
	// Strip marks an implementation (gpython) that cannot parse minipy's
	// PEP 526 variable annotations: it runs against a mechanically
	// annotation-stripped copy of each source, and a run that still fails
	// is reported N/A rather than FAIL, since that reflects gpython's
	// known Python-3.4 ceiling rather than a minipy defect.
	Strip bool
}

// discoverImplementations resolves every implementation pybench knows how to
// drive: python3.13, python3, pypy3, and gpython by PATH lookup, plus minipy
// at -O0 and -O3 using the binary at minipyBin (always present, since main
// builds it from this checkout before discovery runs). It returns the
// runnable subset for gating/timing and the full roster, absent entries
// included, for the report header.
func discoverImplementations(ctx context.Context, minipyBin string) ([]Implementation, []ImplInfo) {
	var runnable []Implementation
	var roster []ImplInfo

	for _, c := range []struct {
		name string
		bin  string
	}{
		{"CPython 3.13", "python3.13"},
		{"CPython (python3)", "python3"},
		{"pypy3", "pypy3"},
	} {
		path, err := exec.LookPath(c.bin)
		if err != nil {
			roster = append(roster, ImplInfo{Name: c.name, Absent: true})
			continue
		}
		version := detectVersion(ctx, path, "--version")
		roster = append(roster, ImplInfo{Name: c.name, Version: version})
		runnable = append(runnable, Implementation{
			Info:    ImplInfo{Name: c.name, Version: version},
			Command: path,
		})
	}

	if path, err := exec.LookPath("gpython"); err != nil {
		roster = append(roster, ImplInfo{Name: "gpython", Absent: true})
	} else {
		version := detectGPythonVersion(ctx, path)
		roster = append(roster, ImplInfo{Name: "gpython", Version: version})
		runnable = append(runnable, Implementation{
			Info:    ImplInfo{Name: "gpython", Version: version},
			Command: path,
			Strip:   true,
		})
	}

	minipyVersion := "built from this checkout, " + detectGitRevision(ctx)
	for _, level := range []string{"0", "3"} {
		info := ImplInfo{Name: "minipy -O" + level, Version: minipyVersion}
		roster = append(roster, info)
		runnable = append(runnable, Implementation{
			Info:    info,
			Command: minipyBin,
			Args:    []string{"run", "-O", level},
		})
	}

	return runnable, roster
}

// detectGitRevision returns this checkout's short commit hash, or
// "unknown revision" outside a git checkout. minipy has no `--version` flag
// (it is built from source per run, not installed), so the commit it was
// built from is the closest equivalent provenance.
func detectGitRevision(ctx context.Context) string {
	rev := detectVersion(ctx, "git", "rev-parse", "--short", "HEAD")
	if rev == "" {
		return "unknown revision"
	}
	return "rev " + rev
}

// detectVersion runs `bin args...` and returns its trimmed combined output,
// or "" if the command errors. It is used for interpreters that support a
// `--version`-style flag.
func detectVersion(ctx context.Context, bin string, args ...string) string {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, args...).CombinedOutput()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// detectGPythonVersion extracts gpython's version banner. gpython has no
// `--version` flag (confirmed: it rejects the flag and prints usage
// instead); its interactive REPL prints a banner ("Python 3.4.0 (none,
// unknown)\n[Gpython dev]\n...") before reading input, so feeding it a
// closed stdin captures the banner and lets it exit on immediate EOF.
func detectGPythonVersion(ctx context.Context, bin string) string {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin)
	cmd.Stdin = strings.NewReader("")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	lines := strings.SplitN(strings.TrimSpace(string(out)), "\n", 3)
	return strings.Join(lines[:min(2, len(lines))], "; ")
}
