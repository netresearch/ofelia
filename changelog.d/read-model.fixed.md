- **Deleting an API job hands its name back to the INI or label job it
  replaced.** When a job from the state file or the web API held the name of
  an INI or label job, deleting it through the API left the configured job
  unregistered: nothing ran under the name, the API refused the name with
  `403`, and only a change to the job's definition or a restart registered it
  again. The configured job is now registered as soon as the API job is
  deleted ([#978](https://github.com/netresearch/ofelia/pull/978)).
- **A label reload and an INI reload, or a reload and a web API update, no
  longer race.** A label reload read the `[global]` settings without the
  configuration lock while an INI reload replaced them, and a reload replaced
  the scheduler's global middlewares while an API update or a registration
  read them ([#978](https://github.com/netresearch/ofelia/pull/978)).
