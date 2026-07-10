package manifest

// FieldState records whether a static package field was absent, explicitly
// assigned a value, or explicitly assigned YAML null.
type FieldState uint8

const (
	Absent FieldState = iota
	Value
	Null
)

// PackagePresence records source-level presence for every static package
// field. It is kept separately from the ergonomic typed values so explicit
// empty values and nulls remain distinguishable from absent fields.
type PackagePresence struct {
	Package              FieldState
	Version              FieldState
	Name                 FieldState
	Description          FieldState
	Author               FieldState
	License              FieldState
	Homepage             FieldState
	MinOSVersion         FieldState
	UnsupportedPlatforms FieldState
	Locales              FieldState
}

// PackageInfo is the static application metadata stored in package.yml for
// LPK v2 and at the top level of manifest.yml for legacy LPK v1 packages.
type PackageInfo struct {
	Package              string          `yaml:"package,omitempty"`
	Version              string          `yaml:"version,omitempty"`
	Name                 string          `yaml:"name,omitempty"`
	Description          string          `yaml:"description,omitempty"`
	Author               string          `yaml:"author,omitempty"`
	License              string          `yaml:"license,omitempty"`
	Homepage             string          `yaml:"homepage,omitempty"`
	MinOSVersion         string          `yaml:"min_os_version,omitempty"`
	UnsupportedPlatforms []string        `yaml:"unsupported_platforms,omitempty"`
	Locales              any             `yaml:"locales,omitempty"`
	Presence             PackagePresence `yaml:"-"`
}

// Manifest is the typed consumer view of an LPK manifest. Its Source
// Document remains authoritative for comments and unknown fields.
type Manifest struct {
	PackageInfo `yaml:",inline"`
	Usage       string             `yaml:"usage,omitempty"`
	ExtConfig   ExtConfig          `yaml:"ext_config,omitempty"`
	Application Application        `yaml:"application,omitempty"`
	Services    map[string]Service `yaml:"services,omitempty"`
}

// ExtConfig contains optional platform integration controls.
type ExtConfig struct {
	Permissions              any    `yaml:"permissions,omitempty"`
	EnableDocumentAccess     bool   `yaml:"enable_document_access,omitempty"`
	EnableMediaAccess        bool   `yaml:"enable_media_access,omitempty"`
	EnableClientFSAccess     bool   `yaml:"enable_clientfs_access,omitempty"`
	DisableGRPCWebOnRoot     bool   `yaml:"disable_grpc_web_on_root,omitempty"`
	DefaultPrefixDomain      string `yaml:"default_prefix_domain,omitempty"`
	EnableBindMIMEGlobs      any    `yaml:"enable_bind_mime_globs,omitempty"`
	DisableURLRawPath        bool   `yaml:"disable_url_raw_path,omitempty"`
	RemoveThisRequestHeaders any    `yaml:"remove_this_request_headers,omitempty"`
	FixWebsocketHeader       bool   `yaml:"fix_websocket_header,omitempty"`
}

// Application describes the primary application container and its ingress
// behavior. Fields with multiple accepted YAML shapes use any.
type Application struct {
	Image            string   `yaml:"image,omitempty"`
	BackgroundTask   bool     `yaml:"background_task,omitempty"`
	Subdomain        string   `yaml:"subdomain,omitempty"`
	SecondaryDomains []string `yaml:"secondary_domains,omitempty"`
	MultiInstance    bool     `yaml:"multi_instance,omitempty"`
	USBAccel         bool     `yaml:"usb_accel,omitempty"`
	GPUAccel         bool     `yaml:"gpu_accel,omitempty"`
	KVMAccel         bool     `yaml:"kvm_accel,omitempty"`
	FileHandler      any      `yaml:"file_handler,omitempty"`
	Entries          any      `yaml:"entries,omitempty"`
	Routes           any      `yaml:"routes,omitempty"`
	Upstreams        any      `yaml:"upstreams,omitempty"`
	Injects          any      `yaml:"injects,omitempty"`
	PublicPath       any      `yaml:"public_path,omitempty"`
	Workdir          string   `yaml:"workdir,omitempty"`
	Ingress          any      `yaml:"ingress,omitempty"`
	Environment      any      `yaml:"environment,omitempty"`
	HealthCheck      any      `yaml:"health_check,omitempty"`
	OIDCRedirectPath string   `yaml:"oidc_redirect_path,omitempty"`
	Handlers         any      `yaml:"handlers,omitempty"`
	UserApp          any      `yaml:"user_app,omitempty"`
	DependsOn        any      `yaml:"depends_on,omitempty"`
}

// Service describes an auxiliary Compose-compatible container.
type Service struct {
	Init        bool   `yaml:"init,omitempty"`
	Image       string `yaml:"image,omitempty"`
	Environment any    `yaml:"environment,omitempty"`
	Entrypoint  any    `yaml:"entrypoint,omitempty"`
	Command     any    `yaml:"command,omitempty"`
	Tmpfs       any    `yaml:"tmpfs,omitempty"`
	DependsOn   any    `yaml:"depends_on,omitempty"`
	Healthcheck any    `yaml:"healthcheck,omitempty"`
	HealthCheck any    `yaml:"health_check,omitempty"`
	User        string `yaml:"user,omitempty"`
	CPUShares   int64  `yaml:"cpu_shares,omitempty"`
	CPUs        any    `yaml:"cpus,omitempty"`
	MemLimit    any    `yaml:"mem_limit,omitempty"`
	ShmSize     any    `yaml:"shm_size,omitempty"`
	NetworkMode string `yaml:"network_mode,omitempty"`
	NetAdmin    bool   `yaml:"netadmin,omitempty"`
	SetupScript string `yaml:"setup_script,omitempty"`
	Binds       any    `yaml:"binds,omitempty"`
	Runtime     string `yaml:"runtime,omitempty"`
}
