//go:build e2e && unix
// +build e2e,unix

// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package e2e

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// uiRefreshInterval mirrors the `setInterval(refresh, 5000)` in
// static/ui/index.html. The test has to outlast one full cycle to prove the
// refresh does not clobber user state, so it is stated once here rather than
// buried as a magic number in the waits below.
const uiRefreshInterval = 5 * time.Second

// TestE2E_WebUI_ExpandedOutputSurvivesRefresh drives the real web UI in a real
// browser and pins the fix for https://github.com/netresearch/ofelia/issues/764.
//
// The UI polls the history endpoint every 5s and re-renders the table by
// replacing the tbody's innerHTML. Before the fix the `<details>` element that
// wraps a run's output was re-created without its `open` attribute, so an
// output the user had expanded collapsed on its own within five seconds.
//
// This is deliberately a browser test and not an assertion on the served HTML:
// the bug lives in what the refresh does to DOM state after the user has
// interacted with it, which no amount of markup inspection can observe.
func TestE2E_WebUI_ExpandedOutputSurvivesRefresh(t *testing.T) {
	t.Parallel()

	browserPath := chromeExecutable()
	if browserPath == "" {
		t.Skip("no Chrome/Chromium executable found; skipping browser-driven UI test")
	}

	addr := reserveLoopbackAddr(t)

	// Cadence is a deliberate trade-off against history eviction. A job keeps
	// the last `HistoryLimit` runs (default 10) and drops the OLDEST first, so
	// at @every 1s an entry is evicted 10s after it appears - faster than this
	// test's wait on a loaded runner, which would make a legitimately dropped
	// row look like the collapse bug. At 2s the entry we expand needs ~20s to
	// reach the cut, while new runs still arrive during the wait so the
	// re-render is genuinely exercised.
	configBody := `[global]
  log-level = info

[job-local "e2e-web-output"]
  schedule = @every 2s
  command = sh -c "echo OFELIA_E2E_WEB_OUTPUT"
`
	configPath := writeConfig(t, configBody)
	daemon := startDaemon(t, configPath, "--enable-web", "--web-address="+addr)
	t.Cleanup(func() { daemon.shutdown(t, 15*time.Second) })

	// Wait until the job has actually run, otherwise the history table renders
	// the "No history yet." placeholder and there is nothing to expand.
	if err := daemon.waitForLog(`Job \"e2e-web-output\"`, 15*time.Second); err != nil {
		t.Fatalf("job did not run before the UI check: %v\nstdout=%s", err, daemon.stdout.String())
	}

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(),
		append(chromedp.DefaultExecAllocatorOptions[:],
			chromedp.ExecPath(browserPath),
		)...)
	t.Cleanup(cancelAlloc)

	// The budget covers browser startup plus the refresh cycle the test waits
	// out; without it a wedged browser would hang until the package timeout.
	browserCtx, cancelBrowser := chromedp.NewContext(allocCtx)
	t.Cleanup(cancelBrowser)
	ctx, cancelTimeout := context.WithTimeout(browserCtx, 90*time.Second)
	t.Cleanup(cancelTimeout)

	const (
		jobRow         = `#jobs tbody tr`
		historyDetails = `#history tbody details`
		historySummary = `#history tbody details summary`
	)

	// The history table renders oldest run first, so the LAST output is the
	// newest one - and the newest is the furthest away from eviction. Clicking
	// the first would expand the entry that is about to be dropped.
	const (
		newestDetails = `document.querySelector('#history tbody details:last-of-type')`
		newestSummary = `#history tbody tr:last-child details summary`
	)

	var openAfterClick, openAfterRefresh, stillPresent bool
	var keyAfterClick, keyAfterRefresh string

	err := chromedp.Run(ctx,
		chromedp.Navigate("http://"+addr+"/"),

		// Selecting the job opens the history panel and loads its runs.
		chromedp.WaitVisible(jobRow, chromedp.ByQuery),
		chromedp.Click(jobRow, chromedp.ByQuery),

		// Expand the newest run's output.
		chromedp.WaitVisible(historySummary, chromedp.ByQuery),
		chromedp.Click(newestSummary, chromedp.ByQuery),

		// Establish the precondition: it really is open, and note which
		// execution it belongs to so we can find the same one afterwards.
		chromedp.Evaluate(newestDetails+`.open`, &openAfterClick),
		chromedp.Evaluate(newestDetails+`.dataset.key || ''`, &keyAfterClick),
	)
	if err != nil {
		t.Fatalf("driving the web UI failed: %v", err)
	}
	if !openAfterClick {
		t.Fatalf("output did not expand on click; the test cannot observe the refresh behavior")
	}

	// Outlast a full refresh cycle. The daemon keeps firing the job, so this
	// also covers the harder case: new runs arrive and the expanded row is no
	// longer the newest one.
	//
	// Presence is read separately from openness. Without that split, a row that
	// legitimately aged out of the capped history is indistinguishable from one
	// the refresh collapsed - the two need different fixes, and conflating them
	// once already sent this test chasing the wrong bug.
	err = chromedp.Run(ctx,
		chromedp.Sleep(uiRefreshInterval+2*time.Second),
		chromedp.Evaluate(selectorForKey(keyAfterClick, historyDetails)+` !== null`, &stillPresent),
		chromedp.Evaluate(selectorForKey(keyAfterClick, historyDetails)+`?.open === true`, &openAfterRefresh),
		chromedp.Evaluate(`document.querySelector('#history tbody details:last-of-type')?.dataset.key || ''`, &keyAfterRefresh),
	)
	if err != nil {
		t.Fatalf("re-reading the expanded output after a refresh failed: %v", err)
	}

	if !stillPresent {
		t.Fatalf("execution %q dropped out of the history table within %s, so the test could not "+
			"observe the refresh behavior — the job cadence is outrunning the history limit, "+
			"not a regression of issue #764", keyAfterClick, uiRefreshInterval+2*time.Second)
	}

	if !openAfterRefresh {
		t.Fatalf("expanded job output collapsed on its own across the %s refresh cycle "+
			"(execution %q is still listed, just no longer open); regression of issue #764",
			uiRefreshInterval, keyAfterClick)
	}

	// Not an assertion, just context: whether the row we kept open was still
	// the newest one tells us which variant was exercised.
	if keyAfterRefresh != keyAfterClick {
		t.Logf("history advanced during the test (newest run is now %q, kept %q open) — "+
			"the expanded output survived a re-render that also reordered rows",
			keyAfterRefresh, keyAfterClick)
	}
}

// selectorForKey builds a JS expression that finds the <details> belonging to
// one specific execution. Falls back to the first output when the build under
// test does not key its rows, so the test reports "collapsed" rather than
// dying on a JS error.
func selectorForKey(key, fallbackSelector string) string {
	if key == "" {
		return `document.querySelector('` + fallbackSelector + `')`
	}
	return fmt.Sprintf(`document.querySelector('#history tbody details[data-key="%s"]')`, key)
}

// reserveLoopbackAddr asks the kernel for a free loopback port and returns it
// as host:port. The listener is closed before returning, so there is a small
// race window — acceptable here, and far better than a hardcoded port that
// would make parallel e2e runs collide.
func reserveLoopbackAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve loopback port: %v", err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("close port reservation: %v", err)
	}
	return addr
}

// chromeExecutable returns the path to a usable Chrome/Chromium binary, or ""
// when none is installed. Mirrors dockerAvailable's skip-cleanly contract: a
// developer machine without a browser should not fail the suite, while CI
// runners (ubuntu-latest ships Chrome) exercise the test for real.
//
// OFELIA_E2E_CHROME overrides the search for developers whose browser is not on
// PATH — e.g. one managed by a browser-automation toolchain in a cache dir.
func chromeExecutable() string {
	if fromEnv := os.Getenv("OFELIA_E2E_CHROME"); fromEnv != "" {
		if _, err := os.Stat(fromEnv); err == nil {
			return fromEnv
		}
	}
	for _, candidate := range []string{
		"google-chrome",
		"google-chrome-stable",
		"chromium",
		"chromium-browser",
	} {
		if path, err := exec.LookPath(candidate); err == nil {
			return path
		}
	}
	return ""
}
