//go:build e2e && unix
// +build e2e,unix

// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package e2e

import (
	"context"
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
	browserPath := chromeExecutable()
	if browserPath == "" {
		t.Skip("no Chrome/Chromium executable found")
	}

	addr := reserveLoopbackAddr(t)
	configBody := `
[job-local "chatty"]
  schedule = @every 3s
  command = sh -c 'seq 1 300 | sed "s/^/log line /"'
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
		openOutput    = `#history tbody tr.output-row.open pre`
	)

	var scrolledTo, maxScroll, afterRefresh float64
	if err := chromedp.Run(ctx,
		chromedp.Navigate("http://"+addr+"/"),
		chromedp.WaitVisible(jobRow, chromedp.ByQuery),
		chromedp.Click(jobRow, chromedp.ByQuery),
		chromedp.WaitVisible(historyToggle, chromedp.ByQuery),

		// Expand a run's output, then scroll it to the bottom the way the
		// reporter did.
		chromedp.Evaluate(`document.querySelector('`+historyToggle+`').click(); true`, nil),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Evaluate(`(() => {
			const pre = document.querySelector('`+openOutput+`');
			if (!pre) return -1;
			pre.scrollTop = pre.scrollHeight;
			return pre.scrollTop;
		})()`, &scrolledTo),
		chromedp.Evaluate(`(() => {
			const pre = document.querySelector('`+openOutput+`');
			return pre ? pre.scrollHeight - pre.clientHeight : -1;
		})()`, &maxScroll),

		// Outlast a full refresh cycle plus another run of the job, so the
		// history genuinely changes and the table is genuinely rebuilt.
		chromedp.Sleep(uiRefreshInterval+4*time.Second),
		chromedp.Evaluate(`(() => {
			const pre = document.querySelector('`+openOutput+`');
			return pre ? pre.scrollTop : -1;
		})()`, &afterRefresh),
	); err != nil {
		t.Fatalf("driving the web UI failed: %v", err)
	}

	if maxScroll <= 0 {
		t.Fatalf("the output block did not overflow (max scroll %.0f), so this test could not "+
			"observe scrolling at all — the fixture log is too short for the block height", maxScroll)
	}
	if scrolledTo <= 0 {
		t.Fatalf("could not scroll the output block (scrollTop %.0f)", scrolledTo)
	}
	if afterRefresh <= 0 {
		t.Fatalf("the refresh reset the log to the top: scrollTop was %.0f before and %.0f after "+
			"(issue #808)", scrolledTo, afterRefresh)
	}
}
