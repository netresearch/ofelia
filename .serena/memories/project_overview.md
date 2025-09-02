# Ofelia Project Overview

## Purpose
Ofelia is a job scheduler designed to orchestrate container tasks with minimal overhead, offering a sleek alternative to cron. It allows labeling Docker containers and scheduling jobs as a Go-powered daemon.

## Tech Stack
- **Language**: Go 1.25
- **Container Runtime**: Docker
- **Key Dependencies**:
  - Docker client: github.com/fsouza/go-dockerclient, github.com/docker/docker
  - Cron scheduler: github.com/robfig/cron/v3
  - Logging: github.com/sirupsen/logrus
  - Configuration: gopkg.in/ini.v1
  - CLI flags: github.com/jessevdk/go-flags
  - Web server: Built-in with embedded static files
  - Testing: gopkg.in/check.v1

## Key Features
- Multiple job types (exec, run, local, compose, service)
- Logging middlewares (Slack, StatsD, email)
- Dynamic Docker container detection
- Configuration validation
- Optional web UI with job management
- Optional pprof server for profiling
- Job history tracking

## Project Structure
- `/core`: Core business logic, job types, scheduler
- `/cli`: Command-line interface and daemon management
- `/middlewares`: Logging and notification middlewares
- `/web`: Web UI server implementation
- `/static`: Embedded static files for web UI
- `/docs`: Documentation
- `/example`: Example configurations
- `/test`: Integration tests

## Build System
- Uses Makefile for build automation
- Supports multi-platform builds (darwin, linux)
- GitHub Actions CI/CD pipeline
- golangci-lint for code quality