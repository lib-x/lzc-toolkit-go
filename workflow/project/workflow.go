// Package projectworkflow composes build, deployment, synchronization, and
// startup without coupling the dependency-light workflow package to project
// lifecycle packages.
package projectworkflow

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
	"github.com/lib-x/lzc-toolkit-go/build"
	"github.com/lib-x/lzc-toolkit-go/project"
	projectrsync "github.com/lib-x/lzc-toolkit-go/project/rsync"
	"github.com/lib-x/lzc-toolkit-go/workflow"
)

const (
	StageBuild  workflow.Stage = "project.build"
	StageDeploy workflow.Stage = "project.deploy"
	StageSync   workflow.Stage = "project.sync"
	StageStart  workflow.Stage = "project.start"
)

type Builder interface {
	Build(context.Context, io.Writer) (build.Result, error)
}

type BuildFunc func(context.Context, io.Writer) (build.Result, error)

func (function BuildFunc) Build(ctx context.Context, destination io.Writer) (build.Result, error) {
	if function == nil {
		return build.Result{}, workflowError(lpkgo.CodeInvalidArgument, "workflow.project.build", errors.New("nil build function"))
	}
	return function(ctx, destination)
}

type Lifecycle interface {
	Deploy(context.Context, project.DeployRequest) (project.DeployResult, error)
	Start(context.Context, project.StartRequest) (project.Info, error)
}

type Synchronizer interface {
	Sync(context.Context) (projectrsync.Result, error)
}

type SyncFunc func(context.Context) (projectrsync.Result, error)

func (function SyncFunc) Sync(ctx context.Context) (projectrsync.Result, error) {
	if function == nil {
		return projectrsync.Result{}, workflowError(lpkgo.CodeInvalidArgument, "workflow.project.sync", errors.New("nil sync function"))
	}
	return function(ctx)
}

type Options struct {
	Builder   Builder
	Lifecycle Lifecycle
	Sync      Synchronizer
	Observer  workflow.Observer
	TempDir   string
}

type Request struct {
	DevID   string
	UserApp bool
	Restart bool
}

type Result struct {
	Build  build.Result
	Deploy project.DeployResult
	Sync   *projectrsync.Result
	Start  project.Info
}

type Runner struct{ options Options }

func New(options Options) (*Runner, error) {
	if options.Builder == nil || options.Lifecycle == nil {
		return nil, workflowError(lpkgo.CodeInvalidArgument, "workflow.project.new", errors.New("builder and lifecycle are required"))
	}
	return &Runner{options: options}, nil
}

func (runner *Runner) Run(ctx context.Context, request Request) (Result, error) {
	if ctx == nil || runner == nil || runner.options.Builder == nil || runner.options.Lifecycle == nil {
		return Result{}, workflowError(lpkgo.CodeInvalidArgument, "workflow.project.run", errors.New("invalid context or runner"))
	}
	if err := ctx.Err(); err != nil {
		return Result{}, workflowError(lpkgo.CodeCancelled, "workflow.project.run", err)
	}
	artifact, err := os.CreateTemp(runner.options.TempDir, "lzc-toolkit-*.lpk")
	if err != nil {
		return Result{}, workflowError(lpkgo.CodeCommandFailed, "workflow.project.artifact", errors.New("cannot create temporary LPK"))
	}
	filename := artifact.Name()
	defer func() {
		_ = artifact.Close()
		_ = os.Remove(filename)
	}()

	state := &runState{runner: runner, request: request, artifact: artifact}
	steps := []workflow.Step[*runState]{buildStep{}}
	steps = append(steps, deployStep{})
	if runner.options.Sync != nil {
		steps = append(steps, syncStep{})
	}
	steps = append(steps, startStep{})
	if err := workflow.NewPipeline(runner.options.Observer, steps...).Run(ctx, state); err != nil {
		return state.result, err
	}
	return state.result, nil
}

type runState struct {
	runner   *Runner
	request  Request
	artifact *os.File
	result   Result
}

type buildStep struct{}

func (buildStep) Name() workflow.Stage { return StageBuild }

func (buildStep) Run(ctx context.Context, state *runState) error {
	result, err := state.runner.options.Builder.Build(ctx, state.artifact)
	if err != nil {
		return err
	}
	if strings.TrimSpace(result.Package) == "" {
		return workflowError(lpkgo.CodeInvalidManifest, "workflow.project.build", errors.New("build result package is empty"))
	}
	info, err := state.artifact.Stat()
	if err != nil || info.Size() == 0 {
		return workflowError(lpkgo.CodeInvalidManifest, "workflow.project.build", errors.New("build produced an empty LPK"))
	}
	state.result.Build = result
	return nil
}

type deployStep struct{}

func (deployStep) Name() workflow.Stage { return StageDeploy }

func (deployStep) Run(ctx context.Context, state *runState) error {
	if _, err := state.artifact.Seek(0, io.SeekStart); err != nil {
		return workflowError(lpkgo.CodeCommandFailed, "workflow.project.artifact", errors.New("cannot rewind temporary LPK"))
	}
	result, err := state.runner.options.Lifecycle.Deploy(ctx, project.DeployRequest{
		Package: state.artifact, PackageID: state.result.Build.Package,
		DevID: strings.TrimSpace(state.request.DevID), UserApp: state.request.UserApp,
	})
	if err != nil {
		return err
	}
	state.result.Deploy = result
	return nil
}

type syncStep struct{}

func (syncStep) Name() workflow.Stage { return StageSync }

func (syncStep) Run(ctx context.Context, state *runState) error {
	result, err := state.runner.options.Sync.Sync(ctx)
	if err != nil {
		return err
	}
	state.result.Sync = &result
	return nil
}

type startStep struct{}

func (startStep) Name() workflow.Stage { return StageStart }

func (startStep) Run(ctx context.Context, state *runState) error {
	result, err := state.runner.options.Lifecycle.Start(ctx, project.StartRequest{
		AppID: state.result.Build.Package, LocalVersion: state.result.Build.Version, Restart: state.request.Restart,
	})
	if err != nil {
		return err
	}
	state.result.Start = result
	return nil
}

func workflowError(code lpkgo.Code, op string, cause error) error {
	return &lpkgo.Error{Code: code, Op: op, Cause: cause}
}
