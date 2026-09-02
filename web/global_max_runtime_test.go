package web

import (
	"testing"
	"time"

	"github.com/netresearch/ofelia/core"
)

// An API-created run job that named no bound of its own used to skip the
// operator's `[global] max-runtime` and land on the scheduler's 24h
// constant, while an INI run job in the same daemon inherited the global
// at config load. Same type, same field, different origin (#806).
//
// The divergence is invisible on a default configuration, because the
// constant and the documented global default are both 24h. These tests use
// a global that differs from it, which is the only case where an operator
// expressed an intent at all.

// stubGlobalConfig stands in for *cli.Config, which this package cannot
// import. cli asserts at compile time that the real one satisfies the same
// interface, so this stub cannot drift into testing only itself.
type stubGlobalConfig struct{ d time.Duration }

func (s stubGlobalConfig) GlobalMaxRuntime() time.Duration { return s.d }

// ptrGlobalConfig has a pointer receiver, so a nil *ptrGlobalConfig in an
// `any` satisfies the interface and panics when called — the shape a nil
// *cli.Config would take. The method body is unreachable in that case and
// exists only to satisfy the interface.
type ptrGlobalConfig struct{ d time.Duration }

func (p *ptrGlobalConfig) GlobalMaxRuntime() time.Duration { return p.d }

func runJobFor(t *testing.T, cfg any, requested string) *core.RunJob {
	t.Helper()

	// Any non-nil provider will do: newRunJobFromRequest only checks that
	// one exists before building the job.
	srv := &Server{config: cfg, provider: newHangingDockerProvider()}
	job, err := srv.newRunJobFromRequest(&jobRequest{
		Name: "guard", Type: jobTypeRun, Image: "alpine", MaxRuntime: requested,
	})
	if err != nil {
		t.Fatalf("newRunJobFromRequest(%q): %v", requested, err)
	}
	rj, ok := job.(*core.RunJob)
	if !ok {
		t.Fatalf("got %T, want *core.RunJob", job)
	}
	return rj
}

func TestRunJob_InheritsGlobalMaxRuntime(t *testing.T) {
	t.Parallel()

	cfg := stubGlobalConfig{d: 2 * time.Hour}

	t.Run("omitted inherits the global", func(t *testing.T) {
		t.Parallel()
		if got := runJobFor(t, cfg, "").MaxRuntime; got != 2*time.Hour {
			t.Errorf("MaxRuntime = %v, want the global 2h", got)
		}
	})

	// "0s" is equivalent to omitting the field (#789), so it inherits too.
	// It is not a way to ask for no bound.
	t.Run("0s inherits the global", func(t *testing.T) {
		t.Parallel()
		if got := runJobFor(t, cfg, "0s").MaxRuntime; got != 2*time.Hour {
			t.Errorf("MaxRuntime = %v, want the global 2h", got)
		}
	})

	t.Run("an explicit value wins over the global", func(t *testing.T) {
		t.Parallel()
		if got := runJobFor(t, cfg, "30m").MaxRuntime; got != 30*time.Minute {
			t.Errorf("MaxRuntime = %v, want the requested 30m", got)
		}
	})
}

// A configuration that exposes no global — or none at all — must leave the
// job at zero, where the scheduler's own default takes over. Servers built
// with a nil config are the common case in tests and embedders.
func TestRunJob_WithoutGlobalStaysZero(t *testing.T) {
	t.Parallel()

	for name, cfg := range map[string]any{
		"nil config":            nil,
		"config without global": struct{ Unrelated int }{},
		"global of zero":        stubGlobalConfig{d: 0},
		// A nil pointer stored in an `any` satisfies the interface and
		// panics on the call. NewServer takes `any` and is exported, so
		// this arrives from outside the repo, not from the daemon.
		"typed-nil provider": (*ptrGlobalConfig)(nil),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := runJobFor(t, cfg, "").MaxRuntime; got != 0 {
				t.Errorf("MaxRuntime = %v, want 0 so the scheduler default applies", got)
			}
		})
	}
}
