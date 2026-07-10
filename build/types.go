// Package build turns an lzc-cli compatible project directory into an LPK.
// It contains no Docker, App Store, SSH, or remote lifecycle implementation.
package build

import (
	"context"
	"io/fs"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
	"github.com/lib-x/lzc-toolkit-go/lpk"
	"github.com/lib-x/lzc-toolkit-go/manifest"
)

const DefaultConfigFile = "lzc-build.yml"

type Profile string

const (
	ProfileRelease     Profile = "release"
	ProfileDevelopment Profile = "dev"
)

// ResourceExport maps one resource kind to a project directory whose direct
// children are resource IDs.
type ResourceExport struct {
	Kind   string `yaml:"kind"`
	Source string `yaml:"source"`
}

// Config is the lzc-build.yml surface supported by lzc-cli 2.0.8.
type Config struct {
	BuildScript      string           `yaml:"buildscript,omitempty"`
	Manifest         string           `yaml:"manifest,omitempty"`
	ContentDir       string           `yaml:"contentdir,omitempty"`
	PackageOutputDir string           `yaml:"pkgout,omitempty"`
	LPKPath          string           `yaml:"lpkPath,omitempty"`
	Icon             string           `yaml:"icon,omitempty"`
	DeployParams     string           `yaml:"deploy_params,omitempty"`
	BrowserExtension string           `yaml:"browser-extension,omitempty"`
	AIPodService     string           `yaml:"ai-pod-service,omitempty"`
	ComposeOverride  map[string]any   `yaml:"compose_override,omitempty"`
	Envs             []string         `yaml:"envs,omitempty"`
	PackageOverride  map[string]any   `yaml:"package_override,omitempty"`
	PackageID        *string          `yaml:"pkg_id,omitempty"`
	PackageName      *string          `yaml:"pkg_name,omitempty"`
	Images           any              `yaml:"images,omitempty"`
	ResourceExports  []ResourceExport `yaml:"resource_exports,omitempty"`
	Remote           map[string]any   `yaml:"remote,omitempty"`
}

// LoadedConfig includes discovery and normalized build-environment metadata.
type LoadedConfig struct {
	Path       string
	ParentPath string
	Profile    Profile
	Config     Config
	Raw        map[string]any
	BuildEnv   map[string]string
}

// Command describes an explicitly authorized project build script.
type Command struct {
	Script string
	Dir    string
	Env    map[string]string
}

// CommandRunner lets callers sandbox, replace, or observe buildscript
// execution. The build package never invokes it unless RunBuildScript is true.
type CommandRunner interface {
	Run(context.Context, Command) error
}

// ImageBuildRequest is the dependency-light input passed to an optional image
// adapter. Docker and remote implementations live outside package build.
type ImageBuildRequest struct {
	Root     string
	Config   any
	Manifest manifest.Manifest
}

// ImageArtifact owns a completed package-relative OCI filesystem containing
// images.lock and images/. Build copies it before calling Close.
type ImageArtifact interface {
	FS() fs.FS
	Close() error
}

// ImageBuilder builds the OCI artifact for an images configuration.
type ImageBuilder interface {
	Build(context.Context, ImageBuildRequest) (ImageArtifact, error)
}

// Request describes a project build. Root defaults to the current directory,
// ConfigFile defaults to lzc-build.yml, and build scripts are disabled unless
// RunBuildScript is explicitly set.
type Request struct {
	Root               string
	ConfigFile         string
	Environment        map[string]string
	InheritEnvironment bool
	LocalIP            string
	VersionOverride    string
	ForceV2            bool
	Strict             bool
	RunBuildScript     bool
	Runner             CommandRunner
	ImageBuilder       ImageBuilder
}

// Result reports the selected configuration and encoded package metadata.
type Result struct {
	ConfigPath     string
	Profile        Profile
	Layout         lpk.Layout
	Package        string
	Version        string
	Warnings       []lpkgo.Warning
	Write          lpk.WriteResult
	ImageCount     int
	ResolvedImages map[string]string
}
