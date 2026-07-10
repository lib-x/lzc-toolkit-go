package archive

import (
	"fmt"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
)

type Limits struct {
	MaxInputBytes    int64
	MaxEntries       int
	MaxFileBytes     int64
	MaxTotalBytes    int64
	MaxPathBytes     int
	MaxDocumentBytes int64
}

func DefaultLimits() Limits {
	return Limits{
		MaxInputBytes:    32 << 30,
		MaxEntries:       1_000_000,
		MaxFileBytes:     16 << 30,
		MaxTotalBytes:    64 << 30,
		MaxPathBytes:     4096,
		MaxDocumentBytes: 16 << 20,
	}
}

func normalizeLimits(limits Limits) (Limits, error) {
	defaults := DefaultLimits()
	if limits.MaxInputBytes == 0 {
		limits.MaxInputBytes = defaults.MaxInputBytes
	}
	if limits.MaxEntries == 0 {
		limits.MaxEntries = defaults.MaxEntries
	}
	if limits.MaxFileBytes == 0 {
		limits.MaxFileBytes = defaults.MaxFileBytes
	}
	if limits.MaxTotalBytes == 0 {
		limits.MaxTotalBytes = defaults.MaxTotalBytes
	}
	if limits.MaxPathBytes == 0 {
		limits.MaxPathBytes = defaults.MaxPathBytes
	}
	if limits.MaxDocumentBytes == 0 {
		limits.MaxDocumentBytes = defaults.MaxDocumentBytes
	}
	if limits.MaxInputBytes < 0 || limits.MaxEntries < 0 || limits.MaxFileBytes < 0 ||
		limits.MaxTotalBytes < 0 || limits.MaxPathBytes < 0 || limits.MaxDocumentBytes < 0 {
		return Limits{}, archiveError(lpkgo.CodeInvalidArgument, "archive.open", fmt.Errorf("negative archive limit"))
	}
	return limits, nil
}
