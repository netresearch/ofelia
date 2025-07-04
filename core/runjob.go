package core

import (
	"fmt"
	"reflect"
	"strconv"
	"sync"
	"time"

	docker "github.com/fsouza/go-dockerclient"
	"github.com/gobs/args"
)

var dockercfg *docker.AuthConfigurations

func init() {
	dockercfg, _ = docker.NewAuthConfigurationsFromDockerCfg()
}

type RunJob struct {
	BareJob `mapstructure:",squash"`
	Client  *docker.Client `json:"-"`
	User    string         `default:"root" hash:"true"`

	// ContainerName specifies the name of the container to be created. If
	// nil, the job name will be used. If set to an empty string, Docker
	// will assign a random name.
	ContainerName *string `gcfg:"container-name" mapstructure:"container-name" hash:"true"`

	TTY bool `default:"false" hash:"true"`

	// do not use bool values with "default:true" because if
	// user would set it to "false" explicitly, it still will be
	// changed to "true" https://github.com/netresearch/ofelia/issues/135
	// so lets use strings here as workaround
	Delete string `default:"true" hash:"true"`
	Pull   string `default:"true" hash:"true"`

	Image       string   `hash:"true"`
	Network     string   `hash:"true"`
	Hostname    string   `hash:"true"`
	Entrypoint  *string  `hash:"true"`
	Container   string   `hash:"true"`
	Volume      []string `hash:"true"`
	VolumesFrom []string `gcfg:"volumes-from" mapstructure:"volumes-from," hash:"true"`
	Environment []string `mapstructure:"environment" hash:"true"`

	MaxRuntime time.Duration `gcfg:"max-runtime" mapstructure:"max-runtime"`

	containerID string
	mu          sync.RWMutex // Protect containerID access
}

func NewRunJob(c *docker.Client) *RunJob {
	return &RunJob{Client: c}
}

func (j *RunJob) setContainerID(id string) {
	j.mu.Lock()
	j.containerID = id
	j.mu.Unlock()
}

func (j *RunJob) getContainerID() string {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.containerID
}

func entrypointSlice(ep *string) []string {
	if ep == nil {
		return nil
	}
	return args.GetArgs(*ep)
}

func (j *RunJob) Run(ctx *Context) error {
	var container *docker.Container
	var err error
	pull, _ := strconv.ParseBool(j.Pull)

	if j.Image != "" && j.Container == "" {
		if err = func() error {
			var pullError error

			// if Pull option "true"
			// try pulling image first
			if pull {
				if pullError = pullImage(j.Client, j.Image); pullError == nil {
					ctx.Log("Pulled image " + j.Image)
					return nil
				}
			}

			// if Pull option "false"
			// try to find image locally first
			searchErr := j.searchLocalImage()
			if searchErr == nil {
				ctx.Log("Found locally image " + j.Image)
				return nil
			}

			// if couldn't find image locally, still try to pull
			if !pull && searchErr == ErrLocalImageNotFound {
				if pullError = pullImage(j.Client, j.Image); pullError == nil {
					ctx.Log("Pulled image " + j.Image)
					return nil
				}
			}

			if pullError != nil {
				return pullError
			}

			if searchErr != nil {
				return searchErr
			}

			return nil
		}(); err != nil {
			return err
		}

		container, err = j.buildContainer()
		if err != nil {
			return err
		}
	} else {
		container, err = j.Client.InspectContainer(j.Container)
		if err != nil {
			return err
		}
	}

	if container != nil {
		j.setContainerID(container.ID)
	}

	// cleanup container if it is a created one
	if j.Container == "" {
		defer func() {
			if delErr := j.deleteContainer(); delErr != nil {
				ctx.Warn("failed to delete container: " + delErr.Error())
			}
		}()
	}

	startTime := time.Now()
	if err := j.startContainer(); err != nil {
		return err
	}

	err = j.watchContainer()
	if err == ErrUnexpected {
		return err
	}

	if logsErr := j.Client.Logs(docker.LogsOptions{
		Container:    j.getContainerID(),
		OutputStream: ctx.Execution.OutputStream,
		ErrorStream:  ctx.Execution.ErrorStream,
		Stdout:       true,
		Stderr:       true,
		Since:        startTime.Unix(),
		RawTerminal:  j.TTY,
	}); logsErr != nil {
		ctx.Warn("failed to fetch container logs: " + logsErr.Error())
	}

	return err
}

func (j *RunJob) searchLocalImage() error {
	imgs, err := j.Client.ListImages(buildFindLocalImageOptions(j.Image))
	if err != nil {
		return err
	}

	if len(imgs) != 1 {
		return ErrLocalImageNotFound
	}

	return nil
}

func (j *RunJob) buildContainer() (*docker.Container, error) {
	name := j.Name
	if j.ContainerName != nil {
		name = *j.ContainerName
	}
	c, err := j.Client.CreateContainer(docker.CreateContainerOptions{
		Name: name,
		Config: &docker.Config{
			Image:        j.Image,
			AttachStdin:  false,
			AttachStdout: true,
			AttachStderr: true,
			Tty:          j.TTY,
			Cmd:          args.GetArgs(j.Command),
			Entrypoint:   entrypointSlice(j.Entrypoint),
			User:         j.User,
			Env:          j.Environment,
			Hostname:     j.Hostname,
		},
		NetworkingConfig: &docker.NetworkingConfig{},
		HostConfig: &docker.HostConfig{
			Binds:       j.Volume,
			VolumesFrom: j.VolumesFrom,
		},
	})

	if err != nil {
		return c, fmt.Errorf("error creating exec: %w", err)
	}

	if j.Network != "" {
		networkOpts := docker.NetworkFilterOpts{}
		networkOpts["name"] = map[string]bool{}
		networkOpts["name"][j.Network] = true
		if networks, err := j.Client.FilteredListNetworks(networkOpts); err == nil {
			for _, network := range networks {
				if err := j.Client.ConnectNetwork(network.ID, docker.NetworkConnectionOptions{
					Container: c.ID,
				}); err != nil {
					return c, fmt.Errorf("error connecting container to network: %w", err)
				}
			}
		}
	}

	return c, nil
}

func (j *RunJob) startContainer() error {
	return j.Client.StartContainer(j.getContainerID(), &docker.HostConfig{})
}

func (j *RunJob) stopContainer(timeout uint) error {
	return j.Client.StopContainer(j.getContainerID(), timeout)
}

func (j *RunJob) getContainer() (*docker.Container, error) {
	id := j.getContainerID()
	return j.Client.InspectContainer(id)
}

const (
	watchDuration = time.Millisecond * 100
)

func (j *RunJob) watchContainer() error {
	var s docker.State
	var r time.Duration
	for {
		time.Sleep(watchDuration)
		r += watchDuration

		if j.MaxRuntime > 0 && r > j.MaxRuntime {
			return ErrMaxTimeRunning
		}

		c, err := j.Client.InspectContainer(j.getContainerID())
		if err != nil {
			return err
		}

		if !c.State.Running {
			s = c.State
			break
		}
	}

	switch s.ExitCode {
	case 0:
		return nil
	case -1:
		return ErrUnexpected
	default:
		return fmt.Errorf("error non-zero exit code: %d", s.ExitCode)
	}
}

func (j *RunJob) deleteContainer() error {
	if delete, _ := strconv.ParseBool(j.Delete); !delete {
		return nil
	}
	return j.Client.RemoveContainer(docker.RemoveContainerOptions{
		ID: j.getContainerID(),
	})
}

func (j *RunJob) Hash() (string, error) {
	var h string
	if err := getHash(reflect.TypeOf(j).Elem(), reflect.ValueOf(j).Elem(), &h); err != nil {
		return "", err
	}
	return h, nil
}
