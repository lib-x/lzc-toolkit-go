package project_test

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	lpkgo "github.com/lib-x/lpk-go"
	"github.com/lib-x/lpk-go/project"
	"github.com/lib-x/lpk-go/remote"
)

type fakeBackend struct {
	backendInfo remote.BackendInfo
	appInfo     remote.AppInfo
	appInfos    []remote.AppInfo
	devshell    []bool
	pauseInfo   *remote.AppInfo
	resumeInfo  *remote.AppInfo
	calls       []string
	installed   io.Reader
	syncRequest remote.SyncDevIDRequest
	uninstallID string
	deleteData  bool
}

func (backend *fakeBackend) Info(context.Context) (remote.BackendInfo, error) {
	backend.calls = append(backend.calls, "backend-info")
	return backend.backendInfo, nil
}

func (backend *fakeBackend) Install(_ context.Context, request remote.InstallRequest) error {
	backend.calls = append(backend.calls, "install")
	backend.installed = request.Package
	return nil
}

func (backend *fakeBackend) Status(context.Context, string) (string, error) {
	backend.calls = append(backend.calls, "status")
	return backend.appInfo.Status, nil
}

func (backend *fakeBackend) AppInfo(context.Context, string) (remote.AppInfo, error) {
	backend.calls = append(backend.calls, "app-info")
	if len(backend.appInfos) > 0 {
		result := backend.appInfos[0]
		backend.appInfos = backend.appInfos[1:]
		backend.appInfo = result
		return result, nil
	}
	return backend.appInfo, nil
}

func (backend *fakeBackend) SyncDevID(_ context.Context, request remote.SyncDevIDRequest) error {
	backend.calls = append(backend.calls, "sync-dev-id")
	backend.syncRequest = request
	return nil
}

func (backend *fakeBackend) IsDevshell(context.Context, string) (bool, error) {
	backend.calls = append(backend.calls, "is-devshell")
	if len(backend.devshell) > 0 {
		result := backend.devshell[0]
		backend.devshell = backend.devshell[1:]
		return result, nil
	}
	return true, nil
}

func TestDeployWaitsForContainerOnOlderBackend(t *testing.T) {
	backend := &fakeBackend{
		backendInfo: remote.BackendInfo{Version: "1.0.3"},
		appInfos: []remote.AppInfo{
			{AppID: "cloud.example.app", Status: "Running", InstanceStatus: "Status_Starting"},
			{AppID: "cloud.example.app", Status: "Running", InstanceStatus: "Status_Running"},
			{AppID: "cloud.example.app", Status: "Running", InstanceStatus: "Status_Running"},
		},
		devshell: []bool{false, true},
	}
	service, err := project.New(project.Options{Backend: backend, PollInterval: time.Nanosecond, StartTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}

	result, err := service.Deploy(t.Context(), project.DeployRequest{Package: &oneByteReader{}, PackageID: "cloud.example.app"})
	if err != nil {
		t.Fatal(err)
	}
	wantCalls := []string{"backend-info", "install", "app-info", "is-devshell", "app-info", "is-devshell", "sync-dev-id", "app-info"}
	if !equalStrings(backend.calls, wantCalls) {
		t.Fatalf("calls=%#v want=%#v", backend.calls, wantCalls)
	}
	if !result.WaitedForContainer || !result.SyncedDevID || !result.Info.Running {
		t.Fatalf("result=%#v", result)
	}
}

func TestWaitReturnsTerminalStartupFailure(t *testing.T) {
	backend := &fakeBackend{appInfo: remote.AppInfo{
		AppID: "cloud.example.app", Status: "Failed", InstanceStatus: "Status_Error", ErrorReason: "private remote detail",
	}}
	service, err := project.New(project.Options{Backend: backend, PollInterval: time.Nanosecond})
	if err != nil {
		t.Fatal(err)
	}

	_, err = service.Wait(t.Context(), project.WaitRequest{AppID: "cloud.example.app", State: project.WaitRunning, Timeout: time.Second})
	if !errors.Is(err, lpkgo.ErrCommandFailed) {
		t.Fatalf("error=%#v", err)
	}
	if err.Error() != string(lpkgo.CodeCommandFailed) {
		t.Fatalf("error text=%q", err.Error())
	}
}

func TestWaitHonorsContextCancellation(t *testing.T) {
	backend := &fakeBackend{appInfo: remote.AppInfo{AppID: "cloud.example.app", Status: "Running", InstanceStatus: "Status_Starting"}}
	service, err := project.New(project.Options{Backend: backend, PollInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err = service.Wait(ctx, project.WaitRequest{AppID: "cloud.example.app", State: project.WaitRunning})
	if !errors.Is(err, lpkgo.ErrCancelled) || !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%#v", err)
	}
}

func TestInfoRejectsMismatchedBackendIdentity(t *testing.T) {
	backend := &fakeBackend{appInfo: remote.AppInfo{AppID: "cloud.other.app"}}
	service, err := project.New(project.Options{Backend: backend})
	if err != nil {
		t.Fatal(err)
	}

	_, err = service.Info(t.Context(), project.InfoRequest{AppID: "cloud.example.app"})
	if !errors.Is(err, lpkgo.ErrRemoteUnavailable) {
		t.Fatalf("error=%#v", err)
	}
}

func TestWaitReturnsTypedDeadline(t *testing.T) {
	backend := &fakeBackend{appInfo: remote.AppInfo{AppID: "cloud.example.app", Status: "Running", InstanceStatus: "Status_Starting"}}
	service, err := project.New(project.Options{Backend: backend, PollInterval: time.Nanosecond})
	if err != nil {
		t.Fatal(err)
	}

	_, err = service.Wait(t.Context(), project.WaitRequest{AppID: "cloud.example.app", State: project.WaitRunning, Timeout: time.Nanosecond})
	if !errors.Is(err, lpkgo.ErrDeadlineExceeded) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error=%#v", err)
	}
}

func (backend *fakeBackend) Pause(context.Context, string) error {
	backend.calls = append(backend.calls, "pause")
	if backend.pauseInfo != nil {
		backend.appInfo = *backend.pauseInfo
	}
	return nil
}

func (backend *fakeBackend) Resume(context.Context, string) error {
	backend.calls = append(backend.calls, "resume")
	if backend.resumeInfo != nil {
		backend.appInfo = *backend.resumeInfo
	}
	return nil
}

func (backend *fakeBackend) Uninstall(_ context.Context, appID string, deleteData bool) error {
	backend.calls = append(backend.calls, "uninstall")
	backend.uninstallID = appID
	backend.deleteData = deleteData
	return nil
}

func (backend *fakeBackend) Docker(context.Context, remote.StreamRequest) (remote.Result, error) {
	return remote.Result{}, nil
}

func (backend *fakeBackend) DockerCompose(context.Context, remote.StreamRequest) (remote.Result, error) {
	return remote.Result{}, nil
}

func (backend *fakeBackend) HostReadFile(context.Context, string, io.Writer) error { return nil }

func TestDeploySyncsDevIDImmediatelyWhenBackendSupportsPendingSync(t *testing.T) {
	backend := &fakeBackend{
		backendInfo: remote.BackendInfo{Version: "1.0.4"},
		appInfo: remote.AppInfo{
			AppID: "cloud.example.app", Status: "Running", InstanceStatus: "Status_Starting",
		},
	}
	service, err := project.New(project.Options{Backend: backend})
	if err != nil {
		t.Fatal(err)
	}
	packageReader := &oneByteReader{}

	result, err := service.Deploy(t.Context(), project.DeployRequest{
		Package: packageReader, PackageID: "cloud.example.app", DevID: "dev-1", UserApp: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantCalls := []string{"backend-info", "install", "sync-dev-id", "app-info"}
	if !equalStrings(backend.calls, wantCalls) {
		t.Fatalf("calls=%#v want=%#v", backend.calls, wantCalls)
	}
	if backend.installed != packageReader {
		t.Fatal("Deploy did not pass the caller-owned package reader through")
	}
	if packageReader.closed {
		t.Fatal("Deploy closed the caller-owned package reader")
	}
	if backend.syncRequest.AppID != "cloud.example.app" || backend.syncRequest.DevID != "dev-1" || !backend.syncRequest.UserApp {
		t.Fatalf("sync request=%#v", backend.syncRequest)
	}
	if result.Info.AppID != "cloud.example.app" || result.WaitedForContainer || !result.SyncedDevID {
		t.Fatalf("result=%#v", result)
	}
}

func TestStartRestartPausesThenResumesAndWaitsForRunning(t *testing.T) {
	paused := remote.AppInfo{AppID: "cloud.example.app", Status: "Paused", InstanceStatus: "Status_Paused"}
	running := remote.AppInfo{AppID: "cloud.example.app", Status: "Running", InstanceStatus: "Status_Running", Version: "1.2.3"}
	backend := &fakeBackend{appInfo: running, pauseInfo: &paused, resumeInfo: &running}
	service, err := project.New(project.Options{Backend: backend, PollInterval: time.Nanosecond})
	if err != nil {
		t.Fatal(err)
	}

	info, err := service.Start(t.Context(), project.StartRequest{AppID: "cloud.example.app", LocalVersion: "1.2.3", Restart: true})
	if err != nil {
		t.Fatal(err)
	}
	wantCalls := []string{"app-info", "pause", "app-info", "resume", "app-info"}
	if !equalStrings(backend.calls, wantCalls) {
		t.Fatalf("calls=%#v want=%#v", backend.calls, wantCalls)
	}
	if !info.Running || !info.CurrentVersionDeployed {
		t.Fatalf("info=%#v", info)
	}
}

func TestStopPausesRunningProject(t *testing.T) {
	paused := remote.AppInfo{AppID: "cloud.example.app", Status: "Paused", InstanceStatus: "Status_Paused"}
	backend := &fakeBackend{
		appInfo:   remote.AppInfo{AppID: "cloud.example.app", Status: "Running", InstanceStatus: "Status_Running"},
		pauseInfo: &paused,
	}
	service, err := project.New(project.Options{Backend: backend, PollInterval: time.Nanosecond})
	if err != nil {
		t.Fatal(err)
	}

	info, err := service.Stop(t.Context(), project.StopRequest{AppID: "cloud.example.app"})
	if err != nil {
		t.Fatal(err)
	}
	if info.Running || !equalStrings(backend.calls, []string{"app-info", "pause", "app-info"}) {
		t.Fatalf("info=%#v calls=%#v", info, backend.calls)
	}
}

func TestUninstallMakesDataDeletionExplicit(t *testing.T) {
	for _, deleteData := range []bool{false, true} {
		name := "preserve data"
		if deleteData {
			name = "delete data"
		}
		t.Run(name, func(t *testing.T) {
			backend := &fakeBackend{}
			service, err := project.New(project.Options{Backend: backend})
			if err != nil {
				t.Fatal(err)
			}
			if err := service.Uninstall(t.Context(), project.UninstallRequest{AppID: "cloud.example.app", DeleteData: deleteData}); err != nil {
				t.Fatal(err)
			}
			if backend.uninstallID != "cloud.example.app" || backend.deleteData != deleteData {
				t.Fatalf("id=%q deleteData=%v", backend.uninstallID, backend.deleteData)
			}
		})
	}
}

type oneByteReader struct{ closed bool }

func (*oneByteReader) Read(buffer []byte) (int, error) {
	if len(buffer) == 0 {
		return 0, nil
	}
	buffer[0] = 'x'
	return 1, io.EOF
}

func (reader *oneByteReader) Close() error {
	reader.closed = true
	return nil
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
