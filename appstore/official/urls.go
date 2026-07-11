package official

import (
	"errors"
	"net/url"
	"path"
	"strings"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
)

func (client *Client) AssetURL(assetPath string) (string, error) {
	return client.resolvePath(client.metadataBase, assetPath, "appstore.official.asset_url")
}

func (client *Client) DownloadURL(packagePath string) (string, error) {
	return client.resolvePath(client.downloadBase, packagePath, "appstore.official.download_url")
}

func (client *Client) ApplicationDownloadURL(application Application) (string, error) {
	if client == nil || strings.TrimSpace(application.Package) == "" || application.Package != application.Version.Package || strings.TrimSpace(application.Version.PackagePath) == "" {
		return "", officialError(lpkgo.CodeInvalidArgument, "appstore.official.application_download_url", errors.New("application and version do not match"), 0)
	}
	return client.DownloadURL(application.Version.PackagePath)
}

func (client *Client) resolvePath(base *url.URL, value, op string) (string, error) {
	if client == nil || client.initErr != nil || base == nil {
		return "", officialError(lpkgo.CodeInvalidArgument, op, errors.New("invalid client"), 0)
	}
	value = strings.TrimSpace(value)
	parsed, err := url.Parse(value)
	if err != nil || value == "" || !strings.HasPrefix(value, "/") || parsed.IsAbs() || parsed.Host != "" || parsed.RawQuery != "" || parsed.Fragment != "" || path.Clean(parsed.Path) != parsed.Path {
		return "", officialError(lpkgo.CodeInvalidArgument, op, errors.New("invalid relative path"), 0)
	}
	target := *base
	target.Path = parsed.Path
	target.RawPath = ""
	target.RawQuery = ""
	target.Fragment = ""
	return target.String(), nil
}
