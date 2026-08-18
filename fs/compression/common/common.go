package common

import (
	"os"
	"time"
)

// CompressedInfo holds uncompressed size and format name for stat/listdir.
type CompressedInfo struct {
	modTime time.Time
	name    string
	size    int64
}

func NewCompressedInfo(name string, size int64, modTime time.Time) *CompressedInfo {
	return &CompressedInfo{
		name:    name,
		size:    size,
		modTime: modTime,
	}
}

func (c *CompressedInfo) Name() string {
	return c.name
}
func (c *CompressedInfo) Size() int64 {
	return c.size
}
func (c *CompressedInfo) Mode() os.FileMode {
	return 0
}
func (c *CompressedInfo) ModTime() time.Time {
	return c.modTime
}
func (c *CompressedInfo) IsDir() bool {
	return false
}
func (c *CompressedInfo) Sys() any {
	return nil
}
