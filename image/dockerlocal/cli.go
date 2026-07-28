package dockerlocal

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	imagebuild "github.com/lib-x/lzc-toolkit-go/image"
	"github.com/lib-x/lzc-toolkit-go/oci"
)

const (
	maxCommandOutput             = 16 << 20
	maxDockerArchiveConfigBytes  = 2 << 20
	maxDockerArchiveMetadata     = 32 << 20
	maxDockerArchiveMetadataFile = 4096
)

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

type dockerInspectResponse struct {
	ID         string `json:"Id"`
	Descriptor struct {
		Digest string `json:"digest"`
	} `json:"Descriptor"`
	RootFS struct {
		Layers []oci.Digest `json:"Layers"`
	} `json:"RootFS"`
	RepoDigests []string `json:"RepoDigests"`
}

type dockerArchiveManifest struct {
	Config   string   `json:"Config"`
	RepoTags []string `json:"RepoTags"`
}

func inspectImage(ctx context.Context, ref string) (inspectedImage, error) {
	output, err := run(ctx, "docker", []string{"image", "inspect", ref}, "", nil, nil)
	if err != nil {
		return inspectedImage{}, err
	}
	var response []dockerInspectResponse
	if err := json.Unmarshal(output, &response); err != nil || len(response) == 0 {
		return inspectedImage{}, errors.New("invalid docker image inspect response")
	}
	imageID, err := oci.ParseDigest(response[0].ID)
	if err != nil || len(response[0].RootFS.Layers) == 0 {
		return inspectedImage{}, errors.New("invalid docker image metadata")
	}
	if shouldResolveDockerArchiveConfigImageID(response[0].Descriptor.Digest, imageID) {
		imageID, err = resolveDockerArchiveConfigImageID(ctx, ref)
		if err != nil {
			return inspectedImage{}, err
		}
	}
	return inspectedImage{ImageID: imageID, DiffIDs: response[0].RootFS.Layers, RepoDigests: response[0].RepoDigests}, nil
}

func shouldResolveDockerArchiveConfigImageID(descriptor string, inspectID oci.Digest) bool {
	descriptor = strings.TrimSpace(descriptor)
	if descriptor == "" {
		return false
	}
	descriptorDigest, err := oci.ParseDigest(descriptor)
	return err != nil || descriptorDigest == inspectID
}

func resolveDockerArchiveConfigImageID(ctx context.Context, ref string) (result oci.Digest, resultErr error) {
	archiveFile, err := os.CreateTemp("", "lzc-toolkit-image-id-*.tar")
	if err != nil {
		return "", errors.New("create Docker image archive")
	}
	archivePath := archiveFile.Name()
	defer func() {
		resultErr = errors.Join(resultErr, os.Remove(archivePath))
	}()
	if _, err := run(ctx, "docker", []string{"image", "save", ref}, "", nil, archiveFile); err != nil {
		_ = archiveFile.Close()
		return "", err
	}
	if err := archiveFile.Sync(); err != nil {
		_ = archiveFile.Close()
		return "", errors.New("sync Docker image archive")
	}
	if err := archiveFile.Close(); err != nil {
		return "", errors.New("close Docker image archive")
	}
	return dockerArchiveConfigImageID(ctx, archivePath, ref)
}

func dockerArchiveConfigImageID(ctx context.Context, archivePath, ref string) (oci.Digest, error) {
	if ctx == nil {
		return "", errors.New("nil context")
	}
	file, err := os.Open(filepath.Clean(archivePath))
	if err != nil {
		return "", errors.New("open Docker image archive")
	}
	defer file.Close()
	reader := tar.NewReader(file)
	metadata := make(map[string][]byte)
	metadataBytes := int64(0)
	for entries := 0; ; entries++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if entries > maxDockerArchiveMetadataFile {
			return "", errors.New("Docker image archive metadata entry limit exceeded")
		}
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", errors.New("read Docker image archive")
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			continue
		}
		name, ok := normalizeDockerArchivePath(header.Name)
		if !ok || header.Size < 0 {
			return "", errors.New("invalid Docker image archive metadata path")
		}
		limit := int64(maxDockerArchiveConfigBytes)
		if name == "manifest.json" {
			limit = maxCommandOutput
		}
		isMetadata := name == "manifest.json" || strings.HasSuffix(name, ".json") || strings.HasPrefix(name, "blobs/sha256/")
		if !isMetadata || header.Size > limit {
			continue
		}
		if metadataBytes+header.Size > maxDockerArchiveMetadata {
			return "", errors.New("Docker image archive metadata size limit exceeded")
		}
		data, err := readDockerArchiveEntry(ctx, reader, header.Size)
		if err != nil {
			return "", err
		}
		metadata[name] = data
		metadataBytes += header.Size
	}
	manifestData := metadata["manifest.json"]
	var manifests []dockerArchiveManifest
	if len(manifestData) == 0 || json.Unmarshal(manifestData, &manifests) != nil || len(manifests) == 0 {
		return "", errors.New("invalid Docker image archive manifest")
	}
	selected := manifests[0]
	for _, candidate := range manifests {
		for _, repoTag := range candidate.RepoTags {
			if strings.TrimSpace(repoTag) == strings.TrimSpace(ref) {
				selected = candidate
				break
			}
		}
	}
	configName, ok := normalizeDockerArchivePath(selected.Config)
	if !ok {
		return "", errors.New("invalid Docker image archive config path")
	}
	configData := metadata[configName]
	if len(configData) == 0 {
		return "", errors.New("Docker image archive config is missing")
	}
	sum := sha256.Sum256(configData)
	digest, err := oci.ParseDigest(fmt.Sprintf("sha256:%x", sum[:]))
	if err != nil {
		return "", errors.New("invalid Docker image archive config digest")
	}
	return digest, nil
}

func normalizeDockerArchivePath(name string) (string, bool) {
	normalized := strings.TrimPrefix(strings.TrimSpace(name), "./")
	return normalized, normalized != "" && normalized != "." && fs.ValidPath(normalized) && path.Clean(normalized) == normalized && !strings.ContainsRune(normalized, '\\')
}

func readDockerArchiveEntry(ctx context.Context, reader io.Reader, size int64) ([]byte, error) {
	var output bytes.Buffer
	remaining := size
	buffer := make([]byte, 32<<10)
	for remaining > 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		chunk := int64(len(buffer))
		if remaining < chunk {
			chunk = remaining
		}
		read, err := io.ReadFull(reader, buffer[:chunk])
		if err != nil {
			return nil, errors.New("read Docker image archive metadata")
		}
		_, _ = output.Write(buffer[:read])
		remaining -= int64(read)
	}
	return output.Bytes(), nil
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
