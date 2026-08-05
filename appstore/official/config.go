package official

import (
	"errors"
	"strings"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
)

// MetadataBaseURL resolves a domain-only official metarepo override.
func MetadataBaseURL(domain string) (string, error) {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return DefaultMetadataBaseURL, nil
	}
	if strings.Contains(domain, "://") || strings.ContainsAny(domain, ":/?#") || strings.ContainsAny(domain, " \t\r\n") {
		return "", officialError(lpkgo.CodeInvalidArgument, "appstore.official.metadata_base_url", errors.New("official App Store metadata domain must contain only a domain name"), 0)
	}
	return "https://" + domain + "/appstore/metarepo", nil
}
