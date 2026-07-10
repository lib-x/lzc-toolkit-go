package lpk

// Layout identifies the on-disk LPK metadata layout.
type Layout string

const (
	// LayoutV1 is the legacy ZIP-based package layout with manifest.yml.
	LayoutV1 Layout = "v1"
	// LayoutV2 is the TAR-based package layout with package.yml.
	LayoutV2 Layout = "v2"
)
