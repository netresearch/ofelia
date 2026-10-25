// Copyright (c) 2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/netresearch/go-cron"

	"github.com/netresearch/ofelia/core/persist"
)

// ErrJobBusy is returned by RunJob for a job with durable schedule state
// while a run of it is in progress or waiting for a global slot (ADR-005).
var ErrJobBusy = errors.New("job is busy")

// The values of persist.Occurrence.Trigger and persist.Dispatch.State and
// Outcome.
const (
	triggerScheduled = "scheduled"
	triggerCatchUp   = "catch-up"
	triggerManual    = "manual"

	dispatchWaiting = "waiting"
	dispatchStarted = "started"
	dispatchEnded   = "ended"

	outcomeSuccess  = "success"
	outcomeFailed   = "failed"
	outcomeCanceled = "canceled"
	outcomeUnknown  = "unknown"
)

// defaultOrphanPollInterval is how often the scheduler checks whether a run
// that was in progress when the daemon stopped is still running, unless
// Scheduler.orphanPollInterval sets another interval.
const defaultOrphanPollInterval = 15 * time.Second

// orphanCheckTimeout bounds one such check.
const orphanCheckTimeout = 10 * time.Second

// ScheduleRecordStore keeps the schedule records of ADR-005. persist.Store
// implements it; each PutSchedule is one atomic write of the state file.
type ScheduleRecordStore interface {
	GetSchedule(name string) (persist.Schedule, bool)
	PutSchedule(name string, r persist.Schedule) error
	RemoveSchedules(names ...string) error
	ScheduleNames() []string
}

// SchedulePolicy is how a job's schedule treats missed fires and overlap.
type SchedulePolicy struct {
	// CatchUp runs one missed fire after a downtime (missed_fires: one).
	CatchUp bool
	// Queue keeps one fire that arrives while a run is in progress and runs
	// it afterwards (concurrency: queue). Without it such a fire is skipped.
	Queue bool
	// Revision identifies the schedule: expression, time zone and both
	// policies. A different revision starts the record afresh.
	Revision string
}

// SchedulePolicyProvider is implemented by jobs whose schedule entry may keep
// a record (ADR-005). ok is false for a job that keeps none.
type SchedulePolicyProvider interface {
	SchedulePolicy() (policy SchedulePolicy, ok bool)
}

// WorkloadChecker is implemented by jobs that can tell whether the workload
// of a run, identified by its execution ID, is still running. After a crash
// the scheduler uses it to keep the entry busy while such a run goes on.
type WorkloadChecker interface {
	WorkloadRunning(ctx context.Context, executionID string) (bool, error)
}

// ScheduleRevision returns the revision of a schedule: its expression (which
// carries CRON_TZ for an explicit zone), the scheduler's local zone when it
// names none, and both policies.
func ScheduleRevision(schedule string, catchUp, queue bool) string {
	return scheduleRevision(schedule, catchUp, queue, time.Local)
}

func scheduleRevision(schedule string, catchUp, queue bool, local *time.Location) string {
	zone := "explicit"
	if !hasExplicitZone(schedule) {
		zone = zoneKey(local)
	}
	sum := sha256.Sum256(fmt.Appendf(nil, "%s\x00%s\x00%t\x00%t", schedule, zone, catchUp, queue))
	return hex.EncodeToString(sum[:])[:12]
}

// zoneKey identifies a zone by what it does to a schedule: its name and its
// abbreviation and UTC offset in January and July. The name alone is
// "Local" when the zone comes from /etc/localtime, so a changed host zone
// would otherwise keep the revision.
func zoneKey(loc *time.Location) string {
	jan, janOff := time.Date(2026, time.January, 1, 12, 0, 0, 0, loc).Zone()
	jul, julOff := time.Date(2026, time.July, 1, 12, 0, 0, 0, loc).Zone()
	return fmt.Sprintf("%s/%s%+d/%s%+d", loc.String(), jan, janOff, jul, julOff)
}

func hasExplicitZone(schedule string) bool {
	return len(schedule) > 8 && (schedule[:8] == "CRON_TZ=" || schedule[:3] == "TZ=")
}

// durableEntry is the in-process state of one schedule entry. It is keyed by
// job name and outlives the go-cron entry, so a reload that replaces the job
// still sees a run of the old entry that has not ended.
type durableEntry struct {
	// inFlight is set while a goroutine of this process owns the record's
	// dispatch: waiting for a slot or running.
	inFlight bool
	// drop is closed to make a dispatch that waits for a slot give up.
	drop chan struct{}
	// orphan is the execution ID of a run that was in progress when the
	// daemon stopped and still runs; "" when there is none.
	orphan string
	// stopWatch ends the goroutine that waits for the orphan to end.
	stopWatch func()
}

// SetScheduleStore sets where schedule records are kept. Without one, no job
// keeps a record and every job behaves as before ADR-005.
func (s *Scheduler) SetScheduleStore(store ScheduleRecordStore) {
	s.durableMu.Lock()
	defer s.durableMu.Unlock()
	s.records = store
}

// durablePolicy returns the policy of j when it keeps a record.
func (s *Scheduler) durablePolicy(j Job) (SchedulePolicy, bool) {
	d, ok := j.(SchedulePolicyProvider)
	if !ok {
		return SchedulePolicy{}, false
	}
	p, ok := d.SchedulePolicy()
	if !ok {
		return SchedulePolicy{}, false
	}
	s.durableMu.Lock()
	defer s.durableMu.Unlock()
	return p, s.records != nil
}

// entryLocked returns the in-process state for name. Caller holds durableMu.
func (s *Scheduler) entryLocked(name string) *durableEntry {
	if s.durable == nil {
		s.durable = make(map[string]*durableEntry)
	}
	e, ok := s.durable[name]
	if !ok {
		e = &durableEntry{}
		s.durable[name] = e
	}
	return e
}

func (s *Scheduler) now() time.Time { return s.clock.Now().UTC() }

// busyLocked reports whether the entry counts as busy: a run of it is in
// progress or waits for a slot, in this process or, after a crash, as an
// orphan. Caller holds durableMu.
func busyLocked(rec persist.Schedule, e *durableEntry) bool {
	if e.inFlight || e.orphan != "" {
		return true
	}
	return rec.Dispatch != nil && rec.Dispatch.State == dispatchWaiting
}

// pendingLaunch is an occurrence the scheduler starts through its own
// dispatch path once the go-cron entry is live.
type pendingLaunch struct {
	name string
	info cron.RunInfo
}

func occurrenceInfo(o persist.Occurrence) cron.RunInfo {
	kind := cron.TriggerScheduled
	if o.Trigger == triggerCatchUp {
		kind = cron.TriggerCatchUp
	}
	return cron.RunInfo{ScheduledTime: o.Scheduled, Trigger: kind}
}

// launch starts an occurrence whose dispatch the record already holds in the
// state "waiting". It arrives at the job wrapper with its own scheduled time
// and kind, where admit recognizes it. If go-cron refuses, the dispatch stays
// waiting and the next registration of the entry starts it.
func (s *Scheduler) launch(l *pendingLaunch) {
	if l == nil {
		return
	}
	if err := s.cron.TriggerEntryWithInfoByName(l.name, l.info); err != nil {
		s.Logger.Error("could not start the occurrence; it starts at the next registration",
			"job", l.name, "scheduled", l.info.ScheduledTime, "trigger", l.info.Trigger, "error", err)
	}
}

// prepareDurableEntry compares j's record with its policy when the entry is
// registered (ADR-005, "When a catch-up runs"). It runs before the go-cron
// entry is live, so no fire can see a record it has not evaluated. The
// occurrence it returns, if any, is already recorded as a waiting dispatch;
// the caller starts it with launch once the entry is live.
func (s *Scheduler) prepareDurableEntry(j Job, policy SchedulePolicy, disabled bool) *pendingLaunch {
	name := j.GetName()
	orphan := ""
	if candidate := s.unendedDispatch(name); candidate != "" {
		orphan = s.checkOrphan(context.Background(), j, candidate)
	}

	s.durableMu.Lock()
	defer s.durableMu.Unlock()
	now := s.now()
	rec, ok := s.records.GetSchedule(name)
	e := s.entryLocked(name)
	// The crash rules hold whatever else changed: a run that started before
	// the restart is never repeated, and its container keeps the job busy,
	// also when the job is disabled or its schedule changed.
	if ok {
		s.settleInterruptedLocked(j, &rec, e, orphan)
	}
	if !ok || rec.Revision != policy.Revision {
		fresh := persist.Schedule{Revision: policy.Revision, Activated: now, Cursor: now}
		if ok {
			s.logDropped(name, rec, "the schedule changed")
			if rec.Dispatch != nil && rec.Dispatch.State == dispatchEnded {
				fresh.Dispatch = rec.Dispatch
			}
		}
		s.putLocked(name, fresh)
		return nil
	}
	if disabled {
		s.putLocked(name, rec)
		return nil
	}

	latest := time.Time{}
	if policy.CatchUp {
		latest = latestMissed(j.GetSchedule(), rec.Cursor, now, time.Local)
	}
	launch := s.chooseLaunchLocked(name, &rec, e, policy, latest)
	s.putLocked(name, rec)
	return launch
}

// settleInterruptedLocked applies the crash rules to what the record shows
// from before this process: a started run that never recorded its end counts
// as handled with the outcome unknown, and stays busy while its workload
// (orphan) still runs; a manual run that was still waiting is dropped.
// Caller holds durableMu and writes rec.
func (s *Scheduler) settleInterruptedLocked(j Job, rec *persist.Schedule, e *durableEntry, orphan string) {
	name := j.GetName()
	d := rec.Dispatch
	if d == nil || e.inFlight {
		return
	}
	switch {
	case d.State == dispatchStarted:
		d.State, d.Outcome = dispatchEnded, outcomeUnknown
		s.Logger.Warn("a run was in progress when the daemon stopped; its outcome is unknown and it is not started again",
			"job", name, "scheduled", d.Scheduled, "execution", d.Execution)
		if orphan != "" && e.orphan == "" {
			s.watchOrphanLocked(j, e, orphan)
		}
	case d.State == dispatchWaiting && d.Trigger == triggerManual:
		s.Logger.Warn("dropping a manual run that was still waiting for a slot", "job", name)
		rec.Dispatch = nil
	}
}

// chooseLaunchLocked decides what starts at registration: at most one
// occurrence. A dispatch that waited for a slot and a pending occurrence
// count as the catch-up; only without them a computed catch-up starts.
// While the entry is busy nothing starts now, and the catch-up follows the
// concurrency policy like a fire. Caller holds durableMu.
func (s *Scheduler) chooseLaunchLocked(name string, rec *persist.Schedule, e *durableEntry, policy SchedulePolicy,
	latest time.Time,
) *pendingLaunch {
	if latest.After(rec.Cursor) {
		defer func() { rec.Cursor = latest }()
	}
	if e.inFlight || e.orphan != "" {
		if !latest.After(rec.Cursor) {
			return nil
		}
		catchUp := persist.Occurrence{Scheduled: latest, Trigger: triggerCatchUp}
		s.busyFireLocked(name, rec, policy, catchUp)
		return nil
	}
	switch {
	case rec.Dispatch != nil && rec.Dispatch.State == dispatchWaiting:
		return &pendingLaunch{name: name, info: occurrenceInfo(rec.Dispatch.Occurrence)}
	case rec.Pending != nil:
		occ := *rec.Pending
		rec.Pending = nil
		rec.Dispatch = &persist.Dispatch{Occurrence: occ, State: dispatchWaiting}
		return &pendingLaunch{name: name, info: occurrenceInfo(occ)}
	case latest.After(rec.Cursor):
		occ := persist.Occurrence{Scheduled: latest, Trigger: triggerCatchUp}
		rec.Dispatch = &persist.Dispatch{Occurrence: occ, State: dispatchWaiting}
		s.Logger.Info("catching up a missed fire", "job", name, "scheduled", latest)
		return &pendingLaunch{name: name, info: occurrenceInfo(occ)}
	}
	return nil
}

// busyFireLocked records what happens to a fire while the entry is busy:
// concurrency: queue keeps it in the empty slot, otherwise it is skipped.
// Caller holds durableMu and writes rec afterwards.
func (s *Scheduler) busyFireLocked(name string, rec *persist.Schedule, policy SchedulePolicy, occ persist.Occurrence) {
	switch {
	case policy.Queue && rec.Pending == nil:
		o := occ
		rec.Pending = &o
		s.Logger.Info("a run is in progress; the fire is queued", "job", name, "scheduled", occ.Scheduled)
	case policy.Queue:
		s.Logger.Warn("a run is in progress and a fire is already queued; the fire is dropped",
			"job", name, "scheduled", occ.Scheduled, "queued", rec.Pending.Scheduled)
	default:
		s.Logger.Warn("a run is in progress; the fire is skipped (concurrency: forbid)",
			"job", name, "scheduled", occ.Scheduled)
	}
}

// unendedDispatch returns the execution ID of a run that the record shows as
// started and that this process does not own: a run that was in progress
// when the daemon stopped.
func (s *Scheduler) unendedDispatch(name string) string {
	s.durableMu.Lock()
	defer s.durableMu.Unlock()
	rec, ok := s.records.GetSchedule(name)
	if !ok || rec.Dispatch == nil || rec.Dispatch.State != dispatchStarted {
		return ""
	}
	if s.entryLocked(name).inFlight {
		return ""
	}
	return rec.Dispatch.Execution
}

// checkOrphan returns executionID when j can tell that the run's workload
// still runs, "" otherwise.
func (s *Scheduler) checkOrphan(parent context.Context, j Job, executionID string) string {
	wc, ok := j.(WorkloadChecker)
	if !ok || executionID == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(parent, orphanCheckTimeout)
	defer cancel()
	running, err := wc.WorkloadRunning(ctx, executionID)
	if err != nil {
		// Unknown counts as running: starting a second run beside one that
		// still runs is the failure at-most-once exists to prevent.
		s.Logger.Warn("could not check whether the interrupted run still runs; treating it as running",
			"job", j.GetName(), "execution", executionID, "error", err)
		return executionID
	}
	if running {
		return executionID
	}
	return ""
}

// watchOrphanLocked keeps the entry busy until the orphaned run's workload
// is gone, then starts a queued occurrence. Stop, or pruning the job's
// record, ends the watcher. Caller holds durableMu.
func (s *Scheduler) watchOrphanLocked(j Job, e *durableEntry, executionID string) {
	name := j.GetName()
	e.orphan = executionID
	done := s.watchersDone
	stop := make(chan struct{})
	var once sync.Once
	e.stopWatch = func() { once.Do(func() { close(stop) }) }
	s.Logger.Warn("the interrupted run still runs; the job stays busy until it ends", "job", name, "execution", executionID)
	interval := s.orphanPollInterval
	if interval <= 0 {
		interval = defaultOrphanPollInterval
	}
	go func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() {
			select {
			case <-done:
			case <-stop:
			case <-ctx.Done():
			}
			cancel()
		}()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			if s.checkOrphan(ctx, j, executionID) != "" {
				continue
			}
			s.Logger.Info("the interrupted run has ended", "job", name, "execution", executionID)
			s.launch(s.orphanEnded(name, executionID))
			return
		}
	}()
}

// orphanEnded clears the orphan and, when an occurrence is queued, records it
// as the dispatch to start.
func (s *Scheduler) orphanEnded(name, executionID string) *pendingLaunch {
	s.durableMu.Lock()
	defer s.durableMu.Unlock()
	e := s.entryLocked(name)
	if e.orphan != executionID {
		return nil
	}
	e.orphan = ""
	e.stopWatch = nil
	return s.startPendingLocked(name, e)
}

// startPendingLocked moves a queued occurrence into the dispatch, recorded as
// waiting, when the entry is idle and registered and enabled. Caller holds
// durableMu.
func (s *Scheduler) startPendingLocked(name string, e *durableEntry) *pendingLaunch {
	rec, ok := s.records.GetSchedule(name)
	if !ok || rec.Pending == nil || busyLocked(rec, e) || !s.liveAndEnabled(name) {
		return nil
	}
	occ := *rec.Pending
	rec.Pending = nil
	rec.Dispatch = &persist.Dispatch{Occurrence: occ, State: dispatchWaiting}
	if !s.putLocked(name, rec) {
		return nil
	}
	return &pendingLaunch{name: name, info: occurrenceInfo(occ)}
}

// liveAndEnabled reports whether name has a go-cron entry and is not
// disabled. It asks go-cron, not the job maps: RemoveJob deletes the entry
// before it updates the maps, and a queued occurrence must not be started on
// an entry that is being removed.
func (s *Scheduler) liveAndEnabled(name string) bool {
	if !s.cron.EntryByName(name).Valid() {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, disabled := s.disabledNames[name]
	return !disabled
}

// endOrphanWatchers stops every orphan watcher; called on Stop.
func (s *Scheduler) endOrphanWatchers() {
	s.durableMu.Lock()
	defer s.durableMu.Unlock()
	if !isClosed(s.watchersDone) {
		close(s.watchersDone)
	}
	for _, e := range s.durable {
		e.stopWatch = nil
		e.orphan = ""
	}
}

// renewWatchers gives a scheduler started again after Stop an open channel
// for its orphan watchers.
func (s *Scheduler) renewWatchers() {
	s.durableMu.Lock()
	defer s.durableMu.Unlock()
	if isClosed(s.watchersDone) {
		s.watchersDone = make(chan struct{})
	}
}

func isClosed(ch chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// putLocked writes rec and logs a failure. Caller holds durableMu.
func (s *Scheduler) putLocked(name string, rec persist.Schedule) bool {
	if err := s.records.PutSchedule(name, rec); err != nil {
		s.Logger.Error("could not write the schedule record", "job", name, "error", err)
		return false
	}
	return true
}

// logDropped logs a pending or waiting occurrence that rec loses.
func (s *Scheduler) logDropped(name string, rec persist.Schedule, reason string) {
	if rec.Pending != nil {
		s.Logger.Warn("dropping the queued occurrence: "+reason, "job", name, "scheduled", rec.Pending.Scheduled)
	}
	if rec.Dispatch != nil && rec.Dispatch.State == dispatchWaiting {
		s.Logger.Warn("dropping the occurrence waiting for a slot: "+reason, "job", name, "scheduled", rec.Dispatch.Scheduled)
	}
}

// latestMissed returns the most recent scheduled time t of schedule with
// cursor < t <= now, or the zero time when there is none or the schedule
// does not parse.
//
// A schedule without CRON_TZ is evaluated in the location of the time it is
// given, and the scheduler evaluates it in loc (time.Local), so the search
// starts from the cursor in loc; the record keeps it in UTC.
func latestMissed(schedule string, cursor, now time.Time, loc *time.Location) time.Time {
	sched, err := cron.FullParser().Parse(schedule)
	if err != nil {
		return time.Time{}
	}
	var last time.Time
	for t := sched.Next(cursor.In(loc)); !t.IsZero() && !t.After(now); t = sched.Next(t) {
		last = t
	}
	if last.IsZero() {
		return last
	}
	return last.UTC()
}

// admission is a run the per-entry check admitted and that holds a global
// slot.
type admission struct {
	name    string
	release func()
}

// admit runs the per-entry check of ADR-005 for an arriving invocation of a
// job with durable schedule state, records the result in one atomic write
// and, for an admitted run, takes a global slot, waiting for one if needed.
// It returns nil when the invocation does not run: skipped, queued,
// coalesced, refused, dropped, or canceled while waiting.
func (s *Scheduler) admit(ctx context.Context, j Job, policy SchedulePolicy, executionID string) *admission {
	name := j.GetName()
	info, ok := cron.RunInfoFromContext(ctx)
	if !ok {
		info = cron.RunInfo{ScheduledTime: s.now(), Trigger: cron.TriggerManual}
	}
	t := info.ScheduledTime.UTC()

	s.durableMu.Lock()
	rec, found := s.records.GetSchedule(name)
	if !found {
		now := s.now()
		rec = persist.Schedule{Revision: policy.Revision, Activated: now, Cursor: now}
	}
	e := s.entryLocked(name)
	occ, admitted := s.arrivalLocked(name, &rec, e, policy, info, t)
	if !admitted {
		s.putLocked(name, rec)
		s.durableMu.Unlock()
		return nil
	}

	slot := s.concurrencySem.getChan()
	gotSlot := trySlot(slot)
	d := &persist.Dispatch{Occurrence: occ, State: dispatchWaiting}
	if gotSlot {
		d.State, d.Execution = dispatchStarted, executionID
	}
	rec.Dispatch = d
	if !s.putLocked(name, rec) {
		s.durableMu.Unlock()
		if gotSlot {
			<-slot
		}
		s.Logger.Error("the run is not started: its dispatch could not be recorded", "job", name)
		return nil
	}
	e.inFlight = true
	e.drop = make(chan struct{})
	drop := e.drop
	s.durableMu.Unlock()

	if !gotSlot && !s.waitForSlot(ctx, name, slot, drop, executionID) {
		return nil
	}
	return &admission{name: name, release: func() { <-slot }}
}

// arrivalLocked classifies an arriving invocation and advances the record.
// It returns the occurrence to admit and whether to admit it. Caller holds
// durableMu and writes rec.
func (s *Scheduler) arrivalLocked(name string, rec *persist.Schedule, e *durableEntry, policy SchedulePolicy,
	info cron.RunInfo, t time.Time,
) (persist.Occurrence, bool) {
	// An occurrence the scheduler started itself (launch): the record holds
	// it as a waiting dispatch nobody in this process owns yet.
	if d := rec.Dispatch; d != nil && d.State == dispatchWaiting && !e.inFlight &&
		d.Scheduled.Equal(t) && occurrenceInfo(d.Occurrence).Trigger == info.Trigger {
		return d.Occurrence, true
	}
	switch info.Trigger {
	case cron.TriggerManual, cron.TriggerDependency, cron.TriggerImmediate:
		// A manual run, one started by another job's dependency chain, and
		// the run-on-startup run are no fires of the schedule: they do not
		// move the cursor and are refused while the entry is busy.
		if busyLocked(*rec, e) {
			s.Logger.Warn("a run is in progress; the manual run is refused", "job", name)
			return persist.Occurrence{}, false
		}
		return persist.Occurrence{Scheduled: t, Trigger: triggerManual}, true
	case cron.TriggerScheduled, cron.TriggerCatchUp:
		kind := triggerScheduled
		if info.Trigger == cron.TriggerCatchUp {
			kind = triggerCatchUp
		}
		if !t.After(rec.Cursor) {
			s.Logger.Debug("fire already evaluated; ignoring it", "job", name, "scheduled", t)
			return persist.Occurrence{}, false
		}
		rec.Cursor = t
		occ := persist.Occurrence{Scheduled: t, Trigger: kind}
		if busyLocked(*rec, e) {
			s.busyFireLocked(name, rec, policy, occ)
			return persist.Occurrence{}, false
		}
		return occ, true
	default:
		s.Logger.Error("unknown trigger kind; the run is not started", "job", name, "trigger", info.Trigger)
		return persist.Occurrence{}, false
	}
}

// waitForSlot blocks until a global slot is free, the entry's context ends
// or the dispatch is dropped. It records the run as started when it gets a
// slot and reports whether the run may start.
func (s *Scheduler) waitForSlot(ctx context.Context, name string, slot chan struct{}, drop <-chan struct{},
	executionID string,
) bool {
	s.Logger.Info("the global job limit is reached; the run waits for a slot", "job", name)
	select {
	case slot <- struct{}{}:
	case <-ctx.Done():
		s.abandonWait(name, "the job was removed or the scheduler stopped")
		return false
	case <-drop:
		s.abandonWait(name, "the job was disabled")
		return false
	}
	// The slot may have been granted in the same instant as a cancellation.
	if ctx.Err() != nil {
		<-slot
		s.abandonWait(name, "the job was removed or the scheduler stopped")
		return false
	}

	s.durableMu.Lock()
	defer s.durableMu.Unlock()
	e := s.entryLocked(name)
	rec, ok := s.records.GetSchedule(name)
	if !ok || rec.Dispatch == nil || rec.Dispatch.State != dispatchWaiting {
		// Disabled in the same instant: the dispatch is gone.
		e.inFlight = false
		<-slot
		return false
	}
	rec.Dispatch.State, rec.Dispatch.Execution = dispatchStarted, executionID
	if !s.putLocked(name, rec) {
		e.inFlight = false
		<-slot
		s.Logger.Error("the run is not started: its start could not be recorded", "job", name)
		return false
	}
	return true
}

// abandonWait ends a wait for a slot without starting the run. A scheduled
// or catch-up dispatch stays waiting and starts at the next registration of
// the entry, or at once when a replacement has already been registered; a
// manual one is dropped.
func (s *Scheduler) abandonWait(name, reason string) {
	s.durableMu.Lock()
	s.entryLocked(name).inFlight = false
	rec, ok := s.records.GetSchedule(name)
	if !ok || rec.Dispatch == nil || rec.Dispatch.State != dispatchWaiting {
		s.durableMu.Unlock()
		return
	}
	if rec.Dispatch.Trigger == triggerManual {
		s.Logger.Warn("dropping a manual run that was waiting for a slot: "+reason, "job", name)
		rec.Dispatch = nil
		s.putLocked(name, rec)
		s.durableMu.Unlock()
		return
	}
	s.Logger.Info("the run waiting for a slot did not start: "+reason+"; it starts at the next registration", "job", name)
	launch := s.resumeLocked(name)
	s.durableMu.Unlock()
	s.launch(launch)
}

// resumeAfterRegistration starts what the record holds for an entry that has
// just been registered and that nothing runs: a waiting dispatch or a queued
// occurrence. prepareDurableEntry does that when the entry is idle at the
// time; this covers a run of the replaced entry that ended, or gave up its
// wait for a slot, between that evaluation and the entry going live, after
// which nothing else would start it.
func (s *Scheduler) resumeAfterRegistration(name string) {
	s.durableMu.Lock()
	launch := s.resumeLocked(name)
	s.durableMu.Unlock()
	s.launch(launch)
}

// resumeLocked returns the occurrence to start on an idle, registered and
// enabled entry: the waiting dispatch, else the queued occurrence. Starting
// one twice is harmless: the second arrival has a scheduled time at or before
// the cursor and is ignored. A waiting manual dispatch, which does not move
// the cursor, never gets here: abandonWait and the registration drop it.
// Caller holds durableMu.
func (s *Scheduler) resumeLocked(name string) *pendingLaunch {
	e := s.entryLocked(name)
	// A stopping scheduler starts nothing; the occurrence stays in the record
	// and starts at the next registration.
	if e.inFlight || e.orphan != "" || !s.cron.IsRunning() || !s.liveAndEnabled(name) {
		return nil
	}
	rec, ok := s.records.GetSchedule(name)
	if ok && rec.Dispatch != nil && rec.Dispatch.State == dispatchWaiting {
		return &pendingLaunch{name: name, info: occurrenceInfo(rec.Dispatch.Occurrence)}
	}
	return s.startPendingLocked(name, e)
}

func trySlot(slot chan struct{}) bool {
	select {
	case slot <- struct{}{}:
		return true
	default:
		return false
	}
}

// finishDispatch records the end of an admitted run, releases its slot and
// starts a queued occurrence.
func (s *Scheduler) finishDispatch(a *admission, outcome string) {
	s.durableMu.Lock()
	e := s.entryLocked(a.name)
	if rec, ok := s.records.GetSchedule(a.name); ok && rec.Dispatch != nil && rec.Dispatch.State == dispatchStarted {
		rec.Dispatch.State = dispatchEnded
		rec.Dispatch.Outcome = outcome
		s.putLocked(a.name, rec)
	}
	e.inFlight = false
	a.release()
	launch := s.startPendingLocked(a.name, e)
	s.durableMu.Unlock()
	s.launch(launch)
}

// runOutcome classifies an ended run for its dispatch record.
func runOutcome(entryCtx context.Context, err error, e *Execution) string {
	switch {
	case entryCtx.Err() != nil:
		return outcomeCanceled
	case err != nil || e.Failed || e.Skipped:
		return outcomeFailed
	default:
		return outcomeSuccess
	}
}

// durableBusy reports whether a job with durable schedule state is busy.
func (s *Scheduler) durableBusy(name string) bool {
	s.durableMu.Lock()
	defer s.durableMu.Unlock()
	rec, _ := s.records.GetSchedule(name)
	return busyLocked(rec, s.entryLocked(name))
}

// durableDisabled drops a queued and a waiting occurrence of a job that was
// just disabled.
func (s *Scheduler) durableDisabled(name string) {
	s.durableMu.Lock()
	defer s.durableMu.Unlock()
	rec, ok := s.records.GetSchedule(name)
	if !ok {
		return
	}
	s.logDropped(name, rec, "the job was disabled")
	changed := rec.Pending != nil
	rec.Pending = nil
	if rec.Dispatch != nil && rec.Dispatch.State == dispatchWaiting {
		rec.Dispatch = nil
		changed = true
		if e := s.entryLocked(name); e.drop != nil && e.inFlight {
			close(e.drop)
			e.drop = nil
		}
	}
	if changed {
		s.putLocked(name, rec)
	}
}

// durableEnabled moves the cursor of a job that was just enabled to now, so
// fires during the disabled period never count as missed.
func (s *Scheduler) durableEnabled(name string) {
	s.durableMu.Lock()
	defer s.durableMu.Unlock()
	rec, ok := s.records.GetSchedule(name)
	if !ok {
		return
	}
	rec.Cursor = s.now()
	s.putLocked(name, rec)
}

// PruneScheduleRecords removes the records of schedule entries for which
// keep returns false, and stops their orphan watchers.
func (s *Scheduler) PruneScheduleRecords(keep func(name string) bool) {
	s.durableMu.Lock()
	defer s.durableMu.Unlock()
	if s.records == nil {
		return
	}
	var gone []string
	for _, name := range s.records.ScheduleNames() {
		if keep(name) {
			continue
		}
		gone = append(gone, name)
		if e, ok := s.durable[name]; ok {
			if e.stopWatch != nil {
				e.stopWatch()
			}
			if !e.inFlight {
				delete(s.durable, name)
			}
		}
	}
	if len(gone) == 0 {
		return
	}
	if err := s.records.RemoveSchedules(gone...); err != nil {
		s.Logger.Error("could not remove schedule records of removed jobs", "jobs", gone, "error", err)
		return
	}
	s.Logger.Info("removed the schedule records of jobs that no longer exist", "jobs", gone)
}
