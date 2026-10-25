// Copyright (c) 2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import "sync"

// runGate keeps a job's runs from starting beside the run of a job of the
// same name that was removed or replaced.
//
// Removing or replacing a job cancels its run in progress, but the run needs
// time to stop, for example to remove its container. RemoveJob and UpdateJob
// do not wait for that, so the replacement is registered while the old run
// is still stopping. Every registration (a jobWrapper) is counted here while
// it runs; a removed or replaced registration is retired. A retired
// registration starts no new run, and while one of them still runs, no other
// registration of the same name starts one either: such a fire is skipped.
// A skip, not a wait, because a fire holds a slot of the global concurrency
// limit, and a run that ignores its cancellation can go on for its whole
// max-runtime. A skipped fire is not run later; that includes a manual run
// and the run-on-startup fire of a job registered in that time.
//
// The scheduler calls register, retire and restore together with the
// go-cron change they describe, under Scheduler.regMu.
type runGate struct {
	mu      sync.Mutex
	current map[string]*jobWrapper
	running map[string]map[*jobWrapper]int
}

// gateVerdict is what runGate.enter decided for a fire.
type gateVerdict int

const (
	gateRun      gateVerdict = iota // no conflict; the run is counted
	gateRetired                     // the registration was removed or replaced
	gateDraining                    // a retired run of the same name still runs
)

// register records w as the live registration of its job's name.
func (g *runGate) register(w *jobWrapper) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.current == nil {
		g.current = make(map[string]*jobWrapper)
	}
	g.current[w.j.GetName()] = w
}

// retire marks the live registration of name as removed or replaced and
// returns it, or nil if name has none. A run of it that is in progress keeps
// counting until it ends; a fire of it that has not started yet is refused.
func (g *runGate) retire(name string) *jobWrapper {
	g.mu.Lock()
	defer g.mu.Unlock()
	w := g.current[name]
	if w == nil {
		return nil
	}
	w.retired = true
	delete(g.current, name)
	return w
}

// restore undoes retire for a registration that stays live after all, as
// when replacing it failed.
func (g *runGate) restore(w *jobWrapper) {
	if w == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	w.retired = false
	if g.current == nil {
		g.current = make(map[string]*jobWrapper)
	}
	g.current[w.j.GetName()] = w
}

// enter decides whether a fire of w may run, and counts it if so. The caller
// must call leave(w) when a counted run ends.
func (g *runGate) enter(w *jobWrapper) gateVerdict {
	g.mu.Lock()
	defer g.mu.Unlock()
	if w.retired {
		return gateRetired
	}
	name := w.j.GetName()
	for other, n := range g.running[name] {
		if other.retired && n > 0 {
			return gateDraining
		}
	}
	if g.running == nil {
		g.running = make(map[string]map[*jobWrapper]int)
	}
	if g.running[name] == nil {
		g.running[name] = make(map[*jobWrapper]int)
	}
	g.running[name][w]++
	return gateRun
}

// leave ends a run that enter counted.
func (g *runGate) leave(w *jobWrapper) {
	g.mu.Lock()
	defer g.mu.Unlock()
	name := w.j.GetName()
	runs := g.running[name]
	runs[w]--
	if runs[w] <= 0 {
		delete(runs, w)
	}
	if len(runs) == 0 {
		delete(g.running, name)
	}
}

// retiredRunning reports whether a run of j is in progress under a retired
// registration, that is, whether j was removed or replaced and its last run
// is still stopping.
func (g *runGate) retiredRunning(j Job) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	for w := range g.running[j.GetName()] {
		if w.retired && w.j == j {
			return true
		}
	}
	return false
}
