package projectworkflow_test

import (
	"context"
	"errors"
	"io"
	"os"
	"reflect"
	"testing"

	lpkgo "github.com/lib-x/lpk-go"
	"github.com/lib-x/lpk-go/build"
	"github.com/lib-x/lpk-go/project"
	projectrsync "github.com/lib-x/lpk-go/project/rsync"
	"github.com/lib-x/lpk-go/workflow"
	projectworkflow "github.com/lib-x/lpk-go/workflow/project"
)

type fakeLifecycle struct {
	calls       *[]string
	packageData string
	deployErr   error
	startErr    error
	start       project.StartRequest
}

func (lifecycle *fakeLifecycle) Deploy(_ context.Context, request project.DeployRequest) (project.DeployResult, error) {
	*lifecycle.calls = append(*lifecycle.calls, "deploy")
	data, _ := io.ReadAll(request.Package)
	lifecycle.packageData = string(data)
	if lifecycle.deployErr != nil {
		return project.DeployResult{}, lifecycle.deployErr
	}
	return project.DeployResult{SyncedDevID: true}, nil
}

func (lifecycle *fakeLifecycle) Start(_ context.Context, request project.StartRequest) (project.Info, error) {
	*lifecycle.calls = append(*lifecycle.calls, "start")
	lifecycle.start = request
	if lifecycle.startErr != nil {
		return project.Info{}, lifecycle.startErr
	}
	return project.Info{AppID: request.AppID, Running: true}, nil
}

func TestWorkflowBuildsDeploysSyncsAndStartsInOrder(t *testing.T) {
	var calls []string
	lifecycle := &fakeLifecycle{calls: &calls}
	var events []workflow.Event
	runner, err := projectworkflow.New(projectworkflow.Options{
		Builder: projectworkflow.BuildFunc(func(_ context.Context, destination io.Writer) (build.Result, error) {
			calls = append(calls, "build")
			_, _ = io.WriteString(destination, "lpk-data")
			return build.Result{Package: "cloud.example.app", Version: "1.2.3"}, nil
		}),
		Lifecycle: lifecycle,
		Sync: projectworkflow.SyncFunc(func(context.Context) (projectrsync.Result, error) {
			calls = append(calls, "sync")
			return projectrsync.Result{Changed: true}, nil
		}),
		Observer: workflow.ObserverFunc(func(_ context.Context, event workflow.Event) {
			events = append(events, event)
		}),
		TempDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := runner.Run(t.Context(), projectworkflow.Request{DevID: "dev-1", UserApp: true, Restart: true})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(calls, []string{"build", "deploy", "sync", "start"}) {
		t.Fatalf("calls=%#v", calls)
	}
	if lifecycle.packageData != "lpk-data" || lifecycle.start.AppID != "cloud.example.app" || lifecycle.start.LocalVersion != "1.2.3" || !lifecycle.start.Restart {
		t.Fatalf("package=%q start=%#v", lifecycle.packageData, lifecycle.start)
	}
	if result.Sync == nil || !result.Sync.Changed || !result.Start.Running {
		t.Fatalf("result=%#v", result)
	}
	wantKinds := []workflow.EventKind{
		workflow.EventStarted, workflow.EventCompleted,
		workflow.EventStarted, workflow.EventCompleted,
		workflow.EventStarted, workflow.EventCompleted,
		workflow.EventStarted, workflow.EventCompleted,
	}
	var kinds []workflow.EventKind
	for _, event := range events {
		kinds = append(kinds, event.Kind)
	}
	if !reflect.DeepEqual(kinds, wantKinds) {
		t.Fatalf("events=%#v", events)
	}
}

func TestWorkflowStopsAfterBuildFailureAndCleansArtifact(t *testing.T) {
	tempDir := t.TempDir()
	buildErr := errors.New("build failed")
	var calls []string
	lifecycle := &fakeLifecycle{calls: &calls}
	runner, err := projectworkflow.New(projectworkflow.Options{
		Builder: projectworkflow.BuildFunc(func(_ context.Context, destination io.Writer) (build.Result, error) {
			_, _ = io.WriteString(destination, "partial")
			return build.Result{}, buildErr
		}),
		Lifecycle: lifecycle, TempDir: tempDir,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = runner.Run(t.Context(), projectworkflow.Request{})
	if !errors.Is(err, buildErr) || len(calls) != 0 {
		t.Fatalf("error=%v calls=%#v", err, calls)
	}
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("temporary artifacts remain: %#v", entries)
	}
}

func TestWorkflowStopsAfterDeployFailure(t *testing.T) {
	deployErr := errors.New("deploy failed")
	var calls []string
	lifecycle := &fakeLifecycle{calls: &calls, deployErr: deployErr}
	runner, err := projectworkflow.New(projectworkflow.Options{
		Builder: projectworkflow.BuildFunc(func(_ context.Context, destination io.Writer) (build.Result, error) {
			calls = append(calls, "build")
			_, _ = io.WriteString(destination, "lpk")
			return build.Result{Package: "cloud.example.app"}, nil
		}),
		Lifecycle: lifecycle,
		Sync: projectworkflow.SyncFunc(func(context.Context) (projectrsync.Result, error) {
			calls = append(calls, "sync")
			return projectrsync.Result{}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := runner.Run(t.Context(), projectworkflow.Request{})
	if !errors.Is(err, deployErr) || !reflect.DeepEqual(calls, []string{"build", "deploy"}) {
		t.Fatalf("error=%v calls=%#v", err, calls)
	}
	if result.Build.Package != "cloud.example.app" {
		t.Fatalf("partial result=%#v", result)
	}
}

func TestWorkflowHonorsPreCancelledContext(t *testing.T) {
	var calls []string
	runner, err := projectworkflow.New(projectworkflow.Options{
		Builder: projectworkflow.BuildFunc(func(context.Context, io.Writer) (build.Result, error) {
			calls = append(calls, "build")
			return build.Result{}, nil
		}),
		Lifecycle: &fakeLifecycle{calls: &calls},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = runner.Run(ctx, projectworkflow.Request{})
	if !errors.Is(err, context.Canceled) || len(calls) != 0 {
		t.Fatalf("error=%v calls=%#v", err, calls)
	}
}

func TestWorkflowRejectsSuccessfulEmptyArtifact(t *testing.T) {
	var calls []string
	runner, err := projectworkflow.New(projectworkflow.Options{
		Builder: projectworkflow.BuildFunc(func(context.Context, io.Writer) (build.Result, error) {
			return build.Result{Package: "cloud.example.app"}, nil
		}),
		Lifecycle: &fakeLifecycle{calls: &calls},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = runner.Run(t.Context(), projectworkflow.Request{})
	if !errors.Is(err, lpkgo.ErrInvalidManifest) || len(calls) != 0 {
		t.Fatalf("error=%#v calls=%#v", err, calls)
	}
}
