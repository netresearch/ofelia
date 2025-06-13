# Config Refactor Proposal

This document outlines a possible redesign of the configuration layer to reduce
duplication between INI files and Docker label parsing. It also discusses the
relationship between the `exec` and `run` job types.

## Unify configuration sources

Both INI-based configuration and label-based configuration are parsed into
different maps today (`ExecJobs` vs `LabelExecJobs`, `RunJobs` vs
`LabelRunJobs`, etc.). The job structures are identical; only the source differs.

### Proposed changes

* Introduce a `JobSource` field (e.g. `"ini"` or `"label"`) in every job
  configuration struct.
* Keep a single map of jobs per type. Jobs parsed from labels are stored in the
  same map as INI jobs, but with `JobSource` set accordingly. Scheduler updates
  then operate on this unified map, removing the repeated loops.
* Extend the existing update logic (`syncJobMap`) to respect the `JobSource`
  flag so jobs originating from labels can be removed when the container
  disappears while INI jobs stay persistent.
  This cleanup now also covers `local` and `service` jobs so stale
  entries are removed when their containers vanish.
* `Config.BuildFromFile` and `buildFromDockerLabels` would populate these maps
  in the same way; the only difference is the value of `JobSource`.
* When merging label-based jobs during startup, INI-defined jobs take
  precedence. If a label reuses an existing job name, the label job is skipped
  and a warning is logged. Runtime reloads use the same precedence: an updated
  INI job replaces any label job with the same name.
  This applies to all job types including `compose` jobs.

This approach removes a large portion of repeated code and simplifies the update
path in `iniConfigUpdate` and `dockerLabelsUpdate`.

## Exec vs Run jobs

`exec` and `run` serve different use cases:

* `exec` runs a command inside an existing container.
* `run` starts a new container (or restarts a stopped one) for each execution and
  handles lifecycle options such as pulling images and deleting containers.

The underlying logic and Docker API calls differ significantly. While some
fields like `user` or `environment` overlap, merging them into a single struct
would complicate option handling and make the behaviour less explicit.

### Recommendation

Keep `ExecJob` and `RunJob` as separate implementations but consider extracting
shared pieces (e.g. log collection, runtime limits) into helper functions or a
common embedded type. This provides clarity while still reducing duplication.

