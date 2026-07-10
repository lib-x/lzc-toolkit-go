package remote_test

import (
	"errors"
	"testing"

	lpkgo "github.com/lib-x/lpk-go"
	"github.com/lib-x/lpk-go/remote"
)

func TestSupportsUsesReferenceBackendFloors(t *testing.T) {
	tests := []struct {
		name       string
		capability remote.Capability
		version    string
		want       bool
	}{
		{name: "lpk v2 below", capability: remote.CapabilityLPKV2, version: "0.9.9", want: false},
		{name: "lpk v2 exact", capability: remote.CapabilityLPKV2, version: "1.0.0", want: true},
		{name: "pending below", capability: remote.CapabilityPendingSyncDevID, version: "1.0.3", want: false},
		{name: "pending exact", capability: remote.CapabilityPendingSyncDevID, version: "1.0.4", want: true},
		{name: "context cache exact", capability: remote.CapabilityBuildPackContextCache, version: "1.0.4", want: true},
		{name: "blob below", capability: remote.CapabilityBlobManifestTransport, version: "1.0.4", want: false},
		{name: "blob exact", capability: remote.CapabilityBlobManifestTransport, version: "1.0.5", want: true},
		{name: "newer", capability: remote.CapabilityBlobManifestTransport, version: "2.0.0", want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := remote.Supports(test.capability, test.version)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("Supports(%q, %q) = %v, want %v", test.capability, test.version, got, test.want)
			}
		})
	}
}

func TestSupportsRejectsUnknownCapabilitiesAndMalformedVersions(t *testing.T) {
	for _, test := range []struct {
		capability remote.Capability
		version    string
	}{
		{capability: remote.Capability("unknown"), version: "1.0.5"},
		{capability: remote.CapabilityLPKV2, version: ""},
		{capability: remote.CapabilityLPKV2, version: "v1.0.0"},
		{capability: remote.CapabilityLPKV2, version: "1.x.0"},
	} {
		if _, err := remote.Supports(test.capability, test.version); !errors.Is(err, lpkgo.ErrInvalidArgument) {
			t.Fatalf("Supports(%q, %q) error = %#v", test.capability, test.version, err)
		}
	}
}

func TestRequireReturnsIncompatibleBackend(t *testing.T) {
	err := remote.Require(remote.CapabilityBlobManifestTransport, "1.0.4")
	if !errors.Is(err, lpkgo.ErrIncompatibleBackend) {
		t.Fatalf("Require() error = %#v", err)
	}
	if err := remote.Require(remote.CapabilityBlobManifestTransport, "1.0.5"); err != nil {
		t.Fatal(err)
	}
}

func TestParsePlatform(t *testing.T) {
	for _, value := range []string{"linux/amd64", "linux/arm64"} {
		platform, err := remote.ParsePlatform(value)
		if err != nil {
			t.Fatal(err)
		}
		if platform.String() != value {
			t.Fatalf("ParsePlatform(%q).String() = %q", value, platform.String())
		}
	}
	for _, value := range []string{"", "linux", "Linux/amd64", "linux/x86_64!", "linux/amd64/extra"} {
		if _, err := remote.ParsePlatform(value); !errors.Is(err, lpkgo.ErrInvalidArgument) {
			t.Fatalf("ParsePlatform(%q) error = %#v", value, err)
		}
	}
}
