# Example Docker Compose Setup

This directory provides a minimal Compose project demonstrating several Ofelia job types.

## Usage

```sh
docker compose up
```

Ofelia reads the jobs defined in `ofelia.ini` and also uses Docker labels to configure jobs.

## Services

- **ofelia** – runs the scheduler with access to `ofelia.ini` and the Docker socket
- **nginx** – simple container with an `exec` job label that prints a message every hour

## Configured jobs

- `job-exec` via labels on the `nginx` service
- `job-run` defined in `ofelia.ini` to start an Alpine container printing the date
- `job-local` defined in `ofelia.ini` executing a command inside the Ofelia container
- `job-service-run` defined in `ofelia.ini` (requires Docker Swarm)
- `job-compose` defined in `ofelia.ini` triggering a Compose service

The `data/` directory is mounted so you can inspect the output of the run job.
