/* ── Stored preferences ──
   A browser with site data blocked (privacy setting, hardened profile,
   some private windows) throws on the localStorage property itself, so
   even a read throws. These calls sit at the top level of the script:
   an unguarded throw would abort app.js before the first render and
   leave the dashboard blank, instead of costing only the remembered
   theme, density and timezone. Both helpers degrade to "nothing
   stored", which is the path a first-time visitor takes anyway. */
function storedPref(key) {
  try {
    return localStorage.getItem(key);
  } catch (e) {
    return null;
  }
}

function storePref(key, value) {
  try {
    localStorage.setItem(key, value);
  } catch (e) {
    /* preference stays for this page view only */
  }
}

/* ── Tabs ── */
document.querySelectorAll('.tabs button').forEach(btn => {
  btn.addEventListener('click', () => {
    document.querySelectorAll('.tabs button').forEach(b => { b.classList.remove('active'); b.setAttribute('aria-selected','false'); });
    document.querySelectorAll('.tab-panel').forEach(p => p.classList.remove('active'));
    btn.classList.add('active');
    btn.setAttribute('aria-selected','true');
    document.getElementById('tab-' + btn.dataset.tab).classList.add('active');
  });
});

function switchTab(name) {
  document.querySelector(`.tabs button[data-tab="${name}"]`).click();
}

/* ── Helpers ── */
function formatDuration(ns) {
  if (ns === null || ns === undefined || ns === '') return '';
  ns = Number(ns);
  if (ns >= 3600e9) { const h = Math.floor(ns / 3600e9); const m = Math.floor((ns % 3600e9) / 60e9); return `${h}:${String(m).padStart(2,'0')}h`; }
  if (ns >= 1e9) return `${(ns / 1e9).toFixed(1)}s`;
  if (ns >= 1e6) return `${(ns / 1e6).toFixed(1)}ms`;
  if (ns >= 1e3) return `${(ns / 1e3).toFixed(0)}µs`;
  return `${ns}ns`;
}

/* Timezone preference and date parse shared by every timestamp
   formatter, so the preference lookup and validity check live once.
   The preference is cached in a variable (updated by the select's
   change listener): tzParse runs per table cell per render, and a
   localStorage read on every call is a synchronous blocking API.
   The stored value is validated against the select's own options — the
   template is the single source of truth for the valid set, so adding
   or renaming an option there can never drift apart from a hand-written
   list here. A value written by another app version (or a manual edit)
   would otherwise leave the select blank (no matching option) while
   formatting silently fell back to local time. */
const tzSelect = document.getElementById('timezoneSelect');
const TZ_PREFS = Array.from(tzSelect.options, o => o.value);
// Single validate-or-fallback used by both the startup read and
// adoptTzPref, so startup and runtime adoption can never disagree
// about the same stored value. Unknown values fall back to the first
// option — the default choice, currently 'local'.
const validTzPref = (v) => TZ_PREFS.includes(v) ? v : TZ_PREFS[0];
let tzPref = validTzPref(storedPref('timezone'));

/* Epoch ms for sort keys and next-run comparisons — never the raw
   RFC3339 string: the server emits local-zone offsets, and
   lexicographic order breaks across offset changes (DST). Unparseable
   input maps to -Infinity, not NaN: a NaN key makes every comparison
   false, which is an inconsistent comparator — Array.prototype.sort can
   then leave unrelated rows out of order, not just the bad one. (The
   server emits nanosecond fractions; engines that parse only
   millisecond precision are exactly how NaN gets here.) Parsing and the
   validity check are delegated to tzParse so sorting and display can
   never disagree about which timestamps are valid. */
function epochMs(dateStr) {
  const { dt, valid } = tzParse(dateStr);
  return valid ? dt.getTime() : -Infinity;
}

function tzParse(dateStr) {
  const str = String(dateStr);
  const dt = new Date(str);
  return { pref: tzPref, str, dt, valid: !Number.isNaN(dt.getTime()) };
}

/* Single-line "date time", composed from formatTimeParts (hoisted, see
   the stat-card section) so the per-preference branching lives exactly
   once — the two formatters had already drifted apart once (milliseconds
   and offsets shown in one table but not the other). */
function formatTime(dateStr) {
  if (dateStr === null || dateStr === undefined) return '';
  const parts = formatTimeParts(dateStr);
  return parts ? `${parts.date} ${parts.time}` : '';
}

function stripControlChars(s) {
  if (!s) return '';
  // Strip stray ASCII control characters from log output. Docker's
  // stdout/stderr stream demuxing happens server-side; this only cleans
  // what remains (escape sequences, bells) so the modal stays readable.
  return s.replace(/[\x00-\x08\x0e-\x1f]/g, '');
}

function escapeHtml(str) {
  // Only null/undefined become '': false and 0 are real config values
  // and must render as "false"/"0", not as an empty cell.
  if (str === null || str === undefined) return '';
  return String(str).replaceAll('&', '&amp;').replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;').replaceAll('"', '&quot;').replaceAll("'", '&#39;');
}

/* Every status dot goes through this one helper so a markup change
   (class, attribute, aria) lands everywhere at once. */
function dotSpan(state, tip) {
  // tip is escaped here (not at call sites) so a future caller passing
  // server-derived text cannot become an injection point.
  return `<span class="dot dot-${state}" data-custom-tooltip="${escapeHtml(tip)}"></span>`;
}
function statusDot(failed, skipped) {
  if (failed) return dotSpan('fail', 'Failed');
  if (skipped) return dotSpan('skip', 'Skipped');
  return dotSpan('ok', 'Success');
}

/* ── Tooltip ── */
/* One shared bubble for every [data-custom-tooltip] element, delegated so rows
   re-rendered by the 5s poll need no re-binding. position: fixed uses
   viewport coordinates, so the bubble is immune to overflow-clipping
   containers (the native title and CSS-only tooltips are not). While a
   <dialog> is open it sits in the browser's top layer above body
   children — the bubble is re-parented into the dialog when its anchor
   lives there. */
(() => {
  const el = document.createElement('div');
  el.id = 'custom-tooltip';
  // role + aria-describedby (set on the anchor while the bubble is up)
  // is what makes the text reachable at all: the reason an edit or
  // delete button is inert ("Managed by the INI config") lived only in
  // data-custom-tooltip, which no screen reader reads.
  el.setAttribute('role', 'tooltip');
  document.body.appendChild(el);
  let anchor = null;

  function show(target) {
    if (anchor && anchor !== target) anchor.removeAttribute('aria-describedby');
    anchor = target;
    target.setAttribute('aria-describedby', el.id);
    // Watch for the anchor being destroyed only while a bubble is
    // visible — a permanent body-wide observer would run on every DOM
    // churn of the 5s poll for a tooltip that is rarely open. Calling
    // observe() again for the same node is a spec-guaranteed no-op
    // (the existing registration is reused), so no guard is needed.
    orphanWatch.observe(document.body, { childList: true, subtree: true });
    const host = target.closest('dialog[open]') || document.body;
    if (el.parentElement !== host) host.appendChild(el);
    el.textContent = target.dataset.customTooltip;
    el.classList.add('visible');
    const r = target.getBoundingClientRect();
    el.style.left = '0px';
    el.style.top = '0px';
    const w = el.offsetWidth;
    const h = el.offsetHeight;
    const x = Math.max(4, Math.min(r.left + r.width / 2 - w / 2, innerWidth - w - 4));
    // Above the anchor by default, below when the top has no room.
    let y = r.top - h - 6;
    if (y < 4) y = r.bottom + 6;
    el.style.left = `${x}px`;
    el.style.top = `${y}px`;
  }
  function hide() {
    if (anchor) anchor.removeAttribute('aria-describedby');
    anchor = null;
    el.classList.remove('visible');
    orphanWatch.disconnect();
  }
  // The 5s poll rebuilds table rows via innerHTML; if that destroys the
  // current anchor the bubble would stay orphaned at its old position.
  const orphanWatch = new MutationObserver(() => {
    if (anchor && !anchor.isConnected) hide();
  });
  document.addEventListener('mouseover', (e) => {
    const t = e.target.closest('[data-custom-tooltip]');
    if (t) show(t);
    else if (anchor) hide();
  });
  document.addEventListener('focusin', (e) => {
    const t = e.target.closest('[data-custom-tooltip]');
    if (t) show(t);
  });
  document.addEventListener('focusout', hide);
  document.addEventListener('scroll', hide, true);
  // WCAG 2.2 SC 1.4.13 requires hover/focus content to be dismissable
  // without moving the pointer or the focus — the bubble is positioned
  // over the row beneath it. The event is neither stopped nor
  // default-prevented, so the history dialog still closes on the same
  // key press rather than needing a second one.
  document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') hide();
  });
  // Pointer left the window entirely: no further mouseover will fire,
  // so hide explicitly (relatedTarget is null only on window exit).
  document.addEventListener('mouseout', (e) => {
    if (!e.relatedTarget) hide();
  });
})();

/* ── Icons ── */
/* Inline stroke SVGs on a shared 24px grid, colored via currentColor so
   the chip styling in styles.css owns the color. Static strings only —
   never interpolate data into these. */
const ICONS = {
  run: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linejoin="round" aria-hidden="true"><polygon points="6 4 20 12 6 20 6 4"/></svg>',
  edit: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M17 3a2.8 2.8 0 1 1 4 4L7.5 20.5 2 22l1.5-5.5Z"/></svg>',
  pause: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true"><line x1="9" y1="5" x2="9" y2="19"/><line x1="15" y1="5" x2="15" y2="19"/></svg>',
  trash: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/></svg>',
  enable: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true"><path d="M18.4 6.6a8 8 0 1 0 2.1 5.4"/><polyline points="21 4 21 12 13 12"/></svg>'
};

/* ── State ── */
let editing = null;
let selectedJob = null;
tzSelect.value = tzPref;
/* Timezone is display-only state: repaint every timestamp-bearing
   section from the data already in memory. Never refetch — render-only
   interactions must not spend the request budget (the rate limiter
   counts every request), and a repaint that depends on a network round
   trip looks dead exactly when the daemon is unreachable and the cached
   data is all the user has. The config table shows no timestamps, so it
   is left alone. */
function repaintTimestamps() {
  // Every section repaints through its guard: the guards' identity keys
  // fold in tzPref, so the changed key both forces this repaint and is
  // recorded by it — the next poll tick with unchanged data is then a
  // no-op instead of a redundant rebuild (which would also destroy any
  // text selection in an open history output block).
  if (dashboardCache) {
    jobsGuard.apply(dashboardCache);
    removedGuard.apply(dashboardCache.removed);
  }
  if (historyCache) historyGuard.apply(historyCache);
}
/* Single adoption path for a preference change, wherever it came from
   (the select, or another tab via the storage event): validate, sync
   tzPref and the select, and repaint — so the pieces can never disagree.
   Unknown values (other app version, manual edit) fall back to 'local';
   both localStorage.clear() and removeItem deliver a null newValue,
   which falls back the same way. */
function adoptTzPref(value, persist) {
  const next = validTzPref(value);
  if (persist) storePref('timezone', next);
  if (next === tzPref) return; // no repaint when nothing changed
  tzPref = next;
  tzSelect.value = next;
  repaintTimestamps();
}
tzSelect.addEventListener('change', () => adoptTzPref(tzSelect.value, true));
// Another tab changed the persisted preference: adopt it here too, or
// this tab's cached tzPref would keep formatting timestamps in the old
// zone forever while both tabs share the same stored setting. e.key is
// null when another tab called localStorage.clear().
globalThis.addEventListener('storage', (e) => {
  // The storage event also fires for sessionStorage changes made by a
  // same-origin document in the same tab (e.g. this dashboard embedded
  // in an iframe): a host page's sessionStorage.clear() must not reset
  // the preference — only localStorage carries it.
  if (e.storageArea !== localStorage) return;
  if (e.key !== 'timezone' && e.key !== null) return;
  adoptTzPref(e.newValue, false);
});

/* ── Theme: auto / light / dark ── */
const themeCycle = ['auto', 'light', 'dark'];
const themeText = { auto: 'Auto', light: 'Light', dark: 'Dark' };
// Validated like the timezone preference: an unknown stored value
// (another app version, a manual edit) must not leak into the button
// label as the literal string "undefined".
let currentTheme = storedPref('theme');
if (!themeCycle.includes(currentTheme)) currentTheme = 'auto';
const themeBtn = document.getElementById('themeBtn');

function applyTheme(t) {
  currentTheme = t;
  storePref('theme', t);
  if (t === 'auto') {
    delete document.documentElement.dataset.theme;
  } else {
    document.documentElement.dataset.theme = t;
  }
  themeBtn.textContent = themeText[t];
  themeBtn.dataset.customTooltip = 'Theme: ' + themeText[t];
}
themeBtn.addEventListener('click', () => {
  applyTheme(themeCycle[(themeCycle.indexOf(currentTheme) + 1) % themeCycle.length]);
});
applyTheme(currentTheme);

/* ── Density: auto / compact / comfortable ── */
const densityCycle = ['auto', 'compact', 'comfortable'];
const densityText = { auto: 'Auto', compact: 'Compact', comfortable: 'Comfy' };
// Validated like theme above: garbage here would additionally skip
// compact resolution entirely (effectiveDensity passes unknown values
// through and they match neither 'compact' nor 'comfortable').
let currentDensity = storedPref('density');
if (!densityCycle.includes(currentDensity)) currentDensity = 'auto';
const densityBtn = document.getElementById('densityBtn');

/* Effective density for a preference: auto resolves via the device
   heuristic (touch screen, narrow viewport, or prefers-reduced-motion),
   defined ONCE by the pre-paint script in layout.html so first paint
   and later toggles agree. The fallback runs only when the inline
   script never executed (a reverse proxy hardening CSP to drop
   'unsafe-inline', template/script version skew) and is a deliberate
   stub, not a second copy of the heuristic — a copy would drift, and
   the accepted worst case on that degraded path is a density mismatch,
   not a top-level TypeError that would kill the whole dashboard. */
const effectiveDensity = globalThis.ofeliaEffectiveDensity ||
  ((pref) => pref === 'auto' ? 'compact' : pref);

function applyDensity(d) {
  currentDensity = d;
  storePref('density', d);
  const effective = effectiveDensity(d);
  document.body.classList.toggle('compact', effective === 'compact');
  document.documentElement.classList.remove('compact-early');
  densityBtn.textContent = densityText[d];
  densityBtn.dataset.customTooltip = 'Density: ' + densityText[d] + (d === 'auto' ? ' (' + effective + ')' : '');
}
densityBtn.addEventListener('click', () => {
  applyDensity(densityCycle[(densityCycle.indexOf(currentDensity) + 1) % densityCycle.length]);
});
applyDensity(currentDensity);
/* Re-evaluate auto density on resize */
globalThis.matchMedia('(max-width: 600px)').addEventListener('change', () => {
  if (currentDensity === 'auto') applyDensity('auto');
});

/* ── Table search ── */
/* Reusable: bind an input to a re-render callback and get a matcher.
   The consuming render loop skips rows the matcher rejects — filtering
   at render time keeps nth-child striping correct (hidden rows would
   still count). Reuse for another table by creating a second instance. */
function createTableSearch(inputId, onChange) {
  const input = document.getElementById(inputId);
  let query = '';
  input.addEventListener('input', () => {
    query = input.value.trim().toLowerCase();
    onChange();
  });
  return {
    // matches(...fields): true when every rendered row field set contains
    // the query (empty query matches everything).
    matches: (...fields) => !query || fields.join(' ').toLowerCase().includes(query),
    // active(): lets render loops skip building the field strings
    // entirely when no query is set (the common case on every poll).
    active: () => query !== ''
  };
}
/* ── Table sort ── */
/* Reusable column sorting: a table opts in by putting data-sort="key"
   on its th elements; keys maps each key to a comparable value. Like
   the search, sorting only re-renders from cached data — no API call. */
function createTableSort(tableId, keys, onChange) {
  const thead = document.querySelector(`#${tableId} thead`);
  let col = null;
  let dir = 1;
  thead.addEventListener('click', (e) => {
    const th = e.target.closest('th[data-sort]');
    if (!th) return;
    const key = th.dataset.sort;
    if (col === key) { dir = -dir; } else { col = key; dir = 1; }
    thead.querySelectorAll('th[data-sort]').forEach(h => {
      const asc = h === th && dir === 1;
      const desc = h === th && dir === -1;
      h.classList.toggle('sort-asc', asc);
      h.classList.toggle('sort-desc', desc);
      // The classes only paint the chevron; aria-sort is what screen
      // readers announce, so it has to track the same state.
      let ariaSort = 'none';
      if (asc) ariaSort = 'ascending';
      else if (desc) ariaSort = 'descending';
      h.setAttribute('aria-sort', ariaSort);
    });
    onChange();
  });
  return {
    apply(rows) {
      if (!col) return rows;
      const key = keys[col];
      // Decorate–sort–undecorate: one key evaluation per row instead of
      // two per comparison — the date keys parse timestamps, and an
      // active sort re-runs on every changed poll tick. sort() is
      // stable, so ordering is identical to sorting rows directly.
      return rows
        .map(row => [key(row), row])
        .sort((a, b) => {
          if (a[0] < b[0]) return -dir;
          if (a[0] > b[0]) return dir;
          return 0;
        })
        .map(pair => pair[1]);
    }
  };
}

const jobSearch = createTableSearch('jobSearch', () => renderJobs());
const jobSort = createTableSort('jobs', {
  name: j => j.name.toLowerCase(),
  schedule: j => j.schedule || '',
  command: j => j.command || '',
  lastRun: j => j.lastRun ? epochMs(j.lastRun.date) : -Infinity,
  duration: j => j.lastRun?.duration ?? -1
}, () => renderJobs());

/* One outcome word per run, and the sparkline class that goes with it.
   Kept as a lookup rather than a chain of ternaries so the two can never
   describe different states. */
function runState(r) {
  if (r.failed) return 'failed';
  if (r.skipped) return 'skipped';
  return 'ok';
}
const SPARK_CLASS = { failed: 'fail', skipped: 'skip', ok: 'ok' };

/* Where a job came from, phrased for the tooltip on its name. Empty when
   the origin is unknown — an empty tooltip is omitted by the caller. */
function originTooltip(origin, configOwned) {
  if (configOwned) {
    const source = origin === 'ini' ? 'INI config file' : 'Docker labels';
    return `Defined in the ${source} — edit or delete it at the source`;
  }
  if (!origin) return '';
  return `Created via the ${origin === 'web' ? 'web UI' : 'API'}`;
}

/* The show/hide control for a run's output subrow. */
function outputToggle(key, isOpen) {
  const label = isOpen ? 'hide' : 'view';
  return `<button type="button" class="toggle-output" data-action="toggle-output" data-key="${key}">${label}</button>`;
}

/* ── Jobs table (merged: active + disabled) ── */
/* The 5s poll fetches everything through the aggregate /api/dashboard
   (one request instead of five — five per tick used to exhaust the
   server's 100/min rate limit with two tabs open). setJobs caches and
   renders; search and sort re-render from the cache without any API
   call. */
/* ── Stat cards ── */
/* Quiet number+label tiles above the jobs table, recomputed from the
   same dashboard payload every tick — no extra API calls. */
/* The zone an RFC3339 timestamp carries, as a display suffix. Read off
   the end of the string rather than matched: the server emits either a
   trailing Z or a six-character offset, and both are fixed-width. Z is
   spelled out — a bare letter reads as part of the time. */
function zoneSuffix(str) {
  if (str.endsWith('Z')) return ' UTC';
  const tail = str.slice(-6);
  const sign = tail.charAt(0);
  if ((sign === '+' || sign === '-') && tail.charAt(3) === ':') return ' ' + tail;
  return '';
}

/* Date and time as separate strings (for two-line table cells),
   honoring the timezone preference like formatTime. */
function formatTimeParts(dateStr) {
  const { pref, str, dt, valid } = tzParse(dateStr);
  if (!valid) return null;
  if (pref === 'utc') {
    // One serialization, two slices; slicing to :19 drops the fractional
    // seconds so UTC cells read "2026-08-18 07:30:00" everywhere.
    const iso = dt.toISOString();
    return { date: iso.slice(0, 10), time: iso.slice(11, 19) };
  }
  if (pref === 'server') {
    // Keep the server's zone offset on the time. Slicing it away left
    // nothing in the UI that reveals which zone the server is in, so a
    // user correlating a timestamp against the server's own logs read it
    // as local time and was wrong by the offset.
    return { date: str.slice(0, 10), time: str.slice(11, 19) + zoneSuffix(str) };
  }
  return { date: dt.toLocaleDateString(), time: dt.toLocaleTimeString() };
}

/* Time-of-day in the user's timezone preference — a wall-clock time
   stays truthful between polls, unlike a countdown that never ticks. */
function formatTimeOnly(dateStr) {
  return formatTimeParts(dateStr)?.time ?? '';
}
function setStat(id, value, label, alert, tooltip) {
  const card = document.getElementById(id);
  card.querySelector('.stat-value').textContent = value;
  card.querySelector('.stat-label').textContent = label;
  card.classList.toggle('alert', Boolean(alert));
  if (tooltip) {
    card.dataset.customTooltip = tooltip;
  } else {
    delete card.dataset.customTooltip;
  }
}
function updateStats(active, disabled) {
  setStat('statActive', String(active.length),
    'Active' + (disabled.length ? ` · ${disabled.length} paused` : ''), false);

  const failing = active.filter(j => j.lastRun?.failed);
  // The tooltip must be re-passed on every tick: setStat removes the
  // attribute when none is given, which would strip the one the template
  // sets after the first poll.
  setStat('statFailing', failing.length ? `${failing.length} ⚠` : '0',
    'Failing', failing.length > 0, 'Show only failing jobs');

  const now = Date.now();
  let nextJobs = [];
  let nextAt = Infinity;
  let nextAtStr = '';
  active.forEach(j => {
    // The job's earliest future run, tracked together with its original
    // string so no second parse is needed to recover it.
    let at = Infinity;
    let atStr = '';
    (j.nextRuns || []).forEach(t => {
      const ms = epochMs(t);
      if (ms > now && ms < at) { at = ms; atStr = t; }
    });
    if (at === Infinity) return;
    if (at < nextAt) {
      nextAt = at;
      nextAtStr = atStr;
      nextJobs = [j.name];
    } else if (at === nextAt) {
      nextJobs.push(j.name); // several jobs share the same next slot
    }
  });
  if (nextJobs.length > 0) {
    const label = nextJobs.length === 1
      ? `Next · ${nextJobs[0]}`
      : `Next · ${nextJobs[0]} +${nextJobs.length - 1}`;
    // Tooltip carries the full list — the label ellipsizes long names
    // and hides everything behind the +N.
    setStat('statNext', formatTimeOnly(nextAtStr), label, false, nextJobs.join('\n'));
  } else {
    setStat('statNext', '–', 'Next run', false);
  }
}

let jobsCache = null;
function jobByName(name) {
  return (jobsCache || []).find(j => j.name === name);
}
function setJobs(active, disabled) {
  updateStats(active, disabled);
  jobsCache = [
    ...active.map(j => ({...j, _disabled: false})),
    ...disabled.map(j => ({...j, _disabled: true}))
  ];
  document.getElementById('badgeJobs').textContent = jobsCache.length;
  document.getElementById('jobCount').textContent = `${active.length} active` + (disabled.length ? ` · ${disabled.length} disabled` : '');
  renderJobs();
}

function renderJobs() {
  if (jobsCache === null) return;
  const tbody = document.querySelector('#jobs tbody');
  tbody.innerHTML = '';

  if (jobsCache.length === 0) {
    tbody.innerHTML = '<tr><td colspan="8" class="empty">No jobs configured.</td></tr>';
    return;
  }

  // Search matches what the user actually sees, so Last Run and
  // Duration are matched in their displayed formats — but only built
  // when a query is active, so the every-5s no-query render skips the
  // date parsing and formatting entirely. The failing filter (toggled
  // by the Failing stat card) narrows further; it matches active jobs
  // only, mirroring how the Failing stat itself is counted.
  const searchActive = jobSearch.active(); // once per render, not per row
  const visible = jobSort.apply(jobsCache.filter(j =>
    (!failingOnly || (!j._disabled && j.lastRun?.failed)) &&
    (!searchActive ||
      jobSearch.matches(
        j.name, j.command || '', j.schedule || '',
        j.lastRun ? formatTime(j.lastRun.date) : '',
        j.lastRun ? formatDuration(j.lastRun.duration) : ''
      ))));
  if (visible.length === 0) {
    tbody.innerHTML = `<tr><td colspan="8" class="empty">${failingOnly ? 'No failing jobs.' : 'No jobs match the search.'}</td></tr>`;
    return;
  }

  // Rows collect in a fragment and land in the live table once, so the
  // browser lays the table out a single time per render.
  const frag = document.createDocumentFragment();
  visible.forEach(j => {
    const tr = document.createElement('tr');
    tr.dataset.jobName = j.name;
    if (j.name === selectedJob) tr.classList.add('selected');

    const eName = escapeHtml(j.name);
    const configOwned = j.origin === 'ini' || j.origin === 'label';
    const { lastRun, dur } = lastRunCells(j);
    const eSched = escapeHtml(j.schedule);
    const sched = j._disabled ? `<s class="disabled-text">${eSched}</s>` : eSched;

    tr.innerHTML =
      `<td>${jobStatusDot(j)}</td><td>${jobNameCell(j, eName, configOwned)}</td>` +
      `<td class="mono">${sched}</td><td class="mono wrap">${escapeHtml(j.command)}</td>` +
      `<td>${lastRun}</td><td class="dur-cell">${durationCell(j, dur)}</td>` +
      `<td>${sparkCell(j)}</td><td>${actionsCell(j, eName, configOwned)}</td>`;
    frag.appendChild(tr);
  });
  tbody.appendChild(frag);
}

/* ── Job row cells ──
   One function per cell, so renderJobs stays a loop that assembles a row
   rather than a loop that also decides what every cell contains. Each
   returns ready-to-insert HTML with its dynamic parts already escaped. */

/* Status dot. Running beats disabled beats the last run's outcome. */
function jobStatusDot(j) {
  if (j.running) return dotSpan('running', 'Running now');
  if (j._disabled) return dotSpan('disabled', 'Disabled');
  if (j.lastRun) return statusDot(j.lastRun.failed, j.lastRun.skipped);
  return dotSpan('none', 'Never run');
}

/* The Last Run cell and the raw duration the Duration cell builds on. A
   disabled job shows neither: its last run says nothing about what it
   will do next. */
function lastRunCells(j) {
  if (j._disabled || !j.lastRun) return { lastRun: '', dur: '' };
  const parts = formatTimeParts(j.lastRun.date);
  const lastRun = parts
    ? `<span class="lr-date">${escapeHtml(parts.date)}</span><span class="lr-time">${escapeHtml(parts.time)}</span>`
    : '';
  return { lastRun, dur: formatDuration(j.lastRun.duration) };
}

/* The job name. A real button, not just a clickable row, so keyboard
   users can Tab to it and open the history with Enter. Its tooltip names
   the job's origin — and for config-owned jobs (which have no delete
   button) where deletion actually happens. */
function jobNameCell(j, eName, configOwned) {
  const originTitle = originTooltip(j.origin, configOwned);
  const tooltipAttr = originTitle ? ` data-custom-tooltip="${escapeHtml(originTitle)}"` : '';
  const nameText = j._disabled ? `<s class="disabled-text">${eName}</s>` : eName;
  return `<button type="button" class="job-name" data-action="history" data-job="${eName}"${tooltipAttr}>${nameText}</button>`;
}

/* Result sparkline: one square per recent run, oldest first. */
function sparkCell(j) {
  const spark = (j.recentRuns || []).map(r => {
    const state = runState(r);
    return `<i class="${SPARK_CLASS[state]}" data-custom-tooltip="${escapeHtml(formatTime(r.date))} · ${state}"></i>`;
  }).join('');
  return spark ? `<span class="spark">${spark}</span>` : '';
}

/* Last / avg / max over the recent completed runs (skipped excluded) — a
   slowing job shows up at a glance. Falls back to the bare last duration
   when there is not enough history to average. */
function durationCell(j, dur) {
  const durs = (j.recentRuns || []).filter(r => !r.skipped).map(r => r.duration);
  if (!dur || durs.length <= 1) return dur;
  const avg = formatDuration(durs.reduce((a, b) => a + b, 0) / durs.length);
  const max = formatDuration(Math.max(...durs));
  return `<span class="dur-line"><small>last</small>${dur}</span>` +
    `<span class="dur-line"><small>avg</small>${avg}</span>` +
    `<span class="dur-line"><small>max</small>${max}</span>`;
}

/* Row actions. Config-owned (ini/label) jobs keep edit and delete visible
   but inert via aria-disabled — a truly `disabled` control swallows hover
   events, so its tooltip would never show, and the API refuses both
   anyway (403; the update gate mirrors delete). Tooltips are kept short:
   the bubble near the table's right edge must not clip. */
function actionsCell(j, eName, configOwned) {
  const ownedTitle = j.origin === 'ini' ? 'Managed by the INI config' : 'Managed by Docker labels';
  const btn = (action, label, icon, inert) =>
    `<button data-action="${action}" data-job="${eName}" aria-label="${label} ${eName}"` +
    ` data-custom-tooltip="${escapeHtml(inert ? ownedTitle : label)}"` +
    (inert ? ' aria-disabled="true"' : '') + `>${icon}</button>`;
  const buttons = j._disabled
    ? [btn('enable', 'Enable', ICONS.enable, false),
       btn('edit', 'Edit', ICONS.edit, configOwned)]
    : [btn('run', 'Run', ICONS.run, false),
       btn('edit', 'Edit', ICONS.edit, configOwned),
       btn('disable', 'Disable', ICONS.pause, false),
       btn('delete', 'Delete', ICONS.trash, configOwned)];
  return `<div class="actions">${buttons.join('')}</div>`;
}

/* Failing filter, toggled by the Failing stat card — a real button in
   the markup, so keyboard activation and focus come for free. */
let failingOnly = false;
document.getElementById('statFailing').addEventListener('click', (e) => {
  const card = e.currentTarget;
  failingOnly = !failingOnly;
  card.classList.toggle('pressed', failingOnly);
  card.setAttribute('aria-pressed', String(failingOnly));
  renderJobs();
});

/* ── History ── */
/* Like the jobs table: loadHistory fetches and caches, renderHistory is
   a pure render — the sort re-renders without touching the API. */
let historyCache = null;
// One sequence domain for every history-bearing response — direct
// loadHistory fetches AND the 5s poll's ?history= rider (see refresh):
// each request takes a seq at fetch start, and a response applies only
// while nothing at least as new has been ADOPTED. Request-start
// ordering makes a stale response harmless in both directions — a slow
// poll response cannot repaint over a fresher direct load that already
// landed (an in-flight yield would only cover the overlap while the
// direct fetch is still pending), and a slow direct response cannot
// repaint over a fresher rider. Comparing against the newest adopted
// (not newest started) response avoids starving the modal when every
// response is slower than the poll interval — same rationale as
// refreshSeq/adoptedSeq below.
let historyReqSeq = 0;
let historyAdoptedSeq = 0;
// Server-issued identity of the runs currently held, echoed back on the
// next poll as ?historyFp=. On a match the server omits the history from
// the response instead of re-serializing (and re-compressing) the full
// stdout/stderr of every run of the open job on every 5s tick — which
// the guard below would discard anyway. Bound to the job it came from,
// so it can never elide the history of a different job.
let historyFp = null;
let historyFpJob = null;
function forgetHistoryFingerprint() {
  historyFp = null;
  historyFpJob = null;
}
/* The fingerprint is server-issued and goes back out in a request URL, so
   it is accepted only in the shape the server produces: base-36 digits,
   which a 64-bit hash never spends more than 13 of (the bound below
   leaves slack rather than tracking that arithmetic). Anything else — a
   proxy rewriting the body, a version skew — is dropped rather than
   echoed, which costs one full history payload and nothing else. */
function adoptHistoryFingerprint(job, value) {
  if (typeof value === 'string' && /^[0-9a-z]{1,16}$/.test(value)) {
    historyFp = value;
    historyFpJob = job;
    return;
  }
  // Absent means the job has no history or vanished; a stale fingerprint
  // must never elide a payload the modal still needs.
  forgetHistoryFingerprint();
}
/* The poll URL. The open job's history rides along, and the fingerprint
   of the runs already held rides with it so the server can omit them. */
function dashboardURL(historyFor) {
  if (!historyFor) return '/api/dashboard';
  let url = `/api/dashboard?history=${encodeURIComponent(historyFor)}`;
  // Only for the job the fingerprint was issued for — see historyFp.
  if (historyFp !== null && historyFpJob === historyFor) {
    url += `&historyFp=${encodeURIComponent(historyFp)}`;
  }
  return url;
}
const historySort = createTableSort('history', {
  date: e => epochMs(e.date),
  duration: e => e.duration,
  error: e => e.error || ''
}, () => renderHistory());

async function loadHistory(name) {
  selectedJob = name;
  // The direct endpoint issues no fingerprint, so the one held (if any)
  // describes runs this load is about to replace.
  forgetHistoryFingerprint();
  const seq = ++historyReqSeq;
  document.getElementById('historyJob').textContent = name;
  const modal = document.getElementById('historyModal');
  // Guard: loadHistory also runs from the 5s refresh while the modal is
  // already open, and showModal() on an open dialog throws.
  if (!modal.open) modal.showModal();
  // Highlight selected row
  document.querySelectorAll('#jobs tbody tr').forEach(r => {
    r.classList.toggle('selected', r.dataset.jobName === name);
  });

  // Opening a different job: drop the stale rows and show a loader
  // (Pico renders a spinner for aria-busy) until the fetch lands.
  const tbody = document.querySelector('#history tbody');
  if (tbody.dataset.job !== name) {
    historyCache = null;
    historyGuard.reset(); // the loader wipe destroys rows the guard has recorded as painted
    tbody.innerHTML = '<tr><td colspan="5" class="empty" aria-busy="true">Loading…</td></tr>';
  }

  let runs;
  try {
    const resp = await fetch(`/api/jobs/${encodeURIComponent(name)}/history`);
    if (!resp.ok) throw new Error(`HTTP ${resp.status}`);
    runs = await resp.json();
  } catch {
    // An outdated request's failure must not paint an error state on
    // top of history a newer response (direct or rider) has already
    // painted — mirror the success path's staleness check below.
    if (seq <= historyAdoptedSeq) return;
    // Replace the loader with an error state, but only while this job is
    // still the selected one and nothing has been rendered for it yet —
    // a failed background refresh must not wipe rows already on screen.
    if (selectedJob === name && tbody.dataset.job !== name) {
      tbody.innerHTML = '<tr><td colspan="5" class="empty">Failed to load history.</td></tr>';
    }
    return;
  }
  // The user may have switched jobs while the fetch was in flight; a
  // late response must not fill another job's modal.
  if (selectedJob !== name) return;
  // A poll rider (or a newer direct load) already adopted history at
  // least as fresh — older rows must not repaint over it.
  if (seq <= historyAdoptedSeq) return;
  historyAdoptedSeq = seq;
  // Apply through the shared guard: when the 5s poll already painted
  // identical data, the redundant rebuild is skipped (it would destroy
  // any text selection in an open output block); the guard also primes
  // itself so the next identical poll tick skips too.
  historyGuard.apply(runs);
}

function renderHistory() {
  if (historyCache === null || selectedJob === null) return;
  const name = selectedJob;
  const tbody = document.querySelector('#history tbody');
  // Preserve which outputs the user has expanded so the periodic refresh
  // does not collapse them (keyed by run timestamp, unique per execution).
  // Scoped to the job the table currently shows: on a job switch these rows
  // still belong to the previously selected job, and its keys must not
  // decide what is expanded in another job's history.
  const openOutputs = new Set(
    tbody.dataset.job === name
      ? Array.from(tbody.querySelectorAll('tr.output-row.open')).map(r => r.dataset.key)
      : []
  );
  // Preserve where the user had scrolled inside each expanded output. The
  // rebuild below replaces every <pre>, and a fresh element starts at
  // scrollTop 0 — so a job that keeps producing runs yanked the reader
  // back to the top of the log on every 5s tick, which made a long log
  // impossible to read (#808). Same keying as openOutputs, plus which
  // stream the block shows, because stdout and stderr each get their own
  // scroller.
  const openScroll = tbody.dataset.job === name ? captureOutputScroll(tbody) : new Map();
  tbody.dataset.job = name;
  tbody.innerHTML = '';
  if (historyCache.length === 0) {
    tbody.innerHTML = '<tr><td colspan="5" class="empty">No history yet.</td></tr>';
    return;
  }
  const frag = document.createDocumentFragment();
  historySort.apply(historyCache).forEach(e => {
    const row = document.createElement('tr');
    const dot = statusDot(e.failed, e.skipped);
    const err = escapeHtml(e.error || '');
    const stdout = escapeHtml(stripControlChars(e.stdout));
    const stderr = escapeHtml(stripControlChars(e.stderr));
    const hasOut = stdout || stderr;
    const key = escapeHtml(String(e.date));
    const isOpen = openOutputs.has(String(e.date));
    const output = hasOut ? outputToggle(key, isOpen) : '';
    if (isOpen) row.classList.add('selected');
    row.innerHTML = `<td>${dot}</td><td>${escapeHtml(formatTime(e.date))}</td><td>${formatDuration(e.duration)}</td>` +
      `<td class="wrap">${err}</td><td>${output}</td>`;
    frag.appendChild(row);
    // Output lives in a full-width subrow so expanding it never changes the
    // column widths above. The subrow is rendered for EVERY run (empty when
    // the run has no output) so the table is strictly run/subrow pairs —
    // the pure-CSS pair striping in styles.css depends on that regularity.
    // Toggled via the `open` class — the DOM is the single source of the
    // expanded state.
    const stdoutBlock = stdout ? `<pre>${stdout}</pre>` : '';
    const stderrBlock = stderr ? `<pre class="stderr">${stderr}</pre>` : '';
    const sub = document.createElement('tr');
    sub.className = 'output-row' + (isOpen ? ' open' : '');
    sub.dataset.key = String(e.date);
    // Empty first cell keeps the output aligned with the Date column.
    sub.innerHTML = `<td></td><td colspan="4">${stdoutBlock}${stderrBlock}</td>`;
    frag.appendChild(sub);
  });
  tbody.appendChild(frag);
  restoreOutputScroll(tbody, openScroll);
}

/* Which stream an output block shows. Used as part of the scroll key so
   the saved position follows the stream rather than its position in the
   subrow: a run that has only stderr renders one block at index 0, and
   keying by index would hand that offset to stdout if the same key ever
   rendered both. */
function outputStream(pre) {
  return pre.classList.contains('stderr') ? 'stderr' : 'stdout';
}
function outputScrollKey(row, pre) {
  return `${row.dataset.key}\u0000${outputStream(pre)}`;
}

/* Where each expanded output block is scrolled to.
   The offset is enough — no separate "was at the bottom" state. The table
   only ever shows COMPLETED runs (SetLastRun runs in jobWrapper.stop,
   after ctx.Stop), so an expanded run's output is immutable: its
   scrollHeight after a rebuild is the height it had before, and the saved
   offset still means the same place. An earlier version carried a
   'bottom' sentinel for runs that keep growing; no such run is ever in
   this table, so it could not differ from the offset it replaced. */
function captureOutputScroll(tbody) {
  const scroll = new Map();
  tbody.querySelectorAll('tr.output-row.open').forEach(row => {
    row.querySelectorAll('pre').forEach(pre => {
      if (pre.scrollTop > 0) scroll.set(outputScrollKey(row, pre), pre.scrollTop);
    });
  });
  return scroll;
}

function restoreOutputScroll(tbody, scroll) {
  if (scroll.size === 0) return;
  tbody.querySelectorAll('tr.output-row.open').forEach(row => {
    row.querySelectorAll('pre').forEach(pre => {
      const at = scroll.get(outputScrollKey(row, pre));
      if (at !== undefined) pre.scrollTop = at;
    });
  });
}

/* Toggle an output subrow open/closed; the run's own row is highlighted
   while its output is open. */
document.querySelector('#history tbody').addEventListener('click', (e) => {
  const btn = e.target.closest('button[data-action="toggle-output"]');
  if (!btn) return;
  const row = btn.closest('tr');
  const sub = row.nextElementSibling;
  if (!sub?.classList.contains('output-row')) return;
  const open = sub.classList.toggle('open');
  row.classList.toggle('selected', open);
  btn.textContent = open ? 'hide' : 'view';
});

function closeHistory() {
  const modal = document.getElementById('historyModal');
  if (modal.open) modal.close();
}

/* All close paths (button, Esc, backdrop click) end in the dialog's close
   event, so the state cleanup lives there once. */
const historyModal = document.getElementById('historyModal');
historyModal.addEventListener('close', () => {
  selectedJob = null;
  // Release the run payloads: a chatty job's history holds megabytes of
  // stdout/stderr, and reopening always refetches — keeping the cache
  // (and the guard identity derived from it) would pin that heap for
  // the lifetime of the tab.
  historyCache = null;
  historyGuard.reset();
  forgetHistoryFingerprint();
  // The rows just wiped (and any loader/error state a reopen paints)
  // live OUTSIDE the guards, but the identical-payload short-circuit in
  // refresh() only knows the raw payload text: left in place, a
  // byte-identical tick after a close-and-reopen would return before
  // the history rider and a failed refetch's error state could never be
  // repaired while the dashboard payload stays byte-identical. Forget
  // the payload so the first tick after a reopen reaches the renderers
  // (the per-section guards still absorb the redundant rebuilds).
  lastDashboardPayload = null;
  // Also drop the rendered rows and the table's job identity. Leaving
  // them meant reopening the same job skipped the loader wipe and, when
  // the refetch failed, suppressed the error state — a modal showing
  // stale rows with sorting and timezone repaints silently dead (both
  // no-op while historyCache is null). Wiping here also releases the
  // row DOM, completing the memory reclaim above.
  const historyTbody = document.querySelector('#history tbody');
  historyTbody.innerHTML = '';
  delete historyTbody.dataset.job;
  document.querySelectorAll('#jobs tbody tr').forEach(r => r.classList.remove('selected'));
});
historyModal.addEventListener('click', (e) => {
  // A click on the dialog element itself is the backdrop; clicks inside
  // the article land on descendants.
  if (e.target === historyModal) historyModal.close();
});

/* ── Removed ── */
function renderRemoved(jobs) {
  const tbody = document.querySelector('#removedJobs tbody');
  const badge = document.getElementById('badgeRemoved');
  const empty = document.getElementById('removedEmpty');
  tbody.innerHTML = '';

  if (jobs.length === 0) {
    badge.style.display = 'none';
    empty.style.display = '';
    return;
  }
  badge.textContent = jobs.length;
  badge.style.display = '';
  empty.style.display = 'none';

  const frag = document.createDocumentFragment();
  jobs.forEach(j => {
    const tr = document.createElement('tr');
    let dot, lastRun, dur;
    if (j.lastRun) {
      dot = statusDot(j.lastRun.failed, j.lastRun.skipped);
      lastRun = escapeHtml(formatTime(j.lastRun.date));
      dur = formatDuration(j.lastRun.duration);
    } else {
      dot = dotSpan('none', 'Never run');
      lastRun = ''; dur = '';
    }
    tr.innerHTML = `<td>${dot}</td><td>${escapeHtml(j.name)}</td><td class="mono">${escapeHtml(j.schedule)}</td><td class="mono wrap">${escapeHtml(j.command)}</td><td>${lastRun}</td><td>${dur}</td>`;
    frag.appendChild(tr);
  });
  tbody.appendChild(frag);
}

/* ── Config ── */
function renderConfigTable(cfg) {
  const tbody = document.querySelector('#configTable tbody');
  tbody.innerHTML = '';
  // The server can legally send "config": null (stripJobs of a nil
  // config) — Object.entries(null) would throw and abort the refresh.
  if (cfg === null || typeof cfg !== 'object') return;
  const frag = document.createDocumentFragment();
  function addRow(k, v) {
    const tr = document.createElement('tr');
    tr.innerHTML = `<td class="mono">${escapeHtml(k)}</td><td>${escapeHtml(v)}</td>`;
    frag.appendChild(tr);
  }
  // Job collections are shown in their own tabs, not as config rows.
  // stripJobs in web/server.go already nulls these fields server-side;
  // this is the client-side backstop, matched structurally (a MAP under
  // a *Jobs key: ExecJobs, RunJobs, ServiceJobs, LocalJobs, ComposeJobs,
  // ...) rather than by a hand-written name list — a list is inert in
  // the one case a backstop exists for: a NEW collection from a newer
  // server is, by definition, not on it. Arrays are excluded from the
  // match: every job collection is a map (cli/config.go), so an array
  // under a *Jobs key is ordinary config (e.g. a list of job names) and
  // must stay visible rather than vanish from the tab with no error.
  function traverse(o, prefix) {
    for (const [k, v] of Object.entries(o)) {
      if (k.endsWith('Jobs') && v !== null && typeof v === 'object' && !Array.isArray(v)) continue;
      if (v === null || (typeof v === 'object' && !Array.isArray(v) && Object.keys(v).length === 0)) continue;
      if (typeof v === 'object' && !Array.isArray(v)) { traverse(v, prefix + k + '.'); }
      else { addRow(prefix + k, v); }
    }
  }
  traverse(cfg, '');
  tbody.appendChild(frag);
}


/* ── Toast ── */
/* Reusable bottom-right notice: toast.success(msg), toast.error(msg),
   toast.info(msg), or toast.show(msg, {kind, duration}). One element,
   the latest message wins; errors stay visible longer by default. */
const toast = (() => {
  let el = null;
  let timer = null;
  function show(message, {kind = 'info', duration} = {}) {
    if (!el) {
      el = document.createElement('div');
      el.id = 'notice';
      document.body.appendChild(el);
    }
    el.textContent = message;
    el.className = `visible ${kind}`;
    clearTimeout(timer);
    timer = setTimeout(() => el.classList.remove('visible'),
      duration ?? (kind === 'error' ? 6000 : 3500));
  }
  return {
    show,
    info: (m) => show(m, {kind: 'info'}),
    success: (m) => show(m, {kind: 'success'}),
    error: (m) => show(m, {kind: 'error'})
  };
})();

/* ── Actions ── */
async function apiPost(url, body) {
  let resp;
  try {
    resp = await fetch(url, {
      method: 'POST',
      body: JSON.stringify(body),
      headers: {'Content-Type': 'application/json', 'X-Origin': 'web'}
    });
  } catch (err) {
    // fetch itself rejected (network drop, daemon restarting): without
    // this the rejection escapes every caller unhandled and the user
    // gets no feedback at all.
    toast.error('Network error — request failed.');
    return false;
  }
  // The body read can also reject (connection dropped mid-response) —
  // fall back to the status line rather than escaping the caller.
  if (!resp.ok) toast.error(await resp.text().catch(() => '') || `Request failed (${resp.status})`);
  return resp.ok;
}
/* One factory for the four row actions: endpoint + success wording is
   all that differs, so the post → toast → refresh sequence lives once.
   The confirmation gate lives inside the action (not a wrapper), so
   there is no unconfirmed delete entry point to call by mistake. */
function jobAction(endpoint, past, confirmMsg) {
  return async (name) => {
    if (confirmMsg && !globalThis.confirm(confirmMsg(name))) return;
    if (await apiPost(endpoint, {name})) toast.success(`Job "${name}" ${past}.`);
    refresh();
  };
}
const runJob = jobAction('/api/jobs/run', 'started');
const disableJob = jobAction('/api/jobs/disable', 'paused');
const enableJob = jobAction('/api/jobs/enable', 'resumed');
const deleteJob = jobAction('/api/jobs/delete', 'deleted',
  n => `Delete job "${n}"? This cannot be undone.`);

/* ── Form: type descriptions ── */
const typeDescriptions = {
  local: 'Runs a command directly on the Ofelia host machine.',
  run: 'Creates a new Docker container from an image, runs the command, then removes the container.',
  exec: 'Runs the command inside an already running Docker container.',
  compose: 'Runs the command via Docker Compose against a service defined in a compose file.'
};

/* ── Form: type-specific field visibility ── */
function updateTypeFields() {
  const type = document.getElementById('jobType').value;
  document.querySelectorAll('.type-fields').forEach(el => el.classList.remove('visible'));
  const panel = document.getElementById('fields-' + type);
  if (panel) panel.classList.add('visible');
  document.getElementById('typeDesc').textContent = typeDescriptions[type] || '';
}
document.getElementById('jobType').addEventListener('change', updateTypeFields);
updateTypeFields();

/* ── Form: update title/button for create vs edit ── */
function updateFormChrome() {
  const title = document.getElementById('formTitle');
  const badge = document.getElementById('editBadge');
  const btn = document.getElementById('formSubmitBtn');
  if (editing) {
    title.textContent = 'Edit Job';
    badge.textContent = editing;
    badge.style.display = '';
    btn.textContent = 'Update Job';
  } else {
    title.textContent = 'Create Job';
    badge.style.display = 'none';
    btn.textContent = 'Create Job';
  }
}

/* ── Form: submit ── */
document.getElementById('jobForm').addEventListener('submit', async e => {
  e.preventDefault();
  // While editing, the name field is read-only and the job keeps its
  // identity — the API has no rename, and posting a changed name to
  // /create would silently fork a duplicate job.
  const name = editing || document.getElementById('jobName').value;
  const type = document.getElementById('jobType').value;
  const schedule = document.getElementById('jobSchedule').value;
  const command = document.getElementById('jobCommand').value;
  const image = document.getElementById('jobImage').value;
  const container = document.getElementById('jobContainer').value;
  const file = document.getElementById('jobFile').value;
  const service = document.getElementById('jobService').value;
  const execEl = document.getElementById('jobExec');
  const exec = execEl ? execEl.checked : false;
  const url = editing ? '/api/jobs/update' : '/api/jobs/create';
  // A paused job stays paused across an update: the scheduler updates
  // disabled entries in place. Restoring the pause from here would take a
  // second, non-atomic request and would re-pause a job another tab had
  // deliberately resumed mid-edit.
  const ok = await apiPost(url, {name,type,schedule,command,image,container,file,service,exec});
  if (!ok) return; // keep the form as-is so the user can correct and retry
  editing = null;
  resetForm();
  switchTab('jobs');
  refresh();
});

/* role="switch" overrides the input's implicit checkbox role, and with it
   the state the browser would have exposed on its own — so aria-checked
   has to be maintained by hand or assistive technology reads the switch
   as permanently off. Every write to .checked goes through here, and the
   listener below covers the user's own toggles. */
function setExecSwitch(checked) {
  const el = document.getElementById('jobExec');
  if (!el) return;
  el.checked = checked;
  el.setAttribute('aria-checked', String(checked));
}
document.getElementById('jobExec')?.addEventListener('change', (e) => {
  e.target.setAttribute('aria-checked', String(e.target.checked));
});

function editJob(name) {
  const j = jobByName(name);
  if (!j) return;
  const nameEl = document.getElementById('jobName');
  nameEl.value = j.name;
  // The API cannot rename a job; an editable name would silently create
  // a duplicate on submit.
  nameEl.readOnly = true;
  nameEl.dataset.customTooltip = 'Job name cannot be changed';
  const typeSel = document.getElementById('jobType');
  typeSel.value = Array.from(typeSel.options).map(o=>o.value).includes(j.type) ? j.type : 'local';
  document.getElementById('jobSchedule').value = j.schedule;
  document.getElementById('jobCommand').value = j.command;
  document.getElementById('jobImage').value = j.config.Image || '';
  document.getElementById('jobContainer').value = j.config.Container || '';
  document.getElementById('jobFile').value = j.config.File || '';
  document.getElementById('jobService').value = j.config.Service || '';
  setExecSwitch(j.config.Exec || false);
  editing = name;
  updateFormChrome();
  updateTypeFields();
  switchTab('form');
}

function resetForm() {
  document.getElementById('jobForm').reset();
  // form.reset() restores .checked but not the ARIA mirror below.
  setExecSwitch(false);
  const nameEl = document.getElementById('jobName');
  nameEl.readOnly = false;
  delete nameEl.dataset.customTooltip;
  editing = null;
  updateFormChrome();
  updateTypeFields();
}

/* ── Event delegation for jobs table ── */
document.querySelector('#jobs tbody').addEventListener('click', (e) => {
  const btn = e.target.closest('button[data-action]');
  if (btn) {
    if (btn.getAttribute('aria-disabled') === 'true') return;
    const action = btn.dataset.action;
    const name = btn.dataset.job;
    if (action === 'run') runJob(name);
    else if (action === 'edit') editJob(name);
    else if (action === 'disable') disableJob(name);
    else if (action === 'delete') deleteJob(name);
    else if (action === 'enable') enableJob(name);
    else if (action === 'history') loadHistory(name);
    return;
  }
  const tr = e.target.closest('tr[data-job-name]');
  if (tr) loadHistory(tr.dataset.jobName);
});
document.getElementById('closeHistoryBtn').addEventListener('click', closeHistory);
document.getElementById('formClearBtn').addEventListener('click', resetForm);

/* ── Refresh ── */
/* One aggregate request per tick; the open job's history rides along
   via the ?history= parameter instead of its own request. */
let lastDashboardPayload = null;
/* One serialize-compare-render-record guard per section, written once:
   render only when the identity key of the data differs from what this
   guard last rendered (key defaults to JSON.stringify; pass a custom
   key when the rendered output depends on more than the data itself,
   or when full serialization is too expensive). Recording happens only
   AFTER the render returns, so a render that throws is retried on the
   next application instead of being marked as painted. reset() forces
   the next apply to render (used when the rendered rows were destroyed
   outside the guard — loader wipe on job switch, modal close). */
function jsonGuard(render, key = JSON.stringify) {
  let last = null;
  return {
    apply(data) {
      const k = key(data);
      if (k === last) return;
      render(data);
      last = k;
    },
    reset() { last = null; }
  };
}
// Every section renders through a guard, so a poll tick only rebuilds
// the sections whose data actually changed — with the history modal
// open the payload changes every tick the open job runs, and an
// unguarded section would be torn down and rebuilt each time for a
// pixel-identical result. tzPref is folded into the identity key of
// every timestamp-bearing section so a timezone change invalidates by
// construction (repaintTimestamps re-applies through these same
// guards, recording the new-tz key as it repaints).
const jobsGuard = jsonGuard(
  (d) => setJobs(d.jobs, d.disabled),
  (d) => JSON.stringify([tzPref, d.jobs, d.disabled])
);
const removedGuard = jsonGuard(renderRemoved,
  (jobs) => JSON.stringify([tzPref, jobs]));
const configGuard = jsonGuard(renderConfigTable);
// History is the heaviest render (escapes every run's full stdout and
// stderr): guard it so an unrelated job finishing a run does not tear
// down the open modal — which would also destroy any text selection
// inside an open output block. The identity key folds in the job name
// and timezone (both change what the same runs LOOK like, so their
// transitions invalidate by construction instead of by remembering to
// reset), and fingerprints each run instead of serializing it whole: a
// completed run's output is immutable, so date/duration/status/error
// plus output lengths detect every real change without re-stringifying
// megabytes of stdout on every changed poll tick.
const historyGuard = jsonGuard(
  (runs) => { historyCache = runs; renderHistory(); },
  (runs) => JSON.stringify([tzPref, selectedJob, runs.map(r => [
    r.date, r.duration, r.failed, r.skipped, r.error,
    r.stdout ? r.stdout.length : 0, r.stderr ? r.stderr.length : 0
  ])])
);
// Timestamp-bearing sections of the last adopted payload, kept so
// display-only changes (timezone) can repaint without another network
// round trip. history is deliberately NOT retained (see refresh).
let dashboardCache = null;
// Response ordering: a response is discarded once a response at least
// as new has been ADOPTED, so a slow older response can never repaint
// state on top of a newer one (e.g. the pre-action state after a job
// action). The comparison is against the newest adopted response, not
// the newest started request: the 5s interval fires unconditionally,
// so on a link where every response takes longer than 5s the newest
// started request would supersede ALL responses and the dashboard
// would starve forever — blank on first load, frozen afterwards.
let refreshSeq = 0;
let adoptedSeq = 0;
let authWarned = false;
async function refresh() {
  // Bind the history parameter to this request: a slow response must
  // only ever fill the modal of the job it was asked about.
  const historyFor = selectedJob;
  const seq = ++refreshSeq;
  // The rider participates in the history sequence domain (see
  // historyReqSeq): its response must not repaint over a fresher
  // direct load adopted while this poll was in flight.
  const historySeq = historyFor ? ++historyReqSeq : 0;
  const url = dashboardURL(historyFor);
  let text;
  try {
    const resp = await fetch(url);
    // Ordering check sits before ANY state change or toast: a slow,
    // already-outpaced response (e.g. a 401 from before the user
    // re-authenticated) must neither warn nor latch authWarned.
    if (seq <= adoptedSeq) return;
    if (resp.status === 401) {
      // Not transient: without a valid session every poll fails the
      // same way and the dashboard would silently freeze on stale data.
      if (!authWarned) {
        authWarned = true;
        toast.error('Authentication required — dashboard data cannot load.');
      }
      return;
    }
    if (!resp.ok) return; // transient failure: keep showing the last state
    text = await resp.text();
  } catch {
    // Network drop (daemon restart, offline): keep the last state.
    return;
  }
  if (seq <= adoptedSeq) return; // outpaced while the body streamed
  authWarned = false;
  // Identical payload: nothing changed server-side, skip the DOM rebuild.
  // Still adopted: an older in-flight response must not later repaint a
  // transient state on top of this confirmation.
  if (text === lastDashboardPayload) { adoptedSeq = seq; return; }
  let d;
  try {
    d = JSON.parse(text);
  } catch (err) {
    return; // 200 with a non-JSON body (misbehaving proxy): keep the last state
  }
  // A 200 with JSON of the wrong shape (same misbehaving-proxy class as
  // above, or server/client version skew) must not reach the renderers
  // half-applied: nothing is cached or painted from it, so a good
  // payload next tick renders normally.
  if (!Array.isArray(d.jobs) || !Array.isArray(d.disabled) || !Array.isArray(d.removed)) return;
  adoptedSeq = seq;
  jobsGuard.apply(d);
  // Cached only after the jobs render returned: cached before, a
  // payload whose render throws would poison every later timezone
  // repaint (which re-renders from this cache) on top of the failing
  // poll. Only the sections the repaint reads are kept — retaining
  // d.history here would pin a chatty job's multi-megabyte runs array
  // long after the modal-close cleanup released historyCache.
  dashboardCache = { jobs: d.jobs, disabled: d.disabled, removed: d.removed };
  removedGuard.apply(d.removed);
  configGuard.apply(d.config);
  // history is null when not requested (or the job vanished) and an
  // array — possibly empty, meaning "render the empty state" — when the
  // job exists. Adopt through the history sequence domain so a stale
  // rider can never repaint over a fresher direct load, and vice versa
  // (see historyReqSeq).
  if (historyFor && historyFor === selectedJob &&
      historySeq > historyAdoptedSeq && Array.isArray(d.history)) {
    historyAdoptedSeq = historySeq;
    historyGuard.apply(d.history);
  }
  if (historyFor === selectedJob) adoptHistoryFingerprint(historyFor, d.historyFingerprint);
  // Recorded last, after every section render returned: recorded any
  // earlier, a section render that throws would mark the payload as
  // painted and the identical-payload short-circuit above would pin
  // that section stale for every future identical tick.
  lastDashboardPayload = text;
}
refresh();
/* A hidden tab stops polling entirely; coming back refreshes at once
   and resumes the interval, so returning users never see stale data. */
let pollTimer = setInterval(refresh, 5000);
document.addEventListener('visibilitychange', () => {
  if (document.hidden) {
    clearInterval(pollTimer);
    pollTimer = null;
  } else if (!pollTimer) {
    refresh();
    pollTimer = setInterval(refresh, 5000);
  }
});

// Build version in the footer; fetched once, /health is auth-exempt.
fetch('/health').then(r => r.json()).then(d => {
  if (d?.version) document.getElementById('footer-version').textContent = d.version;
}).catch((err) => {console.error('Failed to fetch health:', err);});
