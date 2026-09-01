// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package web

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net/http"
	"strconv"
)

// dashboardResponse is the aggregate payload behind /api/dashboard.
//
// WHY THIS ENDPOINT EXISTS: the UI polls its state every 5 seconds. Fetching
// /api/jobs, /api/jobs/disabled, /api/jobs/removed, /api/config (and the
// open job's history) individually cost 4–5 requests per tick — ~60/min per
// browser tab — which exhausted the 100-requests-per-minute rate limit with
// two dashboard tabs open and broke the UI. One aggregate request per tick
// is 12/min per tab, and the sections come from a single moment in time.
//
// The per-resource endpoints stay untouched: they are the documented public
// HTTP API (docs/API.md) that scripts and monitoring consume — a client
// wanting only the job list should not receive the config. This endpoint is
// additive and exists for polling consumers like the bundled UI.
type dashboardResponse struct {
	Jobs     []apiJob `json:"jobs"`
	Disabled []apiJob `json:"disabled"`
	Removed  []apiJob `json:"removed"`
	Config   any      `json:"config"`
	// History carries the runs of the job named in the ?history= query
	// parameter, so an open history view does not need its own poll
	// request. Null when the parameter is absent or names no job — a
	// vanished job must not fail the whole poll. An existing job with an
	// empty history yields [], NOT null: the UI needs to distinguish
	// "history cleared, render the empty state" from "no history
	// requested" (omitempty used to conflate the two, leaving an open
	// modal showing deleted runs forever).
	History []apiExecution `json:"history"`
	// HistoryFingerprint identifies the history of the requested job by
	// the fields that change what the UI renders. The client echoes the
	// last one it received as ?historyFp=; on a match History is omitted,
	// because the client already holds exactly those runs and discards a
	// re-sent copy anyway. Without it a chatty job's full stdout and
	// stderr — up to HistoryLimit × 2 × 10 MB — were serialized and
	// compressed on every 5s tick for as long as the modal stayed open.
	// Empty when no history was requested or the job has none.
	HistoryFingerprint string `json:"historyFingerprint,omitempty"`
}

// historyFingerprint hashes the run fields that decide what the history
// table shows. Output is fingerprinted by length: a completed run's
// output is immutable, so a change in either stream changes its length or
// one of the other fields. This mirrors the identity key the client's own
// history guard already computes.
func historyFingerprint(hist []apiExecution) string {
	h := fnv.New64a()
	for i := range hist {
		e := &hist[i]
		fmt.Fprintf(h, "%d|%d|%t|%t|%s|%d|%d\n",
			e.Date.UnixNano(), e.Duration, e.Failed, e.Skipped, e.Error,
			len(e.Stdout), len(e.Stderr))
	}
	return strconv.FormatUint(h.Sum64(), 36)
}

func (s *Server) dashboardHandler(w http.ResponseWriter, r *http.Request) {
	// One snapshot, not three reads: taken separately, a job disabled or
	// removed between two of them lands in two lists or in none, and the
	// UI renders the lists without deduplicating by name — so it showed
	// as two rows until the next tick healed it.
	active, disabled, removed := s.scheduler.GetJobSnapshot()
	resp := dashboardResponse{
		Jobs:     s.buildAPIJobs(active),
		Disabled: s.buildAPIJobs(disabled),
		Removed:  s.buildAPIJobs(removed),
		Config:   stripJobs(s.config),
	}
	if name := r.URL.Query().Get("history"); name != "" {
		if hist, ok := s.buildAPIHistory(name); ok {
			fp := historyFingerprint(hist)
			resp.HistoryFingerprint = fp
			if r.URL.Query().Get("historyFp") != fp {
				resp.History = hist
			}
		}
	}
	w.Header().Set(headerContentType, contentTypeJSON)
	_ = json.NewEncoder(w).Encode(resp)
}
