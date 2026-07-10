package appstore

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	lpkgo "github.com/lib-x/lpk-go"
)

const imageAPIPath = "/api/v3/developer/app/docker/image/push/v3"

type CopyImageRequest struct {
	Image      string
	Platform   string
	Timeout    time.Duration
	OnProgress func(CopyProgress)
}

type LayerProgress struct {
	Hash     string `json:"hash"`
	Progress int    `json:"progress"`
}

type CopyProgress struct {
	Finished bool            `json:"finished"`
	Layers   []LayerProgress `json:"layers"`
}

type CopyImageResult struct {
	// SourceImage is the image reference submitted to the LazyCat platform.
	SourceImage string
	// Platform is the copied image platform. It defaults to amd64.
	Platform string
	// LazyCatImage is the resulting image reference in the LazyCat registry.
	LazyCatImage string
	// Progress contains the final per-layer copy state.
	Progress CopyProgress
}

type ImageRecord struct {
	SourceImage  string
	LazyCatImage string
	UpdatedAt    time.Time
	ErrorMessage string
}

func (client *Client) CopyImage(ctx context.Context, input CopyImageRequest) (CopyImageResult, error) {
	if ctx == nil {
		return CopyImageResult{}, storeError(lpkgo.CodeInvalidArgument, "appstore.copy_image", errors.New("nil context"))
	}
	image := strings.TrimSpace(input.Image)
	platform := strings.TrimSpace(input.Platform)
	if image == "" {
		return CopyImageResult{}, storeError(lpkgo.CodeInvalidArgument, "appstore.copy_image", errors.New("image is required"))
	}
	if platform == "" {
		platform = "amd64"
	}
	if input.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, input.Timeout)
		defer cancel()
	}
	query := url.Values{"image": {image}, "platform": {platform}}.Encode()
	startRequest, err := client.newRequest(ctx, http.MethodGet, imageAPIPath+"/copy?"+query)
	if err != nil {
		return CopyImageResult{}, err
	}
	if _, err := client.do(startRequest); err != nil {
		return CopyImageResult{}, err
	}
	progressEndpoint := imageAPIPath + "/progress?" + query
	var lastLayers []LayerProgress
	for {
		if err := ctx.Err(); err != nil {
			return CopyImageResult{}, storeError(lpkgo.CodeCancelled, "appstore.copy_image", err)
		}
		progressRequest, err := client.newRequest(ctx, http.MethodGet, progressEndpoint)
		if err != nil {
			return CopyImageResult{}, err
		}
		body, err := client.do(progressRequest)
		if err != nil {
			return CopyImageResult{}, err
		}
		var payload struct {
			Finished bool            `json:"finished"`
			Error    string          `json:"errmsg"`
			Image    string          `json:"lzc_image"`
			Layers   []LayerProgress `json:"layers"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			return CopyImageResult{}, storeRemoteError("appstore.copy_image", errors.New("invalid copy progress response"), http.StatusOK)
		}
		if len(payload.Layers) > 0 {
			lastLayers = append(lastLayers[:0], payload.Layers...)
		}
		if payload.Finished {
			// lzc-cli retains the last reported layer list because the terminal
			// response may contain only finished and lzc_image.
			for i := range lastLayers {
				lastLayers[i].Progress = 100
			}
		}
		progress := CopyProgress{Finished: payload.Finished, Layers: append([]LayerProgress(nil), lastLayers...)}
		if input.OnProgress != nil {
			input.OnProgress(progress)
		}
		if payload.Finished {
			if strings.TrimSpace(payload.Error) != "" {
				return CopyImageResult{}, &lpkgo.Error{Code: lpkgo.CodeRemoteUnavailable, Op: "appstore.copy_image", Cause: errors.New("server-side image copy failed")}
			}
			if strings.TrimSpace(payload.Image) == "" {
				return CopyImageResult{}, storeRemoteError("appstore.copy_image", errors.New("copy result image is missing"), http.StatusOK)
			}
			return CopyImageResult{SourceImage: image, Platform: platform, LazyCatImage: payload.Image, Progress: progress}, nil
		}
		timer := time.NewTimer(client.pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return CopyImageResult{}, storeError(lpkgo.CodeCancelled, "appstore.copy_image", ctx.Err())
		case <-timer.C:
		}
	}
}

func (client *Client) ListImages(ctx context.Context) ([]ImageRecord, error) {
	request, err := client.newRequest(ctx, http.MethodGet, imageAPIPath+"/myimages")
	if err != nil {
		return nil, err
	}
	body, err := client.do(request)
	if err != nil {
		return nil, err
	}
	var payload []struct {
		SourceImage  string `json:"source_image"`
		LazyCatImage string `json:"lzc_image"`
		UpdatedAt    string `json:"UpdatedAt"`
		ErrorMessage string `json:"errmsg"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, storeRemoteError("appstore.list_images", errors.New("invalid image list response"), http.StatusOK)
	}
	result := make([]ImageRecord, 0, len(payload))
	for _, item := range payload {
		updated, _ := time.Parse(time.RFC3339Nano, item.UpdatedAt)
		result = append(result, ImageRecord{SourceImage: item.SourceImage, LazyCatImage: item.LazyCatImage, UpdatedAt: updated, ErrorMessage: item.ErrorMessage})
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].UpdatedAt.After(result[j].UpdatedAt) })
	return result, nil
}
