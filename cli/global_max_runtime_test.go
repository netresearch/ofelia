package cli

import (
	"testing"
	"time"

	"github.com/netresearch/ofelia/core"
	"github.com/netresearch/ofelia/core/persist"
	"github.com/netresearch/ofelia/web"
)

// The web server holds this configuration as `any`, so nothing in the
// compiler links the two sides of #806 except this line. Without it, a
// rename of GlobalMaxRuntime would leave the server's type assertion
// failing at runtime and every API-created run job back on the 24h
// constant — the exact bug, restored silently.
var _ web.GlobalMaxRuntimeProvider = (*Config)(nil)

func TestConfig_GlobalMaxRuntime(t *testing.T) {
	t.Parallel()

	c := &Config{}
	c.Global.MaxRuntime = 90 * time.Minute
	if got := c.GlobalMaxRuntime(); got != 90*time.Minute {
		t.Errorf("GlobalMaxRuntime() = %v, want 90m", got)
	}
}

// A run job restored from the state file has to inherit the global the
// same way the API applied it at creation, or the first restart moves
// every such job back to the scheduler default.
func TestPersistedRunJob_InheritsGlobalMaxRuntime(t *testing.T) {
	t.Parallel()

	cfg := &Config{}
	cfg.Global.MaxRuntime = 2 * time.Hour

	build := func(t *testing.T, stored string) *core.RunJob {
		t.Helper()
		c := &DaemonCommand{config: cfg}
		job, err := c.buildPersistedRunJob(
			"restored",
			&persist.Job{Type: persist.JobTypeRun, Image: "alpine", MaxRuntime: stored},
			&hangingDoctorProvider{},
		)
		if err != nil {
			t.Fatalf("buildPersistedRunJob(%q): %v", stored, err)
		}
		rj, ok := job.(*core.RunJob)
		if !ok {
			t.Fatalf("got %T, want *core.RunJob", job)
		}
		return rj
	}

	if got := build(t, "").MaxRuntime; got != 2*time.Hour {
		t.Errorf("a stored job with no bound got %v, want the global 2h", got)
	}
	if got := build(t, "45m").MaxRuntime; got != 45*time.Minute {
		t.Errorf("a stored job with its own bound got %v, want 45m", got)
	}

	// No configuration at all must not panic and must leave the job at
	// zero, where the scheduler's default applies.
	noCfg := &DaemonCommand{}
	job, err := noCfg.buildPersistedRunJob(
		"restored",
		&persist.Job{Type: persist.JobTypeRun, Image: "alpine"},
		&hangingDoctorProvider{},
	)
	if err != nil {
		t.Fatalf("buildPersistedRunJob without config: %v", err)
	}
	if rj, ok := job.(*core.RunJob); !ok || rj.MaxRuntime != 0 {
		t.Errorf("without a config the job must stay at 0, got %v", job)
	}
}
