package official

import (
	"context"
	"errors"
	"net/http"
	"strings"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
)

// Application returns the current public metadata and latest version for an
// exact package ID.
func (client *Client) Application(ctx context.Context, packageID string) (Application, error) {
	if err := client.validate(ctx, "appstore.official.application"); err != nil {
		return Application{}, err
	}
	packageID = strings.TrimSpace(packageID)
	if !safeSegment(packageID) {
		return Application{}, officialError(lpkgo.CodeInvalidArgument, "appstore.official.application", errors.New("invalid package ID"), 0)
	}
	application, err := getJSON[Application](ctx, client, client.stable("v3", "app_"+packageID+".json"), "appstore.official.application")
	if err != nil {
		return Application{}, err
	}
	if application.Package != packageID || application.Version.Package != packageID || strings.TrimSpace(application.Version.Name) == "" {
		return Application{}, officialError(lpkgo.CodeRemoteUnavailable, "appstore.official.application", errors.New("application response identity mismatch"), http.StatusOK)
	}
	return application, nil
}

// VersionChangelog returns the localized changelog for an exact version.
func (client *Client) VersionChangelog(ctx context.Context, packageID, version string) (string, error) {
	if err := client.validate(ctx, "appstore.official.version_changelog"); err != nil {
		return "", err
	}
	packageID = strings.TrimSpace(packageID)
	version = strings.TrimSpace(version)
	if !safeSegment(packageID) || !safeSegment(version) {
		return "", officialError(lpkgo.CodeInvalidArgument, "appstore.official.version_changelog", errors.New("invalid package ID or version"), 0)
	}
	return getJSON[string](ctx, client, client.stable("apps", packageID, version+".changelog.json"), "appstore.official.version_changelog")
}
