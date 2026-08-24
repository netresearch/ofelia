// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package web

import (
	"encoding/json"
	"net/http"
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
}

func (s *Server) dashboardHandler(w http.ResponseWriter, r *http.Request) {
	resp := dashboardResponse{
		Jobs:     s.buildAPIJobs(s.scheduler.GetActiveJobs()),
		Disabled: s.buildAPIJobs(s.scheduler.GetDisabledJobs()),
		Removed:  s.buildAPIJobs(s.scheduler.GetRemovedJobs()),
		Config:   stripJobs(s.config),
	}
	if name := r.URL.Query().Get("history"); name != "" {
		if hist, ok := s.buildAPIHistory(name); ok {
			resp.History = hist
		}
	}
	w.Header().Set(headerContentType, contentTypeJSON)
	_ = json.NewEncoder(w).Encode(resp)
}
