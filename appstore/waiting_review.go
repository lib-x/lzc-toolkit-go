package appstore

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
	"github.com/lib-x/lzc-toolkit-go/internal/packageid"
)

// WaitingReviewVersion returns the version currently awaiting review for an
// exact package ID. The boolean is false when the developer platform returns
// 404, which means that the package has no review in progress.
func (client *Client) WaitingReviewVersion(ctx context.Context, packageID string) (string, bool, error) {
	packageID = strings.TrimSpace(packageID)
	if !packageid.Valid(packageID) {
		return "", false, storeError(lpkgo.CodeInvalidArgument, "appstore.waiting_review_version", errors.New("invalid package ID"))
	}
	request, err := client.newRequest(ctx, http.MethodGet, "/api/v3/developer/app/"+url.PathEscape(packageID)+"/review/waiting")
	if err != nil {
		return "", false, err
	}
	body, found, err := client.doOptional(request, true)
	if err != nil || !found {
		return "", found, err
	}
	var response struct {
		Version struct {
			Name string `json:"name"`
		} `json:"version"`
	}
	if err := json.Unmarshal(body, &response); err != nil || strings.TrimSpace(response.Version.Name) == "" {
		return "", false, storeRemoteError("appstore.waiting_review_version", errors.New("invalid waiting review response"), http.StatusOK)
	}
	return strings.TrimSpace(response.Version.Name), true, nil
}
