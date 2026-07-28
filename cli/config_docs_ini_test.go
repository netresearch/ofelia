// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	ini "gopkg.in/ini.v1"
)

const (
	// docsINIFence opens a documented INI snippet.
	docsINIFence = "```ini"
	// docsINISkipMarker in the fence info string exempts a snippet from
	// parsing. Reserved for snippets that are deliberately not valid config:
	// placeholder section names, "wrong vs. correct" typo examples.
	docsINISkipMarker = "no-validate"
)

// docsINIBlock is one fenced INI snippet lifted out of a Markdown file.
type docsINIBlock struct {
	file string // repo-relative path
	line int    // 1-based line of the opening fence
	body string
}

// TestDocumentedINIParses feeds every ```ini block in every Markdown file of
// the repository through the real INI loader and section decoder, so a
// documented configuration cannot be invalid and cannot silently stop being
// valid when a key is renamed.
//
// A snippet fails when it does not parse, when it uses a section the daemon
// never looks at, or when it uses a key no struct field claims. The last case is
// the drift this guards: renaming a mapstructure tag without touching the docs
// leaves the old key in the snippet, where the parser ignores it.
func TestDocumentedINIParses(t *testing.T) {
	t.Parallel()

	blocks, _ := collectDocsINIBlocks(t)
	if len(blocks) == 0 {
		t.Fatal("no ```ini blocks found in any *.md file - extraction is broken")
	}

	webhookKeys := webhookINIKeys(t)

	for _, b := range blocks {
		name := b.file + ":" + strconv.Itoa(b.line)
		t.Run(name, func(t *testing.T) {
			cfg, err := ini.LoadSources(
				ini.LoadOptions{AllowShadows: true, InsensitiveKeys: true},
				[]byte(b.body),
			)
			if err != nil {
				t.Fatalf("%s: INI does not load: %v", name, err)
			}

			c := NewConfig(discardLogger())
			res, err := parseIni(cfg, c)
			if err != nil {
				t.Fatalf("%s: parse failed: %v", name, err)
			}

			checkSections(t, b, cfg, webhookKeys)
			checkUnknownKeys(t, b, res)
		})
	}
}

// TestSkipMarkerUsageIsExactlyDeclared pins which snippets are exempt from
// TestDocumentedINIParses. The marker is the one way to bypass that gate, and
// it bypasses silently, so an unwanted key could be hidden by tagging its
// block instead of fixing the docs. Listing the exemptions here means adding
// one is a deliberate edit to this test, reviewed on its own merits.
//
// Both entries below are snippets that cannot be valid config by design.
func TestSkipMarkerUsageIsExactlyDeclared(t *testing.T) {
	t.Parallel()

	// file -> why that file's snippet may not be parsed.
	want := map[string]string{
		"docs/CONFIGURATION.md":   `[job-TYPE "NAME"] placeholder schema, not a real section name`,
		"docs/TROUBLESHOOTING.md": "shows a misspelled key so readers recognize the typo",
	}

	_, skipped := collectDocsINIBlocks(t)

	got := make(map[string]int, len(skipped))
	for _, b := range skipped {
		got[b.file]++
		if _, ok := want[b.file]; !ok {
			t.Errorf("%s:%d: new %q exemption; fix the snippet, or add it to want here with the reason it cannot be valid config",
				b.file, b.line, docsINISkipMarker)
		}
	}
	for file, reason := range want {
		switch got[file] {
		case 1:
		case 0:
			t.Errorf("%s no longer uses %q (%s) - drop the entry from want",
				file, docsINISkipMarker, reason)
		default:
			t.Errorf("%s uses %q %d times, expected 1 - a second exemption needs its own justification",
				file, docsINISkipMarker, got[file])
		}
	}
}

// checkSections rejects section names the daemon never looks at and, for the
// sections whose keys are read by explicit lookups rather than by mapstructure
// ([webhook "name"]), rejects unknown keys too. Both are silent at runtime.
//
// Keys above the first section header come from a snippet quoting a few lines
// without their [section]. They are checked against the union of every
// recognized key, which still catches a rename or a typo.
func checkSections(t *testing.T, b docsINIBlock, cfg *ini.File, webhookKeys map[string]bool) {
	t.Helper()
	for _, section := range cfg.Sections() {
		name := strings.TrimSpace(section.Name())
		switch {
		case name == ini.DefaultSection:
			all := allKnownINIKeys(webhookKeys)
			for _, k := range section.KeyStrings() {
				if !all[strings.ToLower(k)] {
					reportUnknownKey(t, b, "headerless", k)
				}
			}
		case name == "global", name == "docker":
		case strings.HasPrefix(name, jobExec), strings.HasPrefix(name, jobServiceRun),
			strings.HasPrefix(name, jobRun), strings.HasPrefix(name, jobLocal),
			strings.HasPrefix(name, jobCompose):
		case strings.HasPrefix(name, webhookSection):
			for _, k := range section.KeyStrings() {
				if !webhookKeys[strings.ToLower(k)] {
					reportUnknownKey(t, b, name, k)
				}
			}
		default:
			t.Errorf("%s:%d: unknown section [%s]", b.file, b.line, name)
		}
	}
}

// checkUnknownKeys reports every key mapstructure left unused, i.e. every
// documented key with no matching struct field.
func checkUnknownKeys(t *testing.T, b docsINIBlock, res *parseResult) {
	t.Helper()
	if res == nil {
		return
	}
	for _, k := range res.unknownGlobal {
		reportUnknownKey(t, b, "global", k)
	}
	for _, k := range res.unknownDocker {
		reportUnknownKey(t, b, "docker", k)
	}
	for _, j := range res.unknownJobs {
		for _, k := range j.UnknownKeys {
			reportUnknownKey(t, b, j.JobType+" \""+j.JobName+"\"", k)
		}
	}
}

// reportUnknownKey fails the block. Every documented key the parser does not
// recognize is a documentation bug: the daemon ignores it without a warning,
// so an operator who pastes the snippet gets none of the promised behavior.
// There is no allow-list — a key that cannot be honored must not be documented.
func reportUnknownKey(t *testing.T, b docsINIBlock, section, key string) {
	t.Helper()
	t.Errorf("%s:%d: [%s] uses unknown key %q - not a key the parser recognizes",
		b.file, b.line, section, key)
}

// allKnownINIKeys is every key any section recognizes, used to check snippets
// quoted without their section header. All of it is derived from the structs the
// parser decodes into, so a renamed mapstructure tag drops out of the set.
func allKnownINIKeys(webhookKeys map[string]bool) map[string]bool {
	all := make(map[string]bool, len(webhookKeys))
	for k := range webhookKeys {
		all[k] = true
	}
	add := func(keys []string) {
		for _, k := range keys {
			all[strings.ToLower(k)] = true
		}
	}
	add(globalKnownKeys())
	add(dockerKnownKeys())
	for _, jobType := range []string{jobExec, jobRun, jobServiceRun, jobLocal, jobCompose} {
		add(getKnownKeysForJobType(jobType))
	}
	return all
}

// collectDocsINIBlocks extracts the ```ini fenced blocks from every Markdown
// file in the repository, returning the blocks to validate and, separately,
// those whose fence info string carries docsINISkipMarker. The file list is
// derived by walking the tree from the repo root (located via go.mod), so a
// newly added document is covered without touching this test. Only
// vendored/generated trees are excluded — they are not this repo's
// documentation.
func collectDocsINIBlocks(t *testing.T) (blocks, skipped []docsINIBlock) {
	t.Helper()
	repoRoot := filepath.Dir(findRepoFile(t, "go.mod"))

	var paths []string
	err := filepath.WalkDir(repoRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// .git is not documentation; vendor/ and node_modules/ hold
			// third-party files this repo does not author.
			switch d.Name() {
			case ".git", "vendor", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		// CHANGELOG.md is a historical record: its snippets illustrate the
		// syntax of the release they were written for, often as deliberately
		// incomplete fragments. Validating history against the CURRENT parser
		// would demand rewriting old release notes after every rename, so it
		// is the one Markdown file exempted from this gate.
		if path == filepath.Join(repoRoot, "CHANGELOG.md") {
			return nil
		}
		if strings.EqualFold(filepath.Ext(path), ".md") {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk repo for *.md: %v", err)
	}
	sort.Strings(paths)

	blocks = make([]docsINIBlock, 0, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(path) // #nosec G304 -- test reads repo file by computed path
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			t.Fatalf("rel %s: %v", path, err)
		}
		b, sk := extractINIBlocks(filepath.ToSlash(rel), string(data))
		blocks = append(blocks, b...)
		skipped = append(skipped, sk...)
	}
	return blocks, skipped
}

// extractINIBlocks scans Markdown for ```ini fences. Nested fences are not a
// concern: INI has no fenced-code syntax of its own. Blocks carrying the skip
// marker are returned separately rather than dropped, so the exemptions stay
// countable — see TestSkipMarkerUsageIsExactlyDeclared.
func extractINIBlocks(rel, content string) (blocks, skipped []docsINIBlock) {
	lines := strings.Split(content, "\n")
	for i := 0; i < len(lines); i++ {
		info, ok := iniFenceInfo(lines[i])
		if !ok {
			continue
		}
		start := i + 1 // 1-based line of the opening fence
		var body []string
		for i++; i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), "```"); i++ {
			body = append(body, lines[i])
		}
		b := docsINIBlock{file: rel, line: start, body: strings.Join(body, "\n")}
		if strings.Contains(info, docsINISkipMarker) {
			skipped = append(skipped, b)
			continue
		}
		blocks = append(blocks, b)
	}
	return blocks, skipped
}

// iniFenceInfo reports whether line opens an INI code fence and returns the rest
// of the info string, where the skip marker may live.
func iniFenceInfo(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, docsINIFence) {
		return "", false
	}
	rest := trimmed[len(docsINIFence):]
	if rest != "" && !strings.HasPrefix(rest, " ") {
		return "", false // e.g. ```inifoo
	}
	return strings.TrimSpace(rest), true
}

// webhookINIKeys reads the key names parseWebhookConfig actually looks up,
// straight out of config_webhook.go. Deriving them from the source keeps this
// test from carrying a second, drifting copy of the webhook key list — the
// [webhook] section is decoded by explicit GetKey calls, not by mapstructure, so
// parseResult has nothing to report for it.
func webhookINIKeys(t *testing.T) map[string]bool {
	t.Helper()
	src, err := os.ReadFile(findRepoFile(t, "cli", "config_webhook.go")) // #nosec G304 -- repo file
	if err != nil {
		t.Fatalf("read config_webhook.go: %v", err)
	}
	re := regexp.MustCompile(`section\.GetKey\("([^"]+)"\)`)
	keys := make(map[string]bool)
	for _, m := range re.FindAllStringSubmatch(string(src), -1) {
		keys[strings.ToLower(m[1])] = true
	}
	if len(keys) == 0 {
		t.Fatal("no section.GetKey calls found in config_webhook.go - extraction is broken")
	}
	return keys
}
