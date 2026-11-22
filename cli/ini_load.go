// Copyright (c) 2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cli

import (
	"fmt"
	"os"
	"slices"
	"strings"

	ini "gopkg.in/ini.v1"
)

// loadINI parses an INI config the way the daemon reads it: with
// iniLoadOptions, and with a value that holds only a comment left empty.
func loadINI(data []byte) (*ini.File, error) {
	f, err := ini.LoadSources(iniLoadOptions, data)
	if err != nil {
		return nil, fmt.Errorf("parse INI: %w", err)
	}
	// The same text read without SpaceBeforeInlineComment, which the options
	// add, tells which values are only a comment.
	withoutSpaceRule := iniLoadOptions
	withoutSpaceRule.SpaceBeforeInlineComment = false
	plain, err := ini.LoadSources(withoutSpaceRule, data)
	if err != nil {
		return nil, fmt.Errorf("parse INI: %w", err)
	}
	blankCommentValues(f, plain)
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

// blankCommentValues drops from f each value that holds only a comment, such
// as "log-level = # default". With SpaceBeforeInlineComment, go-ini drops the
// space after the delimiter before it looks for a comment, so the comment
// becomes the value. plain is the same text parsed without that option: a
// value of f that starts with # or ; is kept only where plain has it too, as
// for a quoted value or a multi-line block. A key left without a value is
// empty, as plain has it.
func blankCommentValues(f, plain *ini.File) {
	for _, sec := range f.Sections() {
		plainSec, err := plain.GetSection(sec.Name())
		if err != nil {
			continue
		}
		for _, key := range sec.Keys() {
			values := key.ValueWithShadows()
			var plainValues []string
			if plainSec.HasKey(key.Name()) {
				plainValues = plainSec.Key(key.Name()).ValueWithShadows()
			}
			if kept := withoutCommentValues(values, plainValues); len(kept) != len(values) {
				replaceValues(sec, key, kept)
			}
		}
	}
}

// withoutCommentValues returns values without those that start with # or ;
// and that plainValues, the values of the plain parse, do not hold.
func withoutCommentValues(values, plainValues []string) []string {
	return slices.DeleteFunc(slices.Clone(values), func(v string) bool {
		return (strings.HasPrefix(v, "#") || strings.HasPrefix(v, ";")) && !slices.Contains(plainValues, v)
	})
}

// replaceValues sets the values of key to kept, or to an empty value when
// kept is empty. A key with shadows is recreated, because go-ini cannot drop
// a shadow.
func replaceValues(sec *ini.Section, key *ini.Key, kept []string) {
	if len(kept) == 0 {
		kept = []string{""}
	}
	if len(key.ValueWithShadows()) == 1 {
		key.SetValue(kept[0])
		return
	}
	name := key.Name()
	sec.DeleteKey(name)
	nk, _ := sec.NewKey(name, kept[0])
	for _, v := range kept[1:] {
		_ = nk.AddShadow(v)
	}
}
