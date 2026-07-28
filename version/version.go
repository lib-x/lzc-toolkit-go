package version

import "runtime/debug"

const (
	SDKVersion            = "0.3.5"
	ReferenceCLIPackage   = "@lazycatcloud/lzc-cli"
	ReferenceCLIVersion   = "2.0.9"
	ReferenceCLIIntegrity = "sha512-L+DUKBD5HrFctnqZ4a8vofXY7f5+4ukpfw4rSnNbeE9s48lsLOr3vvbaWZCDSR6xkivRYTovQMWKqcli6s8mUQ=="
	ReferenceCLIShasum    = "88a3847bbd1c0c2e709cbc7a96fae52f9f832a85"
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
