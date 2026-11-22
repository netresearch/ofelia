// Copyright (c) 2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"bytes"
	"fmt"
	"os"

	ini "gopkg.in/ini.v1"
)

// loadINI parses an INI config the way the daemon reads it: with
// iniLoadOptions, after blankCommentValues.
func loadINI(data []byte) (*ini.File, error) {
	f, err := ini.LoadSources(iniLoadOptions, blankCommentValues(data))
	if err != nil {
		return nil, fmt.Errorf("parse INI: %w", err)
	}
	return f, nil
}

// LoadINIFile parses the INI file at path as the daemon does, for a reader
// outside this package such as the early read of the log level.
func LoadINIFile(path string) (*ini.File, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path from the config flag, as the daemon reads it
	if err != nil {
		return nil, fmt.Errorf("read %q: %w", path, err)
	}
	return loadINI(data)
}

// blankCommentValues empties a value that holds only a comment, such as
// "log-level = # default". With SpaceBeforeInlineComment, go-ini drops the
// space after the delimiter before it looks for a comment, so the comment
// would become the value; without that option such a line had an empty
// value. A quoted value and the lines of a """ block stay as they are.
func blankCommentValues(src []byte) []byte {
	lines := bytes.Split(src, []byte("\n"))
	inBlock := false
	for i, line := range lines {
		if bytes.Count(line, []byte(`"""`))%2 == 1 {
			inBlock = !inBlock
			continue
		}
		if inBlock {
			continue
		}
		t := bytes.TrimLeft(line, " \t")
		if len(t) == 0 || t[0] == '#' || t[0] == ';' || t[0] == '[' {
			continue
		}
		d := bytes.IndexAny(t, "=:")
		if d < 0 {
			continue
		}
		if v := bytes.TrimLeft(t[d+1:], " \t"); len(v) > 0 && (v[0] == '#' || v[0] == ';') {
			lines[i] = line[:len(line)-len(t)+d+1]
		}
	}
	return bytes.Join(lines, []byte("\n"))
}
