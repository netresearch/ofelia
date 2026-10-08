//go:build e2e && unix

// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"golang.org/x/crypto/bcrypt"
)

// TestE2E_WebUI_LoginWithAuthEnabled drives the real web UI with
// web-auth-enabled and pins the fix for
// https://github.com/netresearch/ofelia/issues/864.
//
// Before the fix the UI had no login form at all: with auth enabled every
// dashboard poll answered 401 and the page stayed empty for good. The API-level
// auth tests in web/ could not see this, because they never load the UI, and
// the other browser tests run with auth disabled.
func TestE2E_WebUI_LoginWithAuthEnabled(t *testing.T) {
	t.Parallel()

	browserPath := chromeExecutable()
	if browserPath == "" {
		t.Skip("no Chrome/Chromium executable found; skipping browser-driven UI test")
	}

	const (
		username = "admin"
		password = "e2e-login-password"
	)
	// MinCost keeps the test fast; the server only compares against the hash.
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	addr := reserveLoopbackAddr(t)
	configBody := `[global]
  log-level = info
  web-auth-enabled = true
  web-username = ` + username + `
  web-password-hash = ` + string(hash) + `
  web-secret-key = e2e-login-secret-key-at-least-32-characters

[job-local "e2e-web-login"]
  schedule = @every 2s
  command = sh -c "echo OFELIA_E2E_WEB_LOGIN"
`
	configPath := writeConfig(t, configBody)
	daemon := startDaemon(t, configPath, "--enable-web", "--web-address="+addr)
	t.Cleanup(func() { daemon.shutdown(t, 15*time.Second) })

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(),
		append(chromedp.DefaultExecAllocatorOptions[:],
			chromedp.ExecPath(browserPath),
		)...)
	t.Cleanup(cancelAlloc)
	browserCtx, cancelBrowser := chromedp.NewContext(allocCtx)
	t.Cleanup(cancelBrowser)
	ctx, cancelTimeout := context.WithTimeout(browserCtx, 90*time.Second)
	t.Cleanup(cancelTimeout)

	const (
		dialogOpen = `document.getElementById('loginDialog').open`
		jobRow     = `#jobs tbody tr`
	)
	var jobRowsBeforeLogin int
	var errorText, logoutTooltip string

	// Step 1: without a session the login dialog opens and no job data loads.
	err = chromedp.Run(ctx,
		chromedp.Navigate("http://"+addr+"/"),
		chromedp.Poll(dialogOpen, nil, chromedp.WithPollingTimeout(15*time.Second)),
		chromedp.Evaluate(`document.querySelectorAll('`+jobRow+`').length`, &jobRowsBeforeLogin),
	)
	if err != nil {
		t.Fatalf("login dialog did not open without a session (regression of issue #864): %v", err)
	}
	if jobRowsBeforeLogin != 0 {
		t.Fatalf("the job table shows %d rows before sign-in; the API must not have served data", jobRowsBeforeLogin)
	}

	// Step 2: a wrong password keeps the dialog open and says why.
	err = chromedp.Run(ctx,
		chromedp.SendKeys(`#loginUser`, username, chromedp.ByQuery),
		chromedp.SendKeys(`#loginPass`, "wrong-password", chromedp.ByQuery),
		chromedp.Click(`#loginSubmit`, chromedp.ByQuery),
		chromedp.WaitVisible(`#loginError`, chromedp.ByQuery),
		chromedp.Text(`#loginError`, &errorText, chromedp.ByQuery),
	)
	if err != nil {
		t.Fatalf("a failed sign-in showed no error: %v", err)
	}
	if errorText != "Invalid username or password." {
		t.Fatalf("failed sign-in error = %q, want the invalid-credentials message", errorText)
	}

	// Step 3: the right password closes the dialog and the dashboard loads.
	err = chromedp.Run(ctx,
		chromedp.Evaluate(`document.getElementById('loginPass').value = ''`, nil),
		chromedp.SendKeys(`#loginPass`, password, chromedp.ByQuery),
		chromedp.Click(`#loginSubmit`, chromedp.ByQuery),
		chromedp.Poll(`!`+dialogOpen, nil, chromedp.WithPollingTimeout(15*time.Second)),
		chromedp.WaitVisible(jobRow, chromedp.ByQuery),
		chromedp.WaitVisible(`#logoutBtn`, chromedp.ByQuery),
		chromedp.Evaluate(`document.getElementById('logoutBtn').dataset.customTooltip`, &logoutTooltip),
	)
	if err != nil {
		t.Fatalf("sign-in with valid credentials did not load the dashboard: %v", err)
	}
	if logoutTooltip != "Signed in as "+username {
		t.Fatalf("logout button tooltip = %q, want it to name the signed-in user", logoutTooltip)
	}

	// Step 4: the session survives a reload, because the cookie carries it.
	err = chromedp.Run(ctx,
		chromedp.Reload(),
		chromedp.WaitVisible(jobRow, chromedp.ByQuery),
		chromedp.WaitVisible(`#logoutBtn`, chromedp.ByQuery),
	)
	if err != nil {
		t.Fatalf("the session did not survive a page reload: %v", err)
	}

	// Step 5: logging out returns to the dialog, with no job data on screen.
	// Logout reloads the page, which destroys the JS context under any
	// in-page poll. So the old page gets a marker, and the check retries
	// until a page without the marker reports the dialog open.
	err = chromedp.Run(ctx,
		chromedp.Evaluate(`globalThis.beforeLogout = true`, nil),
		chromedp.Click(`#logoutBtn`, chromedp.ByQuery),
	)
	if err != nil {
		t.Fatalf("clicking logout failed: %v", err)
	}
	jobRowsAfterLogout := waitForLoginAfterReload(ctx, t, jobRow)
	if jobRowsAfterLogout != 0 {
		t.Fatalf("the job table still shows %d rows after logout", jobRowsAfterLogout)
	}
}

// waitForLoginAfterReload waits until the page has reloaded and shows the
// login dialog, then returns the number of job rows on that page. An
// evaluation error means the page is mid-navigation, so it retries.
func waitForLoginAfterReload(ctx context.Context, t *testing.T, jobRow string) int {
	t.Helper()
	probe := `(() => {
		if (globalThis.beforeLogout) return -1;
		const d = document.getElementById('loginDialog');
		if (!d || !d.open) return -1;
		return document.querySelectorAll('` + jobRow + `').length;
	})()`
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		var rows int
		if err := chromedp.Run(ctx, chromedp.Evaluate(probe, &rows)); err == nil && rows >= 0 {
			return rows
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("logging out did not return to the login dialog within 15s")
	return 0
}
