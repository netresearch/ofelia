// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"fmt"
	"reflect"
)

// The Hash methods below give every job type that embeds BareJob a
// change-detection key over all of its own `hash:"true"` fields, not only the
// BareJob ones. Without them the type inherits BareJob.Hash, which reflects
// over BareJob alone: a live reload then sees no change when only `user`,
// `container`, `environment`, `privileged` and the like differ, and keeps
// running the old definition until a restart (issue #876). GetHash descends
// into the embedded BareJob, so schedule, name, command and run-on-startup stay
// part of each key. RunJob is covered by RunJobConfig.Hash in package cli.

// Hash returns the change-detection key over every `hash:"true"` field of the
// exec job, the embedded BareJob fields included.
func (j *ExecJob) Hash() (string, error) {
	return hashJobFields(reflect.TypeFor[ExecJob](), reflect.ValueOf(j).Elem())
}

// Hash returns the change-detection key over every `hash:"true"` field of the
// local job, the embedded BareJob fields included.
func (j *LocalJob) Hash() (string, error) {
	return hashJobFields(reflect.TypeFor[LocalJob](), reflect.ValueOf(j).Elem())
}

// Hash returns the change-detection key over every `hash:"true"` field of the
// compose job, the embedded BareJob fields included.
func (j *ComposeJob) Hash() (string, error) {
	return hashJobFields(reflect.TypeFor[ComposeJob](), reflect.ValueOf(j).Elem())
}

// Hash returns the change-detection key over every `hash:"true"` field of the
// service job, the embedded BareJob fields included.
func (j *RunServiceJob) Hash() (string, error) {
	return hashJobFields(reflect.TypeFor[RunServiceJob](), reflect.ValueOf(j).Elem())
}

func hashJobFields(t reflect.Type, v reflect.Value) (string, error) {
	var hash string
	if err := GetHash(t, v, &hash); err != nil {
		return "", fmt.Errorf("failed to generate hash for %s: %w", t.Name(), err)
	}
	return hash, nil
}
