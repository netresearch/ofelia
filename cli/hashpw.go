package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/manifoldco/promptui"
	"golang.org/x/crypto/bcrypt"

	"github.com/netresearch/ofelia/core"
)

type HashPasswordCommand struct {
	Cost     int    `long:"cost" default:"12" description:"bcrypt cost factor (10-14 recommended)"`
	LogLevel string `long:"log-level" env:"OFELIA_LOG_LEVEL" description:"Set log level"`
	Logger   core.Logger
}

func (c *HashPasswordCommand) Execute(_ []string) error {
	if err := ApplyLogLevel(c.LogLevel); err != nil {
		c.Logger.Warningf("Failed to apply log level (using default): %v", err)
	}

	if c.Cost < bcrypt.MinCost || c.Cost > bcrypt.MaxCost {
		return fmt.Errorf("bcrypt cost must be between %d and %d", bcrypt.MinCost, bcrypt.MaxCost)
	}

	prompt := promptui.Prompt{
		Label: "Password",
		Mask:  '*',
		Validate: func(input string) error {
			if len(input) < 8 {
				return fmt.Errorf("password must be at least 8 characters")
			}
			return nil
		},
	}

	password, err := prompt.Run()
	if err != nil {
		return fmt.Errorf("password prompt failed: %w", err)
	}

	confirmPrompt := promptui.Prompt{
		Label: "Confirm password",
		Mask:  '*',
	}

	confirm, err := confirmPrompt.Run()
	if err != nil {
		return fmt.Errorf("confirmation prompt failed: %w", err)
	}

	if password != confirm {
		return fmt.Errorf("passwords do not match")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), c.Cost)
	if err != nil {
		return fmt.Errorf("failed to generate hash: %w", err)
	}

	hashStr := string(hash)

	fmt.Fprintln(os.Stdout, "")
	fmt.Fprintln(os.Stdout, "Generated bcrypt hash:")
	fmt.Fprintln(os.Stdout, strings.Repeat("-", 70))
	fmt.Fprintln(os.Stdout, hashStr)
	fmt.Fprintln(os.Stdout, strings.Repeat("-", 70))
	fmt.Fprintln(os.Stdout, "")
	fmt.Fprintln(os.Stdout, "Usage in config.ini:")
	fmt.Fprintln(os.Stdout, "  [global]")
	fmt.Fprintln(os.Stdout, "  web-auth-enabled = true")
	fmt.Fprintln(os.Stdout, "  web-username = admin")
	fmt.Fprintf(os.Stdout, "  web-password-hash = %s\n", hashStr)
	fmt.Fprintln(os.Stdout, "")
	fmt.Fprintln(os.Stdout, "Or via environment variables:")
	fmt.Fprintln(os.Stdout, "  export OFELIA_WEB_AUTH_ENABLED=true")
	fmt.Fprintln(os.Stdout, "  export OFELIA_WEB_USERNAME=admin")
	fmt.Fprintf(os.Stdout, "  export OFELIA_WEB_PASSWORD_HASH='%s'\n", hashStr)

	return nil
}
