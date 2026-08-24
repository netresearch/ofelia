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

    function formatTime(dateStr) {
      const pref = localStorage.getItem('timezone') || 'local';
      if (dateStr === null || dateStr === undefined) return '';
      const str = String(dateStr);
      const dt = new Date(str);
      if (Number.isNaN(dt.getTime())) return '';
      if (pref === 'utc') return dt.toISOString().replaceAll('T',' ').replaceAll('Z','');
      if (pref === 'server') return str.replaceAll('T',' ').replaceAll('Z','');
      return dt.toLocaleString();
    }

    function stripControlChars(s) {
      if (!s) return '';
      // Strip Docker stream mux headers (8-byte prefix per frame) and other control chars
      return s.replace(/[\x00-\x08\x0e-\x1f]/g, '');
    }

    function escapeHtml(str) {
      if (!str && str !== 0) return '';
      return String(str).replaceAll('&', '&amp;').replaceAll('<', '&lt;')
        .replaceAll('>', '&gt;').replaceAll('"', '&quot;').replaceAll("'", '&#39;');
    }

    function statusDot(failed, skipped) {
      if (failed) return '<span class="dot dot-fail" title="Failed"></span>';
      if (skipped) return '<span class="dot dot-skip" title="Skipped"></span>';
      return '<span class="dot dot-ok" title="Success"></span>';
    }

    /* ── State ── */
    let jobsData = {};
    let editing = null;
    let selectedJob = null;
    const tzSelect = document.getElementById('timezoneSelect');
    tzSelect.value = localStorage.getItem('timezone') || 'local';
    tzSelect.addEventListener('change', () => { localStorage.setItem('timezone', tzSelect.value); refresh(); });

    /* ── Theme: auto / light / dark ── */
    const themeCycle = ['auto', 'light', 'dark'];
    const themeText = { auto: 'Auto', light: 'Light', dark: 'Dark' };
    let currentTheme = localStorage.getItem('theme') || 'auto';
    const themeBtn = document.getElementById('themeBtn');

    function applyTheme(t) {
      currentTheme = t;
      localStorage.setItem('theme', t);
      if (t === 'auto') {
        delete document.documentElement.dataset.theme;
      } else {
        document.documentElement.dataset.theme = t;
      }
      themeBtn.textContent = themeText[t];
      themeBtn.title = 'Theme: ' + themeText[t];
    }
    themeBtn.addEventListener('click', () => {
      applyTheme(themeCycle[(themeCycle.indexOf(currentTheme) + 1) % themeCycle.length]);
    });
    applyTheme(currentTheme);

    /* ── Density: auto / compact / comfortable ── */
    const densityCycle = ['auto', 'compact', 'comfortable'];
    const densityText = { auto: 'Auto', compact: 'Compact', comfortable: 'Comfy' };
    let currentDensity = localStorage.getItem('density') || 'auto';
    const densityBtn = document.getElementById('densityBtn');

    /* Detect if device prefers comfortable: touch screen, narrow viewport, or prefers-reduced-motion */
    function systemWantsComfortable() {
      return globalThis.matchMedia('(pointer: coarse)').matches ||
             globalThis.matchMedia('(max-width: 600px)').matches ||
             globalThis.matchMedia('(prefers-reduced-motion: reduce)').matches;
    }

    function applyDensity(d) {
      currentDensity = d;
      localStorage.setItem('density', d);
      let effective = d;
      if (d === 'auto') effective = systemWantsComfortable() ? 'comfortable' : 'compact';
      document.body.classList.toggle('compact', effective === 'compact');
      document.documentElement.classList.remove('compact-early');
      densityBtn.textContent = densityText[d];
      densityBtn.title = 'Density: ' + densityText[d] + (d === 'auto' ? ' (' + effective + ')' : '');
    }
    densityBtn.addEventListener('click', () => {
      applyDensity(densityCycle[(densityCycle.indexOf(currentDensity) + 1) % densityCycle.length]);
    });
    applyDensity(currentDensity);
    /* Re-evaluate auto density on resize */
    globalThis.matchMedia('(max-width: 600px)').addEventListener('change', () => {
      if (currentDensity === 'auto') applyDensity('auto');
    });

    /* ── Jobs table (merged: active + disabled) ── */
    async function loadJobs() {
      const [activeResp, disabledResp] = await Promise.all([fetch('/api/jobs'), fetch('/api/jobs/disabled')]);
      const active = await activeResp.json();
      const disabled = await disabledResp.json();
      const tbody = document.querySelector('#jobs tbody');
      tbody.innerHTML = '';
      jobsData = {};

      const all = [
        ...active.map(j => ({...j, _disabled: false})),
        ...disabled.map(j => ({...j, _disabled: true}))
      ];

      document.getElementById('badgeJobs').textContent = all.length;
      document.getElementById('jobCount').textContent = `${active.length} active` + (disabled.length ? ` · ${disabled.length} disabled` : '');

      if (all.length === 0) {
        tbody.innerHTML = '<tr><td colspan="7" class="empty">No jobs configured.</td></tr>';
        return;
      }

      all.forEach(j => {
        jobsData[j.name] = j;
        const tr = document.createElement('tr');
        tr.dataset.jobName = j.name;
        if (j.name === selectedJob) tr.classList.add('selected');

        let dot, lastRun, dur;
        if (j._disabled) {
          dot = '<span class="dot dot-disabled" title="Disabled"></span>';
          lastRun = ''; dur = '';
        } else if (j.lastRun) {
          dot = statusDot(j.lastRun.failed, j.lastRun.skipped);
          lastRun = formatTime(j.lastRun.date);
          dur = formatDuration(j.lastRun.duration);
        } else {
          dot = '<span class="dot dot-none" title="Never run"></span>';
          lastRun = ''; dur = '';
        }

        const eName = escapeHtml(j.name);
        const name = j._disabled ? `<s class="disabled-text">${eName}</s>` : eName;
        const eSched = escapeHtml(j.schedule);
        const sched = j._disabled ? `<s class="disabled-text">${eSched}</s>` : eSched;
        const eCmd = escapeHtml(j.command);

        let actions;
        if (j._disabled) {
          actions = `<div class="actions">` +
            `<button data-action="enable" data-job="${eName}" aria-label="Enable ${eName}" title="Enable">&#x23EF;</button>` +
            `<button data-action="edit" data-job="${eName}" aria-label="Edit ${eName}" title="Edit">&#x270E;</button>` +
            `</div>`;
        } else {
          actions = `<div class="actions">` +
            `<button data-action="run" data-job="${eName}" aria-label="Run ${eName}" title="Run">&#x25B6;</button>` +
            `<button data-action="edit" data-job="${eName}" aria-label="Edit ${eName}" title="Edit">&#x270E;</button>` +
            `<button data-action="disable" data-job="${eName}" aria-label="Disable ${eName}" title="Disable">&#x23F8;</button>` +
            `<button class="secondary outline" data-action="delete" data-job="${eName}" aria-label="Delete ${eName}" title="Delete">&#x1F5D1;</button>` +
            `</div>`;
        }

        tr.innerHTML = `<td>${dot}</td><td>${name}</td><td class="mono">${sched}</td><td class="mono wrap">${eCmd}</td>` +
          `<td>${lastRun}</td><td>${dur}</td><td>${actions}</td>`;
        tbody.appendChild(tr);
      });
    }

    /* ── History ── */
    async function loadHistory(name) {
      selectedJob = name;
      document.getElementById('historyJob').textContent = name;
      const panel = document.getElementById('historyPanel');
      panel.classList.add('visible');
      // Highlight selected row
      document.querySelectorAll('#jobs tbody tr').forEach(r => {
        r.classList.toggle('selected', r.dataset.jobName === name);
      });

      const resp = await fetch(`/api/jobs/${name}/history`);
      const hist = await resp.json();
      const tbody = document.querySelector('#history tbody');
      // Preserve which outputs the user has expanded so the periodic refresh
      // does not collapse them (keyed by run timestamp, unique per execution).
      // Scoped to the job the table currently shows: on a job switch these rows
      // still belong to the previously selected job, and its keys must not
      // decide what is expanded in another job's history.
      const openOutputs = new Set(
        tbody.dataset.job === name
          ? Array.from(tbody.querySelectorAll('details[open]')).map(d => d.dataset.key)
          : []
      );
      tbody.dataset.job = name;
      tbody.innerHTML = '';
      if (hist.length === 0) {
        tbody.innerHTML = '<tr><td colspan="5" class="empty">No history yet.</td></tr>';
        return;
      }
      hist.forEach(e => {
        const row = document.createElement('tr');
        const dot = statusDot(e.failed, e.skipped);
        const err = escapeHtml(e.error || '');
        const stdout = escapeHtml(stripControlChars(e.stdout));
        const stderr = escapeHtml(stripControlChars(e.stderr));
        const hasOut = stdout || stderr;
        const stdoutBlock = stdout ? `<pre>${stdout}</pre>` : '';
        const stderrBlock = stderr ? `<pre style="color:var(--pico-del-color)">${stderr}</pre>` : '';
        const key = String(e.date);
        const openAttr = openOutputs.has(key) ? ' open' : '';
        const output = hasOut ?
          `<details data-key="${escapeHtml(key)}"${openAttr}><summary>view</summary>` + stdoutBlock + stderrBlock + `</details>` : '';
        row.innerHTML = `<td>${dot}</td><td>${formatTime(e.date)}</td><td>${formatDuration(e.duration)}</td>` +
          `<td class="wrap">${err}</td><td>${output}</td>`;
        tbody.appendChild(row);
      });
    }

    function closeHistory() {
      selectedJob = null;
      document.getElementById('historyPanel').classList.remove('visible');
      document.querySelectorAll('#jobs tbody tr').forEach(r => r.classList.remove('selected'));
    }

    /* ── Removed ── */
    async function loadRemoved() {
      const resp = await fetch('/api/jobs/removed');
      const jobs = await resp.json();
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

      jobs.forEach(j => {
        const tr = document.createElement('tr');
        let dot, lastRun, dur;
        if (j.lastRun) {
          dot = statusDot(j.lastRun.failed, j.lastRun.skipped);
          lastRun = formatTime(j.lastRun.date);
          dur = formatDuration(j.lastRun.duration);
        } else {
          dot = '<span class="dot dot-none"></span>';
          lastRun = ''; dur = '';
        }
        tr.innerHTML = `<td>${dot}</td><td>${escapeHtml(j.name)}</td><td class="mono">${escapeHtml(j.schedule)}</td><td class="mono wrap">${escapeHtml(j.command)}</td><td>${lastRun}</td><td>${dur}</td>`;
        tbody.appendChild(tr);
      });
    }

    /* ── Config ── */
    function renderConfigTable(cfg) {
      const tbody = document.querySelector('#configTable tbody');
      tbody.innerHTML = '';
      function addRow(k, v) {
        const tr = document.createElement('tr');
        tr.innerHTML = `<td class="mono">${escapeHtml(k)}</td><td>${escapeHtml(v)}</td>`;
        tbody.appendChild(tr);
      }
      function traverse(o, prefix) {
        for (const [k, v] of Object.entries(o)) {
          if (['ExecJobs','RunJobs','LabelRunJobs','ServiceJobs','LocalJobs'].includes(k)) continue;
          if (v === null || (typeof v === 'object' && !Array.isArray(v) && Object.keys(v).length === 0)) continue;
          if (typeof v === 'object' && !Array.isArray(v)) { traverse(v, prefix + k + '.'); }
          else { addRow(prefix + k, v); }
        }
      }
      traverse(cfg, '');
    }

    async function loadConfig() {
      const resp = await fetch('/api/config');
      const cfg = await resp.json();
      renderConfigTable(cfg);
    }

    /* ── Actions ── */
    async function runJob(name) {
      await fetch('/api/jobs/run', {method:'POST', body:JSON.stringify({name}), headers:{'Content-Type':'application/json'}});
      refresh();
    }
    async function disableJob(name) {
      await fetch('/api/jobs/disable', {method:'POST', body:JSON.stringify({name}), headers:{'Content-Type':'application/json'}});
      refresh();
    }
    async function enableJob(name) {
      await fetch('/api/jobs/enable', {method:'POST', body:JSON.stringify({name}), headers:{'Content-Type':'application/json'}});
      refresh();
    }
    async function deleteJob(name) {
      await fetch('/api/jobs/delete', {method:'POST', body:JSON.stringify({name}), headers:{'Content-Type':'application/json','X-Origin':'web'}});
      refresh();
    }

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
      const name = document.getElementById('jobName').value;
      const type = document.getElementById('jobType').value;
      const schedule = document.getElementById('jobSchedule').value;
      const command = document.getElementById('jobCommand').value;
      const image = document.getElementById('jobImage').value;
      const container = document.getElementById('jobContainer').value;
      const file = document.getElementById('jobFile').value;
      const service = document.getElementById('jobService').value;
      const execEl = document.getElementById('jobExec');
      const exec = execEl ? execEl.checked : false;
      const url = editing === name ? '/api/jobs/update' : '/api/jobs/create';
      await fetch(url, {method:'POST', body:JSON.stringify({name,type,schedule,command,image,container,file,service,exec}), headers:{'Content-Type':'application/json','X-Origin':'web'}});
      editing = null;
      resetForm();
      switchTab('jobs');
      refresh();
    });

    function editJob(name) {
      const j = jobsData[name];
      if (!j) return;
      document.getElementById('jobName').value = j.name;
      const typeSel = document.getElementById('jobType');
      typeSel.value = Array.from(typeSel.options).map(o=>o.value).includes(j.type) ? j.type : 'local';
      document.getElementById('jobSchedule').value = j.schedule;
      document.getElementById('jobCommand').value = j.command;
      document.getElementById('jobImage').value = j.config.Image || '';
      document.getElementById('jobContainer').value = j.config.Container || '';
      document.getElementById('jobFile').value = j.config.File || '';
      document.getElementById('jobService').value = j.config.Service || '';
      const execEl = document.getElementById('jobExec');
      if (execEl) execEl.checked = j.config.Exec || false;
      editing = name;
      updateFormChrome();
      updateTypeFields();
      switchTab('form');
    }

    function resetForm() {
      document.getElementById('jobForm').reset();
      editing = null;
      updateFormChrome();
      updateTypeFields();
    }

    /* ── Event delegation for jobs table ── */
    document.querySelector('#jobs tbody').addEventListener('click', (e) => {
      const btn = e.target.closest('button[data-action]');
      if (btn) {
        const action = btn.dataset.action;
        const name = btn.dataset.job;
        if (action === 'run') runJob(name);
        else if (action === 'edit') editJob(name);
        else if (action === 'disable') disableJob(name);
        else if (action === 'delete') deleteJob(name);
        else if (action === 'enable') enableJob(name);
        return;
      }
      const tr = e.target.closest('tr[data-job-name]');
      if (tr) loadHistory(tr.dataset.jobName);
    });
    document.getElementById('closeHistoryBtn').addEventListener('click', closeHistory);
    document.getElementById('formClearBtn').addEventListener('click', resetForm);

    /* ── Refresh ── */
    function refresh() {
      loadJobs(); loadRemoved(); loadConfig();
      if (selectedJob) loadHistory(selectedJob);
    }
    refresh();
    setInterval(refresh, 5000);
