package debugbridge

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strings"

	lpkgo "github.com/lib-x/lpk-go"
	"github.com/lib-x/lpk-go/oci"
	"github.com/lib-x/lpk-go/remote"
)

func (client *Client) Version(ctx context.Context) (string, error) {
	if err := client.validate(ctx, "remote.debugbridge.version"); err != nil {
		return "", err
	}
	result, err := client.runner.Run(ctx, client.command(false, "version"))
	if err != nil {
		return "", err
	}
	var payload struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(result.Stdout, &payload); err != nil {
		return "", invalidRemoteData("remote.debugbridge.version")
	}
	value := strings.TrimSpace(payload.Version)
	if _, err := remote.Supports(remote.CapabilityLPKV2, value); err != nil {
		return "", invalidRemoteData("remote.debugbridge.version")
	}
	return value, nil
}

func (client *Client) Platform(ctx context.Context) (remote.Platform, error) {
	if err := client.validate(ctx, "remote.debugbridge.platform"); err != nil {
		return remote.Platform{}, err
	}
	result, err := client.runner.Run(ctx, client.command(false, "platform"))
	if err != nil {
		return remote.Platform{}, err
	}
	var payload struct {
		Platform string `json:"platform"`
	}
	if err := json.Unmarshal(result.Stdout, &payload); err != nil {
		return remote.Platform{}, invalidRemoteData("remote.debugbridge.platform")
	}
	platform, err := remote.ParsePlatform(payload.Platform)
	if err != nil {
		return remote.Platform{}, invalidRemoteData("remote.debugbridge.platform")
	}
	return platform, nil
}

func (client *Client) BuildPack(ctx context.Context, input remote.BuildPackRequest) (remote.BuildPackResult, error) {
	if err := client.validate(ctx, "remote.debugbridge.build_pack"); err != nil {
		return remote.BuildPackResult{}, err
	}
	tag := strings.TrimSpace(input.Tag)
	if tag == "" || input.Context == nil {
		return remote.BuildPackResult{}, bridgeError(lpkgo.CodeInvalidArgument, "remote.debugbridge.build_pack", errors.New("tag and context are required"))
	}
	args := []string{"build-pack", "--tag", tag}
	if input.ContextDigest != "" {
		if !input.ContextDigest.Valid() {
			return remote.BuildPackResult{}, bridgeError(lpkgo.CodeInvalidArgument, "remote.debugbridge.build_pack", errors.New("invalid context digest"))
		}
		args = append(args, "--context-digest", input.ContextDigest.String())
	}
	command := client.command(false, args...)
	command.Stdin = input.Context
	result, err := client.runner.Run(ctx, command)
	if err != nil {
		return remote.BuildPackResult{}, err
	}
	line := lastNonEmptyLine(result.Stdout)
	var payload remote.BuildPackResult
	if line == nil || json.Unmarshal(line, &payload) != nil || validateBuildPackResult(payload) != nil {
		return remote.BuildPackResult{}, invalidRemoteData("remote.debugbridge.build_pack")
	}
	return payload, nil
}

func (client *Client) PackImagesManifest(ctx context.Context, images []remote.PackImageSpec) (remote.PackManifest, error) {
	if err := client.validate(ctx, "remote.debugbridge.pack_images"); err != nil {
		return remote.PackManifest{}, err
	}
	if len(images) == 0 {
		return remote.PackManifest{}, bridgeError(lpkgo.CodeInvalidArgument, "remote.debugbridge.pack_images", errors.New("image specs are required"))
	}
	for _, spec := range images {
		if err := validatePackSpec(spec); err != nil {
			return remote.PackManifest{}, err
		}
	}
	encoded, err := json.Marshal(struct {
		Images []remote.PackImageSpec `json:"images"`
	}{Images: append([]remote.PackImageSpec(nil), images...)})
	if err != nil {
		return remote.PackManifest{}, bridgeError(lpkgo.CodeInvalidArgument, "remote.debugbridge.pack_images", err)
	}
	spec := base64.StdEncoding.EncodeToString(encoded)
	result, err := client.runner.Run(ctx, client.command(false, "pack-images", "--spec", spec, "--manifest-only"))
	if err != nil {
		return remote.PackManifest{}, err
	}
	var manifest remote.PackManifest
	if json.Unmarshal(result.Stdout, &manifest) != nil || validatePackManifest(manifest) != nil {
		return remote.PackManifest{}, invalidRemoteData("remote.debugbridge.pack_images")
	}
	return manifest, nil
}

func (client *Client) BlobCheck(ctx context.Context, digests []oci.Digest) ([]oci.Digest, error) {
	if err := client.validate(ctx, "remote.debugbridge.blob_check"); err != nil {
		return nil, err
	}
	if len(digests) == 0 {
		return nil, nil
	}
	args := []string{"blob-check"}
	requested := make(map[oci.Digest]struct{}, len(digests))
	for _, digest := range digests {
		if !digest.Valid() {
			return nil, bridgeError(lpkgo.CodeInvalidArgument, "remote.debugbridge.blob_check", errors.New("invalid blob digest"))
		}
		args = append(args, digest.String())
		requested[digest] = struct{}{}
	}
	result, err := client.runner.Run(ctx, client.command(false, args...))
	if err != nil {
		return nil, err
	}
	var payload struct {
		Missing []oci.Digest `json:"missing"`
	}
	if json.Unmarshal(result.Stdout, &payload) != nil {
		return nil, invalidRemoteData("remote.debugbridge.blob_check")
	}
	seen := make(map[oci.Digest]struct{}, len(payload.Missing))
	missing := make([]oci.Digest, 0, len(payload.Missing))
	for _, digest := range payload.Missing {
		if !digest.Valid() {
			return nil, invalidRemoteData("remote.debugbridge.blob_check")
		}
		if _, exists := requested[digest]; !exists {
			return nil, invalidRemoteData("remote.debugbridge.blob_check")
		}
		if _, exists := seen[digest]; exists {
			continue
		}
		seen[digest] = struct{}{}
		missing = append(missing, digest)
	}
	sort.Slice(missing, func(i, j int) bool { return missing[i] < missing[j] })
	return missing, nil
}

func (client *Client) BlobGet(ctx context.Context, digest oci.Digest, destination io.Writer) error {
	if err := client.validate(ctx, "remote.debugbridge.blob_get"); err != nil {
		return err
	}
	if !digest.Valid() || destination == nil {
		return bridgeError(lpkgo.CodeInvalidArgument, "remote.debugbridge.blob_get", errors.New("digest and destination are required"))
	}
	command := client.command(false, "blob-get", digest.String())
	command.Stdout = destination
	_, err := client.runner.Run(ctx, command)
	return err
}

func validateBuildPackResult(result remote.BuildPackResult) error {
	if strings.TrimSpace(result.Tag) == "" || strings.TrimSpace(result.ArchiveKey) == "" || !result.ImageID.Valid() || len(result.DiffIDs) == 0 {
		return errors.New("invalid build-pack result")
	}
	for _, digest := range append(append([]oci.Digest(nil), result.DiffIDs...), result.BaseDiffIDs...) {
		if !digest.Valid() {
			return errors.New("invalid build-pack digest")
		}
	}
	if value := strings.TrimSpace(result.BaseRepoDigest); value != "" && !strings.Contains(value, "@sha256:") {
		return errors.New("invalid base repository digest")
	}
	return nil
}

func validatePackSpec(spec remote.PackImageSpec) error {
	if strings.TrimSpace(spec.Ref) == "" || strings.TrimSpace(spec.Alias) == "" || !spec.ImageID.Valid() || (len(spec.EmbeddedDiffIDs) == 0 && strings.TrimSpace(spec.Upstream) == "") {
		return bridgeError(lpkgo.CodeInvalidArgument, "remote.debugbridge.pack_spec", errors.New("invalid pack image spec"))
	}
	for _, digest := range spec.EmbeddedDiffIDs {
		if !digest.Valid() {
			return bridgeError(lpkgo.CodeInvalidArgument, "remote.debugbridge.pack_spec", errors.New("invalid embedded diff ID"))
		}
	}
	return nil
}

func validatePackManifest(manifest remote.PackManifest) error {
	if manifest.Index.SchemaVersion != 2 || manifest.LockImages == nil || manifest.EmbeddedLayerBytes < 0 || manifest.EmbeddedLayerCount < 0 {
		return errors.New("invalid pack manifest")
	}
	seen := make(map[oci.Digest]struct{}, len(manifest.Blobs))
	for _, blob := range manifest.Blobs {
		if !blob.Digest.Valid() || blob.Size < 0 {
			return errors.New("invalid pack blob")
		}
		if _, exists := seen[blob.Digest]; exists {
			return errors.New("duplicate pack blob")
		}
		seen[blob.Digest] = struct{}{}
	}
	for alias, image := range manifest.LockImages {
		if strings.TrimSpace(alias) == "" || !image.ImageID.Valid() {
			return errors.New("invalid lock image")
		}
		for _, layer := range image.Layers {
			if !layer.Digest.Valid() || (layer.Source != oci.LayerSourceEmbed && layer.Source != oci.LayerSourceUpstream) {
				return errors.New("invalid lock layer")
			}
		}
	}
	for _, descriptor := range manifest.Index.Manifests {
		if !descriptor.Digest.Valid() || descriptor.Size < 0 || strings.TrimSpace(descriptor.MediaType) == "" {
			return errors.New("invalid index descriptor")
		}
	}
	return nil
}

func lastNonEmptyLine(data []byte) []byte {
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		line := strings.TrimSpace(lines[index])
		if line != "" {
			return []byte(line)
		}
	}
	return nil
}

func invalidRemoteData(op string) error {
	return bridgeError(lpkgo.CodeRemoteUnavailable, op, errors.New("invalid DebugBridge response"))
}
