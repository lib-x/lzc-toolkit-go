package debugbridge_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"testing"

	lpkgo "github.com/lib-x/lpk-go"
	"github.com/lib-x/lpk-go/remote"
	"github.com/lib-x/lpk-go/remote/debugbridge"
)

type trackingReader struct {
	*bytes.Reader
	closed bool
}

func (reader *trackingReader) Close() error {
	reader.closed = true
	return nil
}

func TestLifecycleCommandsMatchBuildRemoteProtocol(t *testing.T) {
	runner := &fakeRunner{results: []remote.Result{
		{},
		{Stdout: []byte("Status_Running\n")},
		{Stdout: []byte(`{"appid":"cloud.example.app","deploy_id":"deploy-1","version":"1.2.3","domain":"example.heiyu.space","status":"Running","instance_status":"Status_Running"}`)},
		{},
		{Stdout: []byte("true\n")},
		{},
		{},
		{},
	}}
	client := debugbridge.New(runner, bridgeCommand, debugbridge.WithUID("user-1"))
	pkg := &trackingReader{Reader: bytes.NewReader([]byte("lpk-data"))}

	if err := client.Install(context.Background(), remote.InstallRequest{Package: pkg, PackageID: "cloud.example.app"}); err != nil {
		t.Fatal(err)
	}
	status, err := client.Status(context.Background(), "cloud.example.app")
	if err != nil || status != "Status_Running" {
		t.Fatalf("status=%q err=%v", status, err)
	}
	info, err := client.AppInfo(context.Background(), "cloud.example.app")
	if err != nil || info.AppID != "cloud.example.app" || info.DeployID != "deploy-1" || len(info.Raw) == 0 {
		t.Fatalf("info=%#v err=%v", info, err)
	}
	if err := client.SyncDevID(context.Background(), remote.SyncDevIDRequest{AppID: "cloud.example.app", DevID: "dev-1", UserApp: true}); err != nil {
		t.Fatal(err)
	}
	isDevshell, err := client.IsDevshell(context.Background(), "cloud.example.app")
	if err != nil || !isDevshell {
		t.Fatalf("isDevshell=%v err=%v", isDevshell, err)
	}
	if err := client.Pause(context.Background(), "cloud.example.app"); err != nil {
		t.Fatal(err)
	}
	if err := client.Resume(context.Background(), "cloud.example.app"); err != nil {
		t.Fatal(err)
	}
	if err := client.Uninstall(context.Background(), "cloud.example.app", true); err != nil {
		t.Fatal(err)
	}

	wantArgs := [][]string{
		{"install", "--uid", "user-1", "--pkgId", "cloud.example.app"},
		{"status", "--uid", "user-1", "cloud.example.app"},
		{"info", "--uid", "user-1", "cloud.example.app"},
		{"sync-dev-id", "--uid", "user-1", "--dev-id", "dev-1", "--userapp", "cloud.example.app"},
		{"isDevshellV2", "--uid", "user-1", "cloud.example.app"},
		{"pause", "--uid", "user-1", "cloud.example.app"},
		{"resume", "--uid", "user-1", "cloud.example.app"},
		{"uninstall", "--uid", "user-1", "--delete-data", "cloud.example.app"},
	}
	for index, want := range wantArgs {
		if !reflect.DeepEqual(runner.commands[index].Args, want) {
			t.Fatalf("command %d args=%#v want=%#v", index, runner.commands[index].Args, want)
		}
	}
	data, err := io.ReadAll(runner.commands[0].Stdin)
	if err != nil || string(data) != "lpk-data" {
		t.Fatalf("install stdin=%q err=%v", data, err)
	}
	if pkg.closed {
		t.Fatal("Install closed the caller-owned package reader")
	}
}

func TestLifecycleRejectsMissingUIDAndMalformedResponses(t *testing.T) {
	t.Run("missing UID", func(t *testing.T) {
		client := debugbridge.New(&fakeRunner{}, bridgeCommand)
		err := client.Pause(context.Background(), "cloud.example.app")
		if !errors.Is(err, lpkgo.ErrInvalidConfig) {
			t.Fatalf("error=%#v", err)
		}
	})

	t.Run("malformed app info", func(t *testing.T) {
		runner := &fakeRunner{results: []remote.Result{{Stdout: []byte(`{"appid":"cloud.other.app"}`)}}}
		client := debugbridge.New(runner, bridgeCommand, debugbridge.WithUID("user-1"))
		_, err := client.AppInfo(context.Background(), "cloud.example.app")
		if !errors.Is(err, lpkgo.ErrRemoteUnavailable) {
			t.Fatalf("error=%#v", err)
		}
	})

	t.Run("malformed devshell response", func(t *testing.T) {
		runner := &fakeRunner{results: []remote.Result{{Stdout: []byte("yes")}}}
		client := debugbridge.New(runner, bridgeCommand, debugbridge.WithUID("user-1"))
		_, err := client.IsDevshell(context.Background(), "cloud.example.app")
		if !errors.Is(err, lpkgo.ErrRemoteUnavailable) {
			t.Fatalf("error=%#v", err)
		}
	})
}

func TestLifecycleErrorsDoNotExposeRunnerOutput(t *testing.T) {
	runner := &fakeRunner{errors: []error{errors.New("secret remote stdout")}}
	client := debugbridge.New(runner, bridgeCommand, debugbridge.WithUID("user-1"))
	err := client.Pause(context.Background(), "cloud.example.app")
	if !errors.Is(err, lpkgo.ErrCommandFailed) {
		t.Fatalf("error=%#v", err)
	}
	if err.Error() != string(lpkgo.CodeCommandFailed) {
		t.Fatalf("error text=%q", err.Error())
	}
}
