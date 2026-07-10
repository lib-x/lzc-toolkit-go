package oci_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
	"testing/fstest"

	lpkgo "github.com/lib-x/lpk-go"
	"github.com/lib-x/lpk-go/oci"
	"go.yaml.in/yaml/v3"
)

func TestValidateAcceptsLzcCLICompatibleMixedImageLayout(t *testing.T) {
	root := validLayout(t, true)

	report, err := oci.Validate(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if report.ImageCount != 1 || report.EmbeddedLayerCount != 1 || report.UpstreamLayerCount != 1 {
		t.Fatalf("report = %#v", report)
	}
	if report.EmbeddedBytes == 0 {
		t.Fatal("EmbeddedBytes = 0")
	}
	if report.ResolvedByAlias["app"] == "" {
		t.Fatalf("ResolvedByAlias = %#v", report.ResolvedByAlias)
	}
}

func TestValidateRejectsMissingEmbeddedLayerBlob(t *testing.T) {
	root := validLayout(t, true)
	for name := range root {
		if name != "images/oci-layout" && name != "images/index.json" && name != "images.lock" && len(name) > len("images/blobs/sha256/") {
			data := root[name].Data
			if string(data) == "embedded-layer" {
				delete(root, name)
				break
			}
		}
	}

	_, err := oci.Validate(context.Background(), root)
	if !errors.Is(err, lpkgo.ErrIntegrityMismatch) {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateAllowsMissingUpstreamLayerBlob(t *testing.T) {
	root := validLayout(t, true)

	if _, err := oci.Validate(context.Background(), root); err != nil {
		t.Fatal(err)
	}
}

func validLayout(t *testing.T, mixed bool) fstest.MapFS {
	t.Helper()
	config := []byte(`{"architecture":"amd64","os":"linux","rootfs":{"type":"layers","diff_ids":[]}}`)
	embedded := []byte("embedded-layer")
	upstream := []byte("upstream-layer-not-stored")
	configDigest := digest(config)
	embeddedDigest := digest(embedded)
	upstreamDigest := digest(upstream)
	layers := []oci.Descriptor{{MediaType: oci.MediaTypeImageLayerGzip, Digest: embeddedDigest, Size: int64(len(embedded))}}
	lockLayers := []oci.LockLayer{{Digest: embeddedDigest, Source: oci.LayerSourceEmbed}}
	upstreamRef := ""
	if mixed {
		layers = append([]oci.Descriptor{{MediaType: oci.MediaTypeImageLayer, Digest: upstreamDigest, Size: int64(len(upstream))}}, layers...)
		lockLayers = append([]oci.LockLayer{{Digest: upstreamDigest, Source: oci.LayerSourceUpstream}}, lockLayers...)
		upstreamRef = "registry.example/base@" + upstreamDigest.String()
	}
	manifest := oci.Manifest{
		SchemaVersion: 2,
		MediaType:     oci.MediaTypeImageManifest,
		Config:        oci.Descriptor{MediaType: oci.MediaTypeImageConfig, Digest: configDigest, Size: int64(len(config))},
		Layers:        layers,
	}
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	manifestDigest := digest(manifestData)
	indexData, err := json.Marshal(oci.Index{
		SchemaVersion: 2,
		Manifests: []oci.Descriptor{{
			MediaType: oci.MediaTypeImageManifest,
			Digest:    manifestDigest,
			Size:      int64(len(manifestData)),
			Annotations: map[string]string{
				oci.AnnotationRefName: "app",
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	lockData, err := yaml.Marshal(oci.Lock{
		Version: 1,
		Images: map[string]oci.LockImage{
			"app": {ImageID: configDigest, Upstream: upstreamRef, Layers: lockLayers},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return fstest.MapFS{
		"images/oci-layout":                           &fstest.MapFile{Data: []byte(`{"imageLayoutVersion":"1.0.0"}`)},
		"images/index.json":                           &fstest.MapFile{Data: indexData},
		"images/blobs/sha256/" + configDigest.Hex():   &fstest.MapFile{Data: config},
		"images/blobs/sha256/" + manifestDigest.Hex(): &fstest.MapFile{Data: manifestData},
		"images/blobs/sha256/" + embeddedDigest.Hex(): &fstest.MapFile{Data: embedded},
		"images.lock":                                 &fstest.MapFile{Data: lockData},
	}
}

func digest(data []byte) oci.Digest {
	sum := sha256.Sum256(data)
	parsed, err := oci.ParseDigest("sha256:" + hex.EncodeToString(sum[:]))
	if err != nil {
		panic(err)
	}
	return parsed
}
