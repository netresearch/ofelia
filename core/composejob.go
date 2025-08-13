package core

import (
	"fmt"
	"os"
	"os/exec"
	"reflect"

	"github.com/gobs/args"
)

type ComposeJob struct {
	BareJob `mapstructure:",squash"`
	File    string `default:"compose.yml" gcfg:"file" mapstructure:"file" hash:"true"`
	Service string `gcfg:"service" mapstructure:"service" hash:"true"`
	Exec    bool   `default:"false" gcfg:"exec" mapstructure:"exec" hash:"true"`
}

func NewComposeJob() *ComposeJob { return &ComposeJob{} }

func (j *ComposeJob) Run(ctx *Context) error {
	cmd := j.buildCommand(ctx)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("compose run: %w", err)
	}
	return nil
}

func (j *ComposeJob) buildCommand(ctx *Context) *exec.Cmd {
	var argsSlice []string
	argsSlice = append(argsSlice, "compose", "-f", j.File)
	if j.Exec {
		argsSlice = append(argsSlice, "exec", j.Service)
	} else {
		argsSlice = append(argsSlice, "run", "--rm", j.Service)
	}
	if j.Command != "" {
		argsSlice = append(argsSlice, args.GetArgs(j.Command)...)
	}
	// #nosec G204 -- command arguments are constructed from validated job config
	cmd := exec.Command("docker", argsSlice...)
	cmd.Stdout = ctx.Execution.OutputStream
	cmd.Stderr = ctx.Execution.ErrorStream
	cmd.Env = os.Environ()
	return cmd
}

func (j *ComposeJob) Hash() (string, error) {
	var h string
	if err := getHash(reflect.TypeOf(j).Elem(), reflect.ValueOf(j).Elem(), &h); err != nil {
		return "", err
	}
	return h, nil
}
