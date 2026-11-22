- **A `#` or `;` inside an INI value is part of the value.** It starts a
  comment only after a space. Before, `schedule = 0 12 * * FRI#3` ("third
  Friday") was read as `0 12 * * FRI` ("every Friday"), and
  `command = echo a;echo b` as `echo a`; labels and the web API kept both
  values whole. A value that relied on `#` or `;` without a space, or after
  a tab, to start a comment now keeps the rest of the line
  ([#980](https://github.com/netresearch/ofelia/pull/980)).
- **Jobs that can never run are refused at load.** A `job-exec` without
  `container` or `command`, a `job-local` without `command`, and a
  `job-compose` with an invalid service name or a file in a sensitive
  directory were registered and failed on every run. They are now left
  unregistered on every entry path, including jobs restored from the state
  file, with the error logged and the job listed in the health check as not
  scheduled; `ofelia validate` reports them too. A reload that turns a running
  job into such a definition keeps the running job and reports the refused
  one. A state-file job refused at restore can be removed with
  `POST /api/jobs/delete` ([#980](https://github.com/netresearch/ofelia/pull/980)).
