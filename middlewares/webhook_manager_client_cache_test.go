// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package middlewares

import (
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWebhookManager_SharesHTTPClientAcrossReconciles pins the fix for
// https://github.com/netresearch/ofelia/issues/674: WebhookManager
// caches *http.Client per webhook Timeout so cli.Config.rebuildAll-
// Middlewares (called after every Docker label change or INI reload)
// reuses the underlying transport's keep-alive connection pool.
//
// Pre-fix, NewWebhook built a fresh *http.Client and *http.Transport
// per call inside GetMiddlewares, so every reconcile dropped any
// keep-alive connections and started fresh TCP/TLS handshakes for
// every job's webhooks. The cache is on the manager (single instance
// per parsed Config), so two GetMiddlewares calls in sequence return
// webhooks whose Client field points at the same *http.Client.
func TestWebhookManager_SharesHTTPClientAcrossReconciles(t *testing.T) {
	t.Parallel()

	mgr := NewWebhookManager(DefaultWebhookGlobalConfig())
	require.NoError(t, mgr.Register(&WebhookConfig{
		Name:    "wh1",
		Preset:  "slack",
		ID:      "T12345/B67890",
		Secret:  "xoxb-test",
		Timeout: 5 * time.Second,
	}))

	// First reconcile.
	mws1, err := mgr.GetMiddlewares([]string{"wh1"})
	require.NoError(t, err)
	require.Len(t, mws1, 1)
	c1 := mws1[0].(*Webhook).Client

	// Second reconcile — must reuse the same *http.Client pointer.
	mws2, err := mgr.GetMiddlewares([]string{"wh1"})
	require.NoError(t, err)
	require.Len(t, mws2, 1)
	c2 := mws2[0].(*Webhook).Client

	assert.Same(t, c1, c2,
		"WebhookManager must reuse the cached *http.Client across reconciles so the transport's keep-alive connection pool survives rebuildAllMiddlewares (#674)")
	// Belt-and-braces: the headline #674 benefit is the SHARED
	// *http.Transport, not just the *http.Client wrapper. Same-pointer
	// transport is what gives operators the keep-alive pool reuse.
	require.NotNil(t, c1.Transport, "cached client must have a non-nil Transport (TransportFactory output)")
	assert.Same(t, c1.Transport, c2.Transport,
		"the underlying *http.Transport (and thus its keep-alive pool) must be the same instance — otherwise the cache helps nothing")
}

// TestWebhookManager_DifferentTimeoutsGetDifferentClients pins the
// cache key contract: clients are keyed by Timeout because that's the
// only per-webhook input that varies; two webhooks with different
// timeouts must NOT share a client (their *http.Client.Timeout would
// otherwise mismatch the operator's intent).
func TestWebhookManager_DifferentTimeoutsGetDifferentClients(t *testing.T) {
	t.Parallel()

	mgr := NewWebhookManager(DefaultWebhookGlobalConfig())
	require.NoError(t, mgr.Register(&WebhookConfig{
		Name: "fast", Preset: "slack", ID: "T1/B1", Secret: "s1",
		Timeout: 1 * time.Second,
	}))
	require.NoError(t, mgr.Register(&WebhookConfig{
		Name: "slow", Preset: "slack", ID: "T1/B2", Secret: "s2",
		Timeout: 30 * time.Second,
	}))

	mws, err := mgr.GetMiddlewares([]string{"fast", "slow"})
	require.NoError(t, err)
	require.Len(t, mws, 2)

	cFast := mws[0].(*Webhook).Client
	cSlow := mws[1].(*Webhook).Client

	assert.NotSame(t, cFast, cSlow,
		"webhooks with different Timeout must NOT share a *http.Client — their Timeout field would mismatch the operator's intent")
	assert.Equal(t, 1*time.Second, cFast.Timeout)
	assert.Equal(t, 30*time.Second, cSlow.Timeout)
}

// TestWebhookManager_SameTimeoutAcrossWebhooksSharesClient pins the
// inverse: two distinct webhooks (different Config.Name) with the same
// Timeout share a *http.Client — the keep-alive pool serves all jobs
// pointed at the same endpoint family.
func TestWebhookManager_SameTimeoutAcrossWebhooksSharesClient(t *testing.T) {
	t.Parallel()

	mgr := NewWebhookManager(DefaultWebhookGlobalConfig())
	require.NoError(t, mgr.Register(&WebhookConfig{
		Name: "a", Preset: "slack", ID: "T1/B1", Secret: "s1",
		Timeout: 10 * time.Second,
	}))
	require.NoError(t, mgr.Register(&WebhookConfig{
		Name: "b", Preset: "slack", ID: "T1/B2", Secret: "s2",
		Timeout: 10 * time.Second,
	}))

	mws, err := mgr.GetMiddlewares([]string{"a", "b"})
	require.NoError(t, err)
	require.Len(t, mws, 2)

	assert.Same(t, mws[0].(*Webhook).Client, mws[1].(*Webhook).Client,
		"webhooks with the same Timeout must share a single *http.Client so the underlying transport's keep-alive pool is reused")
}

// TestWebhookManager_ConcurrentCacheReadsRaceFree pins the concurrency
// contract for the new cache (sync.Mutex around httpClients). Without
// the lock, parallel GetMiddlewares calls could race on the map and
// produce panics under -race; this test fails fast in `go test -race`
// mode if a future refactor drops the lock.
//
// Each goroutine registers and resolves a UNIQUE webhook (distinct
// Config.Name + distinct *WebhookConfig pointer) so the ApplyDefaults
// mutation of WebhookConfig fields is also race-free — the test
// exercises the manager's httpClients cache contention specifically,
// not the pre-existing ApplyDefaults-on-shared-config concern that
// production avoids by processing webhooks sequentially.
func TestWebhookManager_ConcurrentCacheReadsRaceFree(t *testing.T) {
	t.Parallel()

	mgr := NewWebhookManager(DefaultWebhookGlobalConfig())

	const goroutines = 16
	// Register all configs up front so the parallel goroutines only
	// exercise GetMiddlewares (and thereby getOrBuildClient).
	names := make([]string, goroutines)
	for i := range goroutines {
		// Two distinct timeouts so both the cache-hit and cache-miss
		// paths run concurrently across the goroutine pool.
		name := "wh-" + strconv.Itoa(i)
		names[i] = name
		require.NoError(t, mgr.Register(&WebhookConfig{
			Name: name, Preset: "slack", ID: "T1/B" + strconv.Itoa(i), Secret: "s",
			Timeout: time.Duration(1+i%2) * time.Second,
		}))
	}

	type result struct {
		client *http.Client
		err    error
		gotLen int
	}
	var wg sync.WaitGroup
	results := make([]result, goroutines)
	for i := range goroutines {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			mws, err := mgr.GetMiddlewares([]string{names[idx]})
			results[idx].err = err
			results[idx].gotLen = len(mws)
			if len(mws) == 1 {
				results[idx].client = mws[0].(*Webhook).Client
			}
		}(i)
	}
	wg.Wait()

	// Assert on the main goroutine — testifylint's go-require check
	// requires this so FailNow's runtime.Goexit doesn't strand the
	// other test goroutines.
	distinct := map[*http.Client]struct{}{}
	for i := range goroutines {
		require.NoError(t, results[i].err, "goroutine %d", i)
		require.Equal(t, 1, results[i].gotLen, "goroutine %d", i)
		require.NotNil(t, results[i].client, "goroutine %d got nil client", i)
		distinct[results[i].client] = struct{}{}
	}
	assert.Lenf(t, distinct, 2,
		"with 2 distinct timeouts the cache must yield exactly 2 client instances across 16 concurrent reconciles; got %d", len(distinct))
}

// TestWebhookManager_AdoptClientCacheFrom_PreservesAcrossReload pins
// the most important half of the #674 fix: when WebhookConfigs.Init-
// Manager replaces the manager on a Docker label sync or INI live-
// reload, the new manager adopts the prior manager's *http.Client
// cache so the keep-alive connection pools survive. Without this
// carry-forward, every reload would discard the cache and the
// reviewer's flagged-as-critical concern on PR #700 would defeat
// the headline benefit.
func TestWebhookManager_AdoptClientCacheFrom_PreservesAcrossReload(t *testing.T) {
	t.Parallel()

	mgr1 := NewWebhookManager(DefaultWebhookGlobalConfig())
	require.NoError(t, mgr1.Register(&WebhookConfig{
		Name: "wh", Preset: "slack", ID: "T1/B1", Secret: "s",
		Timeout: 7 * time.Second,
	}))
	mws1, err := mgr1.GetMiddlewares([]string{"wh"})
	require.NoError(t, err)
	require.Len(t, mws1, 1)
	originalClient := mws1[0].(*Webhook).Client

	// Simulate a reload: build a fresh manager (the production path
	// constructs a brand-new NewWebhookManager in InitManager) and
	// adopt the prior cache before re-registering configs.
	mgr2 := NewWebhookManager(DefaultWebhookGlobalConfig())
	mgr2.AdoptClientCacheFrom(mgr1)
	require.NoError(t, mgr2.Register(&WebhookConfig{
		Name: "wh", Preset: "slack", ID: "T1/B1", Secret: "s",
		Timeout: 7 * time.Second,
	}))
	mws2, err := mgr2.GetMiddlewares([]string{"wh"})
	require.NoError(t, err)
	require.Len(t, mws2, 1)
	preservedClient := mws2[0].(*Webhook).Client

	assert.Same(t, originalClient, preservedClient,
		"AdoptClientCacheFrom must carry the *http.Client across the manager swap so keep-alive pools survive Docker-label-sync / INI-reload reconciles (#674)")
	require.NotNil(t, preservedClient.Transport)
	assert.Same(t, originalClient.Transport, preservedClient.Transport,
		"the underlying transport (and its connection pool) is what gives operators the keep-alive benefit — verify it's the same pointer across the reload")
}

// TestWebhookManager_AdoptClientCacheFrom_NilPriorIsNoOp pins the
// first-init contract: when there is no prior manager (the very first
// InitManager call), AdoptClientCacheFrom must be a safe no-op.
func TestWebhookManager_AdoptClientCacheFrom_NilPriorIsNoOp(t *testing.T) {
	t.Parallel()

	mgr := NewWebhookManager(DefaultWebhookGlobalConfig())
	mgr.AdoptClientCacheFrom(nil) // must not panic
	require.NoError(t, mgr.Register(&WebhookConfig{
		Name: "wh", Preset: "slack", ID: "T1/B1", Secret: "s",
		Timeout: 5 * time.Second,
	}))
	mws, err := mgr.GetMiddlewares([]string{"wh"})
	require.NoError(t, err)
	require.Len(t, mws, 1)
	assert.NotNil(t, mws[0].(*Webhook).Client.Transport,
		"first-init via nil-prior must still build a client on demand")
}

// TestNewWebhook_StandaloneStillBuildsOwnClient confirms backward
// compatibility for direct callers of NewWebhook (tests, third-party
// code): without a manager, the legacy "fresh client per call" behavior
// is preserved. Two NewWebhook calls return distinct clients.
func TestNewWebhook_StandaloneStillBuildsOwnClient(t *testing.T) {
	t.Parallel()

	loader := NewPresetLoader(nil)
	cfg := &WebhookConfig{
		Name: "wh", Preset: "slack", ID: "T1/B1", Secret: "s1",
		Timeout: 5 * time.Second,
	}

	m1, err := NewWebhook(cfg, loader)
	require.NoError(t, err)
	m2, err := NewWebhook(cfg, loader)
	require.NoError(t, err)

	assert.NotSame(t, m1.(*Webhook).Client, m2.(*Webhook).Client,
		"standalone NewWebhook must keep building a fresh *http.Client per call (legacy behavior preserved for direct callers / tests)")
}
