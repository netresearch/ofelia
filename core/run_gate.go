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
// max-runtime. A skipped scheduled fire is not run later. RunJob refuses a
// manual run in that time with ErrJobBusy instead of triggering it, and the
// run-on-startup fire of a job registered in that time is deferred: it is
// fired again once the last stopping run of the name has ended.
//
// The scheduler calls register, retire and restore together with the
// go-cron change they describe, under Scheduler.regMu.
type runGate struct {
	mu      sync.Mutex
	current map[string]*jobWrapper
	running map[string]map[*jobWrapper]int
	// startup holds, per name, the registration whose run-on-startup fire
	// was skipped while a stopping run blocked it.
	startup map[string]deferredStartup
}

// deferredStartup is a run-on-startup fire that enter skipped, with the
// ownBusyCheck it was evaluated with.
type deferredStartup struct {
	w            *jobWrapper
	ownBusyCheck bool
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
//
// ownBusyCheck says that w's job has a per-entry check of its own that
// counts a run of its name as busy and applies its concurrency policy (a job
// with durable schedule state, ADR-005). Such a fire passes a stopping run
// that went through that check too, because the check already counts it;
// a stopping run that did not, such as one of a job that kept no record
// before a reload, still makes it skip.
func (g *runGate) enter(w *jobWrapper, ownBusyCheck bool) gateVerdict {
	g.mu.Lock()
	defer g.mu.Unlock()
	if w.retired {
		return gateRetired
	}
	name := w.j.GetName()
	if g.blockedLocked(name, ownBusyCheck) {
		return gateDraining
	}
	w.keepsRecord = ownBusyCheck
	if g.running == nil {
		g.running = make(map[string]map[*jobWrapper]int)
	}
	if g.running[name] == nil {
		g.running[name] = make(map[*jobWrapper]int)
	}
	g.running[name][w]++
	return gateRun
}

// blocked reports whether a fire of name would be skipped because a stopping
// run of a removed or replaced job of that name still runs; ownBusyCheck as
// for enter.
func (g *runGate) blocked(name string, ownBusyCheck bool) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.blockedLocked(name, ownBusyCheck)
}

func (g *runGate) blockedLocked(name string, ownBusyCheck bool) bool {
	for other, n := range g.running[name] {
		if other.retired && n > 0 && (!ownBusyCheck || !other.keepsRecord) {
			return true
		}
	}
	return false
}

// deferStartup records that the run-on-startup fire of w was skipped by
// enter, so that leave hands it back once the name is no longer blocked.
func (g *runGate) deferStartup(w *jobWrapper, ownBusyCheck bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.startup == nil {
		g.startup = make(map[string]deferredStartup)
	}
	g.startup[w.j.GetName()] = deferredStartup{w: w, ownBusyCheck: ownBusyCheck}
}

// leave ends a run that enter counted. It returns the registration whose
// deferred run-on-startup fire is due now, or nil: one is due when a retired
// run ends, the name no longer blocks that fire and the registration is
// still live.
func (g *runGate) leave(w *jobWrapper) *jobWrapper {
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
	due, ok := g.startup[name]
	if !ok || !w.retired || g.blockedLocked(name, due.ownBusyCheck) {
		return nil
	}
	delete(g.startup, name)
	if due.w.retired || g.current[name] != due.w {
		return nil
	}
	return due.w
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
