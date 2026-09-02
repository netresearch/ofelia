// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package web

import (
	"net/http"

	"github.com/klauspost/compress/gzhttp"
)

// compressMiddleware compresses responses for clients that advertise a
// codec it supports; clients that advertise none get identity responses
// untouched.
//
// The negotiated codec is zstd or gzip, not gzip alone:
// gzhttp.NewWrapper enables zstd and prefers it over gzip at equal
// q-values, so Chrome, Edge and Firefox — which all send
// "gzip, deflate, br, zstd" — receive zstd, while Safari and anything
// else without zstd falls back to gzip. Both directions are pinned in
// web/compress_test.go.
//
// The edge cases — Accept-Encoding qvalues, Content-Type sniffing of the
// uncompressed bytes, bodiless statuses (204/304), ranged requests,
// writer pooling — are delegated to gzhttp instead of being maintained
// by hand, and so is the size threshold: gzhttp's default 1 KiB. There
// is no previous contract to keep — the merge base compresses nothing —
// and below roughly a packet's worth of payload the framing plus the CPU
// cost buys no fewer bytes on the wire. /live's two-byte "OK" is the
// clearest case.
var compressWrap = func() func(http.Handler) http.HandlerFunc {
	wrapper, err := gzhttp.NewWrapper()
	if err != nil {
		// Static configuration; can only fail on an invalid option.
		panic(err)
	}
	return wrapper
}()

func compressMiddleware(next http.Handler) http.Handler {
	return compressWrap(next)
}
