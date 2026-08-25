// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package middlewares

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPresetLoader_ClientReused is a regression guard for #630.
// It asserts that NewPresetLoader caches a single *http.Client used across
// multiple loadFromURL invocations, instead of constructing a fresh client
// (and therefore a fresh transport / connection pool) on every call.
//
// We observe this by installing a counting TransportFactory before
// NewPresetLoader (the documented ordering constraint), then making two
// loadFromURL calls. The factory must be invoked exactly once because
// the cached client retains the transport produced at construction time.
//
// Not parallel: mutates SetValidateWebhookURLForTest / SetTransportFactoryForTest globals.
func TestPresetLoader_ClientReused(t *testing.T) {
	SetValidateWebhookURLForTest(func(_ string) error { return nil })
	t.Cleanup(func() { SetValidateWebhookURLForTest(ValidateWebhookURLImpl) })

	presetYAML := "name: cached\nurl_scheme: 'https://example.com/{id}'\nbody: '{}'\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(presetYAML))
	}))
	t.Cleanup(server.Close)

	var factoryCalls atomic.Int32
	SetTransportFactoryForTest(func() *http.Transport {
		factoryCalls.Add(1)
		return &http.Transport{}
	})
	t.Cleanup(func() { SetTransportFactoryForTest(NewSafeTransport) })

	// IMPORTANT: factory override must be installed BEFORE NewPresetLoader
	// because the client is constructed (and the transport captured) here.
	gc := &WebhookGlobalConfig{AllowRemotePresets: true}
	loader := NewPresetLoader(gc)

	preset1, err := loader.loadFromURL(server.URL + "/preset.yaml")
	require.NoError(t, err)
	require.NotNil(t, preset1)

	preset2, err := loader.loadFromURL(server.URL + "/preset.yaml")
	require.NoError(t, err)
	require.NotNil(t, preset2)

	assert.Equal(t, int32(1), factoryCalls.Load(),
		"TransportFactory must be invoked exactly once: the *http.Client is cached on the loader")

	// Pointer-identity check: the loader must expose the same *http.Client
	// across calls. This protects against a future refactor that lazily
	// rebuilds the client on each fetch.
	require.NotNil(t, loader.httpClient, "PresetLoader must cache an *http.Client")
	clientPtr := loader.httpClient
	_, err = loader.loadFromURL(server.URL + "/preset.yaml")
	require.NoError(t, err)
	assert.Same(t, clientPtr, loader.httpClient,
		"PresetLoader.httpClient must remain the same instance across loadFromURL calls")
}
