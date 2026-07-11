package official

import (
	"context"
	"errors"
	"strings"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
)

func (client *Client) Categories(ctx context.Context) ([]Category, error) {
	if err := client.validate(ctx, "appstore.official.categories"); err != nil {
		return nil, err
	}
	return getJSON[[]Category](ctx, client, client.stable("categories.json"), "appstore.official.categories")
}

func (client *Client) Kinds(ctx context.Context) ([]Kind, error) {
	if err := client.validate(ctx, "appstore.official.kinds"); err != nil {
		return nil, err
	}
	return getJSON[[]Kind](ctx, client, client.stable("app_kinds.json"), "appstore.official.kinds")
}

func (client *Client) Homepage(ctx context.Context) ([]HomepageBlock, error) {
	release, err := client.CurrentRelease(ctx)
	if err != nil {
		return nil, err
	}
	return getJSON[[]HomepageBlock](ctx, client, client.snapshot(release, "homepage_block.json"), "appstore.official.homepage")
}

func (client *Client) More(ctx context.Context, blockType string) (HomepageBlock, error) {
	blockType = strings.TrimSpace(blockType)
	if !safeSegment(blockType) {
		return HomepageBlock{}, officialError(lpkgo.CodeInvalidArgument, "appstore.official.more", errors.New("invalid block type"), 0)
	}
	release, err := client.CurrentRelease(ctx)
	if err != nil {
		return HomepageBlock{}, err
	}
	return getJSON[HomepageBlock](ctx, client, client.snapshot(release, "block_"+blockType+".json"), "appstore.official.more")
}
