package inspect

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	lpkgo "github.com/lib-x/lpk-go"
	"github.com/lib-x/lpk-go/archive"
	"github.com/lib-x/lpk-go/lpk"
	"go.yaml.in/yaml/v3"
)

var sha256DigestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

type imageLock struct {
	Images map[string]imageLockImage `yaml:"images"`
}

type imageLockImage struct {
	ImageID  string           `yaml:"image_id"`
	Upstream string           `yaml:"upstream"`
	Layers   []imageLockLayer `yaml:"layers"`
}

type imageLockLayer struct {
	Digest string `yaml:"digest"`
	Source string `yaml:"source"`
}

func summarizeImages(ctx context.Context, r *lpk.Reader, entries map[string]archive.Entry) (ImageInfo, error) {
	data, err := readEntryBytes(ctx, r, "images.lock", "inspect.images")
	if err != nil {
		return ImageInfo{}, err
	}
	var lock imageLock
	if err := yaml.Unmarshal(data, &lock); err != nil {
		return ImageInfo{}, inspectError(lpkgo.CodeInvalidManifest, "inspect.images", fmt.Errorf("invalid images.lock YAML"))
	}
	return buildImageInfo(lock, entries), nil
}

func buildImageInfo(lock imageLock, entries map[string]archive.Entry) ImageInfo {
	aliases := make([]string, 0, len(lock.Images))
	for alias := range lock.Images {
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)

	allEmbeddedDigests := make(map[string]struct{})
	details := make([]ImageDetail, 0, len(aliases))
	for _, alias := range aliases {
		image := lock.Images[alias]
		detail := ImageDetail{
			Alias:    alias,
			ImageID:  strings.TrimSpace(image.ImageID),
			Upstream: strings.TrimSpace(image.Upstream),
		}
		uniqueAliasDigests := make(map[string]struct{})
		for _, layer := range image.Layers {
			digest := normalizeDigest(layer.Digest)
			if digest == "" {
				continue
			}
			source := strings.ToLower(strings.TrimSpace(layer.Source))
			if source != "embed" {
				detail.UpstreamLayerCount++
				continue
			}
			detail.EmbeddedLayerCount++
			uniqueAliasDigests[digest] = struct{}{}
			allEmbeddedDigests[digest] = struct{}{}
			entry, ok := entries[blobEntryName(digest)]
			if ok && entry.Type == archive.EntryRegular {
				detail.EmbeddedBytes += entry.Size
			} else {
				detail.MissingEmbeddedLayerCount++
			}
		}
		detail.UniqueEmbeddedLayerCount = len(uniqueAliasDigests)
		details = append(details, detail)
	}

	info := ImageInfo{
		Aliases:                 aliases,
		Details:                 details,
		TotalEmbeddedLayerCount: len(allEmbeddedDigests),
	}
	for digest := range allEmbeddedDigests {
		entry, ok := entries[blobEntryName(digest)]
		if ok && entry.Type == archive.EntryRegular {
			info.TotalEmbeddedBytes += entry.Size
		} else {
			info.TotalMissingEmbeddedLayerCount++
		}
	}
	return info
}

func normalizeDigest(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	if !sha256DigestPattern.MatchString(value) {
		return ""
	}
	return value
}

func blobEntryName(digest string) string {
	return "images/blobs/sha256/" + strings.TrimPrefix(digest, "sha256:")
}
