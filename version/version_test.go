package version

import (
	"slices"
	"testing"
)

func TestCurrentReferenceMetadata(t *testing.T) {
	got := Current()
	if got.SDKVersion != "0.1.0" {
		t.Fatalf("SDKVersion = %q", got.SDKVersion)
	}
	if got.ReferencePackage != "@lazycatcloud/lzc-cli" || got.ReferenceVersion != "2.0.8" {
		t.Fatalf("unexpected reference: %#v", got)
	}
	if got.ReferenceIntegrity != "sha512-CcH18fg1SBqTN4od7NCXMWYaAwjICgEuguphgNcb9Lp7v5+RDYa27+BEevC7faFFm8Zhjw3Rh/sinYc7fc39SA==" {
		t.Fatalf("ReferenceIntegrity = %q", got.ReferenceIntegrity)
	}
	if got.ReferenceShasum != "af9fece8a9756a00e093f817b3c3083971cc171f" {
		t.Fatalf("ReferenceShasum = %q", got.ReferenceShasum)
	}
	if !slices.Equal(got.LPKLayouts, []string{"v1", "v2"}) {
		t.Fatalf("LPKLayouts = %#v", got.LPKLayouts)
	}
	if !slices.Equal(got.ArchiveFormats, []string{"zip", "tar"}) {
		t.Fatalf("ArchiveFormats = %#v", got.ArchiveFormats)
	}
	if got.Backend.LPKV2 != "1.0.0" ||
		got.Backend.PendingSyncDevID != "1.0.4" ||
		got.Backend.BuildPackContextCache != "1.0.4" ||
		got.Backend.BlobManifestTransport != "1.0.5" {
		t.Fatalf("unexpected backend requirements: %#v", got.Backend)
	}
}

func TestCurrentReturnsFreshSlices(t *testing.T) {
	first := Current()
	first.LPKLayouts[0] = "mutated-layout"
	first.ArchiveFormats[0] = "mutated-format"

	second := Current()
	if !slices.Equal(second.LPKLayouts, []string{"v1", "v2"}) {
		t.Fatalf("LPKLayouts = %#v", second.LPKLayouts)
	}
	if !slices.Equal(second.ArchiveFormats, []string{"zip", "tar"}) {
		t.Fatalf("ArchiveFormats = %#v", second.ArchiveFormats)
	}
}
