package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/netresearch/go-cron"

	"github.com/netresearch/ofelia/core"
)

// DoctorCommand runs comprehensive health checks on Ofelia configuration and environment
type DoctorCommand struct {
	ConfigFile string `long:"config" description:"Path to configuration file"`
	LogLevel   string `long:"log-level" env:"OFELIA_LOG_LEVEL" description:"Set log level"`
	JSON       bool   `long:"json" description:"Output results as JSON"`
	Logger     core.Logger

	// configAutoDetected tracks whether auto-detection was used (for error hints)
	configAutoDetected bool
}

// commonConfigPaths lists config file locations to search (in order of priority)
var commonConfigPaths = []string{
	"./ofelia.ini",
	"./config.ini",
	"/etc/ofelia/config.ini",
	"/etc/ofelia.ini",
}

// findConfigFile searches for a config file in common locations
func findConfigFile() string {
	for _, path := range commonConfigPaths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return "" // No config found
}

// Status constants for health check results.
const (
	statusPass = "pass"
	statusFail = "fail"
	statusSkip = "skip"
)

// CheckResult represents the result of a single health check
type CheckResult struct {
	Category string   `json:"category"`
	Name     string   `json:"name"`
	Status   string   `json:"status"` // "pass", "fail", "skip"
	Message  string   `json:"message,omitempty"`
	Hints    []string `json:"hints,omitempty"`
}

// DoctorReport contains all health check results
type DoctorReport struct {
	Healthy bool          `json:"healthy"`
	Checks  []CheckResult `json:"checks"`
}

// Execute runs all health checks
func (c *DoctorCommand) Execute(_ []string) error {
	if err := ApplyLogLevel(c.LogLevel); err != nil {
		c.Logger.Warningf("Failed to apply log level (using default): %v", err)
	}

	// Auto-detect config file if not specified
	if c.ConfigFile == "" {
		c.configAutoDetected = true
		if found := findConfigFile(); found != "" {
			c.ConfigFile = found
		} else {
			c.ConfigFile = "/etc/ofelia/config.ini" // Fallback for error messages
		}
	}

	report := &DoctorReport{
		Healthy: true,
		Checks:  []CheckResult{},
	}

	// Show progress only in non-JSON mode
	var progress *ProgressReporter
	if !c.JSON {
		c.Logger.Noticef("🏥 Running Ofelia Health Diagnostics...\n")
		totalSteps := 4 // config, docker, schedules, images
		progress = NewProgressReporter(c.Logger, totalSteps)
	}

	// Run all checks with progress feedback
	if progress != nil {
		progress.Step(1, "Checking configuration...")
	}
	c.checkConfiguration(report)
	c.checkWebAuth(report)

	if progress != nil {
		progress.Step(2, "Checking Docker connectivity...")
	}
	dockerOK := c.checkDocker(report)

	if progress != nil {
		progress.Step(3, "Validating job schedules...")
	}
	c.checkSchedules(report)

	// Docker-dependent checks
	if progress != nil {
		progress.Step(4, "Verifying Docker images...")
	}
	if dockerOK {
		c.checkDockerImages(report)
	} else {
		report.Checks = append(report.Checks, CheckResult{
			Category: "Docker Images",
			Name:     "Image Availability",
			Status:   statusSkip,
			Message:  "Skipped (Docker connectivity required)",
		})
	}

	// Clear progress line before output
	if progress != nil {
		progress.Complete("Health check complete")
	}

	// Output results
	if c.JSON {
		return c.outputJSON(report)
	}
	return c.outputHuman(report)
}

// checkConfiguration validates configuration file
func (c *DoctorCommand) checkConfiguration(report *DoctorReport) {
	// Check file exists and is readable
	if _, err := os.Stat(c.ConfigFile); err != nil {
		if os.IsNotExist(err) {
			report.Healthy = false
			hints := []string{
				"Run 'ofelia init' to create a config file interactively",
			}
			// Only show "Searched:" hint when auto-detection was attempted
			if c.configAutoDetected {
				hints = append(hints, "Searched: "+strings.Join(commonConfigPaths, ", "))
			}
			hints = append(hints, "Or specify path with: --config=/path/to/config.ini")
			report.Checks = append(report.Checks, CheckResult{
				Category: "Configuration",
				Name:     "File Exists",
				Status:   statusFail,
				Message:  fmt.Sprintf("Config file not found: %s", c.ConfigFile),
				Hints:    hints,
			})
			return
		}
		report.Healthy = false
		report.Checks = append(report.Checks, CheckResult{
			Category: "Configuration",
			Name:     "File Readable",
			Status:   statusFail,
			Message:  fmt.Sprintf("Cannot read config file: %v", err),
			Hints: []string{
				fmt.Sprintf("Check permissions: ls -l %s", c.ConfigFile),
				fmt.Sprintf("Fix permissions: chmod 644 %s", c.ConfigFile),
			},
		})
		return
	}

	report.Checks = append(report.Checks, CheckResult{
		Category: "Configuration",
		Name:     "File Exists",
		Status:   statusPass,
		Message:  c.ConfigFile,
	})

	// Try to load and parse configuration
	conf, err := BuildFromFile(c.ConfigFile, c.Logger)
	if err != nil {
		report.Healthy = false
		report.Checks = append(report.Checks, CheckResult{
			Category: "Configuration",
			Name:     "Valid Syntax",
			Status:   statusFail,
			Message:  fmt.Sprintf("Parse error: %v", err),
			Hints: []string{
				"Check INI syntax (sections, keys, values)",
				fmt.Sprintf("Validate with: ofelia validate --config=%s", c.ConfigFile),
			},
		})
		return
	}

	report.Checks = append(report.Checks, CheckResult{
		Category: "Configuration",
		Name:     "Valid Syntax",
		Status:   statusPass,
	})

	// Count jobs
	jobCount := len(conf.RunJobs) + len(conf.LocalJobs) +
		len(conf.ExecJobs) + len(conf.ServiceJobs)
	report.Checks = append(report.Checks, CheckResult{
		Category: "Configuration",
		Name:     "Jobs Defined",
		Status:   statusPass,
		Message:  fmt.Sprintf("%d job(s) configured", jobCount),
	})

	// Check for deprecated configuration options
	deprecations := GetDeprecationRegistry().ForDoctor(conf)
	if len(deprecations) > 0 {
		for _, dep := range deprecations {
			report.Checks = append(report.Checks, CheckResult{
				Category: "Configuration",
				Name:     "Deprecated Option",
				Status:   statusFail,
				Message:  fmt.Sprintf("'%s' is deprecated and will be removed in %s", dep.Option, dep.RemovalVersion),
				Hints: []string{
					fmt.Sprintf("Use %s instead", dep.Replacement),
					dep.Message,
				},
			})
		}
		// Deprecations don't make the report unhealthy, just warn
	} else {
		report.Checks = append(report.Checks, CheckResult{
			Category: "Configuration",
			Name:     "Deprecated Options",
			Status:   statusPass,
			Message:  "No deprecated options in use",
		})
	}
}

func (c *DoctorCommand) checkWebAuth(report *DoctorReport) {
	conf, err := BuildFromFile(c.ConfigFile, c.Logger)
	if err != nil {
		return
	}

	if !conf.Global.WebAuthEnabled {
		return
	}

	if conf.Global.WebUsername == "" {
		report.Healthy = false
		report.Checks = append(report.Checks, CheckResult{
			Category: "Configuration",
			Name:     "Web Auth Username",
			Status:   statusFail,
			Message:  "web-auth-enabled is true but web-username is not set",
			Hints: []string{
				"Run 'ofelia init' to configure web authentication interactively",
				"Or set web-username in config: web-username = admin",
				"Or via environment: OFELIA_WEB_USERNAME=admin",
			},
		})
	}

	if conf.Global.WebPasswordHash == "" {
		report.Healthy = false
		report.Checks = append(report.Checks, CheckResult{
			Category: "Configuration",
			Name:     "Web Auth Password",
			Status:   statusFail,
			Message:  "web-auth-enabled is true but web-password-hash is not set",
			Hints: []string{
				"Run 'ofelia hash-password' to generate a bcrypt hash",
				"Or run 'ofelia init' to configure web authentication interactively",
				"Then set web-password-hash in config or OFELIA_WEB_PASSWORD_HASH env var",
			},
		})
	}

	if conf.Global.WebSecretKey == "" {
		report.Checks = append(report.Checks, CheckResult{
			Category: "Configuration",
			Name:     "Web Auth Secret Key",
			Status:   statusSkip,
			Message:  "web-secret-key not set - tokens will not survive daemon restarts",
			Hints: []string{
				"Set OFELIA_WEB_SECRET_KEY for persistent sessions",
				"Generate with: openssl rand -base64 32",
			},
		})
	}

	if conf.Global.WebUsername != "" && conf.Global.WebPasswordHash != "" {
		report.Checks = append(report.Checks, CheckResult{
			Category: "Configuration",
			Name:     "Web Auth",
			Status:   statusPass,
			Message:  fmt.Sprintf("Configured for user '%s'", conf.Global.WebUsername),
		})
	}
}

// checkDocker validates Docker connectivity
func (c *DoctorCommand) checkDocker(report *DoctorReport) bool {
	conf, err := BuildFromFile(c.ConfigFile, c.Logger)
	if err != nil {
		// Config error already reported in checkConfiguration
		return false
	}

	// Only check Docker if there are Docker-based jobs
	hasDockerJobs := len(conf.RunJobs) > 0 ||
		len(conf.ExecJobs) > 0 ||
		len(conf.ServiceJobs) > 0

	if !hasDockerJobs {
		report.Checks = append(report.Checks, CheckResult{
			Category: "Docker",
			Name:     "Connectivity",
			Status:   statusSkip,
			Message:  "No Docker-based jobs configured",
		})
		return true // Not needed, so counts as OK
	}

	// Try to initialize Docker handler
	if err := conf.initDockerHandler(); err != nil {
		report.Healthy = false
		report.Checks = append(report.Checks, CheckResult{
			Category: "Docker",
			Name:     "Connectivity",
			Status:   statusFail,
			Message:  fmt.Sprintf("Cannot connect to Docker: %v", err),
			Hints: []string{
				"Check Docker daemon: docker info",
				"Start Docker service (Linux: systemctl start docker, macOS/Windows: start Docker Desktop)",
				"Check socket: ls -l /var/run/docker.sock",
				"Fix permissions: sudo usermod -aG docker $USER (Linux)",
			},
		})
		return false
	}

	// Ping Docker daemon using SDK provider
	provider := conf.dockerHandler.GetDockerProvider()
	if provider == nil {
		report.Healthy = false
		report.Checks = append(report.Checks, CheckResult{
			Category: "Docker",
			Name:     "Connectivity",
			Status:   statusFail,
			Message:  "Docker provider not initialized",
			Hints: []string{
				"Check Docker daemon: docker info",
				"Verify Docker socket permissions",
			},
		})
		return false
	}

	if err := provider.Ping(context.Background()); err != nil {
		report.Healthy = false
		report.Checks = append(report.Checks, CheckResult{
			Category: "Docker",
			Name:     "Connectivity",
			Status:   statusFail,
			Message:  fmt.Sprintf("Docker ping failed: %v", err),
			Hints: []string{
				"Check daemon status (Linux: systemctl status docker, macOS/Windows: check Docker Desktop)",
				"View logs (Linux: journalctl -u docker -n 50, macOS/Windows: Docker Desktop logs)",
			},
		})
		return false
	}

	report.Checks = append(report.Checks, CheckResult{
		Category: "Docker",
		Name:     "Connectivity",
		Status:   statusPass,
		Message:  "Docker daemon responding",
	})

	return true
}

// checkSchedules validates all job schedules
func (c *DoctorCommand) checkSchedules(report *DoctorReport) {
	conf, err := BuildFromFile(c.ConfigFile, c.Logger)
	if err != nil {
		// Config error already reported
		report.Checks = append(report.Checks, CheckResult{
			Category: "Job Schedules",
			Name:     "Schedule Validation",
			Status:   statusSkip,
			Message:  "Skipped (configuration validation failed)",
		})
		return
	}

	allValid := true

	// Check run jobs
	for name, job := range conf.RunJobs {
		if err := validateCronSchedule(job.Schedule); err != nil {
			allValid = false
			report.Healthy = false
			report.Checks = append(report.Checks, CheckResult{
				Category: "Job Schedules",
				Name:     fmt.Sprintf("job-run \"%s\"", name),
				Status:   statusFail,
				Message:  fmt.Sprintf("Invalid schedule \"%s\": %v", job.Schedule, err),
				Hints: []string{
					"Examples: @daily, @every 1h, 0 2 * * *, */15 * * * *",
					"Test schedule: https://crontab.guru",
				},
			})
		}
	}

	// Check local jobs
	for name, job := range conf.LocalJobs {
		if err := validateCronSchedule(job.Schedule); err != nil {
			allValid = false
			report.Healthy = false
			report.Checks = append(report.Checks, CheckResult{
				Category: "Job Schedules",
				Name:     fmt.Sprintf("job-local \"%s\"", name),
				Status:   statusFail,
				Message:  fmt.Sprintf("Invalid schedule \"%s\": %v", job.Schedule, err),
				Hints: []string{
					"Examples: @daily, @every 1h, 0 2 * * *, */15 * * * *",
					"Test schedule: https://crontab.guru",
				},
			})
		}
	}

	// Check exec jobs
	for name, job := range conf.ExecJobs {
		if err := validateCronSchedule(job.Schedule); err != nil {
			allValid = false
			report.Healthy = false
			report.Checks = append(report.Checks, CheckResult{
				Category: "Job Schedules",
				Name:     fmt.Sprintf("job-exec \"%s\"", name),
				Status:   statusFail,
				Message:  fmt.Sprintf("Invalid schedule \"%s\": %v", job.Schedule, err),
				Hints: []string{
					"Examples: @daily, @every 1h, 0 2 * * *, */15 * * * *",
					"Test schedule: https://crontab.guru",
				},
			})
		}
	}

	// Check service-run jobs
	for name, job := range conf.ServiceJobs {
		if err := validateCronSchedule(job.Schedule); err != nil {
			allValid = false
			report.Healthy = false
			report.Checks = append(report.Checks, CheckResult{
				Category: "Job Schedules",
				Name:     fmt.Sprintf("job-service-run \"%s\"", name),
				Status:   statusFail,
				Message:  fmt.Sprintf("Invalid schedule \"%s\": %v", job.Schedule, err),
				Hints: []string{
					"Examples: @daily, @every 1h, 0 2 * * *, */15 * * * *",
					"Test schedule: https://crontab.guru",
				},
			})
		}
	}

	// Check compose jobs
	for name, job := range conf.ComposeJobs {
		if err := validateCronSchedule(job.Schedule); err != nil {
			allValid = false
			report.Healthy = false
			report.Checks = append(report.Checks, CheckResult{
				Category: "Job Schedules",
				Name:     fmt.Sprintf("job-compose \"%s\"", name),
				Status:   statusFail,
				Message:  fmt.Sprintf("Invalid schedule \"%s\": %v", job.Schedule, err),
				Hints: []string{
					"Examples: @daily, @every 1h, 0 2 * * *, */15 * * * *",
					"Test schedule: https://crontab.guru",
				},
			})
		}
	}

	if allValid {
		totalJobs := len(conf.RunJobs) + len(conf.LocalJobs) +
			len(conf.ExecJobs) + len(conf.ServiceJobs) + len(conf.ComposeJobs)
		report.Checks = append(report.Checks, CheckResult{
			Category: "Job Schedules",
			Name:     "All Schedules Valid",
			Status:   statusPass,
			Message:  fmt.Sprintf("%d schedule(s) validated", totalJobs),
		})
	}
}

// checkDockerImages validates required Docker images exist
func (c *DoctorCommand) checkDockerImages(report *DoctorReport) {
	conf, err := BuildFromFile(c.ConfigFile, c.Logger)
	if err != nil {
		return
	}

	// Collect all required images
	imageMap := make(map[string]bool)
	for _, job := range conf.RunJobs {
		imageMap[job.Image] = true
	}

	if len(imageMap) == 0 {
		report.Checks = append(report.Checks, CheckResult{
			Category: "Docker Images",
			Name:     "Image Availability",
			Status:   statusSkip,
			Message:  "No job-run jobs configured",
		})
		return
	}

	if err := conf.initDockerHandler(); err != nil {
		return // Docker check already failed
	}

	provider := conf.dockerHandler.GetDockerProvider()
	if provider == nil {
		return // Provider not available
	}

	ctx := context.Background()
	allAvailable := true
	for image := range imageMap {
		hasImage, err := provider.HasImageLocally(ctx, image)
		if err != nil || !hasImage {
			allAvailable = false
			report.Healthy = false
			report.Checks = append(report.Checks, CheckResult{
				Category: "Docker Images",
				Name:     image,
				Status:   statusFail,
				Message:  "Image not found locally",
				Hints: []string{
					fmt.Sprintf("Pull image: docker pull %s", image),
				},
			})
		}
	}

	if allAvailable {
		report.Checks = append(report.Checks, CheckResult{
			Category: "Docker Images",
			Name:     "All Images Available",
			Status:   statusPass,
			Message:  fmt.Sprintf("%d image(s) found locally", len(imageMap)),
		})
	}
}

// validateCronSchedule validates a cron schedule expression using go-cron's ValidateSpec API.
// This provides cleaner validation with proper handling of all cron formats including descriptors,
// @every intervals, month/day names, wraparound ranges, and standard cron expressions.
func validateCronSchedule(schedule string) error {
	parseOpts := cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor
	if err := cron.ValidateSpec(schedule, parseOpts); err != nil {
		return fmt.Errorf("invalid cron schedule %q: %w", schedule, err)
	}
	return nil
}

// outputJSON outputs results as JSON
func (c *DoctorCommand) outputJSON(report *DoctorReport) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	c.Logger.Noticef("%s", string(data))

	if !report.Healthy {
		return fmt.Errorf("health check failed")
	}
	return nil
}

// outputHuman outputs results in human-readable format
func (c *DoctorCommand) outputHuman(report *DoctorReport) error {
	c.Logger.Noticef("🏥 Ofelia Health Check\n")

	// Group checks by category
	categories := make(map[string][]CheckResult)
	categoryOrder := []string{"Configuration", "Docker", "Job Schedules", "Docker Images"}

	for _, check := range report.Checks {
		categories[check.Category] = append(categories[check.Category], check)
	}

	// Output by category
	for _, category := range categoryOrder {
		checks, exists := categories[category]
		if !exists {
			continue
		}

		icon := getCategoryIcon(category)
		c.Logger.Noticef("%s %s", icon, category)

		for _, check := range checks {
			statusIcon := getStatusIcon(check.Status)
			if check.Message != "" {
				c.Logger.Noticef("  %s %s: %s", statusIcon, check.Name, check.Message)
			} else {
				c.Logger.Noticef("  %s %s", statusIcon, check.Name)
			}

			// Output hints
			for _, hint := range check.Hints {
				c.Logger.Noticef("    → %s", hint)
			}
		}
		c.Logger.Noticef("")
	}

	// Summary
	failCount := 0
	skipCount := 0
	for _, check := range report.Checks {
		if check.Status == statusFail {
			failCount++
		} else if check.Status == statusSkip {
			skipCount++
		}
	}

	if report.Healthy {
		c.Logger.Noticef("Summary: All checks passed ✅")
		if skipCount > 0 {
			c.Logger.Noticef("  (%d check(s) skipped as not applicable)", skipCount)
		}
		return nil
	}

	c.Logger.Noticef("Summary: %d issue(s) found ❌", failCount)
	if skipCount > 0 {
		c.Logger.Noticef("  (%d check(s) skipped due to blockers)", skipCount)
	}
	return fmt.Errorf("health check failed")
}

// getCategoryIcon returns emoji for category
func getCategoryIcon(category string) string {
	icons := map[string]string{
		"Configuration": "📋",
		"Docker":        "🐳",
		"Job Schedules": "📅",
		"Docker Images": "🖼️",
	}
	if icon, ok := icons[category]; ok {
		return icon
	}
	return "📌"
}

// getStatusIcon returns emoji for check status
func getStatusIcon(status string) string {
	switch status {
	case statusPass:
		return "✅"
	case statusFail:
		return "❌"
	case statusSkip:
		return "⚠️"
	default:
		return "❓"
	}
}
