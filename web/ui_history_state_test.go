package web

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/static"
)

// TestUI_HistoryOutputKeepsExpandedStateAcrossRefresh guards the fix for
// https://github.com/netresearch/ofelia/issues/764.
//
// The UI polls every 5s and re-renders the history table from scratch by
// resetting the tbody innerHTML. Before the fix the expanded output elements
// were re-created collapsed, so any output the user had expanded snapped shut
// within 5 seconds. loadHistory() must therefore record which outputs are
// expanded (open subrows, `tr.output-row.open`) and restore them on the fresh
// rows.
//
// This is a text-level guard on the embedded asset: the repo has no JavaScript
// test harness, and the behavior was verified in a real browser. It fails loudly
// if a future rewrite of loadHistory() drops the state preservation.
func TestUI_HistoryOutputKeepsExpandedStateAcrossRefresh(t *testing.T) {
	t.Parallel()

	appJS, err := static.UI.ReadFile("ui/app.js")
	require.NoError(t, err, "embedded web UI must contain ui/app.js")
	ui := string(appJS)

	require.Contains(t, ui, "tr.output-row.open",
		"loadHistory() must read back which output subrows are currently open before it "+
			"replaces the history table, otherwise the 5s refresh collapses them (issue #764)")

	require.Contains(t, ui, "data-key",
		"each output subrow must carry a stable key so the expanded state is restored "+
			"per execution rather than per row position (issue #764)")

	require.Contains(t, ui, "tbody.dataset.job === name",
		"the expanded-state read-back must be scoped to the job currently shown, so a "+
			"job switch does not leak another job's expanded keys")
}
