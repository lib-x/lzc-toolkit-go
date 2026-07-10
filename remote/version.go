package remote

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	lpkgo "github.com/lib-x/lpk-go"
	"github.com/lib-x/lpk-go/version"
)

type Capability string

const (
	CapabilityLPKV2                 Capability = "lpk-v2"
	CapabilityPendingSyncDevID      Capability = "pending-sync-dev-id"
	CapabilityBuildPackContextCache Capability = "build-pack-context-cache"
	CapabilityBlobManifestTransport Capability = "blob-manifest-transport"
)

var platformPattern = regexp.MustCompile(`^([a-z0-9]+)/([a-z0-9]+)$`)
var backendVersionPattern = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)*$`)

func ParsePlatform(value string) (Platform, error) {
	value = strings.TrimSpace(value)
	match := platformPattern.FindStringSubmatch(value)
	if match == nil {
		return Platform{}, remoteError(lpkgo.CodeInvalidArgument, "remote.parse_platform", errors.New("invalid backend platform"))
	}
	return Platform{OS: match[1], Architecture: match[2]}, nil
}

func Supports(capability Capability, currentVersion string) (bool, error) {
	minimum, err := minimumVersion(capability)
	if err != nil {
		return false, err
	}
	comparison, err := compareBackendVersions(currentVersion, minimum)
	if err != nil {
		return false, err
	}
	return comparison >= 0, nil
}

func Require(capability Capability, currentVersion string) error {
	supported, err := Supports(capability, currentVersion)
	if err != nil {
		return err
	}
	if supported {
		return nil
	}
	minimum, _ := minimumVersion(capability)
	return remoteError(
		lpkgo.CodeIncompatibleBackend,
		"remote.require",
		fmt.Errorf("backend capability %s requires version %s", capability, minimum),
	)
}

func minimumVersion(capability Capability) (string, error) {
	requirements := version.Current().Backend
	switch capability {
	case CapabilityLPKV2:
		return requirements.LPKV2, nil
	case CapabilityPendingSyncDevID:
		return requirements.PendingSyncDevID, nil
	case CapabilityBuildPackContextCache:
		return requirements.BuildPackContextCache, nil
	case CapabilityBlobManifestTransport:
		return requirements.BlobManifestTransport, nil
	default:
		return "", remoteError(lpkgo.CodeInvalidArgument, "remote.capability", errors.New("unknown backend capability"))
	}
}

func compareBackendVersions(current, minimum string) (int, error) {
	currentParts, err := parseBackendVersion(current)
	if err != nil {
		return 0, err
	}
	minimumParts, err := parseBackendVersion(minimum)
	if err != nil {
		return 0, err
	}
	count := max(len(currentParts), len(minimumParts))
	for index := range count {
		var currentPart, minimumPart int
		if index < len(currentParts) {
			currentPart = currentParts[index]
		}
		if index < len(minimumParts) {
			minimumPart = minimumParts[index]
		}
		if currentPart > minimumPart {
			return 1, nil
		}
		if currentPart < minimumPart {
			return -1, nil
		}
	}
	return 0, nil
}

func parseBackendVersion(value string) ([]int, error) {
	value = strings.TrimSpace(value)
	if !backendVersionPattern.MatchString(value) {
		return nil, remoteError(lpkgo.CodeInvalidArgument, "remote.backend_version", errors.New("invalid backend version"))
	}
	components := strings.Split(value, ".")
	result := make([]int, len(components))
	for index, component := range components {
		parsed, err := strconv.Atoi(component)
		if err != nil {
			return nil, remoteError(lpkgo.CodeInvalidArgument, "remote.backend_version", errors.New("invalid backend version"))
		}
		result[index] = parsed
	}
	return result, nil
}

func remoteError(code lpkgo.Code, op string, cause error) error {
	return &lpkgo.Error{Code: code, Op: op, Cause: cause}
}
