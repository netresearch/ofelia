# Ofelia Cross-Reference Index

## Quick Navigation

### By Feature
- [Job Scheduling](#job-scheduling)
- [Job Dependencies](#job-dependencies)
- [Docker Integration](#docker-integration)
- [Configuration](#configuration)
- [Monitoring & Metrics](#monitoring--metrics)
- [Security](#security)
- [API & Web UI](#api--web-ui)
- [Resilience](#resilience)

### By Component
- [Core Components](#core-components)
- [Job Types](#job-types)
- [Middlewares](#middlewares)
- [Configuration Sources](#configuration-sources)

## Job Scheduling

### Core Scheduler
- **Definition**: [`core/scheduler.go`](../core/scheduler.go)
- **Tests**: [`core/scheduler_test.go`](../core/scheduler_test.go)
- **Configuration**: [`cli/config.go#L120-L150`](../cli/config.go)
- **Documentation**: [Core Package Docs](./packages/core.md#scheduler)

### Cron Expression Parsing
- **Implementation**: [`core/cron_utils.go`](../core/cron_utils.go)
- **Tests**: [`core/cron_utils_test.go`](../core/cron_utils_test.go)
- **Configuration Examples**: [Configuration Guide](./CONFIGURATION.md#schedule-expressions)

## Job Dependencies

### Workflow Orchestrator
- **Implementation**: [`core/workflow.go`](../core/workflow.go)
- **Tests**: [`core/workflow_test.go`](../core/workflow_test.go)
- **Job Fields**: [`core/bare_job.go#L18-L20`](../core/bare_job.go) (`Dependencies`, `OnSuccess`, `OnFailure`)
- **Configuration**: [Job Dependencies Config](./CONFIGURATION.md#job-dependencies)

### Configuration Parsing
- **INI Config Tests**: [`cli/config_dependencies_test.go`](../cli/config_dependencies_test.go)
- **Struct Tags**: `gcfg:"depends-on"`, `mapstructure:"depends-on,"`

### Dependency Features
- **Execution Order**: Jobs wait for dependencies via `depends-on`
- **Success Triggers**: Jobs triggered on success via `on-success`
- **Failure Triggers**: Jobs triggered on failure via `on-failure`
- **Circular Detection**: Built-in circular dependency detection

## Job Types

### RunJob (New Container)
- **Implementation**: [`core/runjob.go`](../core/runjob.go)
- **Tests**: [`core/runjob_integration_test.go`](../core/runjob_integration_test.go)
- **Configuration**: [RunJob Config](./CONFIGURATION.md#runjob---execute-in-new-container)
- **Docker Operations**: [`core/docker_client.go#L180-L220`](../core/docker_client.go)

### ExecJob (Existing Container)
- **Implementation**: [`core/execjob.go`](../core/execjob.go)
- **Tests**: [`core/execjob_integration_test.go`](../core/execjob_integration_test.go)
- **Configuration**: [ExecJob Config](./CONFIGURATION.md#execjob---execute-in-existing-container)

### LocalJob (Host Execution)
- **Implementation**: [`core/localjob.go`](../core/localjob.go)
- **Tests**: [`core/localjob_test.go`](../core/localjob_test.go)
- **Security**: [`config/sanitizer.go#L70-L85`](../config/sanitizer.go)
- **Configuration**: [LocalJob Config](./CONFIGURATION.md#localjob---execute-on-host)

### ServiceJob (Swarm)
- **Implementation**: [`core/runservice.go`](../core/runservice.go)
- **Tests**: [`core/runservice_integration_test.go`](../core/runservice_integration_test.go)
- **Configuration**: [ServiceJob Config](./CONFIGURATION.md#servicejob---docker-swarm-service)

### ComposeJob
- **Implementation**: [`core/composejob.go`](../core/composejob.go)
- **Tests**: [`core/composejob_test.go`](../core/composejob_test.go)
- **Configuration**: [ComposeJob Config](./CONFIGURATION.md#composejob---docker-compose-operations)

## Docker Integration

### Docker Client
- **Wrapper**: [`core/docker_client.go`](../core/docker_client.go)
- **Tests**: [`core/docker_client_test.go`](../core/docker_client_test.go)
- **Container Monitor**: [`core/container_monitor.go`](../core/container_monitor.go)
- **Label Parser**: [`cli/docker-labels.go`](../cli/docker-labels.go)

### Container Monitoring
- **Event Listener**: [`core/container_monitor.go#L50-L100`](../core/container_monitor.go)
- **Polling Fallback**: [`core/container_monitor.go#L101-L150`](../core/container_monitor.go)
- **Tests**: [`core/container_monitor_test.go`](../core/container_monitor_test.go)
- **Metrics**: [`metrics/prometheus.go#L172-L194`](../metrics/prometheus.go)

## Configuration

### Configuration Loading
- **INI Parser**: [`cli/config.go#L50-L100`](../cli/config.go)
- **Label Parser**: [`cli/docker-labels.go#L30-L80`](../cli/docker-labels.go)
- **Environment Override**: [`cli/config.go#L200-L230`](../cli/config.go)
- **Tests**: [`cli/config_test.go`](../cli/config_test.go)

### Validation
- **Validator**: [`config/validator.go`](../config/validator.go)
- **Sanitizer**: [`config/sanitizer.go`](../config/sanitizer.go)
- **Tests**: [`config/validator_test.go`](../config/validator_test.go)
- **Validate Command**: [`cli/validate.go`](../cli/validate.go)

### Dynamic Updates
- **Hash Detection**: [`core/common.go#L350`](../core/common.go) → [`cli/config.go#L544-L551`](../cli/config.go)
- **Label Updates**: [`cli/config.go#L300-L350`](../cli/config.go)
- **Tests**: [`cli/config_extra_test.go`](../cli/config_extra_test.go)

## Monitoring & Metrics

### Prometheus Metrics
- **Collector**: [`metrics/prometheus.go`](../metrics/prometheus.go)
- **Tests**: [`metrics/prometheus_test.go`](../metrics/prometheus_test.go)
- **HTTP Handler**: [`metrics/prometheus.go#L241-L247`](../metrics/prometheus.go)
- **Job Metrics**: [`metrics/prometheus.go#L290-L344`](../metrics/prometheus.go)

### Structured Logging
- **Logger**: stdlib `log/slog` (used directly, no wrapper)
- **Logger construction**: [`ofelia.go#L21-L38`](../ofelia.go) (`buildLogger`)
- **Level management**: [`cli/logging.go`](../cli/logging.go) (`ApplyLogLevel`)

## Security

### JWT Authentication
- **Manager**: [`web/jwt_auth.go`](../web/jwt_auth.go)
- **Handlers**: [`web/jwt_handlers.go`](../web/jwt_handlers.go)
- **Tests**: [`web/jwt_auth_test.go`](../web/jwt_auth_test.go)
- **Middleware**: [`web/jwt_auth.go#L113-L150`](../web/jwt_auth.go)

### Input Validation
- **Command Sanitization**: [`config/sanitizer.go#L58-L85`](../config/sanitizer.go)
- **Path Validation**: [`config/sanitizer.go#L88-L120`](../config/sanitizer.go)
- **Docker Image Validation**: [`config/sanitizer.go#L180-L195`](../config/sanitizer.go)
- **URL Validation**: [`config/sanitizer.go#L140-L165`](../config/sanitizer.go)

## API & Web UI

### HTTP Server
- **Server**: [`web/server.go`](../web/server.go)
- **Routes**: [`web/server.go#L50-L150`](../web/server.go)
- **Tests**: [`web/server_test.go`](../web/server_test.go)
- **Static Files**: [`static/ui/`](../static/ui/)

### API Endpoints
- **Job Management**: [`web/server.go#L200-L300`](../web/server.go)
- **Health Checks**: [`web/health.go`](../web/health.go)
- **Authentication**: [`web/auth.go`](../web/auth.go)
- **Documentation**: [API Docs](./API.md)

### Middleware
- **HTTP Middleware**: [`web/middleware.go`](../web/middleware.go)
- **CORS**: [`web/middleware.go#L50-L80`](../web/middleware.go)
- **Rate Limiting**: [`core/resilience.go#L200-L250`](../core/resilience.go)
- **Tests**: [`web/middleware_test.go`](../web/middleware_test.go)

## Resilience

### Retry Logic
- **Retry Policy**: [`core/resilience.go#L13-L90`](../core/resilience.go)
- **Retry Executor**: [`core/retry.go`](../core/retry.go)
- **Tests**: [`core/retry_test.go`](../core/retry_test.go)

### Circuit Breaker
- **Implementation**: [`core/resilience.go#L92-L199`](../core/resilience.go)
- **Usage**: [`core/resilient_job.go#L108-L113`](../core/resilient_job.go)
- **States**: [Documentation](./packages/core.md#circuitbreaker)

### Rate Limiting
- **Token Bucket**: [`core/resilience.go#L202-L260`](../core/resilience.go)
- **Usage**: [`core/resilient_job.go#L89-L93`](../core/resilient_job.go)

### Bulkhead Pattern
- **Implementation**: [`core/resilience.go#L262-L300`](../core/resilience.go)
- **Usage**: [`core/resilient_job.go#L107-L115`](../core/resilient_job.go)

## Middlewares

### Email Notifications
- **Implementation**: [`middlewares/mail.go`](../middlewares/mail.go)
- **Tests**: [`middlewares/mail_test.go`](../middlewares/mail_test.go)
- **Configuration**: [Email Config](./CONFIGURATION.md#email-notifications)

### Slack Integration
- **Implementation**: [`middlewares/slack.go`](../middlewares/slack.go)
- **Tests**: [`middlewares/slack_test.go`](../middlewares/slack_test.go)
- **Configuration**: [Slack Config](./CONFIGURATION.md#slack-notifications)

### Output Persistence
- **Save Middleware**: [`middlewares/save.go`](../middlewares/save.go)
- **Tests**: [`middlewares/save_test.go`](../middlewares/save_test.go)
- **Configuration**: [Save Config](./CONFIGURATION.md#output-saving)

### Overlap Prevention
- **Implementation**: [`middlewares/overlap.go`](../middlewares/overlap.go)
- **Tests**: [`middlewares/overlap_test.go`](../middlewares/overlap_test.go)
- **Configuration**: [Overlap Config](./CONFIGURATION.md#overlap-prevention)

## Testing

### Test Utilities
- **Test Logger**: [`test/testlogger.go`](../test/testlogger.go)
- **Test Helpers**: [`cli/testhelpers_test.go`](../cli/testhelpers_test.go)
- **Integration Suite**: [`core/helpers_suite_test.go`](../core/helpers_suite_test.go)

### Benchmark Tests
- **Buffer Pool**: [`core/buffer_pool_benchmark_test.go`](../core/buffer_pool_benchmark_test.go)
- **Performance**: [`core/scheduler_test.go#L200-L250`](../core/scheduler_test.go)

## Key Workflows

### Job Execution Flow
1. **Schedule Trigger**: [`core/scheduler.go#L150`](../core/scheduler.go)
2. **Context Creation**: [`core/context.go#L42`](../core/context.go)
3. **Middleware Chain**: [`core/context.go#L68-L103`](../core/context.go)
4. **Job Execution**: [`core/runjob.go#L98`](../core/runjob.go)
5. **Metrics Recording**: [`metrics/prometheus.go#L305-L333`](../metrics/prometheus.go)
6. **Logging**: stdlib `log/slog` (used directly throughout)

### Configuration Update Flow
1. **Docker Event/Poll**: [`core/container_monitor.go#L50`](../core/container_monitor.go)
2. **Label Parsing**: [`cli/docker-labels.go#L30`](../cli/docker-labels.go)
3. **Hash Comparison**: [`cli/config.go#L544`](../cli/config.go)
4. **Job Update**: [`core/scheduler.go#L180`](../core/scheduler.go)

### Authentication Flow
1. **Login Request**: [`web/jwt_handlers.go#L20`](../web/jwt_handlers.go)
2. **Token Generation**: [`web/jwt_auth.go#L51`](../web/jwt_auth.go)
3. **Middleware Validation**: [`web/jwt_auth.go#L113`](../web/jwt_auth.go)
4. **Request Processing**: [`web/server.go#L200`](../web/server.go)

---
*Navigation: [Project Index](./PROJECT_INDEX.md) | [API Docs](./API.md) | [Configuration](./CONFIGURATION.md) | [Packages](./packages/)*