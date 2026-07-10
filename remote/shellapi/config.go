package shellapi

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	lpkgo "github.com/lib-x/lpk-go"
)

type Config struct {
	Address    string
	Credential string
	UID        string
	BoxName    string
	Fallback   bool
}

type ConfigOptions struct {
	ConfigDir   string
	HomeDir     string
	GOOS        string
	Environment map[string]string
}

func LoadConfig(ctx context.Context, options ConfigOptions) (Config, error) {
	if ctx == nil {
		return Config{}, shellError(lpkgo.CodeInvalidArgument, "shellapi.load_config", errors.New("nil context"))
	}
	if err := ctx.Err(); err != nil {
		return Config{}, shellError(lpkgo.CodeCancelled, "shellapi.load_config", err)
	}
	directory := strings.TrimSpace(options.ConfigDir)
	if directory == "" {
		home := strings.TrimSpace(options.HomeDir)
		if home == "" {
			var err error
			home, err = os.UserHomeDir()
			if err != nil {
				return Config{}, shellError(lpkgo.CodeInvalidConfig, "shellapi.load_config", err)
			}
		}
		goos := strings.TrimSpace(options.GOOS)
		if goos == "" {
			goos = runtime.GOOS
		}
		switch goos {
		case "darwin":
			directory = filepath.Join(home, "Library", "Application Support", "hportal-client")
		case "windows":
			directory = filepath.Join(home, "AppData", "Roaming", "hportal-client")
		default:
			directory = filepath.Join(home, ".config", "hportal-client")
		}
	}
	address, addressErr := os.ReadFile(filepath.Join(directory, "shellapi_addr"))
	credential, credentialErr := os.ReadFile(filepath.Join(directory, "shellapi_cred"))
	if addressErr == nil && credentialErr == nil {
		config := Config{Address: strings.TrimSpace(string(address)), Credential: strings.TrimSpace(string(credential))}
		if config.Address == "" || config.Credential == "" {
			return Config{}, shellError(lpkgo.CodeInvalidConfig, "shellapi.load_config", errors.New("empty ShellAPI address or credential"))
		}
		return config, nil
	}
	uid := environmentValue(options.Environment, "BOX_UID")
	boxName := environmentValue(options.Environment, "BOX_NAME")
	if uid == "" || boxName == "" {
		return Config{}, shellError(lpkgo.CodeNotFound, "shellapi.load_config", errors.New("ShellAPI config and BOX_UID/BOX_NAME fallback are unavailable"))
	}
	return Config{UID: uid, BoxName: boxName, Fallback: true}, nil
}

func environmentValue(environment map[string]string, name string) string {
	if environment != nil {
		return strings.TrimSpace(environment[name])
	}
	return strings.TrimSpace(os.Getenv(name))
}
