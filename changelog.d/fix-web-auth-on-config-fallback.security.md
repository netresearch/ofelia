- **The web UI no longer starts without a login when the config file does
  not load.** With the web UI enabled by flag or environment and web auth set
  only in the config file, a config file that failed to load at startup left
  the web UI and API running without authentication. The daemon now keeps the
  web UI off in that case and logs why; the scheduler keeps running. Setting
  `web-auth-enabled` by flag or environment, or a config path that names no
  file at all in a readable directory, starts the web UI as before.
