package dockerlocal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"

	imagebuild "github.com/lib-x/lzc-toolkit-go/image"
	"github.com/lib-x/lzc-toolkit-go/oci"
)

const maxCommandOutput = 16 << 20

type CLIEngine struct{}

func (CLIEngine) Build(ctx context.Context, request BuildRequest) (BuildResult, error) {
	if _, err := run(ctx, "docker", []string{"buildx", "version"}, request.Entry.ContextDir, nil, nil); err != nil {
		return BuildResult{}, err
	}
	dockerfile, cleanup, err := dockerfileForEntry(request.Entry)
	if err != nil {
		return BuildResult{}, err
	}
	defer cleanup()
	ref := "debug.bridge/" + request.Entry.ImageLabel
	args := []string{"buildx", "build", "--platform", request.Platform, "--load", "--tag", ref, "--file", dockerfile, "."}
	if _, err := run(ctx, "docker", args, request.Entry.ContextDir, map[string]string{"DOCKER_BUILDKIT": "1"}, nil); err != nil {
		return BuildResult{}, err
	}
	built, err := inspectImage(ctx, ref)
	if err != nil {
		return BuildResult{}, err
	}
	result := BuildResult{Ref: ref, ImageID: built.ImageID, DiffIDs: built.DiffIDs}
	baseRef := finalExternalBase(dockerfileContent(request.Entry, dockerfile))
	if baseRef != "" && strings.TrimSpace(request.Entry.UpstreamMatch) != "" {
		if base, inspectErr := inspectImage(ctx, baseRef); inspectErr == nil && digestPrefix(built.DiffIDs, base.DiffIDs) {
			if upstream := matchingRepoDigest(base.RepoDigests, request.Entry.UpstreamMatch); upstream != "" {
				result.Upstream = upstream
				result.UpstreamDiffIDs = base.DiffIDs
			}
		}
	}
	return result, nil
}

func (CLIEngine) Save(ctx context.Context, refs []string, destination io.Writer) error {
	if destination == nil || len(refs) == 0 {
		return errors.New("nil destination or empty image refs")
	}
	_, err := run(ctx, "docker", append([]string{"image", "save"}, refs...), "", nil, destination)
	return err
}

func (CLIEngine) Remove(ctx context.Context, ref string) error {
	_, err := run(ctx, "docker", []string{"image", "rm", "-f", ref}, "", nil, io.Discard)
	return err
}

type inspectedImage struct {
	ImageID     oci.Digest
	DiffIDs     []oci.Digest
	RepoDigests []string
}

func inspectImage(ctx context.Context, ref string) (inspectedImage, error) {
	output, err := run(ctx, "docker", []string{"image", "inspect", ref}, "", nil, nil)
	if err != nil {
		return inspectedImage{}, err
	}
	var response []struct {
		ID     string `json:"Id"`
		RootFS struct {
			Layers []oci.Digest `json:"Layers"`
		} `json:"RootFS"`
		RepoDigests []string `json:"RepoDigests"`
	}
	if err := json.Unmarshal(output, &response); err != nil || len(response) == 0 {
		return inspectedImage{}, errors.New("invalid docker image inspect response")
	}
	imageID, err := oci.ParseDigest(response[0].ID)
	if err != nil || len(response[0].RootFS.Layers) == 0 {
		return inspectedImage{}, errors.New("invalid docker image metadata")
	}
	return inspectedImage{ImageID: imageID, DiffIDs: response[0].RootFS.Layers, RepoDigests: response[0].RepoDigests}, nil
}

func run(ctx context.Context, executable string, args []string, directory string, extraEnv map[string]string, stdout io.Writer) ([]byte, error) {
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	command := exec.CommandContext(ctx, executable, args...)
	command.Dir = directory
	command.Env = append([]string(nil), os.Environ()...)
	for key, value := range extraEnv {
		command.Env = append(command.Env, key+"="+value)
	}
	var captured limitedBuffer
	if stdout == nil {
		command.Stdout = &captured
	} else {
		command.Stdout = stdout
	}
	command.Stderr = &limitedBuffer{}
	if err := command.Run(); err != nil {
		return nil, errors.New("docker command failed")
	}
	return captured.Bytes(), nil
}

type limitedBuffer struct{ bytes.Buffer }

func (b *limitedBuffer) Write(data []byte) (int, error) {
	if b.Len()+len(data) > maxCommandOutput {
		return 0, errors.New("command output exceeds limit")
	}
	return b.Buffer.Write(data)
}

func dockerfileForEntry(entry imagebuild.Entry) (string, func(), error) {
	if entry.DockerfileContent == "" {
		return entry.DockerfilePath, func() {}, nil
	}
	file, err := os.CreateTemp(entry.ContextDir, ".lpk-Dockerfile-*")
	if err != nil {
		return "", nil, err
	}
	name := file.Name()
	if _, err := file.WriteString(entry.DockerfileContent); err != nil {
		_ = file.Close()
		_ = os.Remove(name)
		return "", nil, err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(name)
		return "", nil, err
	}
	return name, func() { _ = os.Remove(name) }, nil
}

func dockerfileContent(entry imagebuild.Entry, filename string) string {
	if entry.DockerfileContent != "" {
		return entry.DockerfileContent
	}
	data, _ := os.ReadFile(filename)
	return string(data)
}

func finalExternalBase(content string) string {
	stages := make(map[string]string)
	last := ""
	for _, line := range strings.Split(content, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 2 || strings.ToUpper(fields[0]) != "FROM" {
			continue
		}
		index := 1
		for index < len(fields) && strings.HasPrefix(fields[index], "--") {
			index++
		}
		if index >= len(fields) {
			continue
		}
		last = fields[index]
		if index+2 < len(fields) && strings.EqualFold(fields[index+1], "AS") {
			stages[strings.ToLower(fields[index+2])] = last
		}
	}
	seen := make(map[string]struct{})
	for last != "" {
		key := strings.ToLower(last)
		if _, exists := seen[key]; exists {
			return ""
		}
		seen[key] = struct{}{}
		parent, stage := stages[key]
		if !stage {
			return last
		}
		last = parent
	}
	return ""
}

func matchingRepoDigest(values []string, prefix string) string {
	for _, value := range values {
		repository := value
		if at := strings.Index(repository, "@sha256:"); at >= 0 {
			repository = repository[:at]
		}
		if strings.HasPrefix(repository, prefix) {
			return value
		}
	}
	return ""
}

func digestPrefix(all, prefix []oci.Digest) bool {
	if len(prefix) > len(all) {
		return false
	}
	for index := range prefix {
		if all[index] != prefix[index] {
			return false
		}
	}
	return true
}
