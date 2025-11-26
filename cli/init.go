package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/manifoldco/promptui"
	"github.com/netresearch/go-cron"
	"gopkg.in/ini.v1"

	"github.com/netresearch/ofelia/core"
)

// InitCommand creates an interactive wizard for generating Ofelia configuration
type InitCommand struct {
	Output   string `long:"output" short:"o" description:"Output file path" default:"./ofelia.ini"`
	LogLevel string `long:"log-level" env:"OFELIA_LOG_LEVEL" description:"Set log level"`
	Logger   core.Logger
}

// Execute runs the interactive configuration wizard
func (c *InitCommand) Execute(_ []string) error {
	if err := ApplyLogLevel(c.LogLevel); err != nil {
		c.Logger.Warningf("Failed to apply log level (using default): %v", err)
	}

	c.Logger.Noticef("🚀 Welcome to Ofelia Configuration Setup!")
	c.Logger.Noticef("This wizard will help you create your first config file.")

	// Check if output file already exists
	if _, err := os.Stat(c.Output); err == nil {
		if !c.confirmOverwrite() {
			c.Logger.Noticef("Setup canceled")
			return nil
		}
	}

	// Gather configuration
	config := &initConfig{
		Global: &globalConfig{},
		Jobs:   []initJobConfig{},
	}

	// Prompt for global settings
	if err := c.promptGlobalSettings(config.Global); err != nil {
		return fmt.Errorf("failed to gather global settings: %w", err)
	}

	// Prompt for jobs
	if err := c.promptJobs(config); err != nil {
		return fmt.Errorf("failed to gather job configuration: %w", err)
	}

	// Generate and save configuration
	if err := c.saveConfig(config); err != nil {
		return fmt.Errorf("failed to save configuration: %w", err)
	}

	c.Logger.Noticef("✅ Configuration saved to: %s", c.Output)

	// Offer post-creation actions
	if err := c.postCreationActions(); err != nil {
		c.Logger.Warningf("Post-creation action failed: %v", err)
	}

	c.printNextSteps()
	return nil
}

// initConfig holds the configuration being built
type initConfig struct {
	Global *globalConfig
	Jobs   []initJobConfig
}

// globalConfig holds global settings
type globalConfig struct {
	EnableWeb bool
	WebAddr   string
	LogLevel  string
}

// initJobConfig interface for different job types in init wizard
type initJobConfig interface {
	Type() string
	Name() string
	ToINI(section *ini.Section) error
}

// runJobConfig represents a job-run configuration
type runJobConfig struct {
	JobName  string
	Schedule string
	Image    string
	Command  string
	Volume   string
	Network  string
	Delete   bool
}

func (j *runJobConfig) Type() string { return "job-run" }
func (j *runJobConfig) Name() string { return j.JobName }
func (j *runJobConfig) ToINI(section *ini.Section) error {
	section.Key("schedule").SetValue(j.Schedule)
	section.Key("image").SetValue(j.Image)
	section.Key("command").SetValue(j.Command)
	if j.Volume != "" {
		section.Key("volume").SetValue(j.Volume)
	}
	if j.Network != "" {
		section.Key("network").SetValue(j.Network)
	}
	if j.Delete {
		section.Key("delete").SetValue("true")
	}
	return nil
}

// localJobConfig represents a job-local configuration
type localJobConfig struct {
	JobName  string
	Schedule string
	Command  string
	Dir      string
}

func (j *localJobConfig) Type() string { return "job-local" }
func (j *localJobConfig) Name() string { return j.JobName }
func (j *localJobConfig) ToINI(section *ini.Section) error {
	section.Key("schedule").SetValue(j.Schedule)
	section.Key("command").SetValue(j.Command)
	if j.Dir != "" {
		section.Key("dir").SetValue(j.Dir)
	}
	return nil
}

// confirmOverwrite asks user to confirm overwriting existing file
func (c *InitCommand) confirmOverwrite() bool {
	prompt := promptui.Prompt{
		Label:     fmt.Sprintf("File %s already exists. Overwrite", c.Output),
		IsConfirm: true,
		Default:   "n",
	}
	_, err := prompt.Run()
	return err == nil
}

// promptGlobalSettings gathers global configuration
func (c *InitCommand) promptGlobalSettings(global *globalConfig) error {
	c.Logger.Noticef("=== Global Settings ===")

	// Enable web UI
	prompt := promptui.Prompt{
		Label:     "Enable web UI",
		IsConfirm: true,
		Default:   "Y",
	}
	_, err := prompt.Run()
	global.EnableWeb = err == nil

	if global.EnableWeb {
		// Web UI address
		prompt := promptui.Prompt{
			Label:   "Web UI address",
			Default: "127.0.0.1:8081",
		}
		global.WebAddr, err = prompt.Run()
		if err != nil {
			return err //nolint:wrapcheck // promptui errors are user interaction failures, not internal errors
		}
	}

	// Log level
	logLevelPrompt := promptui.Select{
		Label:     "Log level",
		Items:     []string{"panic", "fatal", "error", "warning", "info", "debug", "trace"},
		CursorPos: 4, // Default to "info"
	}
	_, global.LogLevel, err = logLevelPrompt.Run()
	if err != nil {
		return err //nolint:wrapcheck // promptui errors are user interaction failures, not internal errors
	}

	return nil
}

// promptJobs gathers job configurations
func (c *InitCommand) promptJobs(config *initConfig) error {
	c.Logger.Noticef("=== Job Configuration ===")
	c.Logger.Noticef("Let's create your first scheduled job.")

	for {
		// Select job type
		jobTypePrompt := promptui.Select{
			Label: "Job type",
			Items: []string{"run (Docker container)", "local (Shell command)", "Skip - finish setup"},
		}
		_, jobTypeSelection, err := jobTypePrompt.Run()
		if err != nil {
			return err //nolint:wrapcheck // promptui errors are user interaction failures, not internal errors
		}

		if strings.HasPrefix(jobTypeSelection, "Skip") {
			if len(config.Jobs) == 0 {
				c.Logger.Warningf("⚠️  Warning: No jobs configured. Ofelia won't schedule anything.")
			}
			break
		}

		// Determine job type
		var job initJobConfig
		if strings.HasPrefix(jobTypeSelection, "run") {
			job, err = c.promptRunJob()
		} else {
			job, err = c.promptLocalJob()
		}

		if err != nil {
			return err
		}

		config.Jobs = append(config.Jobs, job)
		c.Logger.Noticef("✓ Added %s job: %s", job.Type(), job.Name())

		// Ask if user wants to add another job
		addMore := promptui.Prompt{
			Label:     "Add another job",
			IsConfirm: true,
			Default:   "n",
		}
		_, err = addMore.Run()
		if err != nil {
			break
		}
	}

	return nil //nolint:nilerr // err is from prompt "Add another job" - declining is normal flow
}

// promptRunJob prompts for job-run configuration
func (c *InitCommand) promptRunJob() (*runJobConfig, error) {
	job := &runJobConfig{}

	// Job name
	namePrompt := promptui.Prompt{
		Label: "Job name (alphanumeric, hyphens, underscores)",
		Validate: func(input string) error {
			if input == "" {
				return fmt.Errorf("job name cannot be empty")
			}
			if !regexp.MustCompile(`^[a-zA-Z0-9_-]+$`).MatchString(input) {
				return fmt.Errorf("job name must be alphanumeric with hyphens or underscores only")
			}
			return nil
		},
	}
	name, err := namePrompt.Run()
	if err != nil {
		return nil, err //nolint:wrapcheck // promptui errors are user interaction failures, not internal errors
	}
	job.JobName = name

	// Schedule
	schedulePrompt := promptui.Prompt{
		Label:    "Schedule (cron or @every)",
		Default:  "@daily",
		Validate: validateSchedule,
	}
	job.Schedule, err = schedulePrompt.Run()
	if err != nil {
		return nil, err //nolint:wrapcheck // promptui errors are user interaction failures, not internal errors
	}

	// Docker image
	imagePrompt := promptui.Prompt{
		Label: "Docker image (e.g., alpine:latest, postgres:16)",
		Validate: func(input string) error {
			if input == "" {
				return fmt.Errorf("Docker image cannot be empty")
			}
			return nil
		},
	}
	job.Image, err = imagePrompt.Run()
	if err != nil {
		return nil, err //nolint:wrapcheck // promptui errors are user interaction failures, not internal errors
	}

	// Command
	commandPrompt := promptui.Prompt{
		Label: "Command to run in container",
		Validate: func(input string) error {
			if input == "" {
				return fmt.Errorf("command cannot be empty")
			}
			return nil
		},
	}
	job.Command, err = commandPrompt.Run()
	if err != nil {
		return nil, err //nolint:wrapcheck // promptui errors are user interaction failures, not internal errors
	}

	// Volume (optional)
	volumePrompt := promptui.Prompt{
		Label:   "Volume mounts (optional, format: /host/path:/container/path)",
		Default: "",
	}
	job.Volume, err = volumePrompt.Run()
	if err != nil && !errors.Is(err, promptui.ErrAbort) {
		return nil, err //nolint:wrapcheck // promptui errors are user interaction failures, not internal errors
	}

	// Network (optional)
	networkPrompt := promptui.Prompt{
		Label:   "Docker network (optional)",
		Default: "",
	}
	job.Network, err = networkPrompt.Run()
	if err != nil && !errors.Is(err, promptui.ErrAbort) {
		return nil, err //nolint:wrapcheck // promptui errors are user interaction failures, not internal errors
	}

	// Delete container after execution
	deletePrompt := promptui.Prompt{
		Label:     "Delete container after execution",
		IsConfirm: true,
		Default:   "Y",
	}
	_, err = deletePrompt.Run()
	job.Delete = err == nil

	return job, nil
}

// promptLocalJob prompts for job-local configuration
func (c *InitCommand) promptLocalJob() (*localJobConfig, error) {
	job := &localJobConfig{}

	// Job name
	namePrompt := promptui.Prompt{
		Label: "Job name (alphanumeric, hyphens, underscores)",
		Validate: func(input string) error {
			if input == "" {
				return fmt.Errorf("job name cannot be empty")
			}
			if !regexp.MustCompile(`^[a-zA-Z0-9_-]+$`).MatchString(input) {
				return fmt.Errorf("job name must be alphanumeric with hyphens or underscores only")
			}
			return nil
		},
	}
	name, err := namePrompt.Run()
	if err != nil {
		return nil, err //nolint:wrapcheck // promptui errors are user interaction failures, not internal errors
	}
	job.JobName = name

	// Schedule
	schedulePrompt := promptui.Prompt{
		Label:    "Schedule (cron or @every)",
		Default:  "@hourly",
		Validate: validateSchedule,
	}
	job.Schedule, err = schedulePrompt.Run()
	if err != nil {
		return nil, err //nolint:wrapcheck // promptui errors are user interaction failures, not internal errors
	}

	// Command
	commandPrompt := promptui.Prompt{
		Label: "Shell command to run",
		Validate: func(input string) error {
			if input == "" {
				return fmt.Errorf("command cannot be empty")
			}
			return nil
		},
	}
	job.Command, err = commandPrompt.Run()
	if err != nil {
		return nil, err //nolint:wrapcheck // promptui errors are user interaction failures, not internal errors
	}

	// Working directory (optional)
	dirPrompt := promptui.Prompt{
		Label:   "Working directory (optional)",
		Default: "",
	}
	job.Dir, err = dirPrompt.Run()
	if err != nil && !errors.Is(err, promptui.ErrAbort) {
		return nil, err //nolint:wrapcheck // promptui errors are user interaction failures, not internal errors
	}

	return job, nil
}

// validateSchedule validates cron expression or @every/@daily/@hourly shortcuts
func validateSchedule(schedule string) error {
	if schedule == "" {
		return fmt.Errorf("schedule cannot be empty")
	}

	// Check for special descriptors
	descriptors := []string{"@yearly", "@annually", "@monthly", "@weekly", "@daily", "@midnight", "@hourly"}
	for _, desc := range descriptors {
		if schedule == desc {
			return nil
		}
	}

	// Check for @every format
	if strings.HasPrefix(schedule, "@every ") {
		return nil // Basic validation - actual parsing happens in cron library
	}

	// Validate as cron expression
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	if _, err := parser.Parse(schedule); err != nil {
		return fmt.Errorf("invalid cron expression: %w\n  Examples: @daily, @every 1h, 0 2 * * *, */15 * * * *", err)
	}

	return nil
}

// saveConfig writes the configuration to INI file
func (c *InitCommand) saveConfig(config *initConfig) error {
	// Ensure directory exists
	dir := filepath.Dir(c.Output)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("failed to create directory %q: %w", dir, err)
	}

	// Create INI file
	cfg := ini.Empty()

	// Write global section
	global := cfg.Section("global")
	if config.Global.EnableWeb {
		global.Key("enable-web").SetValue("true")
		if config.Global.WebAddr != "" {
			global.Key("web-address").SetValue(config.Global.WebAddr)
		}
	}
	if config.Global.LogLevel != "" {
		global.Key("log-level").SetValue(config.Global.LogLevel)
	}

	// Write job sections
	for _, job := range config.Jobs {
		sectionName := fmt.Sprintf("%s \"%s\"", job.Type(), job.Name())
		section := cfg.Section(sectionName)
		if err := job.ToINI(section); err != nil {
			return fmt.Errorf("failed to write job %q: %w", job.Name(), err)
		}
	}

	// Save to file
	if err := cfg.SaveTo(c.Output); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

// postCreationActions offers validation and other post-creation options
func (c *InitCommand) postCreationActions() error {
	// Offer to validate
	validatePrompt := promptui.Prompt{
		Label:     "Validate configuration now",
		IsConfirm: true,
		Default:   "Y",
	}
	_, err := validatePrompt.Run()
	if err == nil {
		// Validate the configuration
		conf, err := BuildFromFile(c.Output, c.Logger)
		if err != nil {
			c.Logger.Errorf("❌ Configuration validation failed: %v", err)
			return err
		}
		c.Logger.Noticef("✅ Configuration is valid!")

		// Offer to show configuration
		showPrompt := promptui.Prompt{
			Label:     "Show generated configuration",
			IsConfirm: true,
			Default:   "n",
		}
		_, err = showPrompt.Run()
		if err == nil {
			content, _ := os.ReadFile(c.Output)
			c.Logger.Noticef("\n%s", string(content))
		}

		// Don't offer to start daemon - that's a separate workflow
		_ = conf // Use conf to avoid unused variable
	}

	return nil
}

// printNextSteps displays helpful next steps
func (c *InitCommand) printNextSteps() {
	c.Logger.Noticef("📋 Setup complete! Next steps:")
	c.Logger.Noticef("  → Review configuration: cat %s", c.Output)
	c.Logger.Noticef("  → Validate: ofelia validate --config=%s", c.Output)
	c.Logger.Noticef("  → Start daemon: ofelia daemon --config=%s", c.Output)
}
