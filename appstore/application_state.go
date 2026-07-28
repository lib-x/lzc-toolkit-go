package appstore

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
)

type ApplicationState struct {
	Exists           bool
	ID               int
	InformationReady bool
	ReviewPending    bool
	WaitingReviewID  int
}

// ApplicationState resolves an exact package through the authenticated
// developer application list. Unlike the public catalog, this distinguishes
// a missing application from an application whose first information review
// has not completed yet.
func (client *Client) ApplicationState(ctx context.Context, packageID string) (ApplicationState, error) {
	packageID = strings.TrimSpace(packageID)
	if packageID == "" {
		return ApplicationState{}, storeError(lpkgo.CodeInvalidArgument, "appstore.application_state", errors.New("package is required"))
	}
	query := url.Values{
		"seek": {packageID}, "sort": {"-id"}, "size": {"100"}, "page": {"0"},
	}.Encode()
	request, err := client.newRequest(ctx, http.MethodGet, "/api/v3/developer/app/list?"+query)
	if err != nil {
		return ApplicationState{}, err
	}
	body, err := client.do(request)
	if err != nil {
		return ApplicationState{}, err
	}
	type developerApplication struct {
		ID              int    `json:"id"`
		Package         string `json:"package"`
		WaitingReviewID *int   `json:"waiting_review_id"`
		Resource        struct {
			InfoData map[string]ApplicationInfo `json:"info_data"`
		} `json:"resource"`
	}
	var payload struct {
		Items []developerApplication `json:"items"`
		Data  *struct {
			Items []developerApplication `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ApplicationState{}, storeRemoteError("appstore.application_state", errors.New("invalid application list response"), http.StatusOK)
	}
	items := payload.Items
	if len(items) == 0 && payload.Data != nil {
		items = payload.Data.Items
	}
	var exact *developerApplication
	for index := range items {
		if strings.TrimSpace(items[index].Package) != packageID {
			continue
		}
		if exact != nil {
			return ApplicationState{}, storeRemoteError("appstore.application_state", errors.New("ambiguous exact application response"), http.StatusOK)
		}
		exact = &items[index]
	}
	if exact == nil {
		return ApplicationState{}, nil
	}
	state := ApplicationState{Exists: true, ID: exact.ID}
	if exact.WaitingReviewID != nil && *exact.WaitingReviewID > 0 {
		state.ReviewPending = true
		state.WaitingReviewID = *exact.WaitingReviewID
	}
	for _, information := range exact.Resource.InfoData {
		if applicationInformationReady(information) {
			state.InformationReady = true
			break
		}
	}
	return state, nil
}

func applicationInformationReady(information ApplicationInfo) bool {
	if strings.TrimSpace(information.Language) == "" || strings.TrimSpace(information.Name) == "" || strings.TrimSpace(information.Brief) == "" {
		return false
	}
	desktop := information.SupportPC && len(information.ScreenshotPCPaths) >= 2 && validApplicationScreenshotPaths(information.ScreenshotPCPaths)
	mobile := information.SupportMobile && len(information.ScreenshotMobilePaths) >= 3 && validApplicationScreenshotPaths(information.ScreenshotMobilePaths)
	return desktop || mobile
}
