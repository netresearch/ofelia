- **The web UI no longer starts without a login when the config does not
  load.** With the web UI enabled by flag or environment and web auth set
  only in the config file, a config that failed to load at startup left the
  web UI and API running without authentication. The daemon now keeps the
  web UI off in that case and logs why; the scheduler keeps running. Setting
  `web-auth-enabled` by flag or environment, or a plain config path (no glob)
  that does not exist or is a directory, starts the web UI as before.
