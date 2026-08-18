//go:build !cgo || nochd

package chd

import (
	"fmt"
	"os"

	"github.com/pcm720/udpfsd/fs/compression/common"
)

// Available is false when built without CGo (no libchdr).
const Available = false

var Extensions = []string{}

type chdStub struct{}

func (*chdStub) ReadBlock(blockIdx int) ([]byte, error) { return nil, nil }
func (*chdStub) BlockSize() int                         { return -1 }
func (*chdStub) NumBlocks() int                         { return -1 }
func (*chdStub) UncompressedSize() int64                { return -1 }
func (*chdStub) Close() error                           { return nil }
func (*chdStub) Stat() os.FileInfo                      { return nil }

func Open(path string) (*chdStub, error) {
	return nil, fmt.Errorf("CHD support requires CGo and libchdr (build with CGO_ENABLED=1 and install libchdr)")
}

func GetStat(path string) *common.CompressedInfo {
	return nil
}
