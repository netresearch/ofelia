// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

// Command changelog-fragments checks and collects the CHANGELOG fragments in
// changelog.d/.
//
// A pull request writes its CHANGELOG entry into its own file
// changelog.d/<branch-slug>.<section>.md instead of editing the shared
// "## [Unreleased]" section, where two open pull requests conflict as soon as
// one of them merges.
//
//	check    Refuse a malformed fragment, and refuse fragments while the
//	         topmost released section has no tag yet: either the release
//	         commit skipped collect, and those notes would not ship, or the
//	         release is merged and its tag is not pushed yet.
//	collect  Append every fragment under its heading in "## [Unreleased]" and
//	         delete the fragment. The release commit runs this before it
//	         renames [Unreleased]. With no fragments it changes nothing.
//
// The file names follow towncrier's <name>.<type>.md scheme. towncrier renders
// a complete versioned section; it has no mode that appends to a hand-kept
// [Unreleased] section, which this repository keeps for Renovate and for pull
// requests that predate the fragments. The format is the one netresearch's
// t3x-nr-llm uses.
//
// Usage, from the repository root: go run ./scripts/changelog-fragments check|collect
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

// gitTimeout bounds each git call of the tag lookup, which may reach upstream.
const gitTimeout = 30 * time.Second

// upstreamURL is the repository whose tags are the releases. A fork's origin
// often lacks the later tags, so the lookup asks upstream, not origin.
const upstreamURL = "https://github.com/netresearch/ofelia.git"

// sections maps a fragment type to its CHANGELOG heading, in the order the
// headings appear (Keep a Changelog).
var sections = []struct{ kind, heading string }{
	{"added", "### Added"},
	{"changed", "### Changed"},
	{"deprecated", "### Deprecated"},
	{"removed", "### Removed"},
	{"fixed", "### Fixed"},
	{"security", "### Security"},
}

var (
	namePattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*\.(added|changed|deprecated|removed|fixed|security)\.md$`)
	releasedPattern = regexp.MustCompile(`(?m)^## \[(\d[^\]]*)\]`)
	sectionEnd      = regexp.MustCompile(`(?m)^(## \[|\[[^\]]+\]: \S)`)
	headingLine     = regexp.MustCompile(`(?m)^#`)
)

// fragment is one validated file in changelog.d/.
type fragment struct {
	path, kind, text string
}

// tagLookup reports whether tag v<version> exists.
type tagLookup func(root, version string) (bool, error)

func main() {
	mode := ""
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}
	// The repository root is the working directory: make and go run start
	// there.
	os.Exit(run(mode, ".", tagLookupFor(upstreamURL), os.Stdout, os.Stderr))
}

// run executes mode against the repository at root and returns the exit code:
// 0 for success, 1 for input that is not ready (a fragment, a missing tag, or
// no [Unreleased] section to collect into), 2 for anything else.
func run(mode, root string, tagExists tagLookup, stdout, stderr io.Writer) int {
	if mode != "check" && mode != "collect" {
		fmt.Fprintln(stderr, "usage: changelog-fragments check|collect (run from the repository root)")
		return 2
	}
	changelogPath := filepath.Join(root, "CHANGELOG.md")
	data, err := os.ReadFile(changelogPath) // #nosec G304 -- the repository's own CHANGELOG.md
	if err != nil {
		fmt.Fprintf(stderr, "changelog-fragments: cannot read %s: %v\n", changelogPath, err)
		return 2
	}
	changelog := string(data)

	fragments, problems, err := readFragments(filepath.Join(root, "changelog.d"))
	if err != nil {
		fmt.Fprintf(stderr, "changelog-fragments: %v\n", err)
		return 2
	}

	if mode == "check" {
		problem, err := untaggedRelease(root, changelog, len(fragments), tagExists)
		if err != nil {
			fmt.Fprintf(stderr, "changelog-fragments: %v\n", err)
			return 2
		}
		if problem != "" {
			problems = append(problems, problem)
		}
	}

	if len(problems) > 0 {
		fmt.Fprintf(stderr, "changelog.d/ is not ready:\n\n%s\n\nSee changelog.d/README.md for the format.\n",
			strings.Join(problems, "\n"))
		return 1
	}

	if mode == "check" {
		fmt.Fprintf(stdout, "changelog-fragments: %d fragment(s) ok\n", len(fragments))
		return 0
	}
	return collectInto(changelogPath, changelog, fragments, stdout, stderr)
}

// readFragments reads and validates the fragments, sorted by file name. A
// missing directory holds no fragments.
func readFragments(dir string) ([]fragment, []string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("cannot read %s: %w", dir, err)
	}

	kinds := make([]string, 0, len(sections))
	for _, s := range sections {
		kinds = append(kinds, s.kind)
	}

	var fragments []fragment
	var problems []string
	for _, entry := range entries { // os.ReadDir sorts by file name
		name := entry.Name()
		// Editor and OS leftovers (.DS_Store, .x.md.swp, x.md~) are not
		// fragments; a valid name starts with [a-z0-9] and ends in .md.
		if name == "README.md" || strings.HasPrefix(name, ".") || strings.HasSuffix(name, "~") {
			continue
		}
		m := namePattern.FindStringSubmatch(name)
		if m == nil || !entry.Type().IsRegular() {
			problems = append(problems, fmt.Sprintf("  %s  is not named <branch-slug>.<%s>.md", name, strings.Join(kinds, "|")))
			continue
		}
		path := filepath.Join(dir, name)
		raw, err := os.ReadFile(path) // #nosec G304 -- a regular file in the repository's changelog.d/
		if err != nil {
			return nil, nil, fmt.Errorf("cannot read %s: %w", path, err)
		}
		text := strings.TrimSpace(strings.ReplaceAll(string(raw), "\r\n", "\n"))
		switch {
		case !strings.HasPrefix(text, "- "):
			problems = append(problems, fmt.Sprintf("  %s  must start with a top-level bullet (\"- \")", name))
		case headingLine.MatchString(text):
			problems = append(problems, fmt.Sprintf("  %s  must not contain a heading; the file name selects the heading", name))
		default:
			fragments = append(fragments, fragment{path: path, kind: m[1], text: text})
		}
	}
	return fragments, problems, nil
}

// topmostReleased returns the version of the topmost released section, or "".
func topmostReleased(changelog string) string {
	if m := releasedPattern.FindStringSubmatch(changelog); m != nil {
		return m[1]
	}
	return ""
}

// untaggedRelease returns a problem when fragments are left while the topmost
// released section has no tag yet.
func untaggedRelease(root, changelog string, fragments int, tagExists tagLookup) (string, error) {
	released := topmostReleased(changelog)
	if fragments == 0 || released == "" {
		return "", nil
	}
	ok, err := tagExists(root, released)
	if err != nil || ok {
		return "", err
	}
	return fmt.Sprintf("  [%s] has no tag v%s yet, and %d fragment(s) are left. In the release commit, run"+
		" `make changelog-collect` before renaming [Unreleased], or the release ships without their notes."+
		" If the release is already merged, push tag v%s and run the check again.",
		released, released, fragments, released), nil
}

// tagLookupFor returns a lookup that asks for tag v<version> in the local
// repository first and then at url.
//
// A shallow CI checkout carries no tags, and a fork's origin often lacks the
// later ones, so the release repository itself is asked with ls-remote, which
// fetches nothing and needs no local repository. A failing ls-remote is an
// error, because reporting it as a missing tag would name the wrong cause.
func tagLookupFor(url string) tagLookup {
	return func(root, version string) (bool, error) {
		ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
		defer cancel()
		ref := "refs/tags/v" + version
		// #nosec G204 -- fixed git subcommands; root is the repository, the tag
		// name is built from a version in its CHANGELOG.md, url is a constant
		if exec.CommandContext(ctx, "git", "-C", root, "rev-parse", "--quiet", "--verify", ref).Run() == nil {
			return true, nil
		}
		out, err := exec.CommandContext(ctx, "git", "ls-remote", "--tags", url, ref).Output() // #nosec G204 -- see above
		if err != nil {
			return false, fmt.Errorf("cannot ask %s whether tag v%s exists (%w); check the network and run the check again",
				url, version, err)
		}
		return strings.TrimSpace(string(out)) != "", nil
	}
}

// collect appends the fragments to the [Unreleased] section of changelog. It
// reports false when the changelog has no such section.
//
// The section is split into blocks at its "### " headings. A fragment goes to
// the end of the block with its heading; a missing heading is inserted before
// the first heading that follows it in sections order.
func collect(changelog string, fragments []fragment) (string, bool) {
	start := strings.Index(changelog, "## [Unreleased]")
	if start < 0 {
		return "", false
	}
	rest := changelog[start:]
	end := len(rest)
	// Skip the [Unreleased] heading itself when looking for the next section.
	if loc := sectionEnd.FindStringIndex(rest[len("## [Unreleased]"):]); loc != nil {
		end = len("## [Unreleased]") + loc[0]
	}
	blocks := splitBlocks(rest[:end])
	heads := make([]string, len(blocks))
	for i, b := range blocks {
		heads[i] = strings.TrimRight(strings.SplitN(b, "\n", 2)[0], " \t\r")
	}

	for _, f := range fragments {
		heading := headingOf(f.kind)
		index := slices.Index(heads, heading)
		if index < 0 {
			index = insertionPoint(heads, heading)
			blocks[index-1] = strings.TrimRight(blocks[index-1], "\r\n") + "\n\n"
			blocks = slices.Insert(blocks, index, heading+"\n")
			heads = slices.Insert(heads, index, heading)
		}
		blocks[index] = appendEntry(blocks[index], f.text)
	}
	return changelog[:start] + strings.Join(blocks, "") + rest[end:], true
}

// appendEntry adds an entry at the end of a block. Entries follow each other
// without a blank line, as in this CHANGELOG, so the list stays tight; the
// first entry under a heading gets a blank line after the heading.
func appendEntry(block, entry string) string {
	trimmed := strings.TrimRight(block, " \t\r\n")
	sep := "\n"
	if !strings.Contains(trimmed, "\n") {
		sep = "\n\n"
	}
	return trimmed + sep + entry + "\n\n"
}

// splitBlocks splits a section at lines that start with "### ". Block 0 is the
// section heading plus anything above the first "### " heading.
func splitBlocks(section string) []string {
	var blocks []string
	last := 0
	for i := 1; i < len(section); i++ {
		if section[i-1] == '\n' && strings.HasPrefix(section[i:], "### ") {
			blocks = append(blocks, section[last:i])
			last = i
		}
	}
	return append(blocks, section[last:])
}

func headingOf(kind string) string {
	for _, s := range sections {
		if s.kind == kind {
			return s.heading
		}
	}
	return ""
}

func rank(heading string) int {
	for i, s := range sections {
		if s.heading == heading {
			return i
		}
	}
	return -1
}

// insertionPoint returns the block index before which a missing heading goes:
// the first known heading that ranks after it, or the end of the section.
func insertionPoint(heads []string, heading string) int {
	want := rank(heading)
	for i := 1; i < len(heads); i++ {
		if have := rank(heads[i]); have > want {
			return i
		}
	}
	return len(heads)
}

// collectInto writes the collected changelog and deletes the fragments.
//
// The new changelog is written to a temporary file beside the old one and
// renamed over it only once it is complete, so a failed write leaves
// CHANGELOG.md as it was. The fragments are deleted only after the rename.
func collectInto(changelogPath, changelog string, fragments []fragment, stdout, stderr io.Writer) int {
	if len(fragments) == 0 {
		fmt.Fprintln(stdout, "changelog-fragments: nothing to collect")
		return 0
	}
	collected, ok := collect(changelog, fragments)
	if !ok {
		fmt.Fprintf(stderr, "changelog-fragments: %s has no ## [Unreleased] section to collect into\n", changelogPath)
		return 1
	}
	if err := replaceFile(changelogPath, collected); err != nil {
		fmt.Fprintf(stderr, "changelog-fragments: cannot write %s (%v); it and the fragments are unchanged.\n", changelogPath, err)
		return 2
	}

	// A fragment left behind would be collected a second time by the next run.
	var kept []string
	for _, f := range fragments {
		if err := os.Remove(f.path); err != nil {
			kept = append(kept, f.path)
		}
	}
	if len(kept) > 0 {
		fmt.Fprintf(stderr, "changelog-fragments: %s now holds the fragments, but %d could not be deleted."+
			" Delete them before the next run, or it adds their entries again:\n  %s\n",
			changelogPath, len(kept), strings.Join(kept, "\n  "))
		return 2
	}
	fmt.Fprintf(stdout, "changelog-fragments: collected %d fragment(s) into [Unreleased]\n", len(fragments))
	return 0
}

// replaceFile writes content to a new temporary file beside path, copies the
// mode of path, and renames it over path.
func replaceFile(path, content string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err //nolint:wrapcheck // the caller adds the context
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".collect.")
	if err != nil {
		return err //nolint:wrapcheck // the caller adds the context
	}
	name := tmp.Name()
	_, werr := tmp.WriteString(content)
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr, os.Chmod(name, info.Mode().Perm())); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return err //nolint:wrapcheck // the caller adds the context
	}
	return nil
}
