package official

import (
	"context"
	"errors"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
)

type RankingPeriod string

const (
	RankingWeek  RankingPeriod = "week"
	RankingMonth RankingPeriod = "month"
	RankingAll   RankingPeriod = "all"
)

func (client *Client) DownloadRanking(ctx context.Context, period RankingPeriod) ([]Application, error) {
	if err := client.validate(ctx, "appstore.official.download_ranking"); err != nil {
		return nil, err
	}
	if !validRankingPeriod(period) {
		return nil, officialError(lpkgo.CodeInvalidArgument, "appstore.official.download_ranking", errors.New("invalid ranking period"), 0)
	}
	return getJSON[[]Application](ctx, client, client.stable("app_download_"+string(period)+".json"), "appstore.official.download_ranking")
}

func (client *Client) DeveloperRanking(ctx context.Context, period RankingPeriod) ([]Developer, error) {
	if err := client.validate(ctx, "appstore.official.developer_ranking"); err != nil {
		return nil, err
	}
	if !validRankingPeriod(period) {
		return nil, officialError(lpkgo.CodeInvalidArgument, "appstore.official.developer_ranking", errors.New("invalid ranking period"), 0)
	}
	return getJSON[[]Developer](ctx, client, client.stable("developer_list_"+string(period)+".json"), "appstore.official.developer_ranking")
}

func validRankingPeriod(period RankingPeriod) bool {
	return period == RankingWeek || period == RankingMonth || period == RankingAll
}
