- **One invalid job no longer drops the whole INI file at boot.** A
  `[job-run]` section without `image` or `container`, or a
  `[job-service-run]` without `image`, failed the file: the daemon ran without
  any INI job and without `[global]`, and did not read the file again after it
  was fixed. Such a job is now left unregistered with its error logged, as for
  labels and the web API, and the other jobs and `[global]` load. When the
  file does not load at all, the daemon reads it again once it is fixed.
- **`ofelia validate` and strict validation accept the cron syntax the
  scheduler accepts.** They refused `L`, `W`, `#n`, `H` and a year field,
  which the scheduler and the documentation support; with strict validation
  such a schedule made the whole file fail. They now parse with the
  scheduler's parser.
- **`ofelia validate` and strict validation accept a `[job-compose]` without
  `file`.** The daemon runs it with `compose.yml`, but validation demanded the
  key.
