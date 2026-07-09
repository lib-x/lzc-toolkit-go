package version

import "testing"

func TestCurrentReferenceMetadata(t *testing.T) {
	got := Current()
	if got.SDKVersion != "0.1.0" {
		t.Fatalf("SDKVersion = %q", got.SDKVersion)
	}
	if got.ReferencePackage != "@lazycatcloud/lzc-cli" || got.ReferenceVersion != "2.0.8" {
		t.Fatalf("unexpected reference: %#v", got)
	}
	if got.Backend.LPKV2 != "1.0.0" ||
		got.Backend.PendingSyncDevID != "1.0.4" ||
		got.Backend.BuildPackContextCache != "1.0.4" ||
		got.Backend.BlobManifestTransport != "1.0.5" {
		t.Fatalf("unexpected backend requirements: %#v", got.Backend)
	}
}
