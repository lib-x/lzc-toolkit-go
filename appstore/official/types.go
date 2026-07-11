package official

import "time"

type Application struct {
	ID               int             `json:"id"`
	Package          string          `json:"package"`
	KindIDs          string          `json:"kind_ids"`
	CategoryIDs      []int           `json:"category_ids"`
	Status           int             `json:"status"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
	VersionUpdatedAt time.Time       `json:"version_updated_at"`
	CreateUser       Developer       `json:"create_user"`
	Information      ApplicationInfo `json:"information"`
	Version          Version         `json:"version"`
	Rating           Rating          `json:"rating"`
	IsOriginal       bool            `json:"is_original"`
	Count            Counts          `json:"count"`
}

type ApplicationInfo struct {
	ID                    int      `json:"id"`
	CreateUserID          int      `json:"create_user_id"`
	AppID                 int      `json:"app_id"`
	Language              string   `json:"language"`
	Name                  string   `json:"name"`
	Brief                 string   `json:"brief"`
	Description           string   `json:"description"`
	Keywords              string   `json:"keywords"`
	Source                string   `json:"source"`
	SourceAuthor          string   `json:"source_author"`
	SupportPC             bool     `json:"support_pc"`
	SupportMobile         bool     `json:"support_mobile"`
	ScreenshotPCPaths     []string `json:"screenshot_pc_paths"`
	ScreenshotMobilePaths []string `json:"screenshot_mobile_paths"`
}

type Version struct {
	ID                   int      `json:"id"`
	CreateUserID         int      `json:"create_user_id"`
	AppID                int      `json:"app_id"`
	Name                 string   `json:"name"`
	Package              string   `json:"package"`
	PackageHash          string   `json:"pkg_hash"`
	PackagePath          string   `json:"pkg_path"`
	IconPath             string   `json:"icon_path"`
	UnsupportedPlatforms []string `json:"unsupported_platforms"`
	MinOSVersion         string   `json:"min_os_version"`
	ChangelogList        []string `json:"changelog_list"`
	ChangelogLanguage    string   `json:"changelog_language"`
	LPKSize              int64    `json:"lpk_size"`
	ImageSize            int64    `json:"image_size"`
}

type Developer struct {
	DeveloperID                  int           `json:"developer_id"`
	ID                           int           `json:"id"`
	Username                     string        `json:"username"`
	Nickname                     string        `json:"nickname"`
	Description                  string        `json:"description"`
	Avatar                       string        `json:"avatar"`
	GitHubUsername               string        `json:"github_username"`
	ContinuousSubmissionDayCount int           `json:"continuous_submission_day_count"`
	IsOfficial                   bool          `json:"is_official"`
	AppTotalCount                int           `json:"app_total_count"`
	Apps                         []Application `json:"apps,omitempty"`
}

type Rating struct {
	Score      float64          `json:"score"`
	Statistics RatingStatistics `json:"statistics"`
}

type RatingStatistics struct {
	Total int `json:"total"`
	One   int `json:"one"`
	Two   int `json:"two"`
	Three int `json:"three"`
	Four  int `json:"four"`
	Five  int `json:"five"`
}

type Counts struct {
	Downloads   int `json:"downloads"`
	Likes       int `json:"likes"`
	Comments    int `json:"comments"`
	RemindCount int `json:"remind_count"`
}

type Category struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Icon string `json:"icon"`
}

type Kind struct {
	ID       int    `json:"id"`
	Code     string `json:"code"`
	Name     string `json:"name"`
	OrderNum int    `json:"order_num"`
}

type HomepageBlockOptions struct {
	ShowMore bool `json:"show_more"`
}

type HomepageBlock struct {
	ID                int                   `json:"id"`
	Name              string                `json:"name"`
	BlockType         string                `json:"block_type"`
	APIPath           string                `json:"api_path"`
	HomepageShowLimit int                   `json:"homepage_show_limit"`
	Options           *HomepageBlockOptions `json:"options"`
	Data              []Application         `json:"data"`
}
