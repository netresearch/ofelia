// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package config

import (
	"strings"
	"testing"
)

// A string field carrying no `default` tag is required unless it is on the
// optional list. That heuristic demanded web-UI credentials from every config
// that switched strict validation on, including the ones with no web UI, so
// the only way to run ofelia was to leave the checking off. These pin the
// conditional rule that replaced it: the credentials are required exactly when
// the flag that uses them is set.

// gatedConfig mirrors the shape of the real global section: a boolean that
// turns a feature on, and the fields that feature needs.
type gatedConfig struct {
	WebAuthEnabled bool   `gcfg:"web-auth-enabled"   mapstructure:"web-auth-enabled"   default:"false"`
	WebPasswordarg string `gcfg:"web-password-hash"  mapstructure:"web-password-hash"`
	WebSecretKey   string `gcfg:"web-secret-key"     mapstructure:"web-secret-key"`
}

func TestConditionalRequired_NotDemandedWhenFeatureIsOff(t *testing.T) {
	t.Parallel()

	err := NewConfigValidator(&gatedConfig{WebAuthEnabled: false}).Validate()
	if err != nil {
		t.Errorf("web credentials were demanded with web auth off: %v", err)
	}
}

func TestConditionalRequired_DemandedWhenFeatureIsOn(t *testing.T) {
	t.Parallel()

	err := NewConfigValidator(&gatedConfig{WebAuthEnabled: true}).Validate()
	if err == nil {
		t.Fatal("web auth is on with no password hash or secret key, expected an error")
	}

	for _, want := range []string{"web-password-hash", "web-secret-key"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention the missing %s", err, want)
		}
	}
}

// TestConditionalRequired_SatisfiedWhenProvided closes the loop: with the
// feature on and the fields filled in, nothing is reported.
func TestConditionalRequired_SatisfiedWhenProvided(t *testing.T) {
	t.Parallel()

	err := NewConfigValidator(&gatedConfig{
		WebAuthEnabled: true,
		WebPasswordarg: "$2a$12$abcdefghijklmnopqrstuv",
		WebSecretKey:   "a-secret",
	}).Validate()
	if err != nil {
		t.Errorf("a complete web-auth config was rejected: %v", err)
	}
}

// TestConditionalRequired_UnknownGateStaysRequired pins the fallback. If the
// mapping and the config drift apart so the gate cannot be found, the field
// stays required — surfacing beats silently dropping a check.
func TestConditionalRequired_UnknownGateStaysRequired(t *testing.T) {
	t.Parallel()

	// No web-auth-enabled field at all, so the gate is unresolvable.
	type noGate struct {
		WebSecretKey string `gcfg:"web-secret-key" mapstructure:"web-secret-key"`
	}

	if err := NewConfigValidator(&noGate{}).Validate(); err == nil {
		t.Error("with no gate to consult the field should stay required, got no error")
	}
}
