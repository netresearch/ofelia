// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const header = "# Changelog\n\nIntro.\n\n"

// repo writes CHANGELOG.md and the given fragments (name => content) into a
// temporary repository root.
func repo(t *testing.T, changelog string, fragments map[string]string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "CHANGELOG.md"), []byte(changelog), 0o644); err != nil {
		t.Fatal(err)
	}
	if fragments != nil {
		dir := filepath.Join(root, "changelog.d")
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, content := range fragments {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	return root
}

func tags(exists bool, err error) tagLookup {
	return func(string, string) (bool, error) { return exists, err }
}

func runIn(t *testing.T, mode, root string, lookup tagLookup) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(mode, root, lookup, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestCollectAppendsUnderExistingHeading(t *testing.T) {
	t.Parallel()
	changelog := header + "## [Unreleased]\n\n### Added\n\n- old\n\n### Fixed\n\n- old fix\n\n## [1.0.0] - 2026-01-01\n\n### Fixed\n\n- released\n"
	got, ok := collect(changelog, []fragment{{kind: "added", text: "- new"}, {kind: "fixed", text: "- new fix"}})
	if !ok {
		t.Fatal("collect reported no [Unreleased] section")
	}
	want := header + "## [Unreleased]\n\n### Added\n\n- old\n- new\n\n### Fixed\n\n- old fix\n- new fix\n\n## [1.0.0] - 2026-01-01\n\n### Fixed\n\n- released\n"
	if got != want {
		t.Fatalf("collect =\n%s\nwant\n%s", got, want)
	}
}

func TestCollectInsertsMissingHeadingsInOrder(t *testing.T) {
	t.Parallel()
	changelog := header + "## [Unreleased]\n\n### Changed\n\n- c\n\n## [1.0.0]\n"
	got, _ := collect(changelog, []fragment{
		{kind: "security", text: "- s"},
		{kind: "added", text: "- a"},
		{kind: "fixed", text: "- f"},
	})
	want := header + "## [Unreleased]\n\n### Added\n\n- a\n\n### Changed\n\n- c\n\n### Fixed\n\n- f\n\n### Security\n\n- s\n\n## [1.0.0]\n"
	if got != want {
		t.Fatalf("collect =\n%s\nwant\n%s", got, want)
	}
}

func TestCollectIntoEmptyUnreleased(t *testing.T) {
	t.Parallel()
	changelog := header + "## [Unreleased]\n\n## [1.0.0]\n"
	got, _ := collect(changelog, []fragment{{kind: "fixed", text: "- f\n  continued"}})
	want := header + "## [Unreleased]\n\n### Fixed\n\n- f\n  continued\n\n## [1.0.0]\n"
	if got != want {
		t.Fatalf("collect =\n%s\nwant\n%s", got, want)
	}
}

func TestCollectStopsAtLinkReferences(t *testing.T) {
	t.Parallel()
	changelog := header + "## [Unreleased]\n\n### Fixed\n\n- a\n\n[Unreleased]: https://example.org/compare\n"
	got, _ := collect(changelog, []fragment{{kind: "fixed", text: "- b"}})
	want := header + "## [Unreleased]\n\n### Fixed\n\n- a\n- b\n\n[Unreleased]: https://example.org/compare\n"
	if got != want {
		t.Fatalf("collect =\n%s\nwant\n%s", got, want)
	}
}

func TestCollectMatchesHeadingWithTrailingSpace(t *testing.T) {
	t.Parallel()
	changelog := header + "## [Unreleased]\n\n### Fixed \r\n\n- a\n"
	got, _ := collect(changelog, []fragment{{kind: "fixed", text: "- b"}})
	if strings.Count(got, "### Fixed") != 1 || !strings.HasSuffix(got, "- a\n- b\n\n") {
		t.Fatalf("collect added a second heading or misplaced the entry:\n%q", got)
	}
}

func TestCollectWithoutUnreleased(t *testing.T) {
	t.Parallel()
	if _, ok := collect(header+"## [1.0.0]\n", []fragment{{kind: "fixed", text: "- b"}}); ok {
		t.Fatal("collect reported success without an [Unreleased] section")
	}
}

func TestReadFragmentsValidates(t *testing.T) {
	t.Parallel()
	root := repo(t, header, map[string]string{
		"README.md":               "# Changelog fragments\n",
		"b-fix.fixed.md":          "- second\r\n",
		"a-feature.added.md":      "\n- first\n",
		"Bad-Name.fixed.md":       "- x\n",
		"no-section.md":           "- x\n",
		"unknown.upgrading.md":    "- x\n",
		"prose.changed.md":        "Text without a bullet\n",
		"heading.security.md":     "- x\n\n### Security\n",
		"star-bullet.removed.md":  "* x\n",
		".DS_Store":               "binary",
		".a-feature.added.md.swp": "binary",
		"a-feature.added.md~":     "- backup\n",
	})
	if err := os.Mkdir(filepath.Join(root, "changelog.d", "dir.fixed.md"), 0o755); err != nil {
		t.Fatal(err)
	}

	fragments, problems, err := readFragments(filepath.Join(root, "changelog.d"))
	if err != nil {
		t.Fatal(err)
	}
	if len(fragments) != 2 || fragments[0].kind != "added" || fragments[0].text != "- first" ||
		fragments[1].kind != "fixed" || fragments[1].text != "- second" {
		t.Fatalf("fragments = %+v, want the added and the fixed fragment in name order", fragments)
	}
	for _, name := range []string{
		"Bad-Name.fixed.md", "no-section.md", "unknown.upgrading.md", "prose.changed.md",
		"heading.security.md", "star-bullet.removed.md", "dir.fixed.md",
	} {
		if !strings.Contains(strings.Join(problems, "\n"), "  "+name+"  ") {
			t.Errorf("no problem reported for %s; problems:\n%s", name, strings.Join(problems, "\n"))
		}
	}
	if len(problems) != 7 {
		t.Errorf("got %d problems, want 7:\n%s", len(problems), strings.Join(problems, "\n"))
	}
}

func TestCheck(t *testing.T) {
	t.Parallel()
	released := header + "## [Unreleased]\n\n## [1.2.0] - 2026-11-13\n"
	lookupErr := errors.New("no network")
	tests := []struct {
		name      string
		changelog string
		fragments map[string]string
		lookup    tagLookup
		code      int
		inErr     string
	}{
		{"no fragments", released, nil, tags(false, nil), 0, ""},
		{"fragments, release tagged", released, map[string]string{"x.fixed.md": "- x\n"}, tags(true, nil), 0, ""},
		{"fragments, release not tagged", released, map[string]string{"x.fixed.md": "- x\n"}, tags(false, nil), 1, "has no tag v1.2.0 yet"},
		{"fragments, no release yet", header + "## [Unreleased]\n", map[string]string{"x.fixed.md": "- x\n"}, tags(false, nil), 0, ""},
		{"tag lookup fails", released, map[string]string{"x.fixed.md": "- x\n"}, tags(false, lookupErr), 2, "no network"},
		{"malformed fragment", released, map[string]string{"x.txt": "- x\n"}, tags(true, nil), 1, "x.txt  is not named"},
		{
			"malformed fragment while the tag lookup fails", released,
			map[string]string{"ok.fixed.md": "- x\n", "x.txt": "- x\n"},
			tags(false, lookupErr), 1, "x.txt  is not named",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			code, _, errOut := runIn(t, "check", repo(t, tt.changelog, tt.fragments), tt.lookup)
			if code != tt.code || !strings.Contains(errOut, tt.inErr) {
				t.Fatalf("check = %d, stderr %q; want %d with %q", code, errOut, tt.code, tt.inErr)
			}
		})
	}
}

func TestCollectCommand(t *testing.T) {
	t.Parallel()
	root := repo(t, header+"## [Unreleased]\n\n## [1.0.0]\n", map[string]string{
		"README.md":    "# Changelog fragments\n",
		"a.fixed.md":   "- a\n",
		"b.added.md":   "- b\n",
		"c.removed.md": "- c\n",
	})
	if err := os.Chmod(filepath.Join(root, "CHANGELOG.md"), 0o640); err != nil {
		t.Fatal(err)
	}

	code, out, errOut := runIn(t, "collect", root, nil)
	if code != 0 || !strings.Contains(out, "collected 3 fragment(s)") {
		t.Fatalf("collect = %d, stdout %q, stderr %q", code, out, errOut)
	}
	got, err := os.ReadFile(filepath.Join(root, "CHANGELOG.md"))
	if err != nil {
		t.Fatal(err)
	}
	want := header + "## [Unreleased]\n\n### Added\n\n- b\n\n### Removed\n\n- c\n\n### Fixed\n\n- a\n\n## [1.0.0]\n"
	if string(got) != want {
		t.Fatalf("CHANGELOG.md =\n%s\nwant\n%s", got, want)
	}
	entries, err := os.ReadDir(filepath.Join(root, "changelog.d"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "README.md" {
		t.Fatalf("changelog.d holds %v, want only README.md", entries)
	}
	info, err := os.Stat(filepath.Join(root, "CHANGELOG.md"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("CHANGELOG.md mode = %v, want 0640", info.Mode().Perm())
	}

	code, out, _ = runIn(t, "collect", root, nil)
	if code != 0 || !strings.Contains(out, "nothing to collect") {
		t.Fatalf("second collect = %d, %q; want nothing to collect", code, out)
	}
}

func TestCollectCommandRefusesBadInput(t *testing.T) {
	t.Parallel()
	root := repo(t, header+"## [1.0.0]\n", map[string]string{"a.fixed.md": "- a\n"})
	if code, _, errOut := runIn(t, "collect", root, nil); code != 1 || !strings.Contains(errOut, "no ## [Unreleased] section") {
		t.Fatalf("collect without [Unreleased] = %d, %q", code, errOut)
	}
	if _, err := os.Stat(filepath.Join(root, "changelog.d", "a.fixed.md")); err != nil {
		t.Fatalf("the fragment was deleted although nothing was collected: %v", err)
	}

	bad := repo(t, header+"## [Unreleased]\n", map[string]string{"a.fixed.md": "no bullet\n"})
	if code, _, _ := runIn(t, "collect", bad, nil); code != 1 {
		t.Fatalf("collect with a malformed fragment = %d, want 1", code)
	}
}

func TestCollectCommandReportsFragmentsItCannotDelete(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root can delete from a read-only directory")
	}
	root := repo(t, header+"## [Unreleased]\n", map[string]string{"a.fixed.md": "- a\n"})
	dir := filepath.Join(root, "changelog.d")
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	code, _, errOut := runIn(t, "collect", root, nil)
	if code != 2 || !strings.Contains(errOut, "could not be deleted") || !strings.Contains(errOut, "a.fixed.md") {
		t.Fatalf("collect = %d, stderr %q; want 2 naming the fragment left behind", code, errOut)
	}
}

func TestRunUsageAndMissingChangelog(t *testing.T) {
	t.Parallel()
	if code, _, errOut := runIn(t, "render", t.TempDir(), nil); code != 2 || !strings.Contains(errOut, "usage:") {
		t.Fatalf("unknown mode = %d, %q", code, errOut)
	}
	if code, _, errOut := runIn(t, "check", t.TempDir(), nil); code != 2 || !strings.Contains(errOut, "cannot read") {
		t.Fatalf("missing CHANGELOG.md = %d, %q", code, errOut)
	}
}

// gitEnv isolates the git calls of the tests from the user's and the
// runner's configuration and gives them an identity for commits and tags.
var gitEnv = append(os.Environ(),
	"GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1",
	"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.org",
	"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.org",
)

// gitRepo creates a git repository with one commit in a temporary directory
// and runs the given git commands ("tag -m t v1") in it.
func gitRepo(t *testing.T, cmds ...string) string {
	t.Helper()
	dir := t.TempDir()
	all := append([]string{"init -q", "commit -q --allow-empty -m c"}, cmds...)
	for _, c := range all {
		cmd := exec.Command("git", append([]string{"-C", dir}, strings.Fields(c)...)...)
		cmd.Env = gitEnv
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", c, err, out)
		}
	}
	return dir
}

func TestTagLookup(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}

	upstream := gitRepo(t, "tag -m t v2.0.0")
	lookup := tagLookupFor(upstream)

	local := gitRepo(t, "tag -m t v1.2.0")
	if ok, err := lookup(local, "1.2.0"); !ok || err != nil {
		t.Errorf("local tag: %v, %v; want true", ok, err)
	}
	// A fork without the tag, and a tree that is no repository at all.
	for name, root := range map[string]string{"fork": gitRepo(t), "no repository": t.TempDir()} {
		if ok, err := lookup(root, "2.0.0"); !ok || err != nil {
			t.Errorf("%s, tag upstream: %v, %v; want true", name, ok, err)
		}
		if ok, err := lookup(root, "3.0.0"); ok || err != nil {
			t.Errorf("%s, tag missing upstream: %v, %v; want false, nil", name, ok, err)
		}
	}

	unreachable := tagLookupFor(filepath.Join(t.TempDir(), "missing"))
	_, err := unreachable(local, "9.9.9")
	if err == nil {
		t.Fatal("unreachable upstream: want an error, not a missing tag")
	}
	// git's own reason reaches the message, not only the exit status.
	if !strings.Contains(err.Error(), "does not appear to be a git repository") {
		t.Errorf("error %q lacks git's reason", err)
	}
}

// TestRepositoryChangelogFragments runs the check against this repository,
// so CI refuses a malformed fragment and a release commit that leaves
// fragments behind. Exit code 2 means the tag lookup could not reach
// upstream; the test then skips instead of failing on a network problem.
func TestRepositoryChangelogFragments(t *testing.T) {
	t.Parallel()
	code, out, errOut := runIn(t, "check", filepath.Join("..", ".."), tagLookupFor(upstreamURL))
	if code == 2 {
		t.Skipf("the check could not run: %s", errOut)
	}
	if code != 0 {
		t.Fatalf("changelog-fragments check failed (%d):\n%s%s", code, out, errOut)
	}
}
