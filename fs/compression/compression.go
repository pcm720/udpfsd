package compression

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/pcm720/udpfsd/fs/compression/chd"
	"github.com/pcm720/udpfsd/fs/compression/common"
	"github.com/pcm720/udpfsd/fs/compression/cso"
	"github.com/pcm720/udpfsd/fs/compression/zso"
	"github.com/pcm720/udpfsd/fs/interfaces"
)

// BlockReader is the interface that compressed format backends must implement.
type BlockReader interface {
	ReadBlock(blockIdx int) ([]byte, error)
	BlockSize() int
	NumBlocks() int
	UncompressedSize() int64
	Close() error
	Stat() os.FileInfo
}

// Open opens a compressed file by extension and returns a FileWrapper (cached).
// Returns nil if the format is not supported.
func Open(path string, cacheSize int) interfaces.FileObject {
	var br BlockReader
	var err error
	info := &common.CompressedInfo{}
	ext := strings.ToLower(filepath.Ext(path))
	switch {
	case slices.Contains(zso.Extensions, ext):
		z, err := zso.Open(path)
		if err != nil {
			return nil
		}
		br = z
	case slices.Contains(cso.Extensions, ext):
		c, err := cso.Open(path)
		if err != nil {
			return nil
		}
		br = c
	case slices.Contains(chd.Extensions, ext):
		br, err = chd.Open(path)
		if err != nil {
			return nil
		}
	default:
		return nil
	}
	if err != nil || br == nil {
		return nil
	}
	if cacheSize <= 0 {
		cacheSize = 32
	}

	return NewCachedReader(br, cacheSize, info)
}

// Returns a slice of supported compression formats
func GetSupportedFormats() []string {
	formats := []string{"ZSO", "CSO"}
	if chd.Available {
		formats = append(formats, "CHD")
	}
	return formats
}

// Returns a slice of supported compression formats
func GetSupportedExtensions() []string {
	var exts []string
	exts = append(exts, cso.Extensions...)
	exts = append(exts, zso.Extensions...)
	if chd.Available {
		exts = append(exts, chd.Extensions...)
	}
	return exts
}

// Retrieves os.FileInfo for compressed ISO
// Returns nil if file is invalid
func GetStat(path string) os.FileInfo {
	ext := strings.ToLower(filepath.Ext(path))
	var fstat *common.CompressedInfo = nil
	switch {
	case slices.Contains(zso.Extensions, ext):
		fstat = zso.GetStat(path)
	case slices.Contains(cso.Extensions, ext):
		fstat = cso.GetStat(path)
	case slices.Contains(chd.Extensions, ext):
		fstat = chd.GetStat(path)
	}
	if fstat == nil {
		return nil
	}
	return fstat
}
