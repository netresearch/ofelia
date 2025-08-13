package core

import (
	"crypto/rand"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/armon/circbuf"
	docker "github.com/fsouza/go-dockerclient"
)

var (
	// ErrSkippedExecution pass this error to `Execution.Stop` if you wish to mark
	// it as skipped.
	ErrSkippedExecution   = errors.New("skipped execution")
	ErrUnexpected         = errors.New("error unexpected, docker has returned exit code -1, maybe wrong user?")
	ErrMaxTimeRunning     = errors.New("the job has exceeded the maximum allowed time running")
	ErrLocalImageNotFound = errors.New("couldn't find image on the host")
)

const (
	// maximum size of a stdout/stderr stream to be kept in memory and optional stored/sent via mail
	maxStreamSize = 10 * 1024 * 1024
	logPrefix     = "[Job %q (%s)] %s"
)

type Job interface {
	GetName() string
	GetSchedule() string
	GetCommand() string
	Middlewares() []Middleware
	Use(...Middleware)
	Run(*Context) error
	Running() int32
	NotifyStart()
	NotifyStop()
	GetCronJobID() int
	SetCronJobID(int)
	GetHistory() []*Execution
}

type Context struct {
	Scheduler *Scheduler
	Logger    Logger
	Job       Job
	Execution *Execution

	current     int
	executed    bool
	middlewares []Middleware
}

func NewContext(s *Scheduler, j Job, e *Execution) *Context {
	return &Context{
		Scheduler:   s,
		Logger:      s.Logger,
		Job:         j,
		Execution:   e,
		middlewares: j.Middlewares(),
	}
}

func (c *Context) Start() {
	c.Execution.Start()
	c.Job.NotifyStart()
}

func (c *Context) Next() error {
	if err := c.doNext(); err != nil || c.executed {
		c.Stop(err)
	}

	return nil
}

func (c *Context) doNext() error {
	for {
		m, end := c.getNext()
		if end {
			break
		}

		if !c.Execution.IsRunning && !m.ContinueOnStop() {
			continue
		}

		if err := m.Run(c); err != nil {
			return fmt.Errorf("middleware run: %w", err)
		}
		return nil
	}

	if !c.Execution.IsRunning {
		return nil
	}

	c.executed = true
	if err := c.Job.Run(c); err != nil {
		return fmt.Errorf("job run: %w", err)
	}
	return nil
}

func (c *Context) getNext() (Middleware, bool) {
	if c.current >= len(c.middlewares) {
		return nil, true
	}

	c.current++
	return c.middlewares[c.current-1], false
}

func (c *Context) Stop(err error) {
	if !c.Execution.IsRunning {
		return
	}

	c.Execution.Stop(err)
	c.Job.NotifyStop()
}

func (c *Context) Log(msg string) {
	args := []interface{}{c.Job.GetName(), c.Execution.ID, msg}

	switch {
	case c.Execution.Failed:
		c.Logger.Errorf(logPrefix, args...)
	case c.Execution.Skipped:
		c.Logger.Warningf(logPrefix, args...)
	default:
		c.Logger.Noticef(logPrefix, args...)
	}
}

func (c *Context) Warn(msg string) {
	args := []interface{}{c.Job.GetName(), c.Execution.ID, msg}
	c.Logger.Warningf(logPrefix, args...)
}

// Execution contains all the information relative to a Job execution.
type Execution struct {
	ID        string
	Date      time.Time
	Duration  time.Duration
	IsRunning bool
	Failed    bool
	Skipped   bool
	Error     error

	OutputStream, ErrorStream *circbuf.Buffer `json:"-"`
}

// NewExecution returns a new Execution, with a random ID
func NewExecution() (*Execution, error) {
	bufOut, _ := circbuf.NewBuffer(maxStreamSize)
	bufErr, _ := circbuf.NewBuffer(maxStreamSize)
	id, err := randomID()
	if err != nil {
		return nil, err
	}
	return &Execution{
		ID:           id,
		OutputStream: bufOut,
		ErrorStream:  bufErr,
	}, nil
}

// Start starts the execution, initializes the running flags and the start date.
func (e *Execution) Start() {
	e.IsRunning = true
	e.Date = time.Now()
}

// Stop halts the execution. If a ErrSkippedExecution is given the execution
// is marked as skipped; if any other error is given the execution is marked as
// failed. Also mark the execution as IsRunning false and save the duration time
func (e *Execution) Stop(err error) {
	e.IsRunning = false
	// Guard against zero or unset start time and ensure a positive duration
	if e.Date.IsZero() {
		e.Date = time.Now()
	}
	e.Duration = time.Since(e.Date)
	if e.Duration <= 0 {
		e.Duration = time.Nanosecond
	}

	if err != nil && !errors.Is(err, ErrSkippedExecution) {
		e.Error = err
		e.Failed = true
	} else if errors.Is(err, ErrSkippedExecution) {
		e.Skipped = true
	}
}

// Middleware can wrap any job execution, allowing to execution code before
// or/and after of each `Job.Run`
type Middleware interface {
	// Run is called instead of the original `Job.Run`, you MUST call to `ctx.Run`
	// inside of the middleware `Run` function otherwise you will broken the
	// Job workflow.
	Run(*Context) error
	// ContinueOnStop reports whether Run should be called even when the
	// execution has been stopped
	ContinueOnStop() bool
}

type middlewareContainer struct {
	m     map[string]Middleware
	order []string
}

func (c *middlewareContainer) Use(ms ...Middleware) {
	if c.m == nil {
		c.m = make(map[string]Middleware, 0)
	}

	for _, m := range ms {
		if m == nil {
			continue
		}

		t := reflect.TypeOf(m).String()
		if _, ok := c.m[t]; ok {
			continue
		}

		c.order = append(c.order, t)
		c.m[t] = m
	}
}

func (c *middlewareContainer) ResetMiddlewares(ms ...Middleware) {
	c.m = nil
	c.order = nil
	c.Use(ms...)
}

func (c *middlewareContainer) Middlewares() []Middleware {
	ms := make([]Middleware, 0, len(c.order))
	for _, t := range c.order {
		ms = append(ms, c.m[t])
	}
	return ms
}

type Logger interface {
	Criticalf(format string, args ...interface{})
	Debugf(format string, args ...interface{})
	Errorf(format string, args ...interface{})
	Noticef(format string, args ...interface{})
	Warningf(format string, args ...interface{})
}

func randomID() (string, error) {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("rand read: %w", err)
	}

	return fmt.Sprintf("%x", b), nil
}

func buildFindLocalImageOptions(image string) docker.ListImagesOptions {
	return docker.ListImagesOptions{
		Filters: map[string][]string{
			"reference": {image},
		},
	}
}

func buildPullOptions(image string) (docker.PullImageOptions, docker.AuthConfiguration) {
	repository, tag := docker.ParseRepositoryTag(image)

	registry := parseRegistry(repository)
	// Override registry for two-part repository names (e.g., "repo/name" -> registry "repo")
	parts := strings.Split(repository, "/")
	if registry == "" && len(parts) > 1 {
		registry = parts[0]
	}

	const defaultTagLatest = "latest"
	if tag == "" {
		tag = defaultTagLatest
	}

	return docker.PullImageOptions{
		Repository: repository,
		Registry:   registry,
		Tag:        tag,
	}, buildAuthConfiguration(registry)
}

// pullImage downloads a Docker image if it is not available locally.
// It uses the provided client to fetch the image with authentication
// details from the Docker configuration file.
func pullImage(client *docker.Client, image string) error {
	opts, auth := buildPullOptions(image)
	if err := client.PullImage(opts, auth); err != nil {
		return fmt.Errorf("error pulling image %q: %w", image, err)
	}
	return nil
}

func parseRegistry(repository string) string {
	parts := strings.Split(repository, "/")
	if len(parts) < 2 {
		return ""
	}

	if strings.ContainsAny(parts[0], ".:") || len(parts) > 2 {
		return parts[0]
	}

	return ""
}

func buildAuthConfiguration(registry string) docker.AuthConfiguration {
	var auth docker.AuthConfiguration
	if dockercfg == nil {
		return auth
	}

	if v, ok := dockercfg.Configs[registry]; ok {
		return v
	}

	// try to fetch configs from docker hub default registry urls
	// see example here: https://www.projectatomic.io/blog/2016/03/docker-credentials-store/
	if registry == "" {
		if v, ok := dockercfg.Configs["https://index.docker.io/v2/"]; ok {
			return v
		}
		if v, ok := dockercfg.Configs["https://index.docker.io/v1/"]; ok {
			return v
		}
	}

	return auth
}

const HashmeTagName = "hash"

func getHash(t reflect.Type, v reflect.Value, hash *string) error {
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		fieldv := v.Field(i)
		kind := field.Type.Kind()

		if kind == reflect.Struct && field.Type != reflect.TypeOf(time.Duration(0)) {
			if err := getHash(field.Type, fieldv, hash); err != nil {
				return err
			}
			continue
		}

		hashmeTag := field.Tag.Get(HashmeTagName)
		if hashmeTag != "true" {
			continue
		}

		switch kind {
		case reflect.String:
			*hash += fieldv.String()
		case reflect.Int32, reflect.Int, reflect.Int64, reflect.Int16, reflect.Int8:
			*hash += strconv.FormatInt(fieldv.Int(), 10)
		case reflect.Bool:
			*hash += strconv.FormatBool(fieldv.Bool())
		case reflect.Slice:
			if field.Type.Elem().Kind() != reflect.String {
				return fmt.Errorf("unsupported field type")
			}
			strs := fieldv.Interface().([]string)
			for _, str := range strs {
				*hash += fmt.Sprintf("%d:%s,", len(str), str)
			}
		case reflect.Pointer:
			if fieldv.IsNil() {
				*hash += "<nil>"
				continue
			}
			elem := fieldv.Elem()
			if elem.Kind() == reflect.String {
				*hash += elem.String()
				continue
			}
			return fmt.Errorf("unsupported field type: field '%s' of type '%s'", field.Name, field.Type)
		// Other kinds are intentionally not part of the job hash. They are either
		// not used in our job structs today or would require a more elaborate
		// stable string representation that is out of scope here.
		default:
			return fmt.Errorf("unsupported field type: field '%s' of type '%s'", field.Name, field.Type)
		}
	}

	return nil
}
