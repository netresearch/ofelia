# CLI Package Documentation

## Overview
The `cli` package handles command-line interface, configuration management, and Docker label-based job discovery.

## Key Components

### Configuration Management

#### Config Structure
Central configuration holder for all job types and global settings.

```go
type Config struct {
    ExecJobs     map[string]*ExecJobConfig
    RunJobs      map[string]*RunJobConfig
    LocalJobs    map[string]*LocalJobConfig
    ServiceJobs  map[string]*ServiceJobConfig
    ComposeJobs  map[string]*ComposeJobConfig
    
    Global       GlobalConfig
    Middlewares  map[string]map[string]interface{}
}
```

#### GlobalConfig
System-wide settings and defaults.

```go
type GlobalConfig struct {
    SlackURL     string
    SlackChannel string
    EmailFrom    string
    EmailTo      string
    SMTPHost     string
    SMTPPort     int
    SMTPUser     string
    SMTPPassword string
    SaveFolder   string
    
    // Docker settings
    DockerHost              string
    DockerPollInterval      time.Duration
    DockerEvents            bool
    AllowHostJobsFromLabels bool
    
    // Web UI settings
    EnableWeb    bool
    WebAddress   string
    
    // Monitoring
    EnablePprof  bool
    PprofAddress string
}
```

### Configuration Sources

#### INI File Configuration
Traditional file-based configuration.

```go
func BuildFromFile(path string) (*Config, error) {
    config := &Config{}
    cfg, err := ini.LoadSources(ini.LoadOptions{}, path)
    // Parse sections and build jobs
    return config, nil
}
```

**INI Format:**
```ini
[global]
slack-url = https://hooks.slack.com/...
docker-events = true

[job-exec "database-backup"]
schedule = @midnight
container = postgres
command = pg_dump mydb > /backup/db.sql

[job-run "cleanup"]
schedule = 0 2 * * *
image = alpine:latest
command = find /tmp -mtime +7 -delete
```

#### Docker Labels Configuration
Dynamic configuration from container labels.

```go
func BuildFromDockerContainers(client *docker.Client) (*Config, error) {
    containers, err := client.ListContainers()
    for _, container := range containers {
        labels := container.Labels
        // Parse ofelia.* labels
        // Create job configurations
    }
    return config, nil
}
```

**Label Format:**
```yaml
labels:
  ofelia.enabled: "true"
  ofelia.job-exec.backup.schedule: "0 2 * * *"
  ofelia.job-exec.backup.command: "backup.sh"
  ofelia.job-exec.backup.user: "root"
```

### Configuration Operations

#### Merging Configurations
Combines multiple configuration sources.

```go
func (c *Config) mergeConfig(parsedConfig *Config) {
    // Merge global settings
    mergeGlobalConfig(&c.Global, &parsedConfig.Global)
    
    // Merge job collections
    for name, job := range parsedConfig.ExecJobs {
        c.ExecJobs[name] = job
    }
    // ... merge other job types
}
```

#### Hash-based Change Detection
Detects configuration changes for dynamic updates.

```go
func (c *Config) dockerContainersUpdate(client *docker.Client) error {
    newConfig, err := BuildFromDockerContainers(client)
    
    for name, newJob := range newConfig.ExecJobs {
        if oldJob, exists := c.ExecJobs[name]; exists {
            oldHash, _ := oldJob.Hash()
            newHash, _ := newJob.Hash()
            if oldHash != newHash {
                // Update job configuration
            }
        }
    }
    return nil
}
```

### Commands

#### Daemon Command
Main scheduler daemon.

```go
func DaemonCommand(c *cli.Context) error {
    config := loadConfig(c)
    scheduler := core.NewScheduler(logger)
    
    // Add jobs to scheduler
    for _, job := range config.GetJobs() {
        scheduler.AddJob(job)
    }
    
    // Start monitoring
    if config.Global.DockerEvents {
        monitor := core.NewContainerMonitor(client, scheduler)
        go monitor.Start()
    }
    
    // Start scheduler
    scheduler.Start()
    
    // Wait for shutdown
    <-stopSignal
    scheduler.Stop()
}
```

#### Validate Command
Configuration validation without execution.

```go
func ValidateCommand(c *cli.Context) error {
    config, err := loadConfig(c)
    if err != nil {
        return fmt.Errorf("invalid configuration: %w", err)
    }
    
    validator := config2.NewConfigValidator(config)
    if err := validator.Validate(); err != nil {
        return fmt.Errorf("validation failed: %w", err)
    }
    
    fmt.Println("Configuration is valid")
    return nil
}
```

### Docker Integration

#### Label Parsing
Extracts job configuration from Docker labels.

```go
func parseJobLabels(labels map[string]string) map[string]JobConfig {
    jobs := make(map[string]JobConfig)
    
    for label, value := range labels {
        if !strings.HasPrefix(label, "ofelia.") {
            continue
        }
        
        parts := strings.Split(label, ".")
        // ofelia.job-type.job-name.property
        
        jobType := parts[1]
        jobName := parts[2]
        property := parts[3]
        
        job := getOrCreateJob(jobs, jobType, jobName)
        setJobProperty(job, property, value)
    }
    
    return jobs
}
```

#### Security Validation
Ensures label-based jobs meet security requirements.

```go
func validateLabelJob(job JobConfig, global GlobalConfig) error {
    // Check if host jobs are allowed
    if job.Type == "local" && !global.AllowHostJobsFromLabels {
        return errors.New("host jobs not allowed from labels")
    }
    
    // Validate command injection
    if containsShellMetacharacters(job.Command) {
        return errors.New("potentially unsafe command")
    }
    
    return nil
}
```

## Usage Examples

### Loading Configuration

```go
// From INI file
config, err := BuildFromFile("/etc/ofelia/config.ini")

// From Docker labels
client := docker.NewClient("unix:///var/run/docker.sock")
config, err := BuildFromDockerContainers(client)

// Merged configuration
fileConfig, _ := BuildFromFile("config.ini")
labelConfig, _ := BuildFromDockerContainers(client)
fileConfig.mergeConfig(labelConfig)
```

### Dynamic Updates

```go
// Monitor for configuration changes
ticker := time.NewTicker(30 * time.Second)
for range ticker.C {
    if err := config.dockerContainersUpdate(client); err != nil {
        log.Printf("Update failed: %v", err)
    }
}
```

### Custom Job Creation

```go
job := &RunJobConfig{
    BareJobConfig: BareJobConfig{
        Schedule: "@hourly",
        Command:  "process-data",
    },
    Image:       "myapp:latest",
    Environment: []string{"ENV=production"},
    Volumes:     []string{"/data:/data:ro"},
}

config.RunJobs["data-processor"] = job
```

## Configuration Precedence

1. Command-line flags (highest priority)
2. Environment variables
3. Configuration file
4. Docker labels (lowest priority)

## Testing

The package includes tests for:
- Configuration parsing
- Label extraction
- Job validation
- Hash-based change detection
- Security validation

## Best Practices

1. **Use INI files for static configuration**
2. **Use Docker labels for dynamic, container-specific jobs**
3. **Enable `docker-events` for real-time updates**
4. **Validate configuration before deployment**
5. **Use environment variables for sensitive data**

---
*See also: [Core Package](./core.md) | [Web Package](./web.md) | [Project Index](../PROJECT_INDEX.md)*
