package debugbridge_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"testing"

	lpkgo "github.com/lib-x/lpk-go"
	"github.com/lib-x/lpk-go/oci"
	"github.com/lib-x/lpk-go/remote"
	"github.com/lib-x/lpk-go/remote/debugbridge"
)

type fakeRunner struct {
	commands []remote.Command
	results  []remote.Result
	errors   []error
}

func (runner *fakeRunner) Run(_ context.Context, command remote.Command) (remote.Result, error) {
	copyCommand := command
	copyCommand.Args = append([]string(nil), command.Args...)
	if command.Stdin != nil {
		data, _ := io.ReadAll(command.Stdin)
		copyCommand.Stdin = bytes.NewReader(data)
	}
	runner.commands = append(runner.commands, copyCommand)
	index := len(runner.commands) - 1
	var result remote.Result
	if index < len(runner.results) {
		result = runner.results[index]
	}
	if command.Stdout != nil {
		_, _ = command.Stdout.Write(result.Stdout)
	}
	var err error
	if index < len(runner.errors) {
		err = runner.errors[index]
	}
	return result, err
}

func bridgeCommand(tty bool, args ...string) remote.Command {
	command := remote.NewCommand("bridge", args...)
	command.TTY = tty
	return command
}

func TestClientParsesBackendInfo(t *testing.T) {
	runner := &fakeRunner{results: []remote.Result{
		{Stdout: []byte(`{"version":"1.0.5"}`)},
		{Stdout: []byte(`{"platform":"linux/arm64"}`)},
	}}
	client := debugbridge.New(runner, bridgeCommand)

	info, err := client.Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Version != "1.0.5" || info.Platform.String() != "linux/arm64" {
		t.Fatalf("info = %#v", info)
	}
	if !reflect.DeepEqual(runner.commands[0].Args, []string{"version"}) || !reflect.DeepEqual(runner.commands[1].Args, []string{"platform"}) {
		t.Fatalf("commands = %#v", runner.commands)
	}
}

func TestClientBuildPackStreamsContextAndParsesLastLine(t *testing.T) {
	imageID := digestOf("config")
	diffID := digestOf("layer")
	contextDigest := digestOf("context")
	runner := &fakeRunner{results: []remote.Result{{Stdout: []byte("building\n" + `{"tag":"debug.bridge/demo","archiveKey":"archive-1","imageID":"` + imageID.String() + `","diffIDs":["` + diffID.String() + `"]}` + "\n")}}}
	client := debugbridge.New(runner, bridgeCommand)

	result, err := client.BuildPack(context.Background(), remote.BuildPackRequest{
		Tag: "debug.bridge/demo", Context: bytes.NewBufferString("tar-data"), ContextDigest: contextDigest,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ImageID != imageID || len(result.DiffIDs) != 1 || result.DiffIDs[0] != diffID || result.ArchiveKey != "archive-1" {
		t.Fatalf("result = %#v", result)
	}
	command := runner.commands[0]
	if !reflect.DeepEqual(command.Args, []string{"build-pack", "--tag", "debug.bridge/demo", "--context-digest", contextDigest.String()}) {
		t.Fatalf("args = %#v", command.Args)
	}
	data, _ := io.ReadAll(command.Stdin)
	if string(data) != "tar-data" {
		t.Fatalf("stdin = %q", data)
	}
}

func TestClientPackManifestAndBlobProtocol(t *testing.T) {
	imageID := digestOf("config")
	layer := digestOf("layer")
	manifestDigest := digestOf("manifest")
	manifestJSON := `{"blobs":[{"digest":"` + manifestDigest.String() + `","size":8}],"index":{"schemaVersion":2,"manifests":[]},"lockImages":{"web":{"image_id":"` + imageID.String() + `","upstream":"","layers":[{"digest":"` + layer.String() + `","source":"embed"}]}},"embeddedLayerBytes":8,"embeddedLayerCount":1}`
	runner := &fakeRunner{results: []remote.Result{
		{Stdout: []byte(manifestJSON)},
		{Stdout: []byte(`{"missing":["` + layer.String() + `"]}`)},
		{Stdout: []byte("blob-data")},
	}}
	client := debugbridge.New(runner, bridgeCommand)

	manifest, err := client.PackImagesManifest(context.Background(), []remote.PackImageSpec{{
		Ref: "archive-key:key", Alias: "web", ImageID: imageID, EmbeddedDiffIDs: []oci.Digest{layer}, ArchiveKey: "key",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Blobs) != 1 || manifest.Blobs[0].Digest != manifestDigest || manifest.LockImages["web"].ImageID != imageID {
		t.Fatalf("manifest = %#v", manifest)
	}
	encodedSpec := runner.commands[0].Args[2]
	decoded, err := base64.StdEncoding.DecodeString(encodedSpec)
	if err != nil {
		t.Fatal(err)
	}
	var specBody map[string]any
	if err := json.Unmarshal(decoded, &specBody); err != nil {
		t.Fatal(err)
	}
	if _, ok := specBody["images"]; !ok {
		t.Fatalf("spec = %s", decoded)
	}

	missing, err := client.BlobCheck(context.Background(), []oci.Digest{manifestDigest, layer})
	if err != nil || len(missing) != 1 || missing[0] != layer {
		t.Fatalf("missing=%#v err=%v", missing, err)
	}
	var blob bytes.Buffer
	if err := client.BlobGet(context.Background(), manifestDigest, &blob); err != nil || blob.String() != "blob-data" {
		t.Fatalf("blob=%q err=%v", blob.String(), err)
	}
}

func TestClientRejectsMalformedRemoteData(t *testing.T) {
	runner := &fakeRunner{results: []remote.Result{{Stdout: []byte(`{"version":"secret"}`)}}}
	client := debugbridge.New(runner, bridgeCommand)
	if _, err := client.Info(context.Background()); !errors.Is(err, lpkgo.ErrRemoteUnavailable) {
		t.Fatalf("error = %#v", err)
	}
}

func digestOf(value string) oci.Digest {
	digest, _ := oci.ParseDigest("sha256:" + sha256Hex(value))
	return digest
}

func sha256Hex(value string) string {
	hash := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%x", hash)
}
