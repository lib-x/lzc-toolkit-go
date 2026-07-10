package appstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"

	lpkgo "github.com/lib-x/lpk-go"
)

type TestGroup struct {
	ID   string
	Name string
}

type PrePublishRequest struct {
	GroupID   string
	Changelog string
	FileName  string
	Package   io.Reader
}

type PrePublishResult struct {
	Success bool            `json:"success"`
	Message string          `json:"msg"`
	Data    json.RawMessage `json:"data"`
}

func (client *Client) ListTestGroups(ctx context.Context) ([]TestGroup, error) {
	request, err := client.newRequestAt(ctx, http.MethodGet, client.testflightURL+"/groups/dict", nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+request.Header.Get("X-User-Token"))
	body, err := client.do(request)
	if err != nil {
		return nil, err
	}
	var response struct {
		Data []struct {
			ID   any    `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, storeRemoteError("appstore.testflight_groups", errors.New("invalid test group response"), http.StatusOK)
	}
	groups := make([]TestGroup, 0, len(response.Data))
	for _, item := range response.Data {
		groups = append(groups, TestGroup{ID: strings.TrimSpace(fmt.Sprint(item.ID)), Name: item.Name})
	}
	return groups, nil
}

func (client *Client) PrePublish(ctx context.Context, input PrePublishRequest) (PrePublishResult, error) {
	if ctx == nil || strings.TrimSpace(input.GroupID) == "" || strings.TrimSpace(input.Changelog) == "" || input.Package == nil {
		return PrePublishResult{}, storeError(lpkgo.CodeInvalidArgument, "appstore.pre_publish", errors.New("group ID, changelog, and package are required"))
	}
	filename := strings.TrimSpace(input.FileName)
	if filename == "" {
		filename = "application.lpk"
	}
	reader, writer := io.Pipe()
	multipartWriter := multipart.NewWriter(writer)
	writeDone := make(chan error, 1)
	go func() {
		defer close(writeDone)
		if err := multipartWriter.WriteField("type", "Lpk"); err != nil {
			_ = writer.CloseWithError(err)
			writeDone <- err
			return
		}
		if err := multipartWriter.WriteField("changelog", strings.TrimSpace(input.Changelog)); err != nil {
			_ = writer.CloseWithError(err)
			writeDone <- err
			return
		}
		part, err := multipartWriter.CreateFormFile("file", filename)
		if err == nil {
			_, err = io.Copy(part, &testflightContextReader{ctx: ctx, reader: input.Package})
		}
		if err == nil {
			err = multipartWriter.Close()
		}
		if err != nil {
			_ = writer.CloseWithError(err)
		} else {
			err = writer.Close()
		}
		writeDone <- err
	}()

	endpoint := client.testflightURL + "/group/" + url.PathEscape(strings.TrimSpace(input.GroupID)) + "/upload"
	request, err := client.newRequestAt(ctx, http.MethodPost, endpoint, reader)
	if err != nil {
		_ = reader.CloseWithError(err)
		<-writeDone
		return PrePublishResult{}, err
	}
	request.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	request.Header.Set("Authorization", "Bearer "+request.Header.Get("X-User-Token"))
	body, requestErr := client.do(request)
	if requestErr != nil {
		_ = reader.CloseWithError(requestErr)
	}
	writeErr := <-writeDone
	if requestErr != nil {
		return PrePublishResult{}, requestErr
	}
	if writeErr != nil {
		return PrePublishResult{}, storeError(lpkgo.CodeCommandFailed, "appstore.pre_publish", writeErr)
	}
	var result PrePublishResult
	if err := json.Unmarshal(body, &result); err != nil {
		return PrePublishResult{}, storeRemoteError("appstore.pre_publish", errors.New("invalid pre-publish response"), http.StatusOK)
	}
	if !result.Success {
		return result, &lpkgo.Error{Code: lpkgo.CodeRemoteUnavailable, Op: "appstore.pre_publish", Cause: errors.New("pre-publish rejected")}
	}
	return result, nil
}

type testflightContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader *testflightContextReader) Read(data []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.reader.Read(data)
}
