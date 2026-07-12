package version

import "runtime/debug"

const (
	SDKVersion            = "0.3.2"
	ReferenceCLIPackage   = "@lazycatcloud/lzc-cli"
	ReferenceCLIVersion   = "2.0.8"
	ReferenceCLIIntegrity = "sha512-CcH18fg1SBqTN4od7NCXMWYaAwjICgEuguphgNcb9Lp7v5+RDYa27+BEevC7faFFm8Zhjw3Rh/sinYc7fc39SA=="
	ReferenceCLIShasum    = "af9fece8a9756a00e093f817b3c3083971cc171f"
)

type BackendRequirements struct {
	LPKV2                 string
	PendingSyncDevID      string
	BuildPackContextCache string
	BlobManifestTransport string
}

type Info struct {
	SDKVersion         string
	ModuleVersion      string
	ReferencePackage   string
	ReferenceVersion   string
	ReferenceIntegrity string
	ReferenceShasum    string
	LPKLayouts         []string
	ArchiveFormats     []string
	Backend            BackendRequirements
}

func Current() Info {
	moduleVersion := ""
	if build, ok := debug.ReadBuildInfo(); ok {
		if build.Main.Path == "github.com/lib-x/lzc-toolkit-go" {
			moduleVersion = build.Main.Version
		}
		for _, dep := range build.Deps {
			if dep.Path == "github.com/lib-x/lzc-toolkit-go" {
				moduleVersion = dep.Version
				break
			}
		}
	}
	return Info{
		SDKVersion:         SDKVersion,
		ModuleVersion:      moduleVersion,
		ReferencePackage:   ReferenceCLIPackage,
		ReferenceVersion:   ReferenceCLIVersion,
		ReferenceIntegrity: ReferenceCLIIntegrity,
		ReferenceShasum:    ReferenceCLIShasum,
		LPKLayouts:         []string{"v1", "v2"},
		ArchiveFormats:     []string{"zip", "tar"},
		Backend: BackendRequirements{
			LPKV2:                 "1.0.0",
			PendingSyncDevID:      "1.0.4",
			BuildPackContextCache: "1.0.4",
			BlobManifestTransport: "1.0.5",
		},
	}
}
