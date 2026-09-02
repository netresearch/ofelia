//go:build e2e && unix
// +build e2e,unix

// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package e2e

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// TestE2E_WebUI_LogScrollSurvivesRefresh pins the fix for
// https://github.com/netresearch/ofelia/issues/808.
//
// The reporter scrolled to the bottom of a long log and was thrown back to
// the top about a second later, which makes a long log unreadable. The
// cause is that the 5s poll rebuilds the history table via innerHTML: every
// <pre> that owns the scrollbar is replaced, and a fresh element starts at
// scrollTop 0. Keeping the row expanded across the refresh was already
// handled (#764); where the reader had scrolled inside it was not.
//
// The job below produces a log far taller than the 20rem the output block
// is capped at, and runs often enough that the history really does change
// between ticks — a run that never changes would be skipped by the render
// guard and would prove nothing.
func TestE2E_WebUI_LogScrollSurvivesRefresh(t *testing.T) {
	t.Parallel()

	browserPath := chromeExecutable()
	if browserPath == "" {
		t.Skip("no Chrome/Chromium executable found")
	}

	addr := reserveLoopbackAddr(t)
	configBody := `
[job-local "chatty"]
  schedule = @every 3s
  command = sh -c 'seq 1 300 | sed "s/^/log line /" && seq 1 300 | sed "s/^/err line /" 1>&2'
`
	configPath := writeConfig(t, configBody)
	daemon := startDaemon(t, configPath, "--enable-web", "--web-address="+addr)
	t.Cleanup(func() { daemon.shutdown(t, 15*time.Second) })

	if err := daemon.waitForLog(`Job \"chatty\"`, 20*time.Second); err != nil {
		t.Fatalf("job did not run before the UI check: %v\nstdout=%s", err, daemon.stdout.String())
	}

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(),
		append(chromedp.DefaultExecAllocatorOptions[:],
			chromedp.ExecPath(browserPath),
		)...)
	t.Cleanup(cancelAlloc)

	browserCtx, cancelBrowser := chromedp.NewContext(allocCtx)
	t.Cleanup(cancelBrowser)
	ctx, cancelTimeout := context.WithTimeout(browserCtx, 120*time.Second)
	t.Cleanup(cancelTimeout)

	const (
		jobRow        = `#jobs tbody tr[data-job-name="chatty"] button.job-name`
		historyToggle = `#history tbody button[data-action="toggle-output"]`
		// The stderr block, i.e. the second <pre> of the expanded subrow —
		// the one whose scroll key would be wrong if the state were keyed by
		// position instead of by stream.
		lastOpenPre = `(() => { const p = document.querySelectorAll('#history tbody tr.output-row.open pre'); return p[p.length - 1]; })()`
	)

	var scrolledTo, maxScroll, afterRefresh, bottomAfter float64
	var newestKeyBefore string
	if err := chromedp.Run(ctx,
		chromedp.Navigate("http://"+addr+"/"),
		chromedp.WaitVisible(jobRow, chromedp.ByQuery),
		chromedp.Click(jobRow, chromedp.ByQuery),
		chromedp.WaitVisible(historyToggle, chromedp.ByQuery),

		// Expand the NEWEST run, not the first: the history is capped, so
		// the oldest row is the one about to be evicted, and picking it
		// makes the test flaky on a slow runner for reasons that have
		// nothing to do with scrolling. Same rationale as
		// TestE2E_WebUI_ExpandedOutputSurvivesRefresh.
		chromedp.Evaluate(`(() => {
			const all = document.querySelectorAll('`+historyToggle+`');
			const btn = all[all.length - 1];
			if (!btn) return false;
			btn.click();
			return true;
		})()`, nil),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Evaluate(`(() => {
			const pre = `+lastOpenPre+`;
			if (!pre) return -1;
			pre.scrollTop = pre.scrollHeight;
			return pre.scrollTop;
		})()`, &scrolledTo),
		chromedp.Evaluate(`(() => {
			const pre = `+lastOpenPre+`;
			return pre ? pre.scrollHeight - pre.clientHeight : -1;
		})()`, &maxScroll),

		// The newest run's key, so the wait below can tell a rebuilt table
		// from a table nothing happened to.
		chromedp.Evaluate(`(() => {
			const all = document.querySelectorAll('`+historyToggle+`');
			const b = all[all.length - 1];
			return b ? String(b.dataset.key) : '';
		})()`, &newestKeyBefore),
	); err != nil {
		t.Fatalf("driving the web UI failed: %v", err)
	}

	// Wait for the table to actually be rebuilt rather than assuming it. A
	// bare sleep would pass even if the refresh had stopped happening —
	// precisely the condition this test exists to notice. A different
	// newest key proves a new execution landed and the poll re-rendered
	// the table around the block whose scroll position is under test.
	newestChanged := `(() => {
		const all = document.querySelectorAll('` + historyToggle + `');
		const b = all[all.length - 1];
		return !!b && String(b.dataset.key) !== ` + strconv.Quote(newestKeyBefore) + `;
	})()`
	if err := chromedp.Run(ctx,
		chromedp.Poll(newestChanged, nil, chromedp.WithPollingTimeout(40*time.Second)),
		chromedp.Evaluate(`(() => {
			const pre = `+lastOpenPre+`;
			return pre ? pre.scrollTop : -1;
		})()`, &afterRefresh),
		// The bottom as it stands AFTER the rebuild: comparing only against
		// the old absolute offset would pass even if the block had grown
		// and the reader were no longer at the end of it.
		chromedp.Evaluate(`(() => {
			const pre = `+lastOpenPre+`;
			return pre ? pre.scrollHeight - pre.clientHeight : -1;
		})()`, &bottomAfter),
	); err != nil {
		t.Fatalf("waiting for the refresh failed: %v", err)
	}

	if maxScroll <= 0 {
		t.Fatalf("the output block did not overflow (max scroll %.0f), so this test could not "+
			"observe scrolling at all — the fixture log is too short for the block height", maxScroll)
	}
	if scrolledTo <= 0 {
		t.Fatalf("could not scroll the output block (scrollTop %.0f)", scrolledTo)
	}
	// Not merely "non-zero": a jump to a small offset would satisfy that
	// while still losing the reader's place. The fixture scrolled to the
	// bottom, so the position afterwards has to still be the bottom, give
	// or take sub-pixel rounding.
	const tolerance = 4
	if afterRefresh < scrolledTo-tolerance {
		t.Fatalf("the refresh moved the log away from where the reader left it: scrollTop was "+
			"%.0f (bottom, max %.0f) before and %.0f after (issue #808)",
			scrolledTo, maxScroll, afterRefresh)
	}
	if afterRefresh < bottomAfter-tolerance {
		t.Fatalf("the reader was at the bottom and is no longer: scrollTop %.0f against a "+
			"post-refresh bottom of %.0f (issue #808)", afterRefresh, bottomAfter)
	}
}
