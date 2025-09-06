# Ofelia Configuration Migration Guide

## Overview

This guide helps developers migrate from the legacy configuration system to the new unified architecture while maintaining backward compatibility.

## For End Users (No Changes Required)

**✅ INI Configuration Files**: Work unchanged  
**✅ Docker Labels**: Work unchanged  
**✅ Command Line Interface**: Works unchanged  
**✅ Web UI**: Works unchanged  

The refactoring is **internal only** - all external interfaces remain compatible.

## For Developers

### Architecture Changes

#### Legacy System (Before)
```go
type Config struct {
    ExecJobs    map[string]*ExecJobConfig
    RunJobs     map[string]*RunJobConfig  
    ServiceJobs map[string]*RunServiceConfig
    LocalJobs   map[string]*LocalJobConfig
    ComposeJobs map[string]*ComposeJobConfig
    // ... 5 separate job maps
}
```

#### New Unified System (After)
```go
type UnifiedConfig struct {
    configManager *config.UnifiedConfigManager  // Single manager
    parser        *config.ConfigurationParser   // Unified parsing
    // ... rest remains compatible
}
```

### Code Migration Examples

#### 1. Job Creation

**Legacy Approach:**
```go
// Create different job types separately
execJob := &ExecJobConfig{
    ExecJob: core.ExecJob{
        BareJob: core.BareJob{Name: "test", Schedule: "@every 5s"},
        Container: "test-container",
    },
    OverlapConfig: middlewares.OverlapConfig{NoOverlap: true},
    SlackConfig: middlewares.SlackConfig{SlackWebhook: "http://example.com"},
    // ... duplicate middleware configs
}

runJob := &RunJobConfig{
    RunJob: core.RunJob{
        BareJob: core.BareJob{Name: "test2", Schedule: "@every 10s"},
        Image: "busybox",
    },
    OverlapConfig: middlewares.OverlapConfig{NoOverlap: true},  // Duplicated!
    SlackConfig: middlewares.SlackConfig{SlackWebhook: "http://example.com"}, // Duplicated!
    // ... same middleware configs repeated
}
```

**New Unified Approach:**
```go
import "github.com/netresearch/ofelia/cli/config"

// Create unified job configurations
execJob := config.NewUnifiedJobConfig(config.JobTypeExec)
execJob.ExecJob.Name = "test"
execJob.ExecJob.Schedule = "@every 5s" 
execJob.ExecJob.Container = "test-container"
execJob.MiddlewareConfig.OverlapConfig.NoOverlap = true
execJob.MiddlewareConfig.SlackConfig.SlackWebhook = "http://example.com"

runJob := config.NewUnifiedJobConfig(config.JobTypeRun)
runJob.RunJob.Name = "test2"
runJob.RunJob.Schedule = "@every 10s"
runJob.RunJob.Image = "busybox"
// Middleware config is shared - no duplication!
runJob.MiddlewareConfig = execJob.MiddlewareConfig
```

#### 2. Job Management

**Legacy Approach:**
```go
// Add jobs to different maps
config.ExecJobs["test"] = execJob
config.RunJobs["test2"] = runJob

// Count jobs across all maps  
total := len(config.ExecJobs) + len(config.RunJobs) + 
         len(config.ServiceJobs) + len(config.LocalJobs) + 
         len(config.ComposeJobs)

// Find job by searching all maps
var foundJob interface{}
if job, exists := config.ExecJobs["test"]; exists {
    foundJob = job
} else if job, exists := config.RunJobs["test"]; exists {
    foundJob = job
}
// ... check all 5 maps
```

**New Unified Approach:**
```go
// Add jobs through manager
configManager.AddJob("test", execJob)
configManager.AddJob("test2", runJob)

// Simple operations
total := configManager.GetJobCount()
job, exists := configManager.GetJob("test")

// Type-based queries
execJobs := configManager.ListJobsByType(config.JobTypeExec)
typeCounts := configManager.GetJobCountByType()
```

#### 3. Middleware Building

**Legacy Approach:**
```go
// Duplicate buildMiddlewares methods for each job type
func (c *ExecJobConfig) buildMiddlewares() {
    c.ExecJob.Use(middlewares.NewOverlap(&c.OverlapConfig))
    c.ExecJob.Use(middlewares.NewSlack(&c.SlackConfig))
    c.ExecJob.Use(middlewares.NewSave(&c.SaveConfig))
    c.ExecJob.Use(middlewares.NewMail(&c.MailConfig))
}

func (c *RunJobConfig) buildMiddlewares() {
    c.RunJob.Use(middlewares.NewOverlap(&c.OverlapConfig))    // Same code!
    c.RunJob.Use(middlewares.NewSlack(&c.SlackConfig))        // Same code!  
    c.RunJob.Use(middlewares.NewSave(&c.SaveConfig))          // Same code!
    c.RunJob.Use(middlewares.NewMail(&c.MailConfig))          // Same code!
}
// ... 5 identical methods
```

**New Unified Approach:**
```go
// Single centralized middleware building
builder := config.NewMiddlewareBuilder()
builder.BuildMiddlewares(job.GetCoreJob(), &job.MiddlewareConfig)

// Or use the built-in method
job.buildMiddlewares() // Automatically calls centralized builder
```

### Backward Compatibility

#### Gradual Migration Strategy

**Phase 1: Keep existing code working**
```go
// Existing code continues to work unchanged
config := cli.BuildFromFile("config.ini", logger)
config.InitializeApp()

// Access jobs through legacy interfaces
execJob := config.ExecJobs["my-job"]
execJob.buildMiddlewares()
```

**Phase 2: Introduce unified config alongside legacy**
```go
// Create unified config from legacy 
legacyConfig := cli.BuildFromFile("config.ini", logger) 
unifiedConfig := cli.NewUnifiedConfig(logger)
unifiedConfig.FromLegacyConfig(legacyConfig)

// Use unified features
jobCount := unifiedConfig.GetJobCount()
jobs := unifiedConfig.ListJobsByType(config.JobTypeExec)
```

**Phase 3: Convert to unified approach**
```go
// Pure unified approach
unifiedConfig := cli.NewUnifiedConfig(logger)
unifiedConfig.InitializeApp()

// Direct job management
job := config.NewUnifiedJobConfig(config.JobTypeExec)
unifiedConfig.AddJob("my-job", job)
```

#### Conversion Utilities

**Legacy to Unified:**
```go
import "github.com/netresearch/ofelia/cli/config"

// Convert individual jobs
execJob := &ExecJobConfig{...}
unifiedJob := config.ConvertFromExecJobConfig(execJob)

// Convert entire job maps
unifiedJobs := config.ConvertLegacyJobMaps(
    config.ExecJobs, config.RunJobs, config.ServiceJobs, 
    config.LocalJobs, config.ComposeJobs)
```

**Unified to Legacy:**
```go
// Convert back for compatibility
unifiedJob := &config.UnifiedJobConfig{...}
legacyJob := config.ConvertToExecJobConfig(unifiedJob)

// Convert entire config
unifiedConfig := &UnifiedConfig{...}
legacyConfig := unifiedConfig.ToLegacyConfig()
```

### Testing Migration

#### Legacy Tests (Still Work)
```go
func TestLegacyConfig(t *testing.T) {
    config, err := cli.BuildFromString(`
        [job-exec "test"]
        schedule = @every 10s
        command = echo test
    `, logger)
    
    // Legacy access patterns still work
    assert.Equal(t, 1, len(config.ExecJobs))
    assert.NotNil(t, config.ExecJobs["test"])
}
```

#### New Unified Tests
```go
func TestUnifiedConfig(t *testing.T) {
    unifiedConfig := cli.NewUnifiedConfig(logger)
    
    job := config.NewUnifiedJobConfig(config.JobTypeExec)
    job.ExecJob.Name = "test"
    job.ExecJob.Schedule = "@every 10s"
    
    err := unifiedConfig.configManager.AddJob("test", job)
    assert.NoError(t, err)
    assert.Equal(t, 1, unifiedConfig.GetJobCount())
}
```

### Common Migration Patterns

#### 1. Job Iteration

**Legacy:**
```go
// Iterate through all job types
for name, job := range config.ExecJobs {
    processJob(name, job)
}
for name, job := range config.RunJobs {
    processJob(name, job)
}
// ... repeat for all 5 types
```

**Unified:**
```go
// Single iteration
for name, job := range configManager.ListJobs() {
    processJob(name, job)
}

// Or type-specific
execJobs := configManager.ListJobsByType(config.JobTypeExec)
for name, job := range execJobs {
    processExecJob(name, job)
}
```

#### 2. Configuration Validation

**Legacy:**
```go
func validateConfig(c *Config) error {
    // Validate each job type separately
    for _, job := range c.ExecJobs {
        if err := validateExecJob(job); err != nil {
            return err
        }
    }
    for _, job := range c.RunJobs {
        if err := validateRunJob(job); err != nil {
            return err
        }
    }
    // ... validate all 5 types
}
```

**Unified:**
```go
func validateUnifiedConfig(uc *UnifiedConfig) error {
    jobs := uc.ListJobs()
    for name, job := range jobs {
        if err := validateUnifiedJob(name, job); err != nil {
            return err
        }
    }
}

func validateUnifiedJob(name string, job *config.UnifiedJobConfig) error {
    switch job.Type {
    case config.JobTypeExec:
        return validateExecJob(job.ExecJob)
    case config.JobTypeRun:
        return validateRunJob(job.RunJob)
    // ... handle all types in one place
    }
}
```

### Performance Considerations

#### Memory Usage
- **Before**: ~5x memory overhead from duplicate structures
- **After**: Single unified structures with shared configuration

#### CPU Usage  
- **Before**: 5 separate loops for job operations
- **After**: Single loop with type switching

### Troubleshooting

#### Common Issues

**Issue**: "Cannot find job in ExecJobs map"
```go
// Legacy code looking in wrong map
job, exists := config.ExecJobs["my-run-job"] // Wrong map!

// Solution: Use unified manager
job, exists := unifiedConfig.GetJob("my-run-job")
```

**Issue**: "Middleware not applied to job"  
```go
// Legacy: Forgetting to call buildMiddlewares
job := &ExecJobConfig{...}
// Missing: job.buildMiddlewares()

// Unified: Automatic middleware building
job := config.NewUnifiedJobConfig(config.JobTypeExec)
configManager.AddJob("test", job) // Automatically builds middlewares
```

**Issue**: "Job type casting errors"
```go
// Legacy: Manual type assertions
if execJob, ok := job.(*ExecJobConfig); ok {
    // Process exec job
}

// Unified: Type-safe access
if job.Type == config.JobTypeExec {
    execJob := job.ExecJob // Type-safe access
}
```

## Next Steps

1. **Review**: Understand the new architecture
2. **Test**: Run existing tests to verify compatibility  
3. **Migrate**: Gradually adopt unified patterns
4. **Optimize**: Leverage new features for better performance
5. **Extend**: Use unified system for new features

The unified configuration system provides a solid foundation for future development while maintaining full backward compatibility.