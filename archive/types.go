package archive

import (
	"io/fs"
	"time"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
)

type EntryType string

const (
	EntryDirectory EntryType = "directory"
	EntryRegular   EntryType = "regular"
	EntrySymlink   EntryType = "symlink"
	EntryHardlink  EntryType = "hardlink"
)

type Entry struct {
	Name     string
	Type     EntryType
	Mode     fs.FileMode
	Size     int64
	Linkname string
	ModTime  time.Time
}

func archiveError(code lpkgo.Code, op string, cause error) error {
	if cause == nil {
		return nil
	}
	return &lpkgo.Error{Code: code, Op: op, Cause: cause}
}
