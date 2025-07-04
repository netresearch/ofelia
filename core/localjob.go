package core

import (
	"os"
	"os/exec"
	"reflect"

	"github.com/gobs/args"
)

type LocalJob struct {
	BareJob     `mapstructure:",squash"`
	Dir         string   `hash:"true"`
	Environment []string `mapstructure:"environment" hash:"true"`
}

func NewLocalJob() *LocalJob {
	return &LocalJob{}
}

func (j *LocalJob) Run(ctx *Context) error {
	cmd, err := j.buildCommand(ctx)
	if err != nil {
		return err
	}

	return cmd.Run()
}

func (j *LocalJob) buildCommand(ctx *Context) (*exec.Cmd, error) {
	args := args.GetArgs(j.Command)
	bin, err := exec.LookPath(args[0])
	if err != nil {
		return nil, err
	}

	return &exec.Cmd{
		Path:   bin,
		Args:   args,
		Stdout: ctx.Execution.OutputStream,
		Stderr: ctx.Execution.ErrorStream,
		// add custom env variables to the existing ones
		// instead of overwriting them
		Env: append(os.Environ(), j.Environment...),
		Dir: j.Dir,
	}, nil
}

func (j *LocalJob) Hash() (string, error) {
	var h string
	if err := getHash(reflect.TypeOf(j).Elem(), reflect.ValueOf(j).Elem(), &h); err != nil {
		return "", err
	}
	return h, nil
}
