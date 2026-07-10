// Package oci validates the OCI image layout and images.lock format embedded
// in lzc-cli compatible LPK v2 packages. It has no Docker dependency.
package oci

const (
	MediaTypeImageManifest  = "application/vnd.oci.image.manifest.v1+json"
	MediaTypeImageConfig    = "application/vnd.oci.image.config.v1+json"
	MediaTypeImageLayer     = "application/vnd.oci.image.layer.v1.tar"
	MediaTypeImageLayerGzip = "application/vnd.oci.image.layer.v1.tar+gzip"
	AnnotationRefName       = "org.opencontainers.image.ref.name"
)

type LayerSource string

const (
	LayerSourceEmbed    LayerSource = "embed"
	LayerSourceUpstream LayerSource = "upstream"
)

type Descriptor struct {
	MediaType   string            `json:"mediaType"`
	Digest      Digest            `json:"digest"`
	Size        int64             `json:"size"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

type Index struct {
	SchemaVersion int          `json:"schemaVersion"`
	Manifests     []Descriptor `json:"manifests"`
}

type Manifest struct {
	SchemaVersion int          `json:"schemaVersion"`
	MediaType     string       `json:"mediaType"`
	Config        Descriptor   `json:"config"`
	Layers        []Descriptor `json:"layers"`
}

type LockLayer struct {
	Digest Digest      `yaml:"digest" json:"digest"`
	Source LayerSource `yaml:"source" json:"source"`
}

type LockImage struct {
	ImageID  Digest      `yaml:"image_id" json:"image_id"`
	Upstream string      `yaml:"upstream" json:"upstream"`
	Layers   []LockLayer `yaml:"layers" json:"layers"`
}

type Lock struct {
	Version int                  `yaml:"version" json:"version"`
	Images  map[string]LockImage `yaml:"images" json:"images"`
}

type Layout struct {
	Index     Index
	Lock      Lock
	Manifests map[string]Manifest
	report    Report
}

type Report struct {
	ImageCount         int
	EmbeddedLayerCount int
	UpstreamLayerCount int
	EmbeddedBytes      int64
	ResolvedByAlias    map[string]string
}

func (l *Layout) Report() Report {
	if l == nil {
		return Report{}
	}
	report := l.report
	report.ResolvedByAlias = cloneStrings(report.ResolvedByAlias)
	return report
}

func cloneStrings(source map[string]string) map[string]string {
	clone := make(map[string]string, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}
