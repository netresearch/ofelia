package web

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/static"
)

// TestUI_RefreshOrderingInvariants guards the response-ordering and
// state-repair invariants of the dashboard poll loop in app.js.
//
// Like ui_history_state_test.go, this is a text-level guard on the embedded
// asset: the repo has no JavaScript test harness, and the behaviors were
// verified by tracing the request/response interleavings by hand. Each
// assertion names an interleaving that silently regresses if a future
// rewrite drops the corresponding check.
func TestUI_RefreshOrderingInvariants(t *testing.T) {
	t.Parallel()

	appJS, err := static.UI.ReadFile("ui/app.js")
	require.NoError(t, err, "embedded web UI must contain ui/app.js")
	ui := string(appJS)

	require.Contains(t, ui, "seq <= adoptedSeq",
		"refresh() must discard a poll response once a response at least as new was "+
			"ADOPTED (comparing against the newest started request instead starves the "+
			"dashboard when every response is slower than the poll interval)")

	require.Contains(t, ui, "historyAdoptedSeq",
		"direct history loads and the poll's ?history= rider must share one sequence "+
			"domain; with only an in-flight yield, a stale poll response landing after a "+
			"direct load completed repaints older history over fresher rows")

	require.Contains(t, ui, "historySeq > historyAdoptedSeq",
		"the poll rider must adopt history through the shared sequence domain, not "+
			"apply unconditionally, or a slow stale rider overwrites a fresher direct load")

	require.Contains(t, ui, "lastDashboardPayload = null;",
		"the history modal close handler must forget the last payload: history state "+
			"reset outside the guards (loader/error after a reopen) can otherwise never be "+
			"repaired while the dashboard payload stays byte-identical, because the "+
			"identical-payload short-circuit returns before the history rider")

	require.Contains(t, ui, "e.storageArea !== localStorage",
		"the storage listener must ignore sessionStorage events from same-origin "+
			"iframes/documents, or a host page's sessionStorage.clear() resets the "+
			"displayed timezone while localStorage still holds the real preference")
}
