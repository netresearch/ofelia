package core

import (
	"github.com/gobs/args"
	"os"
	"os/exec"
)

type ComposeJob struct {
	BareJob `mapstructure:",squash"`
	File    string `default:"compose.yml" gcfg:"file" mapstructure:"file" hash:"true"`
	Service string `gcfg:"service" mapstructure:"service" hash:"true"`
	Exec    bool   `default:"false" gcfg:"exec" mapstructure:"exec" hash:"true"`
}

func NewComposeJob() *ComposeJob { return &ComposeJob{} }

func (j *ComposeJob) Run(ctx *Context) error {
	cmd, err := j.buildCommand(ctx)
	if err != nil {
		return err
	}
	return cmd.Run()
}

func (j *ComposeJob) buildCommand(ctx *Context) (*exec.Cmd, error) {
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
	cmd := exec.Command("docker", argsSlice...)
	cmd.Stdout = ctx.Execution.OutputStream
	cmd.Stderr = ctx.Execution.ErrorStream
	cmd.Env = os.Environ()
	return cmd, nil
}
